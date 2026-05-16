package conventions

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Real GitHub API response shape for GET /repos/{owner}/{repo}/actions/permissions/fork-pr-contributor-approval
// (verified against https://docs.github.com/rest/actions/permissions#get-fork-pr-contributor-approval-permissions-for-a-repository):
//
//	{"approval_policy": "first_time_contributors_new_to_github"}
//
// The JSON field is "approval_policy" — NOT "fork-pr-contributor-approval" (the URL path segment).
// All test fixtures below must use "approval_policy" to reflect the real API contract.
// If the fixture and the struct tag use the same wrong name, both the test and production
// code will silently agree on the wrong shape and the test won't catch the mismatch.

// TestForkPRContributorApproval_Correct verifies that a repo with the expected
// policy passes the convention.
func TestForkPRContributorApproval_Correct(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/test_repo/actions/permissions/fork-pr-contributor-approval" && r.Method == "GET" {
			w.WriteHeader(http.StatusOK)
			// "approval_policy" is the real GitHub API field name — see package comment above.
			w.Write([]byte(`{"approval_policy":"first_time_contributors_new_to_github"}`))
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

	result := findConvention(t, "fork-pr-contributor-approval").Check(repo)
	if !result.Pass {
		t.Errorf("expected Pass=true, got Detail=%q", result.Detail)
	}
	if result.Err != nil {
		t.Errorf("expected Err=nil, got %v", result.Err)
	}
}

// TestForkPRContributorApproval_WrongPolicy verifies that a repo with the
// default policy (first_time_contributors) fails the convention.
func TestForkPRContributorApproval_WrongPolicy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/test_repo/actions/permissions/fork-pr-contributor-approval" && r.Method == "GET" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"approval_policy":"first_time_contributors"}`))
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

	result := findConvention(t, "fork-pr-contributor-approval").Check(repo)
	if result.Pass {
		t.Errorf("expected Pass=false, got Detail=%q", result.Detail)
	}
	if result.Err != nil {
		t.Errorf("expected Err=nil, got %v", result.Err)
	}
}

// TestForkPRContributorApproval_UnknownResponseShape verifies that a response
// whose JSON fields don't match the expected schema (e.g. due to a field rename
// in the GitHub API) is treated as an error rather than silently returning an
// empty string and producing a false positive failure on every repo.
// This guards against the class of bug where the struct tag and the fixture
// both use the wrong field name and the test passes against a fiction.
func TestForkPRContributorApproval_UnknownResponseShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/test_repo/actions/permissions/fork-pr-contributor-approval" && r.Method == "GET" {
			w.WriteHeader(http.StatusOK)
			// Simulate a response whose field name doesn't match the struct tag —
			// exactly the failure mode we hit when "fork-pr-contributor-approval"
			// was used instead of "approval_policy".
			w.Write([]byte(`{"wrong_field_name":"first_time_contributors_new_to_github"}`))
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

	result := findConvention(t, "fork-pr-contributor-approval").Check(repo)
	if result.Err == nil {
		t.Errorf("expected Err!=nil for unknown response shape (would otherwise produce silent false positives), got Pass=%v Detail=%q", result.Pass, result.Detail)
	}
}

// TestForkPRContributorApproval_APIError verifies that an API error sets Err.
func TestForkPRContributorApproval_APIError(t *testing.T) {
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

	result := findConvention(t, "fork-pr-contributor-approval").Check(repo)
	if result.Err == nil {
		t.Errorf("expected Err!=nil for API error, got Pass=%v Detail=%q", result.Pass, result.Detail)
	}
}
