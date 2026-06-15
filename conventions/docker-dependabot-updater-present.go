package conventions

import (
	"fmt"
	"log/slog"
	"strings"

	"gopkg.in/yaml.v3"
)

// dockerUpdaterDependabotConfig is a minimal parse of dependabot.yml that
// captures only the fields needed for the docker-updater-present check.
type dockerUpdaterDependabotConfig struct {
	Updates []dockerUpdaterDependabotEntry `yaml:"updates"`
}

type dockerUpdaterDependabotEntry struct {
	PackageEcosystem string   `yaml:"package-ecosystem"`
	Directory        string   `yaml:"directory"`
	Directories      []string `yaml:"directories"`
}

// coveredDirectories returns the set of directories covered by this entry,
// normalised from whichever form was used (singular directory: or plural
// directories:).
func (e dockerUpdaterDependabotEntry) coveredDirectories() []string {
	if len(e.Directories) > 0 {
		return e.Directories
	}
	if e.Directory != "" {
		return []string{e.Directory}
	}
	return nil
}

// dependabotDirForDockerfile converts a Dockerfile path to the corresponding
// dependabot.yml directory value.
//
//	"Dockerfile"      → "/"
//	"api/Dockerfile"  → "/api"
func dependabotDirForDockerfile(dfPath string) string {
	lastSlash := strings.LastIndex(dfPath, "/")
	if lastSlash < 0 {
		return "/"
	}
	return "/" + dfPath[:lastSlash]
}

func init() {
	Register(Convention{
		ID:          "docker-dependabot-updater-present",
		Description: "Every repo that builds Docker images has a docker Dependabot updater entry for each built-service Dockerfile directory",
		Rationale: "A missing docker Dependabot entry means the repo is completely unmonitored for " +
			"base-image CVEs. A FROM python:/golang:/node:/etc. line pulls in an OS package set " +
			"and a language runtime, both of which accrue CVEs over time. Without a docker updater " +
			"entry, those vulnerabilities are never surfaced to the team — even though the same " +
			"rationale that mandates github-actions monitoring (supply chain attacks, dependency " +
			"drift) applies at least as strongly to base images. Non-Python base images (Go, Node, " +
			"Rust, Java, etc.) are just as affected; the absence of a docker entry is silently " +
			"ignored today for all non-Python repos. This convention closes that gap.",
		Guidance: "Add a `docker` entry to `.github/dependabot.yml` for each directory that contains " +
			"a built-service Dockerfile. For a repo with a single Dockerfile at the root:\n\n" +
			"```yaml\nupdates:\n  - package-ecosystem: docker\n    directory: /\n    schedule:\n" +
			"      interval: weekly\n    allow:\n      - dependency-type: all\n    groups:\n" +
			"      minor-and-patch:\n        update-types: [minor, patch]\n      major:\n" +
			"        update-types: [major]\n```\n\n" +
			"For repos with multiple Dockerfiles (e.g. `api/Dockerfile` and `worker/Dockerfile`), " +
			"add a separate entry for each directory, or use the `directories:` plural form:\n\n" +
			"```yaml\n  - package-ecosystem: docker\n    directories: [\"/api\", \"/worker\"]\n" +
			"    schedule:\n      interval: weekly\n    allow:\n      - dependency-type: all\n" +
			"    groups:\n      minor-and-patch:\n        update-types: [minor, patch]\n      major:\n" +
			"        update-types: [major]\n```\n\n" +
			"The `allow: dependency-type: all` and `groups:` blocks are required by the " +
			"`dependabot-configured` convention, which will flag their absence independently.",
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}

			conventionID := "docker-dependabot-updater-present"

			// Step 1: fetch docker-compose.yml. If absent, no built services → skip.
			composeContent, err := GitHubFileContentFromBase(base, repo.GitHubToken, repo.Name, "docker-compose.yml", repo.Ref)
			if err != nil {
				slog.Warn("Convention check failed", "convention", conventionID, "repo", repo.Name, "step", "fetch-compose", "error", err)
				return ConventionResult{Convention: conventionID, Err: fmt.Errorf("error fetching docker-compose.yml: %w", err)}
			}
			if composeContent == nil {
				return ConventionResult{Convention: conventionID, Pass: true, Detail: "no docker-compose.yml; convention does not apply"}
			}

			var compose composeFile
			if err := yaml.Unmarshal(composeContent, &compose); err != nil {
				slog.Warn("Convention check failed", "convention", conventionID, "repo", repo.Name, "step", "parse-compose", "error", err)
				return ConventionResult{Convention: conventionID, Err: fmt.Errorf("error parsing docker-compose.yml: %w", err)}
			}

			// Step 2: collect unique dependabot directories for built, non-test services.
			seen := map[string]bool{}
			var requiredDirs []string
			for _, svc := range compose.Services {
				if svc.Build == nil || isTestProfileService(svc) {
					continue
				}
				path := dockerfilePathForBuild(svc.Build)
				dir := dependabotDirForDockerfile(path)
				if !seen[dir] {
					seen[dir] = true
					requiredDirs = append(requiredDirs, dir)
				}
			}

			if len(requiredDirs) == 0 {
				return ConventionResult{Convention: conventionID, Pass: true, Detail: "no built services; convention does not apply"}
			}

			// Step 3: fetch and parse dependabot.yml.
			dependabotContent, err := GitHubFileContentFromBase(base, repo.GitHubToken, repo.Name, dependabotPath, repo.Ref)
			if err != nil {
				slog.Warn("Convention check failed", "convention", conventionID, "repo", repo.Name, "step", "fetch-dependabot", "error", err)
				return ConventionResult{Convention: conventionID, Err: fmt.Errorf("error fetching %s: %w", dependabotPath, err)}
			}
			if dependabotContent == nil {
				return ConventionResult{
					Convention: conventionID,
					Pass:       false,
					Detail:     fmt.Sprintf("dependabot.yml not found; missing docker updater entries for: %s", strings.Join(requiredDirs, ", ")),
				}
			}

			var depConfig dockerUpdaterDependabotConfig
			if err := yaml.Unmarshal(dependabotContent, &depConfig); err != nil {
				slog.Warn("Convention check failed", "convention", conventionID, "repo", repo.Name, "step", "parse-dependabot", "error", err)
				return ConventionResult{Convention: conventionID, Err: fmt.Errorf("error parsing %s: %w", dependabotPath, err)}
			}

			// Build a set of directories covered by docker updater entries.
			// Supports both singular directory: and plural directories: forms.
			coveredDirs := map[string]bool{}
			for _, u := range depConfig.Updates {
				if u.PackageEcosystem != "docker" {
					continue
				}
				for _, d := range u.coveredDirectories() {
					coveredDirs[d] = true
				}
			}

			// Step 4: check each required directory has a docker entry.
			var missing []string
			for _, dir := range requiredDirs {
				if !coveredDirs[dir] {
					missing = append(missing, fmt.Sprintf("%q", dir))
				}
			}

			if len(missing) == 0 {
				return ConventionResult{Convention: conventionID, Pass: true, Detail: "docker Dependabot updater entries present for all built-service Dockerfile directories"}
			}

			return ConventionResult{
				Convention: conventionID,
				Pass:       false,
				Detail:     fmt.Sprintf("missing docker Dependabot updater entries for: %s", strings.Join(missing, ", ")),
			}
		},
	})
}
