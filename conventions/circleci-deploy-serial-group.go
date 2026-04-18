package conventions

import (
	"fmt"
	"log/slog"
	"strings"
)

func init() {
	// circleci-deploy-serial-group: every lucos/build-* job in a system or
	// component repo must declare serial-group: << pipeline.project.slug >>/build.
	// This prevents concurrent main-branch pipelines from computing the same
	// VERSION and overwriting each other's Docker images on Docker Hub.
	Register(Convention{
		ID:          "circleci-deploy-serial-group",
		Description: "Every `lucos/build-*` job must set `serial-group: << pipeline.project.slug >>/build`",
		Rationale: "When multiple main-branch pipelines run concurrently (e.g. during a " +
			"Dependabot wave), `calc-version` can compute the same VERSION in parallel pipelines. " +
			"This causes Docker Hub images to be overwritten and git tags to drift out of sync " +
			"with the pushed image. The serial-group attribute serialises builds so only one " +
			"main pipeline runs at a time.",
		Guidance: "Add `serial-group: << pipeline.project.slug >>/build` to every `lucos/build-*` " +
			"job in the `jobs:` list of each workflow in `.circleci/config.yml`:\n\n" +
			"```yaml\nworkflows:\n  build:\n    jobs:\n      - lucos/build-amd64:\n" +
			"          serial-group: << pipeline.project.slug >>/build\n```",
		AppliesTo: []RepoType{RepoTypeSystem, RepoTypeComponent},
		// lucos_deploy_orb defines the orb — it cannot consume itself.
		ExcludeRepos: []string{"lucas42/lucos_deploy_orb"},
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}

			cfg, err := parseCIConfig(base, repo.GitHubToken, repo.Name, repo.Ref)
			if err != nil {
				slog.Warn("Convention check failed", "convention", "circleci-deploy-serial-group", "repo", repo.Name, "error", err)
				return ConventionResult{
					Convention: "circleci-deploy-serial-group",
					Err:        fmt.Errorf("error reading config: %w", err),
				}
			}
			if cfg == nil {
				// File doesn't exist — circleci-config-exists will catch this.
				return ConventionResult{
					Convention: "circleci-deploy-serial-group",
					Pass:       true,
					Detail:     ".circleci/config.yml not found; checked by circleci-config-exists",
				}
			}

			const wantSerialGroup = "<< pipeline.project.slug >>/build"
			var missing []string
			for _, entry := range allJobEntries(cfg) {
				if !strings.HasPrefix(entry.Name, "lucos/build-") {
					continue
				}
				if entry.SerialGroup != wantSerialGroup {
					missing = append(missing, entry.Name)
				}
			}

			if len(missing) > 0 {
				return ConventionResult{
					Convention: "circleci-deploy-serial-group",
					Pass:       false,
					Detail: fmt.Sprintf(
						"build job(s) missing `serial-group: %s`: %s",
						wantSerialGroup, strings.Join(missing, ", "),
					),
				}
			}
			return ConventionResult{
				Convention: "circleci-deploy-serial-group",
				Pass:       true,
				Detail:     "All lucos/build-* jobs have the required serial-group",
			}
		},
	})
}
