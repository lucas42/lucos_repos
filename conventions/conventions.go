package conventions

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strings"
)

// httpClient is the HTTP client used by all convention helper functions.
// It defaults to http.DefaultClient. Use SetHTTPClient to override it
// with a caching transport during sweeps.
var httpClient = http.DefaultClient

// SetHTTPClient sets the HTTP client used by convention helper functions.
// Pass nil to reset to http.DefaultClient. This is not safe for concurrent
// use — call it before starting a sweep, not during one.
func SetHTTPClient(c *http.Client) {
	if c == nil {
		httpClient = http.DefaultClient
	} else {
		httpClient = c
	}
}

// RepoType categorises a repository based on its presence in lucos_configy.
type RepoType string

const (
	// RepoTypeSystem is a repo that appears in configy's systems list.
	RepoTypeSystem RepoType = "system"

	// RepoTypeComponent is a repo that appears in configy's components list.
	RepoTypeComponent RepoType = "component"

	// RepoTypeScript is a repo that appears in configy's scripts list.
	// Scripts are tools designed to run locally rather than being deployed.
	RepoTypeScript RepoType = "script"

	// RepoTypeUnconfigured is a repo not found in configy at all.
	RepoTypeUnconfigured RepoType = "unconfigured"

	// RepoTypeDuplicate is a repo that appears in more than one configy list
	// (e.g. both systems and components). This is a configuration error — a
	// repo should belong to exactly one configy category.
	RepoTypeDuplicate RepoType = "duplicate"
)

// RepoContext contains the information available to a convention check function.
type RepoContext struct {
	// Name is the full repository name, e.g. "lucas42/lucos_photos".
	Name string

	// GitHubToken is a valid GitHub App installation token for making API calls.
	GitHubToken string

	// Type is the repo's classification as determined by lucos_configy.
	Type RepoType

	// Hosts is the list of deployment hosts for this repo, as listed in
	// lucos_configy. Only populated for system repos; nil for all other types.
	Hosts []string

	// GitHubBaseURL is the base URL for GitHub API calls. Defaults to
	// GitHubBaseURL ("https://api.github.com") when empty.
	GitHubBaseURL string

	// UnsupervisedAgentCode indicates that this system repo has autonomous
	// agent code enabled (unsupervisedAgentCode=true in lucos_configy).
	// Only meaningful for RepoTypeSystem; always false for other types.
	UnsupervisedAgentCode bool

	// Ref is an optional git ref (branch name or SHA) to check content against.
	// When set, content-fetching helpers append ?ref=X to GitHub Contents API calls.
	// Settings-based checks (branch protection, required status checks) ignore it.
	// When empty, the repo's default branch is used.
	Ref string
}

// ConventionResult is the outcome of running a single convention against a repo.
type ConventionResult struct {
	// Convention is the ID of the convention that was checked.
	Convention string

	// Pass is true if the repo satisfies the convention.
	// Ignored when Err is non-nil.
	Pass bool

	// Detail provides human-readable context about the result (e.g. why it failed).
	Detail string

	// Err is non-nil when the check could not determine compliance due to a
	// transient error (e.g. a 5xx response from the GitHub API). An errored
	// result is not a convention failure — the sweep should skip issue creation
	// and mark the sweep as incomplete so it will be retried.
	Err error
}

// Convention defines a rule that repos are expected to follow.
type Convention struct {
	// ID is a short, stable identifier used in the database and issue titles.
	ID string

	// Description explains what the convention checks, in plain English.
	Description string

	// Rationale explains why this convention exists — what goes wrong if it is
	// violated. This appears in generated issue bodies to give the reader context.
	Rationale string

	// Guidance suggests approaches to fix the violation. This appears in
	// generated issue bodies to help the reader act on the issue.
	Guidance string

	// AppliesTo is the set of repo types this convention applies to. If empty,
	// the convention applies to all repo types.
	AppliesTo []RepoType

	// ExcludeRepos is a set of specific repo full names (e.g.
	// "lucas42/lucos_deploy_orb") that are exempt from this convention.
	// Use this sparingly — only when a repo has a legitimate structural reason
	// why the convention cannot apply (e.g. it defines the thing the convention
	// requires, which would create a circular dependency).
	ExcludeRepos []string

	// ScheduledOnly means this convention should only run during scheduled
	// sweeps, not during PR audits. When true, the audit handler skips this
	// convention when a ref parameter is present (i.e. PR mode).
	ScheduledOnly bool

	// Check runs the convention against a repo and returns the result.
	Check func(repo RepoContext) ConventionResult
}

