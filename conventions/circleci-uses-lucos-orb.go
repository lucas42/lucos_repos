package conventions

import (
	"fmt"
	"log/slog"
)

func init() {
	// circleci-uses-lucos-orb: system and component repos must declare the
	// lucos deploy orb as "lucos: lucos/deploy@0".
	Register(Convention{
		ID:          "circleci-uses-lucos-orb",
		Description: "CircleCI config must declare the lucos deploy orb (`lucos: lucos/deploy@0`)",
		Rationale: "The lucos deploy orb provides standardised build and deploy jobs. " +
			"Without it, repos must implement their own build/deploy logic, leading to " +
			"inconsistency and maintenance burden.",
		Guidance: "Add the following to the `orbs:` section of `.circleci/config.yml`:\n\n" +
			"```yaml\norbs:\n  lucos: lucos/deploy@0\n```",
		AppliesTo: []RepoType{RepoTypeSystem, RepoTypeComponent},
		// lucos_deploy_orb defines the orb itself — it cannot consume itself
		// without creating a circular dependency.
		ExcludeRepos: []string{"lucas42/lucos_deploy_orb"},
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}
			cfg, err := parseCIConfig(base, repo.GitHubToken, repo.Name)
			if err != nil {
				slog.Warn("Convention check failed", "convention", "circleci-uses-lucos-orb", "repo", repo.Name, "error", err)
				return ConventionResult{
					Convention: "circleci-uses-lucos-orb",
					Pass:       false,
					Detail:     fmt.Sprintf("Error reading config: %v", err),
				}
			}
			if cfg == nil {
				// File doesn't exist — circleci-config-exists will catch this.
				return ConventionResult{
					Convention: "circleci-uses-lucos-orb",
					Pass:       true,
					Detail:     ".circleci/config.yml not found; checked by circleci-config-exists",
				}
			}
			if hasLucosOrb(cfg) {
				return ConventionResult{
					Convention: "circleci-uses-lucos-orb",
					Pass:       true,
					Detail:     "lucos orb declared as lucos/deploy@0",
				}
			}
			return ConventionResult{
				Convention: "circleci-uses-lucos-orb",
				Pass:       false,
				Detail:     "CircleCI config does not declare `lucos: lucos/deploy@0` in its orbs",
			}
		},
	})
}
