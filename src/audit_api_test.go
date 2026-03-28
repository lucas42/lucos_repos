package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"lucos_repos/conventions"
)

func TestSingleRepoStatusHandler_NotFound(t *testing.T) {
	db := openTestDB(t)

	handler := newSingleRepoStatusHandler(db)
	req := httptest.NewRequest("GET", "/api/status/lucas42/unknown_repo", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestSingleRepoStatusHandler_Found(t *testing.T) {
	db := openTestDB(t)
	db.UpsertConvention("test-convention", "test")
	db.UpsertRepo("lucas42/test_repo", "system")
	db.SaveFinding(conventions.ConventionResult{
		Convention: "test-convention",
		Pass:       true,
		Detail:     "all good",
	}, "lucas42/test_repo", "")

	handler := newSingleRepoStatusHandler(db)
	req := httptest.NewRequest("GET", "/api/status/lucas42/test_repo", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp singleRepoStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Repo != "lucas42/test_repo" {
		t.Errorf("expected repo 'lucas42/test_repo', got %q", resp.Repo)
	}
	if resp.RepoType != "system" {
		t.Errorf("expected repo_type 'system', got %q", resp.RepoType)
	}
	if check, ok := resp.Checks["test-convention"]; !ok {
		t.Error("expected test-convention in checks")
	} else if check.Status != "pass" {
		t.Errorf("expected check status 'pass', got %q", check.Status)
	}
}

func TestSingleRepoStatusHandler_BadPath(t *testing.T) {
	db := openTestDB(t)

	handler := newSingleRepoStatusHandler(db)
	req := httptest.NewRequest("GET", "/api/status/noslash", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestAuditHandler_NoAPIKey(t *testing.T) {
	db := openTestDB(t)

	handler := newAuditHandler(db, nil, "", "")
	req := httptest.NewRequest("POST", "/api/audit/lucas42/test_repo?ref=my-branch", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when API key not configured, got %d", w.Code)
	}
}

func TestAuditHandler_WrongAPIKey(t *testing.T) {
	db := openTestDB(t)

	handler := newAuditHandler(db, nil, "", "correct-key")
	req := httptest.NewRequest("POST", "/api/audit/lucas42/test_repo?ref=my-branch", nil)
	req.Header.Set("Authorization", "Bearer wrong-key")
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for wrong API key, got %d", w.Code)
	}
}

func TestAuditHandler_UnknownRepo(t *testing.T) {
	db := openTestDB(t)

	handler := newAuditHandler(db, nil, "", "test-key")
	req := httptest.NewRequest("POST", "/api/audit/lucas42/unknown_repo?ref=my-branch", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown repo, got %d", w.Code)
	}
}

func TestAuditRateLimiter(t *testing.T) {
	rl := newAuditRateLimiter(2, time.Minute)

	if !rl.allow("repo1") {
		t.Error("first request should be allowed")
	}
	if !rl.allow("repo1") {
		t.Error("second request should be allowed")
	}
	if rl.allow("repo1") {
		t.Error("third request should be rejected")
	}
	// Different repo should still work.
	if !rl.allow("repo2") {
		t.Error("first request for different repo should be allowed")
	}
}
