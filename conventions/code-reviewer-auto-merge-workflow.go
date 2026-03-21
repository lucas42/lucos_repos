package conventions

import (
	"fmt"
	"log/slog"
	"strings"
)

// codeReviewerAutoMergeReusableWorkflow is the reusable workflow reference that
// the code-reviewer auto-merge workflow must delegate to.
const codeReviewerAutoMergeReusableWorkflow = "lucas42/.github/.github/workflows/code-reviewer-auto-merge.yml"

// codeReviewerAutoMergePermissionsBlock is the permissions block the caller
// workflow must declare. The reusable workflow uses its own GitHub App token
// for all privileged operations, so the caller's GITHUB_TOKEN only needs
// contents: read (required to fetch the reusable workflow definition).
const codeReviewerAutoMergePermissionsBlock = "permissions:\n  contents: read"

func init() {
	// code-reviewer-auto-merge-workflow: all system and component repos must have
	// a code-reviewer auto-merge workflow that delegates to the shared reusable
	// workflow in lucas42/.github. The reusable workflow checks unsupervisedAgentCode
	// at runtime — if false it requires approval from lucas42, if true it requires
	// approval from lucos-code-reviewer[bot]. This makes the workflow safe to install
	// everywhere regardless of unsupervisedAgentCode.
	Register(Convention{
		ID:          "code-reviewer-auto-merge-workflow",
		Description: "System and component repos have a code-reviewer auto-merge workflow referencing the shared reusable workflow with minimal permissions",
		Rationale:   "The code-reviewer auto-merge workflow ensures approved PRs are merged automatically. The shared reusable workflow checks unsupervisedAgentCode at runtime from configy: repos with it enabled auto-merge on lucos-code-reviewer[bot] approval; others auto-merge on lucas42 approval. Without this workflow, PRs require manual merging. The shared workflow also closes linked issues when a bot-opened PR is merged, which the GITHUB_TOKEN cannot do. The caller must declare `permissions: contents: read` (the minimum required to fetch the reusable workflow definition) — all privileged operations go through the reusable workflow's own GitHub App token.",
		Guidance:    "Add a `.github/workflows/code-reviewer-auto-merge.yml` file that calls the shared reusable workflow:\n\n```yaml\nname: Auto-merge on code reviewer approval\n\non:\n  pull_request_review:\n    types:\n      - submitted\n  pull_request:\n    types:\n      - closed\n\npermissions:\n  contents: read\n\njobs:\n  reusable:\n    uses: lucas42/.github/.github/workflows/code-reviewer-auto-merge.yml@main\n    secrets:\n      CODE_REVIEWER_APP_ID: ${{ secrets.CODE_REVIEWER_APP_ID }}\n      CODE_REVIEWER_PRIVATE_KEY: ${{ secrets.CODE_REVIEWER_PRIVATE_KEY }}\n```\n\nYou must also set `CODE_REVIEWER_APP_ID` and `CODE_REVIEWER_PRIVATE_KEY` as Actions secrets on this repository. Without these secrets the workflow silently fails to generate a GitHub App token and auto-merge never runs. Ask lucos-site-reliability or lucos-system-administrator to set them.",
		AppliesTo:   []RepoType{RepoTypeSystem, RepoTypeComponent},
		ExcludeRepos: []string{
			// The .github repo defines the reusable workflow itself.
			"lucas42/.github",
		},
		Check: func(repo RepoContext) ConventionResult {
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

			contentStr := string(content)

			if !strings.Contains(contentStr, codeReviewerAutoMergeReusableWorkflow) {
				return ConventionResult{
					Convention: "code-reviewer-auto-merge-workflow",
					Pass:       false,
					Detail:     fmt.Sprintf("code-reviewer-auto-merge.yml does not reference the shared reusable workflow (%s)", codeReviewerAutoMergeReusableWorkflow),
				}
			}

			if !strings.Contains(contentStr, codeReviewerAutoMergePermissionsBlock) {
				return ConventionResult{
					Convention: "code-reviewer-auto-merge-workflow",
					Pass:       false,
					Detail:     "code-reviewer-auto-merge.yml is missing `permissions: contents: read` — the caller needs contents: read to fetch the reusable workflow definition; all privileged operations use the reusable workflow's own GitHub App token",
				}
			}

			return ConventionResult{
				Convention: "code-reviewer-auto-merge-workflow",
				Pass:       true,
				Detail:     "code-reviewer-auto-merge.yml references the shared reusable workflow with minimal permissions",
			}
		},
	})
}
