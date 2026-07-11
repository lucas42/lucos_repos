package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"lucos_repos/conventions"
)

// fakeGitHubAuth returns a GitHubAuthClient with a pre-cached fake token,
// bypassing actual GitHub App authentication.
func fakeGitHubAuth(t *testing.T) *GitHubAuthClient {
	t.Helper()
	return &GitHubAuthClient{
		cachedToken:  "fake-token",
		tokenExpires: time.Now().Add(1 * time.Hour),
	}
}


func TestRerunHandler_MissingParams(t *testing.T) {
	db := openTestDB(t)
	handler := newRerunHandler(db, fakeGitHubAuth(t), "", "")

	req := httptest.NewRequest("POST", "/api/rerun", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when no params given, got %d", w.Code)
	}
}

func TestRerunHandler_UnknownConvention(t *testing.T) {
	db := openTestDB(t)
	handler := newRerunHandler(db, fakeGitHubAuth(t), "", "")

	req := httptest.NewRequest("POST", "/api/rerun?convention=no-such-convention", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown convention, got %d", w.Code)
	}
}

func TestRerunHandler_UnknownRepo(t *testing.T) {
	db := openTestDB(t)
	handler := newRerunHandler(db, fakeGitHubAuth(t), "", "")

	req := httptest.NewRequest("POST", "/api/rerun?repo=lucas42/no_such_repo", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown repo, got %d", w.Code)
	}
}

