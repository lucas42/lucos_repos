package conventions

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ---- Unit tests for passthroughOnlyEnvVars ----

func TestPassthroughOnlyEnvVars_ListForm_BareOnly(t *testing.T) {
	yamlContent := `
services:
  app:
    environment:
      - PORT
      - DATABASE_URL=postgres://localhost/db
      - LOGANNE_ENDPOINT
`
	var compose envVarsComposeFile
	if err := yaml.Unmarshal([]byte(yamlContent), &compose); err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}
	vars := passthroughOnlyEnvVars(compose)
	if !vars["PORT"] {
		t.Error("expected PORT to be passthrough (bare name)")
	}
	if !vars["LOGANNE_ENDPOINT"] {
		t.Error("expected LOGANNE_ENDPOINT to be passthrough (bare name)")
	}
	if vars["DATABASE_URL"] {
		t.Error("DATABASE_URL=value must NOT be passthrough (has hardcoded value)")
	}
}

func TestPassthroughOnlyEnvVars_MapForm_NullOnly(t *testing.T) {
	yamlContent := `
services:
  app:
    environment:
      PORT:
      DATABASE_URL: "postgres://localhost/db"
      LOGANNE_ENDPOINT:
`
	var compose envVarsComposeFile
	if err := yaml.Unmarshal([]byte(yamlContent), &compose); err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}
	vars := passthroughOnlyEnvVars(compose)
	if !vars["PORT"] {
		t.Error("expected PORT to be passthrough (null value in map form)")
	}
	if !vars["LOGANNE_ENDPOINT"] {
		t.Error("expected LOGANNE_ENDPOINT to be passthrough (null value in map form)")
	}
	if vars["DATABASE_URL"] {
		t.Error("DATABASE_URL with hardcoded value must NOT be passthrough")
	}
}

func TestPassthroughOnlyEnvVars_MultipleServices(t *testing.T) {
	yamlContent := `
services:
  api:
    environment:
      - PORT
  worker:
    environment:
      - REDIS_URL
`
	var compose envVarsComposeFile
	if err := yaml.Unmarshal([]byte(yamlContent), &compose); err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}
	vars := passthroughOnlyEnvVars(compose)
	if !vars["PORT"] {
		t.Error("expected PORT from api service")
	}
	if !vars["REDIS_URL"] {
		t.Error("expected REDIS_URL from worker service")
	}
}

// ---- Unit tests for scanFileForEnvVars ----

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

// ---- Full convention check integration tests ----

func TestEnvVarPassthrough_Registered(t *testing.T) {
	c := findConvention(t, "env_var_passthrough")
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

	result := findConvention(t, "env_var_passthrough").Check(RepoContext{
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

	result := findConvention(t, "env_var_passthrough").Check(RepoContext{
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

	result := findConvention(t, "env_var_passthrough").Check(RepoContext{
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

	result := findConvention(t, "env_var_passthrough").Check(RepoContext{
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

	result := findConvention(t, "env_var_passthrough").Check(RepoContext{
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

	result := findConvention(t, "env_var_passthrough").Check(RepoContext{
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

	result := findConvention(t, "env_var_passthrough").Check(RepoContext{
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

func TestEnvVarPassthrough_HardcodedValueDoesNotCount(t *testing.T) {
	// KEY=value in compose is hardcoded config, not passthrough — must still fail.
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

	result := findConvention(t, "env_var_passthrough").Check(RepoContext{
		Name:          "lucas42/lucos_test",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	})
	if result.Pass {
		t.Error("expected fail: LOGANNE_ENDPOINT=hardcoded must not count as passthrough")
	}
	if !strings.Contains(result.Detail, "LOGANNE_ENDPOINT") {
		t.Errorf("expected detail to mention LOGANNE_ENDPOINT, got: %s", result.Detail)
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

	result := findConvention(t, "env_var_passthrough").Check(RepoContext{
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

	result := findConvention(t, "env_var_passthrough").Check(RepoContext{
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

	result := findConvention(t, "env_var_passthrough").Check(RepoContext{
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