// AppliesToType reports whether the convention applies to the given repo type.
// A convention with no AppliesTo set applies to every repo type.
func (c Convention) AppliesToType(t RepoType) bool {
	if len(c.AppliesTo) == 0 {
		return true
	}
	for _, allowed := range c.AppliesTo {
		if allowed == t {
			return true
		}
	}
	return false
}

// AppliesToRepo reports whether the convention applies to the given repo full
// name (e.g. "lucas42/lucos_photos"). A convention with no ExcludeRepos set
// applies to every repo.
func (c Convention) AppliesToRepo(name string) bool {
	for _, excluded := range c.ExcludeRepos {
		if excluded == name {
			return false
		}
	}
	return true
}

// registry holds all registered conventions. Conventions are added at init time
// by calling Register. The order is preserved for display purposes.
var registry []Convention

// Register adds a convention to the global registry.
func Register(c Convention) {
	registry = append(registry, c)
}

// All returns a copy of the registered conventions slice.
func All() []Convention {
	result := make([]Convention, len(registry))
	copy(result, registry)
	return result
}

// GitHubBaseURL is the base URL for the GitHub API. It can be overridden in
// tests using GitHubFileExistsFromBase or GitHubRequiredStatusChecksFromBase.
const GitHubBaseURL = "https://api.github.com"

// GitHubFileExists checks whether a file exists in a GitHub repository at the
// given path. It uses the Contents API (checking for 200 vs 404) to determine
// existence.
func GitHubFileExists(token, repo, path string) (bool, error) {
	return GitHubFileExistsFromBase(GitHubBaseURL, token, repo, path)
}

// GitHubFileExistsFromBase is the implementation of GitHubFileExists with an
// injectable base URL, used by tests to point at a fake server.
func GitHubFileExistsFromBase(baseURL, token, repo, path string, ref ...string) (bool, error) {
	url := fmt.Sprintf("%s/repos/%s/contents/%s", baseURL, repo, path)
	if len(ref) > 0 && ref[0] != "" {
		url += "?ref=" + neturl.QueryEscape(ref[0])
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return false, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("unexpected GitHub API status %d for %s in %s", resp.StatusCode, path, repo)
	}
}

// gitHubContentsResponse is the subset of the GitHub Contents API response
// that we care about when fetching a file's content.
type gitHubContentsResponse struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

// GitHubFileContent fetches the decoded content of a file from a GitHub
// repository. It returns (nil, nil) if the file does not exist.
func GitHubFileContent(token, repo, path string) ([]byte, error) {
	return GitHubFileContentFromBase(GitHubBaseURL, token, repo, path)
}

// GitHubFileContentFromBase is the implementation of GitHubFileContent with an
// injectable base URL, used by tests to point at a fake server.
func GitHubFileContentFromBase(baseURL, token, repo, path string, ref ...string) ([]byte, error) {
	url := fmt.Sprintf("%s/repos/%s/contents/%s", baseURL, repo, path)
	if len(ref) > 0 && ref[0] != "" {
		url += "?ref=" + neturl.QueryEscape(ref[0])
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	switch resp.StatusCode {
	case http.StatusOK:
		var contents gitHubContentsResponse
		if err := json.NewDecoder(resp.Body).Decode(&contents); err != nil {
			return nil, fmt.Errorf("failed to decode contents response: %w", err)
		}
		if contents.Encoding != "base64" {
			return nil, fmt.Errorf("unexpected encoding %q for %s in %s", contents.Encoding, path, repo)
		}
		// GitHub wraps the base64 content in newlines — strip them before decoding.
		decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(contents.Content, "\n", ""))
		if err != nil {
			return nil, fmt.Errorf("failed to base64-decode content of %s in %s: %w", path, repo, err)
		}
		return decoded, nil
	case http.StatusNotFound:
		return nil, nil
	default:
		return nil, fmt.Errorf("unexpected GitHub API status %d for %s in %s", resp.StatusCode, path, repo)
	}
}

