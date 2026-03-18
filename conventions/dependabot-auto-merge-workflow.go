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
		Guidance:    "Add a `.github/workflows/dependabot-auto-merge.yml` file that calls the shared reusable workflow:\n\n```yaml\nname: Dependabot auto-merge\n\non: pull_request\n\njobs:\n  dependabot:\n    if: github.actor == 'dependabot[bot]'\n    permissions:\n      pull-requests: write\n      contents: write\n    uses: lucas42/.github/.github/workflows/dependabot-auto-merge.yml@main\n```",
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

			if strings.Contains(string(content), dependabotAutoMergeReusableWorkflow) {
				return ConventionResult{
					Convention: "dependabot-auto-merge-workflow",
					Pass:       true,
					Detail:     fmt.Sprintf("%s references the shared reusable workflow", foundFilename),
				}
			}

			return ConventionResult{
				Convention: "dependabot-auto-merge-workflow",
				Pass:       false,
				Detail:     fmt.Sprintf("%s does not reference the shared reusable workflow (%s)", foundFilename, dependabotAutoMergeReusableWorkflow),
			}
		},
	})
}
