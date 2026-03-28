package conventions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// validChecksServer creates a test server for valid-required-status-checks tests.
func validChecksServer(t *testing.T, protectionBody []byte, statusContexts []string, checkRunNames []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/lucas42/test_repo/branches/main/protection":
			if protectionBody != nil {
				w.WriteHeader(http.StatusOK)
				w.Write(protectionBody)
			} else {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"message":"Branch not protected"}`))
			}
		case "/repos/lucas42/test_repo/commits/heads/main/status":
			resp := combinedStatusResponse{}
			for _, ctx := range statusContexts {
				resp.Statuses = append(resp.Statuses, statusEntry{Context: ctx})
			}
			json.NewEncoder(w).Encode(resp)
		case "/repos/lucas42/test_repo/commits/heads/main/check-runs":
			type checkRun struct {
				Name string `json:"name"`
			}
			type checkRunsResp struct {
				CheckRuns []checkRun `json:"check_runs"`
			}
			resp := checkRunsResp{}
			for _, name := range checkRunNames {
				resp.CheckRuns = append(resp.CheckRuns, checkRun{Name: name})
			}
			json.NewEncoder(w).Encode(resp)
		default:
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
