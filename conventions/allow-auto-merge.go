package conventions

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

// gitHubRepoSettingsResponse is the subset of the GitHub repository API
// response that we care about for this convention.
type gitHubRepoSettingsResponse struct {
	AllowAutoMerge bool `json:"allow_auto_merge"`
}

// GitHubRepoSettings fetches the repository-level settings for the given repo.
func GitHubRepoSettings(token, repo string) (*gitHubRepoSettingsResponse, error) {
	return GitHubRepoSettingsFromBase(GitHubBaseURL, token, repo)
}

// GitHubRepoSettingsFromBase is the implementation of GitHubRepoSettings with
// an injectable base URL, used by tests to point at a fake server.
func GitHubRepoSettingsFromBase(baseURL, token, repo string) (*gitHubRepoSettingsResponse, error) {
	url := fmt.Sprintf("%s/repos/%s", baseURL, repo)
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

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected GitHub API status %d fetching repo settings for %s", resp.StatusCode, repo)
	}

	var settings gitHubRepoSettingsResponse
	if err := json.NewDecoder(resp.Body).Decode(&settings); err != nil {
		return nil, fmt.Errorf("failed to decode repo settings response for %s: %w", repo, err)
	}
	return &settings, nil
}

func init() {
	// allow-auto-merge: system and component repos must have "Allow auto-merge"
	// enabled in their GitHub repository settings.
	Register(Convention{
		ID:          "allow-auto-merge",
		Description: "System and component repositories must have the \"Allow auto-merge\" setting enabled",
		Rationale:   "Auto-merge allows PRs to be merged automatically once all required status checks pass and any required reviews are approved. Without this setting enabled, agent-opened PRs (e.g. from Dependabot or lucos-code-reviewer) cannot auto-merge — a human must manually merge every PR, which creates a bottleneck and means security updates sit open longer than necessary.",
		Guidance:    "Enable \"Allow auto-merge\" in the repository's Settings → General page, under the \"Pull Requests\" section.",
		AppliesTo:   []RepoType{RepoTypeSystem, RepoTypeComponent},
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}

			settings, err := GitHubRepoSettingsFromBase(base, repo.GitHubToken, repo.Name)
			if err != nil {
				slog.Warn("Convention check failed", "convention", "allow-auto-merge", "repo", repo.Name, "error", err)
				return ConventionResult{
					Convention: "allow-auto-merge",
					Err:        fmt.Errorf("error fetching repo settings: %w", err),
				}
			}

			if !settings.AllowAutoMerge {
				return ConventionResult{
					Convention: "allow-auto-merge",
					Pass:       false,
					Detail:     "\"Allow auto-merge\" is not enabled in repository settings",
				}
			}

			return ConventionResult{
				Convention: "allow-auto-merge",
				Pass:       true,
				Detail:     "\"Allow auto-merge\" is enabled",
			}
		},
	})
}
