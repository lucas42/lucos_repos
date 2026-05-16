package conventions

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// envVarPassthroughDetector defines a per-language strategy for finding env var
// reads in source code. Each language gets its own set of file extensions,
// regex patterns, and comment prefix for noenv opt-out annotations.
type envVarPassthroughDetector struct {
	// extensions is the set of file extensions this detector applies to (e.g. ".py").
	extensions []string
	// patterns are the compiled regexes. Each must have exactly one capture group
	// that returns the env var name, restricted to [A-Z_]+ to avoid false positives.
	patterns []*regexp.Regexp
	// noenvPrefix is the line-comment prefix used for opt-out annotations.
	// e.g. "# lucos_repos: noenv" for Python/Ruby, "// lucos_repos: noenv" for Go/JS.
	noenvPrefix string
}

// langDetectors is the ordered set of per-language env var read detectors
// covering the lucos estate's languages.
var langDetectors = []envVarPassthroughDetector{
	{
		extensions: []string{".py"},
		patterns: []*regexp.Regexp{
			regexp.MustCompile(`os\.environ\.get\(["']([A-Z_]+)["']`),
			regexp.MustCompile(`os\.environ\[["']([A-Z_]+)["']\]`),
			regexp.MustCompile(`os\.getenv\(["']([A-Z_]+)["']`),
		},
		noenvPrefix: "# lucos_repos: noenv",
	},
	{
		extensions: []string{".rb"},
		patterns: []*regexp.Regexp{
			regexp.MustCompile(`ENV\[["']([A-Z_]+)["']\]`),
			regexp.MustCompile(`ENV\.fetch\(["']([A-Z_]+)["']`),
		},
		noenvPrefix: "# lucos_repos: noenv",
	},
	{
		extensions: []string{".erl"},
		patterns: []*regexp.Regexp{
			regexp.MustCompile(`os:getenv\(["']([A-Z_]+)["']`),
		},
		noenvPrefix: "% lucos_repos: noenv",
	},
	{
		extensions: []string{".go"},
		patterns: []*regexp.Regexp{
			regexp.MustCompile(`os\.Getenv\(["']([A-Z_]+)["']`),
			regexp.MustCompile(`os\.LookupEnv\(["']([A-Z_]+)["']`),
		},
		noenvPrefix: "// lucos_repos: noenv",
	},
	{
		extensions: []string{".js", ".mjs", ".ts"},
		patterns: []*regexp.Regexp{
			regexp.MustCompile(`process\.env\.([A-Z_]+)`),
			regexp.MustCompile(`process\.env\[["']([A-Z_]+)["']\]`),
		},
		noenvPrefix: "// lucos_repos: noenv",
	},
}

// envPassthroughExtMap maps each file extension to its detector for O(1) lookup.
// Populated at init time from langDetectors.
var envPassthroughExtMap map[string]*envVarPassthroughDetector

// passthroughOnlyEnvVars extracts env var names declared as passthrough (bare
// name, no hardcoded value) across all services in a parsed docker-compose.yml.
//
// In list form, passthrough is any entry without "=" (e.g. "PORT" not "PORT=8080").
// In map form, passthrough is any entry with a null value ("PORT:" without a value).
// Entries with hardcoded values (KEY=val in list form, KEY: val in map form) are
// intentional config — the code can depend on them without lucos_creds wiring and
// they must NOT count as passthrough.
func passthroughOnlyEnvVars(compose envVarsComposeFile) map[string]bool {
	vars := make(map[string]bool)
	for _, svc := range compose.Services {
		node := &svc.Environment
		switch node.Kind {
		case yaml.SequenceNode:
			// List form: ["PORT", "FOO=bar", …]
			// Only bare entries (no "=") are passthrough.
			for _, item := range node.Content {
				name := item.Value
				if !strings.Contains(name, "=") {
					vars[name] = true
				}
			}
		case yaml.MappingNode:
			// Map form: {PORT: null, FOO: "bar"}
			// Only entries with a null value are passthrough.
			for i := 0; i+1 < len(node.Content); i += 2 {
				key := node.Content[i].Value
				val := node.Content[i+1]
				if val.Tag == "!!null" || val.Value == "" {
					vars[key] = true
				}
			}
		}
	}
	return vars
}

// envVarReading records a single env var read detected in source code.
type envVarReading struct {
	VarName string
	File    string
	Line    int
}

// scanFileForEnvVars scans a single source file's content for env var reads
// using the appropriate language detector. It honours noenv opt-out annotations.
func scanFileForEnvVars(path string, content []byte) []envVarReading {
	ext := filepath.Ext(path)
	detector := envPassthroughExtMap[ext]
	if detector == nil {
		return nil
	}

	var readings []envVarReading
	lines := strings.Split(string(content), "\n")
	for lineIdx, line := range lines {
		lineNum := lineIdx + 1
		// Deduplicate detections per line per var name (multiple patterns may match).
		seen := make(map[string]bool)
		for _, pat := range detector.patterns {
			matches := pat.FindAllStringSubmatch(line, -1)
			for _, m := range matches {
				if len(m) < 2 {
					continue
				}
				varName := m[1]
				if seen[varName] {
					continue
				}
				if isEnvNoenvSuppressed(line, varName, detector.noenvPrefix) {
					continue
				}
				seen[varName] = true
				readings = append(readings, envVarReading{
					VarName: varName,
					File:    path,
					Line:    lineNum,
				})
			}
		}
	}
	return readings
}

