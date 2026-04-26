package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"
)

//go:embed templates/prs.html.tmpl
var prTemplateFS embed.FS

var prDashboardTemplate = template.Must(
	template.New("prs.html.tmpl").ParseFS(prTemplateFS, "templates/prs.html.tmpl"),
)

// PRState classifies a PR by its check and review status.
type PRState int

const (
	PRStateFailingChecks   PRState = iota // At least one check has failed
	PRStatePendingChecks                  // Checks pending, none failed
	PRStateNoReviews                      // All checks pass, no reviews
	PRStateChangesRequested               // All checks pass, unresolved changes requested
	PRStateBotApprovedOnly                // All checks pass, code-reviewer[bot] approved but not lucas42
	PRStateFullyApproved                  // All checks pass, approved by both code-reviewer[bot] and lucas42
)

// RepoPRCounts holds the PR counts for a single repo.
type RepoPRCounts struct {
	RepoName         string
	FailingChecks    int
	PendingChecks    int
	NoReviews        int
	ChangesRequested int
	BotApprovedOnly  int
	FullyApproved    int
	Total            int
}

// PRDashboardData is passed to the HTML template.
type PRDashboardData struct {
	Repos               []RepoPRCounts
	LastFetchAt         time.Time
	StaleDependabotPRs  []StaleDependabotPR `json:"stale_dependabot_prs,omitempty"`
}

// staleDependabotThreshold is how long a Dependabot PR must be open before
// it is considered stale and reported as a health signal failure.
const staleDependabotThreshold = 48 * time.Hour

// StaleDependabotPR holds minimal info about a stale unmerged Dependabot PR.
type StaleDependabotPR struct {
	Repo      string    `json:"repo"`
	Number    int       `json:"number"`
	CreatedAt time.Time `json:"created_at"`
}

