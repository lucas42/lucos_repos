package conventions

import (
	"strings"
	"testing"
)

// Note: compose env-var parsing is tested via declaredEnvVars in
// standard_env_vars_test.go (TestDeclaredEnvVars_*). The env_var_passthrough
// convention uses that same function. The integration tests below cover the
// full check flow including both bare-name and hardcoded-value compose forms.

// ---- Unit tests for scanFileForEnvVars (per-language detectors) ----

func TestScanFileForEnvVars_Python(t *testing.T) {
	content := `
import os
endpoint = os.environ.get("LOGANNE_ENDPOINT", "")
secret = os.environ["SECRET_KEY"]
port = os.getenv("PORT")
`
	readings := scanFileForEnvVars("app.py", []byte(content))
	found := make(map[string]bool)
	for _, r := range readings {
		found[r.VarName] = true
	}
	for _, want := range []string{"LOGANNE_ENDPOINT", "SECRET_KEY", "PORT"} {
		if !found[want] {
			t.Errorf("expected Python detector to find %s", want)
		}
	}
}

func TestScanFileForEnvVars_Ruby(t *testing.T) {
	content := `
endpoint = ENV["LOGANNE_ENDPOINT"]
secret = ENV.fetch("SECRET_KEY", "default")
`
	readings := scanFileForEnvVars("app.rb", []byte(content))
	found := make(map[string]bool)
	for _, r := range readings {
		found[r.VarName] = true
	}
	for _, want := range []string{"LOGANNE_ENDPOINT", "SECRET_KEY"} {
		if !found[want] {
			t.Errorf("expected Ruby detector to find %s", want)
		}
	}
}

func TestScanFileForEnvVars_Erlang(t *testing.T) {
	content := `
-module(fetcher).
endpoint() -> os:getenv("SCHEDULE_TRACKER_ENDPOINT", ""),
port() -> os:getenv("PORT").
`
	readings := scanFileForEnvVars("fetcher.erl", []byte(content))
	found := make(map[string]bool)
	for _, r := range readings {
		found[r.VarName] = true
	}
	for _, want := range []string{"SCHEDULE_TRACKER_ENDPOINT", "PORT"} {
		if !found[want] {
			t.Errorf("expected Erlang detector to find %s", want)
		}
	}
}

func TestScanFileForEnvVars_Go(t *testing.T) {
	content := `
package main

import "os"

func main() {
    endpoint := os.Getenv("LOGANNE_ENDPOINT")
    val, ok := os.LookupEnv("SECRET_KEY")
    _ = endpoint
    _ = val
    _ = ok
}
`
	readings := scanFileForEnvVars("main.go", []byte(content))
	found := make(map[string]bool)
	for _, r := range readings {
		found[r.VarName] = true
	}
	for _, want := range []string{"LOGANNE_ENDPOINT", "SECRET_KEY"} {
		if !found[want] {
			t.Errorf("expected Go detector to find %s", want)
		}
	}
}

func TestScanFileForEnvVars_NodeJS(t *testing.T) {
	content := `
const endpoint = process.env.LOGANNE_ENDPOINT;
const secret = process.env["SECRET_KEY"];
const port = process.env.PORT || "8080";
`
	readings := scanFileForEnvVars("index.js", []byte(content))
	found := make(map[string]bool)
	for _, r := range readings {
		found[r.VarName] = true
	}
	for _, want := range []string{"LOGANNE_ENDPOINT", "SECRET_KEY", "PORT"} {
		if !found[want] {
			t.Errorf("expected Node.js detector to find %s", want)
		}
	}
}

func TestScanFileForEnvVars_TypeScript(t *testing.T) {
	content := `
const apiKey: string = process.env.API_KEY ?? "";
`
	readings := scanFileForEnvVars("api.ts", []byte(content))
	found := make(map[string]bool)
	for _, r := range readings {
		found[r.VarName] = true
	}
	if !found["API_KEY"] {
		t.Error("expected TypeScript (.ts) detector to find API_KEY")
	}
}

