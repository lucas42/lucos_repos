package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// RepoType categorises a repository based on its presence in lucos_configy.
type RepoType string

const (
	// RepoTypeSystem is a repo that appears in configy's systems list.
	RepoTypeSystem RepoType = "system"

	// RepoTypeComponent is a repo that appears in configy's components list.
	RepoTypeComponent RepoType = "component"

	// RepoTypeUnconfigured is a repo not found in configy at all.
	RepoTypeUnconfigured RepoType = "unconfigured"
)

// configyBaseURL is the base URL for the lucos_configy API. It can be
// overridden in tests via AuditSweeper.configyBaseURL.
const configyBaseURL = "https://configy.l42.eu"

// githubAPIBaseURL is the base URL for the GitHub API used by AuditSweeper.
// It can be overridden in tests via AuditSweeper.githubAPIBaseURL.
const githubAPIBaseURL = "https://api.github.com"

// configySystem represents a single entry from the configy /systems endpoint.
type configySystem struct {
	ID string `json:"id"`
}

// configyComponent represents a single entry from the configy /components endpoint.
type configyComponent struct {
	ID string `json:"id"`
}

// gitHubRepo represents a single entry from the GitHub /orgs/{org}/repos endpoint.
type gitHubRepo struct {
	FullName string `json:"full_name"`
}

// AuditSweeper orchestrates scheduled full sweeps of all known repos.
type AuditSweeper struct {
	db            *DB
	githubAuth    *GitHubAuthClient
	githubOrg     string
	sweepInterval time.Duration

	// Base URLs — overridable in tests.
	configyBaseURL   string
	githubAPIBaseURL string

	// issueClientFactory creates a GitHubIssueClient for a given token.
	// Overridable in tests to inject a fake client.
	issueClientFactory func(token string) *GitHubIssueClient

	mu                   sync.Mutex
	lastSweepCompletedAt time.Time
	lastSweepErr         error
}

// NewAuditSweeper creates a new AuditSweeper. The sweeper does not start
// automatically — call Start to begin the scheduled loop.
func NewAuditSweeper(db *DB, githubAuth *GitHubAuthClient) *AuditSweeper {
	s := &AuditSweeper{
		db:               db,
		githubAuth:       githubAuth,
		githubOrg:        "lucas42",
		sweepInterval:    6 * time.Hour,
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
		return
	}
	slog.Info("Audit sweep completed successfully", "duration", time.Since(start))
	s.mu.Lock()
	s.lastSweepCompletedAt = time.Now()
	s.lastSweepErr = nil
	s.mu.Unlock()
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

	repoTypes, err := s.fetchRepoTypes()
	if err != nil {
		// Non-fatal — we proceed with all repos typed as unconfigured.
		slog.Warn("Failed to fetch configy data; treating all repos as unconfigured", "error", err)
		repoTypes = map[string]RepoType{}
	}

	conventions := AllConventions()
	issueClient := s.issueClientFactory(token)

	for _, repoName := range repos {
		repoType, ok := repoTypes[repoName]
		if !ok {
			repoType = RepoTypeUnconfigured
		}

		if err := s.db.UpsertRepo(repoName); err != nil {
			slog.Warn("Failed to upsert repo", "repo", repoName, "error", err)
			continue
		}

		ctx := RepoContext{
			Name:          repoName,
			GitHubToken:   token,
			Type:          repoType,
			GitHubBaseURL: s.githubAPIBaseURL,
		}

		for _, convention := range conventions {
			// Skip conventions that don't apply to this repo type.
			if !convention.AppliesToType(repoType) {
				continue
			}

			result := convention.Check(ctx)

			issueURL := ""
			if !result.Pass {
				// Ensure an open audit-finding issue exists for this violation.
				var issueErr error
				issueURL, issueErr = issueClient.EnsureIssueExists(repoName, convention.ID, convention.Description)
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
		url := fmt.Sprintf("%s/orgs/%s/repos?per_page=%d&page=%d", s.githubAPIBaseURL, s.githubOrg, perPage, page)
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

// fetchRepoTypes fetches systems and components from lucos_configy and returns
// a map of repo full_name (e.g. "lucas42/lucos_photos") to RepoType.
func (s *AuditSweeper) fetchRepoTypes() (map[string]RepoType, error) {
	result := map[string]RepoType{}

	systems, err := s.fetchConfigySystems()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch configy systems: %w", err)
	}
	for _, sys := range systems {
		result[s.githubOrg+"/"+sys.ID] = RepoTypeSystem
	}

	components, err := s.fetchConfigyComponents()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch configy components: %w", err)
	}
	for _, comp := range components {
		// A repo that is both a system and a component keeps its system type.
		if _, exists := result[s.githubOrg+"/"+comp.ID]; !exists {
			result[s.githubOrg+"/"+comp.ID] = RepoTypeComponent
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
