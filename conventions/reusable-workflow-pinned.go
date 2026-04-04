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

// semverTagRe matches a full three-part semantic version tag (e.g. v1.0.0).
// Short tags like @v1 are intentionally not accepted — only x.y.z form.
var semverTagRe = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

func init() {
	Register(Convention{
		ID:          "reusable-workflow-pinned",
		Description: "Reusable workflow references to lucas42/.github are pinned to a full commit SHA or a semver tag",
		Rationale: "Referencing reusable workflows with a mutable branch ref like `@main` means any commit " +
			"pushed to the upstream repo is immediately picked up by all consumer workflows. " +
			"If an attacker gains push access to lucas42/.github they can modify a shared workflow " +
			"to exfiltrate secrets (notably the code-reviewer GitHub App private key) from the " +
			"next workflow run in every consumer repo.\n\n" +
			"Two pinning strategies are accepted:\n" +
			"- **Full commit SHA** (`@<40-char-hex>`): immutable and auditable — the reference can never " +
			"silently change. Use for one-off pins where Dependabot propagation is not needed.\n" +
			"- **Semver tag** (`@vX.Y.Z`): updated by Dependabot automatically when lucas42/.github " +
			"publishes a new release. Tags are created by the release workflow only after smoke tests " +
			"pass, so updates are gated. The `@vX.Y.Z` constraint prevents branch refs — Dependabot " +
			"will open a PR for each new tag, which auto-merges via the standard workflow.\n\n" +
			"Short tags like `@v1` or bare branch refs like `@main` are not accepted.",
		Guidance: "Update the `uses:` line in your caller workflow to use either a full SHA or a semver tag:\n\n" +
			"```yaml\n" +
			"# Option A: pinned to semver tag (recommended — Dependabot keeps it updated)\n" +
			"uses: lucas42/.github/.github/workflows/reusable-dependabot-auto-merge.yml@v1.0.0\n\n" +
			"# Option B: pinned to full commit SHA (immutable, no auto-updates)\n" +
			"uses: lucas42/.github/.github/workflows/reusable-dependabot-auto-merge.yml@<full-commit-sha>\n" +
			"```\n\n" +
			"To find the latest semver tag on lucas42/.github:\n" +
			"```\n" +
			"gh api repos/lucas42/.github/tags --jq '.[0].name'\n" +
			"```\n\n" +
			"To find the current SHA of lucas42/.github's main branch:\n" +
			"```\n" +
			"gh api repos/lucas42/.github/commits/main --jq '.sha'\n" +
			"```",
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
				slog.Warn("Convention check failed", "convention", "reusable-workflow-pinned", "repo", repo.Name, "step", "list-workflows", "error", err)
				return ConventionResult{
					Convention: "reusable-workflow-pinned",
					Err:        fmt.Errorf("error listing .github/workflows: %w", err),
				}
			}

			if entries == nil {
				// No workflows directory — convention does not apply.
				return ConventionResult{
					Convention: "reusable-workflow-pinned",
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
					Convention: "reusable-workflow-pinned",
					Pass:       true,
					Detail:     "no workflow files found in .github/workflows/",
				}
			}

			// Check each workflow file for unpinned reusable workflow references.
			for _, filename := range workflowFiles {
				path := ".github/workflows/" + filename
				content, err := GitHubFileContentFromBase(base, repo.GitHubToken, repo.Name, path)
				if err != nil {
					slog.Warn("Convention check failed", "convention", "reusable-workflow-pinned", "repo", repo.Name, "step", "fetch-"+filename, "error", err)
					return ConventionResult{
						Convention: "reusable-workflow-pinned",
						Err:        fmt.Errorf("error fetching %s: %w", path, err),
					}
				}
				if content == nil {
					continue
				}

				matches := reusableWorkflowRefRe.FindAllStringSubmatch(string(content), -1)
				for _, match := range matches {
					ref := match[1]
					if !fullSHARe.MatchString(ref) && !semverTagRe.MatchString(ref) {
						return ConventionResult{
							Convention: "reusable-workflow-pinned",
							Pass:       false,
							Detail:     fmt.Sprintf("%s references a reusable workflow with @%s — must use a full 40-character commit SHA or a semver tag (e.g. @v1.0.0)", path, ref),
						}
					}
				}
			}

			return ConventionResult{
				Convention: "reusable-workflow-pinned",
				Pass:       true,
				Detail:     "all reusable workflow references to lucas42/.github are pinned to a full commit SHA or a semver tag",
			}
		},
	})
}