// isEnvNoenvSuppressed reports whether a noenv annotation on the given line
// suppresses the named variable. The annotation form is:
//
//	<noenvPrefix> VARNAME
//
// anywhere on the line. Only the first word after the prefix is matched.
func isEnvNoenvSuppressed(line, varName, noenvPrefix string) bool {
	idx := strings.Index(line, noenvPrefix)
	if idx < 0 {
		return false
	}
	rest := strings.TrimSpace(line[idx+len(noenvPrefix):])
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return false
	}
	return fields[0] == varName
}

func init() {
	// Build extension→detector map.
	envPassthroughExtMap = make(map[string]*envVarPassthroughDetector)
	for i := range langDetectors {
		d := &langDetectors[i]
		for _, ext := range d.extensions {
			envPassthroughExtMap[ext] = d
		}
	}

	Register(Convention{
		ID:          "env_var_passthrough",
		Description: "Every env var read by application code is declared as passthrough in docker-compose.yml",
		Rationale: "Docker Compose only forwards variables listed in a service's `environment:` block into " +
			"the container. A variable read by application code but absent from that block is silently " +
			"empty at runtime — the feature breaks without any error or alert. This was the root cause of " +
			"the 2026-05-13 monitoring blackout (`lucos_monitoring#234`), where `SCHEDULE_TRACKER_ENDPOINT` " +
			"was read in code but never added to `docker-compose.yml`.",
		Guidance: "Add each missing variable as a bare passthrough entry in the `environment:` block " +
			"of `docker-compose.yml`. For example:\n\n```yaml\nenvironment:\n  - PORT\n  - MY_VAR\n```\n\n" +
			"If the variable is set to a hardcoded value directly in compose (`MY_VAR=fixed`), that is " +
			"intentional config — no change needed. If the variable is genuinely read only in tests or " +
			"other contexts where it does not need to be injected by compose, add a `# lucos_repos: noenv MY_VAR` " +
			"annotation (using the appropriate comment syntax for the language) to the line where it is read.",
		AppliesTo: []RepoType{RepoTypeSystem},
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}

			// Fetch and parse docker-compose.yml.
			composeContent, err := GitHubFileContentFromBase(base, repo.GitHubToken, repo.Name, "docker-compose.yml", repo.Ref)
			if err != nil {
				slog.Warn("Convention check failed", "convention", "env_var_passthrough", "repo", repo.Name, "step", "fetch-compose", "error", err)
				return ConventionResult{
					Convention: "env_var_passthrough",
					Err:        fmt.Errorf("error fetching docker-compose.yml: %w", err),
				}
			}
			if composeContent == nil {
				return ConventionResult{
					Convention: "env_var_passthrough",
					Pass:       true,
					Detail:     "docker-compose.yml not found; convention does not apply",
				}
			}

			var compose envVarsComposeFile
			if err := yaml.Unmarshal(composeContent, &compose); err != nil {
				slog.Warn("Convention check failed", "convention", "env_var_passthrough", "repo", repo.Name, "step", "parse-compose", "error", err)
				return ConventionResult{
					Convention: "env_var_passthrough",
					Pass:       false,
					Detail:     fmt.Sprintf("Failed to parse docker-compose.yml: %v", err),
				}
			}

			passthrough := passthroughOnlyEnvVars(compose)

			// Fetch the repo tree to enumerate source files.
			tree, err := GitHubRepoTreeFromBase(base, repo.GitHubToken, repo.Name)
			if err != nil {
				slog.Warn("Convention check failed", "convention", "env_var_passthrough", "repo", repo.Name, "step", "fetch-tree", "error", err)
				return ConventionResult{
					Convention: "env_var_passthrough",
					Err:        fmt.Errorf("error fetching repo tree: %w", err),
				}
			}
			if tree.Truncated {
				return ConventionResult{
					Convention: "env_var_passthrough",
					Err:        fmt.Errorf("git tree response was truncated for %s; cannot reliably scan for env var reads", repo.Name),
				}
			}

			// Scan each source file for env var reads.
			// missing maps varName → first "file:line" location for reporting.
			missing := make(map[string]string)
			for _, entry := range tree.Tree {
				if entry.Type != "blob" {
					continue
				}
				if strings.HasPrefix(entry.Path, "conventions/") {
					continue
				}
				if envPassthroughExtMap[filepath.Ext(entry.Path)] == nil {
					continue
				}
				if entry.Size >= 500000 {
					continue
				}

				content, err := GitHubBlobContent(base, repo.GitHubToken, repo.Name, entry.SHA)
				if err != nil {
					slog.Warn("Failed to fetch blob", "convention", "env_var_passthrough", "repo", repo.Name, "path", entry.Path, "error", err)
					continue
				}

				for _, reading := range scanFileForEnvVars(entry.Path, content) {
					if passthrough[reading.VarName] {
						continue // Already declared as passthrough — fine.
					}
					if _, alreadyRecorded := missing[reading.VarName]; !alreadyRecorded {
						missing[reading.VarName] = fmt.Sprintf("%s:%d", reading.File, reading.Line)
					}
				}
			}

			if len(missing) == 0 {
				return ConventionResult{
					Convention: "env_var_passthrough",
					Pass:       true,
					Detail:     "All env vars read by application code are declared as passthrough in docker-compose.yml",
				}
			}

			// Build a sorted, deterministic failure message.
			varNames := make([]string, 0, len(missing))
			for v := range missing {
				varNames = append(varNames, v)
			}
			sort.Strings(varNames)

			var details []string
			for _, v := range varNames {
				details = append(details, fmt.Sprintf("%s (first read at %s)", v, missing[v]))
			}
			return ConventionResult{
				Convention: "env_var_passthrough",
				Pass:       false,
				Detail: fmt.Sprintf(
					"Env vars read in code but missing from docker-compose.yml passthrough: %s",
					strings.Join(details, "; "),
				),
			}
		},
	})
}
