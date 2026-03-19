package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"lucos_repos/conventions"
)

// newTestSweeper creates an AuditSweeper wired to a temporary DB with its
// base URLs pointing at fake test servers. The githubAuth field is nil — tests
// that exercise sweep() must inject a token some other way.
// The issueClientFactory defaults to using the githubServer URL, so the same
// fake server handles both convention-check and issue-management calls.
func newTestSweeper(t *testing.T, configyServer, githubServer *httptest.Server) *AuditSweeper {
	t.Helper()
	db := openTestDB(t)
	// Pre-populate the conventions table so SaveFinding doesn't hit FK errors.
	for _, c := range conventions.All() {
		if err := db.UpsertConvention(c.ID, c.Description); err != nil {
			t.Fatalf("failed to upsert convention %s: %v", c.ID, err)
		}
	}
	s := &AuditSweeper{
		db:               db,
		githubOrg:        "lucas42",
		sweepInterval:    6 * time.Hour,
		system:           "lucos_repos",
		configyBaseURL:   configyServer.URL,
		githubAPIBaseURL: githubServer.URL,
	}
	s.issueClientFactory = func(token string) *GitHubIssueClient {
		return NewGitHubIssueClient(githubServer.URL, token)
	}
	return s
}

// TestFetchConfigySystems_Success verifies that systems are parsed correctly,
// including the hosts field.
func TestFetchConfigySystems_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/systems" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]configySystem{
				{ID: "lucos_photos", Hosts: []string{"avalon"}},
				{ID: "lucos_media_linuxplayer", Hosts: []string{"xwing", "salvare"}},
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
	if len(systems[0].Hosts) != 1 || systems[0].Hosts[0] != "avalon" {
		t.Errorf("expected lucos_photos to have hosts [avalon], got %v", systems[0].Hosts)
	}
	if systems[1].ID != "lucos_media_linuxplayer" {
		t.Errorf("expected second system 'lucos_media_linuxplayer', got %q", systems[1].ID)
	}
	if len(systems[1].Hosts) != 2 {
		t.Errorf("expected lucos_media_linuxplayer to have 2 hosts, got %v", systems[1].Hosts)
	}
}

// TestFetchRepoTypes_SystemHostsPopulated verifies that hosts are propagated
// from configy into the repoInfo for system repos.
func TestFetchRepoTypes_SystemHostsPopulated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/systems":
			json.NewEncoder(w).Encode([]configySystem{
				{ID: "lucos_router", Hosts: []string{"avalon", "xwing"}},
			})
		case "/components":
			json.NewEncoder(w).Encode([]configyComponent{})
		case "/scripts":
			json.NewEncoder(w).Encode([]configyScript{})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	s := &AuditSweeper{configyBaseURL: server.URL, githubOrg: "lucas42"}
	infos, err := s.fetchRepoTypes()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	info, ok := infos["lucas42/lucos_router"]
	if !ok {
		t.Fatal("expected lucos_router to be present in infos map")
	}
	if info.Type != conventions.RepoTypeSystem {
		t.Errorf("expected lucos_router to be system, got %q", info.Type)
	}
	if len(info.Hosts) != 2 || info.Hosts[0] != "avalon" || info.Hosts[1] != "xwing" {
		t.Errorf("expected hosts [avalon xwing], got %v", info.Hosts)
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

// TestFetchConfigyScripts_Success verifies that scripts are parsed correctly.
func TestFetchConfigyScripts_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/scripts" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]configyScript{
				{ID: "lucos_agent"},
				{ID: ".dotfiles"},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := &AuditSweeper{configyBaseURL: server.URL}
	scripts, err := s.fetchConfigyScripts()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scripts) != 2 {
		t.Fatalf("expected 2 scripts, got %d", len(scripts))
	}
	if scripts[0].ID != "lucos_agent" {
		t.Errorf("expected first script 'lucos_agent', got %q", scripts[0].ID)
	}
}

// TestFetchConfigyScripts_HTTPError verifies that a non-200 response returns an error.
func TestFetchConfigyScripts_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	s := &AuditSweeper{configyBaseURL: server.URL}
	_, err := s.fetchConfigyScripts()
	if err == nil {
		t.Error("expected error for 500 response, got nil")
	}
}

