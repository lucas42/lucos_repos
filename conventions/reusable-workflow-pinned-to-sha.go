package conventions

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"
)

// reusableWorkflowRefRe matches `uses:` lines that reference a reusable workflow
// in lucas42/.github and captures the version suffix after the `@`.
var reusableWorkflowRefRe = regexp.MustCompile(`uses:\s*lucas42/\.github/\.github/workflows/[^@\s]+@(\S+)`)

// fullSHARe matches a full 40-character lowercase hex SHA.
var fullSHARe = regexp.MustCompile(`^[0-9a-f]{40}$`)

func init() {
	Register(Convention{
		ID:          "reusable-workflow-pinned-to-sha",
		Description: "Reusable workflow references to lucas42/.github are pinned to a full commit SHA",
		Rationale: "Referencing reusable workflows with a mutable tag like `@main` means any commit " +
			"pushed to the upstream repo is immediately picked up by all consumer workflows. " +
			"If an attacker gains push access to lucas42/.github they can modify a shared workflow " +
			"to exfiltrate secrets (notably the code-reviewer GitHub App private key) from the " +
			"next workflow run in every consumer repo. Pinning to a full commit SHA makes workflow " +
			"references immutable and auditable — changes require an explicit PR to update the SHA.",
		Guidance: "Update the `uses:` line in your caller workflow to reference the reusable workflow " +
			"by full commit SHA instead of `@main`:\n\n" +
			"```yaml\n" +
			"# Before (mutable — insecure)\n" +
			"uses: lucas42/.github/.github/workflows/code-reviewer-auto-merge.yml@main\n\n" +
			"# After (pinned to SHA)\n" +
			"uses: lucas42/.github/.github/workflows/code-reviewer-auto-merge.yml@<full-commit-sha>\n" +
			"```\n\n" +
			"To find the current SHA of lucas42/.github's main branch:\n" +
			"```\n" +
			"gh api repos/lucas42/.github/commits/main --jq '.sha'\n" +
			"```\n\n" +
			"When the reusable workflows in lucas42/.github are updated, a follow-up PR should " +
			"update the pinned SHA in all consumer repos.",
		AppliesTo: []RepoType{RepoTypeSystem, RepoTypeComponent, RepoTypeScript},
		ExcludeRepos: []string{
			// The .github repo defines the reusable workflows — it does not call them.
			"lucas42/.github",
		},
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}

			// List all workflow files in .github/workflows/.
			entries, err := GitHubListDirectoryFromBase(base, repo.GitHubToken, repo.Name, ".github/workflows")
			if err != nil {
				slog.Warn("Convention check failed", "convention", "reusable-workflow-pinned-to-sha", "repo", repo.Name, "step", "list-workflows", "error", err)
				return ConventionResult{
					Convention: "reusable-workflow-pinned-to-sha",
					Err:        fmt.Errorf("error listing .github/workflows: %w", err),
				}
			}

			if entries == nil {
				// No workflows directory — convention does not apply.
				return ConventionResult{
					Convention: "reusable-workflow-pinned-to-sha",
					Pass:       true,
					Detail:     ".github/workflows/ not found; convention does not apply",
				}
			}

			// Collect all workflow files (*.yml and *.yaml).
			var workflowFiles []string
			for _, entry := range entries {
				if entry.Type == "file" && (strings.HasSuffix(entry.Name, ".yml") || strings.HasSuffix(entry.Name, ".yaml")) {
					workflowFiles = append(workflowFiles, entry.Name)
				}
			}

			if len(workflowFiles) == 0 {
				return ConventionResult{
					Convention: "reusable-workflow-pinned-to-sha",
					Pass:       true,
					Detail:     "no workflow files found in .github/workflows/",
				}
			}

			// Check each workflow file for unpinned reusable workflow references.
			for _, filename := range workflowFiles {
				path := ".github/workflows/" + filename
				content, err := GitHubFileContentFromBase(base, repo.GitHubToken, repo.Name, path)
				if err != nil {
					slog.Warn("Convention check failed", "convention", "reusable-workflow-pinned-to-sha", "repo", repo.Name, "step", "fetch-"+filename, "error", err)
					return ConventionResult{
						Convention: "reusable-workflow-pinned-to-sha",
						Err:        fmt.Errorf("error fetching %s: %w", path, err),
					}
				}
				if content == nil {
					continue
				}

				matches := reusableWorkflowRefRe.FindAllStringSubmatch(string(content), -1)
				for _, match := range matches {
					ref := match[1]
					if !fullSHARe.MatchString(ref) {
						return ConventionResult{
							Convention: "reusable-workflow-pinned-to-sha",
							Pass:       false,
							Detail:     fmt.Sprintf("%s references a reusable workflow with @%s — must use a full 40-character commit SHA", path, ref),
						}
					}
				}
			}

			return ConventionResult{
				Convention: "reusable-workflow-pinned-to-sha",
				Pass:       true,
				Detail:     "all reusable workflow references to lucas42/.github are pinned to a full commit SHA",
			}
		},
	})
}
