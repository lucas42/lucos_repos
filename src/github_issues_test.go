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

// TestConventionIssueTitle verifies the standardised title format.
func TestConventionIssueTitle(t *testing.T) {
	title := conventionIssueTitle("has-circleci-config", "Repository has a .circleci/config.yml file")
	expected := "[Convention] has-circleci-config: Repository has a .circleci/config.yml file"
	if title != expected {
		t.Errorf("expected title %q, got %q", expected, title)
	}
}
