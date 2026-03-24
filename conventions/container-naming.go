package conventions

import (
	"fmt"
	"log/slog"
	"strings"

	"gopkg.in/yaml.v3"
)

// containerNamingComposeFile is the minimal structure we need from a
// docker-compose.yml for the container naming convention. We only care about
// the container_name field of each service.
type containerNamingComposeFile struct {
	Services map[string]containerNamingService `yaml:"services"`
}

type containerNamingService struct {
	ContainerName string   `yaml:"container_name"`
	Profiles      []string `yaml:"profiles"`
}

func init() {
	Register(Convention{
		ID:          "container-naming",
		Description: "Every container_name in docker-compose.yml uses the lucos_{project}_{role} naming convention",
		Rationale: "The ecosystem convention for container names is `lucos_{project}_{role}` (e.g. " +
			"`lucos_photos_api`, `lucos_arachne_web`). Many older services use short names without " +
			"any prefix (`monitoring`, `root`, `time`), which become ambiguous in `docker ps` output " +
			"as the number of containers on a single host grows. Consistent naming makes it easy to " +
			"correlate running containers with their source repo.",
		Guidance: "Rename the `container_name` in `docker-compose.yml` so it starts with the repo " +
			"name (e.g. for repo `lucos_monitoring`, use `lucos_monitoring_web` or just " +
			"`lucos_monitoring` for single-container services).\n\n" +
			"**Important:** Docker may treat a renamed container as a new one rather than replacing " +
			"the old one. When deploying a rename, stop the old container before starting the " +
			"renamed one to avoid port conflicts and orphaned containers.",
		AppliesTo: []RepoType{RepoTypeSystem},
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}

			content, err := GitHubFileContentFromBase(base, repo.GitHubToken, repo.Name, "docker-compose.yml")
			if err != nil {
				slog.Warn("Convention check failed", "convention", "container-naming", "repo", repo.Name, "step", "fetch-compose", "error", err)
				return ConventionResult{
					Convention: "container-naming",
					Err:        fmt.Errorf("error fetching docker-compose.yml: %w", err),
				}
			}

			if content == nil {
				return ConventionResult{
					Convention: "container-naming",
					Pass:       true,
					Detail:     "docker-compose.yml not found; convention does not apply",
				}
			}

			var compose containerNamingComposeFile
			if err := yaml.Unmarshal(content, &compose); err != nil {
				slog.Warn("Convention check failed", "convention", "container-naming", "repo", repo.Name, "step", "parse-compose", "error", err)
				return ConventionResult{
					Convention: "container-naming",
					Pass:       false,
					Detail:     fmt.Sprintf("Failed to parse docker-compose.yml: %v", err),
				}
			}

			// Derive expected prefix from the repo name.
			// e.g. "lucas42/lucos_monitoring" → "lucos_monitoring"
			parts := strings.SplitN(repo.Name, "/", 2)
			repoShortName := parts[len(parts)-1]

			var violations []string
			for _, svc := range compose.Services {
				if svc.ContainerName == "" {
					// No explicit container_name — Docker Compose generates one.
					continue
				}
				if isTestProfileService(composeService{Profiles: svc.Profiles}) {
					continue
				}
				// Pass if container_name equals the repo name exactly or starts with repo_name + "_".
				if svc.ContainerName != repoShortName && !strings.HasPrefix(svc.ContainerName, repoShortName+"_") {
					violations = append(violations, svc.ContainerName)
				}
			}

			if len(violations) == 0 {
				return ConventionResult{
					Convention: "container-naming",
					Pass:       true,
					Detail:     "All container names follow the naming convention",
				}
			}

			return ConventionResult{
				Convention: "container-naming",
				Pass:       false,
				Detail:     fmt.Sprintf("Container names not following convention: %s", strings.Join(violations, ", ")),
			}
		},
	})
}
