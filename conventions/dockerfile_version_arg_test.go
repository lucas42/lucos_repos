package conventions

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// dockerfileFixture encodes a Dockerfile string as a GitHub Contents API response.
func dockerfileFixture(content string) []byte {
	return composeFixture(content) // same encoding
}

// serverWithFiles returns a test server that serves a fixed set of path→body mappings.
func serverWithFiles(files map[string][]byte) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Strip leading /repos/lucas42/test_repo/contents/
		body, ok := files[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"Not Found"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	}))
}

// repoCtx returns a RepoContext pointing at the given test server.
func repoCtx(server *httptest.Server) RepoContext {
	return RepoContext{
		Name:          "lucas42/test_repo",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	}
}

const composeWithBuild = `
services:
  app:
    build: .
    image: lucas42/test_repo_app:${VERSION:-latest}
    healthcheck:
      test: ["CMD", "curl", "-sf", "http://127.0.0.1:8080/_info"]
`

const composeWithMultipleBuilds = `
services:
  api:
    build:
      context: .
      dockerfile: api/Dockerfile
    image: lucas42/test_repo_api:${VERSION:-latest}
  worker:
    build:
      context: .
      dockerfile: worker/Dockerfile
    image: lucas42/test_repo_worker:${VERSION:-latest}
`

const goodDockerfile = `FROM python:3.12-slim
ARG VERSION
ENV VERSION=$VERSION
COPY . .
`

const dockerfileWithDefaultArg = `FROM python:3.12-slim
ARG VERSION=unknown
ENV VERSION=$VERSION
COPY . .
`

const dockerfileWithBraceEnv = `FROM python:3.12-slim
ARG VERSION
ENV VERSION=${VERSION}
COPY . .
`

const dockerfileMissingArg = `FROM python:3.12-slim
ENV VERSION=$VERSION
COPY . .
`

const dockerfileMissingEnv = `FROM python:3.12-slim
ARG VERSION
COPY . .
`

const dockerfileMissingBoth = `FROM python:3.12-slim
COPY . .
`

// --- Registration ---

func TestDockerfileVersion_Registered(t *testing.T) {
	c := findConvention(t, "dockerfile-exposes-version")
	if c.Description == "" {
		t.Error("empty description")
	}
	if c.Rationale == "" {
		t.Error("empty rationale")
	}
	if c.Guidance == "" {
		t.Error("empty guidance")
	}
	if !c.AppliesToType(RepoTypeSystem) {
		t.Error("should apply to RepoTypeSystem")
	}
	if c.AppliesToType(RepoTypeComponent) {
		t.Error("should not apply to RepoTypeComponent")
	}
}

// --- No docker-compose.yml ---

func TestDockerfileVersion_NoComposeFile(t *testing.T) {
	server := serverWithFiles(map[string][]byte{})
	defer server.Close()

	result := findConvention(t, "dockerfile-exposes-version").Check(repoCtx(server))
	if !result.Pass {
		t.Errorf("expected pass when no docker-compose.yml, got: %s", result.Detail)
	}
}

// --- No built services ---

func TestDockerfileVersion_NoBuiltServices(t *testing.T) {
	compose := `
services:
  redis:
    image: redis:7-alpine
`
	server := serverWithFiles(map[string][]byte{
		"/repos/lucas42/test_repo/contents/docker-compose.yml": composeFixture(compose),
	})
	defer server.Close()

	result := findConvention(t, "dockerfile-exposes-version").Check(repoCtx(server))
	if !result.Pass {
		t.Errorf("expected pass when no built services, got: %s", result.Detail)
	}
}

// --- Happy path ---

func TestDockerfileVersion_PassesWithBothInstructions(t *testing.T) {
	server := serverWithFiles(map[string][]byte{
		"/repos/lucas42/test_repo/contents/docker-compose.yml": composeFixture(composeWithBuild),
		"/repos/lucas42/test_repo/contents/Dockerfile":         dockerfileFixture(goodDockerfile),
	})
	defer server.Close()

	result := findConvention(t, "dockerfile-exposes-version").Check(repoCtx(server))
	if !result.Pass {
		t.Errorf("expected pass with ARG VERSION + ENV VERSION=$VERSION, got: %s", result.Detail)
	}
}

