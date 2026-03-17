package conventions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// serveWorkflowDir serves a fake .github/workflows directory listing with one
// file, and serves that file's content when fetched.
func serveWorkflowDir(t *testing.T, fileName, fileContent string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/lucas42/test_repo/contents/.github/workflows":
			entries := []gitHubDirEntry{{Name: fileName, Type: "file"}}
			json.NewEncoder(w).Encode(entries)
		case "/repos/lucas42/test_repo/contents/.github/workflows/" + fileName:
			w.Write([]byte(encodeWorkflowContent(fileContent)))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// TestNoBotAutoMerge_NoWorkflows verifies that a supervised repo with no
// .github/workflows directory passes.
func TestNoBotAutoMerge_NoWorkflows(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	result := findConvention(t, "no-bot-auto-merge").Check(repo)
	if !result.Pass {
		t.Errorf("expected Pass=true (no workflows dir), got Detail=%q", result.Detail)
	}
}

// TestNoBotAutoMerge_NoReferencingWorkflow verifies that a supervised repo with
// workflow files that don't reference the code-reviewer reusable workflow passes.
func TestNoBotAutoMerge_NoReferencingWorkflow(t *testing.T) {
	server := serveWorkflowDir(t, "ci.yml", "name: CI\non: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n")
	defer server.Close()

	repo := RepoContext{
		Name:                  "lucas42/test_repo",
		GitHubToken:           "fake-token",
		Type:                  RepoTypeSystem,
		UnsupervisedAgentCode: false,
		GitHubBaseURL:         server.URL,
	}

	result := findConvention(t, "no-bot-auto-merge").Check(repo)
	if !result.Pass {
		t.Errorf("expected Pass=true (no reusable workflow reference), got Detail=%q", result.Detail)
	}
}

// TestNoBotAutoMerge_ReferencingWorkflowPresent verifies that a supervised repo
// with a workflow that references the code-reviewer reusable workflow fails.
func TestNoBotAutoMerge_ReferencingWorkflowPresent(t *testing.T) {
	server := serveWorkflowDir(t, "code-reviewer-auto-merge.yml", validCodeReviewerAutoMergeYAML)
	defer server.Close()

	repo := RepoContext{
		Name:                  "lucas42/test_repo",
		GitHubToken:           "fake-token",
		Type:                  RepoTypeSystem,
		UnsupervisedAgentCode: false,
		GitHubBaseURL:         server.URL,
	}

	result := findConvention(t, "no-bot-auto-merge").Check(repo)
	if result.Pass {
		t.Errorf("expected Pass=false (code-reviewer workflow present), got Detail=%q", result.Detail)
	}
	if result.Err != nil {
		t.Errorf("expected Err=nil, got %v", result.Err)
	}
}

// TestNoBotAutoMerge_UnsupervisedRepoSkipped verifies that an unsupervised repo
// passes without making API calls.
func TestNoBotAutoMerge_UnsupervisedRepoSkipped(t *testing.T) {
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
		UnsupervisedAgentCode: true,
		GitHubBaseURL:         server.URL,
	}

	result := findConvention(t, "no-bot-auto-merge").Check(repo)
	if !result.Pass {
		t.Errorf("expected Pass=true for unsupervised repo, got Detail=%q", result.Detail)
	}
	if apiCalled {
		t.Error("expected no API calls for unsupervised repo, but API was called")
	}
}

// TestNoBotAutoMerge_ListDirectoryAPIError verifies that an API error on the
// directory listing sets Err.
func TestNoBotAutoMerge_ListDirectoryAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:                  "lucas42/test_repo",
		GitHubToken:           "fake-token",
		Type:                  RepoTypeSystem,
		UnsupervisedAgentCode: false,
		GitHubBaseURL:         server.URL,
	}

	result := findConvention(t, "no-bot-auto-merge").Check(repo)
	if result.Err == nil {
		t.Errorf("expected Err!=nil for API error, got Pass=%v Detail=%q", result.Pass, result.Detail)
	}
}
