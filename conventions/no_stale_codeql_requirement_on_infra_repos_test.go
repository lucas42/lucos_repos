package conventions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// noStaleCodeQLServer creates a test HTTP server that serves both the branch
// protection endpoint and the languages endpoint with the given data.
func noStaleCodeQLServer(t *testing.T, requiredChecks []string, languages map[string]int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/lucas42/lucos_test/branches/main/protection":
			w.WriteHeader(http.StatusOK)
			w.Write(branchProtectionFixture(requiredChecks))
		case "/repos/lucas42/lucos_test/languages":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(languages)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestNoStaleCodeQLRequirement_Registered(t *testing.T) {
	c := findConvention(t, "no-stale-codeql-requirement-on-infra-repos")
	if c.Description == "" {
		t.Error("has empty description")
	}
	if c.Rationale == "" {
		t.Error("has empty rationale")
	}
	if c.Guidance == "" {
		t.Error("has empty guidance")
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

// Infra-only repo with one required Analyze (X) check → fails with a clear message.
func TestNoStaleCodeQLRequirement_InfraRepoOneStaleCheck(t *testing.T) {
	server := noStaleCodeQLServer(t,
		[]string{"Analyze (javascript)"},
		map[string]int{"Shell": 200, "Dockerfile": 100},
	)
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "no-stale-codeql-requirement-on-infra-repos").Check(repo)
	if result.Pass {
		t.Errorf("expected fail for infra repo with stale Analyze check, got pass")
	}
	if !strings.Contains(result.Detail, "Analyze (javascript)") {
		t.Errorf("expected detail to name the offending check, got: %s", result.Detail)
	}
}

// Infra-only repo with two required Analyze (X) checks → fails and names both.
func TestNoStaleCodeQLRequirement_InfraRepoTwoStaleChecks(t *testing.T) {
	server := noStaleCodeQLServer(t,
		[]string{"Analyze (javascript)", "Analyze (go)"},
		map[string]int{"Shell": 200, "Dockerfile": 100},
	)
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "no-stale-codeql-requirement-on-infra-repos").Check(repo)
	if result.Pass {
		t.Errorf("expected fail for infra repo with two stale Analyze checks, got pass")
	}
	if !strings.Contains(result.Detail, "Analyze (javascript)") {
		t.Errorf("expected detail to name Analyze (javascript), got: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "Analyze (go)") {
		t.Errorf("expected detail to name Analyze (go), got: %s", result.Detail)
	}
}

// Infra-only repo with no required Analyze (X) check → passes.
func TestNoStaleCodeQLRequirement_InfraRepoNoAnalyzeCheck(t *testing.T) {
	server := noStaleCodeQLServer(t,
		[]string{"ci/circleci: build", "convention-check"},
		map[string]int{"Shell": 200, "Dockerfile": 100},
	)
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "no-stale-codeql-requirement-on-infra-repos").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass for infra repo without Analyze check, got fail: %s", result.Detail)
	}
}

// Application-code repo with a required Analyze (X) check → passes (other convention handles it).
func TestNoStaleCodeQLRequirement_AppRepoWithAnalyzeCheck(t *testing.T) {
	server := noStaleCodeQLServer(t,
		[]string{"Analyze (javascript)"},
		map[string]int{"JavaScript": 1000},
	)
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "no-stale-codeql-requirement-on-infra-repos").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass for app-code repo with Analyze check (handled by other convention), got fail: %s", result.Detail)
	}
}

// Application-code repo with no required Analyze (X) check → passes (other convention flags it).
func TestNoStaleCodeQLRequirement_AppRepoWithoutAnalyzeCheck(t *testing.T) {
	server := noStaleCodeQLServer(t,
		[]string{"ci/circleci: test"},
		map[string]int{"Go": 500},
	)
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "no-stale-codeql-requirement-on-infra-repos").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass for app-code repo without Analyze check (handled by other convention), got fail: %s", result.Detail)
	}
}

// Repo with zero required status checks → passes trivially.
func TestNoStaleCodeQLRequirement_NoRequiredChecks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/lucas42/lucos_test/branches/main/protection":
			// Branch not protected — GitHubRequiredStatusChecksFromBase returns empty slice.
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"Branch not protected"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "no-stale-codeql-requirement-on-infra-repos").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass for repo with no required checks, got fail: %s", result.Detail)
	}
}

// Error path: GitHub API failure when fetching required checks → returns Err, not Pass: false.
func TestNoStaleCodeQLRequirement_BranchProtectionAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "no-stale-codeql-requirement-on-infra-repos").Check(repo)
	if result.Err == nil {
		t.Error("expected Err when branch protection API returns 500, got nil")
	}
}

// Error path: GitHub API failure when fetching languages → returns Err, not Pass: false.
func TestNoStaleCodeQLRequirement_LanguagesAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/lucas42/lucos_test/branches/main/protection":
			w.WriteHeader(http.StatusOK)
			w.Write(branchProtectionFixture([]string{"Analyze (python)"}))
		default:
			// Languages endpoint (and anything else) returns 500.
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "no-stale-codeql-requirement-on-infra-repos").Check(repo)
	if result.Err == nil {
		t.Error("expected Err when languages API returns 500, got nil")
	}
}
