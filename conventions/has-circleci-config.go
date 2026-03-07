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
