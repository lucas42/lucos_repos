package conventions

import (
	"fmt"
	"log/slog"
	"strings"

	"gopkg.in/yaml.v3"
)

// composeFile is the minimal structure we need from a docker-compose.yml.
// We parse only the services map, and within each service only the keys we
// care about (build and healthcheck).
type composeFile struct {
	Services map[string]composeService `yaml:"services"`
}

// composeService holds the subset of docker-compose service fields we inspect.
// We use interface{} so the YAML decoder accepts both scalar (build: .) and
// mapping (build: {context: .}) forms without type errors. A nil value means
// the key was absent.
type composeService struct {
	Build       interface{} `yaml:"build"`
	Healthcheck interface{} `yaml:"healthcheck"`
}

func init() {
	// docker-healthcheck-on-built-services: every service we build must define
	// a Docker healthcheck so that docker compose up -d waits until the
	// container is actually ready before signalling success.
	Register(Convention{
		ID:          "docker-healthcheck-on-built-services",
		Description: "Every service with a build: key in docker-compose.yml also defines a healthcheck:",
		Rationale:   "Without a Docker healthcheck, `docker compose up -d` returns as soon as the container *starts*, not when it is ready to serve traffic. The deploy suppression mechanism in lucos_monitoring clears suppression at that moment — meaning monitoring polls `/_info` before the process is listening, causing a consistent blip after every deploy. Adding a healthcheck makes Docker wait until the service is actually healthy before signalling readiness.",
		Guidance:    "Add a `healthcheck:` block to every service in `docker-compose.yml` that has a `build:` key. For HTTP services, a suitable target is the `/_info` endpoint, for example:\n\n```yaml\nhealthcheck:\n  test: [\"CMD\", \"wget\", \"-qO-\", \"http://localhost:${PORT}/_info\"]\n  interval: 10s\n  timeout: 5s\n  retries: 3\n  start_period: 15s\n```\n\nOff-the-shelf images (redis, postgres, etc.) are excluded — this rule only applies to services your repo builds from a Dockerfile.",
		AppliesTo:   []RepoType{RepoTypeSystem, RepoTypeComponent},
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}

			content, err := GitHubFileContentFromBase(base, repo.GitHubToken, repo.Name, "docker-compose.yml")
			if err != nil {
				slog.Warn("Convention check failed", "convention", "docker-healthcheck-on-built-services", "repo", repo.Name, "step", "fetch-compose", "error", err)
				return ConventionResult{
					Convention: "docker-healthcheck-on-built-services",
					Pass:       false,
					Detail:     fmt.Sprintf("Error fetching docker-compose.yml: %v", err),
				}
			}

			if content == nil {
				// No docker-compose.yml — convention does not apply.
				return ConventionResult{
					Convention: "docker-healthcheck-on-built-services",
					Pass:       true,
					Detail:     "docker-compose.yml not found; convention does not apply",
				}
			}

			var compose composeFile
			if err := yaml.Unmarshal(content, &compose); err != nil {
				slog.Warn("Convention check failed", "convention", "docker-healthcheck-on-built-services", "repo", repo.Name, "step", "parse-compose", "error", err)
				return ConventionResult{
					Convention: "docker-healthcheck-on-built-services",
					Pass:       false,
					Detail:     fmt.Sprintf("Failed to parse docker-compose.yml: %v", err),
				}
			}

			var missing []string
			for name, svc := range compose.Services {
				if svc.Build != nil && svc.Healthcheck == nil {
					missing = append(missing, name)
				}
			}

			if len(missing) == 0 {
				return ConventionResult{
					Convention: "docker-healthcheck-on-built-services",
					Pass:       true,
					Detail:     "All built services define a healthcheck",
				}
			}

			return ConventionResult{
				Convention: "docker-healthcheck-on-built-services",
				Pass:       false,
				Detail:     fmt.Sprintf("Built services missing a healthcheck: %s", strings.Join(missing, ", ")),
			}
		},
	})
}
