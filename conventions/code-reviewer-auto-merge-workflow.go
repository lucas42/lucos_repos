package conventions

import (
	"fmt"
	"log/slog"
	"strings"
)

// codeReviewerAutoMergeReusableWorkflow is the reusable workflow reference that
// the code-reviewer auto-merge workflow must delegate to.
const codeReviewerAutoMergeReusableWorkflow = "lucas42/.github/.github/workflows/code-reviewer-auto-merge.yml"

func init() {
	// code-reviewer-auto-merge-workflow: system repos with unsupervisedAgentCode=true
	// must have a code-reviewer auto-merge workflow that delegates to the shared
	// reusable workflow in lucas42/.github.
	Register(Convention{
		ID:          "code-reviewer-auto-merge-workflow",
		Description: "System repos with unsupervisedAgentCode enabled have a code-reviewer auto-merge workflow referencing the shared reusable workflow",
		Rationale:   "Repos with autonomous agent code need the code-reviewer auto-merge workflow so that approved PRs from lucos-code-reviewer[bot] are merged automatically. Without it, agent-opened PRs require manual merging, breaking the unsupervised workflow. The shared reusable workflow ensures security controls (login+numeric ID verification) are applied consistently.",
		Guidance:    "Add a `.github/workflows/code-reviewer-auto-merge.yml` file that calls the shared reusable workflow:\n\n```yaml\nname: Auto-merge on code reviewer approval\n\non:\n  pull_request_review:\n    types:\n      - submitted\n  pull_request:\n    types:\n      - closed\n\njobs:\n  reusable:\n    uses: lucas42/.github/.github/workflows/code-reviewer-auto-merge.yml@main\n    secrets:\n      CODE_REVIEWER_APP_ID: ${{ secrets.CODE_REVIEWER_APP_ID }}\n      CODE_REVIEWER_PRIVATE_KEY: ${{ secrets.CODE_REVIEWER_PRIVATE_KEY }}\n```",
		AppliesTo:   []RepoType{RepoTypeSystem},
		ExcludeRepos: []string{
			// The .github repo defines the reusable workflow itself.
			"lucas42/.github",
		},
		Check: func(repo RepoContext) ConventionResult {
			// This convention only applies to repos with unsupervisedAgentCode=true.
			if !repo.UnsupervisedAgentCode {
				return ConventionResult{
					Convention: "code-reviewer-auto-merge-workflow",
					Pass:       true,
					Detail:     "unsupervisedAgentCode is false; convention does not apply",
				}
			}

			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}

			content, err := GitHubFileContentFromBase(base, repo.GitHubToken, repo.Name, ".github/workflows/code-reviewer-auto-merge.yml")
			if err != nil {
				slog.Warn("Convention check failed", "convention", "code-reviewer-auto-merge-workflow", "repo", repo.Name, "step", "fetch-workflow", "error", err)
				return ConventionResult{
					Convention: "code-reviewer-auto-merge-workflow",
					Err:        fmt.Errorf("error fetching code-reviewer-auto-merge.yml: %w", err),
				}
			}

			if content == nil {
				return ConventionResult{
					Convention: "code-reviewer-auto-merge-workflow",
					Pass:       false,
					Detail:     ".github/workflows/code-reviewer-auto-merge.yml not found",
				}
			}

			if strings.Contains(string(content), codeReviewerAutoMergeReusableWorkflow) {
				return ConventionResult{
					Convention: "code-reviewer-auto-merge-workflow",
					Pass:       true,
					Detail:     "code-reviewer-auto-merge.yml references the shared reusable workflow",
				}
			}

			return ConventionResult{
				Convention: "code-reviewer-auto-merge-workflow",
				Pass:       false,
				Detail:     fmt.Sprintf("code-reviewer-auto-merge.yml does not reference the shared reusable workflow (%s)", codeReviewerAutoMergeReusableWorkflow),
			}
		},
	})
}
