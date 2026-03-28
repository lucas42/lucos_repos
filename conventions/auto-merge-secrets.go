package conventions

import (
	"fmt"
	"log/slog"
	"strings"
)

func init() {
	// auto-merge-secrets: any repo with a code-reviewer auto-merge workflow must
	// reference both CODE_REVIEWER_APP_ID and CODE_REVIEWER_PRIVATE_KEY in the
	// workflow file. The reusable workflow declares them as required secrets; if
	// the caller doesn't pass them, the reusable job fails at startup.
	// The dependabot auto-merge workflow uses GITHUB_TOKEN only and does not
	// require these secrets.
	Register(Convention{
		ID:          "auto-merge-secrets",
		Description: "Repos with a code-reviewer auto-merge workflow pass CODE_REVIEWER_APP_ID and CODE_REVIEWER_PRIVATE_KEY to the reusable workflow",
		Rationale:   "The code-reviewer auto-merge reusable workflow declares CODE_REVIEWER_APP_ID and CODE_REVIEWER_PRIVATE_KEY as required secrets. If the caller workflow doesn't pass them, the reusable job fails at startup — auto-merge never runs and there is no obvious error signal. On 2026-03-19, 33 out of 39 repos were found to have the workflow file but not the secrets, causing silent auto-merge failures.",
		Guidance:    "Ensure the `.github/workflows/code-reviewer-auto-merge.yml` workflow passes both secrets to the reusable workflow:\n\n```yaml\njobs:\n  reusable:\n    uses: lucas42/.github/.github/workflows/code-reviewer-auto-merge.yml@main\n    secrets:\n      CODE_REVIEWER_APP_ID: ${{ secrets.CODE_REVIEWER_APP_ID }}\n      CODE_REVIEWER_PRIVATE_KEY: ${{ secrets.CODE_REVIEWER_PRIVATE_KEY }}\n```\n\nYou also need to ensure `CODE_REVIEWER_APP_ID` and `CODE_REVIEWER_PRIVATE_KEY` are set as Actions secrets on this repository. Ask lucos-site-reliability or lucos-system-administrator to set them.",
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
			content, err := GitHubFileContentFromBase(base, repo.GitHubToken, repo.Name, ".github/workflows/code-reviewer-auto-merge.yml", repo.Ref)
			if err != nil {
				slog.Warn("Convention check failed", "convention", "auto-merge-secrets", "repo", repo.Name, "step", "fetch-workflow", "error", err)
				return ConventionResult{
					Convention: "auto-merge-secrets",
					Err:        fmt.Errorf("error fetching code-reviewer-auto-merge.yml: %w", err),
				}
			}

			if content == nil {
				// No code-reviewer auto-merge workflow — convention does not apply.
				return ConventionResult{
					Convention: "auto-merge-secrets",
					Pass:       true,
					Detail:     ".github/workflows/code-reviewer-auto-merge.yml not found; convention does not apply",
				}
			}

			contentStr := string(content)
			hasAppID := strings.Contains(contentStr, "secrets.CODE_REVIEWER_APP_ID")
			hasPrivateKey := strings.Contains(contentStr, "secrets.CODE_REVIEWER_PRIVATE_KEY")

			if hasAppID && hasPrivateKey {
				return ConventionResult{
					Convention: "auto-merge-secrets",
					Pass:       true,
					Detail:     "code-reviewer-auto-merge.yml passes CODE_REVIEWER_APP_ID and CODE_REVIEWER_PRIVATE_KEY to the reusable workflow",
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
				Detail:     fmt.Sprintf("code-reviewer-auto-merge.yml does not pass secret(s) to the reusable workflow: %v", missing),
			}
		},
	})
}
