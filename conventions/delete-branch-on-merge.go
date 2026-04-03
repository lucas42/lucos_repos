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

// graphQLRepoDeleteBranchOnMergeResponse holds the GraphQL response for the
// deleteBranchOnMerge field on a repository.
type graphQLRepoDeleteBranchOnMergeResponse struct {
	Data struct {
		Repository *struct {
			DeleteBranchOnMerge bool `json:"deleteBranchOnMerge"`
		} `json:"repository"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// GitHubDeleteBranchOnMerge returns whether the given repo has
// "Automatically delete head branches" enabled, using the GraphQL API.
// The REST API's delete_branch_on_merge field is only returned to callers
// with administration access; GraphQL's deleteBranchOnMerge is available
// to any app with metadata read access.
func GitHubDeleteBranchOnMerge(token, repo string) (bool, error) {
	return GitHubDeleteBranchOnMergeFromBase(GitHubBaseURL, token, repo)
}

// GitHubDeleteBranchOnMergeFromBase is the implementation of
// GitHubDeleteBranchOnMerge with an injectable base URL, used by tests to
// point at a fake server.
func GitHubDeleteBranchOnMergeFromBase(baseURL, token, repo string) (bool, error) {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 {
		return false, fmt.Errorf("invalid repo name %q: expected owner/repo format", repo)
	}
	owner, name := parts[0], parts[1]

	query := fmt.Sprintf(`{ "query": "{ repository(owner: \"%s\", name: \"%s\") { deleteBranchOnMerge } }" }`, owner, name)

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

	var result graphQLRepoDeleteBranchOnMergeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, fmt.Errorf("failed to decode GraphQL response for %s: %w", repo, err)
	}

	if len(result.Errors) > 0 {
		return false, fmt.Errorf("GraphQL error for %s: %s", repo, result.Errors[0].Message)
	}

	if result.Data.Repository == nil {
		return false, fmt.Errorf("repository %s not found in GraphQL response", repo)
	}

	return result.Data.Repository.DeleteBranchOnMerge, nil
}

func init() {
	// delete-branch-on-merge: system and component repos must have
	// "Automatically delete head branches" enabled in their GitHub settings.
	Register(Convention{
		ID:          "delete-branch-on-merge",
		Description: "System and component repositories must have \"Automatically delete head branches\" enabled",
		Rationale:   "When \"Automatically delete head branches\" is disabled, merged PR branches accumulate indefinitely. Stale branches clutter the repository, make it harder to navigate open branches, and can lead to confusion about which branches are still active.",
		Guidance:    "Enable \"Automatically delete head branches\" in the repository's Settings → General page, under the \"Pull Requests\" section.",
		AppliesTo:   []RepoType{RepoTypeSystem, RepoTypeComponent},
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}

			enabled, err := GitHubDeleteBranchOnMergeFromBase(base, repo.GitHubToken, repo.Name)
			if err != nil {
				slog.Warn("Convention check failed", "convention", "delete-branch-on-merge", "repo", repo.Name, "error", err)
				return ConventionResult{
					Convention: "delete-branch-on-merge",
					Err:        fmt.Errorf("error checking delete-branch-on-merge setting: %w", err),
				}
			}

			if !enabled {
				return ConventionResult{
					Convention: "delete-branch-on-merge",
					Pass:       false,
					Detail:     "\"Automatically delete head branches\" is not enabled in repository settings",
				}
			}

			return ConventionResult{
				Convention: "delete-branch-on-merge",
				Pass:       true,
				Detail:     "\"Automatically delete head branches\" is enabled",
			}
		},
	})
}
