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
// whether the response is a rate limit error. If it is, it either:
//   - sleeps until the reset time (if within rateLimitMaxWait) and returns nil,
//     indicating the caller should retry the request, or
//   - returns a descriptive error if the wait would be too long.
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

	resetUnix := parseRateLimitReset(resp)
	now := time.Now()

	if resetUnix <= 0 {
		// No reset header — can't calculate wait, abort.
		return fmt.Errorf("GitHub API rate limit exceeded; no X-RateLimit-Reset header present")
	}

	resetAt := time.Unix(resetUnix, 0)
	wait := resetAt.Sub(now)

	if wait <= 0 {
		// Reset is in the past — retry immediately.
		slog.Info("GitHub API rate limit exceeded but reset time is in the past; retrying immediately")
		return nil
	}

	if wait > rateLimitMaxWait {
		return fmt.Errorf("GitHub API rate limit exceeded; reset in %s (exceeds %s max wait); aborting sweep",
			wait.Round(time.Second), rateLimitMaxWait)
	}

	slog.Warn("GitHub API rate limit exceeded; waiting for reset",
		"wait", wait.Round(time.Second),
		"reset_at", resetAt.UTC().Format(time.RFC3339),
	)
	rateLimitSleep(wait)
	slog.Info("Rate limit reset window passed; retrying")
	return nil
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