func TestDockerfileVersion_PassesWithDefaultArg(t *testing.T) {
	server := serverWithFiles(map[string][]byte{
		"/repos/lucas42/test_repo/contents/docker-compose.yml": composeFixture(composeWithBuild),
		"/repos/lucas42/test_repo/contents/Dockerfile":         dockerfileFixture(dockerfileWithDefaultArg),
	})
	defer server.Close()

	result := findConvention(t, "dockerfile-exposes-version").Check(repoCtx(server))
	if !result.Pass {
		t.Errorf("expected pass with ARG VERSION=unknown, got: %s", result.Detail)
	}
}

func TestDockerfileVersion_PassesWithBraceEnvSyntax(t *testing.T) {
	server := serverWithFiles(map[string][]byte{
		"/repos/lucas42/test_repo/contents/docker-compose.yml": composeFixture(composeWithBuild),
		"/repos/lucas42/test_repo/contents/Dockerfile":         dockerfileFixture(dockerfileWithBraceEnv),
	})
	defer server.Close()

	result := findConvention(t, "dockerfile-exposes-version").Check(repoCtx(server))
	if !result.Pass {
		t.Errorf("expected pass with ENV VERSION=${VERSION}, got: %s", result.Detail)
	}
}

// --- Failing cases ---

func TestDockerfileVersion_FailsMissingArg(t *testing.T) {
	server := serverWithFiles(map[string][]byte{
		"/repos/lucas42/test_repo/contents/docker-compose.yml": composeFixture(composeWithBuild),
		"/repos/lucas42/test_repo/contents/Dockerfile":         dockerfileFixture(dockerfileMissingArg),
	})
	defer server.Close()

	result := findConvention(t, "dockerfile-exposes-version").Check(repoCtx(server))
	if result.Pass {
		t.Errorf("expected fail when ARG VERSION missing, got pass")
	}
	if !strings.Contains(result.Detail, "ARG VERSION") {
		t.Errorf("expected detail to mention ARG VERSION, got: %s", result.Detail)
	}
}

func TestDockerfileVersion_FailsMissingEnv(t *testing.T) {
	server := serverWithFiles(map[string][]byte{
		"/repos/lucas42/test_repo/contents/docker-compose.yml": composeFixture(composeWithBuild),
		"/repos/lucas42/test_repo/contents/Dockerfile":         dockerfileFixture(dockerfileMissingEnv),
	})
	defer server.Close()

	result := findConvention(t, "dockerfile-exposes-version").Check(repoCtx(server))
	if result.Pass {
		t.Errorf("expected fail when ENV VERSION missing, got pass")
	}
	if !strings.Contains(result.Detail, "ENV VERSION") {
		t.Errorf("expected detail to mention ENV VERSION, got: %s", result.Detail)
	}
}

