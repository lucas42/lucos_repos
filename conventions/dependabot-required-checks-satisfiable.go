package conventions

import (
	"fmt"
	"log/slog"
)

func init() {
	// dependabot-required-checks-satisfiable: detects required status checks
	// that will never be satisfied by Dependabot PRs, silently blocking
	// auto-merge for all dependency updates.
	Register(Convention{
		ID:          "dependabot-required-checks-satisfiable",
		Description: "All required status checks on main are reported on recent Dependabot PRs",
		Rationale:   "A required status check that doesn't fire on Dependabot PRs will silently block auto-merge for all dependency updates. This was discovered when three Dependabot PRs were permanently stuck because GitHub's default CodeQL setup (\"Analyze (actions)\") does not run on Dependabot-authored PRs — a GitHub platform restriction. The check appeared valid on HEAD of main, so existing conventions passed, but Dependabot PRs could never satisfy it. Any required check with this property — including path-filtered workflows — creates the same silent block.",
		Guidance:    "A required status check is listed in Settings → Branches → Branch protection rules for `main` but does not appear in the checks tab of recent Dependabot PRs. For CodeQL specifically, switch from GitHub's \"default setup\" to a workflow-based setup (`.github/workflows/codeql-analysis.yml` with a `pull_request` trigger) which fires on all PR types including Dependabot. For other checks, either remove the check from the required status checks list, or update the underlying workflow's trigger configuration to also fire on Dependabot-authored PRs.",
		AppliesTo:     []RepoType{RepoTypeSystem, RepoTypeComponent},
		ScheduledOnly: true,
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}

			// Precondition 1: repo has Dependabot configured.
			hasDependabot, err := GitHubFileExistsFromBase(base, repo.GitHubToken, repo.Name, ".github/dependabot.yml")
			if err != nil {
				slog.Warn("Convention check failed", "convention", "dependabot-required-checks-satisfiable", "repo", repo.Name, "step", "fetch-dependabot-yml", "error", err)
				return ConventionResult{
					Convention: "dependabot-required-checks-satisfiable",
					Err:        fmt.Errorf("error checking for .github/dependabot.yml: %w", err),
				}
			}
			if !hasDependabot {
				return ConventionResult{
					Convention: "dependabot-required-checks-satisfiable",
					Pass:       true,
					Detail:     "no .github/dependabot.yml found; convention does not apply",
				}
			}

			// Precondition 2: repo has required status checks on main.
			requiredChecks, err := GitHubRequiredStatusChecksFromBase(base, repo.GitHubToken, repo.Name, "main")
			if err != nil {
				slog.Warn("Convention check failed", "convention", "dependabot-required-checks-satisfiable", "repo", repo.Name, "step", "fetch-branch-protection", "error", err)
				return ConventionResult{
					Convention: "dependabot-required-checks-satisfiable",
					Err:        fmt.Errorf("error fetching branch protection for main: %w", err),
				}
			}
			if len(requiredChecks) == 0 {
				return ConventionResult{
					Convention: "dependabot-required-checks-satisfiable",
					Pass:       true,
					Detail:     "no required status checks configured on main; convention does not apply",
				}
			}

			// Precondition 3: a recent Dependabot PR exists to sample.
			checkNames, err := GitHubRecentDependabotPRCheckNamesFromBase(base, repo.GitHubToken, repo.Name)
			if err != nil {
				slog.Warn("Convention check failed", "convention", "dependabot-required-checks-satisfiable", "repo", repo.Name, "step", "fetch-dependabot-pr-checks", "error", err)
				return ConventionResult{
					Convention: "dependabot-required-checks-satisfiable",
					Err:        fmt.Errorf("error fetching Dependabot PR checks: %w", err),
				}
			}
			if checkNames == nil {
				return ConventionResult{
					Convention: "dependabot-required-checks-satisfiable",
					Pass:       true,
					Detail:     "no recent Dependabot PRs found; cannot verify required checks are satisfiable",
				}
			}

			// Core check: find required checks that did not fire on the
			// Dependabot PR.
			reported := make(map[string]bool)
			for _, name := range checkNames {
				reported[name] = true
			}

			var missing []string
			for _, check := range requiredChecks {
				if !reported[check] {
					missing = append(missing, check)
				}
			}

			if len(missing) > 0 {
				return ConventionResult{
					Convention: "dependabot-required-checks-satisfiable",
					Pass:       false,
					Detail:     fmt.Sprintf("required status checks not reported on recent Dependabot PR (will permanently block auto-merge for dependency updates): %v", missing),
				}
			}

			return ConventionResult{
				Convention: "dependabot-required-checks-satisfiable",
				Pass:       true,
				Detail:     fmt.Sprintf("all %d required status checks were reported on a recent Dependabot PR", len(requiredChecks)),
			}
		},
	})
}