// branchProtectionResponse is the subset of the GitHub branch protection API
// response that we care about.
type branchProtectionResponse struct {
	RequiredStatusChecks *struct {
		// Strict corresponds to "Require branches to be up to date before merging".
		// When true, PRs must be rebased onto the latest main before merging.
		// This blocks Dependabot PRs from merging when more than one is open.
		Strict bool `json:"strict"`
		// Contexts is the legacy field populated by older GitHub UI and API calls.
		Contexts []string `json:"contexts"`
		// Checks is the modern field populated by the current GitHub UI.
		// Each entry has a "context" field with the check name and an optional "app_id".
		Checks []struct {
			Context string `json:"context"`
		} `json:"checks"`
	} `json:"required_status_checks"`
	// RequiredPullRequestReviews is non-nil when "Require approvals" is enabled.
	// A nil value means the setting is disabled.
	RequiredPullRequestReviews *struct{} `json:"required_pull_request_reviews"`
}

// GitHubBranchProtectionDetails fetches and parses the branch protection rules
// for the given branch. It returns (nil, nil) when the branch is unprotected.
func GitHubBranchProtectionDetails(token, repo, branch string) (*branchProtectionResponse, error) {
	return GitHubBranchProtectionDetailsFromBase(GitHubBaseURL, token, repo, branch)
}

// GitHubBranchProtectionDetailsFromBase is the implementation of
// GitHubBranchProtectionDetails with an injectable base URL.
func GitHubBranchProtectionDetailsFromBase(baseURL, token, repo, branch string) (*branchProtectionResponse, error) {
	url := fmt.Sprintf("%s/repos/%s/branches/%s/protection", baseURL, repo, branch)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	switch resp.StatusCode {
	case http.StatusOK:
		var protection branchProtectionResponse
		if err := json.NewDecoder(resp.Body).Decode(&protection); err != nil {
			return nil, fmt.Errorf("failed to decode branch protection response: %w", err)
		}
		return &protection, nil
	case http.StatusNotFound:
		return nil, nil
	default:
		return nil, fmt.Errorf("unexpected GitHub API status %d fetching branch protection for %s in %s", resp.StatusCode, branch, repo)
	}
}

// GitHubBranchProtectionEnabled returns true if the given branch has protection
// rules enabled. It returns false (not an error) when the branch is unprotected.
func GitHubBranchProtectionEnabled(token, repo, branch string) (bool, error) {
	return GitHubBranchProtectionEnabledFromBase(GitHubBaseURL, token, repo, branch)
}

// GitHubBranchProtectionEnabledFromBase is the implementation of
// GitHubBranchProtectionEnabled with an injectable base URL.
func GitHubBranchProtectionEnabledFromBase(baseURL, token, repo, branch string) (bool, error) {
	url := fmt.Sprintf("%s/repos/%s/branches/%s/protection", baseURL, repo, branch)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return false, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		// Branch is unprotected or does not exist.
		return false, nil
	default:
		return false, fmt.Errorf("unexpected GitHub API status %d fetching branch protection for %s in %s", resp.StatusCode, branch, repo)
	}
}

// GitHubRequiredStatusChecks returns the list of required status check names
// configured on the given branch's protection rules. It returns an empty slice
// if the branch is unprotected (404) or has no required status checks.
func GitHubRequiredStatusChecks(token, repo, branch string) ([]string, error) {
	return GitHubRequiredStatusChecksFromBase(GitHubBaseURL, token, repo, branch)
}

// GitHubRequiredStatusChecksFromBase is the implementation of
// GitHubRequiredStatusChecks with an injectable base URL, used by tests to
// point at a fake server.
func GitHubRequiredStatusChecksFromBase(baseURL, token, repo, branch string) ([]string, error) {
	url := fmt.Sprintf("%s/repos/%s/branches/%s/protection", baseURL, repo, branch)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	switch resp.StatusCode {
	case http.StatusOK:
		// Branch is protected — parse the response.
		var protection branchProtectionResponse
		if err := json.NewDecoder(resp.Body).Decode(&protection); err != nil {
			return nil, fmt.Errorf("failed to decode branch protection response: %w", err)
		}
		if protection.RequiredStatusChecks == nil {
			return []string{}, nil
		}
		// Merge both the legacy contexts field and the modern checks array,
		// deduplicating entries that appear in both. GitHub may populate both
		// fields with the same check names.
		seen := make(map[string]bool)
		var result []string
		for _, name := range protection.RequiredStatusChecks.Contexts {
			if !seen[name] {
				seen[name] = true
				result = append(result, name)
			}
		}
		for _, c := range protection.RequiredStatusChecks.Checks {
			if !seen[c.Context] {
				seen[c.Context] = true
				result = append(result, c.Context)
			}
		}
		return result, nil
	case http.StatusNotFound:
		// Branch is either unprotected or doesn't exist — treat as no checks.
		return []string{}, nil
	default:
		return nil, fmt.Errorf("unexpected GitHub API status %d fetching branch protection for %s in %s", resp.StatusCode, branch, repo)
	}
}

