package conventions

import (
	"fmt"
	"log/slog"
)

func init() {
	// branch-protection-enabled: system and component repos must have branch
	// protection rules enabled on the main branch, and must not require
	// approvals (which blocks Dependabot auto-merge).
	Register(Convention{
		ID:          "branch-protection-enabled",
		Description: "System and component repositories must have branch protection rules enabled on the main branch, without requiring approvals",
		Rationale:   "Branch protection prevents direct pushes to main and can enforce required status checks before merging. Without it, accidental or malicious direct pushes can bypass CI and deploy untested code. Requiring approvals is explicitly disabled because it blocks Dependabot PRs from auto-merging, causing security updates to pile up.",
		Guidance:    "Enable branch protection on `main` in the repository's Settings → Branches page. At minimum, require pull requests before merging. Ensure \"Require approvals\" is disabled — this setting blocks Dependabot auto-merge. Note: admin bypass is a known and accepted residual risk for this organisation — admins can override protection rules by design.",
		AppliesTo:   []RepoType{RepoTypeSystem, RepoTypeComponent},
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}

			protection, err := GitHubBranchProtectionDetailsFromBase(base, repo.GitHubToken, repo.Name, "main")
			if err != nil {
				slog.Warn("Convention check failed", "convention", "branch-protection-enabled", "repo", repo.Name, "error", err)
				return ConventionResult{
					Convention: "branch-protection-enabled",
					Err:        fmt.Errorf("error checking branch protection: %w", err),
				}
			}

			if protection == nil {
				return ConventionResult{
					Convention: "branch-protection-enabled",
					Pass:       false,
					Detail:     "Branch protection is not enabled on main",
				}
			}

			if protection.RequiredPullRequestReviews != nil {
				return ConventionResult{
					Convention: "branch-protection-enabled",
					Pass:       false,
					Detail:     "Branch protection is enabled on main but \"Require approvals\" is turned on — this blocks Dependabot auto-merge",
				}
			}

			return ConventionResult{
				Convention: "branch-protection-enabled",
				Pass:       true,
				Detail:     "Branch protection is enabled on main without required approvals",
			}
		},
	})
}