func TestScanFileForEnvVars_UnknownExtension(t *testing.T) {
	content := `process.env.SOME_VAR`
	readings := scanFileForEnvVars("config.toml", []byte(content))
	if len(readings) != 0 {
		t.Errorf("expected no readings for unknown extension, got %d", len(readings))
	}
}

// ---- Unit tests for isEnvNoenvSuppressed ----

func TestIsEnvNoenvSuppressed_PythonStyle(t *testing.T) {
	line := `port = os.getenv("PORT")  # lucos_repos: noenv PORT`
	if !isEnvNoenvSuppressed(line, "PORT", "# lucos_repos: noenv") {
		t.Error("expected PORT to be suppressed by noenv annotation")
	}
	if isEnvNoenvSuppressed(line, "OTHER_VAR", "# lucos_repos: noenv") {
		t.Error("expected OTHER_VAR not to be suppressed by PORT noenv annotation")
	}
}

func TestIsEnvNoenvSuppressed_JSStyle(t *testing.T) {
	line := `const x = process.env.HOSTNAME; // lucos_repos: noenv HOSTNAME`
	if !isEnvNoenvSuppressed(line, "HOSTNAME", "// lucos_repos: noenv") {
		t.Error("expected HOSTNAME to be suppressed by noenv annotation")
	}
}

func TestIsEnvNoenvSuppressed_ErlangStyle(t *testing.T) {
	line := `hostname() -> os:getenv("HOSTNAME", "unknown"). % lucos_repos: noenv HOSTNAME`
	if !isEnvNoenvSuppressed(line, "HOSTNAME", "% lucos_repos: noenv") {
		t.Error("expected HOSTNAME to be suppressed by Erlang noenv annotation")
	}
}

func TestIsEnvNoenvSuppressed_NoAnnotation(t *testing.T) {
	line := `port = os.getenv("PORT")`
	if isEnvNoenvSuppressed(line, "PORT", "# lucos_repos: noenv") {
		t.Error("expected PORT not to be suppressed when no annotation present")
	}
}

// ---- Unit tests for noenv opt-out suppressing detection ----

func TestScanFileForEnvVars_NoenvSuppressesPython(t *testing.T) {
	content := `
import os
port = os.getenv("PORT")  # lucos_repos: noenv PORT
endpoint = os.getenv("LOGANNE_ENDPOINT")
`
	readings := scanFileForEnvVars("app.py", []byte(content))
	found := make(map[string]bool)
	for _, r := range readings {
		found[r.VarName] = true
	}
	if found["PORT"] {
		t.Error("PORT should be suppressed by noenv annotation")
	}
	if !found["LOGANNE_ENDPOINT"] {
		t.Error("LOGANNE_ENDPOINT should still be detected (no annotation)")
	}
}

func TestScanFileForEnvVars_NoenvSuppressesNode(t *testing.T) {
	content := `
const h = process.env.HOSTNAME; // lucos_repos: noenv HOSTNAME
const e = process.env.LOGANNE_ENDPOINT;
`
	readings := scanFileForEnvVars("main.js", []byte(content))
	found := make(map[string]bool)
	for _, r := range readings {
		found[r.VarName] = true
	}
	if found["HOSTNAME"] {
		t.Error("HOSTNAME should be suppressed by noenv annotation")
	}
	if !found["LOGANNE_ENDPOINT"] {
		t.Error("LOGANNE_ENDPOINT should still be detected")
	}
}

// ---- Unit tests for isEnvTestFile (test-path exclusion) ----

func TestIsEnvTestFile_PythonTestDir(t *testing.T) {
	detector := envPassthroughExtMap[".py"]
	cases := []struct {
		path   string
		expect bool
	}{
		{"src/tests/helpers.py", true},        // "tests" directory component
		{"test/conftest.py", true},             // "test" directory component
		{"test_config.py", true},               // starts with test_ — pytest discovers this as a test file
		{"src/contests/foo.py", false},         // "contests" is NOT "tests" (path-component check)
		{"src/main.py", false},                 // ordinary source file
		{"test_main.py", true},                 // filename starts with test_
		{"app_test.py", true},                  // filename ends with _test.py
		{"conftest.py", true},                  // conftest.py exact match
		{"src/app/conftest.py", true},          // conftest.py in subdirectory
	}
	for _, tc := range cases {
		got := isEnvTestFile(tc.path, detector)
		if got != tc.expect {
			t.Errorf("isEnvTestFile(%q): got %v, want %v", tc.path, got, tc.expect)
		}
	}
}

