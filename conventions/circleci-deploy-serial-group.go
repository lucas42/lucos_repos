package conventions

import (
	"fmt"
	"log/slog"
	"strings"
)

func init() {
	// circleci-deploy-serial-group: every lucos/build* job must declare
	// serial-group: << pipeline.project.slug >>/build, and every lucos/deploy-*
	// job must declare serial-group: deploy-<host> (e.g. deploy-avalon).
	//
	// Build serial groups prevent concurrent main-branch pipelines from
	// computing the same VERSION and overwriting each other's Docker images.
	// Deploy serial groups prevent concurrent deploys to the same host from
	// racing in containerd (blob-lease conflicts observed 2026-04-21).
	Register(Convention{
		ID: "circleci-deploy-serial-group",
		Description: "Every `lucos/build*` job must set `serial-group: << pipeline.project.slug >>/build`; " +
			"every `lucos/deploy-*` job must set `serial-group: deploy-<host>`",
		Rationale: "Build serial groups prevent concurrent main-branch pipelines from computing the same " +
			"VERSION in parallel, which causes Docker Hub images to be overwritten and git tags to drift. " +
			"Deploy serial groups prevent concurrent deploys to the same host from racing in containerd " +
			"(blob-lease conflicts observed 2026-04-21 during an estate-wide rollout).",
		Guidance: "Add the correct `serial-group` to each job in the `jobs:` list of each workflow in " +
			"`.circleci/config.yml`:\n\n" +
			"```yaml\nworkflows:\n  build:\n    jobs:\n      - lucos/build:\n" +
			"          serial-group: << pipeline.project.slug >>/build\n" +
			"      - lucos/deploy-avalon:\n" +
			"          serial-group: deploy-avalon\n```",
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

			const wantBuildSerialGroup = "<< pipeline.project.slug >>/build"
			var missingBuild []string
			var missingDeploy []string

			for _, entry := range allJobEntries(cfg) {
				if strings.HasPrefix(entry.Name, "lucos/build") {
					if entry.SerialGroup != wantBuildSerialGroup {
						missingBuild = append(missingBuild, entry.Name)
					}
				} else if strings.HasPrefix(entry.Name, "lucos/deploy-") {
					host := strings.TrimPrefix(entry.Name, "lucos/deploy-")
					wantDeploySerialGroup := "deploy-" + host
					if entry.SerialGroup != wantDeploySerialGroup {
						missingDeploy = append(missingDeploy, entry.Name)
					}
				}
			}

			var problems []string
			if len(missingBuild) > 0 {
				problems = append(problems, fmt.Sprintf(
					"build job(s) missing `serial-group: %s`: %s",
					wantBuildSerialGroup, strings.Join(missingBuild, ", "),
				))
			}
			if len(missingDeploy) > 0 {
				problems = append(problems, fmt.Sprintf(
					"deploy job(s) missing `serial-group: deploy-<host>`: %s",
					strings.Join(missingDeploy, ", "),
				))
			}

			if len(problems) > 0 {
				return ConventionResult{
					Convention: "circleci-deploy-serial-group",
					Pass:       false,
					Detail:     strings.Join(problems, "; "),
				}
			}
			return ConventionResult{
				Convention: "circleci-deploy-serial-group",
				Pass:       true,
				Detail:     "All lucos/build* and lucos/deploy-* jobs have the required serial-group",
			}
		},
	})
}
