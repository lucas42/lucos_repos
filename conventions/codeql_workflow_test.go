package conventions

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- has-codeql-workflow tests ---

func TestHasCodeQLWorkflow_Registered(t *testing.T) {
	c := findConvention(t, "has-codeql-workflow")
	if c.Description == "" {
		t.Error("has-codeql-workflow has empty description")
	}
	if c.Rationale == "" {
		t.Error("has-codeql-workflow has empty rationale")
	}
	if c.Guidance == "" {
		t.Error("has-codeql-workflow has empty guidance")
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
}

func TestHasCodeQLWorkflow_FileExists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/lucos_test/contents/.github/workflows/codeql-analysis.yml" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"type":"file"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "has-codeql-workflow").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when file exists, got fail: %s", result.Detail)
	}
}

func TestHasCodeQLWorkflow_FileMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "has-codeql-workflow").Check(repo)
	if result.Pass {
		t.Errorf("expected fail when file missing, got pass")
	}
}

func TestHasCodeQLWorkflow_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "has-codeql-workflow").Check(repo)
	if result.Err == nil {
		t.Error("expected Err when API returns 500")
	}
}

// --- codeql-workflow-security-settings tests ---

func TestCodeQLSecuritySettings_Registered(t *testing.T) {
	c := findConvention(t, "codeql-workflow-security-settings")
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
}

func TestCodeQLSecuritySettings_FileNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "codeql-workflow-security-settings").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when file not found (defers to has-codeql-workflow), got fail: %s", result.Detail)
	}
}

func TestCodeQLSecuritySettings_AllSettingsPresent(t *testing.T) {
	workflow := `
name: CodeQL Analysis
on:
  push:
    branches: [main]
  pull_request:
    branches: [main]
  schedule:
    - cron: '0 6 * * 1'

permissions: {}

jobs:
  analyze:
    runs-on: ubuntu-latest
    permissions:
      security-events: write
    steps:
      - uses: actions/checkout@v4
`
	server := codeqlServer(t, workflow)
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "codeql-workflow-security-settings").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass with all settings present, got fail: %s", result.Detail)
	}
}

func TestCodeQLSecuritySettings_MissingPullRequest(t *testing.T) {
	workflow := `
name: CodeQL Analysis
on:
  push:
    branches: [main]
  schedule:
    - cron: '0 6 * * 1'

permissions: {}

jobs:
  analyze:
    runs-on: ubuntu-latest
    permissions:
      security-events: write
    steps:
      - uses: actions/checkout@v4
`
	server := codeqlServer(t, workflow)
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "codeql-workflow-security-settings").Check(repo)
	if result.Pass {
		t.Error("expected fail when pull_request trigger missing")
	}
	if !strings.Contains(result.Detail, "pull_request") {
		t.Errorf("expected detail to mention pull_request, got: %s", result.Detail)
	}
}

func TestCodeQLSecuritySettings_MissingSchedule(t *testing.T) {
	workflow := `
name: CodeQL Analysis
on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

permissions: {}

jobs:
  analyze:
    runs-on: ubuntu-latest
    permissions:
      security-events: write
    steps:
      - uses: actions/checkout@v4
`
	server := codeqlServer(t, workflow)
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "codeql-workflow-security-settings").Check(repo)
	if result.Pass {
		t.Error("expected fail when schedule trigger missing")
	}
	if !strings.Contains(result.Detail, "schedule") {
		t.Errorf("expected detail to mention schedule, got: %s", result.Detail)
	}
}

func TestCodeQLSecuritySettings_MissingTopLevelPermissions(t *testing.T) {
	workflow := `
name: CodeQL Analysis
on:
  push:
    branches: [main]
  pull_request:
    branches: [main]
  schedule:
    - cron: '0 6 * * 1'

jobs:
  analyze:
    runs-on: ubuntu-latest
    permissions:
      security-events: write
    steps:
      - uses: actions/checkout@v4
`
	server := codeqlServer(t, workflow)
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "codeql-workflow-security-settings").Check(repo)
	if result.Pass {
		t.Error("expected fail when top-level permissions missing")
	}
	if !strings.Contains(result.Detail, "permissions") {
		t.Errorf("expected detail to mention permissions, got: %s", result.Detail)
	}
}

func TestCodeQLSecuritySettings_MissingSecurityEventsWrite(t *testing.T) {
	workflow := `
name: CodeQL Analysis
on:
  push:
    branches: [main]
  pull_request:
    branches: [main]
  schedule:
    - cron: '0 6 * * 1'

permissions: {}

jobs:
  analyze:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
`
	server := codeqlServer(t, workflow)
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "codeql-workflow-security-settings").Check(repo)
	if result.Pass {
		t.Error("expected fail when security-events: write missing")
	}
	if !strings.Contains(result.Detail, "security-events") {
		t.Errorf("expected detail to mention security-events, got: %s", result.Detail)
	}
}

func TestCodeQLSecuritySettings_MultipleIssues(t *testing.T) {
	workflow := `
name: CodeQL Analysis
on:
  push:
    branches: [main]

jobs:
  analyze:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
`
	server := codeqlServer(t, workflow)
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "codeql-workflow-security-settings").Check(repo)
	if result.Pass {
		t.Error("expected fail with multiple missing settings")
	}
	// Should mention all missing items
	for _, keyword := range []string{"pull_request", "schedule", "permissions", "security-events"} {
		if !strings.Contains(result.Detail, keyword) {
			t.Errorf("expected detail to mention %q, got: %s", keyword, result.Detail)
		}
	}
}

func TestCodeQLSecuritySettings_OnAsList(t *testing.T) {
	// "on" as a list of event names is uncommon but valid
	workflow := `
name: CodeQL Analysis
"on": [push, pull_request, schedule]

permissions: {}

jobs:
  analyze:
    runs-on: ubuntu-latest
    permissions:
      security-events: write
    steps:
      - uses: actions/checkout@v4
`
	server := codeqlServer(t, workflow)
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "codeql-workflow-security-settings").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass with 'on' as list, got fail: %s", result.Detail)
	}
}

func TestCodeQLSecuritySettings_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "codeql-workflow-security-settings").Check(repo)
	if result.Err == nil {
		t.Error("expected Err when API returns 500")
	}
}

// codeqlServer creates a test server that serves a codeql-analysis.yml file.
func codeqlServer(t *testing.T, workflowContent string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/lucos_test/contents/.github/workflows/codeql-analysis.yml" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(composeFixture(workflowContent))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}
