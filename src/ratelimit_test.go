package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestCheckRateLimitHeaders_LowRemaining verifies that a warning is logged
// when X-RateLimit-Remaining drops below the threshold.
// (We verify behaviour indirectly via the absence of errors — slog output is
// not easily captured in unit tests without a custom handler, but the function
// must not panic or return an error.)
func TestCheckRateLimitHeaders_LowRemaining(t *testing.T) {
	// Build a fake response with a low remaining count.
	rec := httptest.NewRecorder()
	rec.Header().Set("X-RateLimit-Remaining", "5")
	resp := rec.Result()
	// Must not panic.
	checkRateLimitHeaders(resp)
}

// TestCheckRateLimitHeaders_HighRemaining verifies that a high remaining count
// does not cause issues.
func TestCheckRateLimitHeaders_HighRemaining(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Header().Set("X-RateLimit-Remaining", "4999")
	resp := rec.Result()
	checkRateLimitHeaders(resp)
}

// TestCheckRateLimitHeaders_MissingHeader verifies that a missing header is tolerated.
func TestCheckRateLimitHeaders_MissingHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	resp := rec.Result()
	checkRateLimitHeaders(resp) // must not panic
}

// TestIsRateLimitBody_True verifies detection of a GitHub rate limit body.
func TestIsRateLimitBody_True(t *testing.T) {
	body, _ := json.Marshal(rateLimitBody{
		Message:     "API rate limit exceeded for installation ID 12345.",
		DocumentURL: "https://docs.github.com/rest/overview/rate-limits-for-rest-api",
	})
	if !isRateLimitBody(body) {
		t.Error("expected isRateLimitBody to return true for rate limit message")
	}
}

// TestIsRateLimitBody_False verifies that a non-rate-limit 403 body is not misidentified.
func TestIsRateLimitBody_False(t *testing.T) {
	body, _ := json.Marshal(rateLimitBody{
		Message: "Resource not accessible by integration",
	})
	if isRateLimitBody(body) {
		t.Error("expected isRateLimitBody to return false for non-rate-limit message")
	}
}

// TestIsRateLimitBody_InvalidJSON verifies that invalid JSON is handled gracefully.
func TestIsRateLimitBody_InvalidJSON(t *testing.T) {
	if isRateLimitBody([]byte("not json")) {
		t.Error("expected isRateLimitBody to return false for invalid JSON")
	}
}

// TestHandleRateLimitError_NonRateLimit verifies that a 403 with a non-rate-limit
// body returns an error immediately without sleeping.
func TestHandleRateLimitError_NonRateLimit(t *testing.T) {
	body, _ := json.Marshal(rateLimitBody{Message: "Resource not accessible by integration"})
	rec := httptest.NewRecorder()
	resp := rec.Result()

	err := handleRateLimitError(resp, body)
	if err == nil {
		t.Error("expected error for non-rate-limit 403, got nil")
	}
	if !strings.Contains(err.Error(), "not a rate limit") {
		t.Errorf("expected error to mention 'not a rate limit', got: %v", err)
	}
}

// TestHandleRateLimitError_WaitAndRetry verifies that a rate limit response with
// a reset time within the max wait window causes a sleep and returns nil.
func TestHandleRateLimitError_WaitAndRetry(t *testing.T) {
	body, _ := json.Marshal(rateLimitBody{Message: "API rate limit exceeded"})

	// Set reset 10 seconds from now — well within rateLimitMaxWait.
	resetAt := time.Now().Add(10 * time.Second)

	rec := httptest.NewRecorder()
	rec.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetAt.Unix(), 10))
	resp := rec.Result()

	// Replace sleep with a no-op so the test doesn't actually wait.
	var sleptFor time.Duration
	origSleep := rateLimitSleep
	rateLimitSleep = func(d time.Duration) { sleptFor = d }
	defer func() { rateLimitSleep = origSleep }()

	err := handleRateLimitError(resp, body)
	if err != nil {
		t.Errorf("expected nil error for short wait, got: %v", err)
	}
	if sleptFor <= 0 {
		t.Error("expected rateLimitSleep to be called with a positive duration")
	}
}

