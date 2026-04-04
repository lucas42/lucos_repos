package conventions

import (
	"encoding/json"
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
		switch r.URL.Path {
		case "/repos/lucas42/lucos_test/languages":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]int{"JavaScript": 1000})
		case "/repos/lucas42/lucos_test/contents/.github/workflows/codeql-analysis.yml":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"type":"file"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
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
		switch r.URL.Path {
		case "/repos/lucas42/lucos_test/languages":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]int{"Python": 500})
		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"Not Found"}`))
		}
	}))
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "has-codeql-workflow").Check(repo)
	if result.Pass {
		t.Errorf("expected fail when file missing, got pass")
	}
}

func TestHasCodeQLWorkflow_NoCodeQLLanguages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/lucas42/lucos_test/languages":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]int{"Shell": 200, "Dockerfile": 100})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "has-codeql-workflow").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when no CodeQL languages, got fail: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "no CodeQL-supported languages") {
		t.Errorf("expected detail to mention no CodeQL languages, got: %s", result.Detail)
	}
}

func TestHasCodeQLWorkflow_EmptyLanguages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/lucas42/lucos_test/languages":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]int{})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "has-codeql-workflow").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when repo has no languages, got fail: %s", result.Detail)
	}
}

func TestHasCodeQLWorkflow_LanguagesAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "has-codeql-workflow").Check(repo)
	if result.Err == nil {
		t.Error("expected Err when languages API returns 500")
	}
}

func TestHasCodeQLWorkflow_APIError(t *testing.T) {
	// Languages API succeeds but file check fails
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/lucas42/lucos_test/languages":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]int{"Go": 300})
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
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

func TestCodeQLSecuritySettings_NoCodeQLLanguages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/lucas42/lucos_test/languages":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]int{"Erlang": 400})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "codeql-workflow-security-settings").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when no CodeQL languages, got fail: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "no CodeQL-supported languages") {
		t.Errorf("expected detail to mention no CodeQL languages, got: %s", result.Detail)
	}
}

func TestCodeQLSecuritySettings_FileNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/lucas42/lucos_test/languages":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]int{"JavaScript": 1000})
		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"Not Found"}`))
		}
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
	server := codeqlServerWithLanguages(t, workflow, map[string]int{"Python": 500})
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
	server := codeqlServerWithLanguages(t, workflow, map[string]int{"JavaScript": 1000})
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
	server := codeqlServerWithLanguages(t, workflow, map[string]int{"Go": 300})
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
	server := codeqlServerWithLanguages(t, workflow, map[string]int{"TypeScript": 800})
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
	server := codeqlServerWithLanguages(t, workflow, map[string]int{"Ruby": 200})
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
	server := codeqlServerWithLanguages(t, workflow, map[string]int{"Java": 600})
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
	server := codeqlServerWithLanguages(t, workflow, map[string]int{"Python": 500})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "codeql-workflow-security-settings").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass with 'on' as list, got fail: %s", result.Detail)
	}
}

func TestCodeQLSecuritySettings_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/lucas42/lucos_test/languages":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]int{"Go": 300})
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "codeql-workflow-security-settings").Check(repo)
	if result.Err == nil {
		t.Error("expected Err when API returns 500")
	}
}

// codeqlServer creates a test server that serves a codeql-analysis.yml file.
// Deprecated: use codeqlServerWithLanguages for new tests.
func codeqlServer(t *testing.T, workflowContent string) *httptest.Server {
	t.Helper()
	return codeqlServerWithLanguages(t, workflowContent, map[string]int{"JavaScript": 1000})
}

