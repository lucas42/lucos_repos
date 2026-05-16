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
// regex patterns, comment prefix for noenv opt-out annotations, and test-path
// exclusion rules.
type envVarPassthroughDetector struct {
	// extensions is the set of file extensions this detector applies to (e.g. ".py").
	extensions []string
	// patterns are the compiled regexes. Each must have exactly one capture group
	// that returns the env var name, restricted to [A-Z_]+ to avoid false positives.
	patterns []*regexp.Regexp
	// noenvPrefix is the line-comment prefix used for opt-out annotations.
	// e.g. "# lucos_repos: noenv" for Python/Ruby, "// lucos_repos: noenv" for Go/JS.
	noenvPrefix string
	// testDirs is the set of directory path-segment names that indicate test code.
	// Matched by splitting the path on "/" and checking each non-final segment.
	testDirs map[string]bool
	// isTestFilename reports whether the given file basename identifies test code.
	isTestFilename func(base string) bool
}

// langDetectors is the ordered set of per-language env var read detectors
// covering the lucos estate's languages. Each entry includes test-path exclusion
// rules (directory names and filename patterns) so that test files are not
// scanned — they don't run inside the production container.
var langDetectors = []envVarPassthroughDetector{
	{
		extensions: []string{".py"},
		patterns: []*regexp.Regexp{
			regexp.MustCompile(`os\.environ\.get\(["']([A-Z_]+)["']`),
			regexp.MustCompile(`os\.environ\[["']([A-Z_]+)["']\]`),
			regexp.MustCompile(`os\.getenv\(["']([A-Z_]+)["']`),
		},
		noenvPrefix: "# lucos_repos: noenv",
		testDirs:    map[string]bool{"tests": true, "test": true},
		isTestFilename: func(base string) bool {
			return strings.HasPrefix(base, "test_") ||
				strings.HasSuffix(base, "_test.py") ||
				base == "conftest.py"
		},
	},
	{
		extensions: []string{".rb"},
		patterns: []*regexp.Regexp{
			regexp.MustCompile(`ENV\[["']([A-Z_]+)["']\]`),
			regexp.MustCompile(`ENV\.fetch\(["']([A-Z_]+)["']`),
		},
		noenvPrefix: "# lucos_repos: noenv",
		testDirs:    map[string]bool{"spec": true, "test": true},
		isTestFilename: func(base string) bool {
			return strings.HasSuffix(base, "_spec.rb") ||
				strings.HasSuffix(base, "_test.rb") ||
				strings.HasPrefix(base, "test_")
		},
	},
	{
		extensions: []string{".erl"},
		patterns: []*regexp.Regexp{
			regexp.MustCompile(`os:getenv\(["']([A-Z_]+)["']`),
		},
		noenvPrefix: "% lucos_repos: noenv",
		testDirs:    map[string]bool{"test": true},
		isTestFilename: func(base string) bool {
			return strings.HasSuffix(base, "_SUITE.erl") ||
				strings.HasSuffix(base, "_tests.erl")
		},
	},
	{
		extensions: []string{".go"},
		patterns: []*regexp.Regexp{
			regexp.MustCompile(`os\.Getenv\(["']([A-Z_]+)["']`),
			regexp.MustCompile(`os\.LookupEnv\(["']([A-Z_]+)["']`),
		},
		noenvPrefix: "// lucos_repos: noenv",
		testDirs:    map[string]bool{"testdata": true},
		isTestFilename: func(base string) bool {
			return strings.HasSuffix(base, "_test.go")
		},
	},
	{
		// Node.js and TypeScript, including JSX/TSX variants.
		extensions: []string{".js", ".mjs", ".cjs", ".jsx", ".ts", ".tsx"},
		patterns: []*regexp.Regexp{
			regexp.MustCompile(`process\.env\.([A-Z_]+)`),
			regexp.MustCompile(`process\.env\[["']([A-Z_]+)["']\]`),
		},
		noenvPrefix: "// lucos_repos: noenv",
		testDirs: map[string]bool{
			"__tests__": true,
			"test":      true,
			"tests":     true,
			"e2e":       true,
			"cypress":   true,
		},
		isTestFilename: func(base string) bool {
			return strings.Contains(base, ".test.") || strings.Contains(base, ".spec.")
		},
	},
}

// envPassthroughExtMap maps each file extension to its detector for O(1) lookup.
// Populated at init time from langDetectors.
var envPassthroughExtMap map[string]*envVarPassthroughDetector

// runtimeSuppliedEnvVars is the set of variable names provided by the OS
// userland, base image shell setup, or container runtime — not by the
// application's compose declaration. These are excluded from drift detection
// because they are never legitimately declared in a lucos service's compose file.
var runtimeSuppliedEnvVars = map[string]bool{
	"HOME":    true, // POSIX / base image user setup
	"PATH":    true, // POSIX / base image / Docker default
	"USER":    true, // POSIX / base image user setup
	"LOGNAME": true, // POSIX
	"SHELL":   true, // POSIX
	"TERM":    true, // POSIX (terminal type)
	"LANG":    true, // POSIX locale
	"LANGUAGE": true, // GNU locale extension
	"PWD":     true, // Shell-set
	"OLDPWD":  true, // Shell-set
	"SHLVL":   true, // Shell-set
	"_":       true, // Shell-set (last argv[0])
	"HOSTNAME": true, // Docker container runtime
}

// isRuntimeSuppliedEnvVar reports whether the given variable name is a
// runtime-supplied OS/shell/container variable that should not be checked
// for compose passthrough. This covers the exact-match list and the LC_*
// locale prefix family.
func isRuntimeSuppliedEnvVar(name string) bool {
	return runtimeSuppliedEnvVars[name] || strings.HasPrefix(name, "LC_")
}

// isEnvTestFile reports whether the given source file path should be skipped
// because it is test code. A file is considered test code if either:
//   - any path segment (split on "/", excluding the filename itself) equals a
//     test directory name for this language; or
//   - the file's basename matches the language's test filename patterns.
//
// Path-component matching splits on "/" to avoid false exclusions: a directory
// named "contests" is not the same as "test".
func isEnvTestFile(path string, detector *envVarPassthroughDetector) bool {
	if detector == nil {
		return false
	}
	segments := strings.Split(path, "/")
	// Check all directory components (all segments except the filename).
	for _, seg := range segments[:len(segments)-1] {
		if detector.testDirs[seg] {
			return true
		}
	}
	// Check the filename itself.
	base := segments[len(segments)-1]
	return detector.isTestFilename(base)
}

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
// using the appropriate language detector. Returns nil if the file is:
//   - an unsupported extension, or
//   - identified as test code (via test-path exclusion rules).
//
// It honours noenv opt-out annotations on individual lines.
func scanFileForEnvVars(path string, content []byte) []envVarReading {
	ext := filepath.Ext(path)
	detector := envPassthroughExtMap[ext]
	if detector == nil {
		return nil
	}
	if isEnvTestFile(path, detector) {
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
			"intentional config — no change needed. Test files and runtime-supplied OS/shell variables " +
			"(`HOME`, `PATH`, `HOSTNAME`, `LC_*`, etc.) are excluded from scanning automatically. " +
			"For any other variable that is genuinely not a compose concern, add a `# lucos_repos: noenv MY_VAR` " +
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
					if isRuntimeSuppliedEnvVar(reading.VarName) {
						continue // OS/shell/runtime-supplied — not a compose concern.
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
