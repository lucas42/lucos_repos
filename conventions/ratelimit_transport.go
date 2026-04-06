package conventions

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// rateLimitMaxWait is the maximum duration RateLimitTransport will sleep
// waiting for a rate-limit reset before giving up and returning an error.
const rateLimitMaxWait = 5 * time.Minute

// rateLimitSleep is the sleep function used by RateLimitTransport.
// It is a package-level variable so tests can replace it with a no-op.
var rateLimitSleep = time.Sleep

// rateLimitBodyMessage is the JSON structure GitHub returns on rate-limit 403s.
type rateLimitBodyMessage struct {
	Message string `json:"message"`
}

// RateLimitTransport is an http.RoundTripper that detects GitHub secondary
// rate-limit 403 responses and either waits for the reset window and retries,
// or returns a clear rate-limit error if the wait would be too long.
//
// Non-rate-limit 403 responses are passed through unchanged so callers can
// handle permission errors normally.
//
// Wrap the innermost transport with RateLimitTransport before passing it to
// CachingTransport, so rate-limit responses are never cached.
type RateLimitTransport struct {
	// Wrapped is the underlying transport for actual network requests.
	Wrapped http.RoundTripper
}

// NewRateLimitTransport creates a RateLimitTransport wrapping the given transport.
// If wrapped is nil, http.DefaultTransport is used.
func NewRateLimitTransport(wrapped http.RoundTripper) *RateLimitTransport {
	if wrapped == nil {
		wrapped = http.DefaultTransport
	}
	return &RateLimitTransport{Wrapped: wrapped}
}

// RoundTrip implements http.RoundTripper. On a 403 response, it inspects the
// body to determine whether it is a GitHub secondary rate-limit error. If it
// is, and the reset window is within rateLimitMaxWait, it sleeps and retries
// the request once. Otherwise it returns a rate-limit error. Non-rate-limit
// 403s are returned unchanged.
func (t *RateLimitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.Wrapped.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusForbidden {
		return resp, nil
	}

	// Read and buffer the body so we can inspect it and replay it if needed.
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("reading 403 response body: %w", err)
	}

	if !isConventionRateLimitBody(body) {
		// Not a rate limit — return the 403 response unchanged with body restored.
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return resp, nil
	}

	// It is a rate limit. Decide whether to wait and retry or give up.
	resetUnix := parseConventionRateLimitReset(resp)
	now := time.Now()

	if resetUnix <= 0 {
		return nil, fmt.Errorf("GitHub secondary rate limit exceeded; no X-RateLimit-Reset header — cannot retry")
	}

	resetAt := time.Unix(resetUnix, 0)
	wait := resetAt.Sub(now)

	if wait > rateLimitMaxWait {
		return nil, fmt.Errorf("GitHub secondary rate limit exceeded; reset in %s (exceeds %s max wait)",
			wait.Round(time.Second), rateLimitMaxWait)
	}

	if wait > 0 {
		slog.Warn("GitHub secondary rate limit hit in convention check; waiting for reset",
			"wait", wait.Round(time.Second),
			"reset_at", resetAt.UTC().Format(time.RFC3339),
			"url", req.URL.String(),
		)
		rateLimitSleep(wait)
		slog.Info("Rate limit reset window passed; retrying convention check request",
			"url", req.URL.String(),
		)
	} else {
		slog.Info("GitHub secondary rate limit reset is in the past; retrying immediately",
			"url", req.URL.String(),
		)
	}

	// Clone the request before retrying — the original req.Body may already be
	// consumed (though convention helpers use GET with no body, so this is safe).
	retryReq := req.Clone(req.Context())
	return t.Wrapped.RoundTrip(retryReq)
}

// isConventionRateLimitBody returns true if the 403 body is a GitHub rate-limit response.
func isConventionRateLimitBody(body []byte) bool {
	var msg rateLimitBodyMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(msg.Message), "rate limit")
}

// parseConventionRateLimitReset reads X-RateLimit-Reset (Unix timestamp) from
// the response headers. Returns 0 if the header is absent or unparseable.
func parseConventionRateLimitReset(resp *http.Response) int64 {
	val := resp.Header.Get("X-RateLimit-Reset")
	if val == "" {
		return 0
	}
	n, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return 0
	}
	return n
}