// TestFetchRepoTypes_ClassifiesCorrectly verifies systems, components, scripts, and
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
		case "/scripts":
			json.NewEncoder(w).Encode([]configyScript{
				{ID: "lucos_agent"},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	s := &AuditSweeper{configyBaseURL: server.URL, githubOrg: "lucas42"}
	infos, err := s.fetchRepoTypes()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if infos["lucas42/lucos_photos"].Type != conventions.RepoTypeSystem {
		t.Errorf("expected lucos_photos to be system, got %q", infos["lucas42/lucos_photos"].Type)
	}
	if infos["lucas42/lucos_navbar"].Type != conventions.RepoTypeComponent {
		t.Errorf("expected lucos_navbar to be component, got %q", infos["lucas42/lucos_navbar"].Type)
	}
	if infos["lucas42/lucos_agent"].Type != conventions.RepoTypeScript {
		t.Errorf("expected lucos_agent to be script, got %q", infos["lucas42/lucos_agent"].Type)
	}
	// A repo not in configy should be absent from the map.
	if _, ok := infos["lucas42/lucos_unknown"]; ok {
		t.Error("expected lucos_unknown to be absent from types map")
	}
}

// TestFetchRepoTypes_DuplicateSystemAndComponent verifies that a repo listed
// as both a system and a component is classified as duplicate.
func TestFetchRepoTypes_DuplicateSystemAndComponent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/systems":
			json.NewEncoder(w).Encode([]configySystem{{ID: "lucos_shared"}})
		case "/components":
			json.NewEncoder(w).Encode([]configyComponent{{ID: "lucos_shared"}})
		case "/scripts":
			json.NewEncoder(w).Encode([]configyScript{})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	s := &AuditSweeper{configyBaseURL: server.URL, githubOrg: "lucas42"}
	infos, err := s.fetchRepoTypes()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if infos["lucas42/lucos_shared"].Type != conventions.RepoTypeDuplicate {
		t.Errorf("expected lucos_shared to be duplicate (listed in both systems and components), got %q", infos["lucas42/lucos_shared"].Type)
	}
}

