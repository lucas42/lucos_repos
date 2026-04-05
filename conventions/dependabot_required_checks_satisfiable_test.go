package conventions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// dependabotSatisfiableServerOpts configures the test server for
// dependabot-required-checks-satisfiable tests.
type dependabotSatisfiableServerOpts struct {
	hasDependabotYML    bool
	requiredChecks      []string // nil or empty = no required checks
	dependabotPRSHA     string   // empty = no Dependabot PR found
	dependabotPRChecks  []string // check runs on the Dependabot PR head
}

// dependabotSatisfiableServer creates a test HTTP server that serves just
// enough of the GitHub API for the dependabot-required-checks-satisfiable
// convention tests.
func dependabotSatisfiableServer(t *testing.T, opts dependabotSatisfiableServerOpts) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		type checkRun struct {
			Name string `json:"name"`
		}
		type checkRunsResp struct {
			CheckRuns []checkRun `json:"check_runs"`
		}

		switch r.URL.Path {
		case "/repos/lucas42/test_repo/contents/.github/dependabot.yml":
			if opts.hasDependabotYML {
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]string{"type": "file"})
			} else {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"message":"Not Found"}`))
			}

		case "/repos/lucas42/test_repo/branches/main/protection":
			if len(opts.requiredChecks) > 0 {
				w.WriteHeader(http.StatusOK)
				w.Write(branchProtectionFixture(opts.requiredChecks))
			} else {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"message":"Branch not protected"}`))
			}

		case "/repos/lucas42/test_repo/pulls":
			if opts.dependabotPRSHA != "" {
				type prUser struct {
					Login string `json:"login"`
				}
				type prHead struct {
					SHA string `json:"sha"`
				}
				type pr struct {
					Number int    `json:"number"`
					Head   prHead `json:"head"`
					User   prUser `json:"user"`
				}
				json.NewEncoder(w).Encode([]pr{
					{Number: 5, Head: prHead{SHA: opts.dependabotPRSHA}, User: prUser{Login: "dependabot[bot]"}},
				})
			} else {
				json.NewEncoder(w).Encode([]struct{}{})
			}

		default:
			// Handle Dependabot PR head commit check-runs endpoint.
			if opts.dependabotPRSHA != "" {
				if r.URL.Path == "/repos/lucas42/test_repo/commits/"+opts.dependabotPRSHA+"/check-runs" {
					resp := checkRunsResp{}
					for _, name := range opts.dependabotPRChecks {
						resp.CheckRuns = append(resp.CheckRuns, checkRun{Name: name})
					}
					json.NewEncoder(w).Encode(resp)
					return
				}
				if r.URL.Path == "/repos/lucas42/test_repo/commits/"+opts.dependabotPRSHA+"/status" {
					// No status contexts in these tests (check runs only).
					json.NewEncoder(w).Encode(combinedStatusResponse{})
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestDependabotRequiredChecksSatisfiable_Registered(t *testing.T) {
	c := findConvention(t, "dependabot-required-checks-satisfiable")
	if c.Description == "" {
		t.Error("empty description")
	}
	if c.Rationale == "" {
		t.Error("empty rationale")
	}
	if c.Guidance == "" {
		t.Error("empty guidance")
	}
	if !c.AppliesToType(RepoTypeSystem) {
		t.Error("should apply to RepoTypeSystem")
	}
	if !c.AppliesToType(RepoTypeComponent) {
		t.Error("should apply to RepoTypeComponent")
	}
	if c.AppliesToType(RepoTypeScript) {
		t.Error("should not apply to RepoTypeScript")
	}
	if !c.ScheduledOnly {
		t.Error("should be ScheduledOnly")
	}
}

func TestDependabotRequiredChecksSatisfiable_NoDependabotYML(t *testing.T) {
	server := dependabotSatisfiableServer(t, dependabotSatisfiableServerOpts{
		hasDependabotYML: false,
	})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "dependabot-required-checks-satisfiable").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when no dependabot.yml, got: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "dependabot.yml") {
		t.Errorf("expected Detail to mention dependabot.yml, got: %s", result.Detail)
	}
}