// ghPR is a subset of the GitHub pull request API response.
type ghPR struct {
	Number    int       `json:"number"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"created_at"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
}

// ghCombinedStatus is a subset of the GitHub combined status API response.
type ghCombinedStatus struct {
	State    string         `json:"state"` // "success", "failure", "pending"
	Statuses []ghStatusItem `json:"statuses"`
}

type ghStatusItem struct {
	State string `json:"state"` // "success", "error", "failure", "pending"
}

// ghCheckRuns is a subset of the GitHub check runs API response.
type ghCheckRuns struct {
	CheckRuns []ghCheckRun `json:"check_runs"`
}

type ghCheckRun struct {
	Status     string  `json:"status"`     // "queued", "in_progress", "completed"
	Conclusion *string `json:"conclusion"` // "success", "failure", "neutral", etc. (null when not completed)
}

// ghReview is a subset of the GitHub review API response.
type ghReview struct {
	User  ghReviewUser `json:"user"`
	State string       `json:"state"` // "APPROVED", "CHANGES_REQUESTED", "DISMISSED", "COMMENTED"
}

type ghReviewUser struct {
	Login string `json:"login"`
}

// PRSweeper periodically fetches open PRs for all repos.
type PRSweeper struct {
	githubAuth      *GitHubAuthClient
	githubOrg       string
	githubAPIBase   string
	sweepInterval   time.Duration

	mu              sync.RWMutex
	data            PRDashboardData
	sweepInProgress bool
}

// NewPRSweeper creates a new PRSweeper.
func NewPRSweeper(githubAuth *GitHubAuthClient) *PRSweeper {
	return &PRSweeper{
		githubAuth:    githubAuth,
		githubOrg:     "lucas42",
		githubAPIBase: githubAPIBaseURL,
		sweepInterval: 6 * time.Hour,
	}
}

// Start begins the periodic PR sweep.
func (p *PRSweeper) Start() {
	go func() {
		p.TriggerSweep()
		ticker := time.NewTicker(p.sweepInterval)
		defer ticker.Stop()
		for range ticker.C {
			p.TriggerSweep()
		}
	}()
}

// TriggerSweep starts a PR sweep in a background goroutine.
// It returns true if the sweep was started, or false if one is already in progress.
func (p *PRSweeper) TriggerSweep() bool {
	p.mu.Lock()
	if p.sweepInProgress {
		p.mu.Unlock()
		return false
	}
	p.sweepInProgress = true
	p.mu.Unlock()
	go p.runSweep()
	return true
}

// Data returns the current PR dashboard data.
func (p *PRSweeper) Data() PRDashboardData {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.data
}

func (p *PRSweeper) runSweep() {
	defer func() {
		p.mu.Lock()
		p.sweepInProgress = false
		p.mu.Unlock()
	}()

	slog.Info("PR sweep starting")
	start := time.Now()

	token, err := p.githubAuth.GetInstallationToken()
	if err != nil {
		slog.Error("PR sweep: failed to get token", "error", err)
		return
	}

	repos, err := p.fetchAllRepos(token)
	if err != nil {
		slog.Error("PR sweep: failed to fetch repos", "error", err)
		return
	}

	var results []RepoPRCounts
	var staleDependabotPRs []StaleDependabotPR
	for _, repo := range repos {
		if repo.Archived || repo.Fork {
			continue
		}
		counts, stale := p.fetchRepoPRCounts(token, repo.FullName)
		if counts.Total > 0 {
			results = append(results, counts)
		}
		staleDependabotPRs = append(staleDependabotPRs, stale...)
	}

	// Sort by total PRs descending.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Total > results[j].Total
	})
	// Sort stale Dependabot PRs oldest-first so the oldest is easy to find.
	sort.Slice(staleDependabotPRs, func(i, j int) bool {
		return staleDependabotPRs[i].CreatedAt.Before(staleDependabotPRs[j].CreatedAt)
	})

	p.mu.Lock()
	p.data = PRDashboardData{
		Repos:              results,
		LastFetchAt:        time.Now(),
		StaleDependabotPRs: staleDependabotPRs,
	}
	p.mu.Unlock()

	slog.Info("PR sweep completed", "repos_with_prs", len(results), "stale_dependabot_prs", len(staleDependabotPRs), "duration", time.Since(start))
}

func (p *PRSweeper) fetchAllRepos(token string) ([]gitHubRepo, error) {
	var allRepos []gitHubRepo
	page := 1
	const perPage = 100

	for {
		url := fmt.Sprintf("%s/users/%s/repos?per_page=%d&page=%d", p.githubAPIBase, p.githubOrg, perPage, page)
		body, err := p.githubGet(token, url)
		if err != nil {
			return nil, err
		}
		var pageRepos []gitHubRepo
		if err := json.Unmarshal(body, &pageRepos); err != nil {
			return nil, fmt.Errorf("failed to decode repos page: %w", err)
		}
		allRepos = append(allRepos, pageRepos...)
		if len(pageRepos) < perPage {
			break
		}
		page++
	}
	return allRepos, nil
}

func (p *PRSweeper) fetchRepoPRCounts(token, repoName string) (RepoPRCounts, []StaleDependabotPR) {
	counts := RepoPRCounts{RepoName: repoName}
	var stalePRs []StaleDependabotPR

	// Fetch all open PRs with pagination.
	var prs []ghPR
	page := 1
	const perPage = 100
	for {
		url := fmt.Sprintf("%s/repos/%s/pulls?state=open&per_page=%d&page=%d", p.githubAPIBase, repoName, perPage, page)
		body, err := p.githubGet(token, url)
		if err != nil {
			slog.Warn("PR sweep: failed to fetch PRs", "repo", repoName, "page", page, "error", err)
			return counts, stalePRs
		}

		var pagePRs []ghPR
		if err := json.Unmarshal(body, &pagePRs); err != nil {
			slog.Warn("PR sweep: failed to decode PRs", "repo", repoName, "error", err)
			return counts, stalePRs
		}
		prs = append(prs, pagePRs...)
		if len(pagePRs) < perPage {
			break
		}
		page++
	}

	counts.Total = len(prs)

	for _, pr := range prs {
		state := p.classifyPR(token, repoName, pr.Number)
		switch state {
		case PRStateFailingChecks:
			counts.FailingChecks++
		case PRStatePendingChecks:
			counts.PendingChecks++
		case PRStateNoReviews:
			counts.NoReviews++
		case PRStateChangesRequested:
			counts.ChangesRequested++
		case PRStateBotApprovedOnly:
			counts.BotApprovedOnly++
		case PRStateFullyApproved:
			counts.FullyApproved++
		}

		// Detect stale Dependabot PRs regardless of check/review state.
		if pr.User.Login == "dependabot[bot]" && time.Since(pr.CreatedAt) > staleDependabotThreshold {
			stalePRs = append(stalePRs, StaleDependabotPR{
				Repo:      repoName,
				Number:    pr.Number,
				CreatedAt: pr.CreatedAt,
			})
		}
	}

	return counts, stalePRs
}

func (p *PRSweeper) classifyPR(token, repoName string, prNumber int) PRState {
	// Check combined status + check runs.
	checksPassing, checksPending := p.getCheckStatus(token, repoName, prNumber)
	if !checksPassing && !checksPending {
		return PRStateFailingChecks
	}
	if checksPending {
		return PRStatePendingChecks
	}

	// All checks pass — now check reviews.
	reviews := p.getReviews(token, repoName, prNumber)
	if reviews == nil {
		return PRStateNoReviews
	}

	// Determine latest review state per reviewer.
	latestReview := make(map[string]string)
	for _, r := range reviews {
		if r.State == "COMMENTED" || r.State == "DISMISSED" {
			continue
		}
		latestReview[r.User.Login] = r.State
	}

	// Check for unresolved changes requested.
	for _, state := range latestReview {
		if state == "CHANGES_REQUESTED" {
			return PRStateChangesRequested
		}
	}

	botApproved := latestReview["code-reviewer[bot]"] == "APPROVED" ||
		latestReview["lucos-code-reviewer[bot]"] == "APPROVED"
	ownerApproved := latestReview["lucas42"] == "APPROVED"

	if botApproved && ownerApproved {
		return PRStateFullyApproved
	}
	if botApproved {
		return PRStateBotApprovedOnly
	}

	return PRStateNoReviews
}

// getCheckStatus returns (passing, pending) booleans.
// passing=true means all checks passed. pending=true means some are still running.
// If neither is true, at least one check has failed.
func (p *PRSweeper) getCheckStatus(token, repoName string, prNumber int) (passing, pending bool) {
	// Fetch combined status for the PR head.
	url := fmt.Sprintf("%s/repos/%s/commits/refs/pull/%d/head/status", p.githubAPIBase, repoName, prNumber)
	body, err := p.githubGet(token, url)
	if err != nil {
		// On error, treat as pending to avoid false negatives.
		return false, true
	}

	var combined ghCombinedStatus
	if err := json.Unmarshal(body, &combined); err != nil {
		return false, true
	}

	// Also fetch check runs.
	crURL := fmt.Sprintf("%s/repos/%s/commits/refs/pull/%d/head/check-runs?per_page=100", p.githubAPIBase, repoName, prNumber)
	crBody, crErr := p.githubGet(token, crURL)
	if crErr != nil {
		slog.Warn("PR sweep: failed to fetch check runs", "repo", repoName, "pr", prNumber, "error", crErr)
	}

	hasFailure := false
	hasPending := false

	// Process commit statuses.
	for _, s := range combined.Statuses {
		switch s.State {
		case "failure", "error":
			hasFailure = true
		case "pending":
			hasPending = true
		}
	}

	// Process check runs.
	if crErr == nil {
		var runs ghCheckRuns
		if json.Unmarshal(crBody, &runs) == nil {
			for _, run := range runs.CheckRuns {
				if run.Status != "completed" {
					hasPending = true
				} else if run.Conclusion != nil {
					switch *run.Conclusion {
					case "failure", "timed_out", "cancelled", "action_required":
						hasFailure = true
					}
				}
			}
		}
	}

	if hasFailure {
		return false, false
	}
	if hasPending {
		return false, true
	}
	return true, false
}

func (p *PRSweeper) getReviews(token, repoName string, prNumber int) []ghReview {
	url := fmt.Sprintf("%s/repos/%s/pulls/%d/reviews?per_page=100", p.githubAPIBase, repoName, prNumber)
	body, err := p.githubGet(token, url)
	if err != nil {
		return nil
	}
	var reviews []ghReview
	if err := json.Unmarshal(body, &reviews); err != nil {
		return nil
	}
	return reviews
}

func (p *PRSweeper) githubGet(token, url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d for %s", resp.StatusCode, url)
	}
	return body, nil
}

// newPRDashboardHandler returns the GET /prs handler.
func newPRDashboardHandler(sweeper *PRSweeper) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := sweeper.Data()

		w.Header().Set("Vary", "Accept")

		if wantsJSON(r) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(data)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := prDashboardTemplate.Execute(w, data); err != nil {
			slog.Error("Failed to render PR dashboard template", "error", err)
		}
	}
}