// TestFetchRepoTypes_DuplicateSystemAndScript verifies that a repo listed as
// both a system and a script is classified as duplicate.
func TestFetchRepoTypes_DuplicateSystemAndScript(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/systems":
			json.NewEncoder(w).Encode([]configySystem{{ID: "lucos_shared"}})
		case "/components":
			json.NewEncoder(w).Encode([]configyComponent{})
		case "/scripts":
			json.NewEncoder(w).Encode([]configyScript{{ID: "lucos_shared"}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	s := &AuditSweeper{configyBaseURL: server.URL, githubOrg: "lucas42"}
	infos, err := s.fetchRepoTypes()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if infos["lucas42/lucos_shared"].Type != conventions.RepoTypeDuplicate {
		t.Errorf("expected lucos_shared to be duplicate (listed in both systems and scripts), got %q", infos["lucas42/lucos_shared"].Type)
	}
}

// TestFetchRepoTypes_DuplicateComponentAndScript verifies that a repo listed
// as both a component and a script is classified as duplicate.
func TestFetchRepoTypes_DuplicateComponentAndScript(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/systems":
			json.NewEncoder(w).Encode([]configySystem{})
		case "/components":
			json.NewEncoder(w).Encode([]configyComponent{{ID: "lucos_shared"}})
		case "/scripts":
			json.NewEncoder(w).Encode([]configyScript{{ID: "lucos_shared"}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	s := &AuditSweeper{configyBaseURL: server.URL, githubOrg: "lucas42"}
	infos, err := s.fetchRepoTypes()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if infos["lucas42/lucos_shared"].Type != conventions.RepoTypeDuplicate {
		t.Errorf("expected lucos_shared to be duplicate (listed in both components and scripts), got %q", infos["lucas42/lucos_shared"].Type)
	}
}

// TestFetchRepos_SinglePage verifies basic repo fetching without pagination.
func TestFetchRepos_SinglePage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users/lucas42/repos" {
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
	if repos[0].FullName != "lucas42/lucos_photos" {
		t.Errorf("expected first repo 'lucas42/lucos_photos', got %q", repos[0].FullName)
	}
}

// TestFetchRepos_Pagination verifies that the sweeper follows pagination to
// fetch all repos when a single page isn't enough.
func TestFetchRepos_Pagination(t *testing.T) {
	// Serve exactly 100 repos on page 1 (triggering a second request) and 3 on page 2.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/lucas42/repos" {
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
	c := conventions.Convention{ID: "any-convention"}
	for _, rt := range []conventions.RepoType{conventions.RepoTypeSystem, conventions.RepoTypeComponent, conventions.RepoTypeScript, conventions.RepoTypeUnconfigured, conventions.RepoTypeDuplicate} {
		if !c.AppliesToType(rt) {
			t.Errorf("expected convention with no AppliesTo to apply to %q, got false", rt)
		}
	}
}

// TestAppliesToType_Restricted verifies a convention with AppliesTo only matches declared types.
func TestAppliesToType_Restricted(t *testing.T) {
	c := conventions.Convention{
		ID:        "systems-only",
		AppliesTo: []conventions.RepoType{conventions.RepoTypeSystem},
	}
	if !c.AppliesToType(conventions.RepoTypeSystem) {
		t.Error("expected convention to apply to system repos")
	}
	if c.AppliesToType(conventions.RepoTypeComponent) {
		t.Error("expected convention NOT to apply to component repos")
	}
	if c.AppliesToType(conventions.RepoTypeUnconfigured) {
		t.Error("expected convention NOT to apply to unconfigured repos")
	}
}

// TestAppliesToRepo_NoExclusions verifies a convention with no ExcludeRepos
// applies to every repo.
func TestAppliesToRepo_NoExclusions(t *testing.T) {
	c := conventions.Convention{ID: "any-convention"}
	for _, repo := range []string{"lucas42/lucos_photos", "lucas42/lucos_deploy_orb", "lucas42/lucos_repos"} {
		if !c.AppliesToRepo(repo) {
			t.Errorf("expected convention with no ExcludeRepos to apply to %q, got false", repo)
		}
	}
}

// TestAppliesToRepo_ExcludedRepo verifies a convention with ExcludeRepos does
// not apply to a listed repo but still applies to others.
func TestAppliesToRepo_ExcludedRepo(t *testing.T) {
	c := conventions.Convention{
		ID:           "some-convention",
		ExcludeRepos: []string{"lucas42/lucos_deploy_orb"},
	}
	if c.AppliesToRepo("lucas42/lucos_deploy_orb") {
		t.Error("expected convention NOT to apply to the excluded repo")
	}
	if !c.AppliesToRepo("lucas42/lucos_photos") {
		t.Error("expected convention to apply to non-excluded repo")
	}
}

// minimalValidCIConfig is a base64-encoded minimal CircleCI config that satisfies
// all circleci-* conventions for a system with no configured hosts. It declares the
// lucos deploy orb and includes a build job but no deploy jobs (matching a system
// with zero hosts).
const minimalValidCIConfig = `version: 2.1
orbs:
  lucos: lucos/deploy@0
workflows:
  version: 2
  build:
    jobs:
      - lucos/build-amd64
`

// encodedCIConfig returns minimalValidCIConfig as a GitHub Contents API JSON
// response, suitable for use as a fake server response.
func encodedCIConfig() string {
	import64 := base64.StdEncoding.EncodeToString([]byte(minimalValidCIConfig))
	return `{"encoding":"base64","content":"` + import64 + `"}`
}

// TestSweep_StoresFindings verifies that a full sweep stores findings in the DB.
func TestSweep_StoresFindings(t *testing.T) {
	// Fake GitHub API: one repo, with a valid circleci config file.
	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/users/lucas42/repos":
			json.NewEncoder(w).Encode([]gitHubRepo{
				{FullName: "lucas42/lucos_photos"},
			})
		case "/repos/lucas42/lucos_photos/contents/.circleci/config.yml":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(encodedCIConfig()))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer githubServer.Close()

	// Fake configy: lucos_photos is a system with no configured hosts.
	configyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/systems":
			json.NewEncoder(w).Encode([]configySystem{{ID: "lucos_photos", Hosts: []string{}}})
		case "/components":
			json.NewEncoder(w).Encode([]configyComponent{})
		case "/scripts":
			json.NewEncoder(w).Encode([]configyScript{})
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

	// Verify the circleci-config-exists convention passed for lucos_photos.
	found := false
	for _, f := range findings {
		if f.Repo == "lucas42/lucos_photos" && f.Convention == "circleci-config-exists" {
			found = true
			if !f.Pass {
				t.Errorf("expected circleci-config-exists to pass for lucos_photos, got fail")
			}
			break
		}
	}
	if !found {
		t.Error("no finding for circleci-config-exists on lucos_photos")
	}
}

// TestSweep_FailingConventionCreatesIssue verifies that when a convention fails,
// the sweep creates an issue and stores its URL in the findings table.
func TestSweep_FailingConventionCreatesIssue(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	const issueURL = "https://github.com/lucas42/lucos_missing/issues/1"
	// The circleci-config-exists convention will fail since the file is absent.
	issueCreated := false

	// Fake GitHub API: one repo with NO circleci config file, plus issue search/create endpoints.
	// Multiple conventions will fail (circleci-config-exists, circleci-uses-lucos-orb,
	// circleci-system-deploy-jobs), so multiple issue creation calls may occur. We
	// track that at least one was made.
	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/users/lucas42/repos":
			json.NewEncoder(w).Encode([]gitHubRepo{
				{FullName: "lucas42/lucos_missing"},
			})
		case r.URL.Path == "/repos/lucas42/lucos_missing/contents/.circleci/config.yml":
			// File does not exist — conventions fail.
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"Not Found"}`))
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/repos/lucas42/lucos_missing/issues"):
			// No existing issues.
			json.NewEncoder(w).Encode([]gitHubIssue{})
		case r.Method == "POST" && r.URL.Path == "/repos/lucas42/lucos_missing/issues":
			issueCreated = true
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(gitHubIssue{
				Number:  1,
				HTMLURL: issueURL,
				State:   "open",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer githubServer.Close()

	// Fake configy: lucos_missing is a system with no configured hosts.
	configyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/systems":
			json.NewEncoder(w).Encode([]configySystem{{ID: "lucos_missing", Hosts: []string{}}})
		case "/components":
			json.NewEncoder(w).Encode([]configyComponent{})
		case "/scripts":
			json.NewEncoder(w).Encode([]configyScript{})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer configyServer.Close()

	s := newTestSweeper(t, configyServer, githubServer)
	s.githubAuth = &GitHubAuthClient{cachedToken: "fake-token", tokenExpires: time.Now().Add(1 * time.Hour)}

	if err := s.sweep(); err != nil {
		t.Fatalf("sweep() returned error: %v", err)
	}

	if !issueCreated {
		t.Error("expected at least one issue to be created for failing conventions, but POST was never called")
	}

	findings, err := s.db.GetFindings()
	if err != nil {
		t.Fatalf("GetFindings failed: %v", err)
	}

	var found bool
	for _, f := range findings {
		if f.Repo == "lucas42/lucos_missing" && f.Convention == "circleci-config-exists" {
			found = true
			if f.Pass {
				t.Error("expected circleci-config-exists finding to fail for lucos_missing")
			}
			if f.IssueURL == "" {
				t.Error("expected IssueURL to be set for failing convention")
			}
			break
		}
	}
	if !found {
		t.Error("no finding for circleci-config-exists on lucos_missing")
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

// TestReportToScheduleTracker_Success verifies a successful sweep posts to the
// schedule tracker with status "success" and no message.
func TestReportToScheduleTracker_Success(t *testing.T) {
	var received scheduleTrackerPayload
	trackerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/report-status" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("failed to decode schedule tracker payload: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer trackerServer.Close()

	s := &AuditSweeper{
		system:                  "lucos_repos",
		sweepInterval:           6 * time.Hour,
		scheduleTrackerEndpoint: trackerServer.URL + "/report-status",
	}
	s.reportToScheduleTracker("success", "")

	if received.System != "lucos_repos" {
		t.Errorf("expected system %q, got %q", "lucos_repos", received.System)
	}
	if received.Frequency != int((6 * time.Hour).Seconds()) {
		t.Errorf("expected frequency %d, got %d", int((6*time.Hour).Seconds()), received.Frequency)
	}
	if received.Status != "success" {
		t.Errorf("expected status %q, got %q", "success", received.Status)
	}
	if received.Message != "" {
		t.Errorf("expected empty message for success, got %q", received.Message)
	}
}

// TestReportToScheduleTracker_Error verifies a failed sweep posts to the
// schedule tracker with status "error" and the error message.
func TestReportToScheduleTracker_Error(t *testing.T) {
	var received scheduleTrackerPayload
	trackerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/report-status" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("failed to decode schedule tracker payload: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer trackerServer.Close()

	s := &AuditSweeper{
		system:                  "lucos_repos",
		sweepInterval:           6 * time.Hour,
		scheduleTrackerEndpoint: trackerServer.URL + "/report-status",
	}
	s.reportToScheduleTracker("error", "failed to get GitHub token: some auth error")

	if received.Status != "error" {
		t.Errorf("expected status %q, got %q", "error", received.Status)
	}
	if received.Message != "failed to get GitHub token: some auth error" {
		t.Errorf("expected error message, got %q", received.Message)
	}
}

// TestReportToScheduleTracker_NoEndpoint verifies that no HTTP call is made when
// scheduleTrackerEndpoint is empty.
func TestReportToScheduleTracker_NoEndpoint(t *testing.T) {
	// If any HTTP call were made, it would fail on a non-existent host.
	s := &AuditSweeper{
		system:        "lucos_repos",
		sweepInterval: 6 * time.Hour,
		// scheduleTrackerEndpoint intentionally left empty
	}
	// Should not panic or make any network call.
	s.reportToScheduleTracker("success", "")
}

// TestSweep_IssueAPIErrorReturnsError verifies that when EnsureIssueExists
// fails for one or more convention checks (e.g. due to rate limiting), sweep()
// returns a non-nil error rather than silently reporting success.
func TestSweep_IssueAPIErrorReturnsError(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	// Fake GitHub API: one repo with NO circleci config file (so conventions
	// fail and the issue API is invoked), plus an issue list endpoint that
	// returns 403 to simulate rate limiting.
	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/users/lucas42/repos":
			json.NewEncoder(w).Encode([]gitHubRepo{
				{FullName: "lucas42/lucos_failing"},
			})
		case r.URL.Path == "/repos/lucas42/lucos_failing/contents/.circleci/config.yml":
			// File absent — all circleci conventions fail.
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"Not Found"}`))
		case strings.HasPrefix(r.URL.Path, "/repos/lucas42/lucos_failing/issues"):
			// Simulate rate limiting on the issue API.
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"message":"API rate limit exceeded"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer githubServer.Close()

	// Fake configy: lucos_failing is a system with no configured hosts.
	configyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/systems":
			json.NewEncoder(w).Encode([]configySystem{{ID: "lucos_failing", Hosts: []string{}}})
		case "/components":
			json.NewEncoder(w).Encode([]configyComponent{})
		case "/scripts":
			json.NewEncoder(w).Encode([]configyScript{})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer configyServer.Close()

	s := newTestSweeper(t, configyServer, githubServer)
	s.githubAuth = &GitHubAuthClient{cachedToken: "fake-token", tokenExpires: time.Now().Add(1 * time.Hour)}

	err := s.sweep()
	if err == nil {
		t.Fatal("expected sweep() to return an error when issue API calls fail, got nil")
	}
	if !strings.Contains(err.Error(), "skipped") {
		t.Errorf("expected error message to mention skipped checks, got: %q", err.Error())
	}
}

