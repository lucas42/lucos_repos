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

on:
  pull_request:
    types: [opened, synchronize, reopened]

permissions:
  pull-requests: write
  contents: write

jobs:
  dependabot:
    uses: lucas42/.github/.github/workflows/dependabot-auto-merge.yml@main
`

const oldPullRequestTargetYAML = `name: Dependabot auto-merge
on:
  pull_request_target:
    types: [opened, synchronize, reopened]

jobs:
  dependabot:
    uses: lucas42/.github/.github/workflows/dependabot-auto-merge.yml@main
    secrets: inherit
`

const missingPermissionsYAML = `name: Dependabot auto-merge

on:
  pull_request:
    types: [opened, synchronize, reopened]

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

// TestDependabotAutoMergeWorkflow_ValidWorkflow_NewFilename verifies that a workflow
// at the canonical new filename passes.
func TestDependabotAutoMergeWorkflow_ValidWorkflow_NewFilename(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
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

	result := findConvention(t, "dependabot-auto-merge-workflow").Check(repo)
	if !result.Pass {
		t.Errorf("expected Pass=true, got Detail=%q", result.Detail)
	}
}

// TestDependabotAutoMergeWorkflow_ValidWorkflow_LegacyFilename verifies that a workflow
// at the legacy auto-merge.yml filename still passes (fallback support).
func TestDependabotAutoMergeWorkflow_ValidWorkflow_LegacyFilename(t *testing.T) {
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
		t.Errorf("expected Pass=true for legacy filename, got Detail=%q", result.Detail)
	}
}

// TestDependabotAutoMergeWorkflow_PullRequestTarget verifies that a workflow using
// pull_request_target fails — this trigger causes startup_failure with reusable workflows.
func TestDependabotAutoMergeWorkflow_PullRequestTarget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/repos/lucas42/test_repo/contents/.github/workflows/dependabot-auto-merge.yml" {
			w.Write([]byte(encodeWorkflowContent(oldPullRequestTargetYAML)))
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
		t.Errorf("expected Pass=false for pull_request_target workflow, got Detail=%q", result.Detail)
	}
	if result.Err != nil {
		t.Errorf("expected Err=nil, got %v", result.Err)
	}
}

// TestDependabotAutoMergeWorkflow_MissingPermissions verifies that a workflow using
// pull_request but without a top-level permissions block fails.
func TestDependabotAutoMergeWorkflow_MissingPermissions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/repos/lucas42/test_repo/contents/.github/workflows/dependabot-auto-merge.yml" {
			w.Write([]byte(encodeWorkflowContent(missingPermissionsYAML)))
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
		t.Errorf("expected Pass=false for missing permissions block, got Detail=%q", result.Detail)
	}
	if result.Err != nil {
		t.Errorf("expected Err=nil, got %v", result.Err)
	}
}

const secretsInheritYAML = `name: Dependabot auto-merge
on:
  pull_request:
    types: [opened, synchronize, reopened]

permissions:
  pull-requests: write
  contents: write

jobs:
  dependabot:
    uses: lucas42/.github/.github/workflows/dependabot-auto-merge.yml@main
    secrets: inherit
`

// TestDependabotAutoMergeWorkflow_SecretsInherit verifies that a workflow using
// secrets: inherit fails — Dependabot PRs cannot access secrets on pull_request events.
func TestDependabotAutoMergeWorkflow_SecretsInherit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/repos/lucas42/test_repo/contents/.github/workflows/dependabot-auto-merge.yml" {
			w.Write([]byte(encodeWorkflowContent(secretsInheritYAML)))
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
		t.Errorf("expected Pass=false for secrets: inherit workflow, got Detail=%q", result.Detail)
	}
	if result.Err != nil {
		t.Errorf("expected Err=nil, got %v", result.Err)
	}
}

// TestDependabotAutoMergeWorkflow_InlineWorkflow verifies that a workflow
// with inline logic (not using the reusable workflow) fails.
func TestDependabotAutoMergeWorkflow_InlineWorkflow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/repos/lucas42/test_repo/contents/.github/workflows/dependabot-auto-merge.yml" {
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
