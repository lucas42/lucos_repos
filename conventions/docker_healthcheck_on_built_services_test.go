package conventions

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// composeFixture encodes a docker-compose.yml string as a GitHub Contents API
// response body, matching the base64+newline wrapping that GitHub uses.
func composeFixture(content string) []byte {
	encoded := base64.StdEncoding.EncodeToString([]byte(content))
	// GitHub wraps the base64 output in newlines every 60 chars.
	var wrapped strings.Builder
	for i, ch := range encoded {
		if i > 0 && i%60 == 0 {
			wrapped.WriteRune('\n')
		}
		wrapped.WriteRune(ch)
	}
	wrapped.WriteRune('\n')

	type contentsResp struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	b, _ := json.Marshal(contentsResp{
		Content:  wrapped.String(),
		Encoding: "base64",
	})
	return b
}

// TestDockerHealthcheck_Registered verifies the convention is registered with
// the expected fields.
func TestDockerHealthcheck_Registered(t *testing.T) {
	cs := All()
	var found *Convention
	for i, c := range cs {
		if c.ID == "docker-healthcheck-on-built-services" {
			found = &cs[i]
			break
		}
	}
	if found == nil {
		t.Fatal("docker-healthcheck-on-built-services convention not found in registry")
	}
	if found.Description == "" {
		t.Error("docker-healthcheck-on-built-services has empty description")
	}
	if found.Rationale == "" {
		t.Error("docker-healthcheck-on-built-services has empty rationale")
	}
	if found.Guidance == "" {
		t.Error("docker-healthcheck-on-built-services has empty guidance")
	}
	if found.Check == nil {
		t.Error("docker-healthcheck-on-built-services has nil Check function")
	}
	if !found.AppliesToType(RepoTypeSystem) {
		t.Error("docker-healthcheck-on-built-services should apply to RepoTypeSystem")
	}
	if found.AppliesToType(RepoTypeComponent) {
		t.Error("docker-healthcheck-on-built-services should not apply to RepoTypeComponent")
	}
	if found.AppliesToType(RepoTypeUnconfigured) {
		t.Error("docker-healthcheck-on-built-services should not apply to RepoTypeUnconfigured")
	}
}

// TestDockerHealthcheck_NoComposeFile verifies the convention passes when there
// is no docker-compose.yml.
func TestDockerHealthcheck_NoComposeFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/test_repo",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "docker-healthcheck-on-built-services").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when no docker-compose.yml, got fail: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "does not apply") {
		t.Errorf("expected detail to say convention does not apply, got: %s", result.Detail)
	}
}

// TestDockerHealthcheck_NoBuiltServices verifies the convention passes when the
// compose file exists but has no services with a build: key.
func TestDockerHealthcheck_NoBuiltServices(t *testing.T) {
	compose := `
services:
  redis:
    image: redis:7-alpine
  postgres:
    image: postgres:16
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/test_repo/contents/docker-compose.yml" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(composeFixture(compose))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/test_repo",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "docker-healthcheck-on-built-services").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when no built services, got fail: %s", result.Detail)
	}
}

// TestDockerHealthcheck_AllBuiltServicesHaveHealthcheck verifies the convention
// passes when every built service defines a healthcheck.
func TestDockerHealthcheck_AllBuiltServicesHaveHealthcheck(t *testing.T) {
	compose := `
services:
  app:
    build: .
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost:8080/_info"]
      interval: 10s
      timeout: 5s
      retries: 3
      start_period: 15s
  redis:
    image: redis:7-alpine
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/test_repo/contents/docker-compose.yml" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(composeFixture(compose))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/test_repo",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "docker-healthcheck-on-built-services").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when all built services have healthcheck, got fail: %s", result.Detail)
	}
}

// TestDockerHealthcheck_BuildContextObject verifies that the convention handles
// the long-form build: {context: ., dockerfile: ...} syntax as well as the
// short-form build: . syntax.
func TestDockerHealthcheck_BuildContextObject(t *testing.T) {
	compose := `
services:
  api:
    build:
      context: .
      dockerfile: api/Dockerfile
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost:8080/_info"]
      interval: 10s
      timeout: 5s
      retries: 3
      start_period: 15s
  worker:
    build:
      context: .
      dockerfile: worker/Dockerfile
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost:8081/_info"]
      interval: 10s
      timeout: 5s
      retries: 3
      start_period: 15s
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/test_repo/contents/docker-compose.yml" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(composeFixture(compose))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/test_repo",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "docker-healthcheck-on-built-services").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when all built services have healthcheck (object build syntax), got fail: %s", result.Detail)
	}
}

// TestDockerHealthcheck_MissingHealthcheck verifies the convention fails when a
// built service is missing a healthcheck.
func TestDockerHealthcheck_MissingHealthcheck(t *testing.T) {
	compose := `
services:
  app:
    build: .
  redis:
    image: redis:7-alpine
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/test_repo/contents/docker-compose.yml" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(composeFixture(compose))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/test_repo",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "docker-healthcheck-on-built-services").Check(repo)
	if result.Pass {
		t.Errorf("expected fail when built service has no healthcheck, got pass: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "app") {
		t.Errorf("expected detail to mention the offending service 'app', got: %s", result.Detail)
	}
}

