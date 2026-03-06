package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newTestSweeper creates an AuditSweeper wired to a temporary DB with its
// base URLs pointing at fake test servers. The githubAuth field is nil — tests
// that exercise sweep() must inject a token some other way.
func newTestSweeper(t *testing.T, configyServer, githubServer *httptest.Server) *AuditSweeper {
	t.Helper()
	db := openTestDB(t)
	// Pre-populate the conventions table so SaveFinding doesn't hit FK errors.
	for _, c := range AllConventions() {
		if err := db.UpsertConvention(c.ID, c.Description); err != nil {
			t.Fatalf("failed to upsert convention %s: %v", c.ID, err)
		}
	}
	s := &AuditSweeper{
		db:               db,
		githubOrg:        "lucas42",
		sweepInterval:    6 * time.Hour,
		configyBaseURL:   configyServer.URL,
		githubAPIBaseURL: githubServer.URL,
	}
	return s
}

// TestFetchConfigySystems_Success verifies that systems are parsed correctly.
func TestFetchConfigySystems_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/systems" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]configySystem{
				{ID: "lucos_photos"},
				{ID: "lucos_notes"},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := &AuditSweeper{configyBaseURL: server.URL}
	systems, err := s.fetchConfigySystems()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(systems) != 2 {
		t.Fatalf("expected 2 systems, got %d", len(systems))
	}
	if systems[0].ID != "lucos_photos" {
		t.Errorf("expected first system 'lucos_photos', got %q", systems[0].ID)
	}
	if systems[1].ID != "lucos_notes" {
		t.Errorf("expected second system 'lucos_notes', got %q", systems[1].ID)
	}
}

// TestFetchConfigySystems_HTTPError verifies that a non-200 response returns an error.
func TestFetchConfigySystems_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	s := &AuditSweeper{configyBaseURL: server.URL}
	_, err := s.fetchConfigySystems()
	if err == nil {
		t.Error("expected error for 500 response, got nil")
	}
}

// TestFetchConfigyComponents_Success verifies that components are parsed correctly.
func TestFetchConfigyComponents_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/components" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]configyComponent{
				{ID: "lucos_navbar"},
				{ID: "restful-queue"},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := &AuditSweeper{configyBaseURL: server.URL}
	components, err := s.fetchConfigyComponents()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(components) != 2 {
		t.Fatalf("expected 2 components, got %d", len(components))
	}
	if components[0].ID != "lucos_navbar" {
		t.Errorf("expected first component 'lucos_navbar', got %q", components[0].ID)
	}
}

// TestFetchRepoTypes_ClassifiesCorrectly verifies systems, components, and
// unconfigured repos are classified correctly.
func TestFetchRepoTypes_ClassifiesCorrectly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/systems":
			json.NewEncoder(w).Encode([]configySystem{
				{ID: "lucos_photos"},
			})
		case "/components":
			json.NewEncoder(w).Encode([]configyComponent{
				{ID: "lucos_navbar"},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	s := &AuditSweeper{configyBaseURL: server.URL, githubOrg: "lucas42"}
	types, err := s.fetchRepoTypes()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if types["lucas42/lucos_photos"] != RepoTypeSystem {
		t.Errorf("expected lucos_photos to be system, got %q", types["lucas42/lucos_photos"])
	}
	if types["lucas42/lucos_navbar"] != RepoTypeComponent {
		t.Errorf("expected lucos_navbar to be component, got %q", types["lucas42/lucos_navbar"])
	}
	// A repo not in configy should be absent from the map.
	if _, ok := types["lucas42/lucos_unknown"]; ok {
		t.Error("expected lucos_unknown to be absent from types map")
	}
}

// TestFetchRepoTypes_SystemTakesPrecedenceOverComponent verifies that a repo
// listed as both a system and a component is classified as system.
func TestFetchRepoTypes_SystemTakesPrecedenceOverComponent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/systems":
			json.NewEncoder(w).Encode([]configySystem{{ID: "lucos_shared"}})
		case "/components":
			json.NewEncoder(w).Encode([]configyComponent{{ID: "lucos_shared"}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	s := &AuditSweeper{configyBaseURL: server.URL, githubOrg: "lucas42"}
	types, err := s.fetchRepoTypes()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if types["lucas42/lucos_shared"] != RepoTypeSystem {
		t.Errorf("expected lucos_shared to be system (not component), got %q", types["lucas42/lucos_shared"])
	}
}

// TestFetchRepos_SinglePage verifies basic repo fetching without pagination.
func TestFetchRepos_SinglePage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/orgs/lucas42/repos" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]gitHubRepo{
				{FullName: "lucas42/lucos_photos"},
				{FullName: "lucas42/lucos_notes"},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := &AuditSweeper{githubAPIBaseURL: server.URL, githubOrg: "lucas42"}
	repos, err := s.fetchRepos("fake-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(repos))
	}
	if repos[0] != "lucas42/lucos_photos" {
		t.Errorf("expected first repo 'lucas42/lucos_photos', got %q", repos[0])
	}
}

// TestFetchRepos_Pagination verifies that the sweeper follows pagination to
// fetch all repos when a single page isn't enough.
func TestFetchRepos_Pagination(t *testing.T) {
	// Serve exactly 100 repos on page 1 (triggering a second request) and 3 on page 2.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/orgs/lucas42/repos" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		page := r.URL.Query().Get("page")
		w.Header().Set("Content-Type", "application/json")
		if page == "1" || page == "" {
			repos := make([]gitHubRepo, 100)
			for i := range repos {
				repos[i] = gitHubRepo{FullName: "lucas42/repo" + string(rune('a'+i%26))}
			}
			json.NewEncoder(w).Encode(repos)
		} else {
			json.NewEncoder(w).Encode([]gitHubRepo{
				{FullName: "lucas42/extra_repo1"},
				{FullName: "lucas42/extra_repo2"},
				{FullName: "lucas42/extra_repo3"},
			})
		}
	}))
	defer server.Close()

	s := &AuditSweeper{githubAPIBaseURL: server.URL, githubOrg: "lucas42"}
	repos, err := s.fetchRepos("fake-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repos) != 103 {
		t.Errorf("expected 103 repos (100 + 3), got %d", len(repos))
	}
}

