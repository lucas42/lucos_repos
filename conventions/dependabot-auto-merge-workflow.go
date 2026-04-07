package conventions

import (
	"fmt"
	"log/slog"
	"strings"
)

// dependabotAutoMergeReusableWorkflow is the reusable workflow reference that
// the auto-merge workflow must delegate to.
const dependabotAutoMergeReusableWorkflow = "lucas42/.github/.github/workflows/reusable-dependabot-auto-merge.yml"

func init() {
	// dependabot-auto-merge-workflow: system, component, and script repos must have a
	// Dependabot auto-merge workflow that delegates to the shared reusable
	// workflow in lucas42/.github.
	Register(Convention{
		ID:          "dependabot-auto-merge-workflow",
		Description: "Repository has a Dependabot auto-merge workflow that references the shared reusable workflow with CODE_REVIEWER_APP_ID and CODE_REVIEWER_PRIVATE_KEY configured as both Actions secrets and Dependabot secrets",
		Rationale:   "Without auto-merge configured, Dependabot PRs pile up and require manual merging. The shared reusable workflow ensures consistent auto-merge behaviour across all repos. Repos that implement their own logic drift from the standard and may miss security fixes applied to the central workflow.",
		Guidance:    "Add a `.github/workflows/dependabot-auto-merge.yml` file that calls the shared reusable workflow:\n\n```yaml\nname: Dependabot auto-merge\n\non:\n  pull_request:\n    types: [opened, synchronize, reopened]\n\npermissions:\n  pull-requests: write\n  contents: write\n\njobs:\n  dependabot:\n    uses: lucas42/.github/.github/workflows/dependabot-auto-merge.yml@<commit-sha>\n    secrets:\n      CODE_REVIEWER_APP_ID: ${{ secrets.CODE_REVIEWER_APP_ID }}\n      CODE_REVIEWER_PRIVATE_KEY: ${{ secrets.CODE_REVIEWER_PRIVATE_KEY }}\n```\n\nNote: use `pull_request` (not `pull_request_target`) and include the top-level `permissions:` block. Using `pull_request_target` with a reusable workflow call causes `startup_failure` on every non-Dependabot PR. Do not use `secrets: inherit`. The `CODE_REVIEWER_APP_ID` and `CODE_REVIEWER_PRIVATE_KEY` secrets must be configured in **both** the Actions secret store and the Dependabot secret store (Settings → Security → Secrets and variables). GitHub only exposes Dependabot secrets — not Actions secrets — when a Dependabot PR triggers the workflow. Without them in the Dependabot store, the reusable workflow falls back to GITHUB_TOKEN, which suppresses push events and breaks CodeQL required status checks.",
		AppliesTo:   []RepoType{RepoTypeSystem, RepoTypeComponent, RepoTypeScript},
		ExcludeRepos: []string{
			// The .github repo defines the reusable workflow itself — it cannot
			// call itself without creating a circular dependency.
			"lucas42/.github",
		},
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}

			// Try the canonical new filename first, then fall back to the legacy name.
			filenames := []string{
				".github/workflows/dependabot-auto-merge.yml",
				".github/workflows/auto-merge.yml",
			}

			var content []byte
			var foundFilename string
			for _, filename := range filenames {
				c, err := GitHubFileContentFromBase(base, repo.GitHubToken, repo.Name, filename, repo.Ref)
				if err != nil {
					slog.Warn("Convention check failed", "convention", "dependabot-auto-merge-workflow", "repo", repo.Name, "step", "fetch-workflow", "error", err)
					return ConventionResult{
						Convention: "dependabot-auto-merge-workflow",
						Err:        fmt.Errorf("error fetching %s: %w", filename, err),
					}
				}
				if c != nil {
					content = c
					foundFilename = filename
					break
				}
			}

			if content == nil {
				return ConventionResult{
					Convention: "dependabot-auto-merge-workflow",
					Pass:       false,
					Detail:     ".github/workflows/dependabot-auto-merge.yml not found",
				}
			}

			contentStr := string(content)

			if !strings.Contains(contentStr, dependabotAutoMergeReusableWorkflow) {
				return ConventionResult{
					Convention: "dependabot-auto-merge-workflow",
					Pass:       false,
					Detail:     fmt.Sprintf("%s does not reference the shared reusable workflow (%s)", foundFilename, dependabotAutoMergeReusableWorkflow),
				}
			}

			// pull_request_target + uses: causes startup_failure on every non-Dependabot PR.
			// Callers must use pull_request instead.
			if strings.Contains(contentStr, "pull_request_target") {
				return ConventionResult{
					Convention: "dependabot-auto-merge-workflow",
					Pass:       false,
					Detail:     fmt.Sprintf("%s uses pull_request_target — must use pull_request instead (pull_request_target + uses: causes startup_failure)", foundFilename),
				}
			}

			// secrets: inherit is prohibited even though it would work for
			// Dependabot secrets in a Dependabot-triggered context. GitHub
			// restricts Actions secrets (not Dependabot secrets) on
			// Dependabot-triggered pull_request events, so inherit would
			// actually expose Dependabot secrets. We still require explicit
			// secret enumeration for clarity and maintainability.
			if strings.Contains(contentStr, "secrets: inherit") {
				return ConventionResult{
					Convention: "dependabot-auto-merge-workflow",
					Pass:       false,
					Detail:     fmt.Sprintf("%s uses secrets: inherit — explicitly enumerate CODE_REVIEWER_APP_ID and CODE_REVIEWER_PRIVATE_KEY instead", foundFilename),
				}
			}

			// A top-level permissions block is required so the reusable workflow's
			// job-level permissions are honoured under the pull_request event token.
			if !strings.Contains(contentStr, "permissions:") {
				return ConventionResult{
					Convention: "dependabot-auto-merge-workflow",
					Pass:       false,
					Detail:     fmt.Sprintf("%s is missing a top-level permissions block (pull-requests: write, contents: write)", foundFilename),
				}
			}

			// The caller must pass CODE_REVIEWER_APP_ID and CODE_REVIEWER_PRIVATE_KEY
			// to the reusable workflow. Without them the reusable workflow falls back to
			// GITHUB_TOKEN, which suppresses push events and breaks CodeQL required status checks.
			if !strings.Contains(contentStr, "CODE_REVIEWER_APP_ID") {
				return ConventionResult{
					Convention: "dependabot-auto-merge-workflow",
					Pass:       false,
					Detail:     fmt.Sprintf("%s is missing CODE_REVIEWER_APP_ID in its secrets block — required to avoid GITHUB_TOKEN fallback which suppresses push events and breaks CodeQL required status checks", foundFilename),
				}
			}

			if !strings.Contains(contentStr, "CODE_REVIEWER_PRIVATE_KEY") {
				return ConventionResult{
					Convention: "dependabot-auto-merge-workflow",
					Pass:       false,
					Detail:     fmt.Sprintf("%s is missing CODE_REVIEWER_PRIVATE_KEY in its secrets block — required to avoid GITHUB_TOKEN fallback which suppresses push events and breaks CodeQL required status checks", foundFilename),
				}
			}

			// The workflow references both secrets — verify they're configured
			// in the Actions secret store. Without them, the reusable workflow
			// falls back to GITHUB_TOKEN for non-Dependabot PRs, suppressing
			// push events and breaking CodeQL required status checks.
			actionsSecretNames, err := GitHubRepoSecretNamesFromBase(base, repo.GitHubToken, repo.Name)
			if err != nil {
				slog.Warn("Convention check failed", "convention", "dependabot-auto-merge-workflow", "repo", repo.Name, "step", "fetch-actions-secrets", "error", err)
				return ConventionResult{
					Convention: "dependabot-auto-merge-workflow",
					Err:        fmt.Errorf("error fetching Actions secrets: %w", err),
				}
			}
			actionsSecretSet := make(map[string]bool, len(actionsSecretNames))
			for _, name := range actionsSecretNames {
				actionsSecretSet[name] = true
			}
			var missingActionsSecrets []string
			if !actionsSecretSet["CODE_REVIEWER_APP_ID"] {
				missingActionsSecrets = append(missingActionsSecrets, "CODE_REVIEWER_APP_ID")
			}
			if !actionsSecretSet["CODE_REVIEWER_PRIVATE_KEY"] {
				missingActionsSecrets = append(missingActionsSecrets, "CODE_REVIEWER_PRIVATE_KEY")
			}
			if len(missingActionsSecrets) > 0 {
				return ConventionResult{
					Convention: "dependabot-auto-merge-workflow",
					Pass:       false,
					Detail:     fmt.Sprintf("%v referenced in %s but not configured as Actions secrets on this repo — ask lucos-system-administrator to add them", missingActionsSecrets, foundFilename),
				}
			}

			// Also verify the secrets exist in the Dependabot secret store.
			// GitHub only exposes Dependabot secrets (not Actions secrets) when
			// a Dependabot PR triggers the workflow. Without them in the Dependabot
			// store, the reusable workflow falls back to GITHUB_TOKEN, which
			// suppresses push events and breaks CodeQL required status checks.
			dependabotSecretNames, err := GitHubRepoDependabotSecretNamesFromBase(base, repo.GitHubToken, repo.Name)
			if err != nil {
				slog.Warn("Convention check failed", "convention", "dependabot-auto-merge-workflow", "repo", repo.Name, "step", "fetch-dependabot-secrets", "error", err)
				return ConventionResult{
					Convention: "dependabot-auto-merge-workflow",
					Err:        fmt.Errorf("error fetching Dependabot secrets: %w", err),
				}
			}
			dependabotSecretSet := make(map[string]bool, len(dependabotSecretNames))
			for _, name := range dependabotSecretNames {
				dependabotSecretSet[name] = true
			}
			var missingDependabotSecrets []string
			if !dependabotSecretSet["CODE_REVIEWER_APP_ID"] {
				missingDependabotSecrets = append(missingDependabotSecrets, "CODE_REVIEWER_APP_ID")
			}
			if !dependabotSecretSet["CODE_REVIEWER_PRIVATE_KEY"] {
				missingDependabotSecrets = append(missingDependabotSecrets, "CODE_REVIEWER_PRIVATE_KEY")
			}
			if len(missingDependabotSecrets) > 0 {
				return ConventionResult{
					Convention: "dependabot-auto-merge-workflow",
					Pass:       false,
					Detail:     fmt.Sprintf("%v referenced in %s but not configured as Dependabot secrets on this repo — ask lucos-system-administrator to add them to the Dependabot secret store", missingDependabotSecrets, foundFilename),
				}
			}

			return ConventionResult{
				Convention: "dependabot-auto-merge-workflow",
				Pass:       true,
				Detail:     fmt.Sprintf("%s references the shared reusable workflow with correct trigger, permissions, and app secrets in both Actions and Dependabot stores", foundFilename),
			}
		},
	})
}
