package main

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// newIdleSweeper returns an AuditSweeper that is not in progress,
// suitable for testing the sweep handler without touching real APIs.
// The background sweep goroutine will fail fast (bad URLs) but won't panic.
func newIdleSweeper(t *testing.T) *AuditSweeper {
	t.Helper()
	db := openTestDB(t)
	s := &AuditSweeper{
		db:               db,
		githubAuth:       fakeGitHubAuth(t),
		githubOrg:        "lucas42",
		sweepInterval:    6 * time.Hour,
		system:           "lucos_repos",
		configyBaseURL:   "http://localhost:0",
		githubAPIBaseURL: "http://localhost:0",
	}
	s.issueClientFactory = func(token string) *GitHubIssueClient {
		return NewGitHubIssueClient("http://localhost:0", token)
	}
	return s
}

func TestSweepHandler_AcceptsWhenIdle(t *testing.T) {
	sweeper := newIdleSweeper(t)
	handler := newSweepHandler(sweeper)

	req := httptest.NewRequest("POST", "/api/sweep", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("expected 202 Accepted when idle, got %d", w.Code)
	}
}

func TestSweepHandler_ConflictWhenInProgress(t *testing.T) {
	sweeper := newIdleSweeper(t)

	// Mark a sweep as already in progress.
	sweeper.mu.Lock()
	sweeper.sweepInProgress = true
	sweeper.mu.Unlock()

	handler := newSweepHandler(sweeper)

	req := httptest.NewRequest("POST", "/api/sweep", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 Conflict when sweep already running, got %d", w.Code)
	}
}

func TestSweepHandler_PreventsConcurrentTriggers(t *testing.T) {
	sweeper := newIdleSweeper(t)
	handler := newSweepHandler(sweeper)

	// Use a WaitGroup to fire two requests simultaneously.
	var wg sync.WaitGroup
	codes := make([]int, 2)
	for i := range 2 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := httptest.NewRequest("POST", "/api/sweep", nil)
			w := httptest.NewRecorder()
			handler(w, req)
			codes[idx] = w.Code
		}(i)
	}
	wg.Wait()

	accepted := 0
	conflict := 0
	for _, code := range codes {
		switch code {
		case http.StatusAccepted:
			accepted++
		case http.StatusConflict:
			conflict++
		default:
			t.Errorf("unexpected status code %d", code)
		}
	}
	if accepted != 1 || conflict != 1 {
		t.Errorf("expected exactly one 202 and one 409, got %d 202s and %d 409s", accepted, conflict)
	}
}
