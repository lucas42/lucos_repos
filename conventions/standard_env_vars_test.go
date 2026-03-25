package conventions

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// treeBlobServer creates a test server that serves docker-compose.yml via
// the contents API and source files via the git trees + blobs APIs.
// sourceFiles maps file paths to their content. If nil, the tree API returns
// an empty tree.
func treeBlobServer(t *testing.T, compose string, sourceFiles map[string]string) *httptest.Server {
	t.Helper()

	// Build blob map: sha → content
	blobs := make(map[string]string)
	var treeEntries []gitTreeEntry
	for path, content := range sourceFiles {
		sha := "sha-" + strings.ReplaceAll(path, "/", "-")
		blobs[sha] = content
		treeEntries = append(treeEntries, gitTreeEntry{
			Path: path,
			Type: "blob",
			SHA:  sha,
			Size: len(content),
		})
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Serve docker-compose.yml via contents API
		if r.URL.Path == "/repos/lucas42/lucos_test/contents/docker-compose.yml" {
			if compose == "" {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"message":"Not Found"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(composeFixture(compose))
			return
		}

		// Serve git tree
		if r.URL.Path == "/repos/lucas42/lucos_test/git/trees/HEAD" {
			w.Header().Set("Content-Type", "application/json")
			resp := gitTreeResponse{Tree: treeEntries}
			json.NewEncoder(w).Encode(resp)
			return
		}

		// Serve git blobs
		if strings.HasPrefix(r.URL.Path, "/repos/lucas42/lucos_test/git/blobs/") {
			sha := strings.TrimPrefix(r.URL.Path, "/repos/lucas42/lucos_test/git/blobs/")
			content, ok := blobs[sha]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			encoded := base64.StdEncoding.EncodeToString([]byte(content))
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(gitBlobResponse{
				Content:  encoded,
				Encoding: "base64",
				Size:     len(content),
			})
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
}

func TestStandardEnvVars_Registered(t *testing.T) {
	c := findConvention(t, "standard-env-vars-in-compose")
	if c.Description == "" {
		t.Error("standard-env-vars-in-compose has empty description")
	}
	if c.Rationale == "" {
		t.Error("standard-env-vars-in-compose has empty rationale")
	}
	if c.Guidance == "" {
		t.Error("standard-env-vars-in-compose has empty guidance")
	}
	if c.Check == nil {
		t.Error("standard-env-vars-in-compose has nil Check function")
	}
	if !c.AppliesToType(RepoTypeSystem) {
		t.Error("standard-env-vars-in-compose should apply to RepoTypeSystem")
	}
	if c.AppliesToType(RepoTypeComponent) {
		t.Error("standard-env-vars-in-compose should not apply to RepoTypeComponent")
	}
}

func TestStandardEnvVars_NoComposeFile(t *testing.T) {
	server := treeBlobServer(t, "", nil)
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_test",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "standard-env-vars-in-compose").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when no docker-compose.yml, got fail: %s", result.Detail)
	}
}

func TestStandardEnvVars_VarDeclaredAndUsed(t *testing.T) {
	compose := `
services:
  app:
    build: .
    environment:
      - PORT
      - LOGANNE_ENDPOINT
`
	server := treeBlobServer(t, compose, map[string]string{
		"src/main.js": `const endpoint = process.env.LOGANNE_ENDPOINT;`,
	})
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_test",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "standard-env-vars-in-compose").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when LOGANNE_ENDPOINT is declared, got fail: %s", result.Detail)
	}
}

func TestStandardEnvVars_VarDeclaredMapForm(t *testing.T) {
	compose := `
services:
  app:
    build: .
    environment:
      PORT: "8080"
      LOGANNE_ENDPOINT: "http://loganne:8080"
`
	server := treeBlobServer(t, compose, nil)
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_test",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "standard-env-vars-in-compose").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when LOGANNE_ENDPOINT is declared (map form), got fail: %s", result.Detail)
	}
}

func TestStandardEnvVars_VarUsedButNotDeclared(t *testing.T) {
	compose := `
services:
  app:
    build: .
    environment:
      - PORT
`
	server := treeBlobServer(t, compose, map[string]string{
		"src/app.js": `const loganne = process.env.LOGANNE_ENDPOINT || "";`,
	})
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_test",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "standard-env-vars-in-compose").Check(repo)
	if result.Pass {
		t.Errorf("expected fail when LOGANNE_ENDPOINT used in code but not declared, got pass")
	}
	if !strings.Contains(result.Detail, "LOGANNE_ENDPOINT") {
		t.Errorf("expected detail to mention LOGANNE_ENDPOINT, got: %s", result.Detail)
	}
}

