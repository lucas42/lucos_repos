package conventions

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// mockRoundTripper is a simple RoundTripper backed by a response sequence.
type mockRoundTripper struct {
	responses []*http.Response
	errors    []error
	calls     int
}

func (m *mockRoundTripper) RoundTrip(_ *http.Request) (*http.Response, error) {
	i := m.calls
	m.calls++
	if i < len(m.errors) && m.errors[i] != nil {
		return nil, m.errors[i]
	}
	if i < len(m.responses) {
		return m.responses[i], nil
	}
	return nil, fmt.Errorf("mockRoundTripper: no response for call %d", i)
}

// makeResp creates a minimal *http.Response for testing.
func makeResp(statusCode int, body string, headers http.Header) *http.Response {
	if headers == nil {
		headers = http.Header{}
	}
	return &http.Response{
		StatusCode: statusCode,
		Header:     headers,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}

func TestRateLimitTransport_NonForbiddenPassedThrough(t *testing.T) {
	mock := &mockRoundTripper{
		responses: []*http.Response{
			makeResp(http.StatusOK, `{"ok":true}`, nil),
		},
	}
	transport := NewRateLimitTransport(mock)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("should not be called"))
	}))
	defer server.Close()

	req, _ := http.NewRequest("GET", server.URL, nil)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if mock.calls != 1 {
		t.Errorf("expected 1 call, got %d", mock.calls)
	}
}

func TestRateLimitTransport_Non403Forbidden_PassedThrough(t *testing.T) {
	// A 403 that is NOT a rate limit should be returned unchanged.
	mock := &mockRoundTripper{
		responses: []*http.Response{
			makeResp(http.StatusForbidden, `{"message":"Resource not accessible by integration","documentation_url":"..."}`, nil),
		},
	}
	transport := NewRateLimitTransport(mock)

	req, _ := http.NewRequest("GET", "http://example.com/test", nil)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 to be passed through, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) == 0 {
		t.Error("expected body to be non-empty on pass-through 403")
	}
	if mock.calls != 1 {
		t.Errorf("expected 1 call, got %d", mock.calls)
	}
}

func TestRateLimitTransport_RateLimitWithinWait_RetriesAndSucceeds(t *testing.T) {
	// First call: rate-limit 403 with reset in 1 second.
	// Second call: success.
	resetAt := time.Now().Add(1 * time.Second)
	rateLimitHeaders := http.Header{}
	rateLimitHeaders.Set("X-RateLimit-Reset", strconv.FormatInt(resetAt.Unix(), 10))
	mock := &mockRoundTripper{
		responses: []*http.Response{
			makeResp(http.StatusForbidden, `{"message":"API rate limit exceeded for installation ID 12345"}`, rateLimitHeaders),
			makeResp(http.StatusOK, `{"protected":true}`, nil),
		},
	}

	// Replace sleep to a no-op so tests don't actually wait.
	origSleep := rateLimitSleep
	defer func() { rateLimitSleep = origSleep }()
	var sleptFor time.Duration
	rateLimitSleep = func(d time.Duration) { sleptFor = d }

	transport := NewRateLimitTransport(mock)
	req, _ := http.NewRequest("GET", "http://example.com/repos/lucas42/test/branches/main/protection", nil)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 after retry, got %d", resp.StatusCode)
	}
	if mock.calls != 2 {
		t.Errorf("expected 2 calls (rate limit + retry), got %d", mock.calls)
	}
	if sleptFor <= 0 {
		t.Error("expected sleep to be called with a positive duration")
	}
}