func TestIsEnvTestFile_RubyTestDir(t *testing.T) {
	detector := envPassthroughExtMap[".rb"]
	cases := []struct {
		path   string
		expect bool
	}{
		{"spec/models/user_spec.rb", true},    // "spec" directory
		{"test/unit/user_test.rb", true},       // "test" directory
		{"src/main.rb", false},                 // ordinary source
		{"user_spec.rb", true},                 // filename _spec.rb
		{"user_test.rb", true},                 // filename _test.rb
		{"test_helper.rb", true},               // filename test_*.rb
		{"src/helpers/test_helper.rb", true},   // test_ filename in subdir
	}
	for _, tc := range cases {
		got := isEnvTestFile(tc.path, detector)
		if got != tc.expect {
			t.Errorf("isEnvTestFile(%q): got %v, want %v", tc.path, got, tc.expect)
		}
	}
}

func TestIsEnvTestFile_ErlangTestDir(t *testing.T) {
	detector := envPassthroughExtMap[".erl"]
	cases := []struct {
		path   string
		expect bool
	}{
		{"test/fetcher_SUITE.erl", true},      // "test" directory
		{"src/fetcher.erl", false},             // ordinary source
		{"fetcher_SUITE.erl", true},            // _SUITE.erl filename
		{"fetcher_tests.erl", true},            // _tests.erl filename
		{"src/fetcher_tests.erl", true},        // _tests.erl in subdir
	}
	for _, tc := range cases {
		got := isEnvTestFile(tc.path, detector)
		if got != tc.expect {
			t.Errorf("isEnvTestFile(%q): got %v, want %v", tc.path, got, tc.expect)
		}
	}
}

func TestIsEnvTestFile_GoTestFile(t *testing.T) {
	detector := envPassthroughExtMap[".go"]
	cases := []struct {
		path   string
		expect bool
	}{
		{"conventions/env_var_passthrough_test.go", true}, // _test.go filename
		{"src/main_test.go", true},                        // _test.go in subdir
		{"testdata/fixtures.go", true},                    // "testdata" directory
		{"src/main.go", false},                            // ordinary source
		{"src/testdata/helpers.go", true},                 // testdata in the middle
	}
	for _, tc := range cases {
		got := isEnvTestFile(tc.path, detector)
		if got != tc.expect {
			t.Errorf("isEnvTestFile(%q): got %v, want %v", tc.path, got, tc.expect)
		}
	}
}

func TestIsEnvTestFile_NodeJS(t *testing.T) {
	detector := envPassthroughExtMap[".js"]
	cases := []struct {
		path   string
		expect bool
	}{
		{"__tests__/routes.js", true},         // __tests__ directory
		{"test/server.js", true},              // test directory
		{"e2e/flows.js", true},                // e2e directory
		{"cypress/integration.js", true},      // cypress directory
		{"src/app.test.js", true},             // .test. filename
		{"src/app.spec.js", true},             // .spec. filename
		{"src/server.js", false},              // ordinary source
		{"src/contests/score.js", false},      // "contests" ≠ "tests"
	}
	for _, tc := range cases {
		got := isEnvTestFile(tc.path, detector)
		if got != tc.expect {
			t.Errorf("isEnvTestFile(%q): got %v, want %v", tc.path, got, tc.expect)
		}
	}
}