func TestStandardEnvVars_VarNotUsedNotDeclared(t *testing.T) {
	compose := `
services:
  app:
    build: .
    environment:
      - PORT
`
	server := treeBlobServer(t, compose, map[string]string{
		"src/main.js": `console.log("Hello world");`,
	})
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_test",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "standard-env-vars-in-compose").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when LOGANNE_ENDPOINT not used in code, got fail: %s", result.Detail)
	}
}

func TestStandardEnvVars_ComposeAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_test",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "standard-env-vars-in-compose").Check(repo)
	if result.Err == nil {
		t.Errorf("expected Err when GitHub API returns 500 for compose file")
	}
}

func TestStandardEnvVars_TreeAPIError(t *testing.T) {
	compose := `
services:
  app:
    build: .
    environment:
      - PORT
`
	// Server serves compose file but returns 500 for tree API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/lucos_test/contents/docker-compose.yml" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(composeFixture(compose))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_test",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "standard-env-vars-in-compose").Check(repo)
	if result.Err == nil {
		t.Errorf("expected Err when tree API fails")
	}
}

func TestStandardEnvVars_VarDeclaredWithValueInList(t *testing.T) {
	compose := `
services:
  app:
    build: .
    environment:
      - PORT=8080
      - LOGANNE_ENDPOINT=http://loganne:8080
`
	server := treeBlobServer(t, compose, nil)
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_test",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "standard-env-vars-in-compose").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when LOGANNE_ENDPOINT=value is declared, got fail: %s", result.Detail)
	}
}

func TestStandardEnvVars_NonSourceFilesIgnored(t *testing.T) {
	compose := `
services:
  app:
    build: .
    environment:
      - PORT
`
	// LOGANNE_ENDPOINT only appears in a .md file (not a source file)
	server := treeBlobServer(t, compose, map[string]string{
		"README.md":  `Set LOGANNE_ENDPOINT to configure logging`,
		"src/app.js": `console.log("Hello");`,
	})
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_test",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "standard-env-vars-in-compose").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when env var only in non-source files, got fail: %s", result.Detail)
	}
}

func TestDeclaredEnvVars_ListForm(t *testing.T) {
	yamlContent := `
services:
  app:
    environment:
      - PORT
      - LOGANNE_ENDPOINT=http://loganne
      - DEBUG
`
	var compose envVarsComposeFile
	if err := yaml.Unmarshal([]byte(yamlContent), &compose); err != nil {
		t.Fatalf("failed to parse test YAML: %v", err)
	}
	vars := declaredEnvVars(compose)
	for _, expected := range []string{"PORT", "LOGANNE_ENDPOINT", "DEBUG"} {
		if !vars[expected] {
			t.Errorf("expected %s to be declared, but it was not", expected)
		}
	}
}

func TestDeclaredEnvVars_MapForm(t *testing.T) {
	yamlContent := `
services:
  app:
    environment:
      PORT: "8080"
      LOGANNE_ENDPOINT: "http://loganne"
`
	var compose envVarsComposeFile
	if err := yaml.Unmarshal([]byte(yamlContent), &compose); err != nil {
		t.Fatalf("failed to parse test YAML: %v", err)
	}
	vars := declaredEnvVars(compose)
	for _, expected := range []string{"PORT", "LOGANNE_ENDPOINT"} {
		if !vars[expected] {
			t.Errorf("expected %s to be declared, but it was not", expected)
		}
	}
}

func TestDeclaredEnvVars_MultipleServices(t *testing.T) {
	yamlContent := `
services:
  app:
    environment:
      - PORT
  worker:
    environment:
      - LOGANNE_ENDPOINT
`
	var compose envVarsComposeFile
	if err := yaml.Unmarshal([]byte(yamlContent), &compose); err != nil {
		t.Fatalf("failed to parse test YAML: %v", err)
	}
	vars := declaredEnvVars(compose)
	if !vars["PORT"] {
		t.Error("expected PORT to be declared from app service")
	}
	if !vars["LOGANNE_ENDPOINT"] {
		t.Error("expected LOGANNE_ENDPOINT to be declared from worker service")
	}
}