// TestSweep_ConventionAPIErrorSkipsIssueCreation verifies that when a convention
// check returns Err != nil (i.e. a transient GitHub API error prevented the
// check from running), the sweep does NOT create a false-positive issue and
// instead marks the sweep as incomplete (returns a non-nil error).
func TestSweep_ConventionAPIErrorSkipsIssueCreation(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")

	issueCreated := false
	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/users/lucas42/repos":
			json.NewEncoder(w).Encode([]gitHubRepo{
				{FullName: "lucas42/lucos_transient"},
			})
		case strings.HasPrefix(r.URL.Path, "/repos/lucas42/lucos_transient/contents/"),
			strings.HasPrefix(r.URL.Path, "/repos/lucas42/lucos_transient/branches/"):
			// Simulate a transient server error for any file fetch or branch
			// protection check (e.g. .circleci/config.yml, branches/main/protection).
			w.WriteHeader(http.StatusInternalServerError)
		case strings.HasPrefix(r.URL.Path, "/repos/lucas42/lucos_transient/issues"):
			// This endpoint must NOT be called — a transient error is not a convention failure.
			issueCreated = true
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{"number": 1, "html_url": "https://github.com/lucas42/lucos_transient/issues/1"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer githubServer.Close()

	configyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/systems":
			json.NewEncoder(w).Encode([]configySystem{{ID: "lucos_transient", Hosts: []string{}}})
		case "/components":
			json.NewEncoder(w).Encode([]configyComponent{})
		case "/scripts":
			json.NewEncoder(w).Encode([]configyScript{})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer configyServer.Close()

	s := newTestSweeper(t, configyServer, githubServer)
	s.githubAuth = &GitHubAuthClient{cachedToken: "fake-token", tokenExpires: time.Now().Add(1 * time.Hour)}

	err := s.sweep()
	if err == nil {
		t.Error("expected sweep() to return an error (incomplete) when convention checks fail due to API errors, got nil")
	}
	if issueCreated {
		t.Error("expected no issue to be created for a transient API error, but issue endpoint was called")
	}
}

// TestSweep_FullSuccessReturnsNil verifies that sweep() returns nil when all
// repos and conventions are processed without any API errors.
func TestSweep_FullSuccessReturnsNil(t *testing.T) {
	// Fake GitHub API: one repo with a valid circleci config (all conventions pass,
	// no issue API calls needed).
	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/users/lucas42/repos":
			json.NewEncoder(w).Encode([]gitHubRepo{
				{FullName: "lucas42/lucos_clean"},
			})
		case "/repos/lucas42/lucos_clean/contents/.circleci/config.yml":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(encodedCIConfig()))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer githubServer.Close()

	// Fake configy: lucos_clean is a system with no configured hosts.
	configyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/systems":
			json.NewEncoder(w).Encode([]configySystem{{ID: "lucos_clean", Hosts: []string{}}})
		case "/components":
			json.NewEncoder(w).Encode([]configyComponent{})
		case "/scripts":
			json.NewEncoder(w).Encode([]configyScript{})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer configyServer.Close()

	s := newTestSweeper(t, configyServer, githubServer)
	s.githubAuth = &GitHubAuthClient{cachedToken: "fake-token", tokenExpires: time.Now().Add(1 * time.Hour)}

	if err := s.sweep(); err != nil {
		t.Fatalf("expected sweep() to return nil for a full successful sweep, got: %v", err)
	}
}