// codeqlServerWithLanguages creates a test server that serves both the languages
// endpoint and a codeql-analysis.yml file.
func codeqlServerWithLanguages(t *testing.T, workflowContent string, languages map[string]int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/lucas42/lucos_test/languages":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(languages)
		case "/repos/lucas42/lucos_test/contents/.github/workflows/codeql-analysis.yml":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(composeFixture(workflowContent))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// codeqlServerFull creates a test server serving the languages endpoint,
// the codeql-analysis.yml file, and the branch protection endpoint.
// Used for Check 5 (language matrix consistency) tests.
func codeqlServerFull(t *testing.T, workflowContent string, languages map[string]int, protectionBody []byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/lucas42/lucos_test/languages":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(languages)
		case "/repos/lucas42/lucos_test/contents/.github/workflows/codeql-analysis.yml":
			w.WriteHeader(http.StatusOK)
			w.Write(composeFixture(workflowContent))
		case "/repos/lucas42/lucos_test/branches/main/protection":
			if protectionBody != nil {
				w.WriteHeader(http.StatusOK)
				w.Write(protectionBody)
			} else {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"message":"Branch not protected"}`))
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// --- codeql-workflow-security-settings Check 5 (language matrix) tests ---

// workflowWithMatrix returns a minimal valid CodeQL workflow YAML with the
// given languages in the analyze job's strategy.matrix.language list.
func workflowWithMatrix(languages []string) string {
	langList := ""
	for _, l := range languages {
		langList += "\n        - " + l
	}
	return `name: CodeQL Analysis
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
    strategy:
      matrix:
        language:` + langList + `
    permissions:
      security-events: write
    steps:
      - uses: actions/checkout@v4
`
}

// workflowWithoutMatrix returns a minimal valid CodeQL workflow YAML with
// no explicit language matrix (relies on auto-detection).
func workflowWithoutMatrix() string {
	return `name: CodeQL Analysis
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
}

func TestCodeQLSecuritySettings_ExplicitMatrixCoversRequiredAnalyzeCheck(t *testing.T) {
	// Analyze (javascript) is required; workflow has explicit matrix with javascript — pass.
	server := codeqlServerFull(t,
		workflowWithMatrix([]string{"javascript"}),
		map[string]int{"JavaScript": 1000},
		branchProtectionFixture([]string{"Analyze (javascript)"}),
	)
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "codeql-workflow-security-settings").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when explicit matrix covers required Analyze check, got fail: %s", result.Detail)
	}
}

func TestCodeQLSecuritySettings_MissingMatrixForRequiredAnalyzeCheck(t *testing.T) {
	// Analyze (javascript) is required; workflow has no explicit matrix — fail.
	server := codeqlServerFull(t,
		workflowWithoutMatrix(),
		map[string]int{"JavaScript": 1000},
		branchProtectionFixture([]string{"Analyze (javascript)"}),
	)
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "codeql-workflow-security-settings").Check(repo)
	if result.Pass {
		t.Errorf("expected fail when required Analyze check not covered by explicit matrix, got pass")
	}
	if !strings.Contains(result.Detail, "javascript") {
		t.Errorf("expected detail to mention the missing language, got: %s", result.Detail)
	}
}

func TestCodeQLSecuritySettings_MatrixMissingOneOfTwoRequiredAnalyzeChecks(t *testing.T) {
	// Analyze (javascript) and Analyze (python) required; matrix only has javascript — fail for python.
	server := codeqlServerFull(t,
		workflowWithMatrix([]string{"javascript"}),
		map[string]int{"JavaScript": 600, "Python": 400},
		branchProtectionFixture([]string{"Analyze (javascript)", "Analyze (python)"}),
	)
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "codeql-workflow-security-settings").Check(repo)
	if result.Pass {
		t.Errorf("expected fail when matrix misses a required Analyze language, got pass")
	}
	if !strings.Contains(result.Detail, "python") {
		t.Errorf("expected detail to mention missing language, got: %s", result.Detail)
	}
}

func TestCodeQLSecuritySettings_NoAnalyzeChecksRequired(t *testing.T) {
	// No Analyze (X) checks in branch protection — language matrix check skipped, pass.
	server := codeqlServerFull(t,
		workflowWithoutMatrix(),
		map[string]int{"JavaScript": 1000},
		branchProtectionFixture([]string{"ci/circleci: test"}),
	)
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "codeql-workflow-security-settings").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when no Analyze checks required, got fail: %s", result.Detail)
	}
}

func TestCodeQLSecuritySettings_BranchProtectionUnset_LanguageMatrixCheckSkipped(t *testing.T) {
	// Branch protection endpoint returns 404 (not protected) — language matrix check skipped, pass.
	server := codeqlServerFull(t,
		workflowWithoutMatrix(),
		map[string]int{"JavaScript": 1000},
		nil, // no protection
	)
	defer server.Close()

	repo := RepoContext{Name: "lucas42/lucos_test", GitHubToken: "fake", GitHubBaseURL: server.URL}
	result := findConvention(t, "codeql-workflow-security-settings").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when branch protection not set (language matrix check skipped), got fail: %s", result.Detail)
	}
}
