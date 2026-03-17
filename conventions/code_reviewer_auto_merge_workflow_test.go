package conventions

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const validCodeReviewerAutoMergeYAML = `name: Auto-merge on code reviewer approval

on:
  pull_request_review:
    types:
      - submitted
  pull_request:
    types:
      - closed

jobs:
  reusable:
    uses: lucas42/.github/.github/workflows/code-reviewer-auto-merge.yml@main
    secrets:
      CODE_REVIEWER_APP_ID: ${{ secrets.CODE_REVIEWER_APP_ID }}
      CODE_REVIEWER_PRIVATE_KEY: ${{ secrets.CODE_REVIEWER_PRIVATE_KEY }}
`

const invalidCodeReviewerAutoMergeYAML = `name: Auto-merge on code reviewer approval

on:
  pull_request_review:
    types:
      - submitted

jobs:
  auto-merge:
    runs-on: ubuntu-latest
    steps:
      - run: gh pr merge --auto --merge "$PR_URL"
`

// TestCodeReviewerAutoMergeWorkflow_ValidWorkflow verifies that an unsupervised
// repo with a valid code-reviewer workflow passes.
func TestCodeReviewerAutoMergeWorkflow_ValidWorkflow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/repos/lucas42/test_repo/contents/.github/workflows/code-reviewer-auto-merge.yml" {
			w.Write([]byte(encodeWorkflowContent(validCodeReviewerAutoMergeYAML)))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:                  "lucas42/test_repo",
		GitHubToken:           "fake-token",
		Type:                  RepoTypeSystem,
		UnsupervisedAgentCode: true,
		GitHubBaseURL:         server.URL,
	}

	result := findConvention(t, "code-reviewer-auto-merge-workflow").Check(repo)
	if !result.Pass {
		t.Errorf("expected Pass=true, got Detail=%q", result.Detail)
	}
}

// TestCodeReviewerAutoMergeWorkflow_InlineWorkflow verifies that an unsupervised
// repo with an inline (non-reusable) workflow fails.
func TestCodeReviewerAutoMergeWorkflow_InlineWorkflow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/repos/lucas42/test_repo/contents/.github/workflows/code-reviewer-auto-merge.yml" {
			w.Write([]byte(encodeWorkflowContent(invalidCodeReviewerAutoMergeYAML)))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:                  "lucas42/test_repo",
		GitHubToken:           "fake-token",
		Type:                  RepoTypeSystem,
		UnsupervisedAgentCode: true,
		GitHubBaseURL:         server.URL,
	}

	result := findConvention(t, "code-reviewer-auto-merge-workflow").Check(repo)
	if result.Pass {
		t.Errorf("expected Pass=false for inline workflow, got Detail=%q", result.Detail)
	}
	if result.Err != nil {
		t.Errorf("expected Err=nil, got %v", result.Err)
	}
}

// TestCodeReviewerAutoMergeWorkflow_Missing verifies that an unsupervised repo
// with no code-reviewer workflow fails.
func TestCodeReviewerAutoMergeWorkflow_Missing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:                  "lucas42/test_repo",
		GitHubToken:           "fake-token",
		Type:                  RepoTypeSystem,
		UnsupervisedAgentCode: true,
		GitHubBaseURL:         server.URL,
	}

	result := findConvention(t, "code-reviewer-auto-merge-workflow").Check(repo)
	if result.Pass {
		t.Errorf("expected Pass=false for missing workflow, got Detail=%q", result.Detail)
	}
}

// TestCodeReviewerAutoMergeWorkflow_SupervisedRepoSkipped verifies that a repo
// without unsupervisedAgentCode passes without making any API calls.
func TestCodeReviewerAutoMergeWorkflow_SupervisedRepoSkipped(t *testing.T) {
	apiCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalled = true
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:                  "lucas42/test_repo",
		GitHubToken:           "fake-token",
		Type:                  RepoTypeSystem,
		UnsupervisedAgentCode: false,
		GitHubBaseURL:         server.URL,
	}

	result := findConvention(t, "code-reviewer-auto-merge-workflow").Check(repo)
	if !result.Pass {
		t.Errorf("expected Pass=true for supervised repo, got Detail=%q", result.Detail)
	}
	if apiCalled {
		t.Error("expected no API calls for supervised repo, but API was called")
	}
}

// TestCodeReviewerAutoMergeWorkflow_APIError verifies that an API error sets Err.
func TestCodeReviewerAutoMergeWorkflow_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:                  "lucas42/test_repo",
		GitHubToken:           "fake-token",
		Type:                  RepoTypeSystem,
		UnsupervisedAgentCode: true,
		GitHubBaseURL:         server.URL,
	}

	result := findConvention(t, "code-reviewer-auto-merge-workflow").Check(repo)
	if result.Err == nil {
		t.Errorf("expected Err!=nil for API error, got Pass=%v Detail=%q", result.Pass, result.Detail)
	}
}
