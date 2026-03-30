package conventions

import (
	"fmt"
	"log/slog"
)

func init() {
	// valid-required-status-checks: detects stale or invalid required status
	// check names in branch protection that will silently block all PRs.
	Register(Convention{
		ID:          "valid-required-status-checks",
		Description: "All required status checks on main correspond to checks that are actually reported",
		Rationale:   "Branch protection check name mismatches are insidious — they cause zero errors and silently prevent all merges. This happens when check names change format (e.g. CodeQL migrating from 'Analyze (javascript)' to 'CodeQL') but the branch protection rules are not updated. Without automated detection, these are only discovered when someone notices PRs have been stuck.",
		Guidance:    "Go to the repository's Settings → Branches → Branch protection rules for `main`. Review the required status checks and remove or update any that do not match an active check. Compare against the checks listed in the GitHub Checks tab of a recent PR. Note: this check only samples HEAD of main — if the most recent commit didn't trigger all CI checks (e.g. docs-only change with path filters), this convention may report a false positive that clears on the next full-CI commit.",
		AppliesTo:     []RepoType{RepoTypeSystem, RepoTypeComponent},
		ScheduledOnly: true,
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}

			// Step 1: fetch required status checks.
			requiredChecks, err := GitHubRequiredStatusChecksFromBase(base, repo.GitHubToken, repo.Name, "main")
			if err != nil {
				slog.Warn("Convention check failed", "convention", "valid-required-status-checks", "repo", repo.Name, "step", "fetch-branch-protection", "error", err)
				return ConventionResult{
					Convention: "valid-required-status-checks",
					Err:        fmt.Errorf("error fetching branch protection for main: %w", err),
				}
			}

			if len(requiredChecks) == 0 {
				return ConventionResult{
					Convention: "valid-required-status-checks",
					Pass:       true,
					Detail:     "no required status checks configured on main",
				}
			}

			// Step 2: fetch the actual status contexts and check run names
			// reported on HEAD of main.
			statusContexts, err := GitHubCommitStatusContextsFromBase(base, repo.GitHubToken, repo.Name, "heads/main")
			if err != nil {
				slog.Warn("Convention check failed", "convention", "valid-required-status-checks", "repo", repo.Name, "step", "fetch-commit-statuses", "error", err)
				return ConventionResult{
					Convention: "valid-required-status-checks",
					Err:        fmt.Errorf("error fetching commit statuses for HEAD on main: %w", err),
				}
			}

			checkRunNames, err := GitHubCheckRunNamesFromBase(base, repo.GitHubToken, repo.Name, "heads/main")
			if err != nil {
				slog.Warn("Convention check failed", "convention", "valid-required-status-checks", "repo", repo.Name, "step", "fetch-check-runs", "error", err)
				return ConventionResult{
					Convention: "valid-required-status-checks",
					Err:        fmt.Errorf("error fetching check runs for HEAD on main: %w", err),
				}
			}

			// Build a set of all reported check names.
			reported := make(map[string]bool)
			for _, ctx := range statusContexts {
				reported[ctx] = true
			}
			for _, name := range checkRunNames {
				reported[name] = true
			}

			// If no checks were reported at all (empty HEAD or no CI), skip
			// the comparison — we can't distinguish stale checks from a repo
			// that simply hasn't had a recent push.
			if len(reported) == 0 {
				return ConventionResult{
					Convention: "valid-required-status-checks",
					Pass:       true,
					Detail:     "no status checks or check runs reported on HEAD of main; cannot validate required checks",
				}
			}

			// Step 3: find required checks not in the reported set.
			var stale []string
			for _, check := range requiredChecks {
				if !reported[check] {
					stale = append(stale, check)
				}
			}

			if len(stale) == 0 {
				return ConventionResult{
					Convention: "valid-required-status-checks",
					Pass:       true,
					Detail:     fmt.Sprintf("all %d required status checks match reported checks", len(requiredChecks)),
				}
			}

			return ConventionResult{
				Convention: "valid-required-status-checks",
				Pass:       false,
				Detail:     fmt.Sprintf("required status checks not reported on HEAD of main (likely stale/renamed): %v — these will silently block all PRs from merging", stale),
			}
		},
	})
}
