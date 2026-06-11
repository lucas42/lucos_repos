package conventions

import (
	"fmt"
	"log/slog"
	"strings"

	"gopkg.in/yaml.v3"
)

// pythonPrereleaseDependabotConfig is a minimal parse of dependabot.yml that
// captures only the fields needed for the python pre-release ignore check.
type pythonPrereleaseDependabotConfig struct {
	Updates []pythonPrereleaseDependabotUpdate `yaml:"updates"`
}

type pythonPrereleaseDependabotUpdate struct {
	PackageEcosystem string                                 `yaml:"package-ecosystem"`
	Directory        string                                 `yaml:"directory"`
	Directories      []string                               `yaml:"directories"`
	Ignore           []pythonPrereleaseDependabotIgnoreRule `yaml:"ignore"`
}

type pythonPrereleaseDependabotIgnoreRule struct {
	DependencyName string   `yaml:"dependency-name"`
	Versions       []string `yaml:"versions"`
}

// hasPythonBaseImage reports whether a Dockerfile contains any line of the
// form FROM python:... (indicating the image uses a Python base image).
func hasPythonBaseImage(content []byte) bool {
	for _, line := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "FROM python:") {
			return true
		}
	}
	return false
}

// pythonBaseImageTag extracts the image reference from the first FROM python:
// line in a Dockerfile (e.g. "python:3.15.0b2-alpine"). Returns "" if not found.
func pythonBaseImageTag(content []byte) string {
	for _, line := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "FROM python:") {
			// Return everything up to the first space (strips AS name)
			parts := strings.Fields(trimmed)
			return parts[1]
		}
	}
	return ""
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

// hasPythonIgnoreRule reports whether a docker dependabot entry has at least
// one ignore rule targeting dependency-name "python".
func hasPythonIgnoreRule(u pythonPrereleaseDependabotUpdate) bool {
	for _, rule := range u.Ignore {
		if rule.DependencyName == "python" {
			return true
		}
	}
	return false
}