func TestScanFileForEnvVars_SkipsTestFiles(t *testing.T) {
	// Reads in test directories/files must return no readings.
	testCases := []struct {
		path    string
		content string
	}{
		{"__tests__/routes.js", `const k = process.env.KEY_EXAMPLE;`},
		{"test/server_test.go", `_ = os.Getenv("SECRET_KEY")`},
		{"tests/conftest.py", `import os; val = os.getenv("PRIVATE_KEY")`},
		{"spec/user_spec.rb", `token = ENV["AUTH_TOKEN"]`},
		{"test/fetcher_SUITE.erl", `os:getenv("SECRET_TOKEN")`},
	}
	for _, tc := range testCases {
		readings := scanFileForEnvVars(tc.path, []byte(tc.content))
		if len(readings) != 0 {
			t.Errorf("scanFileForEnvVars(%q): expected no readings for test file, got %d", tc.path, len(readings))
		}
	}
}

// Path-component split-on-"/" correctness: "contests" must NOT exclude.
func TestIsEnvTestFile_PathComponentSplitCorrectness(t *testing.T) {
	detector := envPassthroughExtMap[".py"]
	// "contests" contains "test" as a substring but is NOT the segment "test" or "tests".
	if isEnvTestFile("src/contests/foo.py", detector) {
		t.Error("src/contests/foo.py must NOT be excluded — 'contests' != 'tests'")
	}
	// "src/tests/foo.py" MUST be excluded.
	if !isEnvTestFile("src/tests/foo.py", detector) {
		t.Error("src/tests/foo.py must be excluded — 'tests' is a test directory")
	}
}

// ---- Unit tests for isRuntimeSuppliedEnvVar ----

func TestIsRuntimeSuppliedEnvVar_ExactMatches(t *testing.T) {
	runtimeVars := []string{
		"HOME", "PATH", "USER", "LOGNAME", "SHELL", "TERM",
		"LANG", "LANGUAGE", "PWD", "OLDPWD", "SHLVL", "_", "HOSTNAME",
	}
	for _, v := range runtimeVars {
		if !isRuntimeSuppliedEnvVar(v) {
			t.Errorf("expected %s to be runtime-supplied", v)
		}
	}
}

func TestIsRuntimeSuppliedEnvVar_LCPrefix(t *testing.T) {
	lcVars := []string{"LC_ALL", "LC_CTYPE", "LC_MESSAGES", "LC_TIME", "LC_NUMERIC"}
	for _, v := range lcVars {
		if !isRuntimeSuppliedEnvVar(v) {
			t.Errorf("expected %s (LC_* prefix) to be runtime-supplied", v)
		}
	}
}

func TestIsRuntimeSuppliedEnvVar_AppVarsNotExcluded(t *testing.T) {
	appVars := []string{
		"LOGANNE_ENDPOINT", "DEBUG", "NODE_ENV", "SYSTEM",
		"PORT", "APP_ORIGIN", "DATABASE_URL", "TZ",
	}
	for _, v := range appVars {
		if isRuntimeSuppliedEnvVar(v) {
			t.Errorf("expected %s NOT to be runtime-supplied (should remain in scope)", v)
		}
	}
}

// ---- Full convention check integration tests ----

func TestEnvVarPassthrough_Registered(t *testing.T) {
	c := findConvention(t, "env-var-passthrough")
	if c.Description == "" {
		t.Error("env_var_passthrough has empty description")
	}
	if c.Rationale == "" {
		t.Error("env_var_passthrough has empty rationale")
	}
	if c.Guidance == "" {
		t.Error("env_var_passthrough has empty guidance")
	}
	if c.Check == nil {
		t.Error("env_var_passthrough has nil Check function")
	}
	if !c.AppliesToType(RepoTypeSystem) {
		t.Error("env_var_passthrough should apply to RepoTypeSystem")
	}
	if c.AppliesToType(RepoTypeComponent) {
		t.Error("env_var_passthrough should not apply to RepoTypeComponent")
	}
}

func TestEnvVarPassthrough_NoComposeFile(t *testing.T) {
	server := treeBlobServer(t, "", nil)
	defer server.Close()

	result := findConvention(t, "env-var-passthrough").Check(RepoContext{
		Name:          "lucas42/lucos_test",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	})
	if !result.Pass {
		t.Errorf("expected pass when no docker-compose.yml, got: %s", result.Detail)
	}
}

