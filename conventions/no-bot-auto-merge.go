package conventions

import (
	"fmt"
	"log/slog"
	"strings"
)

func init() {
	// no-bot-auto-merge: system repos with unsupervisedAgentCode=false must
	// not have any workflow file that references the code-reviewer auto-merge
	// reusable workflow.
	Register(Convention{
		ID:          "no-bot-auto-merge",
		Description: "System repos without unsupervisedAgentCode do not have any workflow referencing the code-reviewer auto-merge reusable workflow",
		Rationale:   "Repos that have not opted into autonomous agent code (unsupervisedAgentCode=false) should not have automated bot-driven merging. If the code-reviewer auto-merge workflow is present, it can silently auto-merge PRs approved by lucos-code-reviewer[bot] without human review — which is only appropriate for repos that have deliberately opted in.",
		Guidance:    "Remove the `.github/workflows/code-reviewer-auto-merge.yml` file (or any other workflow file that references `lucas42/.github/.github/workflows/code-reviewer-auto-merge.yml`) from this repository. If you intend to enable autonomous agent code, set `unsupervisedAgentCode: true` in `lucos_configy` for this system instead.",
		AppliesTo:   []RepoType{RepoTypeSystem},
		Check: func(repo RepoContext) ConventionResult {
			// This convention only applies to repos with unsupervisedAgentCode=false.
			if repo.UnsupervisedAgentCode {
				return ConventionResult{
					Convention: "no-bot-auto-merge",
					Pass:       true,
					Detail:     "unsupervisedAgentCode is true; convention does not apply",
				}
			}

			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}

			// List all workflow files and check each one for a reference to the
			// code-reviewer reusable workflow. Checking by content (not just filename)
			// catches renamed files and is more robust now that the logic is centralised.
			entries, err := GitHubListDirectoryFromBase(base, repo.GitHubToken, repo.Name, ".github/workflows")
			if err != nil {
				slog.Warn("Convention check failed", "convention", "no-bot-auto-merge", "repo", repo.Name, "step", "list-workflows", "error", err)
				return ConventionResult{
					Convention: "no-bot-auto-merge",
					Err:        fmt.Errorf("error listing .github/workflows: %w", err),
				}
			}

			if entries == nil {
				// No .github/workflows directory at all — convention passes.
				return ConventionResult{
					Convention: "no-bot-auto-merge",
					Pass:       true,
					Detail:     ".github/workflows directory not found; no workflows to check",
				}
			}

			for _, entry := range entries {
				if entry.Type != "file" {
					continue
				}
				path := ".github/workflows/" + entry.Name
				content, err := GitHubFileContentFromBase(base, repo.GitHubToken, repo.Name, path)
				if err != nil {
					slog.Warn("Convention check failed", "convention", "no-bot-auto-merge", "repo", repo.Name, "step", "fetch-workflow", "file", entry.Name, "error", err)
					return ConventionResult{
						Convention: "no-bot-auto-merge",
						Err:        fmt.Errorf("error fetching workflow file %s: %w", entry.Name, err),
					}
				}
				if content != nil && strings.Contains(string(content), codeReviewerAutoMergeReusableWorkflow) {
					return ConventionResult{
						Convention: "no-bot-auto-merge",
						Pass:       false,
						Detail:     fmt.Sprintf("Workflow file %s references the code-reviewer auto-merge reusable workflow", entry.Name),
					}
				}
			}

			return ConventionResult{
				Convention: "no-bot-auto-merge",
				Pass:       true,
				Detail:     "No workflow files reference the code-reviewer auto-merge reusable workflow",
			}
		},
	})
}
