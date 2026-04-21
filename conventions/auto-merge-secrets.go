package conventions

import (
	"fmt"
	"log/slog"
	"strings"
)

func init() {
	// auto-merge-secrets: any repo with a code-reviewer auto-merge workflow must
	// reference both LUCOS_CI_APP_ID and LUCOS_CI_PRIVATE_KEY in the
	// workflow file. The reusable workflow declares them as required secrets; if
	// the caller doesn't pass them, the reusable job fails at startup.
	// The dependabot auto-merge workflow uses GITHUB_TOKEN only and does not
	// require these secrets.
	Register(Convention{
		ID:          "auto-merge-secrets",
		Description: "Repos with a code-reviewer auto-merge workflow pass LUCOS_CI_APP_ID and LUCOS_CI_PRIVATE_KEY to the reusable workflow and have both configured as Actions secrets on the repo",
		Rationale:   "The code-reviewer auto-merge reusable workflow declares LUCOS_CI_APP_ID and LUCOS_CI_PRIVATE_KEY as required secrets (migrated from CODE_REVIEWER_* in v1.15.0). If the caller workflow doesn't pass them, the reusable job fails at startup — auto-merge never runs and there is no obvious error signal.",
		Guidance:    "Ensure the `.github/workflows/code-reviewer-auto-merge.yml` workflow passes both secrets to the reusable workflow:\n\n```yaml\njobs:\n  reusable:\n    uses: lucas42/.github/.github/workflows/code-reviewer-auto-merge.yml@<commit-sha>\n    secrets:\n      LUCOS_CI_APP_ID: ${{ secrets.LUCOS_CI_APP_ID }}\n      LUCOS_CI_PRIVATE_KEY: ${{ secrets.LUCOS_CI_PRIVATE_KEY }}\n```\n\nYou also need to ensure `LUCOS_CI_APP_ID` and `LUCOS_CI_PRIVATE_KEY` are set as Actions secrets on this repository. Ask lucos-system-administrator to set them.",
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
			hasAppID := strings.Contains(contentStr, "secrets.LUCOS_CI_APP_ID")
			hasPrivateKey := strings.Contains(contentStr, "secrets.LUCOS_CI_PRIVATE_KEY")

			var missingFromFile []string
			if !hasAppID {
				missingFromFile = append(missingFromFile, "LUCOS_CI_APP_ID")
			}
			if !hasPrivateKey {
				missingFromFile = append(missingFromFile, "LUCOS_CI_PRIVATE_KEY")
			}
			if len(missingFromFile) > 0 {
				return ConventionResult{
					Convention: "auto-merge-secrets",
					Pass:       false,
					Detail:     fmt.Sprintf("code-reviewer-auto-merge.yml does not pass secret(s) to the reusable workflow: %v", missingFromFile),
				}
			}

			// The workflow file references both secrets — verify they're actually
			// configured on the repo. Without them, the reusable workflow falls back
			// to GITHUB_TOKEN, which suppresses push events and breaks CodeQL checks.
			secretNames, err := GitHubRepoSecretNamesFromBase(base, repo.GitHubToken, repo.Name)
			if err != nil {
				slog.Warn("Convention check failed", "convention", "auto-merge-secrets", "repo", repo.Name, "step", "fetch-secrets", "error", err)
				return ConventionResult{
					Convention: "auto-merge-secrets",
					Err:        fmt.Errorf("error fetching repo secrets: %w", err),
				}
			}
			secretSet := make(map[string]bool, len(secretNames))
			for _, name := range secretNames {
				secretSet[name] = true
			}
			var missingFromRepo []string
			if !secretSet["LUCOS_CI_APP_ID"] {
				missingFromRepo = append(missingFromRepo, "LUCOS_CI_APP_ID")
			}
			if !secretSet["LUCOS_CI_PRIVATE_KEY"] {
				missingFromRepo = append(missingFromRepo, "LUCOS_CI_PRIVATE_KEY")
			}
			if len(missingFromRepo) > 0 {
				return ConventionResult{
					Convention: "auto-merge-secrets",
					Pass:       false,
					Detail:     fmt.Sprintf("%v referenced in code-reviewer-auto-merge.yml but not configured as Actions secrets on this repo — ask lucos-system-administrator to add them", missingFromRepo),
				}
			}

			return ConventionResult{
				Convention: "auto-merge-secrets",
				Pass:       true,
				Detail:     "code-reviewer-auto-merge.yml passes LUCOS_CI_APP_ID and LUCOS_CI_PRIVATE_KEY to the reusable workflow",
			}
		},
	})
}
