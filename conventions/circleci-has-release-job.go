package conventions

import (
	"fmt"
	"log/slog"
	"strings"
)

func init() {
	// circleci-has-release-job: component repos must have at least one job
	// whose name begins with "lucos/release-".
	Register(Convention{
		ID:          "circleci-has-release-job",
		Description: "Component CircleCI config must include at least one `lucos/release-*` job",
		Rationale: "Component repos are shared libraries or infrastructure that other services " +
			"depend on. The `lucos/release-*` job publishes new versions to the package registry. " +
			"Without it, updates to the component cannot be consumed by downstream services.",
		Guidance: "Add a `lucos/release-*` job (e.g. `lucos/release-npm`) to the `jobs:` list " +
			"in a workflow in `.circleci/config.yml`. Refer to the lucos deploy orb documentation " +
			"for the correct job name for your package type.",
		AppliesTo: []RepoType{RepoTypeComponent},
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}
			cfg, err := parseCIConfig(base, repo.GitHubToken, repo.Name, repo.Ref)
			if err != nil {
				slog.Warn("Convention check failed", "convention", "circleci-has-release-job", "repo", repo.Name, "error", err)
				return ConventionResult{
					Convention: "circleci-has-release-job",
					Err:        fmt.Errorf("error reading config: %w", err),
				}
			}
			if cfg == nil {
				// File doesn't exist — circleci-config-exists will catch this.
				return ConventionResult{
					Convention: "circleci-has-release-job",
					Pass:       true,
					Detail:     ".circleci/config.yml not found; checked by circleci-config-exists",
				}
			}
			for _, name := range allJobNames(cfg) {
				if strings.HasPrefix(name, "lucos/release-") {
					return ConventionResult{
						Convention: "circleci-has-release-job",
						Pass:       true,
						Detail:     fmt.Sprintf("Found release job: %s", name),
					}
				}
			}
			return ConventionResult{
				Convention: "circleci-has-release-job",
				Pass:       false,
				Detail:     "No job beginning with `lucos/release-` found in CircleCI config",
			}
		},
	})
}
