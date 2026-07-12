package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"lucos_repos/conventions"
)

// auditDryRunMaxWaitDefault is how long the dry-run's RateLimitTransport will
// wait for a primary (points-based) GitHub rate-limit reset before giving up.
// It's much longer than the production 5-min ceiling (conventions' own
// default) because unlike a live request-serving path, this CI job can afford
// to block for the ~20-25 min a primary-quota reset typically takes — the
// alternative is a red dry-run check that self-heals at the top of the next
// hour anyway (lucas42/lucos_repos#462).
const auditDryRunMaxWaitDefault = 30 * time.Minute

// auditDryRunMaxWaitEnvVar overrides auditDryRunMaxWaitDefault when set, as a
// Go duration string (e.g. "45m").
const auditDryRunMaxWaitEnvVar = "AUDIT_DRYRUN_MAX_WAIT"

// auditDryRunMaxWait returns the configured max-wait for the dry-run's
// RateLimitTransport, falling back to auditDryRunMaxWaitDefault if
// AUDIT_DRYRUN_MAX_WAIT is unset or unparseable.
func auditDryRunMaxWait() time.Duration {
	v := os.Getenv(auditDryRunMaxWaitEnvVar)
	if v == "" {
		return auditDryRunMaxWaitDefault
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		slog.Warn("invalid AUDIT_DRYRUN_MAX_WAIT value; using default",
			"value", v, "default", auditDryRunMaxWaitDefault)
		return auditDryRunMaxWaitDefault
	}
	return d
}

// DryRunReport is the output of a dry-run audit sweep. It matches the structure
// of StatusReport from the /api/status endpoint so the diff is straightforward.
type DryRunReport struct {
	Repos   map[string]DryRunRepoStatus `json:"repos"`
	Summary DryRunSummary               `json:"summary"`
	// SkippedChecks is the number of convention checks that could not be
	// completed due to API errors. Non-zero means the results are incomplete.
	SkippedChecks int `json:"skipped_checks"`
}

// DryRunRepoStatus holds the dry-run results for a single repo.
type DryRunRepoStatus struct {
	Type        conventions.RepoType          `json:"type"`
	Conventions map[string]DryRunConvStatus   `json:"conventions"`
	Compliant   bool                          `json:"compliant"`
}

// DryRunConvStatus holds the dry-run result for a single convention on a repo.
type DryRunConvStatus struct {
	Pass    bool   `json:"pass"`
	Detail  string `json:"detail"`
	// Skipped is true when the check could not determine compliance due to an
	// API error. The convention result should be ignored for diff purposes.
	Skipped bool   `json:"skipped,omitempty"`
}

// DryRunSummary holds aggregate counts from the dry-run.
type DryRunSummary struct {
	TotalRepos      int `json:"total_repos"`
	CompliantRepos  int `json:"compliant_repos"`
	TotalViolations int `json:"total_violations"`
}

// dryRunPendingCheck captures everything needed to retry a convention check
// that returned an indeterminate result on the first pass.
type dryRunPendingCheck struct {
	repoName   string
	convention conventions.Convention
	ctx        conventions.RepoContext
}

