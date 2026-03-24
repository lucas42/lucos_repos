package conventions

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDependabotConfigured_Registered(t *testing.T) {
	c := findConvention(t, "dependabot-configured")
	if c.Description == "" {
		t.Error("has empty description")
	}
	if c.Rationale == "" {
		t.Error("has empty rationale")
	}
	if c.Guidance == "" {
		t.Error("has empty guidance")
	}
	if c.Check == nil {
		t.Error("has nil Check function")
	}
	// Should apply to all repo types (no AppliesTo filter)
	if !c.AppliesToType(RepoTypeSystem) {
		t.Error("should apply to RepoTypeSystem")
	}
	if !c.AppliesToType(RepoTypeComponent) {
		t.Error("should apply to RepoTypeComponent")
	}
}

func TestDependabotConfigured_FileNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "dependabot-configured").Check(repo)
	if result.Pass {
		t.Error("expected fail when dependabot.yml not found")
	}
	if !strings.Contains(result.Detail, "not found") {
		t.Errorf("expected detail to mention 'not found', got: %s", result.Detail)
	}
}

func TestDependabotConfigured_FullyValid(t *testing.T) {
	config := `
version: 2
updates:
  - package-ecosystem: github-actions
    directory: /
    schedule:
      interval: weekly
    allow:
      - dependency-type: all
  - package-ecosystem: npm
    directory: /
    schedule:
      interval: weekly
    allow:
      - dependency-type: all
`
	server := dependabotServer(t, config)
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "dependabot-configured").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass with fully valid config, got fail: %s", result.Detail)
	}
}

func TestDependabotConfigured_MissingGitHubActions(t *testing.T) {
	config := `
version: 2
updates:
  - package-ecosystem: npm
    directory: /
    schedule:
      interval: weekly
    allow:
      - dependency-type: all
`
	server := dependabotServer(t, config)
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "dependabot-configured").Check(repo)
	if result.Pass {
		t.Error("expected fail when github-actions entry missing")
	}
	if !strings.Contains(result.Detail, "github-actions") {
		t.Errorf("expected detail to mention github-actions, got: %s", result.Detail)
	}
}

func TestDependabotConfigured_GitHubActionsWrongDirectory(t *testing.T) {
	config := `
version: 2
updates:
  - package-ecosystem: github-actions
    directory: /src
    schedule:
      interval: weekly
    allow:
      - dependency-type: all
`
	server := dependabotServer(t, config)
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "dependabot-configured").Check(repo)
	if result.Pass {
		t.Error("expected fail when github-actions directory is not /")
	}
}

func TestDependabotConfigured_MissingAllowAll(t *testing.T) {
	config := `
version: 2
updates:
  - package-ecosystem: github-actions
    directory: /
    schedule:
      interval: weekly
  - package-ecosystem: npm
    directory: /
    schedule:
      interval: weekly
`
	server := dependabotServer(t, config)
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "dependabot-configured").Check(repo)
	if result.Pass {
		t.Error("expected fail when allow blocks missing")
	}
	if !strings.Contains(result.Detail, "dependency-type: all") {
		t.Errorf("expected detail to mention dependency-type: all, got: %s", result.Detail)
	}
}

func TestDependabotConfigured_PartialAllowAll(t *testing.T) {
	config := `
version: 2
updates:
  - package-ecosystem: github-actions
    directory: /
    schedule:
      interval: weekly
    allow:
      - dependency-type: all
  - package-ecosystem: npm
    directory: /
    schedule:
      interval: weekly
`
	server := dependabotServer(t, config)
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "dependabot-configured").Check(repo)
	if result.Pass {
		t.Error("expected fail when only some entries have allow-all")
	}
	if !strings.Contains(result.Detail, "npm") {
		t.Errorf("expected detail to mention npm, got: %s", result.Detail)
	}
}

func TestDependabotConfigured_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "dependabot-configured").Check(repo)
	if result.Err == nil {
		t.Error("expected Err when API returns 500")
	}
}

// dependabotServer creates a test server that serves a dependabot.yml file.
func dependabotServer(t *testing.T, content string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/lucos_test/contents/.github/dependabot.yml" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(composeFixture(content))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}
