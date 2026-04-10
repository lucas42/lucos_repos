package conventions

import (
	"fmt"
	"log/slog"
	"strings"
)

func init() {
	// no-stale-codeql-requirement-on-infra-repos: detects infrastructure-only
	// repos that still carry a required "Analyze (X)" status check on main,
	// even though CodeQL cannot scan their languages.
	//
	// ADR-0005 formalises the policy that only application-code repos (those with
	// a CodeQL-supported language) should have a required Analyze (X) check. The
	// symmetric check for the application-code side is enforced by
	// required-status-checks-coherent; this convention closes the other half.
	Register(Convention{
		ID:          "no-stale-codeql-requirement-on-infra-repos",
		Description: "Infrastructure-only repos must not have a required Analyze (X) CodeQL status check on main",
		Rationale: "If an infrastructure-only repo (Dockerfile, shell, config, etc.) carries a required " +
			"`Analyze (X)` status check on main, CodeQL will never produce that check run — " +
			"because the language isn't supported — and every Dependabot PR will be silently " +
			"blocked from auto-merging indefinitely. This is exactly what happened to " +
			"`lucos_private` and `lucos_static_media` on 2026-04-10 (see ADR-0005). " +
			"The existing `required-status-checks-coherent` convention only fires when " +
			"`HasCodeQLLanguage()` is true; this convention closes the symmetric gap for " +
			"repos where it is false.",
		Guidance: "Remove the stale `Analyze (X)` required status check from the branch protection " +
			"rules for `main`. Go to Settings → Branches → Branch protection rules → Edit the " +
			"rule for `main`, then delete the offending check from the \"Require status checks " +
			"to pass before merging\" list. See ADR-0005 (docs/adr/0005-codeql-policy-by-repo-class.md) " +
			"for the full policy context.",
		AppliesTo:     []RepoType{RepoTypeSystem, RepoTypeComponent},
		ScheduledOnly: true,
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}

			// Step 1: fetch required status checks for main. If none exist,
			// there's nothing stale — pass trivially.
			requiredChecks, err := GitHubRequiredStatusChecksFromBase(base, repo.GitHubToken, repo.Name, "main")
			if err != nil {
				slog.Warn("Convention check failed", "convention", "no-stale-codeql-requirement-on-infra-repos", "repo", repo.Name, "step", "fetch-branch-protection", "error", err)
				return ConventionResult{
					Convention: "no-stale-codeql-requirement-on-infra-repos",
					Err:        fmt.Errorf("error fetching branch protection for main: %w", err),
				}
			}

			// Collect any Analyze (X) checks present in the required list.
			var staleChecks []string
			for _, check := range requiredChecks {
				if analyzeLanguageRe.MatchString(check) {
					staleChecks = append(staleChecks, check)
				}
			}

			// If there are no Analyze (X) checks required at all, this convention
			// does not apply — pass trivially.
			if len(staleChecks) == 0 {
				return ConventionResult{
					Convention: "no-stale-codeql-requirement-on-infra-repos",
					Pass:       true,
					Detail:     "no Analyze (X) required status checks on main",
				}
			}

			// Step 2: fetch repo languages to determine whether this is an
			// infrastructure-only repo. If the repo has a CodeQL-supported language,
			// this convention does not fire — required-status-checks-coherent handles
			// the application-code side.
			languages, err := GitHubRepoLanguagesFromBase(base, repo.GitHubToken, repo.Name)
			if err != nil {
				slog.Warn("Convention check failed", "convention", "no-stale-codeql-requirement-on-infra-repos", "repo", repo.Name, "step", "fetch-languages", "error", err)
				return ConventionResult{
					Convention: "no-stale-codeql-requirement-on-infra-repos",
					Err:        fmt.Errorf("error fetching languages: %w", err),
				}
			}

			if HasCodeQLLanguage(languages) {
				// Application-code repo: the required Analyze check is expected here.
				// required-status-checks-coherent is responsible for this side.
				return ConventionResult{
					Convention: "no-stale-codeql-requirement-on-infra-repos",
					Pass:       true,
					Detail:     "repo has CodeQL-supported languages; required Analyze check is expected (see required-status-checks-coherent)",
				}
			}

			// Infrastructure-only repo with at least one stale Analyze (X) check.
			return ConventionResult{
				Convention: "no-stale-codeql-requirement-on-infra-repos",
				Pass:       false,
				Detail: fmt.Sprintf(
					"infrastructure-only repo has stale required CodeQL check(s) on main that will never fire and will silently block Dependabot auto-merges: %s",
					strings.Join(staleChecks, ", "),
				),
			}
		},
	})
}