// TestHandleRateLimitError_TooLong verifies that when the reset time is beyond
// rateLimitMaxWait, handleRateLimitError returns an error without sleeping.
func TestHandleRateLimitError_TooLong(t *testing.T) {
	body, _ := json.Marshal(rateLimitBody{Message: "API rate limit exceeded"})

	// Set reset 30 minutes from now — well beyond rateLimitMaxWait (5 min).
	resetAt := time.Now().Add(30 * time.Minute)

	rec := httptest.NewRecorder()
	rec.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetAt.Unix(), 10))
	resp := rec.Result()

	slept := false
	origSleep := rateLimitSleep
	rateLimitSleep = func(d time.Duration) { slept = true }
	defer func() { rateLimitSleep = origSleep }()

	err := handleRateLimitError(resp, body)
	if err == nil {
		t.Error("expected error when reset time exceeds max wait, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("expected error to mention exceeding max wait, got: %v", err)
	}
	if slept {
		t.Error("expected no sleep when reset time is too far in the future")
	}
}

// TestHandleRateLimitError_NoResetHeader verifies that a missing X-RateLimit-Reset
// header results in an error rather than an infinite wait.
func TestHandleRateLimitError_NoResetHeader(t *testing.T) {
	body, _ := json.Marshal(rateLimitBody{Message: "API rate limit exceeded"})
	rec := httptest.NewRecorder()
	resp := rec.Result()

	err := handleRateLimitError(resp, body)
	if err == nil {
		t.Error("expected error when X-RateLimit-Reset header is absent, got nil")
	}
}

// TestFetchIssuesList_RateLimitRetry verifies that fetchIssuesList retries after
// a 403 rate limit response and succeeds on the second attempt.
func TestFetchIssuesList_RateLimitRetry(t *testing.T) {
	const title = "[Convention] has-circleci-config: has config"
	const issueURL = "https://github.com/lucas42/test_repo/issues/1"

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First call: rate limit response.
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(1*time.Second).Unix(), 10))
			w.WriteHeader(http.StatusForbidden)
			body, _ := json.Marshal(rateLimitBody{Message: "API rate limit exceeded"})
			w.Write(body)
			return
		}
		// Second call: success.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode([]gitHubIssue{
			{Number: 1, HTMLURL: issueURL, Title: title, State: "open"},
		})
	}))
	defer server.Close()

	// Replace sleep with a no-op.
	origSleep := rateLimitSleep
	rateLimitSleep = func(d time.Duration) {}
	defer func() { rateLimitSleep = origSleep }()

	client := NewGitHubIssueClient(server.URL, "fake-token")
	issues, err := client.fetchIssuesList(server.URL + "/repos/lucas42/test_repo/issues")
	if err != nil {
		t.Fatalf("expected success after retry, got error: %v", err)
	}
	if len(issues) != 1 {
		t.Errorf("expected 1 issue, got %d", len(issues))
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls (initial + retry), got %d", callCount)
	}
}

// TestFetchIssuesList_RateLimitTooLong verifies that fetchIssuesList returns an
// error when the rate limit reset time exceeds the max wait.
func TestFetchIssuesList_RateLimitTooLong(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(30*time.Minute).Unix(), 10))
		w.WriteHeader(http.StatusForbidden)
		body, _ := json.Marshal(rateLimitBody{Message: "API rate limit exceeded"})
		w.Write(body)
	}))
	defer server.Close()

	client := NewGitHubIssueClient(server.URL, "fake-token")
	_, err := client.fetchIssuesList(server.URL + "/repos/lucas42/test_repo/issues")
	if err == nil {
		t.Error("expected error when rate limit reset is too far in the future, got nil")
	}
}

// TestFetchRepos_RateLimitRetry verifies that fetchRepos retries a page after
// a 403 rate limit response and succeeds on the second attempt.
func TestFetchRepos_RateLimitRetry(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First call: rate limit response.
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(1*time.Second).Unix(), 10))
			w.WriteHeader(http.StatusForbidden)
			body, _ := json.Marshal(rateLimitBody{Message: "API rate limit exceeded"})
			w.Write(body)
			return
		}
		// Second call: success with a small repo list.
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]gitHubRepo{
			{FullName: "lucas42/lucos_photos"},
		})
	}))
	defer server.Close()

	// Replace sleep with a no-op.
	origSleep := rateLimitSleep
	rateLimitSleep = func(d time.Duration) {}
	defer func() { rateLimitSleep = origSleep }()

	s := &AuditSweeper{githubAPIBaseURL: server.URL, githubOrg: "lucas42"}
	repos, err := s.fetchRepos("fake-token")
	if err != nil {
		t.Fatalf("expected success after retry, got error: %v", err)
	}
	if len(repos) != 1 || repos[0].FullName != "lucas42/lucos_photos" {
		t.Errorf("expected [lucas42/lucos_photos], got %v", repos)
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls (initial + retry), got %d", callCount)
	}
}