// TestRerunHandler_RepoAndConvention verifies that re-running a single
// convention for a single repo returns the correct result and updates the DB.
func TestRerunHandler_RepoAndConvention(t *testing.T) {
	db := openTestDB(t)
	db.UpsertRepo("lucas42/test_repo", "system", false)

	// Seed a stale failing finding so we can verify the DB is updated.
	db.UpsertConvention("allow-auto-merge", "Allow auto-merge")
	db.SaveFinding(conventions.ConventionResult{
		Convention: "allow-auto-merge",
		Pass:       false,
		Detail:     "stale: auto-merge not allowed",
	}, "lucas42/test_repo", "https://github.com/lucas42/test_repo/issues/1")

	// Fake GitHub API that reports auto-merge as allowed (GraphQL) and
	// returns 404 for all file content requests.
	ghServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/graphql" {
			w.Write([]byte(`{"data":{"repository":{"autoMergeAllowed":true}}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ghServer.Close()

	// Fake configy that returns no systems (configy errors are non-fatal).
	configyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer configyServer.Close()

	handler := newRerunHandler(db, fakeGitHubAuth(t), ghServer.URL, configyServer.URL)

	req := httptest.NewRequest("POST", "/api/rerun?repo=lucas42/test_repo&convention=allow-auto-merge", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var results []rerunRepoResult
	if err := json.NewDecoder(w.Body).Decode(&results); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	res := results[0]
	if res.Repo != "lucas42/test_repo" {
		t.Errorf("unexpected repo %q", res.Repo)
	}
	check, ok := res.Checks["allow-auto-merge"]
	if !ok {
		t.Fatal("expected allow-auto-merge in checks")
	}
	if check.Status != "pass" {
		t.Errorf("expected pass, got %q (detail: %q)", check.Status, check.Detail)
	}

	// Verify the DB was updated.
	report, err := db.GetStatusReport()
	if err != nil {
		t.Fatalf("failed to get report: %v", err)
	}
	if cs, ok := report.Repos["lucas42/test_repo"].Conventions["allow-auto-merge"]; !ok {
		t.Error("finding not in DB after rerun")
	} else if !cs.Pass {
		t.Errorf("expected DB finding to be pass, got fail (detail: %q)", cs.Detail)
	}
}

// TestRerunHandler_PreservesIssueURL verifies that when a re-run result is
// still failing, the existing issue URL is preserved in the response and DB.
func TestRerunHandler_PreservesIssueURL(t *testing.T) {
	db := openTestDB(t)
	db.UpsertRepo("lucas42/test_repo", "system", false)
	db.UpsertConvention("allow-auto-merge", "Allow auto-merge")
	db.SaveFinding(conventions.ConventionResult{
		Convention: "allow-auto-merge",
		Pass:       false,
		Detail:     "auto-merge not allowed",
	}, "lucas42/test_repo", "https://github.com/lucas42/test_repo/issues/42")

	// Fake GitHub API that reports auto-merge as NOT allowed.
	ghServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/graphql" {
			w.Write([]byte(`{"data":{"repository":{"autoMergeAllowed":false}}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ghServer.Close()

	configyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer configyServer.Close()

	handler := newRerunHandler(db, fakeGitHubAuth(t), ghServer.URL, configyServer.URL)

	req := httptest.NewRequest("POST", "/api/rerun?repo=lucas42/test_repo&convention=allow-auto-merge", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var results []rerunRepoResult
	if err := json.NewDecoder(w.Body).Decode(&results); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	check := results[0].Checks["allow-auto-merge"]
	if check.Status != "fail" {
		t.Errorf("expected fail, got %q", check.Status)
	}
	if check.Issue != "https://github.com/lucas42/test_repo/issues/42" {
		t.Errorf("expected issue URL to be preserved, got %q", check.Issue)
	}

	// Confirm issue URL preserved in DB too.
	report, _ := db.GetStatusReport()
	if cs := report.Repos["lucas42/test_repo"].Conventions["allow-auto-merge"]; cs.IssueURL != "https://github.com/lucas42/test_repo/issues/42" {
		t.Errorf("expected issue URL preserved in DB, got %q", cs.IssueURL)
	}
}

// TestRerunHandler_ConventionOnlyScope verifies that specifying only a
// convention re-runs it across all repos that have it in scope.
func TestRerunHandler_ConventionOnlyScope(t *testing.T) {
	db := openTestDB(t)
	db.UpsertRepo("lucas42/repo_a", "system", false)
	db.UpsertRepo("lucas42/repo_b", "component", false)
	db.UpsertConvention("allow-auto-merge", "Allow auto-merge")
	db.SaveFinding(conventions.ConventionResult{Convention: "allow-auto-merge", Pass: true, Detail: "ok"}, "lucas42/repo_a", "")
	db.SaveFinding(conventions.ConventionResult{Convention: "allow-auto-merge", Pass: true, Detail: "ok"}, "lucas42/repo_b", "")

	ghServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/graphql" {
			w.Write([]byte(`{"data":{"repository":{"autoMergeAllowed":true}}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ghServer.Close()

	configyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer configyServer.Close()

	handler := newRerunHandler(db, fakeGitHubAuth(t), ghServer.URL, configyServer.URL)

	req := httptest.NewRequest("POST", "/api/rerun?convention=allow-auto-merge", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var results []rerunRepoResult
	if err := json.NewDecoder(w.Body).Decode(&results); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	// Both system and component repos have allow-auto-merge in scope.
	if len(results) != 2 {
		t.Errorf("expected 2 results (one per repo), got %d", len(results))
	}
}

// TestRerunHandler_RepoOnly verifies that specifying only a repo re-runs
// all applicable conventions for it.
func TestRerunHandler_RepoOnly(t *testing.T) {
	db := openTestDB(t)
	db.UpsertRepo("lucas42/test_repo", "system", false)
	db.UpsertConvention("allow-auto-merge", "Allow auto-merge")
	db.SaveFinding(conventions.ConventionResult{Convention: "allow-auto-merge", Pass: true, Detail: "ok"}, "lucas42/test_repo", "")

	ghServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/graphql" {
			w.Write([]byte(`{"data":{"repository":{"autoMergeAllowed":true}}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ghServer.Close()

	configyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer configyServer.Close()

	handler := newRerunHandler(db, fakeGitHubAuth(t), ghServer.URL, configyServer.URL)

	req := httptest.NewRequest("POST", "/api/rerun?repo=lucas42/test_repo", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var results []rerunRepoResult
	if err := json.NewDecoder(w.Body).Decode(&results); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Repo != "lucas42/test_repo" {
		t.Errorf("unexpected repo %q", results[0].Repo)
	}
	// Should have at least one check (allow-auto-merge applies to system repos).
	if len(results[0].Checks) == 0 {
		t.Error("expected at least one check in result")
	}
}

// TestRerunHandler_UsesLiveTypeNotCachedType verifies that RepoContext.Type
// (and the AppliesToType gate) comes from a fresh configy fetch, not the
// DB-cached value from the last full sweep (#453). The repo is cached as
// "component" but configy now reports it as a system — the rerun should pick
// up "system": the response's RepoType reflects it, and a system-only
// convention (container-naming) becomes selectable.
func TestRerunHandler_UsesLiveTypeNotCachedType(t *testing.T) {
	db := openTestDB(t)
	// Cached (stale) Type from the last sweep says "component".
	db.UpsertRepo("lucas42/test_repo", "component", false)
	// A repo only appears in GetStatusReport().Repos once it has at least one
	// finding — seed an unrelated one so the repo/convention filters resolve.
	db.UpsertConvention("in-lucos-configy", "In lucos configy")
	db.SaveFinding(conventions.ConventionResult{Convention: "in-lucos-configy", Pass: true, Detail: "ok"}, "lucas42/test_repo", "")

	ghServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// docker-compose.yml (and everything else) 404s — container-naming
		// treats a missing compose file as a pass, which is enough to prove
		// it was selected at all.
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ghServer.Close()

	// Live configy now classifies test_repo as a system.
	configyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/systems":
			json.NewEncoder(w).Encode([]configySystem{{ID: "test_repo", Hosts: []string{"avalon"}}})
		default:
			json.NewEncoder(w).Encode([]struct{}{})
		}
	}))
	defer configyServer.Close()

	handler := newRerunHandler(db, fakeGitHubAuth(t), ghServer.URL, configyServer.URL)

	req := httptest.NewRequest("POST", "/api/rerun?repo=lucas42/test_repo&convention=container-naming", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var results []rerunRepoResult
	if err := json.NewDecoder(w.Body).Decode(&results); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result (container-naming applies once Type=system is picked up live), got %d", len(results))
	}
	if results[0].RepoType != "system" {
		t.Errorf("expected live-refreshed RepoType %q, got %q (cached DB value was 'component')", "system", results[0].RepoType)
	}
	if _, ok := results[0].Checks["container-naming"]; !ok {
		t.Error("expected container-naming (system-only convention) to be selected using the live Type")
	}
}

// TestRerunHandler_FallsBackToCachedTypeOnConfigyError verifies that when the
// live configy fetch fails entirely, the rerun degrades to the DB-cached
// Type rather than failing the whole request (#453).
func TestRerunHandler_FallsBackToCachedTypeOnConfigyError(t *testing.T) {
	db := openTestDB(t)
	db.UpsertRepo("lucas42/test_repo", "system", false)
	db.UpsertConvention("allow-auto-merge", "Allow auto-merge")
	db.SaveFinding(conventions.ConventionResult{Convention: "allow-auto-merge", Pass: true, Detail: "ok"}, "lucas42/test_repo", "")

	ghServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/graphql" {
			w.Write([]byte(`{"data":{"repository":{"autoMergeAllowed":true}}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ghServer.Close()

	// configy /systems returns a server error (not just 404-not-found —
	// genuinely unreachable/broken), so fetchRepoTypesFrom fails outright.
	configyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer configyServer.Close()

	handler := newRerunHandler(db, fakeGitHubAuth(t), ghServer.URL, configyServer.URL)

	req := httptest.NewRequest("POST", "/api/rerun?repo=lucas42/test_repo&convention=allow-auto-merge", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (degraded, not failed) even when configy is unreachable, got %d: %s", w.Code, w.Body.String())
	}

	var results []rerunRepoResult
	if err := json.NewDecoder(w.Body).Decode(&results); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].RepoType != "system" {
		t.Errorf("expected fallback to cached RepoType %q, got %q", "system", results[0].RepoType)
	}
}
