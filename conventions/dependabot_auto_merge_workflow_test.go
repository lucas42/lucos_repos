package conventions

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
)

func encodeWorkflowContent(content string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(content))
	return `{"content":"` + encoded + `","encoding":"base64"}`
}

const validDependabotAutoMergeYAML = `name: Dependabot auto-merge

on: pull_request

jobs:
  dependabot:
    uses: lucas42/.github/.github/workflows/dependabot-auto-merge.yml@main
`

const invalidDependabotAutoMergeYAML = `name: Dependabot auto-merge

on: pull_request

jobs:
  dependabot:
    runs-on: ubuntu-latest
    steps:
      - run: gh pr merge --auto --merge "$PR_URL"
`

// TestDependabotAutoMergeWorkflow_ValidWorkflow verifies that a workflow
// referencing the shared reusable workflow passes.
func TestDependabotAutoMergeWorkflow_ValidWorkflow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/repos/lucas42/test_repo/contents/.github/workflows/auto-merge.yml" {
			w.Write([]byte(encodeWorkflowContent(validDependabotAutoMergeYAML)))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/test_repo",
		GitHubToken:   "fake-token",
		Type:          RepoTypeSystem,
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "dependabot-auto-merge-workflow").Check(repo)
	if !result.Pass {
		t.Errorf("expected Pass=true, got Detail=%q", result.Detail)
	}
}

// TestDependabotAutoMergeWorkflow_InlineWorkflow verifies that a workflow
// with inline logic (not using the reusable workflow) fails.
func TestDependabotAutoMergeWorkflow_InlineWorkflow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/repos/lucas42/test_repo/contents/.github/workflows/auto-merge.yml" {
			w.Write([]byte(encodeWorkflowContent(invalidDependabotAutoMergeYAML)))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/test_repo",
		GitHubToken:   "fake-token",
		Type:          RepoTypeSystem,
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "dependabot-auto-merge-workflow").Check(repo)
	if result.Pass {
		t.Errorf("expected Pass=false for inline workflow, got Detail=%q", result.Detail)
	}
	if result.Err != nil {
		t.Errorf("expected Err=nil, got %v", result.Err)
	}
}

// TestDependabotAutoMergeWorkflow_Missing verifies that a missing workflow file fails.
func TestDependabotAutoMergeWorkflow_Missing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/test_repo",
		GitHubToken:   "fake-token",
		Type:          RepoTypeSystem,
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "dependabot-auto-merge-workflow").Check(repo)
	if result.Pass {
		t.Errorf("expected Pass=false for missing workflow, got Detail=%q", result.Detail)
	}
}

// TestDependabotAutoMergeWorkflow_APIError verifies that an API error sets Err.
func TestDependabotAutoMergeWorkflow_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/test_repo",
		GitHubToken:   "fake-token",
		Type:          RepoTypeSystem,
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "dependabot-auto-merge-workflow").Check(repo)
	if result.Err == nil {
		t.Errorf("expected Err!=nil for API error, got Pass=%v Detail=%q", result.Pass, result.Detail)
	}
}
