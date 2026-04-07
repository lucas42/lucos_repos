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
		Description: "System and component repositories must have branch protection rules enabled on the main branch, without requiring approvals or requiring branches to be up to date",
		Rationale:   "Branch protection prevents direct pushes to main and can enforce required status checks before merging. Without it, accidental or malicious direct pushes can bypass CI and deploy untested code. Requiring approvals and requiring branches to be up to date are both disabled because they block Dependabot PRs from auto-merging when more than one is open, causing security updates to pile up.",
		Guidance:    "Enable branch protection on `main` in the repository's Settings → Branches page. At minimum, require pull requests before merging. Ensure \"Require approvals\" is disabled and \"Require branches to be up to date before merging\" is disabled — both settings block Dependabot auto-merge. Note: admin bypass is a known and accepted residual risk for this organisation — admins can override protection rules by design.",
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

			if protection.RequiredStatusChecks != nil && protection.RequiredStatusChecks.Strict {
				return ConventionResult{
					Convention: "branch-protection-enabled",
					Pass:       false,
					Detail:     "Branch protection is enabled on main but \"Require branches to be up to date before merging\" is turned on — this blocks Dependabot auto-merge when multiple PRs are open",
				}
			}

			return ConventionResult{
				Convention: "branch-protection-enabled",
				Pass:       true,
				Detail:     "Branch protection is enabled on main without required approvals or requiring branches to be up to date",
			}
		},
	})
}
