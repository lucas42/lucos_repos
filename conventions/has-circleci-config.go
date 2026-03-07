package conventions

import (
	"fmt"
	"log/slog"
)

func init() {
	// has-circleci-config: every repo must have a CircleCI configuration file.
	Register(Convention{
		ID:          "has-circleci-config",
		Description: "Repository has a .circleci/config.yml file",
		Rationale:   "Without a CircleCI config, changes to this repository are not automatically built, tested, or deployed. This means code changes require manual intervention to reach production, which is error-prone and slows down delivery.",
		Guidance:    "Add a `.circleci/config.yml` following the standard lucos CI template (see the lucos CLAUDE.md for the canonical config). If this repository is intentionally not deployed (e.g. a documentation-only repo or an archive), consider whether it should be excluded from this convention via `AppliesTo`.",
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}
			exists, err := GitHubFileExistsFromBase(base, repo.GitHubToken, repo.Name, ".circleci/config.yml")
			if err != nil {
				slog.Warn("Convention check failed", "convention", "has-circleci-config", "repo", repo.Name, "error", err)
				return ConventionResult{
					Convention: "has-circleci-config",
					Pass:       false,
					Detail:     fmt.Sprintf("Error checking file: %v", err),
				}
			}
			if exists {
				return ConventionResult{
					Convention: "has-circleci-config",
					Pass:       true,
					Detail:     ".circleci/config.yml found",
				}
			}
			return ConventionResult{
				Convention: "has-circleci-config",
				Pass:       false,
				Detail:     ".circleci/config.yml not found",
			}
		},
	})
}
