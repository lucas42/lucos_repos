package conventions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Helper to build a directory listing response for the GitHub Contents API.
func encodeDirListing(names []string) string {
	type entry struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	var entries []entry
	for _, n := range names {
		entries = append(entries, entry{Name: n, Type: "file"})
	}
	b, _ := json.Marshal(entries)
	return string(b)
}

const pinnedCodeReviewerYAML = `name: Auto-merge on code reviewer approval

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
    uses: lucas42/.github/.github/workflows/code-reviewer-auto-merge.yml@fa6177c065517f4c8cb8938730c3bc27ff5c2f0d
    secrets:
      CODE_REVIEWER_APP_ID: ${{ secrets.CODE_REVIEWER_APP_ID }}
      CODE_REVIEWER_PRIVATE_KEY: ${{ secrets.CODE_REVIEWER_PRIVATE_KEY }}
`

const unpinnedCodeReviewerYAML = `name: Auto-merge on code reviewer approval

on:
  pull_request_review:
    types:
      - submitted

permissions:
  contents: read

jobs:
  reusable:
    uses: lucas42/.github/.github/workflows/code-reviewer-auto-merge.yml@main
    secrets:
      CODE_REVIEWER_APP_ID: ${{ secrets.CODE_REVIEWER_APP_ID }}
      CODE_REVIEWER_PRIVATE_KEY: ${{ secrets.CODE_REVIEWER_PRIVATE_KEY }}
`

const pinnedDependabotYAML = `name: Dependabot auto-merge

on:
  pull_request:
    types: [opened, synchronize, reopened]

permissions:
  pull-requests: write
  contents: write

jobs:
  dependabot:
    uses: lucas42/.github/.github/workflows/dependabot-auto-merge.yml@fa6177c065517f4c8cb8938730c3bc27ff5c2f0d
`

const unpinnedDependabotYAML = `name: Dependabot auto-merge

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

const noReusableWorkflowYAML = `name: CI

on: push

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: npm test
`

const tagRefYAML = `name: Auto-merge

on:
  pull_request_review:
    types: [submitted]

permissions:
  contents: read

jobs:
  reusable:
    uses: lucas42/.github/.github/workflows/code-reviewer-auto-merge.yml@v1