// gitHubDirEntry is a single file or directory entry from the GitHub Contents
// API when called on a directory path.
type gitHubDirEntry struct {
	Name string `json:"name"`
	Type string `json:"type"` // "file", "dir", "symlink", etc.
}

// GitHubListDirectory lists the entries in a directory in a GitHub repository.
// It returns (nil, nil) if the directory does not exist.
func GitHubListDirectory(token, repo, path string) ([]gitHubDirEntry, error) {
	return GitHubListDirectoryFromBase(GitHubBaseURL, token, repo, path)
}

// GitHubListDirectoryFromBase is the implementation of GitHubListDirectory
// with an injectable base URL.
func GitHubListDirectoryFromBase(baseURL, token, repo, path string, ref ...string) ([]gitHubDirEntry, error) {
	url := fmt.Sprintf("%s/repos/%s/contents/%s", baseURL, repo, path)
	if len(ref) > 0 && ref[0] != "" {
		url += "?ref=" + neturl.QueryEscape(ref[0])
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	switch resp.StatusCode {
	case http.StatusOK:
		var entries []gitHubDirEntry
		if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
			return nil, fmt.Errorf("failed to decode directory listing for %s in %s: %w", path, repo, err)
		}
		return entries, nil
	case http.StatusNotFound:
		return nil, nil
	default:
		return nil, fmt.Errorf("unexpected GitHub API status %d listing directory %s in %s", resp.StatusCode, path, repo)
	}
}

// GitHubRepoLanguages fetches the language breakdown for a repository using
// the GitHub Languages API. It returns a map of language name to byte count.
// Returns an empty map (not an error) if the repo has no detected languages.
func GitHubRepoLanguages(token, repo string) (map[string]int, error) {
	return GitHubRepoLanguagesFromBase(GitHubBaseURL, token, repo)
}

// GitHubRepoLanguagesFromBase is the implementation of GitHubRepoLanguages
// with an injectable base URL, used by tests to point at a fake server.
func GitHubRepoLanguagesFromBase(baseURL, token, repo string) (map[string]int, error) {
	url := fmt.Sprintf("%s/repos/%s/languages", baseURL, repo)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	switch resp.StatusCode {
	case http.StatusOK:
		var languages map[string]int
		if err := json.NewDecoder(resp.Body).Decode(&languages); err != nil {
			return nil, fmt.Errorf("failed to decode languages response for %s: %w", repo, err)
		}
		return languages, nil
	case http.StatusNotFound:
		return map[string]int{}, nil
	default:
		return nil, fmt.Errorf("unexpected GitHub API status %d fetching languages for %s", resp.StatusCode, repo)
	}
}

// combinedStatusResponse is a subset of the GitHub combined status API response.
type combinedStatusResponse struct {
	Statuses []statusEntry `json:"statuses"`
}

// statusEntry is a single status entry from the combined status response.
type statusEntry struct {
	Context string `json:"context"`
}

// GitHubCommitStatusContextsFromBase fetches the combined status for a ref and
// returns the list of status context names. Returns nil (not an error) if the
// API returns 404.
func GitHubCommitStatusContextsFromBase(baseURL, token, repo, ref string) ([]string, error) {
	url := fmt.Sprintf("%s/repos/%s/commits/%s/status", baseURL, repo, ref)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	switch resp.StatusCode {
	case http.StatusOK:
		var combined combinedStatusResponse
		if err := json.NewDecoder(resp.Body).Decode(&combined); err != nil {
			return nil, fmt.Errorf("failed to decode combined status response: %w", err)
		}
		contexts := make([]string, len(combined.Statuses))
		for i, s := range combined.Statuses {
			contexts[i] = s.Context
		}
		return contexts, nil
	case http.StatusNotFound:
		return nil, nil
	default:
		return nil, fmt.Errorf("unexpected GitHub API status %d fetching commit status for %s/%s", resp.StatusCode, repo, ref)
	}
}