// TestAppliesToType_NoAppliesTo verifies a convention with no AppliesTo applies to all types.
func TestAppliesToType_NoAppliesTo(t *testing.T) {
	c := Convention{ID: "any-convention"}
	for _, rt := range []RepoType{RepoTypeSystem, RepoTypeComponent, RepoTypeUnconfigured} {
		if !c.AppliesToType(rt) {
			t.Errorf("expected convention with no AppliesTo to apply to %q, got false", rt)
		}
	}
}

// TestAppliesToType_Restricted verifies a convention with AppliesTo only matches declared types.
func TestAppliesToType_Restricted(t *testing.T) {
	c := Convention{
		ID:        "systems-only",
		AppliesTo: []RepoType{RepoTypeSystem},
	}
	if !c.AppliesToType(RepoTypeSystem) {
		t.Error("expected convention to apply to system repos")
	}
	if c.AppliesToType(RepoTypeComponent) {
		t.Error("expected convention NOT to apply to component repos")
	}
	if c.AppliesToType(RepoTypeUnconfigured) {
		t.Error("expected convention NOT to apply to unconfigured repos")
	}
}

// TestSweep_StoresFindings verifies that a full sweep stores findings in the DB.
func TestSweep_StoresFindings(t *testing.T) {
	// Fake GitHub API: one repo, and the file exists for the circleci convention.
	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/orgs/lucas42/repos":
			json.NewEncoder(w).Encode([]gitHubRepo{
				{FullName: "lucas42/lucos_photos"},
			})
		case "/repos/lucas42/lucos_photos/contents/.circleci/config.yml":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"type":"file"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer githubServer.Close()

	// Fake configy: lucos_photos is a system.
	configyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/systems":
			json.NewEncoder(w).Encode([]configySystem{{ID: "lucos_photos"}})
		case "/components":
			json.NewEncoder(w).Encode([]configyComponent{})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer configyServer.Close()

	s := newTestSweeper(t, configyServer, githubServer)

	// inject a mock githubAuth that returns a fake token
	s.githubAuth = &GitHubAuthClient{cachedToken: "fake-token", tokenExpires: time.Now().Add(1 * time.Hour)}

	if err := s.sweep(); err != nil {
		t.Fatalf("sweep() returned error: %v", err)
	}

	findings, err := s.db.GetFindings()
	if err != nil {
		t.Fatalf("GetFindings failed: %v", err)
	}
	if len(findings) == 0 {
		t.Fatal("expected at least one finding after sweep, got none")
	}

	// Verify the circleci convention passed for lucos_photos.
	found := false
	for _, f := range findings {
		if f.Repo == "lucas42/lucos_photos" && f.Convention == "has-circleci-config" {
			found = true
			if !f.Pass {
				t.Errorf("expected has-circleci-config to pass for lucos_photos, got fail")
			}
			break
		}
	}
	if !found {
		t.Error("no finding for has-circleci-config on lucos_photos")
	}
}

// TestSweeper_Status_BeforeFirstSweep verifies the zero status before any sweep.
func TestSweeper_Status_BeforeFirstSweep(t *testing.T) {
	s := &AuditSweeper{}
	completedAt, lastErr := s.Status()
	if !completedAt.IsZero() {
		t.Errorf("expected zero completedAt before first sweep, got %v", completedAt)
	}
	if lastErr != nil {
		t.Errorf("expected nil lastErr before first sweep, got %v", lastErr)
	}
}
