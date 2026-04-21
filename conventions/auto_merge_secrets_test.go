package conventions

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const validCodeReviewerWithSecrets = `name: Auto-merge on code reviewer approval

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
      LUCOS_CI_APP_ID: ${{ secrets.LUCOS_CI_APP_ID }}
      LUCOS_CI_PRIVATE_KEY: ${{ secrets.LUCOS_CI_PRIVATE_KEY }}
`

const codeReviewerMissingSecrets = `name: Auto-merge on code reviewer approval

on:
  pull_request_review:
    types:
      - submitted

jobs:
  reusable:
    uses: lucas42/.github/.github/workflows/code-reviewer-auto-merge.yml@main
`

const codeReviewerMissingPrivateKey = `name: Auto-merge on code reviewer approval

on:
  pull_request_review:
    types:
      - submitted

jobs:
  reusable:
    uses: lucas42/.github/.github/workflows/code-reviewer-auto-merge.yml@main
    secrets:
      LUCOS_CI_APP_ID: ${{ secrets.LUCOS_CI_APP_ID }}
`

// TestAutoMergeSecrets_BothSecretsPresent verifies that a workflow that passes
// both secrets to the reusable workflow, with secrets configured on the repo, passes.
func TestAutoMergeSecrets_BothSecretsPresent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/repos/lucas42/test_repo/contents/.github/workflows/code-reviewer-auto-merge.yml" {
			w.Write([]byte(encodeWorkflowContent(validCodeReviewerWithSecrets)))
			return
		}
		if r.URL.Path == "/repos/lucas42/test_repo/actions/secrets" {
			w.Write([]byte(repoSecretsJSON))
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

	result := findConvention(t, "auto-merge-secrets").Check(repo)
	if !result.Pass {
		t.Errorf("expected Pass=true, got Detail=%q", result.Detail)
	}
}

// TestAutoMergeSecrets_SecretsNotConfiguredOnRepo verifies that a workflow that
// references the secrets but the secrets aren't configured on the repo fails.
func TestAutoMergeSecrets_SecretsNotConfiguredOnRepo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/repos/lucas42/test_repo/contents/.github/workflows/code-reviewer-auto-merge.yml" {
			w.Write([]byte(encodeWorkflowContent(validCodeReviewerWithSecrets)))
			return
		}
		if r.URL.Path == "/repos/lucas42/test_repo/actions/secrets" {
			w.Write([]byte(repoSecretsEmptyJSON))
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

	result := findConvention(t, "auto-merge-secrets").Check(repo)
	if result.Pass {
		t.Errorf("expected Pass=false when secrets not configured on repo, got Detail=%q", result.Detail)
	}
	if result.Err != nil {
		t.Errorf("expected Err=nil, got %v", result.Err)
	}
}

// TestAutoMergeSecrets_SecretsAPIError verifies that a secrets API error sets Err.
func TestAutoMergeSecrets_SecretsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/repos/lucas42/test_repo/contents/.github/workflows/code-reviewer-auto-merge.yml" {
			w.Write([]byte(encodeWorkflowContent(validCodeReviewerWithSecrets)))
			return
		}
		// secrets API returns 500
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/test_repo",
		GitHubToken:   "fake-token",
		Type:          RepoTypeSystem,
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "auto-merge-secrets").Check(repo)
	if result.Err == nil {
		t.Errorf("expected Err!=nil for secrets API error, got Pass=%v Detail=%q", result.Pass, result.Detail)
	}
}

// TestAutoMergeSecrets_MissingBothSecrets verifies that a workflow with no
// secrets block fails.
func TestAutoMergeSecrets_MissingBothSecrets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/repos/lucas42/test_repo/contents/.github/workflows/code-reviewer-auto-merge.yml" {
			w.Write([]byte(encodeWorkflowContent(codeReviewerMissingSecrets)))
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

	result := findConvention(t, "auto-merge-secrets").Check(repo)
	if result.Pass {
		t.Errorf("expected Pass=false when both secrets missing from workflow, got Detail=%q", result.Detail)
	}
	if result.Err != nil {
		t.Errorf("expected Err=nil, got %v", result.Err)
	}
}

// TestAutoMergeSecrets_MissingPrivateKey verifies that a workflow missing only
// CODE_REVIEWER_PRIVATE_KEY fails.
func TestAutoMergeSecrets_MissingPrivateKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/repos/lucas42/test_repo/contents/.github/workflows/code-reviewer-auto-merge.yml" {
			w.Write([]byte(encodeWorkflowContent(codeReviewerMissingPrivateKey)))
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

	result := findConvention(t, "auto-merge-secrets").Check(repo)
	if result.Pass {
		t.Errorf("expected Pass=false when CODE_REVIEWER_PRIVATE_KEY missing from workflow, got Detail=%q", result.Detail)
	}
	if result.Err != nil {
		t.Errorf("expected Err=nil, got %v", result.Err)
	}
}

// TestAutoMergeSecrets_NoWorkflow verifies that a repo with no code-reviewer
// auto-merge workflow passes (convention does not apply).
func TestAutoMergeSecrets_NoWorkflow(t *testing.T) {
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

	result := findConvention(t, "auto-merge-secrets").Check(repo)
	if !result.Pass {
		t.Errorf("expected Pass=true when no workflow exists, got Detail=%q", result.Detail)
	}
}

// TestAutoMergeSecrets_DependabotOnlyRepo verifies that a repo with only a
// dependabot-auto-merge workflow passes — that workflow uses GITHUB_TOKEN only.
func TestAutoMergeSecrets_DependabotOnlyRepo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// dependabot-auto-merge.yml exists but code-reviewer-auto-merge.yml does not
		if r.URL.Path == "/repos/lucas42/test_repo/contents/.github/workflows/dependabot-auto-merge.yml" {
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

	result := findConvention(t, "auto-merge-secrets").Check(repo)
	if !result.Pass {
		t.Errorf("expected Pass=true for dependabot-only repo, got Detail=%q", result.Detail)
	}
}

// TestAutoMergeSecrets_APIError verifies that an API error sets Err.
func TestAutoMergeSecrets_APIError(t *testing.T) {
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

	result := findConvention(t, "auto-merge-secrets").Check(repo)
	if result.Err == nil {
		t.Errorf("expected Err!=nil for API error, got Pass=%v Detail=%q", result.Pass, result.Detail)
	}
}