func TestEnvVarPassthrough_AllVarsPassthrough(t *testing.T) {
	compose := `
services:
  app:
    build: .
    environment:
      - PORT
      - LOGANNE_ENDPOINT
`
	server := treeBlobServer(t, compose, map[string]string{
		"src/app.py": `import os; url = os.environ.get("LOGANNE_ENDPOINT")`,
	})
	defer server.Close()

	result := findConvention(t, "env-var-passthrough").Check(RepoContext{
		Name:          "lucas42/lucos_test",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	})
	if !result.Pass {
		t.Errorf("expected pass when all vars are in passthrough, got: %s", result.Detail)
	}
}

// Per-language integration tests: var read in code, missing from compose passthrough → finding.

func TestEnvVarPassthrough_Python_MissingPassthrough(t *testing.T) {
	compose := `
services:
  app:
    build: .
    environment:
      - PORT
`
	server := treeBlobServer(t, compose, map[string]string{
		"src/app.py": `import os; url = os.environ.get("LOGANNE_ENDPOINT", "")`,
	})
	defer server.Close()

	result := findConvention(t, "env-var-passthrough").Check(RepoContext{
		Name:          "lucas42/lucos_test",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	})
	if result.Pass {
		t.Error("expected fail: LOGANNE_ENDPOINT read in Python code but not in compose passthrough")
	}
	if !strings.Contains(result.Detail, "LOGANNE_ENDPOINT") {
		t.Errorf("expected detail to mention LOGANNE_ENDPOINT, got: %s", result.Detail)
	}
}

func TestEnvVarPassthrough_Ruby_MissingPassthrough(t *testing.T) {
	compose := `
services:
  app:
    build: .
    environment:
      - PORT
`
	server := treeBlobServer(t, compose, map[string]string{
		"src/app.rb": `endpoint = ENV["LOGANNE_ENDPOINT"]`,
	})
	defer server.Close()

	result := findConvention(t, "env-var-passthrough").Check(RepoContext{
		Name:          "lucas42/lucos_test",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	})
	if result.Pass {
		t.Error("expected fail: LOGANNE_ENDPOINT read in Ruby code but not in compose passthrough")
	}
	if !strings.Contains(result.Detail, "LOGANNE_ENDPOINT") {
		t.Errorf("expected detail to mention LOGANNE_ENDPOINT, got: %s", result.Detail)
	}
}

func TestEnvVarPassthrough_Erlang_MissingPassthrough(t *testing.T) {
	compose := `
services:
  app:
    build: .
    environment:
      - PORT
`
	server := treeBlobServer(t, compose, map[string]string{
		"src/fetcher.erl": `endpoint() -> os:getenv("SCHEDULE_TRACKER_ENDPOINT", "").`,
	})
	defer server.Close()

	result := findConvention(t, "env-var-passthrough").Check(RepoContext{
		Name:          "lucas42/lucos_test",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	})
	if result.Pass {
		t.Error("expected fail: SCHEDULE_TRACKER_ENDPOINT read in Erlang code but not in compose passthrough")
	}
	if !strings.Contains(result.Detail, "SCHEDULE_TRACKER_ENDPOINT") {
		t.Errorf("expected detail to mention SCHEDULE_TRACKER_ENDPOINT, got: %s", result.Detail)
	}
}

func TestEnvVarPassthrough_Go_MissingPassthrough(t *testing.T) {
	compose := `
services:
  app:
    build: .
    environment:
      - PORT
`
	server := treeBlobServer(t, compose, map[string]string{
		"src/main.go": `package main; import "os"; func main() { _ = os.Getenv("LOGANNE_ENDPOINT") }`,
	})
	defer server.Close()

	result := findConvention(t, "env-var-passthrough").Check(RepoContext{
		Name:          "lucas42/lucos_test",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	})
	if result.Pass {
		t.Error("expected fail: LOGANNE_ENDPOINT read in Go code but not in compose passthrough")
	}
	if !strings.Contains(result.Detail, "LOGANNE_ENDPOINT") {
		t.Errorf("expected detail to mention LOGANNE_ENDPOINT, got: %s", result.Detail)
	}
}

