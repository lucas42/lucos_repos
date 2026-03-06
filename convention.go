package main

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

// RepoContext contains the information available to a convention check function.
type RepoContext struct {
	// Name is the full repository name, e.g. "lucas42/lucos_photos".
	Name string

	// GitHubToken is a valid GitHub App installation token for making API calls.
	GitHubToken string

	// Type is the repo's classification as determined by lucos_configy.
	Type RepoType

	// GitHubBaseURL is the base URL for GitHub API calls. Defaults to
	// githubBaseURL ("https://api.github.com") when empty.
	GitHubBaseURL string
}

// ConventionResult is the outcome of running a single convention against a repo.
type ConventionResult struct {
	// Convention is the ID of the convention that was checked.
	Convention string

	// Pass is true if the repo satisfies the convention.
	Pass bool

	// Detail provides human-readable context about the result (e.g. why it failed).
	Detail string
}

// Convention defines a rule that repos are expected to follow.
type Convention struct {
	// ID is a short, stable identifier used in the database and issue titles.
	ID string

	// Description explains what the convention checks, in plain English.
	Description string

	// AppliesTo is the set of repo types this convention applies to. If empty,
	// the convention applies to all repo types.
	AppliesTo []RepoType

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

// registry holds all registered conventions. Conventions are added at init time
// by calling RegisterConvention. The order is preserved for display purposes.
var registry []Convention

// RegisterConvention adds a convention to the global registry.
func RegisterConvention(c Convention) {
	registry = append(registry, c)
}

// AllConventions returns a copy of the registered conventions slice.
func AllConventions() []Convention {
	result := make([]Convention, len(registry))
	copy(result, registry)
	return result
}

// githubBaseURL is the base URL for the GitHub API. It can be overridden in
// tests using githubFileExistsFromBase.
const githubBaseURL = "https://api.github.com"

// githubFileExists checks whether a file exists in a GitHub repository at the
// given path. It uses the Contents API (checking for 200 vs 404) to determine
// existence.
func githubFileExists(token, repo, path string) (bool, error) {
	return githubFileExistsFromBase(githubBaseURL, token, repo, path)
}

// githubFileExistsFromBase is the implementation of githubFileExists with an
// injectable base URL, used by tests to point at a fake server.
func githubFileExistsFromBase(baseURL, token, repo, path string) (bool, error) {
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

func init() {
	// has-circleci-config: every repo must have a CircleCI configuration file.
	RegisterConvention(Convention{
		ID:          "has-circleci-config",
		Description: "Repository has a .circleci/config.yml file",
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = githubBaseURL
			}
			exists, err := githubFileExistsFromBase(base, repo.GitHubToken, repo.Name, ".circleci/config.yml")
			if err != nil {
				slog.Warn("Convention check failed", "convention", "has-circleci-config", "repo", repo.Name, "error", err)
				return ConventionResult{
					Convention: "has-circleci-config",
					Pass:       false,
					Detail:     fmt.Sprintf("Error checking file: %v", err),
				}
			}
			if exists {
				return ConventionResult{
					Convention: "has-circleci-config",
					Pass:       true,
					Detail:     ".circleci/config.yml found",
				}
			}
			return ConventionResult{
				Convention: "has-circleci-config",
				Pass:       false,
				Detail:     ".circleci/config.yml not found",
			}
		},
	})
}
