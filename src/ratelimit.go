package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// rateLimitLowThreshold is the X-RateLimit-Remaining value below which a
// warning is logged proactively, before exhaustion occurs.
const rateLimitLowThreshold = 100

// rateLimitMaxWait is the maximum duration we will sleep waiting for the
// rate limit to reset before giving up and returning an error instead.
const rateLimitMaxWait = 5 * time.Minute

// rateLimitSleep is the function used to wait for the rate limit to reset.
// It is a package-level variable so tests can replace it with a no-op.
var rateLimitSleep = time.Sleep

// rateLimitBody is the JSON structure returned by GitHub when a rate limit is hit.
type rateLimitBody struct {
	Message     string `json:"message"`
	DocumentURL string `json:"documentation_url"`
}

// checkRateLimitHeaders inspects the X-RateLimit-Remaining header on a
// successful (non-403) response and logs a warning if the remaining quota
// is below rateLimitLowThreshold.
func checkRateLimitHeaders(resp *http.Response) {
	remaining := parseRateLimitRemaining(resp)
	if remaining >= 0 && remaining < rateLimitLowThreshold {
		slog.Warn("GitHub API rate limit quota is low",
			"x_rate_limit_remaining", remaining,
			"threshold", rateLimitLowThreshold,
		)
	}
}

// handleRateLimitError is called when a GitHub API returns a 403. It checks
// whether the response is a rate limit error. If it is, it determines how
// long to wait — preferring Retry-After (used by GitHub's secondary/
// abuse-detection rate limit) over X-RateLimit-Reset (used by the primary
// points-based rate limit; see lucas42/lucos_repos#433) — and either:
//   - sleeps until the wait elapses (if within rateLimitMaxWait) and returns
//     nil, indicating the caller should retry the request, or
//   - returns a descriptive error, including the response body and
//     rate-limit headers, if the wait would be too long or no wait can be
//     determined.
//
// If the 403 is not a rate limit error, it returns a non-nil error describing
// the unexpected 403.
//
// body is the already-read response body (passed in so the caller can also use it
// for error messages without re-reading the closed body).
func handleRateLimitError(resp *http.Response, body []byte) error {
	if !isRateLimitBody(body) {
		return fmt.Errorf("GitHub API returned 403 (not a rate limit): %s", body)
	}

	wait, resetAt, haveWait := parseRetryWait(resp)

	if !haveWait {
		return fmt.Errorf("GitHub API rate limit exceeded; no Retry-After or X-RateLimit-Reset header present: %s",
			rateLimitDiagnostic(resp, body))
	}

	if wait <= 0 {
		// Reset is in the past — retry immediately.
		slog.Info("GitHub API rate limit exceeded but reset time is in the past; retrying immediately")
		return nil
	}

	if wait > rateLimitMaxWait {
		return fmt.Errorf("GitHub API rate limit exceeded; wait %s exceeds %s max wait; aborting sweep: %s",
			wait.Round(time.Second), rateLimitMaxWait, rateLimitDiagnostic(resp, body))
	}

	fields := []any{"wait", wait.Round(time.Second)}
	if !resetAt.IsZero() {
		fields = append(fields, "reset_at", resetAt.UTC().Format(time.RFC3339))
	}
	slog.Warn("GitHub API rate limit exceeded; waiting for reset", fields...)
	rateLimitSleep(wait)
	slog.Info("Rate limit reset window passed; retrying")
	return nil
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

// parseRetryWait determines how long to wait before retrying a rate-limited
// request. It prefers Retry-After (an integer number of seconds to wait, per
// RFC 9110 §10.2.3 — the form GitHub's secondary/abuse-detection rate limit
// uses) over X-RateLimit-Reset (an absolute Unix timestamp, used by the
// primary points-based rate limit). resetAt is the zero time when the wait
// came from Retry-After (no absolute reset instant to report). haveWait is
// false if neither header is present or parseable.
func parseRetryWait(resp *http.Response) (wait time.Duration, resetAt time.Time, haveWait bool) {
	if val := resp.Header.Get("Retry-After"); val != "" {
		if secs, err := strconv.ParseInt(val, 10, 64); err == nil && secs >= 0 {
			return time.Duration(secs) * time.Second, time.Time{}, true
		}
	}

	resetUnix := parseRateLimitReset(resp)
	if resetUnix <= 0 {
		return 0, time.Time{}, false
	}
	resetAt = time.Unix(resetUnix, 0)
	return resetAt.Sub(time.Now()), resetAt, true
}

// isRateLimitBody returns true if the 403 body looks like a GitHub rate limit response.
// GitHub rate limit 403s have a "message" field containing "rate limit" (case-insensitive).
func isRateLimitBody(body []byte) bool {
	var parsed rateLimitBody
	if err := json.Unmarshal(body, &parsed); err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(parsed.Message), "rate limit")
}

// parseRateLimitRemaining reads X-RateLimit-Remaining from the response headers.
// Returns -1 if the header is absent or unparseable.
func parseRateLimitRemaining(resp *http.Response) int64 {
	val := resp.Header.Get("X-RateLimit-Remaining")
	if val == "" {
		return -1
	}
	n, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return -1
	}
	return n
}

// parseRateLimitReset reads X-RateLimit-Reset (a Unix timestamp) from the
// response headers. Returns 0 if the header is absent or unparseable.
func parseRateLimitReset(resp *http.Response) int64 {
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