func TestEnvVarPassthrough_Node_MissingPassthrough(t *testing.T) {
	compose := `
services:
  app:
    build: .
    environment:
      - PORT
`
	server := treeBlobServer(t, compose, map[string]string{
		"src/index.js": `const endpoint = process.env.LOGANNE_ENDPOINT;`,
	})
	defer server.Close()

	result := findConvention(t, "env-var-passthrough").Check(RepoContext{
		Name:          "lucas42/lucos_test",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	})
	if result.Pass {
		t.Error("expected fail: LOGANNE_ENDPOINT read in Node.js code but not in compose passthrough")
	}
	if !strings.Contains(result.Detail, "LOGANNE_ENDPOINT") {
		t.Errorf("expected detail to mention LOGANNE_ENDPOINT, got: %s", result.Detail)
	}
}

func TestEnvVarPassthrough_HardcodedValueSatisfiesRequirement(t *testing.T) {
	// KEY=value in compose is intentional hardcoded config — the container receives
	// the variable regardless, so the convention must not flag it.
	compose := `
services:
  app:
    build: .
    environment:
      - PORT
      - LOGANNE_ENDPOINT=http://hardcoded-loganne:8080
`
	server := treeBlobServer(t, compose, map[string]string{
		"src/app.py": `import os; url = os.environ.get("LOGANNE_ENDPOINT", "")`,
	})
	defer server.Close()

	result := findConvention(t, "env-var-passthrough").Check(RepoContext{
		Name:          "lucas42/lucos_test",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	})
	if !result.Pass {
		t.Errorf("expected pass: LOGANNE_ENDPOINT=hardcoded satisfies the requirement (container receives the var), got: %s", result.Detail)
	}
}

func TestEnvVarPassthrough_BothFormsPresent_NoFinding(t *testing.T) {
	// When a var appears both as bare passthrough AND hardcoded KEY=value,
	// the convention must still pass — belt-and-suspenders compose setups are fine.
	compose := `
services:
  app:
    build: .
    environment:
      - PORT
      - LOGANNE_ENDPOINT
      - STATE_DIR=/var/lib/app
`
	server := treeBlobServer(t, compose, map[string]string{
		"src/app.py": `
import os
url = os.environ.get("LOGANNE_ENDPOINT", "")
d = os.environ.get("STATE_DIR", "")
`,
	})
	defer server.Close()

	result := findConvention(t, "env-var-passthrough").Check(RepoContext{
		Name:          "lucas42/lucos_test",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	})
	if !result.Pass {
		t.Errorf("expected pass when vars are declared in either or both compose forms, got: %s", result.Detail)
	}
}

func TestEnvVarPassthrough_NoenvAnnotationSuppresses(t *testing.T) {
	compose := `
services:
  app:
    build: .
    environment:
      - PORT
`
	// HOSTNAME is read but annotated with noenv — should not produce a finding.
	server := treeBlobServer(t, compose, map[string]string{
		"src/app.py": `
import os
h = os.getenv("HOSTNAME")  # lucos_repos: noenv HOSTNAME
p = os.getenv("PORT")
`,
	})
	defer server.Close()

	result := findConvention(t, "env-var-passthrough").Check(RepoContext{
		Name:          "lucas42/lucos_test",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	})
	if !result.Pass {
		t.Errorf("expected pass when HOSTNAME is suppressed by noenv, got: %s", result.Detail)
	}
}

func TestEnvVarPassthrough_NonSourceFilesIgnored(t *testing.T) {
	compose := `
services:
  app:
    build: .
    environment:
      - PORT
`
	// LOGANNE_ENDPOINT only in README (non-source) — must not produce a finding.
	server := treeBlobServer(t, compose, map[string]string{
		"README.md":  `Set LOGANNE_ENDPOINT to configure log shipping`,
		"src/app.py": `import os; port = os.getenv("PORT")`,
	})
	defer server.Close()

	result := findConvention(t, "env-var-passthrough").Check(RepoContext{
		Name:          "lucas42/lucos_test",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	})
	if !result.Pass {
		t.Errorf("expected pass when env var only in non-source files, got: %s", result.Detail)
	}
}

