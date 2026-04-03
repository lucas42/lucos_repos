package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// buildIssuesList returns a JSON-encoded slice of gitHubIssue for use in test servers.
func buildIssuesList(issues []gitHubIssue) []byte {
	b, _ := json.Marshal(issues)
	return b
}

// TestEnsureIssueExists_OpenIssueAlreadyExists verifies that when an open issue
// with the correct title already exists, EnsureIssueExists returns its URL
// without creating a new issue.
func TestEnsureIssueExists_OpenIssueAlreadyExists(t *testing.T) {
	const existingURL = "https://github.com/lucas42/test_repo/issues/5"
	title := conventionIssueTitle("has-circleci-config", "Repository has a .circleci/config.yml file")
	createCalled := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/repos/"):
			w.Header().Set("Content-Type", "application/json")
			w.Write(buildIssuesList([]gitHubIssue{
				{Number: 5, HTMLURL: existingURL, Title: title, State: "open"},
			}))
		case r.Method == "POST":
			createCalled = true
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewGitHubIssueClient(server.URL, "fake-token")
	conv := ConventionInfo{ID: "has-circleci-config", Description: "Repository has a .circleci/config.yml file"}
	gotURL, err := client.EnsureIssueExists("lucas42/test_repo", conv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotURL != existingURL {
		t.Errorf("expected URL %q, got %q", existingURL, gotURL)
	}
	if createCalled {
		t.Error("expected no issue creation when open issue already exists")
	}
}

// TestEnsureIssueExists_CreatesNewIssue verifies that when no open or closed
// issue exists, a new issue is created and its URL returned.
func TestEnsureIssueExists_CreatesNewIssue(t *testing.T) {
	const newURL = "https://github.com/lucas42/test_repo/issues/10"
	title := conventionIssueTitle("has-circleci-config", "Repository has a .circleci/config.yml file")
	createCalled := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/repos/"):
			// No issues found for either open or closed queries.
			w.Header().Set("Content-Type", "application/json")
			w.Write(buildIssuesList([]gitHubIssue{}))
		case r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/repos/"):
			createCalled = true
			// Parse request to verify title and label.
			var payload createIssueRequest
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("failed to decode create issue request: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if payload.Title != title {
				t.Errorf("expected title %q, got %q", title, payload.Title)
			}
			hasLabel := false
			for _, l := range payload.Labels {
				if l == auditFindingLabel {
					hasLabel = true
					break
				}
			}
			if !hasLabel {
				t.Errorf("expected label %q in created issue, got %v", auditFindingLabel, payload.Labels)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(gitHubIssue{
				Number:  10,
				HTMLURL: newURL,
				Title:   title,
				State:   "open",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewGitHubIssueClient(server.URL, "fake-token")
	conv := ConventionInfo{ID: "has-circleci-config", Description: "Repository has a .circleci/config.yml file"}
	gotURL, err := client.EnsureIssueExists("lucas42/test_repo", conv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotURL != newURL {
		t.Errorf("expected URL %q, got %q", newURL, gotURL)
	}
	if !createCalled {
		t.Error("expected issue creation, but POST was never called")
	}
}

// TestEnsureIssueExists_ReferencesClosedIssue verifies that when a closed issue
// exists, the new issue body references it.
func TestEnsureIssueExists_ReferencesClosedIssue(t *testing.T) {
	const closedURL = "https://github.com/lucas42/test_repo/issues/3"
	const newURL = "https://github.com/lucas42/test_repo/issues/11"
	title := conventionIssueTitle("has-circleci-config", "Repository has a .circleci/config.yml file")

	var createdBody string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/repos/"):
			w.Header().Set("Content-Type", "application/json")
			state := r.URL.Query().Get("state")
			if state == "open" {
				// No open issues.
				w.Write(buildIssuesList([]gitHubIssue{}))
			} else {
				// One closed issue.
				w.Write(buildIssuesList([]gitHubIssue{
					{Number: 3, HTMLURL: closedURL, Title: title, State: "closed"},
				}))
			}
		case r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/repos/"):
			var payload createIssueRequest
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("failed to decode create issue request: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			createdBody = payload.Body
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(gitHubIssue{
				Number:  11,
				HTMLURL: newURL,
				Title:   title,
				State:   "open",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewGitHubIssueClient(server.URL, "fake-token")
	conv := ConventionInfo{ID: "has-circleci-config", Description: "Repository has a .circleci/config.yml file"}
	gotURL, err := client.EnsureIssueExists("lucas42/test_repo", conv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotURL != newURL {
		t.Errorf("expected URL %q, got %q", newURL, gotURL)
	}
	if !strings.Contains(createdBody, closedURL) {
		t.Errorf("expected created issue body to reference closed issue URL %q, got body: %q", closedURL, createdBody)
	}
}

// TestEnsureIssueExists_ExactTitleMatch verifies that a list result with a
// different (but similar) title does not count as an existing open issue.
func TestEnsureIssueExists_ExactTitleMatch(t *testing.T) {
	title := conventionIssueTitle("has-circleci-config", "Repository has a .circleci/config.yml file")
	differentTitle := conventionIssueTitle("has-circleci-config", "Some other description")
	const newURL = "https://github.com/lucas42/test_repo/issues/12"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/repos/"):
			w.Header().Set("Content-Type", "application/json")
			// Return a result with a different title — should be filtered out by local title check.
			w.Write(buildIssuesList([]gitHubIssue{
				{Number: 7, HTMLURL: "https://github.com/lucas42/test_repo/issues/7", Title: differentTitle, State: "open"},
			}))
		case r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/repos/"):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(gitHubIssue{
				Number:  12,
				HTMLURL: newURL,
				Title:   title,
				State:   "open",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewGitHubIssueClient(server.URL, "fake-token")
	conv := ConventionInfo{ID: "has-circleci-config", Description: "Repository has a .circleci/config.yml file"}
	gotURL, err := client.EnsureIssueExists("lucas42/test_repo", conv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should have created a new issue, not returned the mismatched one.
	if gotURL != newURL {
		t.Errorf("expected new issue URL %q, got %q", newURL, gotURL)
	}
}

// TestEnsureIssueExists_CreateError verifies that a GitHub API error during
// issue creation propagates as an error.
func TestEnsureIssueExists_CreateError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/repos/"):
			w.Header().Set("Content-Type", "application/json")
			w.Write(buildIssuesList([]gitHubIssue{}))
		case r.Method == "POST":
			w.WriteHeader(http.StatusUnprocessableEntity)
			w.Write([]byte(`{"message":"Validation Failed"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewGitHubIssueClient(server.URL, "fake-token")
	conv := ConventionInfo{ID: "has-circleci-config", Description: "Repository has a .circleci/config.yml file"}
	_, err := client.EnsureIssueExists("lucas42/test_repo", conv)
	if err == nil {
		t.Error("expected error when issue creation fails, got nil")
	}
}

// TestEnsureIssueExists_IncludesRationaleAndGuidance verifies that when a
// convention has Rationale and Guidance set, those appear in the created issue body.
func TestEnsureIssueExists_IncludesRationaleAndGuidance(t *testing.T) {
	const newURL = "https://github.com/lucas42/test_repo/issues/20"
	conv := ConventionInfo{
		ID:          "has-circleci-config",
		Description: "Repository has a .circleci/config.yml file",
		Rationale:   "Without CI, changes are not automatically deployed.",
		Guidance:    "Add a .circleci/config.yml following the standard template.",
	}
	title := conventionIssueTitle(conv.ID, conv.Description)
	var createdBody string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/repos/"):
			w.Header().Set("Content-Type", "application/json")
			w.Write(buildIssuesList([]gitHubIssue{}))
		case r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/repos/"):
			var payload createIssueRequest
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("failed to decode create issue request: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			createdBody = payload.Body
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(gitHubIssue{
				Number:  20,
				HTMLURL: newURL,
				Title:   title,
				State:   "open",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewGitHubIssueClient(server.URL, "fake-token")
	_, err := client.EnsureIssueExists("lucas42/test_repo", conv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(createdBody, "**Why this matters:**") {
		t.Errorf("expected body to contain rationale header, got: %q", createdBody)
	}
	if !strings.Contains(createdBody, conv.Rationale) {
		t.Errorf("expected body to contain rationale text %q, got: %q", conv.Rationale, createdBody)
	}
	if !strings.Contains(createdBody, "**Suggested fix:**") {
		t.Errorf("expected body to contain guidance header, got: %q", createdBody)
	}
	if !strings.Contains(createdBody, conv.Guidance) {
		t.Errorf("expected body to contain guidance text %q, got: %q", conv.Guidance, createdBody)
	}
}

// TestEnsureIssueExists_IncludesDetail verifies that when a convention result
// has a non-empty Detail string, it appears in the created issue body.
func TestEnsureIssueExists_IncludesDetail(t *testing.T) {
	const newURL = "https://github.com/lucas42/test_repo/issues/30"
	conv := ConventionInfo{
		ID:          "circleci-system-deploy-jobs",
		Description: "CircleCI config includes the correct deploy jobs for all configured hosts",
		Detail:      "Expected deploy jobs: lucos/deploy-avalon; Found: none",
	}
	title := conventionIssueTitle(conv.ID, conv.Description)
	var createdBody string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/repos/"):
			w.Header().Set("Content-Type", "application/json")
			w.Write(buildIssuesList([]gitHubIssue{}))
		case r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/repos/"):
			var payload createIssueRequest
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("failed to decode create issue request: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			createdBody = payload.Body
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(gitHubIssue{
				Number:  30,
				HTMLURL: newURL,
				Title:   title,
				State:   "open",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewGitHubIssueClient(server.URL, "fake-token")
	_, err := client.EnsureIssueExists("lucas42/test_repo", conv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(createdBody, "**Detail:**") {
		t.Errorf("expected body to contain detail header, got: %q", createdBody)
	}
	if !strings.Contains(createdBody, conv.Detail) {
		t.Errorf("expected body to contain detail text %q, got: %q", conv.Detail, createdBody)
	}
}

// TestEnsureIssueExists_EmptyDetailOmitted verifies that when a convention result
// has an empty Detail string, no detail section appears in the issue body.
func TestEnsureIssueExists_EmptyDetailOmitted(t *testing.T) {
	const newURL = "https://github.com/lucas42/test_repo/issues/31"
	conv := ConventionInfo{
		ID:          "circleci-config-exists",
		Description: "Repository has a .circleci/config.yml file",
		Detail:      "", // empty — should be omitted
	}
	title := conventionIssueTitle(conv.ID, conv.Description)
	var createdBody string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/repos/"):
			w.Header().Set("Content-Type", "application/json")
			w.Write(buildIssuesList([]gitHubIssue{}))
		case r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/repos/"):
			var payload createIssueRequest
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("failed to decode create issue request: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			createdBody = payload.Body
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(gitHubIssue{
				Number:  31,
				HTMLURL: newURL,
				Title:   title,
				State:   "open",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewGitHubIssueClient(server.URL, "fake-token")
	_, err := client.EnsureIssueExists("lucas42/test_repo", conv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(createdBody, "**Detail:**") {
		t.Errorf("expected body NOT to contain detail header when Detail is empty, got: %q", createdBody)
	}
}

// TestConventionIssueTitle verifies the standardised title format.
func TestConventionIssueTitle(t *testing.T) {
	title := conventionIssueTitle("has-circleci-config", "Repository has a .circleci/config.yml file")
	expected := "[Convention] has-circleci-config: Repository has a .circleci/config.yml file"
	if title != expected {
		t.Errorf("expected title %q, got %q", expected, title)
	}
}

// TestEnsureIssueExists_ArchivedRepo403 verifies that when the issues list
// endpoint returns a non-rate-limit 403 (e.g. archived repo), EnsureIssueExists
// returns an error wrapping ErrIssuesUnavailable.
func TestEnsureIssueExists_ArchivedRepo403(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return a non-rate-limit 403 on the issues list — simulates archived repo.
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"Repository was archived so is read-only."}`))
	}))
	defer server.Close()

	client := NewGitHubIssueClient(server.URL, "fake-token")
	conv := ConventionInfo{ID: "has-circleci-config", Description: "Repository has a .circleci/config.yml file"}
	_, err := client.EnsureIssueExists("lucas42/archived_repo", conv)
	if err == nil {
		t.Fatal("expected error for 403, got nil")
	}
	if !isIssuesUnavailableErr(err) {
		t.Errorf("expected ErrIssuesUnavailable, got: %v", err)
	}
}

// TestEnsureIssueExists_IssuesDisabled410 verifies that when the issues list
// endpoint returns 410 (issues disabled), EnsureIssueExists returns an error
// wrapping ErrIssuesUnavailable.
func TestEnsureIssueExists_IssuesDisabled410(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
		w.Write([]byte(`{"message":"Issues has been disabled in this repository."}`))
	}))
	defer server.Close()

	client := NewGitHubIssueClient(server.URL, "fake-token")
	conv := ConventionInfo{ID: "has-circleci-config", Description: "Repository has a .circleci/config.yml file"}
	_, err := client.EnsureIssueExists("lucas42/no_issues_repo", conv)
	if err == nil {
		t.Fatal("expected error for 410, got nil")
	}
	if !isIssuesUnavailableErr(err) {
		t.Errorf("expected ErrIssuesUnavailable, got: %v", err)
	}
}

// TestEnsureIssueExists_CreateReturns403 verifies that when the issue create
// endpoint returns 403 (e.g. archived repo, despite passing the list check),
// EnsureIssueExists returns ErrIssuesUnavailable.
func TestEnsureIssueExists_CreateReturns403(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET":
			// Issue list: no existing issues.
			w.Header().Set("Content-Type", "application/json")
			w.Write(buildIssuesList([]gitHubIssue{}))
		case r.Method == "POST":
			// Create: archived repo 403.
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"message":"Repository was archived so is read-only."}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewGitHubIssueClient(server.URL, "fake-token")
	conv := ConventionInfo{ID: "has-circleci-config", Description: "Repository has a .circleci/config.yml file"}
	_, err := client.EnsureIssueExists("lucas42/archived_repo", conv)
	if err == nil {
		t.Fatal("expected error for 403 on create, got nil")
	}
	if !isIssuesUnavailableErr(err) {
		t.Errorf("expected ErrIssuesUnavailable, got: %v", err)
	}
}

// TestCloseIssueIfOpen_NoOpenIssue verifies that when there is no open
// audit-finding issue for the convention, CloseIssueIfOpen is a no-op.
func TestCloseIssueIfOpen_NoOpenIssue(t *testing.T) {
	closeCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/issues"):
			// No open issues.
			w.Write(buildIssuesList([]gitHubIssue{}))
		case r.Method == http.MethodPatch:
			closeCalled = true
			w.Write([]byte(`{"number":1,"state":"closed"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewGitHubIssueClient(server.URL, "fake-token")
	conv := ConventionInfo{ID: "has-circleci-config", Description: "Repository has a .circleci/config.yml file"}
	err := client.CloseIssueIfOpen("lucas42/test_repo", conv)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if closeCalled {
		t.Error("expected close endpoint not to be called when no open issue exists")
	}
}

// TestCloseIssueIfOpen_ClosesOpenIssue verifies that when an open audit-finding
// issue exists for the convention, CloseIssueIfOpen posts a comment and closes it.
func TestCloseIssueIfOpen_ClosesOpenIssue(t *testing.T) {
	commentPosted := false
	issueClosed := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/issues"):
			// One open audit-finding issue with matching title.
			title := conventionIssueTitle("has-circleci-config", "Repository has a .circleci/config.yml file")
			w.Write(buildIssuesList([]gitHubIssue{
				{Number: 42, HTMLURL: "https://github.com/lucas42/test_repo/issues/42", Title: title, State: "open"},
			}))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/comments"):
			commentPosted = true
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"id":1}`))
		case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/issues/42"):
			issueClosed = true
			w.Write([]byte(`{"number":42,"state":"closed"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewGitHubIssueClient(server.URL, "fake-token")
	conv := ConventionInfo{ID: "has-circleci-config", Description: "Repository has a .circleci/config.yml file"}
	err := client.CloseIssueIfOpen("lucas42/test_repo", conv)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !commentPosted {
		t.Error("expected a closing comment to be posted before closing")
	}
	if !issueClosed {
		t.Error("expected issue to be closed")
	}
}

// TestCloseIssueIfOpen_IssuesUnavailable verifies that when the issues API
// returns 410, CloseIssueIfOpen returns ErrIssuesUnavailable.
func TestCloseIssueIfOpen_IssuesUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
		w.Write([]byte(`{"message":"Issues has been disabled."}`))
	}))
	defer server.Close()

	client := NewGitHubIssueClient(server.URL, "fake-token")
	conv := ConventionInfo{ID: "has-circleci-config", Description: "Repository has a .circleci/config.yml file"}
	err := client.CloseIssueIfOpen("lucas42/no_issues_repo", conv)
	if err == nil {
		t.Fatal("expected error for unavailable issues, got nil")
	}
	if !isIssuesUnavailableErr(err) {
		t.Errorf("expected ErrIssuesUnavailable, got: %v", err)
	}
}

// TestCloseIssueIfOpen_CloseAPIError verifies that when the close PATCH returns
// an unexpected error, CloseIssueIfOpen propagates that error.
func TestCloseIssueIfOpen_CloseAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/issues"):
			title := conventionIssueTitle("has-circleci-config", "Repository has a .circleci/config.yml file")
			w.Write(buildIssuesList([]gitHubIssue{
				{Number: 5, HTMLURL: "https://github.com/lucas42/test_repo/issues/5", Title: title, State: "open"},
			}))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/comments"):
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"id":1}`))
		case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/issues/5"):
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"message":"server error"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewGitHubIssueClient(server.URL, "fake-token")
	conv := ConventionInfo{ID: "has-circleci-config", Description: "Repository has a .circleci/config.yml file"}
	err := client.CloseIssueIfOpen("lucas42/test_repo", conv)
	if err == nil {
		t.Fatal("expected error when close API fails, got nil")
	}
}
