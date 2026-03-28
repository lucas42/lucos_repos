package conventions

import (
	"fmt"
	"log/slog"
	"strings"
)

// isTestOrBuildJob returns true if the CircleCI job name looks like a test or
// build job. This handles both plain job names (e.g. "test", "build") and
// orb-namespaced job names (e.g. "lucos/build-amd64", "lucos/test-unit").
// The match is against the final path segment after the last slash.
func isTestOrBuildJob(name string) bool {
	segment := name
	if i := strings.LastIndex(name, "/"); i >= 0 {
		segment = name[i+1:]
	}
	return strings.HasPrefix(segment, "test") || strings.HasPrefix(segment, "build")
}

func init() {
	// circleci-jobs-in-required-checks: system and component repos must have
	// their CircleCI test* and build* jobs listed as required status checks on
	// main, so auto-merge cannot race ahead of CI.
	Register(Convention{
		ID:          "circleci-jobs-in-required-checks",
		Description: "CircleCI test* and build* jobs appear in the required status checks for the main branch",
		Rationale:   "Without required status checks, auto-merge can complete before CircleCI finishes — meaning a broken build or failing test can land silently on main. Requiring test and build jobs as status checks ensures that code cannot merge until CI has confirmed they pass.",
		Guidance:    "Go to the repository's Settings → Branches → Branch protection rules for `main`. Under 'Require status checks to pass before merging', add each CircleCI test and build job as a required check. The exact check name must match what CircleCI reports in the GitHub Checks tab (e.g. `lucos/build-amd64` for orb jobs, or `test` for simple jobs). Trigger a pull request first to make the check names available in the search box.",
		AppliesTo:   []RepoType{RepoTypeSystem, RepoTypeComponent},
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}

			// Step 1: parse the CircleCI config to find test* and build* job names.
			cfg, err := parseCIConfig(base, repo.GitHubToken, repo.Name)
			if err != nil {
				slog.Warn("Convention check failed", "convention", "circleci-jobs-in-required-checks", "repo", repo.Name, "step", "parse-circleci-config", "error", err)
				return ConventionResult{
					Convention: "circleci-jobs-in-required-checks",
					Err:        fmt.Errorf("error parsing .circleci/config.yml: %w", err),
				}
			}
			if cfg == nil {
				// No CircleCI config — convention does not apply.
				return ConventionResult{
					Convention: "circleci-jobs-in-required-checks",
					Pass:       true,
					Detail:     ".circleci/config.yml not present; convention does not apply",
				}
			}

			var ciJobs []string
			for _, name := range allJobNames(cfg) {
				if isTestOrBuildJob(name) {
					ciJobs = append(ciJobs, name)
				}
			}

			if len(ciJobs) == 0 {
				// No test/build jobs in the config — nothing to require.
				return ConventionResult{
					Convention: "circleci-jobs-in-required-checks",
					Pass:       true,
					Detail:     "No test* or build* CircleCI jobs found; convention does not apply",
				}
			}

			// Step 2: fetch required status checks.
			checks, err := GitHubRequiredStatusChecksFromBase(base, repo.GitHubToken, repo.Name, "main")
			if err != nil {
				slog.Warn("Convention check failed", "convention", "circleci-jobs-in-required-checks", "repo", repo.Name, "step", "fetch-branch-protection", "error", err)
				return ConventionResult{
					Convention: "circleci-jobs-in-required-checks",
					Err:        fmt.Errorf("error fetching branch protection for main: %w", err),
				}
			}

			// GitHub prefixes CircleCI check names with "ci/circleci: " in the
			// legacy format (e.g. "ci/circleci: test", "ci/circleci: build-amd64").
			// Newer repos use bare names (e.g. "test", "lucos/build-amd64").
			// Strip the prefix and deduplicate before building the lookup set.
			const circlePrefix = "ci/circleci: "
			seen := make(map[string]bool, len(checks))
			var uniqueChecks []string
			for _, c := range checks {
				if !seen[c] {
					seen[c] = true
					uniqueChecks = append(uniqueChecks, c)
				}
			}
			requiredSet := make(map[string]bool, len(uniqueChecks))
			for _, c := range uniqueChecks {
				requiredSet[strings.TrimPrefix(c, circlePrefix)] = true
			}

			// Step 3: verify every test/build job is in required status checks.
			// A CI job "lucos/build-amd64" matches either the full name
			// "lucos/build-amd64" or the bare segment "build-amd64" in the
			// required set — the legacy GitHub format drops the orb namespace.
			var missing []string
			for _, job := range ciJobs {
				bareSegment := job
				if i := strings.LastIndex(job, "/"); i >= 0 {
					bareSegment = job[i+1:]
				}
				if !requiredSet[job] && !requiredSet[bareSegment] {
					missing = append(missing, job)
				}
			}

			if len(missing) == 0 {
				return ConventionResult{
					Convention: "circleci-jobs-in-required-checks",
					Pass:       true,
					Detail:     fmt.Sprintf("All CircleCI test/build jobs are required status checks: %v", ciJobs),
				}
			}

			// Look up the actual status context names from HEAD on main so we
			// can tell the user the exact check name to add, including the
			// correct format (legacy "ci/circleci: ..." or bare).
			suggestedNames := resolveCheckNames(base, repo.GitHubToken, repo.Name, missing)

			return ConventionResult{
				Convention: "circleci-jobs-in-required-checks",
				Pass:       false,
				Detail:     fmt.Sprintf("CircleCI test/build jobs not in required status checks. Add these exact names: %v (current required checks: %v)", suggestedNames, uniqueChecks),
			}
		},
	})
}

// resolveCheckNames maps CI job names to their actual reported status context
// names by fetching the combined commit status for HEAD on main. This tells us
// whether CircleCI is using the legacy "ci/circleci: ..." format or bare names.
// If the status API fails or a job can't be matched, the original job name is
// returned as a fallback.
func resolveCheckNames(baseURL, token, repo string, jobs []string) []string {
	contexts, err := GitHubCommitStatusContextsFromBase(baseURL, token, repo, "heads/main")
	if err != nil || contexts == nil {
		// Can't determine actual format — return job names as-is.
		return jobs
	}

	const circlePrefix = "ci/circleci: "

	// Build a lookup from normalized job name → actual context string.
	// "ci/circleci: build-amd64" normalizes to "build-amd64".
	// "lucos/build-amd64" normalizes to both "lucos/build-amd64" and "build-amd64".
	contextByJob := make(map[string]string)
	for _, ctx := range contexts {
		normalized := strings.TrimPrefix(ctx, circlePrefix)
		contextByJob[normalized] = ctx
		// Also map the bare segment for orb-namespaced names.
		if i := strings.LastIndex(normalized, "/"); i >= 0 {
			contextByJob[normalized[i+1:]] = ctx
		}
	}

	result := make([]string, len(jobs))
	for i, job := range jobs {
		bareSegment := job
		if j := strings.LastIndex(job, "/"); j >= 0 {
			bareSegment = job[j+1:]
		}
		if actual, ok := contextByJob[job]; ok {
			result[i] = actual
		} else if actual, ok := contextByJob[bareSegment]; ok {
			result[i] = actual
		} else {
			result[i] = job
		}
	}
	return result
}