// TestSweep_ArchivedRepoSkipped verifies that archived repos are excluded from
// convention checks entirely, so no issue creation is attempted and the sweep
// completes successfully.
func TestSweep_ArchivedRepoSkipped(t *testing.T) {
	issueAPICalled := false

	// Fake GitHub API: one archived repo (no CI config, so conventions would
	// fail — but the archived flag should cause it to be skipped entirely).
	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/users/lucas42/repos":
			json.NewEncoder(w).Encode([]gitHubRepo{
				{FullName: "lucas42/old_archived_repo", Archived: true},
			})
		case strings.HasPrefix(r.URL.Path, "/repos/lucas42/old_archived_repo/issues"):
			issueAPICalled = true
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"message":"Repository was archived so is read-only."}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer githubServer.Close()

	configyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/systems":
			json.NewEncoder(w).Encode([]configySystem{})
		case "/components":
			json.NewEncoder(w).Encode([]configyComponent{})
		case "/scripts":
			json.NewEncoder(w).Encode([]configyScript{})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer configyServer.Close()

	s := newTestSweeper(t, configyServer, githubServer)
	s.githubAuth = &GitHubAuthClient{cachedToken: "fake-token", tokenExpires: time.Now().Add(1 * time.Hour)}

	if err := s.sweep(); err != nil {
		t.Fatalf("expected sweep() to return nil when only archived repos present, got: %v", err)
	}
	if issueAPICalled {
		t.Error("expected no issue API calls for archived repos, but the issues endpoint was called")
	}
}

