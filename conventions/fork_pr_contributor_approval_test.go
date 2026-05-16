package conventions

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestForkPRContributorApproval_Correct verifies that a repo with the expected
// policy passes the convention.
func TestForkPRContributorApproval_Correct(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/test_repo/actions/permissions/fork-pr-contributor-approval" && r.Method == "GET" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"fork-pr-contributor-approval":"first_time_contributors_new_to_github"}`))
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
			w.Write([]byte(`{"fork-pr-contributor-approval":"first_time_contributors"}`))
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