func TestEnvVarPassthrough_MultipleVarsMissingReportsAll(t *testing.T) {
	compose := `
services:
  app:
    build: .
    environment:
      - PORT
`
	server := treeBlobServer(t, compose, map[string]string{
		"src/app.py": `
import os
a = os.getenv("ALPHA_VAR")
b = os.environ.get("BETA_VAR", "")
`,
	})
	defer server.Close()

	result := findConvention(t, "env-var-passthrough").Check(RepoContext{
		Name:          "lucas42/lucos_test",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	})
	if result.Pass {
		t.Error("expected fail when multiple vars missing from compose passthrough")
	}
	if !strings.Contains(result.Detail, "ALPHA_VAR") {
		t.Errorf("expected detail to mention ALPHA_VAR, got: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "BETA_VAR") {
		t.Errorf("expected detail to mention BETA_VAR, got: %s", result.Detail)
	}
}

// ---- Integration tests for test-path exclusion ----

// Test that reads in test directories don't trigger findings.
func TestEnvVarPassthrough_TestDirExcluded_Python(t *testing.T) {
	compose := `
services:
  app:
    build: .
    environment:
      - PORT
`
	server := treeBlobServer(t, compose, map[string]string{
		"tests/test_routes.py": `import os; key = os.getenv("KEY_EXAMPLE")`,
	})
	defer server.Close()

	result := findConvention(t, "env-var-passthrough").Check(RepoContext{
		Name:          "lucas42/lucos_test",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	})
	if !result.Pass {
		t.Errorf("expected pass: KEY_EXAMPLE in tests/ dir should be excluded, got: %s", result.Detail)
	}
}

func TestEnvVarPassthrough_TestDirExcluded_Node(t *testing.T) {
	compose := `
services:
  app:
    build: .
    environment:
      - PORT
`
	server := treeBlobServer(t, compose, map[string]string{
		"__tests__/routes.js": `const k = process.env.KEY_EXAMPLE;`,
	})
	defer server.Close()

	result := findConvention(t, "env-var-passthrough").Check(RepoContext{
		Name:          "lucas42/lucos_test",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	})
	if !result.Pass {
		t.Errorf("expected pass: KEY_EXAMPLE in __tests__/ should be excluded, got: %s", result.Detail)
	}
}

func TestEnvVarPassthrough_TestDirExcluded_Erlang(t *testing.T) {
	compose := `
services:
  app:
    build: .
    environment:
      - PORT
`
	server := treeBlobServer(t, compose, map[string]string{
		"test/server_SUITE.erl": `os:getenv("TEST_ONLY_TOKEN")`,
	})
	defer server.Close()

	result := findConvention(t, "env-var-passthrough").Check(RepoContext{
		Name:          "lucas42/lucos_test",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	})
	if !result.Pass {
		t.Errorf("expected pass: read in Erlang _SUITE.erl should be excluded, got: %s", result.Detail)
	}
}

func TestEnvVarPassthrough_TestDirExcluded_Go(t *testing.T) {
	compose := `
services:
  app:
    build: .
    environment:
      - PORT
`
	server := treeBlobServer(t, compose, map[string]string{
		"src/server_test.go": `_ = os.Getenv("SECRET_KEY")`,
	})
	defer server.Close()

	result := findConvention(t, "env-var-passthrough").Check(RepoContext{
		Name:          "lucas42/lucos_test",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	})
	if !result.Pass {
		t.Errorf("expected pass: read in Go _test.go should be excluded, got: %s", result.Detail)
	}
}

func TestEnvVarPassthrough_TestDirExcluded_Ruby(t *testing.T) {
	compose := `
services:
  app:
    build: .
    environment:
      - PORT
`
	server := treeBlobServer(t, compose, map[string]string{
		"spec/server_spec.rb": `token = ENV["AUTH_TOKEN"]`,
	})
	defer server.Close()

	result := findConvention(t, "env-var-passthrough").Check(RepoContext{
		Name:          "lucas42/lucos_test",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	})
	if !result.Pass {
		t.Errorf("expected pass: read in Ruby spec/ should be excluded, got: %s", result.Detail)
	}
}

// "contests" directory must NOT be excluded (substring ≠ path component).
func TestEnvVarPassthrough_ContestsDirNotExcluded(t *testing.T) {
	compose := `
services:
  app:
    build: .
    environment:
      - PORT
`
	server := treeBlobServer(t, compose, map[string]string{
		"src/contests/score.py": `import os; v = os.getenv("CONTEST_API_KEY")`,
	})
	defer server.Close()

	result := findConvention(t, "env-var-passthrough").Check(RepoContext{
		Name:          "lucas42/lucos_test",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	})
	if result.Pass {
		t.Error("expected fail: src/contests/ is not a test dir; CONTEST_API_KEY should still be flagged")
	}
	if !strings.Contains(result.Detail, "CONTEST_API_KEY") {
		t.Errorf("expected detail to mention CONTEST_API_KEY, got: %s", result.Detail)
	}
}

// ---- Integration tests for runtime-supplied env var exclusion ----

// HOME read in non-test production code must NOT produce a finding.
func TestEnvVarPassthrough_RuntimeVar_HOME_Excluded(t *testing.T) {
	compose := `
services:
  app:
    build: .
    environment:
      - PORT
`
	server := treeBlobServer(t, compose, map[string]string{
		"src/player.js": `const home = process.env.HOME; const url = process.env.MEDIA_MANAGER_URL;`,
	})
	defer server.Close()

	result := findConvention(t, "env-var-passthrough").Check(RepoContext{
		Name:          "lucas42/lucos_test",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	})
	// HOME should be excluded; MEDIA_MANAGER_URL should still be flagged.
	if result.Pass {
		t.Error("expected fail for MEDIA_MANAGER_URL (HOME should be excluded but MEDIA_MANAGER_URL is not)")
	}
	if strings.Contains(result.Detail, "HOME") {
		t.Errorf("HOME should be excluded from findings, got: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "MEDIA_MANAGER_URL") {
		t.Errorf("MEDIA_MANAGER_URL should still be flagged, got: %s", result.Detail)
	}
}

// All runtime-supplied vars read in production code → no findings.
func TestEnvVarPassthrough_AllRuntimeVarsExcluded(t *testing.T) {
	compose := `
services:
  app:
    build: .
    environment:
      - PORT
`
	server := treeBlobServer(t, compose, map[string]string{
		"src/app.py": `
import os
h = os.getenv("HOME")
p = os.getenv("PATH")
u = os.getenv("USER")
lo = os.getenv("LOGNAME")
sh = os.getenv("SHELL")
t = os.getenv("TERM")
la = os.getenv("LANG")
lan = os.getenv("LANGUAGE")
pw = os.getenv("PWD")
op = os.getenv("OLDPWD")
sl = os.getenv("SHLVL")
un = os.getenv("HOSTNAME")
lca = os.getenv("LC_ALL")
lcc = os.getenv("LC_CTYPE")
`,
	})
	defer server.Close()

	result := findConvention(t, "env-var-passthrough").Check(RepoContext{
		Name:          "lucas42/lucos_test",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	})
	if !result.Pass {
		t.Errorf("expected pass: all runtime-supplied vars should be excluded, got: %s", result.Detail)
	}
}

// DEBUG must NOT be excluded (application-level flag, in scope).
func TestEnvVarPassthrough_DEBUG_InScope(t *testing.T) {
	compose := `
services:
  app:
    build: .
    environment:
      - PORT
`
	server := treeBlobServer(t, compose, map[string]string{
		"src/main.go": `_ = os.Getenv("DEBUG")`,
	})
	defer server.Close()

	result := findConvention(t, "env-var-passthrough").Check(RepoContext{
		Name:          "lucas42/lucos_test",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	})
	if result.Pass {
		t.Error("expected fail: DEBUG is an application-level var and must remain in scope")
	}
	if !strings.Contains(result.Detail, "DEBUG") {
		t.Errorf("expected detail to mention DEBUG, got: %s", result.Detail)
	}
}
