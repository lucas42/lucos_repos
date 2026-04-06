package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"lucos_repos/conventions"
)

// configyBaseURL is the base URL for the lucos_configy API. It can be
// overridden in tests via AuditSweeper.configyBaseURL.
const configyBaseURL = "https://configy.l42.eu"

// githubAPIBaseURL is the base URL for the GitHub API used by AuditSweeper.
// It can be overridden in tests via AuditSweeper.githubAPIBaseURL.
const githubAPIBaseURL = "https://api.github.com"

// configySystem represents a single entry from the configy /systems endpoint.
type configySystem struct {
	ID                    string   `json:"id"`
	Hosts                 []string `json:"hosts"`
	UnsupervisedAgentCode bool     `json:"unsupervisedAgentCode"`
}

// repoInfo holds the repo type and (for systems) the list of deployment hosts.
type repoInfo struct {
	Type                  conventions.RepoType
	Hosts                 []string
	UnsupervisedAgentCode bool
}

// configyComponent represents a single entry from the configy /components endpoint.
type configyComponent struct {
	ID string `json:"id"`
}

// configyScript represents a single entry from the configy /scripts endpoint.
type configyScript struct {
	ID string `json:"id"`
}

// gitHubRepo represents a single entry from the GitHub /users/{user}/repos endpoint.
type gitHubRepo struct {
	FullName string `json:"full_name"`
	Archived bool   `json:"archived"`
	Fork     bool   `json:"fork"`
}

