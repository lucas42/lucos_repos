package conventions

import (
	"fmt"
	"log/slog"
)

func init() {
	// auto-merge-secrets: any repo with a code-reviewer auto-merge workflow must
	// also have both CODE_REVIEWER_APP_ID and CODE_REVIEWER_PRIVATE_KEY set as
	// Actions secrets. The dependabot auto-merge workflow uses GITHUB_TOKEN only
	// and does not require these secrets.
	Register(Convention{
		ID:          "auto-merge-secrets",
		Description: "Repos with a code-reviewer auto-merge workflow have both CODE_REVIEWER_APP_ID and CODE_REVIEWER_PRIVATE_KEY secrets set",
		Rationale:   "The code-reviewer auto-merge workflow uses a GitHub App token to merge PRs and close linked issues. Without CODE_REVIEWER_APP_ID and CODE_REVIEWER_PRIVATE_KEY set as Actions secrets, the workflow silently fails to generate the token — auto-merge never runs and there is no obvious error signal. On 2026-03-19, 33 out of 39 repos were found to have the workflow file but not the secrets, causing silent auto-merge failures.",
		Guidance:    "Set both `CODE_REVIEWER_APP_ID` and `CODE_REVIEWER_PRIVATE_KEY` as Actions secrets on this repository. These credentials belong to the lucos-code-reviewer GitHub App and allow the auto-merge workflow to generate a token to merge PRs and close linked issues. Ask lucos-site-reliability or lucos-system-administrator to set the secrets via the GitHub API or the repository Settings UI.",
		AppliesTo:   []RepoType{RepoTypeSystem, RepoTypeComponent},
		ExcludeRepos: []string{
			// The .github repo defines the reusable workflow itself, not a caller.
			"lucas42/.github",
		},
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}

			// Only the code-reviewer auto-merge workflow requires these secrets.
			// The dependabot auto-merge workflow uses GITHUB_TOKEN only.
			exists, err := GitHubFileExistsFromBase(base, repo.GitHubToken, repo.Name, ".github/workflows/code-reviewer-auto-merge.yml")
			if err != nil {
				slog.Warn("Convention check failed", "convention", "auto-merge-secrets", "repo", repo.Name, "step", "check-workflow-file", "error", err)
				return ConventionResult{
					Convention: "auto-merge-secrets",
					Err:        fmt.Errorf("error checking code-reviewer-auto-merge.yml: %w", err),
				}
			}

			if !exists {
				// No code-reviewer auto-merge workflow — convention does not apply.
				return ConventionResult{
					Convention: "auto-merge-secrets",
					Pass:       true,
					Detail:     ".github/workflows/code-reviewer-auto-merge.yml not found; convention does not apply",
				}
			}

			// Workflow exists — verify both secrets are present.
			secretNames, err := GitHubActionsSecretsFromBase(base, repo.GitHubToken, repo.Name)
			if err != nil {
				slog.Warn("Convention check failed", "convention", "auto-merge-secrets", "repo", repo.Name, "step", "fetch-secrets", "error", err)
				return ConventionResult{
					Convention: "auto-merge-secrets",
					Err:        fmt.Errorf("error fetching Actions secrets: %w", err),
				}
			}

			secretSet := make(map[string]bool, len(secretNames))
			for _, name := range secretNames {
				secretSet[name] = true
			}

			hasAppID := secretSet["CODE_REVIEWER_APP_ID"]
			hasPrivateKey := secretSet["CODE_REVIEWER_PRIVATE_KEY"]

			if hasAppID && hasPrivateKey {
				return ConventionResult{
					Convention: "auto-merge-secrets",
					Pass:       true,
					Detail:     "CODE_REVIEWER_APP_ID and CODE_REVIEWER_PRIVATE_KEY are both set",
				}
			}

			var missing []string
			if !hasAppID {
				missing = append(missing, "CODE_REVIEWER_APP_ID")
			}
			if !hasPrivateKey {
				missing = append(missing, "CODE_REVIEWER_PRIVATE_KEY")
			}

			return ConventionResult{
				Convention: "auto-merge-secrets",
				Pass:       false,
				Detail:     fmt.Sprintf("code-reviewer-auto-merge.yml found but missing Actions secret(s): %v", missing),
			}
		},
	})
}
