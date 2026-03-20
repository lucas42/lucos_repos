package conventions

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

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
func GitHubFileExistsFromBase(baseURL, token, repo, path string) (bool, error) {
	url := fmt.Sprintf("%s/repos/%s/contents/%s", baseURL, repo, path)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return false, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
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
func GitHubFileContentFromBase(baseURL, token, repo, path string) ([]byte, error) {
	url := fmt.Sprintf("%s/repos/%s/contents/%s", baseURL, repo, path)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
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

	resp, err := http.DefaultClient.Do(req)
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

	resp, err := http.DefaultClient.Do(req)
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

	resp, err := http.DefaultClient.Do(req)
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
		// Merge both the legacy contexts field and the modern checks array.
		// GitHub populates one or the other depending on how the check was configured.
		result := make([]string, 0, len(protection.RequiredStatusChecks.Contexts)+len(protection.RequiredStatusChecks.Checks))
		result = append(result, protection.RequiredStatusChecks.Contexts...)
		for _, c := range protection.RequiredStatusChecks.Checks {
			result = append(result, c.Context)
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
func GitHubListDirectoryFromBase(baseURL, token, repo, path string) ([]gitHubDirEntry, error) {
	url := fmt.Sprintf("%s/repos/%s/contents/%s", baseURL, repo, path)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
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