func TestDependabotRequiredChecksSatisfiable_NoRequiredChecks(t *testing.T) {
	server := dependabotSatisfiableServer(t, dependabotSatisfiableServerOpts{
		hasDependabotYML: true,
		requiredChecks:   nil,
	})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "dependabot-required-checks-satisfiable").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when no required checks, got: %s", result.Detail)
	}
}

func TestDependabotRequiredChecksSatisfiable_NoDependabotPRs(t *testing.T) {
	server := dependabotSatisfiableServer(t, dependabotSatisfiableServerOpts{
		hasDependabotYML: true,
		requiredChecks:   []string{"ci/circleci: test", "CodeQL"},
		dependabotPRSHA:  "",
	})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "dependabot-required-checks-satisfiable").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when no Dependabot PRs found (can't verify), got: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "no recent Dependabot PRs") {
		t.Errorf("expected Detail to mention no recent Dependabot PRs, got: %s", result.Detail)
	}
}

func TestDependabotRequiredChecksSatisfiable_AllChecksFire(t *testing.T) {
	server := dependabotSatisfiableServer(t, dependabotSatisfiableServerOpts{
		hasDependabotYML:   true,
		requiredChecks:     []string{"ci/circleci: test", "CodeQL"},
		dependabotPRSHA:    "abc123",
		dependabotPRChecks: []string{"ci/circleci: test", "CodeQL"},
	})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "dependabot-required-checks-satisfiable").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when all required checks fire on Dependabot PR, got: %s", result.Detail)
	}
}

func TestDependabotRequiredChecksSatisfiable_MissingCheck(t *testing.T) {
	// CodeQL ("Analyze (actions)") is required but does not fire on Dependabot PRs.
	server := dependabotSatisfiableServer(t, dependabotSatisfiableServerOpts{
		hasDependabotYML:   true,
		requiredChecks:     []string{"ci/circleci: test", "Analyze (actions)"},
		dependabotPRSHA:    "abc123",
		dependabotPRChecks: []string{"ci/circleci: test"},
	})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "dependabot-required-checks-satisfiable").Check(repo)
	if result.Pass {
		t.Errorf("expected fail when required check missing from Dependabot PR, got pass: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "Analyze (actions)") {
		t.Errorf("expected Detail to mention missing check name, got: %s", result.Detail)
	}
}

func TestDependabotRequiredChecksSatisfiable_MultipleMissingChecks(t *testing.T) {
	server := dependabotSatisfiableServer(t, dependabotSatisfiableServerOpts{
		hasDependabotYML:   true,
		requiredChecks:     []string{"ci/circleci: test", "Analyze (actions)", "another-check"},
		dependabotPRSHA:    "abc123",
		dependabotPRChecks: []string{"ci/circleci: test"},
	})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "dependabot-required-checks-satisfiable").Check(repo)
	if result.Pass {
		t.Errorf("expected fail when multiple required checks missing, got pass: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "Analyze (actions)") {
		t.Errorf("expected Detail to mention Analyze (actions), got: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "another-check") {
		t.Errorf("expected Detail to mention another-check, got: %s", result.Detail)
	}
}

func TestDependabotRequiredChecksSatisfiable_IgnoresNonDependabotPRs(t *testing.T) {
	// The test server returns a human-authored PR — convention should skip it
	// and report "no recent Dependabot PRs found".
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/lucas42/test_repo/contents/.github/dependabot.yml":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"type": "file"})
		case "/repos/lucas42/test_repo/branches/main/protection":
			w.WriteHeader(http.StatusOK)
			w.Write(branchProtectionFixture([]string{"ci/circleci: test"}))
		case "/repos/lucas42/test_repo/pulls":
			// Human-authored PR only.
			type prUser struct{ Login string `json:"login"` }
			type prHead struct{ SHA string `json:"sha"` }
			type pr struct {
				Number int    `json:"number"`
				Head   prHead `json:"head"`
				User   prUser `json:"user"`
			}
			json.NewEncoder(w).Encode([]pr{
				{Number: 1, Head: prHead{SHA: "humansha"}, User: prUser{Login: "lucas42"}},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "dependabot-required-checks-satisfiable").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass (no Dependabot PRs to sample), got fail: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "no recent Dependabot PRs") {
		t.Errorf("expected Detail to mention no recent Dependabot PRs, got: %s", result.Detail)
	}
}
