package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
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
	ID    string   `json:"id"`
	Hosts []string `json:"hosts"`
}

// repoInfo holds the repo type and (for systems) the list of deployment hosts.
type repoInfo struct {
	Type  conventions.RepoType
	Hosts []string
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
		s.runSweep()
		ticker := time.NewTicker(s.sweepInterval)
		defer ticker.Stop()
		for range ticker.C {
			s.runSweep()
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

// runSweep performs one full audit sweep and records the outcome.
func (s *AuditSweeper) runSweep() {
	slog.Info("Audit sweep starting")
	start := time.Now()
	if err := s.sweep(); err != nil {
		slog.Error("Audit sweep failed", "error", err, "duration", time.Since(start))
		s.mu.Lock()
		s.lastSweepErr = err
		s.mu.Unlock()
		s.reportToScheduleTracker("error", err.Error())
		return
	}
	slog.Info("Audit sweep completed successfully", "duration", time.Since(start))
	s.mu.Lock()
	s.lastSweepCompletedAt = time.Now()
	s.lastSweepErr = nil
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
func (s *AuditSweeper) sweep() error {
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
		// Non-fatal — we proceed with all repos typed as unconfigured.
		slog.Warn("Failed to fetch configy data; treating all repos as unconfigured", "error", err)
		repoInfos = map[string]repoInfo{}
	}

	allConventions := conventions.All()
	issueClient := s.issueClientFactory(token)

	for _, repoName := range repos {
		info, ok := repoInfos[repoName]
		if !ok {
			info = repoInfo{Type: conventions.RepoTypeUnconfigured}
		}

		if err := s.db.UpsertRepo(repoName); err != nil {
			slog.Warn("Failed to upsert repo", "repo", repoName, "error", err)
			continue
		}

		ctx := conventions.RepoContext{
			Name:          repoName,
			GitHubToken:   token,
			Type:          info.Type,
			Hosts:         info.Hosts,
			GitHubBaseURL: s.githubAPIBaseURL,
		}

		for _, convention := range allConventions {
			// Skip conventions that don't apply to this repo type.
			if !convention.AppliesToType(info.Type) {
				continue
			}

			result := convention.Check(ctx)

			issueURL := ""
			if !result.Pass {
				// Ensure an open audit-finding issue exists for this violation.
				convInfo := ConventionInfo{
					ID:          convention.ID,
					Description: convention.Description,
					Rationale:   convention.Rationale,
					Guidance:    convention.Guidance,
				}
				var issueErr error
				issueURL, issueErr = issueClient.EnsureIssueExists(repoName, convInfo)
				if issueErr != nil {
					slog.Warn("Failed to ensure issue exists for failing convention",
						"repo", repoName, "convention", convention.ID, "error", issueErr)
				}
			}

			if err := s.db.SaveFinding(result, repoName, issueURL); err != nil {
				slog.Warn("Failed to save finding", "repo", repoName, "convention", convention.ID, "error", err)
			}
		}
	}

	return nil
}

// fetchRepos fetches the full list of repos in the GitHub org, handling pagination.
func (s *AuditSweeper) fetchRepos(token string) ([]string, error) {
	var allRepos []string
	page := 1
	const perPage = 100

	for {
		url := fmt.Sprintf("%s/users/%s/repos?per_page=%d&page=%d", s.githubAPIBaseURL, s.githubOrg, perPage, page)
		req, err := http.NewRequest("GET", url, nil)
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

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GitHub repos API returned %d", resp.StatusCode)
		}

		var pageRepos []gitHubRepo
		if err := json.Unmarshal(body, &pageRepos); err != nil {
			return nil, fmt.Errorf("failed to decode repos response: %w", err)
		}

		for _, r := range pageRepos {
			allRepos = append(allRepos, r.FullName)
		}

		if len(pageRepos) < perPage {
			break
		}
		page++
	}

	return allRepos, nil
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
			Type:  conventions.RepoTypeSystem,
			Hosts: sys.Hosts,
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