// checkRunsResponse is a subset of the GitHub check runs API response.
type checkRunsResponse struct {
	CheckRuns []checkRunEntry `json:"check_runs"`
}

// checkRunEntry is a single check run entry.
type checkRunEntry struct {
	Name string `json:"name"`
}

// GitHubCheckRunNamesFromBase fetches check run names for a given ref.
// Returns nil (not an error) if the API returns 404.
func GitHubCheckRunNamesFromBase(baseURL, token, repo, ref string) ([]string, error) {
	url := fmt.Sprintf("%s/repos/%s/commits/%s/check-runs?per_page=100", baseURL, repo, ref)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	switch resp.StatusCode {
	case http.StatusOK:
		var runs checkRunsResponse
		if err := json.NewDecoder(resp.Body).Decode(&runs); err != nil {
			return nil, fmt.Errorf("failed to decode check runs response: %w", err)
		}
		names := make([]string, len(runs.CheckRuns))
		for i, r := range runs.CheckRuns {
			names[i] = r.Name
		}
		return names, nil
	case http.StatusNotFound:
		return nil, nil
	default:
		return nil, fmt.Errorf("unexpected GitHub API status %d fetching check runs for %s/%s", resp.StatusCode, repo, ref)
	}
}

// pullRequestEntry is a single PR from the GitHub pulls API.
type pullRequestEntry struct {
	Number int `json:"number"`
	Head   struct {
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		SHA string `json:"sha"`
	} `json:"base"`
	User struct {
		Login string `json:"login"`
	} `json:"user"`
}

// GitHubRecentPRCheckNamesFromBase finds a recent PR (open, then recently
// closed) and returns all check names reported on its head commit — both
// check runs and commit status contexts. This mirrors how the main-branch
// side fetches both sources. Returns (nil, nil) if no suitable PR is found
// or the API is unavailable.
func GitHubRecentPRCheckNamesFromBase(baseURL, token, repo string) ([]string, error) {
	// Try open PRs first, then recently closed.
	for _, state := range []string{"open", "closed"} {
		url := fmt.Sprintf("%s/repos/%s/pulls?state=%s&sort=updated&direction=desc&per_page=1", baseURL, repo, state)
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to build PR list request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("GitHub API request failed: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			continue
		}

		var prs []pullRequestEntry
		err = json.NewDecoder(resp.Body).Decode(&prs)
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to decode PR list response: %w", err)
		}

		if len(prs) == 0 || prs[0].Head.SHA == "" {
			continue
		}

		sha := prs[0].Head.SHA

		// Fetch both check runs and commit statuses for the PR head,
		// mirroring the main-branch approach.
		checkRunNames, err := GitHubCheckRunNamesFromBase(baseURL, token, repo, sha)
		if err != nil {
			return nil, err
		}
		statusContexts, err := GitHubCommitStatusContextsFromBase(baseURL, token, repo, sha)
		if err != nil {
			return nil, err
		}

		// Merge both sources into a single deduplicated list.
		seen := make(map[string]bool)
		var names []string
		for _, name := range checkRunNames {
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
		for _, ctx := range statusContexts {
			if !seen[ctx] {
				seen[ctx] = true
				names = append(names, ctx)
			}
		}
		return names, nil
	}

	// No suitable PR found.
	return nil, nil
}

