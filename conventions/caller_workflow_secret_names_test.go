package conventions

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const codeReviewerWithNewSecretNames = `name: Auto-merge on code reviewer approval

on:
  pull_request_review:
    types:
      - submitted
  pull_request:
    types:
      - closed

permissions:
  contents: read

jobs:
  reusable:
    uses: lucas42/.github/.github/workflows/reusable-code-reviewer-auto-merge.yml@main
    secrets:
      LUCOS_CI_APP_ID: ${{ secrets.LUCOS_CI_APP_ID }}
      LUCOS_CI_PRIVATE_KEY: ${{ secrets.LUCOS_CI_PRIVATE_KEY }}
`

const codeReviewerWithOldSecretNames = `name: Auto-merge on code reviewer approval

on:
  pull_request_review:
    types:
      - submitted

permissions:
  contents: read

jobs:
  reusable:
    uses: lucas42/.github/.github/workflows/reusable-code-reviewer-auto-merge.yml@main
    secrets:
      CODE_REVIEWER_APP_ID: ${{ secrets.CODE_REVIEWER_APP_ID }}
      CODE_REVIEWER_PRIVATE_KEY: ${{ secrets.CODE_REVIEWER_PRIVATE_KEY }}
`

const dependabotWithOldSecretNames = `name: Dependabot auto-merge

on:
  pull_request:
    types: [opened, synchronize, reopened]

permissions:
  pull-requests: write
  contents: write

jobs:
  dependabot:
    uses: lucas42/.github/.github/workflows/reusable-dependabot-auto-merge.yml@main
    secrets:
      CODE_REVIEWER_APP_ID: ${{ secrets.CODE_REVIEWER_APP_ID }}
      CODE_REVIEWER_PRIVATE_KEY: ${{ secrets.CODE_REVIEWER_PRIVATE_KEY }}
`

const dependabotWithNewSecretNames = `name: Dependabot auto-merge

on:
  pull_request:
    types: [opened, synchronize, reopened]

permissions:
  pull-requests: write
  contents: write

jobs:
  dependabot:
    uses: lucas42/.github/.github/workflows/reusable-dependabot-auto-merge.yml@main
    secrets:
      LUCOS_CI_APP_ID: ${{ secrets.LUCOS_CI_APP_ID }}
      LUCOS_CI_PRIVATE_KEY: ${{ secrets.LUCOS_CI_PRIVATE_KEY }}
`

// TestCallerWorkflowSecretNames_NoWorkflows verifies that a repo with no
// auto-merge workflows passes (convention does not apply).
func TestCallerWorkflowSecretNames_NoWorkflows(t *testing.T) {
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

	result := findConvention(t, "caller-workflow-secret-names").Check(repo)
	if !result.Pass {
		t.Errorf("expected Pass=true for repo with no workflows, got Detail=%q", result.Detail)
	}
}

// TestCallerWorkflowSecretNames_CodeReviewerNewNames verifies that a repo with
// a code-reviewer workflow using the new LUCOS_CI_* names passes.
func TestCallerWorkflowSecretNames_CodeReviewerNewNames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/repos/lucas42/test_repo/contents/.github/workflows/code-reviewer-auto-merge.yml" {
			w.Write([]byte(encodeWorkflowContent(codeReviewerWithNewSecretNames)))
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

	result := findConvention(t, "caller-workflow-secret-names").Check(repo)
	if !result.Pass {
		t.Errorf("expected Pass=true for code-reviewer workflow with new secret names, got Detail=%q", result.Detail)
	}
}

// TestCallerWorkflowSecretNames_CodeReviewerOldNames verifies that a repo with
// a code-reviewer workflow still using CODE_REVIEWER_* names fails.
func TestCallerWorkflowSecretNames_CodeReviewerOldNames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/repos/lucas42/test_repo/contents/.github/workflows/code-reviewer-auto-merge.yml" {
			w.Write([]byte(encodeWorkflowContent(codeReviewerWithOldSecretNames)))
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

	result := findConvention(t, "caller-workflow-secret-names").Check(repo)
	if result.Pass {
		t.Errorf("expected Pass=false for code-reviewer workflow with old secret names, got Detail=%q", result.Detail)
	}
	if result.Err != nil {
		t.Errorf("expected Err=nil, got %v", result.Err)
	}
}

