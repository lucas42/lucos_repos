package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"lucos_repos/conventions"
)

// singleRepoStatusResponse is the response for GET /api/status/{repo}.
type singleRepoStatusResponse struct {
	Repo     string                     `json:"repo"`
	RepoType string                     `json:"repo_type"`
	Checks   map[string]jsonCheckResult `json:"checks"`
}

// auditResponse is the response for POST /api/audit/{repo}?ref={ref}.
type auditResponse struct {
	Repo             string                      `json:"repo"`
	Pass             bool                        `json:"pass"`
	Regressions      []string                    `json:"regressions"`
	BaselineFailures []string                    `json:"baseline_failures"`
	Details          map[string]auditCheckDetail `json:"details"`
}

type auditCheckDetail struct {
	Baseline string `json:"baseline"` // "pass", "fail", or "unknown"
	Current  string `json:"current"`  // "pass", "fail", or "error"
}

// newSingleRepoStatusHandler returns the GET /api/status/{repo} handler.
func newSingleRepoStatusHandler(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract repo name from path: /api/status/lucas42/lucos_photos
		path := strings.TrimPrefix(r.URL.Path, "/api/status/")
		if path == "" || !strings.Contains(path, "/") {
			http.Error(w, "repo name required (e.g. /api/status/lucas42/lucos_photos)", http.StatusBadRequest)
			return
		}

		repoName := path
		report, err := db.GetStatusReport()
		if err != nil {
			slog.Error("Failed to build status report", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		rs, ok := report.Repos[repoName]
		if !ok {
			http.Error(w, "repo not found in audit data", http.StatusNotFound)
			return
		}

		checks := make(map[string]jsonCheckResult, len(rs.Conventions))
		for conv, cs := range rs.Conventions {
			var cr jsonCheckResult
			if cs.Pass {
				cr.Status = "pass"
			} else {
				cr.Status = "fail"
				cr.Issue = cs.IssueURL
			}
			checks[conv] = cr
		}

		resp := singleRepoStatusResponse{
			Repo:     repoName,
			RepoType: string(rs.Type),
			Checks:   checks,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

// auditRateLimiter tracks per-repo request timestamps for rate limiting.
type auditRateLimiter struct {
	mu       sync.Mutex
	requests map[string][]time.Time
	limit    int
	window   time.Duration
}

func newAuditRateLimiter(limit int, window time.Duration) *auditRateLimiter {
	return &auditRateLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}
}

func (rl *auditRateLimiter) allow(repo string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	// Remove expired entries.
	var valid []time.Time
	for _, t := range rl.requests[repo] {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	if len(valid) >= rl.limit {
		rl.requests[repo] = valid
		return false
	}

	if len(valid) == 0 {
		delete(rl.requests, repo)
	}

	rl.requests[repo] = append(valid, now)
	return true
}

// newAuditHandler returns the POST /api/audit/{repo}?ref={ref} handler.
func newAuditHandler(db *DB, githubAuth *GitHubAuthClient, githubAPIBase string, oidcValidator *GitHubOIDCValidator) http.HandlerFunc {
	limiter := newAuditRateLimiter(10, time.Minute)

	return func(w http.ResponseWriter, r *http.Request) {
		// OIDC auth — validate GitHub Actions JWT.
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, "unauthorized: missing Bearer token", http.StatusUnauthorized)
			return
		}
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if _, err := oidcValidator.ValidateToken(token); err != nil {
			slog.Warn("OIDC token validation failed", "error", err)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		// Extract repo name from path: /api/audit/lucas42/lucos_photos
		path := strings.TrimPrefix(r.URL.Path, "/api/audit/")
		if path == "" || !strings.Contains(path, "/") {
			http.Error(w, "repo name required (e.g. /api/audit/lucas42/lucos_photos?ref=my-branch)", http.StatusBadRequest)
			return
		}
		repoName := path
		ref := r.URL.Query().Get("ref")

		// Rate limit: 10 requests per minute per repo.
		if !limiter.allow(repoName) {
			http.Error(w, "rate limit exceeded (10 requests per minute per repo)", http.StatusTooManyRequests)
			return
		}

		// Get baseline from the database.
		report, err := db.GetStatusReport()
		if err != nil {
			slog.Error("Failed to build baseline report", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		baseline, ok := report.Repos[repoName]
		if !ok {
			http.Error(w, "repo not found in audit data", http.StatusNotFound)
			return
		}

		// Get a GitHub token.
		ghToken, err := githubAuth.GetInstallationToken()
		if err != nil {
			slog.Error("Failed to get GitHub token for audit", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		// Run all applicable conventions against the ref.
		ctx := conventions.RepoContext{
			Name:          repoName,
			GitHubToken:   ghToken,
			Type:          baseline.Type,
			GitHubBaseURL: githubAPIBase,
			Ref:           ref,
		}

		allConventions := conventions.All()
		details := make(map[string]auditCheckDetail)
		var regressions []string
		var baselineFailures []string

		for _, conv := range allConventions {
			if !conv.AppliesToType(baseline.Type) {
				continue
			}
			if !conv.AppliesToRepo(repoName) {
				continue
			}

			result := conv.Check(ctx)

			// Determine baseline state.
			baselineState := "unknown"
			if bcs, ok := baseline.Conventions[conv.ID]; ok {
				if bcs.Pass {
					baselineState = "pass"
				} else {
					baselineState = "fail"
				}
			}

			// Determine current state.
			currentState := "pass"
			if result.Err != nil {
				currentState = "error"
			} else if !result.Pass {
				currentState = "fail"
			}

			details[conv.ID] = auditCheckDetail{
				Baseline: baselineState,
				Current:  currentState,
			}

			// A regression is when baseline was pass but current is fail.
			if baselineState == "pass" && currentState == "fail" {
				regressions = append(regressions, conv.ID)
			}
			if baselineState == "fail" {
				baselineFailures = append(baselineFailures, conv.ID)
			}
		}

		resp := auditResponse{
			Repo:             repoName,
			Pass:             len(regressions) == 0,
			Regressions:      regressions,
			BaselineFailures: baselineFailures,
			Details:          details,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}