// TestSweep_ForkedRepoSkipped verifies that forked repos are excluded from
// convention checks entirely, so no issue creation is attempted and the sweep
// completes successfully.
func TestSweep_ForkedRepoSkipped(t *testing.T) {
	issueAPICalled := false

	// Fake GitHub API: one forked repo (no CI config, so conventions would
	// fail — but the fork flag should cause it to be skipped entirely).
	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/users/lucas42/repos":
			json.NewEncoder(w).Encode([]gitHubRepo{
				{FullName: "lucas42/some_upstream_fork", Fork: true},
			})
		case strings.HasPrefix(r.URL.Path, "/repos/lucas42/some_upstream_fork/issues"):
			issueAPICalled = true
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer githubServer.Close()

	configyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/systems":
			json.NewEncoder(w).Encode([]configySystem{})
		case "/components":
			json.NewEncoder(w).Encode([]configyComponent{})
		case "/scripts":
			json.NewEncoder(w).Encode([]configyScript{})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer configyServer.Close()

	s := newTestSweeper(t, configyServer, githubServer)
	s.githubAuth = &GitHubAuthClient{cachedToken: "fake-token", tokenExpires: time.Now().Add(1 * time.Hour)}

	if err := s.sweep(); err != nil {
		t.Fatalf("expected sweep() to return nil when only forked repos present, got: %v", err)
	}
	if issueAPICalled {
		t.Error("expected no issue API calls for forked repos, but the issues endpoint was called")
	}
}

