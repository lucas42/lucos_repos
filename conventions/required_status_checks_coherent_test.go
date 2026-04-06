package conventions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// coherentChecksServerOpts configures the test server for
// required-status-checks-coherent tests.
type coherentChecksServerOpts struct {
	requiredChecks       []string          // nil/empty = no branch protection
	headStatusContexts   []string          // status contexts on HEAD of main
	headCheckRunNames    []string          // check run names on HEAD of main
	languages            map[string]int    // repo languages (nil = empty map)
	hasDependabotYML     bool              // whether .github/dependabot.yml exists
	dependabotPRSHA      string            // empty = no Dependabot PR found
	dependabotPRChecks   []string          // check runs on the Dependabot PR head
}

// coherentChecksServer builds a test HTTP server that covers all GitHub API
// endpoints used by the required-status-checks-coherent convention.
func coherentChecksServer(t *testing.T, opts coherentChecksServerOpts) *httptest.Server {
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
		case "/repos/lucas42/test_repo/branches/main/protection":
			if len(opts.requiredChecks) > 0 {
				w.WriteHeader(http.StatusOK)
				w.Write(branchProtectionFixture(opts.requiredChecks))
			} else {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"message":"Branch not protected"}`))
			}

		case "/repos/lucas42/test_repo/commits/heads/main/status":
			resp := combinedStatusResponse{}
			for _, ctx := range opts.headStatusContexts {
				resp.Statuses = append(resp.Statuses, statusEntry{Context: ctx})
			}
			json.NewEncoder(w).Encode(resp)

		case "/repos/lucas42/test_repo/commits/heads/main/check-runs":
			resp := checkRunsResp{}
			for _, name := range opts.headCheckRunNames {
				resp.CheckRuns = append(resp.CheckRuns, checkRun{Name: name})
			}
			json.NewEncoder(w).Encode(resp)

		case "/repos/lucas42/test_repo/languages":
			langs := opts.languages
			if langs == nil {
				langs = map[string]int{}
			}
			json.NewEncoder(w).Encode(langs)

		case "/repos/lucas42/test_repo/contents/.github/dependabot.yml":
			if opts.hasDependabotYML {
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]string{"type": "file"})
			} else {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"message":"Not Found"}`))
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
			// Handle Dependabot PR head commit endpoints (dynamic SHA path).
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
					json.NewEncoder(w).Encode(combinedStatusResponse{})
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestRequiredStatusChecksCoherent_Registered(t *testing.T) {
	c := findConvention(t, "required-status-checks-coherent")
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

func TestRequiredStatusChecksCoherent_NoRequiredChecks(t *testing.T) {
	server := coherentChecksServer(t, coherentChecksServerOpts{})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "required-status-checks-coherent").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when no required checks configured, got: %s", result.Detail)
	}
}

func TestRequiredStatusChecksCoherent_AllCoherent(t *testing.T) {
	// Required: "ci/circleci: test" and "Analyze (go)"
	// HEAD reports both; repo has Go; Dependabot has a PR that reports both.
	server := coherentChecksServer(t, coherentChecksServerOpts{
		requiredChecks:     []string{"ci/circleci: test", "Analyze (go)"},
		headStatusContexts: []string{"ci/circleci: test"},
		headCheckRunNames:  []string{"Analyze (go)"},
		languages:          map[string]int{"Go": 10000},
		hasDependabotYML:   true,
		dependabotPRSHA:    "dep123",
		dependabotPRChecks: []string{"ci/circleci: test", "Analyze (go)"},
	})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "required-status-checks-coherent").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when all checks are coherent, got: %s", result.Detail)
	}
}

func TestRequiredStatusChecksCoherent_StaleCheck(t *testing.T) {
	// "CodeQL" is required but HEAD only reports "Analyze (go)".
	server := coherentChecksServer(t, coherentChecksServerOpts{
		requiredChecks:    []string{"ci/circleci: test", "CodeQL"},
		headCheckRunNames: []string{"ci/circleci: test", "Analyze (go)"},
		languages:         map[string]int{"Go": 10000},
	})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "required-status-checks-coherent").Check(repo)
	if result.Pass {
		t.Errorf("expected fail when stale check is required, got pass: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "CodeQL") {
		t.Errorf("expected Detail to mention stale check name 'CodeQL', got: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "stale") {
		t.Errorf("expected Detail to mention 'stale', got: %s", result.Detail)
	}
}

func TestRequiredStatusChecksCoherent_NoAnalyzeCheck_CodeQLLanguage(t *testing.T) {
	// Repo has Go (CodeQL-supported) but no Analyze (X) in required checks.
	server := coherentChecksServer(t, coherentChecksServerOpts{
		requiredChecks:    []string{"ci/circleci: test"},
		headCheckRunNames: []string{"ci/circleci: test"},
		languages:         map[string]int{"Go": 10000},
	})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "required-status-checks-coherent").Check(repo)
	if result.Pass {
		t.Errorf("expected fail when no CodeQL Analyze check required for CodeQL-enabled repo, got pass: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "Analyze") {
		t.Errorf("expected Detail to mention Analyze, got: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "CodeQL") {
		t.Errorf("expected Detail to mention CodeQL, got: %s", result.Detail)
	}
}

func TestRequiredStatusChecksCoherent_NoCodeQLLanguage_SkipsAnalyzeCheck(t *testing.T) {
	// Repo has only PHP (not CodeQL-supported) — no CodeQL Analyze check needed.
	server := coherentChecksServer(t, coherentChecksServerOpts{
		requiredChecks:    []string{"ci/circleci: test"},
		headCheckRunNames: []string{"ci/circleci: test"},
		languages:         map[string]int{"PHP": 10000},
	})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "required-status-checks-coherent").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when repo has no CodeQL-supported languages, got: %s", result.Detail)
	}
}