// runAuditDryRun runs a full convention sweep without creating issues or writing
// to any database. Findings are written as JSON to stdout.
func runAuditDryRun() {
	// Enable in-memory response caching to deduplicate GitHub API calls.
	// ThrottleTransport paces actual network requests and RateLimitTransport
	// retries transient rate-limit 403s — the same chain as the production
	// sweep (audit.go). This path previously used only CachingTransport with
	// no retry or pacing at all, which is why the audit-dry-run CI check
	// intermittently failed with a mass of unretried 403s
	// (lucas42/lucos_repos#433).
	//
	// dryRunMaxWait is set much longer than RateLimitTransport's 5m default:
	// on a busy estate-rollout hour, several sweeps (this dry-run, other open
	// PRs' dry-runs, the production sweep) can exhaust the *shared* primary
	// hourly quota, whose reset is typically 20-25 min away — well beyond the
	// 5m ceiling that's appropriate for a live request-serving path. Waiting
	// it out here lets this CI job finish with a full diff instead of going
	// red (lucas42/lucos_repos#462).
	dryRunMaxWait := auditDryRunMaxWait()
	throttleTransport := conventions.NewThrottleTransport(http.DefaultTransport, contentFetchThrottleInterval)
	rateLimitTransport := conventions.NewRateLimitTransport(throttleTransport)
	rateLimitTransport.MaxWait = dryRunMaxWait
	cachingTransport := conventions.NewCachingTransport(rateLimitTransport)
	cachingClient := &http.Client{Transport: cachingTransport}
	conventions.SetHTTPClient(cachingClient)
	defer conventions.SetHTTPClient(nil)
	defer func() {
		slog.Info("GitHub API cache stats",
			"unique_urls", cachingTransport.Stats(),
			"cache_hits", cachingTransport.Hits(),
			"network_calls", cachingTransport.Misses(),
		)
	}()

	githubAuth, err := NewGitHubAuthClient("GITHUB_APP_ID", "GITHUB_APP_PEM")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to initialise GitHub App authentication: %v\n", err)
		os.Exit(1)
	}

	// Use a minimal AuditSweeper just for its fetch methods — no DB needed.
	s := NewAuditSweeper(nil, githubAuth, "")

	token, err := githubAuth.GetInstallationToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to get GitHub token: %v\n", err)
		os.Exit(1)
	}

	repos, err := s.fetchRepos(token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to fetch repos: %v\n", err)
		os.Exit(1)
	}
	slog.Info("Dry-run: fetched repos", "count", len(repos))

	repoInfos, err := s.fetchRepoTypes()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to fetch configy data: %v\n", err)
		os.Exit(1)
	}

	allConventions := conventions.All()
	report := DryRunReport{
		Repos: map[string]DryRunRepoStatus{},
	}

	// pendingRetries holds checks that returned an indeterminate result on the
	// first pass, to be retried once as a batch after the full sweep — the
	// same "don't fail on a small transient-skip tail" behaviour as the
	// production sweep (audit.go), applied here too since this dry-run is
	// exactly the path that hit mass rate-limit 403s on lucas42/lucos_repos#433.
	var pendingRetries []dryRunPendingCheck

	for _, repo := range repos {
		if repo.Archived || repo.Fork {
			continue
		}

		// Re-fetch the token per-repo — cheap due to caching; guarantees >5 min of life.
		token, tokenErr := githubAuth.GetInstallationToken()
		if tokenErr != nil {
			fmt.Fprintf(os.Stderr, "error: failed to refresh GitHub token mid-sweep: %v\n", tokenErr)
			os.Exit(1)
		}

		repoName := repo.FullName
		info, ok := repoInfos[repoName]
		if !ok {
			info = repoInfo{Type: conventions.RepoTypeUnconfigured}
		}

		ctx := conventions.RepoContext{
			Name:                  repoName,
			GitHubToken:           token,
			Type:                  info.Type,
			Hosts:                 info.Hosts,
			GitHubBaseURL:         s.githubAPIBaseURL,
			UnsupervisedAgentCode: info.UnsupervisedAgentCode,
		}

		repoStatus := DryRunRepoStatus{
			Type:        info.Type,
			Conventions: map[string]DryRunConvStatus{},
			Compliant:   true,
		}

		for _, convention := range allConventions {
			if !convention.AppliesToType(info.Type) {
				continue
			}
			if !convention.AppliesToRepo(repoName) {
				continue
			}

			result := convention.Check(ctx)

			if result.Err != nil {
				slog.Warn("Dry-run: convention check indeterminate; will retry after full pass",
					"repo", repoName, "convention", convention.ID, "error", result.Err)
				pendingRetries = append(pendingRetries, dryRunPendingCheck{
					repoName:   repoName,
					convention: convention,
					ctx:        ctx,
				})
				continue
			}

			repoStatus.Conventions[convention.ID] = DryRunConvStatus{
				Pass:   result.Pass,
				Detail: result.Detail,
			}
			if !result.Pass {
				repoStatus.Compliant = false
			}
		}

		report.Repos[repoName] = repoStatus
	}

	if len(pendingRetries) > 0 {
		slog.Info("Dry-run: retrying convention checks skipped due to API errors",
			"count", len(pendingRetries), "wait", auditRetryTailDelay)
		auditRetryTailSleep(auditRetryTailDelay)

		// Use a fresh, uncached client for the retry pass — see the matching
		// comment in audit.go's sweep(): CachingTransport caches non-2xx
		// responses too, so reusing the cached client here would just replay
		// the same failure instead of hitting the network again
		// (lucas42/lucos_repos#433).
		retryThrottle := conventions.NewThrottleTransport(http.DefaultTransport, s.contentFetchThrottleInterval)
		retryRateLimit := conventions.NewRateLimitTransport(retryThrottle)
		retryRateLimit.MaxWait = dryRunMaxWait
		conventions.SetHTTPClient(&http.Client{Transport: retryRateLimit})

		for _, pc := range pendingRetries {
			result := pc.convention.Check(pc.ctx)
			repoStatus := report.Repos[pc.repoName]

			if result.Err != nil {
				slog.Warn("Dry-run: convention check still indeterminate after retry",
					"repo", pc.repoName, "convention", pc.convention.ID, "error", result.Err)
				repoStatus.Conventions[pc.convention.ID] = DryRunConvStatus{Skipped: true}
				report.SkippedChecks++
			} else {
				repoStatus.Conventions[pc.convention.ID] = DryRunConvStatus{
					Pass:   result.Pass,
					Detail: result.Detail,
				}
				if !result.Pass {
					repoStatus.Compliant = false
				}
			}

			report.Repos[pc.repoName] = repoStatus
		}
	}

	// Compute summary.
	for _, rs := range report.Repos {
		report.Summary.TotalRepos++
		if rs.Compliant {
			report.Summary.CompliantRepos++
		}
		for _, cs := range rs.Conventions {
			if !cs.Skipped && !cs.Pass {
				report.Summary.TotalViolations++
			}
		}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to encode output: %v\n", err)
		os.Exit(1)
	}

	if report.SkippedChecks > 0 {
		fmt.Fprintf(os.Stderr, "warning: %d convention check(s) skipped due to API errors — results are incomplete\n", report.SkippedChecks)
		os.Exit(2)
	}
}