func init() {
	Register(Convention{
		ID:          "dependabot-python-prerelease-ignore",
		Description: "Python-Docker repos have a Dependabot ignore rule suppressing Python pre-release base-image tags (alpha/beta/rc)",
		Rationale: "When Python releases a new minor version, it goes through alpha, beta, and " +
			"release-candidate stages before the stable release. These pre-release images are " +
			"missing compiled wheels for many C-extension packages (bcrypt, cffi, cryptography, " +
			"etc.), causing pipenv/pip installs to fail with build errors inside the container. " +
			"Dependabot treats pre-release tags as newer versions and will create bumping PRs for " +
			"them. Without an ignore rule, those PRs break CI. " +
			"The ignore rule must cover alpha (`.pre.alpine.a`), beta (`.pre.alpine.b`), and " +
			"release-candidate (`.pre.alpine.rc*`) normalized forms — Dependabot normalises " +
			"`python:3.15.0a2-alpine` to `3.15.0a2.pre.alpine.a` internally and applies the " +
			"ignore versions as globs against that form. Non-alpine variants use the same scheme " +
			"with their own suffix (e.g. `.pre.slim.a` for `3.15.0a2-slim`).",
		Guidance: "Add `ignore` rules to every `docker` entry in `.github/dependabot.yml` that " +
			"monitors a Python-base-image directory.\n\n" +
			"For **alpine** variants (e.g. `python:3.x-alpine`):\n\n" +
			"```yaml\nignore:\n  - dependency-name: \"python\"\n    versions:\n" +
			"      - \"*.pre.alpine.a\"   # alpha (e.g. 3.15.0a2-alpine)\n" +
			"      - \"*.pre.alpine.b\"   # beta  (e.g. 3.15.0b2-alpine)\n" +
			"      - \"*.pre.alpine.rc*\" # rc    (e.g. 3.15.0rc1-alpine)\n```\n\n" +
			"For **slim** variants (e.g. `python:3.x-slim`):\n\n" +
			"```yaml\nignore:\n  - dependency-name: \"python\"\n    versions:\n" +
			"      - \"*.pre.slim.a\"\n" +
			"      - \"*.pre.slim.b\"\n" +
			"      - \"*.pre.slim.rc*\"\n```\n\n" +
			"The normalized internal form is `{version}.pre.{variant}.{prerelease_letter}` " +
			"(evidenced by `3.15.0a2-alpine → 3.15.0a2.pre.alpine.a`, confirmed by " +
			"lucas42/lucos_backups#189). The `*.pre.alpine.a` wildcard is confirmed effective " +
			"for alpha tags. The beta (`*.pre.alpine.b`) and rc (`*.pre.alpine.rc*`) forms " +
			"follow the same normalization scheme but have not been empirically verified — " +
			"if a pre-release PR appears after adding the ignore rule, fall back to the " +
			"per-series range syntax from lucas42/lucos_backups#186: " +
			"`>= 3.15.0b1.pre.alpine.b, < 3.15.1`.",
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}

			conventionID := "dependabot-python-prerelease-ignore"

			// Step 1: fetch docker-compose.yml. If absent, no Docker services → skip.
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

			// Step 2: collect unique Dockerfile paths for built, non-test services.
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
				return ConventionResult{Convention: conventionID, Pass: true, Detail: "no built services; convention does not apply"}
			}

			// Step 3: identify which Dockerfiles use a python:* base image.
			type pythonDockerEntry struct {
				dfPath    string
				depDir    string
				baseImage string
			}
			var pythonEntries []pythonDockerEntry

			for _, dfPath := range dockerfilePaths {
				dfContent, err := GitHubFileContentFromBase(base, repo.GitHubToken, repo.Name, dfPath, repo.Ref)
				if err != nil {
					slog.Warn("Convention check failed", "convention", conventionID, "repo", repo.Name, "step", "fetch-dockerfile", "path", dfPath, "error", err)
					return ConventionResult{Convention: conventionID, Err: fmt.Errorf("error fetching %s: %w", dfPath, err)}
				}
				if dfContent == nil {
					// Missing Dockerfile — let another convention flag this.
					continue
				}
				if hasPythonBaseImage(dfContent) {
					pythonEntries = append(pythonEntries, pythonDockerEntry{
						dfPath:    dfPath,
						depDir:    dependabotDirForDockerfile(dfPath),
						baseImage: pythonBaseImageTag(dfContent),
					})
				}
			}

			if len(pythonEntries) == 0 {
				return ConventionResult{Convention: conventionID, Pass: true, Detail: "no Python base images found; convention does not apply"}
			}

			// Step 4: fetch and parse dependabot.yml.
			dependabotContent, err := GitHubFileContentFromBase(base, repo.GitHubToken, repo.Name, dependabotPath, repo.Ref)
			if err != nil {
				slog.Warn("Convention check failed", "convention", conventionID, "repo", repo.Name, "step", "fetch-dependabot", "error", err)
				return ConventionResult{Convention: conventionID, Err: fmt.Errorf("error fetching %s: %w", dependabotPath, err)}
			}
			if dependabotContent == nil {
				// No dependabot.yml at all — docker-dependabot-updater-present will flag
				// the missing docker entries. Nothing to check here.
				return ConventionResult{
					Convention: conventionID,
					Pass:       true,
					Detail:     "dependabot.yml not found; docker-dependabot-updater-present will flag missing entries",
				}
			}

			var depConfig pythonPrereleaseDependabotConfig
			if err := yaml.Unmarshal(dependabotContent, &depConfig); err != nil {
				slog.Warn("Convention check failed", "convention", conventionID, "repo", repo.Name, "step", "parse-dependabot", "error", err)
				return ConventionResult{Convention: conventionID, Err: fmt.Errorf("error parsing %s: %w", dependabotPath, err)}
			}

			// Build a lookup: docker ecosystem directory → update entry.
			// Supports both singular directory: and plural directories: forms.
			dockerUpdates := map[string]pythonPrereleaseDependabotUpdate{}
			for _, u := range depConfig.Updates {
				if u.PackageEcosystem != "docker" {
					continue
				}
				dirs := u.Directories
				if len(dirs) == 0 && u.Directory != "" {
					dirs = []string{u.Directory}
				}
				for _, d := range dirs {
					dockerUpdates[d] = u
				}
			}

			// Step 5: check each Python directory has the ignore rule.
			// If no docker entry exists for a directory, defer to
			// docker-dependabot-updater-present to raise the missing-entry issue —
			// this convention's single responsibility is checking that an EXISTING
			// docker entry carries the Python pre-release ignore rule.
			var failures []string
			for _, entry := range pythonEntries {
				u, ok := dockerUpdates[entry.depDir]
				if !ok {
					// No docker entry — docker-dependabot-updater-present will flag this.
					continue
				}
				if !hasPythonIgnoreRule(u) {
					failures = append(failures, fmt.Sprintf("docker entry for %q is missing a Python pre-release ignore rule (base image: %s)", entry.depDir, entry.baseImage))
				}
			}

			if len(failures) == 0 {
				return ConventionResult{Convention: conventionID, Pass: true, Detail: "Python pre-release ignore rules present on all docker dependabot entries"}
			}

			return ConventionResult{
				Convention: conventionID,
				Pass:       false,
				Detail:     strings.Join(failures, "; "),
			}
		},
	})
}