func TestDockerfileVersion_FailsMissingBoth(t *testing.T) {
	server := serverWithFiles(map[string][]byte{
		"/repos/lucas42/test_repo/contents/docker-compose.yml": composeFixture(composeWithBuild),
		"/repos/lucas42/test_repo/contents/Dockerfile":         dockerfileFixture(dockerfileMissingBoth),
	})
	defer server.Close()

	result := findConvention(t, "dockerfile-exposes-version").Check(repoCtx(server))
	if result.Pass {
		t.Errorf("expected fail when both missing, got pass")
	}
	if !strings.Contains(result.Detail, "ARG VERSION") {
		t.Errorf("expected detail to mention ARG VERSION, got: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "ENV VERSION") {
		t.Errorf("expected detail to mention ENV VERSION, got: %s", result.Detail)
	}
}

// --- Multiple Dockerfiles ---

func TestDockerfileVersion_MultipleDockerfilesBothPass(t *testing.T) {
	server := serverWithFiles(map[string][]byte{
		"/repos/lucas42/test_repo/contents/docker-compose.yml":  composeFixture(composeWithMultipleBuilds),
		"/repos/lucas42/test_repo/contents/api/Dockerfile":      dockerfileFixture(goodDockerfile),
		"/repos/lucas42/test_repo/contents/worker/Dockerfile":   dockerfileFixture(goodDockerfile),
	})
	defer server.Close()

	result := findConvention(t, "dockerfile-exposes-version").Check(repoCtx(server))
	if !result.Pass {
		t.Errorf("expected pass when all Dockerfiles are correct, got: %s", result.Detail)
	}
}

func TestDockerfileVersion_MultipleDockerfilesOneFails(t *testing.T) {
	server := serverWithFiles(map[string][]byte{
		"/repos/lucas42/test_repo/contents/docker-compose.yml":  composeFixture(composeWithMultipleBuilds),
		"/repos/lucas42/test_repo/contents/api/Dockerfile":      dockerfileFixture(goodDockerfile),
		"/repos/lucas42/test_repo/contents/worker/Dockerfile":   dockerfileFixture(dockerfileMissingBoth),
	})
	defer server.Close()

	result := findConvention(t, "dockerfile-exposes-version").Check(repoCtx(server))
	if result.Pass {
		t.Errorf("expected fail when one Dockerfile is missing instructions, got pass")
	}
	if !strings.Contains(result.Detail, "worker/Dockerfile") {
		t.Errorf("expected detail to mention worker/Dockerfile, got: %s", result.Detail)
	}
}

// --- Test profile services skipped ---

func TestDockerfileVersion_TestProfileServiceSkipped(t *testing.T) {
	compose := `
services:
  app:
    build: .
    image: lucas42/test_repo_app:${VERSION:-latest}
    healthcheck:
      test: ["CMD", "curl", "-sf", "http://127.0.0.1:8080/_info"]
  test:
    build: .
    profiles:
      - test
`
	server := serverWithFiles(map[string][]byte{
		"/repos/lucas42/test_repo/contents/docker-compose.yml": composeFixture(compose),
		"/repos/lucas42/test_repo/contents/Dockerfile":         dockerfileFixture(goodDockerfile),
	})
	defer server.Close()

	result := findConvention(t, "dockerfile-exposes-version").Check(repoCtx(server))
	if !result.Pass {
		t.Errorf("expected pass when only test-profile service Dockerfile missing instructions, got: %s", result.Detail)
	}
}

// --- Duplicate Dockerfiles only checked once ---

func TestDockerfileVersion_DuplicateDockerfileCheckedOnce(t *testing.T) {
	compose := `
services:
  api:
    build: .
    image: lucas42/test_repo_api:${VERSION:-latest}
  worker:
    build: .
    image: lucas42/test_repo_worker:${VERSION:-latest}
`
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/test_repo/contents/docker-compose.yml" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(composeFixture(compose))
			return
		}
		if r.URL.Path == "/repos/lucas42/test_repo/contents/Dockerfile" {
			callCount++
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(dockerfileFixture(goodDockerfile))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	result := findConvention(t, "dockerfile-exposes-version").Check(repoCtx(server))
	if !result.Pass {
		t.Errorf("expected pass, got: %s", result.Detail)
	}
	if callCount != 1 {
		t.Errorf("expected Dockerfile to be fetched exactly once, got %d fetches", callCount)
	}
}

// --- Image tag checks ---

func TestDockerfileVersion_FailsMissingImageTag(t *testing.T) {
	compose := `
services:
  app:
    build: .
    image: lucas42/test_repo_app
    healthcheck:
      test: ["CMD", "curl", "-sf", "http://127.0.0.1:8080/_info"]
`
	server := serverWithFiles(map[string][]byte{
		"/repos/lucas42/test_repo/contents/docker-compose.yml": composeFixture(compose),
		"/repos/lucas42/test_repo/contents/Dockerfile":         dockerfileFixture(goodDockerfile),
	})
	defer server.Close()

	result := findConvention(t, "dockerfile-exposes-version").Check(repoCtx(server))
	if result.Pass {
		t.Errorf("expected fail when image tag missing ${VERSION:-latest}, got pass")
	}
	if !strings.Contains(result.Detail, "${VERSION:-latest}") {
		t.Errorf("expected detail to mention ${VERSION:-latest}, got: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "app") {
		t.Errorf("expected detail to mention service name 'app', got: %s", result.Detail)
	}
}

func TestDockerfileVersion_FailsNoImageField(t *testing.T) {
	compose := `
services:
  app:
    build: .
    healthcheck:
      test: ["CMD", "curl", "-sf", "http://127.0.0.1:8080/_info"]
`
	server := serverWithFiles(map[string][]byte{
		"/repos/lucas42/test_repo/contents/docker-compose.yml": composeFixture(compose),
		"/repos/lucas42/test_repo/contents/Dockerfile":         dockerfileFixture(goodDockerfile),
	})
	defer server.Close()

	result := findConvention(t, "dockerfile-exposes-version").Check(repoCtx(server))
	if result.Pass {
		t.Errorf("expected fail when image field absent, got pass")
	}
	if !strings.Contains(result.Detail, "${VERSION:-latest}") {
		t.Errorf("expected detail to mention ${VERSION:-latest}, got: %s", result.Detail)
	}
}

func TestDockerfileVersion_FailsImageTagLatestOnly(t *testing.T) {
	compose := `
services:
  app:
    build: .
    image: lucas42/test_repo_app:latest
    healthcheck:
      test: ["CMD", "curl", "-sf", "http://127.0.0.1:8080/_info"]
`
	server := serverWithFiles(map[string][]byte{
		"/repos/lucas42/test_repo/contents/docker-compose.yml": composeFixture(compose),
		"/repos/lucas42/test_repo/contents/Dockerfile":         dockerfileFixture(goodDockerfile),
	})
	defer server.Close()

	result := findConvention(t, "dockerfile-exposes-version").Check(repoCtx(server))
	if result.Pass {
		t.Errorf("expected fail when image tag is bare :latest (not ${VERSION:-latest}), got pass")
	}
}

func TestDockerfileVersion_TestProfileServiceImageTagNotChecked(t *testing.T) {
	compose := `
services:
  app:
    build: .
    image: lucas42/test_repo_app:${VERSION:-latest}
    healthcheck:
      test: ["CMD", "curl", "-sf", "http://127.0.0.1:8080/_info"]
  test:
    build: .
    profiles:
      - test
`
	server := serverWithFiles(map[string][]byte{
		"/repos/lucas42/test_repo/contents/docker-compose.yml": composeFixture(compose),
		"/repos/lucas42/test_repo/contents/Dockerfile":         dockerfileFixture(goodDockerfile),
	})
	defer server.Close()

	result := findConvention(t, "dockerfile-exposes-version").Check(repoCtx(server))
	if !result.Pass {
		t.Errorf("expected pass when test-profile service lacks image tag, got: %s", result.Detail)
	}
}

// --- Error handling ---

func TestDockerfileVersion_ComposeAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	result := findConvention(t, "dockerfile-exposes-version").Check(repoCtx(server))
	if result.Err == nil {
		t.Errorf("expected Err when API returns 500, got nil (Pass=%v Detail=%q)", result.Pass, result.Detail)
	}
}

// --- Helper tests ---

func TestDockerfilePathForBuild_StringContext(t *testing.T) {
	if got := dockerfilePathForBuild("."); got != "Dockerfile" {
		t.Errorf("expected Dockerfile, got %s", got)
	}
	if got := dockerfilePathForBuild("./api"); got != "./api/Dockerfile" {
		t.Errorf("expected ./api/Dockerfile, got %s", got)
	}
}

func TestDockerfilePathForBuild_MapContext(t *testing.T) {
	build := map[string]interface{}{
		"context":    ".",
		"dockerfile": "api/Dockerfile",
	}
	if got := dockerfilePathForBuild(build); got != "api/Dockerfile" {
		t.Errorf("expected api/Dockerfile, got %s", got)
	}
}

func TestDockerfilePathForBuild_MapNoDockerfile(t *testing.T) {
	build := map[string]interface{}{
		"context": ".",
	}
	if got := dockerfilePathForBuild(build); got != "Dockerfile" {
		t.Errorf("expected Dockerfile, got %s", got)
	}
}

func TestDockerfileHasVersionArg(t *testing.T) {
	if !dockerfileHasVersionArg([]byte("ARG VERSION\n")) {
		t.Error("expected true for 'ARG VERSION'")
	}
	if !dockerfileHasVersionArg([]byte("ARG VERSION=unknown\n")) {
		t.Error("expected true for 'ARG VERSION=unknown'")
	}
	if dockerfileHasVersionArg([]byte("ARG OTHER\n")) {
		t.Error("expected false for 'ARG OTHER'")
	}
}

func TestDockerfileExposesVersionEnv(t *testing.T) {
	if !dockerfileExposesVersionEnv([]byte("ENV VERSION=$VERSION\n")) {
		t.Error("expected true for 'ENV VERSION=$VERSION'")
	}
	if !dockerfileExposesVersionEnv([]byte("ENV VERSION=${VERSION}\n")) {
		t.Error("expected true for 'ENV VERSION=${VERSION}'")
	}
	if dockerfileExposesVersionEnv([]byte("ENV VERSION=1.0.0\n")) {
		t.Error("expected false for hardcoded 'ENV VERSION=1.0.0'")
	}
}