func TestRateLimitTransport_RateLimitExceedsMaxWait_ReturnsError(t *testing.T) {
	// Rate limit with reset far in the future (beyond rateLimitMaxWait).
	resetAt := time.Now().Add(rateLimitMaxWait + 10*time.Minute)
	rateLimitHeaders := http.Header{}
	rateLimitHeaders.Set("X-RateLimit-Reset", strconv.FormatInt(resetAt.Unix(), 10))
	mock := &mockRoundTripper{
		responses: []*http.Response{
			makeResp(http.StatusForbidden, `{"message":"API rate limit exceeded"}`, rateLimitHeaders),
		},
	}

	origSleep := rateLimitSleep
	defer func() { rateLimitSleep = origSleep }()
	rateLimitSleep = func(d time.Duration) { t.Error("sleep should not be called when wait exceeds max") }

	transport := NewRateLimitTransport(mock)
	req, _ := http.NewRequest("GET", "http://example.com/test", nil)
	_, err := transport.RoundTrip(req)
	if err == nil {
		t.Fatal("expected error when rate limit wait exceeds max, got nil")
	}
	if mock.calls != 1 {
		t.Errorf("expected 1 call (no retry), got %d", mock.calls)
	}
}

func TestRateLimitTransport_RateLimitNoResetHeader_ReturnsError(t *testing.T) {
	// Rate limit 403 but no X-RateLimit-Reset header.
	mock := &mockRoundTripper{
		responses: []*http.Response{
			makeResp(http.StatusForbidden, `{"message":"API rate limit exceeded"}`, nil),
		},
	}

	transport := NewRateLimitTransport(mock)
	req, _ := http.NewRequest("GET", "http://example.com/test", nil)
	_, err := transport.RoundTrip(req)
	if err == nil {
		t.Fatal("expected error when no reset header, got nil")
	}
}

func TestRateLimitTransport_RateLimitResetInPast_RetriesImmediately(t *testing.T) {
	// Reset timestamp is in the past — should retry immediately without sleeping.
	resetAt := time.Now().Add(-1 * time.Minute)
	rateLimitHeaders := http.Header{}
	rateLimitHeaders.Set("X-RateLimit-Reset", strconv.FormatInt(resetAt.Unix(), 10))
	mock := &mockRoundTripper{
		responses: []*http.Response{
			makeResp(http.StatusForbidden, `{"message":"API rate limit exceeded"}`, rateLimitHeaders),
			makeResp(http.StatusOK, `{}`, nil),
		},
	}

	origSleep := rateLimitSleep
	defer func() { rateLimitSleep = origSleep }()
	rateLimitSleep = func(d time.Duration) { t.Error("sleep should not be called when reset is in the past") }

	transport := NewRateLimitTransport(mock)
	req, _ := http.NewRequest("GET", "http://example.com/test", nil)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 after immediate retry, got %d", resp.StatusCode)
	}
	if mock.calls != 2 {
		t.Errorf("expected 2 calls, got %d", mock.calls)
	}
}

func TestRateLimitTransport_IntegrationWithCachingTransport(t *testing.T) {
	// Verify that CachingTransport → RateLimitTransport works correctly:
	// a rate-limit 403 should not be cached, and the retried success should be cached.
	var callCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First request: rate limit.
			reset := strconv.FormatInt(time.Now().Add(-1*time.Second).Unix(), 10)
			w.Header().Set("X-RateLimit-Reset", reset)
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"message":"API rate limit exceeded"}`))
			return
		}
		// Subsequent requests: success.
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	origSleep := rateLimitSleep
	defer func() { rateLimitSleep = origSleep }()
	rateLimitSleep = func(d time.Duration) {} // no-op

	rl := NewRateLimitTransport(http.DefaultTransport)
	ct := NewCachingTransport(rl)
	client := &http.Client{Transport: ct}

	// First request — hits rate limit, retries, gets 200.
	resp1, err := client.Get(server.URL + "/test")
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	resp1.Body.Close()
	if resp1.StatusCode != http.StatusOK {
		t.Errorf("expected 200 from first request (after retry), got %d", resp1.StatusCode)
	}

	// Second request to same URL — should be served from cache (no new network calls).
	resp2, err := client.Get(server.URL + "/test")
	if err != nil {
		t.Fatalf("second request failed: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("expected 200 from cached response, got %d", resp2.StatusCode)
	}

	// Total server calls: 1 (rate limit) + 1 (retry) = 2. Third request is cached.
	if callCount != 2 {
		t.Errorf("expected 2 server calls (rate limit + retry), got %d", callCount)
	}
}
