package conventions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// secretsResponse builds a JSON response body for the GitHub Actions secrets API.
func secretsResponse(names ...string) string {
	type secret struct {
		Name string `json:"name"`
	}
	type response struct {
		TotalCount int      `json:"total_count"`
		Secrets    []secret `json:"secrets"`
	}
	r := response{TotalCount: len(names)}
	for _, n := range names {
		r.Secrets = append(r.Secrets, secret{Name: n})
	}
	b, _ := json.Marshal(r)
	return string(b)
}

// TestAutoMergeSecrets_BothSecretsPresent verifies that a repo with a
// code-reviewer-auto-merge workflow and both secrets set passes.
func TestAutoMergeSecrets_BothSecretsPresent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/lucas42/test_repo/contents/.github/workflows/code-reviewer-auto-merge.yml":
			w.Write([]byte(`{"type":"file"}`))
		case "/repos/lucas42/test_repo/actions/secrets":
			w.Write([]byte(secretsResponse("CODE_REVIEWER_APP_ID", "CODE_REVIEWER_PRIVATE_KEY")))
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

	result := findConvention(t, "auto-merge-secrets").Check(repo)
	if !result.Pass {
		t.Errorf("expected Pass=true, got Detail=%q", result.Detail)
	}
}

// TestAutoMergeSecrets_MissingBothSecrets verifies that a repo with a
// code-reviewer-auto-merge workflow but neither secret set fails.
func TestAutoMergeSecrets_MissingBothSecrets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/lucas42/test_repo/contents/.github/workflows/code-reviewer-auto-merge.yml":
			w.Write([]byte(`{"type":"file"}`))
		case "/repos/lucas42/test_repo/actions/secrets":
			w.Write([]byte(secretsResponse()))
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

	result := findConvention(t, "auto-merge-secrets").Check(repo)
	if result.Pass {
		t.Errorf("expected Pass=false when both secrets missing, got Detail=%q", result.Detail)
	}
	if result.Err != nil {
		t.Errorf("expected Err=nil, got %v", result.Err)
	}
}

// TestAutoMergeSecrets_MissingPrivateKey verifies that a repo missing only
// CODE_REVIEWER_PRIVATE_KEY fails.
func TestAutoMergeSecrets_MissingPrivateKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/lucas42/test_repo/contents/.github/workflows/code-reviewer-auto-merge.yml":
			w.Write([]byte(`{"type":"file"}`))
		case "/repos/lucas42/test_repo/actions/secrets":
			w.Write([]byte(secretsResponse("CODE_REVIEWER_APP_ID")))
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

	result := findConvention(t, "auto-merge-secrets").Check(repo)
	if result.Pass {
		t.Errorf("expected Pass=false when CODE_REVIEWER_PRIVATE_KEY missing, got Detail=%q", result.Detail)
	}
	if result.Err != nil {
		t.Errorf("expected Err=nil, got %v", result.Err)
	}
}

// TestAutoMergeSecrets_MissingAppID verifies that a repo missing only
// CODE_REVIEWER_APP_ID fails.
func TestAutoMergeSecrets_MissingAppID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/lucas42/test_repo/contents/.github/workflows/code-reviewer-auto-merge.yml":
			w.Write([]byte(`{"type":"file"}`))
		case "/repos/lucas42/test_repo/actions/secrets":
			w.Write([]byte(secretsResponse("CODE_REVIEWER_PRIVATE_KEY")))
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

	result := findConvention(t, "auto-merge-secrets").Check(repo)
	if result.Pass {
		t.Errorf("expected Pass=false when CODE_REVIEWER_APP_ID missing, got Detail=%q", result.Detail)
	}
	if result.Err != nil {
		t.Errorf("expected Err=nil, got %v", result.Err)
	}
}

// TestAutoMergeSecrets_NoWorkflow verifies that a repo with no code-reviewer
// auto-merge workflow passes without checking secrets.
func TestAutoMergeSecrets_NoWorkflow(t *testing.T) {
	secretsAPICalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/test_repo/actions/secrets" {
			secretsAPICalled = true
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
		t.Errorf("expected Pass=true when no workflow exists, got Detail=%q", result.Detail)
	}
	if secretsAPICalled {
		t.Error("expected secrets API not to be called when no workflow exists, but it was")
	}
}

// TestAutoMergeSecrets_DependabotOnlyRepo verifies that a repo with only a
// dependabot-auto-merge workflow (no code-reviewer one) passes — the dependabot
// workflow uses GITHUB_TOKEN only and does not require these secrets.
func TestAutoMergeSecrets_DependabotOnlyRepo(t *testing.T) {
	secretsAPICalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/repos/lucas42/test_repo/actions/secrets" {
			secretsAPICalled = true
		}
		// dependabot-auto-merge.yml exists but code-reviewer-auto-merge.yml does not
		if r.URL.Path == "/repos/lucas42/test_repo/contents/.github/workflows/dependabot-auto-merge.yml" {
			w.Write([]byte(`{"type":"file"}`))
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
	if secretsAPICalled {
		t.Error("expected secrets API not to be called for dependabot-only repo, but it was")
	}
}

// TestAutoMergeSecrets_WorkflowFileAPIError verifies that an API error when
// checking for the workflow file sets Err.
func TestAutoMergeSecrets_WorkflowFileAPIError(t *testing.T) {
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

// TestAutoMergeSecrets_SecretsAPIError verifies that an API error when
// fetching secrets sets Err.
func TestAutoMergeSecrets_SecretsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/repos/lucas42/test_repo/contents/.github/workflows/code-reviewer-auto-merge.yml" {
			w.Write([]byte(`{"type":"file"}`))
			return
		}
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
