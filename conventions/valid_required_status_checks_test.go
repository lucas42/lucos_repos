package conventions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// validChecksServerOpts configures the test server for valid-required-status-checks tests.
type validChecksServerOpts struct {
	protectionBody      []byte
	statusContexts      []string
	checkRunNames       []string // check runs on HEAD of main
	prSHA               string   // if non-empty, the PR list endpoint returns a PR with this head SHA
	prCheckRunNames     []string // check runs on the PR head commit
	prStatusContexts    []string // status contexts on the PR head commit
}

// validChecksServer creates a test server for valid-required-status-checks tests.
// For backward compatibility, it does not serve PR endpoints (no recent PR).
func validChecksServer(t *testing.T, protectionBody []byte, statusContexts []string, checkRunNames []string) *httptest.Server {
	t.Helper()
	return validChecksServerFull(t, validChecksServerOpts{
		protectionBody: protectionBody,
		statusContexts: statusContexts,
		checkRunNames:  checkRunNames,
	})
}

// validChecksServerFull creates a test server with full control over PR endpoints.
func validChecksServerFull(t *testing.T, opts validChecksServerOpts) *httptest.Server {
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
			if opts.protectionBody != nil {
				w.WriteHeader(http.StatusOK)
				w.Write(opts.protectionBody)
			} else {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"message":"Branch not protected"}`))
			}
		case "/repos/lucas42/test_repo/commits/heads/main/status":
			resp := combinedStatusResponse{}
			for _, ctx := range opts.statusContexts {
				resp.Statuses = append(resp.Statuses, statusEntry{Context: ctx})
			}
			json.NewEncoder(w).Encode(resp)
		case "/repos/lucas42/test_repo/commits/heads/main/check-runs":
			resp := checkRunsResp{}
			for _, name := range opts.checkRunNames {
				resp.CheckRuns = append(resp.CheckRuns, checkRun{Name: name})
			}
			json.NewEncoder(w).Encode(resp)
		case "/repos/lucas42/test_repo/pulls":
			if opts.prSHA != "" {
				type prHead struct {
					SHA string `json:"sha"`
				}
				type pr struct {
					Number int    `json:"number"`
					Head   prHead `json:"head"`
				}
				json.NewEncoder(w).Encode([]pr{{Number: 1, Head: prHead{SHA: opts.prSHA}}})
			} else {
				json.NewEncoder(w).Encode([]struct{}{})
			}
		default:
			// Handle PR head commit endpoints (dynamic SHA path).
			if opts.prSHA != "" {
				switch r.URL.Path {
				case "/repos/lucas42/test_repo/commits/" + opts.prSHA + "/check-runs":
					resp := checkRunsResp{}
					for _, name := range opts.prCheckRunNames {
						resp.CheckRuns = append(resp.CheckRuns, checkRun{Name: name})
					}
					json.NewEncoder(w).Encode(resp)
					return
				case "/repos/lucas42/test_repo/commits/" + opts.prSHA + "/status":
					resp := combinedStatusResponse{}
					for _, ctx := range opts.prStatusContexts {
						resp.Statuses = append(resp.Statuses, statusEntry{Context: ctx})
					}
					json.NewEncoder(w).Encode(resp)
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestValidRequiredStatusChecks_Registered(t *testing.T) {
	c := findConvention(t, "valid-required-status-checks")
	if c.Description == "" {
		t.Error("empty description")
	}
	if !c.AppliesToType(RepoTypeSystem) {
		t.Error("should apply to RepoTypeSystem")
	}
	if !c.AppliesToType(RepoTypeComponent) {
		t.Error("should apply to RepoTypeComponent")
	}
	if !c.ScheduledOnly {
		t.Error("should be ScheduledOnly (must not run in PR audit mode)")
	}
}

func TestValidRequiredStatusChecks_AllValid(t *testing.T) {
	server := validChecksServer(t,
		branchProtectionFixture([]string{"ci/circleci: test", "CodeQL"}),
		[]string{"ci/circleci: test"},
		[]string{"CodeQL"},
	)
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "valid-required-status-checks").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when all required checks are reported, got: %s", result.Detail)
	}
}

func TestValidRequiredStatusChecks_StaleCheck(t *testing.T) {
	// Required: "Analyze (javascript)" and "ci/circleci: test"
	// Reported: "CodeQL" (check run) and "ci/circleci: test" (status)
	// "Analyze (javascript)" is stale — it's the old CodeQL format.
	server := validChecksServer(t,
		branchProtectionFixture([]string{"Analyze (javascript)", "ci/circleci: test"}),
		[]string{"ci/circleci: test"},
		[]string{"CodeQL"},
	)
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "valid-required-status-checks").Check(repo)
	if result.Pass {
		t.Errorf("expected fail when stale check is required, got pass: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "Analyze (javascript)") {
		t.Errorf("expected Detail to mention stale check name, got: %s", result.Detail)
	}
}

func TestValidRequiredStatusChecks_NoRequiredChecks(t *testing.T) {
	server := validChecksServer(t, nil, nil, nil)
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "valid-required-status-checks").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when no required checks configured, got: %s", result.Detail)
	}
}

func TestValidRequiredStatusChecks_NoReportedChecks(t *testing.T) {
	// Required checks exist but no statuses/check runs reported on HEAD.
	server := validChecksServer(t,
		branchProtectionFixture([]string{"ci/circleci: test"}),
		nil,
		nil,
	)
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "valid-required-status-checks").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when no checks reported (can't validate), got: %s", result.Detail)
	}
}

func TestValidRequiredStatusChecks_PushOnlyCheck(t *testing.T) {
	// Required: "ci/circleci: test" and "Analyze (actions)"
	// Main reports both (circleci via status, Analyze via check run).
	// PR reports circleci (via status) but NOT "Analyze (actions)".
	// "Analyze (actions)" is push-only — present on main but absent from PR.
	server := validChecksServerFull(t, validChecksServerOpts{
		protectionBody:   branchProtectionFixture([]string{"ci/circleci: test", "Analyze (actions)"}),
		statusContexts:   []string{"ci/circleci: test"},
		checkRunNames:    []string{"Analyze (actions)"},
		prSHA:            "abc123",
		prCheckRunNames:  nil,
		prStatusContexts: []string{"ci/circleci: test"},
	})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "valid-required-status-checks").Check(repo)
	if result.Pass {
		t.Errorf("expected fail for push-only check, got pass: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "Analyze (actions)") {
		t.Errorf("expected Detail to mention push-only check name, got: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "push-only") {
		t.Errorf("expected Detail to mention 'push-only', got: %s", result.Detail)
	}
}

func TestValidRequiredStatusChecks_AllChecksOnPR(t *testing.T) {
	// All required checks appear on both main and the PR — should pass.
	// circleci reports via status API, CodeQL via check runs — both sources.
	server := validChecksServerFull(t, validChecksServerOpts{
		protectionBody:   branchProtectionFixture([]string{"ci/circleci: test", "CodeQL"}),
		statusContexts:   []string{"ci/circleci: test"},
		checkRunNames:    []string{"CodeQL"},
		prSHA:            "def456",
		prCheckRunNames:  []string{"CodeQL"},
		prStatusContexts: []string{"ci/circleci: test"},
	})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "valid-required-status-checks").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when all checks also appear on PR, got: %s", result.Detail)
	}
}

func TestValidRequiredStatusChecks_NoPRAvailable(t *testing.T) {
	// All required checks match on main, but no PR exists to sample.
	server := validChecksServerFull(t, validChecksServerOpts{
		protectionBody: branchProtectionFixture([]string{"ci/circleci: test"}),
		statusContexts: []string{"ci/circleci: test"},
		checkRunNames:  nil,
		// no prSHA — PR list returns empty
	})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "valid-required-status-checks").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when no PR available, got: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "no recent PR") {
		t.Errorf("expected Detail to mention no recent PR, got: %s", result.Detail)
	}
}

func TestValidRequiredStatusChecks_ProtectionAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "valid-required-status-checks").Check(repo)
	if result.Err == nil {
		t.Error("expected Err when protection API fails")
	}
}

func TestValidRequiredStatusChecks_StatusesAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/lucas42/test_repo/branches/main/protection":
			w.WriteHeader(http.StatusOK)
			w.Write(branchProtectionFixture([]string{"ci/circleci: test"}))
		case "/repos/lucas42/test_repo/commits/heads/main/status":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "valid-required-status-checks").Check(repo)
	if result.Err == nil {
		t.Error("expected Err when statuses API fails")
	}
}

func TestValidRequiredStatusChecks_CheckRunsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/lucas42/test_repo/branches/main/protection":
			w.WriteHeader(http.StatusOK)
			w.Write(branchProtectionFixture([]string{"ci/circleci: test"}))
		case "/repos/lucas42/test_repo/commits/heads/main/status":
			w.Write([]byte(`{"statuses":[{"context":"ci/circleci: test"}]}`))
		case "/repos/lucas42/test_repo/commits/heads/main/check-runs":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "valid-required-status-checks").Check(repo)
	if result.Err == nil {
		t.Error("expected Err when check-runs API fails")
	}
}