// TestDockerHealthcheck_MultipleMissing verifies the convention fails and lists
// all services missing a healthcheck when there are multiple.
func TestDockerHealthcheck_MultipleMissing(t *testing.T) {
	compose := `
services:
  api:
    build:
      context: .
      dockerfile: api/Dockerfile
  worker:
    build:
      context: .
      dockerfile: worker/Dockerfile
  redis:
    image: redis:7-alpine
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/test_repo/contents/docker-compose.yml" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(composeFixture(compose))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/test_repo",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "docker-healthcheck-on-built-services").Check(repo)
	if result.Pass {
		t.Errorf("expected fail when multiple built services lack healthcheck, got pass: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "api") {
		t.Errorf("expected detail to mention 'api', got: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "worker") {
		t.Errorf("expected detail to mention 'worker', got: %s", result.Detail)
	}
}

// TestDockerHealthcheck_TestProfileServiceSkipped verifies that a built service
// in the "test" docker-compose profile is not flagged, even without a healthcheck.
func TestDockerHealthcheck_TestProfileServiceSkipped(t *testing.T) {
	compose := `
services:
  app:
    build: .
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://127.0.0.1:8080/_info"]
      interval: 10s
      timeout: 5s
      retries: 3
      start_period: 15s
  test:
    build: .
    profiles:
      - test
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/test_repo/contents/docker-compose.yml" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(composeFixture(compose))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/test_repo",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "docker-healthcheck-on-built-services").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when only test-profile service lacks healthcheck, got fail: %s", result.Detail)
	}
}

// TestDockerHealthcheck_NonTestProfileStillChecked verifies that a built service
// in a non-test profile (e.g. "debug") is still checked and flagged.
func TestDockerHealthcheck_NonTestProfileStillChecked(t *testing.T) {
	compose := `
services:
  app:
    build: .
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://127.0.0.1:8080/_info"]
      interval: 10s
      timeout: 5s
      retries: 3
      start_period: 15s
  debug:
    build: .
    profiles:
      - debug
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/test_repo/contents/docker-compose.yml" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(composeFixture(compose))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/test_repo",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "docker-healthcheck-on-built-services").Check(repo)
	if result.Pass {
		t.Errorf("expected fail when non-test-profile service lacks healthcheck, got pass: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "debug") {
		t.Errorf("expected detail to mention 'debug', got: %s", result.Detail)
	}
}

// TestDockerHealthcheck_MultipleProfilesIncludingTest verifies that a service
// belonging to multiple profiles, one of which is "test", is skipped.
func TestDockerHealthcheck_MultipleProfilesIncludingTest(t *testing.T) {
	compose := `
services:
  app:
    build: .
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://127.0.0.1:8080/_info"]
      interval: 10s
      timeout: 5s
      retries: 3
      start_period: 15s
  test-worker:
    build: .
    profiles:
      - test
      - ci
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/test_repo/contents/docker-compose.yml" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(composeFixture(compose))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/test_repo",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "docker-healthcheck-on-built-services").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when service with test profile (and others) lacks healthcheck, got fail: %s", result.Detail)
	}
}

// TestDockerHealthcheck_APIError verifies the convention fails gracefully when
// the GitHub API returns an unexpected error.
func TestDockerHealthcheck_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/test_repo",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "docker-healthcheck-on-built-services").Check(repo)
	if result.Pass {
		t.Errorf("expected fail when GitHub API returns an error, got pass: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "Error") {
		t.Errorf("expected detail to mention error, got: %s", result.Detail)
	}
}

// TestGitHubFileContent_ReturnsContent verifies that GitHubFileContentFromBase
// correctly decodes a base64-encoded file response.
func TestGitHubFileContent_ReturnsContent(t *testing.T) {
	expected := "services:\n  app:\n    build: .\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/test_repo/contents/docker-compose.yml" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(composeFixture(expected))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	content, err := GitHubFileContentFromBase(server.URL, "fake-token", "lucas42/test_repo", "docker-compose.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(content) != expected {
		t.Errorf("expected content %q, got %q", expected, string(content))
	}
}

// TestGitHubFileContent_NotFound verifies that a 404 returns nil, nil.
func TestGitHubFileContent_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer server.Close()

	content, err := GitHubFileContentFromBase(server.URL, "fake-token", "lucas42/test_repo", "docker-compose.yml")
	if err != nil {
		t.Fatalf("unexpected error for 404: %v", err)
	}
	if content != nil {
		t.Errorf("expected nil content for 404, got %q", string(content))
	}
}

// TestGitHubFileContent_APIError verifies that unexpected HTTP status codes
// return an error.
func TestGitHubFileContent_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"Resource not accessible by integration"}`))
	}))
	defer server.Close()

	_, err := GitHubFileContentFromBase(server.URL, "fake-token", "lucas42/test_repo", "docker-compose.yml")
	if err == nil {
		t.Error("expected error for 403 response, got nil")
	}
}

// findConvention is a test helper that looks up a convention by ID and fails the
// test if it is not found.
func findConvention(t *testing.T, id string) Convention {
	t.Helper()
	for _, c := range All() {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("convention %q not found in registry", id)
	return Convention{}
}
