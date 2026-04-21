package conventions

import (
	"fmt"
	"log/slog"
	"strings"
)

func init() {
	// caller-workflow-secret-names: per-repo auto-merge caller workflows must
	// use the new LUCOS_CI_APP_ID / LUCOS_CI_PRIVATE_KEY secret names, not the
	// legacy CODE_REVIEWER_APP_ID / CODE_REVIEWER_PRIVATE_KEY names. The reusable
	// workflows in lucas42/.github were updated to the new names in v1.15.0 (PR #57).
	Register(Convention{
		ID:          "caller-workflow-secret-names",
		Description: "Auto-merge caller workflows use LUCOS_CI_APP_ID / LUCOS_CI_PRIVATE_KEY instead of the legacy CODE_REVIEWER_APP_ID / CODE_REVIEWER_PRIVATE_KEY secret names",
		Rationale:   "The reusable auto-merge workflows in lucas42/.github were migrated to use lucos-ci credentials (LUCOS_CI_APP_ID / LUCOS_CI_PRIVATE_KEY) in v1.15.0. Per-repo caller workflows that still pass the old CODE_REVIEWER_APP_ID / CODE_REVIEWER_PRIVATE_KEY names will send undefined values to the reusable workflow, causing it to fall back to GITHUB_TOKEN and silently breaking auto-merge.",
		Guidance:    "Update `.github/workflows/code-reviewer-auto-merge.yml` and/or `.github/workflows/dependabot-auto-merge.yml` to pass the new secret names:\n\n```yaml\nsecrets:\n  LUCOS_CI_APP_ID: ${{ secrets.LUCOS_CI_APP_ID }}\n  LUCOS_CI_PRIVATE_KEY: ${{ secrets.LUCOS_CI_PRIVATE_KEY }}\n```\n\nAlso ask lucos-system-administrator to ensure `LUCOS_CI_APP_ID` and `LUCOS_CI_PRIVATE_KEY` are set as Actions secrets (and Dependabot secrets, for the dependabot workflow) on this repository.",
		AppliesTo:   []RepoType{RepoTypeSystem, RepoTypeComponent, RepoTypeScript},
		ExcludeRepos: []string{
			// The .github repo defines the reusable workflows themselves.
			"lucas42/.github",
		},
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}

			// Track which workflow files we found and which use old names.
			foundAny := false
			var violations []string

			// Check the code-reviewer auto-merge caller workflow.
			crContent, err := GitHubFileContentFromBase(base, repo.GitHubToken, repo.Name, ".github/workflows/code-reviewer-auto-merge.yml", repo.Ref)
			if err != nil {
				slog.Warn("Convention check failed", "convention", "caller-workflow-secret-names", "repo", repo.Name, "step", "fetch-code-reviewer-workflow", "error", err)
				return ConventionResult{
					Convention: "caller-workflow-secret-names",
					Err:        fmt.Errorf("error fetching code-reviewer-auto-merge.yml: %w", err),
				}
			}
			if crContent != nil {
				foundAny = true
				crStr := string(crContent)
				if strings.Contains(crStr, "CODE_REVIEWER_APP_ID") || strings.Contains(crStr, "CODE_REVIEWER_PRIVATE_KEY") {
					violations = append(violations, "code-reviewer-auto-merge.yml")
				}
			}

			// Check the dependabot auto-merge caller workflow (canonical name, then legacy).
			dependabotFilenames := []string{
				".github/workflows/dependabot-auto-merge.yml",
				".github/workflows/auto-merge.yml",
			}
			for _, filename := range dependabotFilenames {
				depContent, err := GitHubFileContentFromBase(base, repo.GitHubToken, repo.Name, filename, repo.Ref)
				if err != nil {
					slog.Warn("Convention check failed", "convention", "caller-workflow-secret-names", "repo", repo.Name, "step", "fetch-dependabot-workflow", "error", err)
					return ConventionResult{
						Convention: "caller-workflow-secret-names",
						Err:        fmt.Errorf("error fetching %s: %w", filename, err),
					}
				}
				if depContent != nil {
					foundAny = true
					depStr := string(depContent)
					if strings.Contains(depStr, "CODE_REVIEWER_APP_ID") || strings.Contains(depStr, "CODE_REVIEWER_PRIVATE_KEY") {
						violations = append(violations, filename)
					}
					break
				}
			}

			if !foundAny {
				return ConventionResult{
					Convention: "caller-workflow-secret-names",
					Pass:       true,
					Detail:     "no auto-merge caller workflows found; convention does not apply",
				}
			}

			if len(violations) > 0 {
				return ConventionResult{
					Convention: "caller-workflow-secret-names",
					Pass:       false,
					Detail:     fmt.Sprintf("%v still pass legacy CODE_REVIEWER_APP_ID / CODE_REVIEWER_PRIVATE_KEY — update to LUCOS_CI_APP_ID / LUCOS_CI_PRIVATE_KEY", violations),
				}
			}

			return ConventionResult{
				Convention: "caller-workflow-secret-names",
				Pass:       true,
				Detail:     "auto-merge caller workflow(s) use the current LUCOS_CI_APP_ID / LUCOS_CI_PRIVATE_KEY secret names",
			}
		},
	})
}
