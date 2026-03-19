package conventions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// branchProtectionFixture builds a minimal branch protection JSON response
// containing the given required status check context names in the legacy
// "contexts" field.
func branchProtectionFixture(contexts []string) []byte {
	type requiredStatusChecks struct {
		Contexts []string `json:"contexts"`
	}
	type response struct {
		RequiredStatusChecks requiredStatusChecks `json:"required_status_checks"`
	}
	b, _ := json.Marshal(response{
		RequiredStatusChecks: requiredStatusChecks{Contexts: contexts},
	})
	return b
}

// branchProtectionFixtureWithChecks builds a branch protection JSON response
// containing required status checks in the modern "checks" array (as populated
// by the current GitHub UI), leaving "contexts" empty.
func branchProtectionFixtureWithChecks(checkNames []string) []byte {
	type checkEntry struct {
		Context string `json:"context"`
		AppID   int    `json:"app_id"`
	}
	type requiredStatusChecks struct {
		Contexts []string     `json:"contexts"`
		Checks   []checkEntry `json:"checks"`
	}
	type response struct {
		RequiredStatusChecks requiredStatusChecks `json:"required_status_checks"`
	}
	entries := make([]checkEntry, len(checkNames))
	for i, name := range checkNames {
		entries[i] = checkEntry{Context: name, AppID: 12345}
	}
	b, _ := json.Marshal(response{
		RequiredStatusChecks: requiredStatusChecks{
			Contexts: []string{},
			Checks:   entries,
		},
	})
	return b
}

// TestCodeQLRequiredForAutoMerge_Registered verifies the convention is registered
// with the expected fields.
func TestCodeQLRequiredForAutoMerge_Registered(t *testing.T) {
	cs := All()
	var found *Convention
	for i, c := range cs {
		if c.ID == "codeql-required-for-auto-merge" {
			found = &cs[i]
			break
		}
	}
	if found == nil {
		t.Fatal("codeql-required-for-auto-merge convention not found in registry")
	}
	if found.Description == "" {
		t.Error("codeql-required-for-auto-merge has empty description")
	}
	if found.Rationale == "" {
		t.Error("codeql-required-for-auto-merge has empty rationale")
	}
	if found.Guidance == "" {
		t.Error("codeql-required-for-auto-merge has empty guidance")
	}
	if found.Check == nil {
		t.Error("codeql-required-for-auto-merge has nil Check function")
	}
	if !found.AppliesToType(RepoTypeSystem) {
		t.Error("codeql-required-for-auto-merge should apply to RepoTypeSystem")
	}
	if !found.AppliesToType(RepoTypeComponent) {
		t.Error("codeql-required-for-auto-merge should apply to RepoTypeComponent")
	}
	// The convention is not meaningful for unconfigured repos (which wouldn't
	// have auto-merge workflows in practice), so it is scoped to exclude them.
	// If that changes, update this assertion.
	if found.AppliesToType(RepoTypeUnconfigured) {
		t.Error("codeql-required-for-auto-merge should not apply to RepoTypeUnconfigured")
	}
}

// TestCodeQLRequiredForAutoMerge_NoAutoMergeWorkflow verifies the convention
// passes when the repo has no code-reviewer-auto-merge.yml workflow.
func TestCodeQLRequiredForAutoMerge_NoAutoMergeWorkflow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Repo has no workflows at all.
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/test_repo",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	}

	var conv Convention
	for _, c := range All() {
		if c.ID == "codeql-required-for-auto-merge" {
			conv = c
			break
		}
	}
	if conv.Check == nil {
		t.Fatal("convention not found")
	}

	result := conv.Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when no auto-merge workflow present, got fail: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "does not apply") {
		t.Errorf("expected detail to indicate convention does not apply, got: %s", result.Detail)
	}
}

// TestCodeQLRequiredForAutoMerge_AutoMergeWithCodeQL verifies the convention
// passes when auto-merge is present and a CodeQL check is in the required checks.
func TestCodeQLRequiredForAutoMerge_AutoMergeWithCodeQL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/lucas42/test_repo/contents/.github/workflows/code-reviewer-auto-merge.yml":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"type":"file"}`))
		case "/repos/lucas42/test_repo/branches/main/protection":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(branchProtectionFixture([]string{"Analyze (python)", "lucos/build-amd64"}))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/test_repo",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	}

	var conv Convention
	for _, c := range All() {
		if c.ID == "codeql-required-for-auto-merge" {
			conv = c
			break
		}
	}
	if conv.Check == nil {
		t.Fatal("convention not found")
	}

	result := conv.Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when CodeQL check is required, got fail: %s", result.Detail)
	}
}

// TestCodeQLRequiredForAutoMerge_AutoMergeWithoutCodeQL verifies the convention
// fails when auto-merge is present but no CodeQL check is in the required checks.
func TestCodeQLRequiredForAutoMerge_AutoMergeWithoutCodeQL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/lucas42/test_repo/contents/.github/workflows/code-reviewer-auto-merge.yml":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"type":"file"}`))
		case "/repos/lucas42/test_repo/branches/main/protection":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			// Only non-CodeQL checks required.
			w.Write(branchProtectionFixture([]string{"lucos/build-amd64", "test"}))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/test_repo",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	}

	var conv Convention
	for _, c := range All() {
		if c.ID == "codeql-required-for-auto-merge" {
			conv = c
			break
		}
	}
	if conv.Check == nil {
		t.Fatal("convention not found")
	}

	result := conv.Check(repo)
	if result.Pass {
		t.Errorf("expected fail when no CodeQL check is required, got pass: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "no CodeQL Analyze check found") {
		t.Errorf("expected detail to mention missing CodeQL check, got: %s", result.Detail)
	}
}