// scheduleTrackerPayload is the JSON body sent to the schedule tracker endpoint.
type scheduleTrackerPayload struct {
	System    string `json:"system"`
	Frequency int    `json:"frequency"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
}

// AuditSweeper orchestrates scheduled full sweeps of all known repos.
type AuditSweeper struct {
	db            *DB
	githubAuth    *GitHubAuthClient
	githubOrg     string
	sweepInterval time.Duration
	system        string

	// Base URLs — overridable in tests.
	configyBaseURL          string
	githubAPIBaseURL        string
	scheduleTrackerEndpoint string

	// issueClientFactory creates a GitHubIssueClient for a given token.
	// Overridable in tests to inject a fake client.
	issueClientFactory func(token string) *GitHubIssueClient

	mu                   sync.Mutex
	lastSweepCompletedAt time.Time
	lastSweepErr         error
	sweepInProgress      bool
}

// NewAuditSweeper creates a new AuditSweeper. The sweeper does not start
// automatically — call Start to begin the scheduled loop.
func NewAuditSweeper(db *DB, githubAuth *GitHubAuthClient, system string) *AuditSweeper {
	s := &AuditSweeper{
		db:               db,
		githubAuth:       githubAuth,
		githubOrg:        "lucas42",
		sweepInterval:    6 * time.Hour,
		system:           system,
		configyBaseURL:   configyBaseURL,
		githubAPIBaseURL: githubAPIBaseURL,
	}
	s.issueClientFactory = func(token string) *GitHubIssueClient {
		return NewGitHubIssueClient(s.githubAPIBaseURL, token)
	}
	return s
}

// Start runs an immediate sweep in a goroutine, then repeats every
// sweepInterval. It is safe to call only once.
func (s *AuditSweeper) Start() {
	go func() {
		s.TriggerSweep()
		ticker := time.NewTicker(s.sweepInterval)
		defer ticker.Stop()
		for range ticker.C {
			s.TriggerSweep()
		}
	}()
}

// Status returns the time of the last successful sweep and any error from the
// most recent sweep attempt.
func (s *AuditSweeper) Status() (completedAt time.Time, lastErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastSweepCompletedAt, s.lastSweepErr
}

// TriggerSweep starts a full audit sweep in a background goroutine.
// It returns true if the sweep was started, or false if one is already in progress.
func (s *AuditSweeper) TriggerSweep() bool {
	s.mu.Lock()
	if s.sweepInProgress {
		s.mu.Unlock()
		return false
	}
	s.sweepInProgress = true
	s.mu.Unlock()
	go s.runSweep()
	return true
}

// runSweep performs one full audit sweep and records the outcome.
func (s *AuditSweeper) runSweep() {
	slog.Info("Audit sweep starting")
	start := time.Now()
	if err := s.sweep(); err != nil {
		slog.Error("Audit sweep failed", "error", err, "duration", time.Since(start))
		s.mu.Lock()
		s.lastSweepErr = err
		s.sweepInProgress = false
		s.mu.Unlock()
		s.reportToScheduleTracker("error", err.Error())
		return
	}
	slog.Info("Audit sweep completed successfully", "duration", time.Since(start))
	s.mu.Lock()
	s.lastSweepCompletedAt = time.Now()
	s.lastSweepErr = nil
	s.sweepInProgress = false
	s.mu.Unlock()
	s.reportToScheduleTracker("success", "")
}

// reportToScheduleTracker posts the sweep outcome to the schedule tracker
// endpoint if one is configured. Errors are logged but do not affect the sweep
// result.
func (s *AuditSweeper) reportToScheduleTracker(status, message string) {
	if s.scheduleTrackerEndpoint == "" {
		return
	}
	payload := scheduleTrackerPayload{
		System:    s.system,
		Frequency: int(s.sweepInterval.Seconds()),
		Status:    status,
		Message:   message,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		slog.Warn("Failed to marshal schedule tracker payload", "error", err)
		return
	}
	resp, err := http.Post(s.scheduleTrackerEndpoint, "application/json", bytes.NewReader(body)) //nolint:gosec // URL comes from trusted config
	if err != nil {
		slog.Warn("Failed to POST to schedule tracker", "error", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Warn("Schedule tracker returned non-2xx response", "status", resp.StatusCode)
	}
}

// sweep fetches repos and configy data, then runs all conventions.
// It returns an error if any convention checks were skipped due to API errors,
// so that callers can report a degraded (rather than successful) status.
// On a fully successful sweep, stale findings (for repo+convention pairs no
// longer in scope) are deleted from the database.
func (s *AuditSweeper) sweep() error {
	start := time.Now()

	// Enable in-memory response caching for the duration of this sweep.
	// This deduplicates identical GitHub API calls made by different conventions
	// against the same repo (e.g. branch protection fetched 3-5x per repo).
	// RateLimitTransport sits inside the cache so rate-limit 403s are never
	// cached — they are either retried (after waiting for reset) or surfaced
	// as distinct errors rather than being misattributed as permission failures.
	rateLimitTransport := conventions.NewRateLimitTransport(http.DefaultTransport)
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
	token, err := s.githubAuth.GetInstallationToken()
	if err != nil {
		return fmt.Errorf("failed to get GitHub token: %w", err)
	}

	repos, err := s.fetchRepos(token)
	if err != nil {
		return fmt.Errorf("failed to fetch repos: %w", err)
	}
	slog.Info("Fetched repos", "count", len(repos))

	repoInfos, err := s.fetchRepoTypes()
	if err != nil {
		// Abort the sweep entirely — acting on incomplete configy data would create
		// false-positive audit issues for every repo (e.g. false in-lucos-configy failures).
		return fmt.Errorf("failed to fetch configy data: %w", err)
	}

	allConventions := conventions.All()
	issueClient := s.issueClientFactory(token)

	// skippedCount tracks convention checks that could not be fully processed
	// due to API errors (e.g. rate limiting). A non-zero count means the sweep
	// has incomplete coverage and should not be reported as successful.
	skippedCount := 0

	for _, repo := range repos {
		// Archived repos are intentionally frozen — convention compliance is
		// irrelevant and no new issues can be filed on them anyway.
		if repo.Archived {
			slog.Debug("Skipping archived repo", "repo", repo.FullName)
			continue
		}

		// Forked repos follow the upstream owner's conventions, not ours.
		if repo.Fork {
			slog.Debug("Skipping forked repo", "repo", repo.FullName)
			continue
		}

		repoName := repo.FullName
		info, ok := repoInfos[repoName]
		if !ok {
			info = repoInfo{Type: conventions.RepoTypeUnconfigured}
		}

		if err := s.db.UpsertRepo(repoName, info.Type); err != nil {
			slog.Warn("Failed to upsert repo", "repo", repoName, "error", err)
			continue
		}

		ctx := conventions.RepoContext{
			Name:                  repoName,
			GitHubToken:           token,
			Type:                  info.Type,
			Hosts:                 info.Hosts,
			GitHubBaseURL:         s.githubAPIBaseURL,
			UnsupervisedAgentCode: info.UnsupervisedAgentCode,
		}

		for _, convention := range allConventions {
			// Skip conventions that don't apply to this repo type or this specific repo.
			if !convention.AppliesToType(info.Type) {
				continue
			}
			if !convention.AppliesToRepo(repoName) {
				continue
			}

			result := convention.Check(ctx)

			if result.Err != nil {
				// The check could not determine compliance — skip issue creation
				// and mark the sweep as incomplete so it will be retried.
				slog.Warn("Convention check indeterminate due to API error",
					"repo", repoName, "convention", convention.ID, "error", result.Err)
				skippedCount++
				continue
			}

			convInfo := ConventionInfo{
				ID:          convention.ID,
				Description: convention.Description,
				Rationale:   convention.Rationale,
				Guidance:    convention.Guidance,
				Detail:      result.Detail,
			}

			issueURL := ""
			if !result.Pass {
				// Ensure an open audit-finding issue exists for this violation.
				if os.Getenv("ENVIRONMENT") == "production" {
					var issueErr error
					issueURL, issueErr = issueClient.EnsureIssueExists(repoName, convInfo)
					if issueErr != nil {
						if isIssuesUnavailableErr(issueErr) {
							// 403/410 means issues are unavailable (archived or disabled) —
							// this is an expected state, not an API error. Log and move on.
							slog.Warn("Issues unavailable for repo, skipping issue creation",
								"repo", repoName, "convention", convention.ID, "error", issueErr)
						} else {
							slog.Warn("Failed to ensure issue exists for failing convention",
								"repo", repoName, "convention", convention.ID, "error", issueErr)
							skippedCount++
						}
					}
				} else {
					slog.Info("Skipping issue creation in non-production environment",
						"repo", repoName, "convention", convention.ID)
				}
			} else {
				// Convention passes — close any open audit-finding issue.
				if os.Getenv("ENVIRONMENT") == "production" {
					if closeErr := issueClient.CloseIssueIfOpen(repoName, convInfo); closeErr != nil {
						if isIssuesUnavailableErr(closeErr) {
							slog.Warn("Issues unavailable for repo, skipping issue close",
								"repo", repoName, "convention", convention.ID, "error", closeErr)
						} else {
							// Close failure does not invalidate the sweep result — the
							// convention check succeeded. Log and continue.
							slog.Warn("Failed to close audit-finding issue for passing convention",
								"repo", repoName, "convention", convention.ID, "error", closeErr)
						}
					}
				} else {
					slog.Debug("Skipping issue close in non-production environment",
						"repo", repoName, "convention", convention.ID)
				}
			}

			if err := s.db.SaveFinding(result, repoName, issueURL); err != nil {
				slog.Warn("Failed to save finding", "repo", repoName, "convention", convention.ID, "error", err)
			}
		}
	}

	if skippedCount > 0 {
		return fmt.Errorf("sweep incomplete: %d convention check(s) skipped due to API errors", skippedCount)
	}

	// Clean up findings for repo+convention pairs no longer in scope.
	// Only runs after a fully successful sweep to avoid deleting findings
	// that were merely skipped due to transient API errors.
	if err := s.db.DeleteStaleFindings(start); err != nil {
		slog.Warn("Failed to clean up stale findings", "error", err)
	}

	return nil
}

// fetchRepos fetches the full list of repos in the GitHub org, handling pagination.
// It handles rate limit responses by waiting for the reset window (up to
// rateLimitMaxWait) and retrying the affected page once.
func (s *AuditSweeper) fetchRepos(token string) ([]gitHubRepo, error) {
	var allRepos []gitHubRepo
	page := 1
	const perPage = 100

	for {
		pageRepos, err := s.fetchReposPage(token, page, perPage)
		if err != nil {
			return nil, err
		}
		allRepos = append(allRepos, pageRepos...)
		if len(pageRepos) < perPage {
			break
		}
		page++
	}

	return allRepos, nil
}

// fetchReposPage fetches a single page of repos and handles rate limit responses
// by waiting and retrying once.
func (s *AuditSweeper) fetchReposPage(token string, page, perPage int) ([]gitHubRepo, error) {
	pageURL := fmt.Sprintf("%s/users/%s/repos?per_page=%d&page=%d", s.githubAPIBaseURL, s.githubOrg, perPage, page)

	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequest("GET", pageURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to build repos request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("GitHub repos request failed: %w", err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read repos response: %w", err)
		}

		if resp.StatusCode == http.StatusForbidden {
			if waitErr := handleRateLimitError(resp, body); waitErr != nil {
				return nil, waitErr
			}
			// Rate limit wait succeeded — loop to retry.
			continue
		}

		checkRateLimitHeaders(resp)

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GitHub repos API returned %d", resp.StatusCode)
		}

		var pageRepos []gitHubRepo
		if err := json.Unmarshal(body, &pageRepos); err != nil {
			return nil, fmt.Errorf("failed to decode repos response: %w", err)
		}
		return pageRepos, nil
	}

	return nil, fmt.Errorf("GitHub repos API: rate limit retry exhausted")
}

// fetchRepoTypes fetches systems, components, and scripts from lucos_configy
// and returns a map of repo full_name (e.g. "lucas42/lucos_photos") to repoInfo.
func (s *AuditSweeper) fetchRepoTypes() (map[string]repoInfo, error) {
	result := map[string]repoInfo{}

	systems, err := s.fetchConfigySystems()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch configy systems: %w", err)
	}
	for _, sys := range systems {
		result[s.githubOrg+"/"+sys.ID] = repoInfo{
			Type:                  conventions.RepoTypeSystem,
			Hosts:                 sys.Hosts,
			UnsupervisedAgentCode: sys.UnsupervisedAgentCode,
		}
	}

	components, err := s.fetchConfigyComponents()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch configy components: %w", err)
	}
	for _, comp := range components {
		key := s.githubOrg + "/" + comp.ID
		if _, exists := result[key]; exists {
			// Already classified under another type — mark as duplicate.
			result[key] = repoInfo{Type: conventions.RepoTypeDuplicate}
		} else {
			result[key] = repoInfo{Type: conventions.RepoTypeComponent}
		}
	}

	scripts, err := s.fetchConfigyScripts()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch configy scripts: %w", err)
	}
	for _, script := range scripts {
		key := s.githubOrg + "/" + script.ID
		if _, exists := result[key]; exists {
			// Already classified under another type — mark as duplicate.
			result[key] = repoInfo{Type: conventions.RepoTypeDuplicate}
		} else {
			result[key] = repoInfo{Type: conventions.RepoTypeScript}
		}
	}

	return result, nil
}

// fetchConfigySystems fetches the list of systems from the configy API.
func (s *AuditSweeper) fetchConfigySystems() ([]configySystem, error) {
	url := s.configyBaseURL + "/systems"
	resp, err := http.Get(url) //nolint:gosec // URL comes from trusted config
	if err != nil {
		return nil, fmt.Errorf("configy systems request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("configy /systems returned %d", resp.StatusCode)
	}

	var systems []configySystem
	if err := json.NewDecoder(resp.Body).Decode(&systems); err != nil {
		return nil, fmt.Errorf("failed to decode configy systems: %w", err)
	}
	return systems, nil
}

// fetchConfigyComponents fetches the list of components from the configy API.
func (s *AuditSweeper) fetchConfigyComponents() ([]configyComponent, error) {
	url := s.configyBaseURL + "/components"
	resp, err := http.Get(url) //nolint:gosec // URL comes from trusted config
	if err != nil {
		return nil, fmt.Errorf("configy components request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("configy /components returned %d", resp.StatusCode)
	}

	var components []configyComponent
	if err := json.NewDecoder(resp.Body).Decode(&components); err != nil {
		return nil, fmt.Errorf("failed to decode configy components: %w", err)
	}
	return components, nil
}

// fetchConfigyScripts fetches the list of scripts from the configy API.
func (s *AuditSweeper) fetchConfigyScripts() ([]configyScript, error) {
	url := s.configyBaseURL + "/scripts"
	resp, err := http.Get(url) //nolint:gosec // URL comes from trusted config
	if err != nil {
		return nil, fmt.Errorf("configy scripts request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("configy /scripts returned %d", resp.StatusCode)
	}

	var scripts []configyScript
	if err := json.NewDecoder(resp.Body).Decode(&scripts); err != nil {
		return nil, fmt.Errorf("failed to decode configy scripts: %w", err)
	}
	return scripts, nil
}

// isIssuesUnavailableErr returns true if err wraps ErrIssuesUnavailable —
// meaning the GitHub Issues API returned 403 (archived/read-only) or 410
// (issues disabled). These are expected states and should not cause the sweep
// to be reported as incomplete.
func isIssuesUnavailableErr(err error) bool {
	return errors.Is(err, ErrIssuesUnavailable)
}
