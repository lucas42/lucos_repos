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
// body to determine whether it is a GitHub rate-limit error. If it is, it
// determines how long to wait before retrying — preferring the Retry-After
// header (used by GitHub's secondary/abuse-detection rate limit, which is
// what actually trips during a sweep's content-fetch fan-out — see
// lucas42/lucos_repos#433) and falling back to X-RateLimit-Reset (used by the
// primary, points-based rate limit). If the wait is within rateLimitMaxWait,
// it sleeps and retries the request once. Otherwise it returns a descriptive
// rate-limit error including the response body and rate-limit headers, so a
// future occurrence is self-diagnosing rather than requiring log archaeology.
// Non-rate-limit 403s are returned unchanged.
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
	// Retry-After (seconds-from-now, used by the secondary/abuse-detection
	// limit) takes priority over X-RateLimit-Reset (an absolute Unix
	// timestamp, used by the primary points-based limit) — GitHub's secondary
	// rate limit responses often carry Retry-After without X-RateLimit-Reset.
	wait, resetAt, haveWait := parseConventionRetryWait(resp)

	if !haveWait {
		return nil, fmt.Errorf("GitHub rate limit exceeded; no Retry-After or X-RateLimit-Reset header — cannot retry: %s",
			rateLimitDiagnostic(resp, body))
	}

	if wait > rateLimitMaxWait {
		return nil, fmt.Errorf("GitHub rate limit exceeded; wait %s exceeds %s max wait: %s",
			wait.Round(time.Second), rateLimitMaxWait, rateLimitDiagnostic(resp, body))
	}

	if wait > 0 {
		fields := []any{"wait", wait.Round(time.Second), "url", req.URL.String()}
		if !resetAt.IsZero() {
			fields = append(fields, "reset_at", resetAt.UTC().Format(time.RFC3339))
		}
		slog.Warn("GitHub rate limit hit in convention check; waiting for reset", fields...)
		rateLimitSleep(wait)
		slog.Info("Rate limit reset window passed; retrying convention check request",
			"url", req.URL.String(),
		)
	} else {
		slog.Info("GitHub rate limit reset is in the past; retrying immediately",
			"url", req.URL.String(),
		)
	}

	// Clone the request before retrying — the original req.Body may already be
	// consumed (though convention helpers use GET with no body, so this is safe).
	retryReq := req.Clone(req.Context())
	return t.Wrapped.RoundTrip(retryReq)
}

// rateLimitDiagnostic formats the response body and rate-limit headers for
// inclusion in an error message, so a rate-limit failure is self-diagnosing
// from the logs alone (lucas42/lucos_repos#433 observability gap).
func rateLimitDiagnostic(resp *http.Response, body []byte) string {
	return fmt.Sprintf("body=%q retry-after=%q x-ratelimit-remaining=%q x-ratelimit-reset=%q",
		strings.TrimSpace(string(body)),
		resp.Header.Get("Retry-After"),
		resp.Header.Get("X-RateLimit-Remaining"),
		resp.Header.Get("X-RateLimit-Reset"),
	)
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

// parseConventionRetryWait determines how long to wait before retrying a
// rate-limited request. It prefers Retry-After (an integer number of seconds
// to wait, per RFC 9110 §10.2.3 — the form GitHub's secondary/abuse-detection
// rate limit uses) over X-RateLimit-Reset (an absolute Unix timestamp, used by
// the primary points-based rate limit). resetAt is the zero time when the
// wait came from Retry-After (no absolute reset instant to report). haveWait
// is false if neither header is present or parseable.
func parseConventionRetryWait(resp *http.Response) (wait time.Duration, resetAt time.Time, haveWait bool) {
	if val := resp.Header.Get("Retry-After"); val != "" {
		if secs, err := strconv.ParseInt(val, 10, 64); err == nil && secs >= 0 {
			return time.Duration(secs) * time.Second, time.Time{}, true
		}
	}

	resetUnix := parseConventionRateLimitReset(resp)
	if resetUnix <= 0 {
		return 0, time.Time{}, false
	}
	resetAt = time.Unix(resetUnix, 0)
	return resetAt.Sub(time.Now()), resetAt, true
}
