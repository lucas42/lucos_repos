package conventions

import (
	"fmt"
	"log/slog"
	"strings"

	"gopkg.in/yaml.v3"
)

// composeBuild is a typed representation of a docker-compose service's build
// field when written as a mapping (build: {context: ., dockerfile: ...}).
type composeBuild struct {
	Context    string `yaml:"context"`
	Dockerfile string `yaml:"dockerfile"`
}

// dockerfilePathForBuild returns the Dockerfile path for a service's build
// config, which may be a plain string (context directory) or a mapping.
func dockerfilePathForBuild(build interface{}) string {
	switch v := build.(type) {
	case string:
		if v == "" || v == "." {
			return "Dockerfile"
		}
		return strings.TrimRight(v, "/") + "/Dockerfile"
	case map[string]interface{}:
		// Re-marshal to parse as composeBuild struct.
		var b composeBuild
		if raw, err := yaml.Marshal(v); err == nil {
			_ = yaml.Unmarshal(raw, &b)
		}
		if b.Dockerfile != "" {
			return b.Dockerfile
		}
		if b.Context == "" || b.Context == "." {
			return "Dockerfile"
		}
		return strings.TrimRight(b.Context, "/") + "/Dockerfile"
	}
	return "Dockerfile"
}

// dockerfileHasVersionArg reports whether a Dockerfile declares VERSION as a
// build argument (ARG VERSION or ARG VERSION=...).
func dockerfileHasVersionArg(content []byte) bool {
	for _, line := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "ARG VERSION" || strings.HasPrefix(trimmed, "ARG VERSION=") {
			return true
		}
	}
	return false
}

// dockerfileExposesVersionEnv reports whether a Dockerfile exposes the VERSION
// build arg as an environment variable. Accepts ENV VERSION=$VERSION,
// ENV VERSION=${VERSION}, and their legacy space-separated forms.
func dockerfileExposesVersionEnv(content []byte) bool {
	for _, line := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "ENV VERSION") &&
			(strings.Contains(trimmed, "$VERSION") || strings.Contains(trimmed, "${VERSION")) {
			return true
		}
	}
	return false
}

func init() {
	Register(Convention{
		ID:          "dockerfile-exposes-version",
		Description: "Every service Dockerfile declares ARG VERSION and exposes it as ENV VERSION=$VERSION",
		Rationale:   "The deploy orb sets VERSION at build time via `VERSION=$NEXT_VERSION docker compose build`. For the running container to report its own version (e.g. via the `/_info` endpoint), the build arg must be declared with `ARG VERSION` and then persisted as an environment variable with `ENV VERSION=$VERSION`. Without both instructions, the VERSION variable is unavailable at runtime.",
		Guidance:    "Add the following two lines to every service Dockerfile, after the FROM instruction and before the COPY/RUN steps:\n\n```dockerfile\nARG VERSION\nENV VERSION=$VERSION\n```\n\nNote that Docker build args are scoped — if your Dockerfile uses multi-stage builds, you may need to repeat `ARG VERSION` in each stage that needs it.",
		AppliesTo:   []RepoType{RepoTypeSystem},
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}

			// Step 1: find all Dockerfiles referenced by built services.
			composeContent, err := GitHubFileContentFromBase(base, repo.GitHubToken, repo.Name, "docker-compose.yml", repo.Ref)
			if err != nil {
				slog.Warn("Convention check failed", "convention", "dockerfile-exposes-version", "repo", repo.Name, "step", "fetch-compose", "error", err)
				return ConventionResult{
					Convention: "dockerfile-exposes-version",
					Err:        fmt.Errorf("error fetching docker-compose.yml: %w", err),
				}
			}
			if composeContent == nil {
				return ConventionResult{
					Convention: "dockerfile-exposes-version",
					Pass:       true,
					Detail:     "docker-compose.yml not found; convention does not apply",
				}
			}

			var compose composeFile
			if err := yaml.Unmarshal(composeContent, &compose); err != nil {
				slog.Warn("Convention check failed", "convention", "dockerfile-exposes-version", "repo", repo.Name, "step", "parse-compose", "error", err)
				return ConventionResult{
					Convention: "dockerfile-exposes-version",
					Pass:       false,
					Detail:     fmt.Sprintf("Failed to parse docker-compose.yml: %v", err),
				}
			}

			// Collect unique Dockerfile paths for built, non-test-profile services.
			seen := map[string]bool{}
			var dockerfilePaths []string
			for _, svc := range compose.Services {
				if svc.Build == nil || isTestProfileService(svc) {
					continue
				}
				path := dockerfilePathForBuild(svc.Build)
				if !seen[path] {
					seen[path] = true
					dockerfilePaths = append(dockerfilePaths, path)
				}
			}

			if len(dockerfilePaths) == 0 {
				return ConventionResult{
					Convention: "dockerfile-exposes-version",
					Pass:       true,
					Detail:     "No built services found; convention does not apply",
				}
			}

			// Step 2: check each Dockerfile.
			var missingArg []string
			var missingEnv []string

			for _, dfPath := range dockerfilePaths {
				dfContent, err := GitHubFileContentFromBase(base, repo.GitHubToken, repo.Name, dfPath, repo.Ref)
				if err != nil {
					slog.Warn("Convention check failed", "convention", "dockerfile-exposes-version", "repo", repo.Name, "step", "fetch-dockerfile", "path", dfPath, "error", err)
					return ConventionResult{
						Convention: "dockerfile-exposes-version",
						Err:        fmt.Errorf("error fetching %s: %w", dfPath, err),
					}
				}
				if dfContent == nil {
					return ConventionResult{
						Convention: "dockerfile-exposes-version",
						Pass:       false,
						Detail:     fmt.Sprintf("%s not found", dfPath),
					}
				}

				if !dockerfileHasVersionArg(dfContent) {
					missingArg = append(missingArg, dfPath)
				}
				if !dockerfileExposesVersionEnv(dfContent) {
					missingEnv = append(missingEnv, dfPath)
				}
			}

			if len(missingArg) == 0 && len(missingEnv) == 0 {
				return ConventionResult{
					Convention: "dockerfile-exposes-version",
					Pass:       true,
					Detail:     "All service Dockerfiles declare ARG VERSION and ENV VERSION=$VERSION",
				}
			}

			var parts []string
			if len(missingArg) > 0 {
				parts = append(parts, fmt.Sprintf("missing ARG VERSION: %s", strings.Join(missingArg, ", ")))
			}
			if len(missingEnv) > 0 {
				parts = append(parts, fmt.Sprintf("missing ENV VERSION=$VERSION: %s", strings.Join(missingEnv, ", ")))
			}
			return ConventionResult{
				Convention: "dockerfile-exposes-version",
				Pass:       false,
				Detail:     strings.Join(parts, "; "),
			}
		},
	})
}