// GitHubRecentDependabotPRCheckNamesFromBase finds a recent PR authored by
// dependabot[bot] (prefer closed/merged, then open) and returns all check
// names reported on its head commit — both check runs and commit status
// contexts. Fetches up to 10 recent PRs per state and filters client-side
// for dependabot authorship. Returns (nil, nil) if no suitable Dependabot PR
// is found or the API is unavailable.
func GitHubRecentDependabotPRCheckNamesFromBase(baseURL, token, repo string) ([]string, error) {
	// Prefer closed PRs (checks have had time to complete), then open.
	for _, state := range []string{"closed", "open"} {
		url := fmt.Sprintf("%s/repos/%s/pulls?state=%s&sort=updated&direction=desc&per_page=10", baseURL, repo, state)
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to build PR list request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("GitHub API request failed: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			continue
		}

		var prs []pullRequestEntry
		err = json.NewDecoder(resp.Body).Decode(&prs)
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to decode PR list response: %w", err)
		}

		// Filter to Dependabot-authored PRs.
		for _, pr := range prs {
			if pr.User.Login != "dependabot[bot]" {
				continue
			}
			if pr.Head.SHA == "" {
				continue
			}
			sha := pr.Head.SHA

			// Fetch both check runs and commit statuses for the PR head,
			// mirroring the main-branch approach.
			checkRunNames, err := GitHubCheckRunNamesFromBase(baseURL, token, repo, sha)
			if err != nil {
				return nil, err
			}
			statusContexts, err := GitHubCommitStatusContextsFromBase(baseURL, token, repo, sha)
			if err != nil {
				return nil, err
			}

			// Merge both sources into a single deduplicated list.
			seen := make(map[string]bool)
			var names []string
			for _, name := range checkRunNames {
				if !seen[name] {
					seen[name] = true
					names = append(names, name)
				}
			}
			for _, ctx := range statusContexts {
				if !seen[ctx] {
					seen[ctx] = true
					names = append(names, ctx)
				}
			}
			return names, nil
		}
	}

	// No suitable Dependabot PR found.
	return nil, nil
}

// DependabotPRInfo holds check names for both the head and base commits of a
// recent Dependabot PR.  BaseCheckNames are the checks that ran on the
// main-branch commit the PR was based on — used to distinguish timing
// artefacts (check added to main after the dep PR was opened) from genuine
// Dependabot-unsatisfiable checks (check structurally cannot run on PRs).
// BaseCheckNames is nil when the base SHA is unavailable.
type DependabotPRInfo struct {
	HeadCheckNames []string
	BaseCheckNames []string
}

// GitHubRecentDependabotPRInfoFromBase finds a recent PR authored by
// dependabot[bot] (prefer closed/merged, then open) and returns check names
// for both the head commit and the base commit (the main-branch SHA at the
// time the PR was created). Returns (nil, nil) if no suitable Dependabot PR
// is found or the API is unavailable.
func GitHubRecentDependabotPRInfoFromBase(baseURL, token, repo string) (*DependabotPRInfo, error) {
	// Prefer closed PRs (checks have had time to complete), then open.
	for _, state := range []string{"closed", "open"} {
		url := fmt.Sprintf("%s/repos/%s/pulls?state=%s&sort=updated&direction=desc&per_page=10", baseURL, repo, state)
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to build PR list request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("GitHub API request failed: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			continue
		}

		var prs []pullRequestEntry
		err = json.NewDecoder(resp.Body).Decode(&prs)
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to decode PR list response: %w", err)
		}

		// Filter to Dependabot-authored PRs.
		for _, pr := range prs {
			if pr.User.Login != "dependabot[bot]" {
				continue
			}
			if pr.Head.SHA == "" {
				continue
			}

			// Fetch check names for the PR head commit.
			headCheckRuns, err := GitHubCheckRunNamesFromBase(baseURL, token, repo, pr.Head.SHA)
			if err != nil {
				return nil, err
			}
			headStatusContexts, err := GitHubCommitStatusContextsFromBase(baseURL, token, repo, pr.Head.SHA)
			if err != nil {
				return nil, err
			}

			seen := make(map[string]bool)
			var headNames []string
			for _, name := range headCheckRuns {
				if !seen[name] {
					seen[name] = true
					headNames = append(headNames, name)
				}
			}
			for _, ctx := range headStatusContexts {
				if !seen[ctx] {
					seen[ctx] = true
					headNames = append(headNames, ctx)
				}
			}

			info := &DependabotPRInfo{HeadCheckNames: headNames}

			// Fetch check names for the base commit (main at the time the PR
			// was opened) if a base SHA is available.
			if pr.Base.SHA != "" {
				baseCheckRuns, err := GitHubCheckRunNamesFromBase(baseURL, token, repo, pr.Base.SHA)
				if err != nil {
					return nil, err
				}
				baseStatusContexts, err := GitHubCommitStatusContextsFromBase(baseURL, token, repo, pr.Base.SHA)
				if err != nil {
					return nil, err
				}

				baseSeen := make(map[string]bool)
				baseNames := make([]string, 0)
				for _, name := range baseCheckRuns {
					if !baseSeen[name] {
						baseSeen[name] = true
						baseNames = append(baseNames, name)
					}
				}
				for _, ctx := range baseStatusContexts {
					if !baseSeen[ctx] {
						baseSeen[ctx] = true
						baseNames = append(baseNames, ctx)
					}
				}
				info.BaseCheckNames = baseNames
			}

			return info, nil
		}
	}

	// No suitable Dependabot PR found.
	return nil, nil
}

