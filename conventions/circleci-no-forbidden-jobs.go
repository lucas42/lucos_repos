package conventions

import (
	"fmt"
	"log/slog"
	"strings"
)

func init() {
	// circleci-no-forbidden-jobs: repos that are not systems or components must
	// not use lucos/release-* or lucos/deploy-* jobs in their CircleCI config
	// (if they have one). Unconfigured repos pass trivially.
	Register(Convention{
		ID:          "circleci-no-forbidden-jobs",
		Description: "Non-system, non-component repositories must not include `lucos/release-*` or `lucos/deploy-*` jobs in their CircleCI config",
		Rationale: "Release and deploy jobs are reserved for components and systems respectively. " +
			"Including them in other repo types (e.g. scripts) indicates a misconfiguration — " +
			"either the repo type in lucos_configy is wrong, or the CI config contains jobs that " +
			"shouldn't be there.",
		Guidance: "Remove any `lucos/release-*` and `lucos/deploy-*` jobs from the CircleCI config. " +
			"If this repo should be deploying to a server or releasing a package, update its type " +
			"in lucos_configy (`config/systems.yaml` or `config/components.yaml`) accordingly.",
		Check: func(repo RepoContext) ConventionResult {
			// Unconfigured repos pass trivially — the in-lucos-configy convention
			// handles the gap. Systems and components are checked by their own
			// targeted deploy/release job conventions.
			if repo.Type == RepoTypeUnconfigured || repo.Type == RepoTypeSystem || repo.Type == RepoTypeComponent {
				return ConventionResult{
					Convention: "circleci-no-forbidden-jobs",
					Pass:       true,
					Detail:     fmt.Sprintf("Convention does not apply to %s repos", repo.Type),
				}
			}

			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}
			cfg, err := parseCIConfig(base, repo.GitHubToken, repo.Name)
			if err != nil {
				slog.Warn("Convention check failed", "convention", "circleci-no-forbidden-jobs", "repo", repo.Name, "error", err)
				return ConventionResult{
					Convention: "circleci-no-forbidden-jobs",
					Err:        fmt.Errorf("error reading config: %w", err),
				}
			}
			if cfg == nil {
				// No config file — nothing to check.
				return ConventionResult{
					Convention: "circleci-no-forbidden-jobs",
					Pass:       true,
					Detail:     "No .circleci/config.yml present",
				}
			}

			var forbidden []string
			for _, name := range allJobNames(cfg) {
				if strings.HasPrefix(name, "lucos/release-") || strings.HasPrefix(name, "lucos/deploy-") {
					forbidden = append(forbidden, name)
				}
			}

			if len(forbidden) > 0 {
				return ConventionResult{
					Convention: "circleci-no-forbidden-jobs",
					Pass:       false,
					Detail:     fmt.Sprintf("CircleCI config contains forbidden jobs: %s", strings.Join(forbidden, ", ")),
				}
			}
			return ConventionResult{
				Convention: "circleci-no-forbidden-jobs",
				Pass:       true,
				Detail:     "No forbidden jobs found in CircleCI config",
			}
		},
	})
}
