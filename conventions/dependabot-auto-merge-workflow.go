package conventions

import (
	"fmt"
	"log/slog"
	"strings"
)

// dependabotAutoMergeReusableWorkflow is the reusable workflow reference that
// the auto-merge workflow must delegate to.
const dependabotAutoMergeReusableWorkflow = "lucas42/.github/.github/workflows/dependabot-auto-merge.yml"

func init() {
	// dependabot-auto-merge-workflow: system and component repos must have a
	// Dependabot auto-merge workflow that delegates to the shared reusable
	// workflow in lucas42/.github.
	Register(Convention{
		ID:          "dependabot-auto-merge-workflow",
		Description: "Repository has a Dependabot auto-merge workflow that references the shared reusable workflow",
		Rationale:   "Without auto-merge configured, Dependabot PRs pile up and require manual merging. The shared reusable workflow ensures consistent auto-merge behaviour across all repos. Repos that implement their own logic drift from the standard and may miss security fixes applied to the central workflow.",
		Guidance:    "Add a `.github/workflows/dependabot-auto-merge.yml` file that calls the shared reusable workflow:\n\n```yaml\nname: Dependabot auto-merge\n\non:\n  pull_request:\n    types: [opened, synchronize, reopened]\n\npermissions:\n  pull-requests: write\n  contents: write\n\njobs:\n  dependabot:\n    uses: lucas42/.github/.github/workflows/dependabot-auto-merge.yml@main\n```\n\nNote: use `pull_request` (not `pull_request_target`) and include the top-level `permissions:` block. Using `pull_request_target` with a reusable workflow call causes `startup_failure` on every non-Dependabot PR. Do not use `secrets: inherit`.",
		AppliesTo:   []RepoType{RepoTypeSystem, RepoTypeComponent},
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
				c, err := GitHubFileContentFromBase(base, repo.GitHubToken, repo.Name, filename)
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

			// A top-level permissions block is required so the reusable workflow's
			// job-level permissions are honoured under the pull_request event token.
			if !strings.Contains(contentStr, "permissions:") {
				return ConventionResult{
					Convention: "dependabot-auto-merge-workflow",
					Pass:       false,
					Detail:     fmt.Sprintf("%s is missing a top-level permissions block (pull-requests: write, contents: write)", foundFilename),
				}
			}

			return ConventionResult{
				Convention: "dependabot-auto-merge-workflow",
				Pass:       true,
				Detail:     fmt.Sprintf("%s references the shared reusable workflow with correct trigger and permissions", foundFilename),
			}
		},
	})
}