func TestRequiredStatusChecksCoherent_NoDependabotYML(t *testing.T) {
	// No .github/dependabot.yml — Dependabot aspect should be skipped entirely.
	server := coherentChecksServer(t, coherentChecksServerOpts{
		requiredChecks:    []string{"ci/circleci: test", "Analyze (go)"},
		headCheckRunNames: []string{"ci/circleci: test", "Analyze (go)"},
		languages:         map[string]int{"Go": 10000},
		hasDependabotYML:  false,
	})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "required-status-checks-coherent").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when no dependabot.yml (Dependabot aspect skipped), got: %s", result.Detail)
	}
}

func TestRequiredStatusChecksCoherent_NoDependabotPRs(t *testing.T) {
	// .github/dependabot.yml exists but no Dependabot PRs to sample — skip
	// the satisfiability check rather than failing.
	server := coherentChecksServer(t, coherentChecksServerOpts{
		requiredChecks:    []string{"ci/circleci: test", "Analyze (go)"},
		headCheckRunNames: []string{"ci/circleci: test", "Analyze (go)"},
		languages:         map[string]int{"Go": 10000},
		hasDependabotYML:  true,
		dependabotPRSHA:   "", // no Dependabot PR
	})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "required-status-checks-coherent").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when no recent Dependabot PRs (can't verify), got: %s", result.Detail)
	}
}

func TestRequiredStatusChecksCoherent_DependabotUnsatisfiable(t *testing.T) {
	// "Analyze (go)" fires on HEAD of main but not on the Dependabot PR.
	server := coherentChecksServer(t, coherentChecksServerOpts{
		requiredChecks:     []string{"ci/circleci: test", "Analyze (go)"},
		headCheckRunNames:  []string{"ci/circleci: test", "Analyze (go)"},
		languages:          map[string]int{"Go": 10000},
		hasDependabotYML:   true,
		dependabotPRSHA:    "dep123",
		dependabotPRChecks: []string{"ci/circleci: test"},
	})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "required-status-checks-coherent").Check(repo)
	if result.Pass {
		t.Errorf("expected fail when required check doesn't fire on Dependabot PR, got pass: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "Analyze (go)") {
		t.Errorf("expected Detail to mention 'Analyze (go)', got: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "Dependabot") {
		t.Errorf("expected Detail to mention 'Dependabot', got: %s", result.Detail)
	}
}

func TestRequiredStatusChecksCoherent_EmptyHeadChecks_NoStaleFlag(t *testing.T) {
	// HEAD reports no checks at all (e.g. docs-only commit with path filters).
	// The stale check aspect should be skipped rather than flagging everything
	// as stale.
	server := coherentChecksServer(t, coherentChecksServerOpts{
		requiredChecks:     []string{"ci/circleci: test", "Analyze (go)"},
		headStatusContexts: nil,
		headCheckRunNames:  nil,
		languages:          map[string]int{"Go": 10000},
		hasDependabotYML:   true,
		dependabotPRSHA:    "dep123",
		dependabotPRChecks: []string{"ci/circleci: test", "Analyze (go)"},
	})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "required-status-checks-coherent").Check(repo)
	// The stale-check aspect is skipped (empty HEAD), CodeQL is covered,
	// Dependabot is satisfiable — should pass.
	if !result.Pass {
		t.Errorf("expected pass when HEAD reports no checks (docs-only commit), got: %s", result.Detail)
	}
}

func TestRequiredStatusChecksCoherent_MultipleIssues(t *testing.T) {
	// Three issues at once:
	// 1. "CodeQL" is stale (not on HEAD)
	// 2. No Analyze (X) check (so CodeQL coverage missing)
	// 3. "ci/circleci: test" is not on the Dependabot PR
	server := coherentChecksServer(t, coherentChecksServerOpts{
		requiredChecks:     []string{"CodeQL", "ci/circleci: test"},
		headCheckRunNames:  []string{"Analyze (go)", "ci/circleci: test"},
		languages:          map[string]int{"Go": 10000},
		hasDependabotYML:   true,
		dependabotPRSHA:    "dep123",
		dependabotPRChecks: []string{"Analyze (go)"},
	})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "required-status-checks-coherent").Check(repo)
	if result.Pass {
		t.Errorf("expected fail with multiple issues, got pass: %s", result.Detail)
	}
	// Stale check
	if !strings.Contains(result.Detail, "CodeQL") {
		t.Errorf("expected Detail to mention stale 'CodeQL' check, got: %s", result.Detail)
	}
	// Missing Analyze (X)
	if !strings.Contains(result.Detail, "Analyze") {
		t.Errorf("expected Detail to mention missing Analyze check, got: %s", result.Detail)
	}
	// Dependabot unsatisfiable
	if !strings.Contains(result.Detail, "Dependabot") {
		t.Errorf("expected Detail to mention Dependabot issue, got: %s", result.Detail)
	}
}
