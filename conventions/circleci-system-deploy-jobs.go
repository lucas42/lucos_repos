package conventions

import (
	"fmt"
	"log/slog"
	"strings"
)

func init() {
	// circleci-system-deploy-jobs: system repos must have exactly the right set
	// of deploy jobs — one per host listed in configy, and no extras.
	Register(Convention{
		ID:          "circleci-system-deploy-jobs",
		Description: "System CircleCI config must include exactly the correct `lucos/deploy-*` jobs for its configured hosts",
		Rationale: "Each host listed in lucos_configy for a system needs its own deploy job so " +
			"that changes are automatically deployed to every target host. Extra deploy jobs risk " +
			"deploying to hosts that aren't configured to run the service.",
		Guidance: "Edit the `jobs:` list in `.circleci/config.yml` to include exactly one " +
			"`lucos/deploy-{host}` job per host listed in lucos_configy — no more, no fewer. " +
			"Check `lucos_configy/config/systems.yaml` for the authoritative list of hosts.",
		AppliesTo: []RepoType{RepoTypeSystem},
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}
			cfg, err := parseCIConfig(base, repo.GitHubToken, repo.Name)
			if err != nil {
				slog.Warn("Convention check failed", "convention", "circleci-system-deploy-jobs", "repo", repo.Name, "error", err)
				return ConventionResult{
					Convention: "circleci-system-deploy-jobs",
					Err:        fmt.Errorf("error reading config: %w", err),
				}
			}
			if cfg == nil {
				// File doesn't exist — circleci-config-exists will catch this.
				return ConventionResult{
					Convention: "circleci-system-deploy-jobs",
					Pass:       true,
					Detail:     ".circleci/config.yml not found; checked by circleci-config-exists",
				}
			}

			// Build a set of expected deploy job names from the configured hosts.
			expected := make(map[string]bool, len(repo.Hosts))
			for _, host := range repo.Hosts {
				expected["lucos/deploy-"+host] = false // false = not yet found
			}

			// Scan all job names in the config.
			var extraDeployJobs []string
			for _, name := range allJobNames(cfg) {
				if !strings.HasPrefix(name, "lucos/deploy-") {
					continue
				}
				if _, ok := expected[name]; ok {
					expected[name] = true // mark as found
				} else {
					extraDeployJobs = append(extraDeployJobs, name)
				}
			}

			// Check for missing expected jobs.
			var missingJobs []string
			for jobName, found := range expected {
				if !found {
					missingJobs = append(missingJobs, jobName)
				}
			}

			if len(missingJobs) > 0 && len(extraDeployJobs) > 0 {
				return ConventionResult{
					Convention: "circleci-system-deploy-jobs",
					Pass:       false,
					Detail: fmt.Sprintf("Missing deploy jobs: %s; unexpected deploy jobs: %s",
						strings.Join(missingJobs, ", "), strings.Join(extraDeployJobs, ", ")),
				}
			}
			if len(missingJobs) > 0 {
				return ConventionResult{
					Convention: "circleci-system-deploy-jobs",
					Pass:       false,
					Detail:     fmt.Sprintf("Missing deploy jobs for configured hosts: %s", strings.Join(missingJobs, ", ")),
				}
			}
			if len(extraDeployJobs) > 0 {
				return ConventionResult{
					Convention: "circleci-system-deploy-jobs",
					Pass:       false,
					Detail:     fmt.Sprintf("Unexpected deploy jobs not matching any configured host: %s", strings.Join(extraDeployJobs, ", ")),
				}
			}
			return ConventionResult{
				Convention: "circleci-system-deploy-jobs",
				Pass:       true,
				Detail:     "Deploy jobs match configured hosts",
			}
		},
	})
}