// gitHubSecretsResponse is the response from the GitHub Actions secrets API.
type gitHubSecretsResponse struct {
	TotalCount int `json:"total_count"`
	Secrets    []struct {
		Name string `json:"name"`
	} `json:"secrets"`
}

// GitHubRepoSecretNames returns the names of all Actions secrets configured on
// the given repository. It uses the GitHub Actions secrets API, which returns
// secret names but not their values. Returns an empty slice (not an error) if
// the repo has no secrets.
func GitHubRepoSecretNames(token, repo string) ([]string, error) {
	return GitHubRepoSecretNamesFromBase(GitHubBaseURL, token, repo)
}

// GitHubRepoSecretNamesFromBase is the implementation of GitHubRepoSecretNames
// with an injectable base URL, used by tests to point at a fake server.
//
// Note: this fetches only the first page (up to 100 secrets). Pagination is not
// implemented because lucos repos have far fewer than 100 secrets in practice.
// If a repo ever exceeds 100 secrets this check would produce a false negative.
func GitHubRepoSecretNamesFromBase(baseURL, token, repo string) ([]string, error) {
	return gitHubRepoSecretNamesForStoreFromBase(baseURL, token, repo, "actions/secrets")
}

// GitHubRepoDependabotSecretNames returns the names of all Dependabot secrets
// configured on the given repository. Returns an empty slice (not an error) if
// the repo has no Dependabot secrets.
func GitHubRepoDependabotSecretNames(token, repo string) ([]string, error) {
	return GitHubRepoDependabotSecretNamesFromBase(GitHubBaseURL, token, repo)
}

// GitHubRepoDependabotSecretNamesFromBase is the implementation of
// GitHubRepoDependabotSecretNames with an injectable base URL, used by tests.
func GitHubRepoDependabotSecretNamesFromBase(baseURL, token, repo string) ([]string, error) {
	return gitHubRepoSecretNamesForStoreFromBase(baseURL, token, repo, "dependabot/secrets")
}

// gitHubRepoSecretNamesForStoreFromBase fetches secret names from a GitHub
// secrets API endpoint. The store parameter selects which store to query:
// "actions/secrets" for Actions secrets, "dependabot/secrets" for Dependabot.
// Both endpoints share the same response schema.
func gitHubRepoSecretNamesForStoreFromBase(baseURL, token, repo, store string) ([]string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s?per_page=100", baseURL, repo, store)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	switch resp.StatusCode {
	case http.StatusOK:
		var secrets gitHubSecretsResponse
		if err := json.NewDecoder(resp.Body).Decode(&secrets); err != nil {
			return nil, fmt.Errorf("failed to decode secrets response for %s: %w", repo, err)
		}
		names := make([]string, 0, len(secrets.Secrets))
		for _, s := range secrets.Secrets {
			names = append(names, s.Name)
		}
		return names, nil
	case http.StatusNotFound:
		return []string{}, nil
	default:
		return nil, fmt.Errorf("unexpected GitHub API status %d fetching %s for %s", resp.StatusCode, store, repo)
	}
}

// codeQLSupportedLanguages is the set of languages that CodeQL can analyse.
// Language names match what the GitHub Languages API returns (title case).
var codeQLSupportedLanguages = map[string]bool{
	"JavaScript": true,
	"TypeScript": true,
	"Python":     true,
	"Go":         true,
	"Java":       true,
	"C":          true,
	"C++":        true,
	"C#":         true,
	"Ruby":       true,
	"Kotlin":     true,
	"Swift":      true,
}

// HasCodeQLLanguage reports whether any of the given languages (as returned by
// the GitHub Languages API) are supported by CodeQL.
func HasCodeQLLanguage(languages map[string]int) bool {
	for lang := range languages {
		if codeQLSupportedLanguages[lang] {
			return true
		}
	}
	return false
}
