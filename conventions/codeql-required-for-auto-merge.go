package conventions

import (
	"fmt"
	"log/slog"
	"strings"
)

func init() {
	// codeql-required-for-auto-merge: repos with auto-merge must have CodeQL as
	// a required status check to prevent a race condition at merge time.
	Register(Convention{
		ID:          "codeql-required-for-auto-merge",
		Description: "Repositories using code-reviewer-auto-merge have CodeQL as a required status check on main",
		Rationale:   "Without CodeQL as a required status check, there is a race condition where the code reviewer can approve a PR and auto-merge can complete before CodeQL finishes — causing CodeQL findings to be silently ignored at merge time. Making CodeQL required prevents merges from racing ahead of the security scan.",
		Guidance:    "Go to the repository's branch protection settings for `main` and add the CodeQL check run name (e.g. `Analyze (python)` or `Analyze (javascript)`) as a required status check. The exact name must match what CodeQL reports in the Checks tab of a PR. If the repository has no CodeQL analysis workflow, add one first (`.github/workflows/codeql-analysis.yml`) for the languages used in the repo.",
		AppliesTo:   []RepoType{RepoTypeSystem, RepoTypeComponent},
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}

			// Step 1: check whether code-reviewer-auto-merge.yml is present.
			// If it isn't, this convention doesn't apply — pass immediately.
			hasAutoMerge, err := GitHubFileExistsFromBase(base, repo.GitHubToken, repo.Name, ".github/workflows/code-reviewer-auto-merge.yml")
			if err != nil {
				slog.Warn("Convention check failed", "convention", "codeql-required-for-auto-merge", "repo", repo.Name, "step", "check-auto-merge-workflow", "error", err)
				return ConventionResult{
					Convention: "codeql-required-for-auto-merge",
					Err:        fmt.Errorf("error checking for code-reviewer-auto-merge.yml: %w", err),
				}
			}
			if !hasAutoMerge {
				return ConventionResult{
					Convention: "codeql-required-for-auto-merge",
					Pass:       true,
					Detail:     "code-reviewer-auto-merge.yml not present; convention does not apply",
				}
			}

			// Step 2: fetch required status checks from branch protection.
			checks, err := GitHubRequiredStatusChecksFromBase(base, repo.GitHubToken, repo.Name, "main")
			if err != nil {
				slog.Warn("Convention check failed", "convention", "codeql-required-for-auto-merge", "repo", repo.Name, "step", "fetch-branch-protection", "error", err)
				return ConventionResult{
					Convention: "codeql-required-for-auto-merge",
					Err:        fmt.Errorf("error fetching branch protection for main: %w", err),
				}
			}

			// Step 3: check that at least one required status check looks like a
			// CodeQL Analyze check. CodeQL check names follow the pattern
			// "Analyze (<language>)", e.g. "Analyze (python)", "Analyze (javascript)".
			for _, check := range checks {
				if strings.HasPrefix(check, "Analyze (") && strings.HasSuffix(check, ")") {
					return ConventionResult{
						Convention: "codeql-required-for-auto-merge",
						Pass:       true,
						Detail:     fmt.Sprintf("CodeQL required status check found: %q", check),
					}
				}
			}

			if len(checks) == 0 {
				return ConventionResult{
					Convention: "codeql-required-for-auto-merge",
					Pass:       false,
					Detail:     "code-reviewer-auto-merge.yml is present but no required status checks are configured on main",
				}
			}

			return ConventionResult{
				Convention: "codeql-required-for-auto-merge",
				Pass:       false,
				Detail:     fmt.Sprintf("code-reviewer-auto-merge.yml is present but no CodeQL Analyze check found in required status checks: %v", checks),
			}
		},
	})
}
