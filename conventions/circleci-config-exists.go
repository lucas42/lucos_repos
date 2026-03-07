package conventions

import (
	"fmt"
	"log/slog"
)

func init() {
	// circleci-config-exists: system and component repos must have a CircleCI
	// configuration file.
	Register(Convention{
		ID:          "circleci-config-exists",
		Description: "System and component repositories must have a .circleci/config.yml file",
		Rationale: "Without a CircleCI config, changes to this repository are not automatically " +
			"built, tested, or deployed. This means code changes require manual intervention to " +
			"reach production, which is error-prone and slows down delivery.",
		Guidance: "Add a `.circleci/config.yml` following the standard lucos CI template " +
			"(see the lucos CLAUDE.md for the canonical config).",
		AppliesTo: []RepoType{RepoTypeSystem, RepoTypeComponent},
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}
			exists, err := GitHubFileExistsFromBase(base, repo.GitHubToken, repo.Name, ".circleci/config.yml")
			if err != nil {
				slog.Warn("Convention check failed", "convention", "circleci-config-exists", "repo", repo.Name, "error", err)
				return ConventionResult{
					Convention: "circleci-config-exists",
					Pass:       false,
					Detail:     fmt.Sprintf("Error checking file: %v", err),
				}
			}
			if exists {
				return ConventionResult{
					Convention: "circleci-config-exists",
					Pass:       true,
					Detail:     ".circleci/config.yml found",
				}
			}
			return ConventionResult{
				Convention: "circleci-config-exists",
				Pass:       false,
				Detail:     ".circleci/config.yml not found",
			}
		},
	})
}
