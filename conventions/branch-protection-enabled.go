package conventions

import (
	"fmt"
	"log/slog"
)

func init() {
	// branch-protection-enabled: system and component repos must have branch
	// protection rules enabled on the main branch.
	Register(Convention{
		ID:          "branch-protection-enabled",
		Description: "System and component repositories must have branch protection rules enabled on the main branch",
		Rationale:   "Branch protection prevents direct pushes to main and can enforce required status checks before merging. Without it, accidental or malicious direct pushes can bypass CI and deploy untested code. It is also a prerequisite for configuring required status checks (e.g. CodeQL, CircleCI).",
		Guidance:    "Enable branch protection on `main` in the repository's Settings → Branches page. At minimum, require pull requests before merging. Note: admin bypass is a known and accepted residual risk for this organisation — admins can override protection rules by design.",
		AppliesTo:   []RepoType{RepoTypeSystem, RepoTypeComponent},
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}

			enabled, err := GitHubBranchProtectionEnabledFromBase(base, repo.GitHubToken, repo.Name, "main")
			if err != nil {
				slog.Warn("Convention check failed", "convention", "branch-protection-enabled", "repo", repo.Name, "error", err)
				return ConventionResult{
					Convention: "branch-protection-enabled",
					Err:        fmt.Errorf("error checking branch protection: %w", err),
				}
			}

			if enabled {
				return ConventionResult{
					Convention: "branch-protection-enabled",
					Pass:       true,
					Detail:     "Branch protection is enabled on main",
				}
			}

			return ConventionResult{
				Convention: "branch-protection-enabled",
				Pass:       false,
				Detail:     "Branch protection is not enabled on main",
			}
		},
	})
}
