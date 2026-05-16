package conventions

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

// requiredForkPRPolicy is the expected value for the fork pull request
// contributor approval setting. "first_time_contributors_new_to_github"
// exempts existing GitHub accounts (including all lucos agent bots) from the
// manual "Approve and run" gate on GitHub Actions workflow runs, while still
// requiring approval for genuinely new GitHub accounts.
const requiredForkPRPolicy = "first_time_contributors_new_to_github"

// forkPRContributorApprovalResponse is the response from the GitHub Actions
// fork pull request contributor approval API.
//
// Real API response shape (verified against GitHub docs at
// https://docs.github.com/rest/actions/permissions#get-fork-pr-contributor-approval-permissions-for-a-repository):
//
//	{"approval_policy": "first_time_contributors_new_to_github"}
//
// The JSON field is "approval_policy" — NOT "fork-pr-contributor-approval"
// (which is the URL path segment). Mixing these up causes silent empty-string
// decodes; update the test fixtures in fork_pr_contributor_approval_test.go
// when changing this struct.
type forkPRContributorApprovalResponse struct {
	ApprovalPolicy string `json:"approval_policy"`
}

// GitHubForkPRContributorApproval fetches the fork pull request contributor
// approval policy for the given repository using the GitHub REST API.
func GitHubForkPRContributorApproval(token, repo string) (string, error) {
	return GitHubForkPRContributorApprovalFromBase(GitHubBaseURL, token, repo)
}

// GitHubForkPRContributorApprovalFromBase is the implementation of
// GitHubForkPRContributorApproval with an injectable base URL, used by tests
// to point at a fake server.
func GitHubForkPRContributorApprovalFromBase(baseURL, token, repo string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/actions/permissions/fork-pr-contributor-approval", baseURL, repo)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	switch resp.StatusCode {
	case http.StatusOK:
		var result forkPRContributorApprovalResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return "", fmt.Errorf("failed to decode fork-pr-contributor-approval response for %s: %w", repo, err)
		}
		// Guard against silent empty-string decodes caused by a JSON field name
		// mismatch. If the API response shape has changed, treat it as an error
		// (incomplete sweep) rather than silently failing every repo.
		if result.ApprovalPolicy == "" {
			return "", fmt.Errorf("fork-pr-contributor-approval response for %s had an empty approval_policy field — API response shape may have changed", repo)
		}
		return result.ApprovalPolicy, nil
	default:
		return "", fmt.Errorf("unexpected GitHub API status %d fetching fork-pr-contributor-approval for %s", resp.StatusCode, repo)
	}
}

func init() {
	// fork-pr-contributor-approval: all repos must use the
	// "first_time_contributors_new_to_github" policy so that lucos agent bots
	// can open PRs without triggering a manual "Approve and run" gate.
	Register(Convention{
		ID:          "fork-pr-contributor-approval",
		Description: "Repositories must use the \"first_time_contributors_new_to_github\" GitHub Actions fork pull request contributor approval policy",
		Rationale:   "The default policy (\"first_time_contributors\") gates GitHub Actions workflow runs on any PR from an account that has not previously contributed to the repo — including lucos agent bots on newly-created repos. This causes the `code-reviewer-auto-merge.yml` workflow to be blocked behind a manual \"Approve and run\" click. The \"first_time_contributors_new_to_github\" policy exempts established GitHub accounts (including all lucos agent bots) while still requiring approval for brand-new GitHub accounts. See lucos_contacts#690 for the original blocking incident.",
		Guidance:    "Apply the correct policy via the GitHub API:\n\n```\nPUT /repos/{owner}/{repo}/actions/permissions/fork-pr-contributor-approval\n{\"fork-pr-contributor-approval\": \"first_time_contributors_new_to_github\"}\n```\n\nOr use the lucos_agent_coding_sandbox#75 script to apply it estate-wide.",
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}

			policy, err := GitHubForkPRContributorApprovalFromBase(base, repo.GitHubToken, repo.Name)
			if err != nil {
				slog.Warn("Convention check failed", "convention", "fork-pr-contributor-approval", "repo", repo.Name, "error", err)
				return ConventionResult{
					Convention: "fork-pr-contributor-approval",
					Err:        fmt.Errorf("error checking fork-pr-contributor-approval setting: %w", err),
				}
			}

			if policy != requiredForkPRPolicy {
				return ConventionResult{
					Convention: "fork-pr-contributor-approval",
					Pass:       false,
					Detail:     fmt.Sprintf("fork-pr-contributor-approval is %q, expected %q", policy, requiredForkPRPolicy),
				}
			}

			return ConventionResult{
				Convention: "fork-pr-contributor-approval",
				Pass:       true,
				Detail:     fmt.Sprintf("fork-pr-contributor-approval is correctly set to %q", requiredForkPRPolicy),
			}
		},
	})
}
