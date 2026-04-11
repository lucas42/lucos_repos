package conventions

import (
	"fmt"
	"log/slog"
	"strings"
)

func init() {
	// required-status-checks-coherent: detects incoherent branch-protection
	// required-status-check configurations — stale check names, missing CodeQL
	// coverage, and checks that Dependabot PRs can never satisfy.
	//
	// This convention merges three previously separate conventions
	// (valid-required-status-checks, codeql-required-for-auto-merge,
	// dependabot-required-checks-satisfiable) to avoid the cascading fix
	// pattern: fixing one convention's complaint frequently broke another,
	// and each misstep took 6 hours to surface.
	Register(Convention{
		ID:          "required-status-checks-coherent",
		Description: "Required status checks on main are internally consistent: no stale names, CodeQL is required when applicable, and all checks fire on Dependabot PRs",
		Rationale: "Three separate failure modes can silently block all PRs or all Dependabot PRs from merging:\n\n" +
			"1. Stale check names — required checks that no longer fire on HEAD of main (e.g. after GitHub renamed CodeQL checks from 'Analyze (X)' to 'CodeQL'). These cause zero visible errors but prevent all merges.\n" +
			"2. Missing CodeQL requirement — without a required Analyze (X) check, auto-merge can complete before CodeQL finishes, allowing security findings to be silently ignored at merge time.\n" +
			"3. Dependabot-unsatisfiable checks — a required check that doesn't fire on Dependabot PRs permanently blocks auto-merge for all dependency updates.\n\n" +
			"Diagnosing these separately meant fixing one issue often broke another. A unified check surfaces all three problems at once and provides a coherent remediation plan.",
		Guidance: "Review the repository's Settings → Branches → Branch protection rules for `main`.\n\n" +
			"For stale check names: remove or rename required checks that no longer appear in the Checks tab of a recent commit to main.\n\n" +
			"For missing CodeQL: add the CodeQL check run name (e.g. `Analyze (go)` or `Analyze (python)`) as a required status check. Ensure `.github/workflows/codeql-analysis.yml` uses an explicit language matrix.\n\n" +
			"For Dependabot-unsatisfiable checks: either switch from GitHub's 'default setup' CodeQL to a workflow-based setup with a `pull_request` trigger, or remove the unsatisfiable check from the required checks list.",
		AppliesTo:     []RepoType{RepoTypeSystem, RepoTypeComponent},
		ScheduledOnly: true,
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}

			// Step 1: fetch required status checks. If none are configured,
			// there's nothing to check — pass trivially.
			requiredChecks, err := GitHubRequiredStatusChecksFromBase(base, repo.GitHubToken, repo.Name, "main")
			if err != nil {
				slog.Warn("Convention check failed", "convention", "required-status-checks-coherent", "repo", repo.Name, "step", "fetch-branch-protection", "error", err)
				return ConventionResult{
					Convention: "required-status-checks-coherent",
					Err:        fmt.Errorf("error fetching branch protection for main: %w", err),
				}
			}
			if len(requiredChecks) == 0 {
				return ConventionResult{
					Convention: "required-status-checks-coherent",
					Pass:       true,
					Detail:     "no required status checks configured on main",
				}
			}

			var issues []string

			// Step 2: fetch actual checks reported on HEAD of main, then
			// identify stale required checks.
			statusContexts, err := GitHubCommitStatusContextsFromBase(base, repo.GitHubToken, repo.Name, "heads/main")
			if err != nil {
				slog.Warn("Convention check failed", "convention", "required-status-checks-coherent", "repo", repo.Name, "step", "fetch-commit-statuses", "error", err)
				return ConventionResult{
					Convention: "required-status-checks-coherent",
					Err:        fmt.Errorf("error fetching commit statuses for HEAD on main: %w", err),
				}
			}
			checkRunNames, err := GitHubCheckRunNamesFromBase(base, repo.GitHubToken, repo.Name, "heads/main")
			if err != nil {
				slog.Warn("Convention check failed", "convention", "required-status-checks-coherent", "repo", repo.Name, "step", "fetch-check-runs", "error", err)
				return ConventionResult{
					Convention: "required-status-checks-coherent",
					Err:        fmt.Errorf("error fetching check runs for HEAD on main: %w", err),
				}
			}

			reported := make(map[string]bool)
			for _, ctx := range statusContexts {
				reported[ctx] = true
			}
			for _, name := range checkRunNames {
				reported[name] = true
			}

			// Only flag stale checks if HEAD actually reported something — an
			// empty reported set might just mean no CI has run yet (e.g. a
			// docs-only commit with path filters), not that all checks are stale.
			if len(reported) > 0 {
				for _, check := range requiredChecks {
					if !reported[check] {
						issues = append(issues, fmt.Sprintf("required check %q is not reported on HEAD of main — likely a stale or renamed check name that will silently block all PRs from merging", check))
					}
				}
			}

			// Step 3: check CodeQL coverage if the repo uses CodeQL-supported languages.
			languages, err := GitHubRepoLanguagesFromBase(base, repo.GitHubToken, repo.Name)
			if err != nil {
				slog.Warn("Convention check failed", "convention", "required-status-checks-coherent", "repo", repo.Name, "step", "fetch-languages", "error", err)
				return ConventionResult{
					Convention: "required-status-checks-coherent",
					Err:        fmt.Errorf("error fetching languages: %w", err),
				}
			}
			if HasCodeQLLanguage(languages) {
				hasAnalyzeCheck := false
				for _, check := range requiredChecks {
					if strings.HasPrefix(check, "Analyze (") && strings.HasSuffix(check, ")") {
						hasAnalyzeCheck = true
						break
					}
				}
				if !hasAnalyzeCheck {
					issues = append(issues, "no CodeQL Analyze (X) check is required — auto-merge can complete before CodeQL finishes, bypassing security scanning")
				}
			}

			// Step 4: if Dependabot is configured, check that all required status
			// checks also fire on recent Dependabot PRs.
			hasDependabot, err := GitHubFileExistsFromBase(base, repo.GitHubToken, repo.Name, ".github/dependabot.yml")
			if err != nil {
				slog.Warn("Convention check failed", "convention", "required-status-checks-coherent", "repo", repo.Name, "step", "fetch-dependabot-yml", "error", err)
				return ConventionResult{
					Convention: "required-status-checks-coherent",
					Err:        fmt.Errorf("error checking for .github/dependabot.yml: %w", err),
				}
			}
			if hasDependabot {
				depInfo, err := GitHubRecentDependabotPRInfoFromBase(base, repo.GitHubToken, repo.Name)
				if err != nil {
					slog.Warn("Convention check failed", "convention", "required-status-checks-coherent", "repo", repo.Name, "step", "fetch-dependabot-pr-checks", "error", err)
					return ConventionResult{
						Convention: "required-status-checks-coherent",
						Err:        fmt.Errorf("error fetching Dependabot PR checks: %w", err),
					}
				}
				if depInfo != nil {
					depReported := make(map[string]bool)
					for _, name := range depInfo.HeadCheckNames {
						depReported[name] = true
					}
					depBaseReported := make(map[string]bool)
					for _, name := range depInfo.BaseCheckNames {
						depBaseReported[name] = true
					}
					for _, check := range requiredChecks {
						if !depReported[check] {
							// Only flag as Dependabot-unsatisfiable if the check
							// was already present on the dep PR's base commit (i.e.
							// on main when the PR was opened). If it wasn't on the
							// base, the check was added to main after the dep PR
							// was created — a timing artefact that will resolve
							// naturally on the next Dependabot PR.
							//
							// When BaseCheckNames is nil (base SHA unavailable),
							// err on the side of caution and flag the check.
							if depInfo.BaseCheckNames == nil || depBaseReported[check] {
								issues = append(issues, fmt.Sprintf("required check %q is not reported on recent Dependabot PRs — will permanently block auto-merge for all dependency updates", check))
							}
						}
					}
				}
			}

			if len(issues) == 0 {
				return ConventionResult{
					Convention: "required-status-checks-coherent",
					Pass:       true,
					Detail:     fmt.Sprintf("all %d required status checks are coherent (valid on HEAD, CodeQL covered, Dependabot-satisfiable)", len(requiredChecks)),
				}
			}

			detail := "Required status checks on main are not internally consistent: "
			for i, issue := range issues {
				if i > 0 {
					detail += "; "
				}
				detail += issue
			}

			return ConventionResult{
				Convention: "required-status-checks-coherent",
				Pass:       false,
				Detail:     detail,
			}
		},
	})
}
