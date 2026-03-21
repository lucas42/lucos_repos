package conventions

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// graphQLRepoAutoMergeResponse holds the GraphQL response for the
// autoMergeAllowed field on a repository.
type graphQLRepoAutoMergeResponse struct {
	Data struct {
		Repository *struct {
			AutoMergeAllowed bool `json:"autoMergeAllowed"`
		} `json:"repository"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// GitHubAutoMergeAllowed returns whether the given repo has auto-merge enabled,
// using the GraphQL API. The REST API's allow_auto_merge field is only returned
// to callers with administration access; GraphQL's autoMergeAllowed is available
// to any app with metadata read access.
func GitHubAutoMergeAllowed(token, repo string) (bool, error) {
	return GitHubAutoMergeAllowedFromBase(GitHubBaseURL, token, repo)
}

// GitHubAutoMergeAllowedFromBase is the implementation of GitHubAutoMergeAllowed
// with an injectable base URL, used by tests to point at a fake server.
func GitHubAutoMergeAllowedFromBase(baseURL, token, repo string) (bool, error) {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 {
		return false, fmt.Errorf("invalid repo name %q: expected owner/repo format", repo)
	}
	owner, name := parts[0], parts[1]

	query := fmt.Sprintf(`{ "query": "{ repository(owner: \"%s\", name: \"%s\") { autoMergeAllowed } }" }`, owner, name)

	url := fmt.Sprintf("%s/graphql", baseURL)
	req, err := http.NewRequest("POST", url, bytes.NewBufferString(query))
	if err != nil {
		return false, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("GitHub GraphQL request failed: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("unexpected GitHub GraphQL status %d for %s", resp.StatusCode, repo)
	}

	var result graphQLRepoAutoMergeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, fmt.Errorf("failed to decode GraphQL response for %s: %w", repo, err)
	}

	if len(result.Errors) > 0 {
		return false, fmt.Errorf("GraphQL error for %s: %s", repo, result.Errors[0].Message)
	}

	if result.Data.Repository == nil {
		return false, fmt.Errorf("repository %s not found in GraphQL response", repo)
	}

	return result.Data.Repository.AutoMergeAllowed, nil
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

			allowed, err := GitHubAutoMergeAllowedFromBase(base, repo.GitHubToken, repo.Name)
			if err != nil {
				slog.Warn("Convention check failed", "convention", "allow-auto-merge", "repo", repo.Name, "error", err)
				return ConventionResult{
					Convention: "allow-auto-merge",
					Err:        fmt.Errorf("error checking auto-merge setting: %w", err),
				}
			}

			if !allowed {
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
