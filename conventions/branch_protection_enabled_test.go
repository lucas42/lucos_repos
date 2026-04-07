package conventions

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestBranchProtectionEnabled_Protected verifies that a repo with branch
// protection enabled (and no required approvals) passes the convention.
func TestBranchProtectionEnabled_Protected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/test_repo/branches/main/protection" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"required_status_checks":null,"required_pull_request_reviews":null}`))
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

	result := findConvention(t, "branch-protection-enabled").Check(repo)
	if !result.Pass {
		t.Errorf("expected Pass=true, got Detail=%q", result.Detail)
	}
}

// TestBranchProtectionEnabled_RequiredApprovalsEnabled verifies that a repo
// with "Require approvals" turned on fails the convention.
func TestBranchProtectionEnabled_RequiredApprovalsEnabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/test_repo/branches/main/protection" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"required_status_checks":null,"required_pull_request_reviews":{"required_approving_review_count":1}}`))
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

	result := findConvention(t, "branch-protection-enabled").Check(repo)
	if result.Pass {
		t.Errorf("expected Pass=false for required approvals, got Detail=%q", result.Detail)
	}
	if result.Err != nil {
		t.Errorf("expected Err=nil, got %v", result.Err)
	}
}

// TestBranchProtectionEnabled_NotProtected verifies that a repo without branch
// protection fails the convention.
func TestBranchProtectionEnabled_NotProtected(t *testing.T) {
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

	result := findConvention(t, "branch-protection-enabled").Check(repo)
	if result.Pass {
		t.Errorf("expected Pass=false, got Detail=%q", result.Detail)
	}
	if result.Err != nil {
		t.Errorf("expected Err=nil for missing protection, got %v", result.Err)
	}
}

// TestBranchProtectionEnabled_StrictEnabled verifies that a repo with
// "Require branches to be up to date before merging" turned on fails the convention.
func TestBranchProtectionEnabled_StrictEnabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/test_repo/branches/main/protection" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"required_status_checks":{"strict":true,"contexts":[],"checks":[]},"required_pull_request_reviews":null}`))
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

	result := findConvention(t, "branch-protection-enabled").Check(repo)
	if result.Pass {
		t.Errorf("expected Pass=false for strict=true, got Detail=%q", result.Detail)
	}
	if result.Err != nil {
		t.Errorf("expected Err=nil, got %v", result.Err)
	}
}

// TestBranchProtectionEnabled_StrictDisabled verifies that a repo with
// required status checks but strict=false still passes the convention.
func TestBranchProtectionEnabled_StrictDisabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/test_repo/branches/main/protection" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"required_status_checks":{"strict":false,"contexts":[],"checks":[{"context":"ci/circleci"}]},"required_pull_request_reviews":null}`))
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

	result := findConvention(t, "branch-protection-enabled").Check(repo)
	if !result.Pass {
		t.Errorf("expected Pass=true for strict=false, got Detail=%q", result.Detail)
	}
}

// TestBranchProtectionEnabled_APIError verifies that an API error sets Err.
func TestBranchProtectionEnabled_APIError(t *testing.T) {
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

	result := findConvention(t, "branch-protection-enabled").Check(repo)
	if result.Err == nil {
		t.Errorf("expected Err!=nil for API error, got Pass=%v Detail=%q", result.Pass, result.Detail)
	}
}