// TestCodeQLRequiredForAutoMerge_AutoMergeUnprotectedBranch verifies the
// convention fails when auto-merge is present but the branch has no protection
// rules at all.
func TestCodeQLRequiredForAutoMerge_AutoMergeUnprotectedBranch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/lucas42/test_repo/contents/.github/workflows/code-reviewer-auto-merge.yml":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"type":"file"}`))
		case "/repos/lucas42/test_repo/branches/main/protection":
			// Branch is unprotected — GitHub returns 404.
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"Branch not protected"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/test_repo",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	}

	var conv Convention
	for _, c := range All() {
		if c.ID == "codeql-required-for-auto-merge" {
			conv = c
			break
		}
	}
	if conv.Check == nil {
		t.Fatal("convention not found")
	}

	result := conv.Check(repo)
	if result.Pass {
		t.Errorf("expected fail when branch is unprotected, got pass: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "no required status checks are configured") {
		t.Errorf("expected detail to mention missing status checks, got: %s", result.Detail)
	}
}

// TestCodeQLRequiredForAutoMerge_VariousLanguages verifies that the CodeQL check
// name pattern matches different language names.
func TestCodeQLRequiredForAutoMerge_VariousLanguages(t *testing.T) {
	for _, lang := range []string{"python", "javascript", "go", "java", "ruby"} {
		checkName := "Analyze (" + lang + ")"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/repos/lucas42/test_repo/contents/.github/workflows/code-reviewer-auto-merge.yml":
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"type":"file"}`))
			case "/repos/lucas42/test_repo/branches/main/protection":
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write(branchProtectionFixture([]string{checkName}))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))

		repo := RepoContext{
			Name:          "lucas42/test_repo",
			GitHubToken:   "fake-token",
			GitHubBaseURL: server.URL,
		}

		var conv Convention
		for _, c := range All() {
			if c.ID == "codeql-required-for-auto-merge" {
				conv = c
				break
			}
		}
		if conv.Check == nil {
			server.Close()
			t.Fatal("convention not found")
		}

		result := conv.Check(repo)
		if !result.Pass {
			t.Errorf("expected pass for language %q (check %q), got fail: %s", lang, checkName, result.Detail)
		}
		server.Close()
	}
}

// TestGitHubRequiredStatusChecks_ReturnsChecks verifies the helper returns
// status check names from a successful protection response.
func TestGitHubRequiredStatusChecks_ReturnsChecks(t *testing.T) {
	expected := []string{"Analyze (python)", "lucos/build-amd64"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/test_repo/branches/main/protection" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(branchProtectionFixture(expected))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	checks, err := GitHubRequiredStatusChecksFromBase(server.URL, "fake-token", "lucas42/test_repo", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(checks) != len(expected) {
		t.Fatalf("expected %d checks, got %d: %v", len(expected), len(checks), checks)
	}
	for i, c := range checks {
		if c != expected[i] {
			t.Errorf("check[%d]: expected %q, got %q", i, expected[i], c)
		}
	}
}

// TestGitHubRequiredStatusChecks_ReturnsChecksFromModernArray verifies that
// checks configured via the modern GitHub UI (in the "checks" array rather than
// the legacy "contexts" field) are also returned by the helper.
func TestGitHubRequiredStatusChecks_ReturnsChecksFromModernArray(t *testing.T) {
	expected := []string{"Analyze (javascript-typescript)", "ci/circleci: test"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/test_repo/branches/main/protection" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(branchProtectionFixtureWithChecks(expected))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	checks, err := GitHubRequiredStatusChecksFromBase(server.URL, "fake-token", "lucas42/test_repo", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(checks) != len(expected) {
		t.Fatalf("expected %d checks from modern checks array, got %d: %v", len(expected), len(checks), checks)
	}
	for i, c := range checks {
		if c != expected[i] {
			t.Errorf("check[%d]: expected %q, got %q", i, expected[i], c)
		}
	}
}

// TestGitHubRequiredStatusChecks_Unprotected verifies that an unprotected branch
// returns an empty slice without error.
func TestGitHubRequiredStatusChecks_Unprotected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Branch not protected"}`))
	}))
	defer server.Close()

	checks, err := GitHubRequiredStatusChecksFromBase(server.URL, "fake-token", "lucas42/test_repo", "main")
	if err != nil {
		t.Fatalf("unexpected error for unprotected branch: %v", err)
	}
	if len(checks) != 0 {
		t.Errorf("expected empty slice for unprotected branch, got: %v", checks)
	}
}

// TestGitHubRequiredStatusChecks_APIError verifies that unexpected HTTP status
// codes return an error.
func TestGitHubRequiredStatusChecks_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"Resource not accessible by integration"}`))
	}))
	defer server.Close()

	_, err := GitHubRequiredStatusChecksFromBase(server.URL, "fake-token", "lucas42/test_repo", "main")
	if err == nil {
		t.Error("expected error for 403 response, got nil")
	}
}