`

// TestReusableWorkflowPinnedToSHA_AllPinned verifies that a repo with all
// reusable workflow references pinned to SHA passes.
func TestReusableWorkflowPinnedToSHA_AllPinned(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/lucas42/test_repo/contents/.github/workflows":
			w.Write([]byte(encodeDirListing([]string{
				"code-reviewer-auto-merge.yml",
				"dependabot-auto-merge.yml",
			})))
		case "/repos/lucas42/test_repo/contents/.github/workflows/code-reviewer-auto-merge.yml":
			w.Write([]byte(encodeWorkflowContent(pinnedCodeReviewerYAML)))
		case "/repos/lucas42/test_repo/contents/.github/workflows/dependabot-auto-merge.yml":
			w.Write([]byte(encodeWorkflowContent(pinnedDependabotYAML)))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/test_repo",
		GitHubToken:   "fake-token",
		Type:          RepoTypeSystem,
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "reusable-workflow-pinned-to-sha").Check(repo)
	if !result.Pass {
		t.Errorf("expected Pass=true, got Detail=%q", result.Detail)
	}
}

// TestReusableWorkflowPinnedToSHA_UnpinnedCodeReviewer verifies that a repo
// with @main on the code-reviewer workflow fails.
func TestReusableWorkflowPinnedToSHA_UnpinnedCodeReviewer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/lucas42/test_repo/contents/.github/workflows":
			w.Write([]byte(encodeDirListing([]string{
				"code-reviewer-auto-merge.yml",
				"dependabot-auto-merge.yml",
			})))
		case "/repos/lucas42/test_repo/contents/.github/workflows/code-reviewer-auto-merge.yml":
			w.Write([]byte(encodeWorkflowContent(unpinnedCodeReviewerYAML)))
		case "/repos/lucas42/test_repo/contents/.github/workflows/dependabot-auto-merge.yml":
			w.Write([]byte(encodeWorkflowContent(pinnedDependabotYAML)))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/test_repo",
		GitHubToken:   "fake-token",
		Type:          RepoTypeSystem,
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "reusable-workflow-pinned-to-sha").Check(repo)
	if result.Pass {
		t.Errorf("expected Pass=false for unpinned @main reference, got Detail=%q", result.Detail)
	}
}

// TestReusableWorkflowPinnedToSHA_UnpinnedDependabot verifies that a repo
// with @main on the dependabot workflow fails.
func TestReusableWorkflowPinnedToSHA_UnpinnedDependabot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/lucas42/test_repo/contents/.github/workflows":
			w.Write([]byte(encodeDirListing([]string{
				"dependabot-auto-merge.yml",
			})))
		case "/repos/lucas42/test_repo/contents/.github/workflows/dependabot-auto-merge.yml":
			w.Write([]byte(encodeWorkflowContent(unpinnedDependabotYAML)))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/test_repo",
		GitHubToken:   "fake-token",
		Type:          RepoTypeSystem,
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "reusable-workflow-pinned-to-sha").Check(repo)
	if result.Pass {
		t.Errorf("expected Pass=false for unpinned dependabot @main, got Detail=%q", result.Detail)
	}
}

// TestReusableWorkflowPinnedToSHA_TagRef verifies that a tag reference like
// @v1 also fails — only full SHAs are accepted.
func TestReusableWorkflowPinnedToSHA_TagRef(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/lucas42/test_repo/contents/.github/workflows":
			w.Write([]byte(encodeDirListing([]string{"code-reviewer-auto-merge.yml"})))
		case "/repos/lucas42/test_repo/contents/.github/workflows/code-reviewer-auto-merge.yml":
			w.Write([]byte(encodeWorkflowContent(tagRefYAML)))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/test_repo",
		GitHubToken:   "fake-token",
		Type:          RepoTypeSystem,
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "reusable-workflow-pinned-to-sha").Check(repo)
	if result.Pass {
		t.Errorf("expected Pass=false for tag @v1 reference, got Detail=%q", result.Detail)
	}
}

// TestReusableWorkflowPinnedToSHA_NoReusableRefs verifies that a repo with
// workflow files that don't reference lucas42/.github passes.
func TestReusableWorkflowPinnedToSHA_NoReusableRefs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/lucas42/test_repo/contents/.github/workflows":
			w.Write([]byte(encodeDirListing([]string{"ci.yml"})))
		case "/repos/lucas42/test_repo/contents/.github/workflows/ci.yml":
			w.Write([]byte(encodeWorkflowContent(noReusableWorkflowYAML)))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/test_repo",
		GitHubToken:   "fake-token",
		Type:          RepoTypeSystem,
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "reusable-workflow-pinned-to-sha").Check(repo)
	if !result.Pass {
		t.Errorf("expected Pass=true for workflow with no reusable refs, got Detail=%q", result.Detail)
	}
}

// TestReusableWorkflowPinnedToSHA_NoWorkflowsDir verifies that a repo with
// no .github/workflows/ directory passes (convention does not apply).
func TestReusableWorkflowPinnedToSHA_NoWorkflowsDir(t *testing.T) {
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

	result := findConvention(t, "reusable-workflow-pinned-to-sha").Check(repo)
	if !result.Pass {
		t.Errorf("expected Pass=true for repo without workflows dir, got Detail=%q", result.Detail)
	}
}

// TestReusableWorkflowPinnedToSHA_ScriptRepo verifies the convention applies
// to script repos (which have dependabot but not code-reviewer workflows).
func TestReusableWorkflowPinnedToSHA_ScriptRepo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/lucas42/test_script/contents/.github/workflows":
			w.Write([]byte(encodeDirListing([]string{"dependabot-auto-merge.yml"})))
		case "/repos/lucas42/test_script/contents/.github/workflows/dependabot-auto-merge.yml":
			w.Write([]byte(encodeWorkflowContent(unpinnedDependabotYAML)))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/test_script",
		GitHubToken:   "fake-token",
		Type:          RepoTypeScript,
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "reusable-workflow-pinned-to-sha").Check(repo)
	if result.Pass {
		t.Errorf("expected Pass=false for script repo with unpinned ref, got Detail=%q", result.Detail)
	}
}

// TestReusableWorkflowPinnedToSHA_APIError verifies that an API error sets Err.
func TestReusableWorkflowPinnedToSHA_APIError(t *testing.T) {
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

	result := findConvention(t, "reusable-workflow-pinned-to-sha").Check(repo)
	if result.Err == nil {
		t.Errorf("expected Err!=nil for API error, got Pass=%v Detail=%q", result.Pass, result.Detail)
	}
}