// TestCallerWorkflowSecretNames_DependabotOldNames verifies that a repo with
// a dependabot workflow still using CODE_REVIEWER_* names fails.
func TestCallerWorkflowSecretNames_DependabotOldNames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/repos/lucas42/test_repo/contents/.github/workflows/dependabot-auto-merge.yml" {
			w.Write([]byte(encodeWorkflowContent(dependabotWithOldSecretNames)))
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

	result := findConvention(t, "caller-workflow-secret-names").Check(repo)
	if result.Pass {
		t.Errorf("expected Pass=false for dependabot workflow with old secret names, got Detail=%q", result.Detail)
	}
	if result.Err != nil {
		t.Errorf("expected Err=nil, got %v", result.Err)
	}
}

// TestCallerWorkflowSecretNames_DependabotNewNames verifies that a repo with
// a dependabot workflow using the new LUCOS_CI_* names passes.
func TestCallerWorkflowSecretNames_DependabotNewNames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/repos/lucas42/test_repo/contents/.github/workflows/dependabot-auto-merge.yml" {
			w.Write([]byte(encodeWorkflowContent(dependabotWithNewSecretNames)))
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

	result := findConvention(t, "caller-workflow-secret-names").Check(repo)
	if !result.Pass {
		t.Errorf("expected Pass=true for dependabot workflow with new secret names, got Detail=%q", result.Detail)
	}
}

// TestCallerWorkflowSecretNames_BothWorkflowsOldNames verifies that a repo
// with both workflows using old names lists both files in the failure detail.
func TestCallerWorkflowSecretNames_BothWorkflowsOldNames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/repos/lucas42/test_repo/contents/.github/workflows/code-reviewer-auto-merge.yml" {
			w.Write([]byte(encodeWorkflowContent(codeReviewerWithOldSecretNames)))
			return
		}
		if r.URL.Path == "/repos/lucas42/test_repo/contents/.github/workflows/dependabot-auto-merge.yml" {
			w.Write([]byte(encodeWorkflowContent(dependabotWithOldSecretNames)))
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

	result := findConvention(t, "caller-workflow-secret-names").Check(repo)
	if result.Pass {
		t.Errorf("expected Pass=false for both workflows with old secret names, got Detail=%q", result.Detail)
	}
	if result.Err != nil {
		t.Errorf("expected Err=nil, got %v", result.Err)
	}
}

// TestCallerWorkflowSecretNames_MixedNames verifies that a repo with one
// workflow using old names and one using new names fails (only the old-names
// file is listed).
func TestCallerWorkflowSecretNames_MixedNames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/repos/lucas42/test_repo/contents/.github/workflows/code-reviewer-auto-merge.yml" {
			w.Write([]byte(encodeWorkflowContent(codeReviewerWithOldSecretNames)))
			return
		}
		if r.URL.Path == "/repos/lucas42/test_repo/contents/.github/workflows/dependabot-auto-merge.yml" {
			w.Write([]byte(encodeWorkflowContent(dependabotWithNewSecretNames)))
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

	result := findConvention(t, "caller-workflow-secret-names").Check(repo)
	if result.Pass {
		t.Errorf("expected Pass=false when code-reviewer workflow has old names, got Detail=%q", result.Detail)
	}
	if result.Err != nil {
		t.Errorf("expected Err=nil, got %v", result.Err)
	}
}

// TestCallerWorkflowSecretNames_ScriptRepo verifies that the convention applies
// to script repos that have a dependabot workflow.
func TestCallerWorkflowSecretNames_ScriptRepo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/repos/lucas42/test_repo/contents/.github/workflows/dependabot-auto-merge.yml" {
			w.Write([]byte(encodeWorkflowContent(dependabotWithOldSecretNames)))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/test_repo",
		GitHubToken:   "fake-token",
		Type:          RepoTypeScript,
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "caller-workflow-secret-names").Check(repo)
	if result.Pass {
		t.Errorf("expected Pass=false for script repo with dependabot workflow using old names, got Detail=%q", result.Detail)
	}
}

// TestCallerWorkflowSecretNames_APIError verifies that an API error sets Err.
func TestCallerWorkflowSecretNames_APIError(t *testing.T) {
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

	result := findConvention(t, "caller-workflow-secret-names").Check(repo)
	if result.Err == nil {
		t.Errorf("expected Err!=nil for API error, got Pass=%v Detail=%q", result.Pass, result.Detail)
	}
}