// TestSweep_DeletesStaleFindings verifies that a successful sweep removes findings
// for repo+convention pairs that are no longer in scope (e.g. archived/removed repos).
func TestSweep_DeletesStaleFindings(t *testing.T) {
	// Fake GitHub API: only lucos_active exists; lucos_removed is gone.
	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/users/lucas42/repos":
			json.NewEncoder(w).Encode([]gitHubRepo{
				{FullName: "lucas42/lucos_active"},
			})
		case r.URL.Path == "/repos/lucas42/lucos_active/contents/.circleci/config.yml":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(encodedCIConfig()))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer githubServer.Close()

	configyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/systems":
			json.NewEncoder(w).Encode([]configySystem{{ID: "lucos_active", Hosts: []string{}}})
		case "/components":
			json.NewEncoder(w).Encode([]configyComponent{})
		case "/scripts":
			json.NewEncoder(w).Encode([]configyScript{})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer configyServer.Close()

	s := newTestSweeper(t, configyServer, githubServer)
	s.githubAuth = &GitHubAuthClient{cachedToken: "fake-token", tokenExpires: time.Now().Add(1 * time.Hour)}

	// Seed the DB with a stale finding for a repo that won't be swept.
	// Insert the stale repo and a finding directly with a timestamp in the past.
	if _, err := s.db.conn.Exec(
		`INSERT INTO repos (name, last_audited) VALUES (?, ?)`,
		"lucas42/lucos_removed", time.Now().Add(-1*time.Hour),
	); err != nil {
		t.Fatalf("failed to insert stale repo: %v", err)
	}
	staleConvID := conventions.All()[0].ID
	if _, err := s.db.conn.Exec(
		`INSERT INTO findings (repo, convention, pass, detail, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"lucas42/lucos_removed", staleConvID, 1, "old finding", time.Now().Add(-1*time.Hour),
	); err != nil {
		t.Fatalf("failed to insert stale finding: %v", err)
	}

	if err := s.sweep(); err != nil {
		t.Fatalf("sweep() returned error: %v", err)
	}

	findings, err := s.db.GetFindings()
	if err != nil {
		t.Fatalf("GetFindings failed: %v", err)
	}

	// All remaining findings should be for lucos_active only.
	for _, f := range findings {
		if f.Repo == "lucas42/lucos_removed" {
			t.Errorf("stale finding for lucos_removed should have been deleted, but got: %+v", f)
		}
	}
	// And there should be at least one finding for lucos_active.
	found := false
	for _, f := range findings {
		if f.Repo == "lucas42/lucos_active" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one finding for lucos_active after sweep")
	}
}

// TestSweep_DoesNotDeleteFindingsOnIncompleteSweep verifies that stale findings
// are NOT cleaned up when the sweep is incomplete (skippedCount > 0).
func TestSweep_DoesNotDeleteFindingsOnIncompleteSweep(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	// Fake GitHub API: one active repo, but the issues API returns a transient
	// error so the sweep is marked incomplete.
	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/users/lucas42/repos":
			json.NewEncoder(w).Encode([]gitHubRepo{
				{FullName: "lucas42/lucos_active"},
			})
		case r.URL.Path == "/repos/lucas42/lucos_active/contents/.circleci/config.yml":
			// No CI config — convention fails, triggering an issue API call.
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"Not Found"}`))
		case strings.HasPrefix(r.URL.Path, "/repos/lucas42/lucos_active/issues"):
			// Simulate a transient server error (not a 403/410 "unavailable" — a real error).
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"message":"Internal Server Error"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer githubServer.Close()

	configyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/systems":
			json.NewEncoder(w).Encode([]configySystem{{ID: "lucos_active", Hosts: []string{}}})
		case "/components":
			json.NewEncoder(w).Encode([]configyComponent{})
		case "/scripts":
			json.NewEncoder(w).Encode([]configyScript{})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer configyServer.Close()

	s := newTestSweeper(t, configyServer, githubServer)
	s.githubAuth = &GitHubAuthClient{cachedToken: "fake-token", tokenExpires: time.Now().Add(1 * time.Hour)}

	// Seed a stale finding.
	if _, err := s.db.conn.Exec(
		`INSERT INTO repos (name, last_audited) VALUES (?, ?)`,
		"lucas42/lucos_removed", time.Now().Add(-1*time.Hour),
	); err != nil {
		t.Fatalf("failed to insert stale repo: %v", err)
	}
	staleConvID := conventions.All()[0].ID
	if _, err := s.db.conn.Exec(
		`INSERT INTO findings (repo, convention, pass, detail, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"lucas42/lucos_removed", staleConvID, 1, "old finding", time.Now().Add(-1*time.Hour),
	); err != nil {
		t.Fatalf("failed to insert stale finding: %v", err)
	}

	// Sweep should return an error (incomplete).
	if err := s.sweep(); err == nil {
		t.Fatal("expected sweep() to return an error for incomplete sweep, got nil")
	}

	// Stale finding must still be present.
	findings, err := s.db.GetFindings()
	if err != nil {
		t.Fatalf("GetFindings failed: %v", err)
	}

	found := false
	for _, f := range findings {
		if f.Repo == "lucas42/lucos_removed" {
			found = true
			break
		}
	}
	if !found {
		t.Error("stale finding for lucos_removed should NOT have been deleted on an incomplete sweep")
	}
}

// TestSweep_IssuesDisabledTreatedAsSoftFailure verifies that when a repo has
// issues disabled (410 from GitHub), the sweep does not increment the skipped
// count and returns nil (success), rather than treating it as an API error.
// TestSweep_ConfigyFailureAbortsSweptEntirely verifies that when the configy
// fetch fails, the sweep returns an error without creating any findings or issues.
// This prevents false-positive audit issues being raised for all repos.
func TestSweep_ConfigyFailureAbortsSweptEntirely(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")

	issueCreateCalled := false

	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/users/lucas42/repos":
			json.NewEncoder(w).Encode([]gitHubRepo{
				{FullName: "lucas42/lucos_photos"},
			})
		default:
			// Any issue creation call would come through here.
			if strings.Contains(r.URL.Path, "/issues") && r.Method == http.MethodPost {
				issueCreateCalled = true
			}
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer githubServer.Close()

	// Configy server returns 503 — simulating a transient DNS/network failure.
	configyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer configyServer.Close()

	s := newTestSweeper(t, configyServer, githubServer)
	s.githubAuth = &GitHubAuthClient{cachedToken: "fake-token", tokenExpires: time.Now().Add(1 * time.Hour)}

	err := s.sweep()
	if err == nil {
		t.Fatal("expected sweep() to return an error when configy fetch fails, got nil")
	}

	if issueCreateCalled {
		t.Error("expected no issues to be created when configy fetch fails, but issue creation was attempted")
	}

	findings, findErr := s.db.GetFindings()
	if findErr != nil {
		t.Fatalf("GetFindings failed: %v", findErr)
	}
	if len(findings) > 0 {
		t.Errorf("expected no findings when configy fetch fails, got %d", len(findings))
	}
}

func TestSweep_IssuesDisabledTreatedAsSoftFailure(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	// Fake GitHub API: one non-archived repo with no CI config, but the issues
	// API returns 410 (issues disabled).
	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/users/lucas42/repos":
			json.NewEncoder(w).Encode([]gitHubRepo{
				{FullName: "lucas42/no_issues_repo", Archived: false},
			})
		case r.URL.Path == "/repos/lucas42/no_issues_repo/contents/.circleci/config.yml":
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"Not Found"}`))
		case strings.HasPrefix(r.URL.Path, "/repos/lucas42/no_issues_repo/issues"):
			// Issues disabled on this repo.
			w.WriteHeader(http.StatusGone)
			w.Write([]byte(`{"message":"Issues has been disabled in this repository."}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer githubServer.Close()

	configyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/systems":
			json.NewEncoder(w).Encode([]configySystem{{ID: "no_issues_repo", Hosts: []string{}}})
		case "/components":
			json.NewEncoder(w).Encode([]configyComponent{})
		case "/scripts":
			json.NewEncoder(w).Encode([]configyScript{})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer configyServer.Close()

	s := newTestSweeper(t, configyServer, githubServer)
	s.githubAuth = &GitHubAuthClient{cachedToken: "fake-token", tokenExpires: time.Now().Add(1 * time.Hour)}

	if err := s.sweep(); err != nil {
		t.Fatalf("expected sweep() to return nil when issues are disabled (410), got: %v", err)
	}
}
