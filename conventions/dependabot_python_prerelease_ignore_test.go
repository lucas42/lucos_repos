package conventions

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- Registration ---

func TestDependabotPythonPrereleaseIgnore_Registered(t *testing.T) {
	c := findConvention(t, "dependabot-python-prerelease-ignore")
	if c.Description == "" {
		t.Error("has empty description")
	}
	if c.Rationale == "" {
		t.Error("has empty rationale")
	}
	if c.Guidance == "" {
		t.Error("has empty guidance")
	}
	if c.Check == nil {
		t.Error("has nil Check function")
	}
}

// --- Helper ---

// pythonPrereleaseServer creates a test server that serves the given files
// under /repos/lucas42/test_repo/contents/...
func pythonPrereleaseServer(t *testing.T, files map[string]string) *httptest.Server {
	t.Helper()
	encoded := make(map[string][]byte, len(files))
	for path, content := range files {
		encoded["/repos/lucas42/test_repo/contents/"+path] = composeFixture(content)
	}
	return serverWithFiles(encoded)
}

// --- Tests: no Docker setup ---

func TestDependabotPythonPrereleaseIgnore_NoCompose(t *testing.T) {
	// Repo with no docker-compose.yml — convention should pass.
	server := serverWithFiles(map[string][]byte{}) // 404 for everything
	defer server.Close()

	result := findConvention(t, "dependabot-python-prerelease-ignore").Check(repoCtx(server))
	if !result.Pass {
		t.Errorf("expected pass when no docker-compose.yml, got fail: %s", result.Detail)
	}
}

func TestDependabotPythonPrereleaseIgnore_NoBuiltServices(t *testing.T) {
	// docker-compose.yml with only image-based services (no build:).
	compose := `
services:
  redis:
    image: redis:7-alpine
`
	server := pythonPrereleaseServer(t, map[string]string{"docker-compose.yml": compose})
	defer server.Close()

	result := findConvention(t, "dependabot-python-prerelease-ignore").Check(repoCtx(server))
	if !result.Pass {
		t.Errorf("expected pass when no built services, got fail: %s", result.Detail)
	}
}

// --- Tests: non-Python base image ---

func TestDependabotPythonPrereleaseIgnore_NonPythonDockerfile(t *testing.T) {
	// Repo with a built service but no Python base image.
	compose := `
services:
  app:
    build: .
    image: lucas42/test_repo_app:${VERSION:-latest}
`
	dockerfile := `FROM node:22-alpine
ARG VERSION
ENV VERSION=$VERSION
COPY . .
`
	server := pythonPrereleaseServer(t, map[string]string{
		"docker-compose.yml": compose,
		"Dockerfile":         dockerfile,
	})
	defer server.Close()

	result := findConvention(t, "dependabot-python-prerelease-ignore").Check(repoCtx(server))
	if !result.Pass {
		t.Errorf("expected pass for non-Python Dockerfile, got fail: %s", result.Detail)
	}
}

// --- Tests: Python base image, correct ignore rule ---

func TestDependabotPythonPrereleaseIgnore_AlpineWithIgnoreRule(t *testing.T) {
	// Python-alpine image WITH a correct ignore rule → should pass.
	compose := `
services:
  app:
    build: .
    image: lucas42/test_repo_app:${VERSION:-latest}
`
	dockerfile := `FROM python:3.14.6-alpine
ARG VERSION
ENV VERSION=$VERSION
COPY . .
`
	dependabot := `
version: 2
updates:
  - package-ecosystem: docker
    directory: /
    schedule:
      interval: daily
    allow:
      - dependency-type: all
    ignore:
      - dependency-name: "python"
        versions:
          - "*.pre.alpine.a"
          - "*.pre.alpine.b"
          - "*.pre.alpine.rc*"
    groups:
      minor-and-patch:
        update-types: [minor, patch]
      major:
        update-types: [major]
  - package-ecosystem: github-actions
    directory: /
    schedule:
      interval: daily
    allow:
      - dependency-type: all
    groups:
      minor-and-patch:
        update-types: [minor, patch]
      major:
        update-types: [major]
`
	server := pythonPrereleaseServer(t, map[string]string{
		"docker-compose.yml":        compose,
		"Dockerfile":                dockerfile,
		".github/dependabot.yml":    dependabot,
	})
	defer server.Close()

	result := findConvention(t, "dependabot-python-prerelease-ignore").Check(repoCtx(server))
	if !result.Pass {
		t.Errorf("expected pass with correct alpine ignore rule, got fail: %s", result.Detail)
	}
}

func TestDependabotPythonPrereleaseIgnore_SlimWithIgnoreRule(t *testing.T) {
	// Python-slim image WITH a correct ignore rule → should pass.
	compose := `
services:
  app:
    build: .
    image: lucas42/test_repo_app:${VERSION:-latest}
`
	dockerfile := `FROM python:3.14-slim
ARG VERSION
ENV VERSION=$VERSION
COPY . .
`
	dependabot := `
version: 2
updates:
  - package-ecosystem: docker
    directory: /
    schedule:
      interval: daily
    allow:
      - dependency-type: all
    ignore:
      - dependency-name: "python"
        versions:
          - "*.pre.slim.a"
          - "*.pre.slim.b"
          - "*.pre.slim.rc*"
    groups:
      minor-and-patch:
        update-types: [minor, patch]
      major:
        update-types: [major]
  - package-ecosystem: github-actions
    directory: /
    schedule:
      interval: daily
    allow:
      - dependency-type: all
    groups:
      minor-and-patch:
        update-types: [minor, patch]
      major:
        update-types: [major]
`
	server := pythonPrereleaseServer(t, map[string]string{
		"docker-compose.yml":        compose,
		"Dockerfile":                dockerfile,
		".github/dependabot.yml":    dependabot,
	})
	defer server.Close()

	result := findConvention(t, "dependabot-python-prerelease-ignore").Check(repoCtx(server))
	if !result.Pass {
		t.Errorf("expected pass with correct slim ignore rule, got fail: %s", result.Detail)
	}
}

// --- Tests: Python base image, MISSING ignore rule ---

func TestDependabotPythonPrereleaseIgnore_MissingIgnoreRule(t *testing.T) {
	// Python image with a docker dependabot entry but NO ignore rule → fail.
	compose := `
services:
  app:
    build: .
    image: lucas42/test_repo_app:${VERSION:-latest}
`
	dockerfile := `FROM python:3.15.0b2-alpine
ARG VERSION
ENV VERSION=$VERSION
COPY . .
`
	dependabot := `
version: 2
updates:
  - package-ecosystem: docker
    directory: /
    schedule:
      interval: daily
    allow:
      - dependency-type: all
    groups:
      minor-and-patch:
        update-types: [minor, patch]
      major:
        update-types: [major]
  - package-ecosystem: github-actions
    directory: /
    schedule:
      interval: daily
    allow:
      - dependency-type: all
    groups:
      minor-and-patch:
        update-types: [minor, patch]
      major:
        update-types: [major]
`
	server := pythonPrereleaseServer(t, map[string]string{
		"docker-compose.yml":        compose,
		"Dockerfile":                dockerfile,
		".github/dependabot.yml":    dependabot,
	})
	defer server.Close()

	result := findConvention(t, "dependabot-python-prerelease-ignore").Check(repoCtx(server))
	if result.Pass {
		t.Error("expected fail when Python ignore rule is missing from docker entry")
	}
	if !strings.Contains(result.Detail, "missing a Python pre-release ignore rule") {
		t.Errorf("expected detail to mention missing ignore rule, got: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "python:3.15.0b2-alpine") {
		t.Errorf("expected detail to include base image tag, got: %s", result.Detail)
	}
}

func TestDependabotPythonPrereleaseIgnore_NoDependabotYml(t *testing.T) {
	// Python image with no dependabot.yml at all → fail.
	compose := `
services:
  app:
    build: .
    image: lucas42/test_repo_app:${VERSION:-latest}
`
	dockerfile := `FROM python:3.14.6-alpine
ARG VERSION
ENV VERSION=$VERSION
COPY . .
`
	server := pythonPrereleaseServer(t, map[string]string{
		"docker-compose.yml": compose,
		"Dockerfile":         dockerfile,
	})
	defer server.Close()

	result := findConvention(t, "dependabot-python-prerelease-ignore").Check(repoCtx(server))
	if result.Pass {
		t.Error("expected fail when no dependabot.yml and Python base image present")
	}
	if !strings.Contains(result.Detail, "dependabot.yml not found") {
		t.Errorf("expected detail to mention missing dependabot.yml, got: %s", result.Detail)
	}
}

func TestDependabotPythonPrereleaseIgnore_NoDockerDependabotEntry(t *testing.T) {
	// Python image with dependabot.yml but no docker ecosystem entry → fail.
	compose := `
services:
  app:
    build: .
    image: lucas42/test_repo_app:${VERSION:-latest}
`
	dockerfile := `FROM python:3.14.6-alpine
ARG VERSION
ENV VERSION=$VERSION
COPY . .
`
	dependabot := `
version: 2
updates:
  - package-ecosystem: github-actions
    directory: /
    schedule:
      interval: daily
    allow:
      - dependency-type: all
    groups:
      minor-and-patch:
        update-types: [minor, patch]
      major:
        update-types: [major]
`
	server := pythonPrereleaseServer(t, map[string]string{
		"docker-compose.yml":        compose,
		"Dockerfile":                dockerfile,
		".github/dependabot.yml":    dependabot,
	})
	defer server.Close()

	result := findConvention(t, "dependabot-python-prerelease-ignore").Check(repoCtx(server))
	if result.Pass {
		t.Error("expected fail when no docker dependabot entry for Python directory")
	}
	if !strings.Contains(result.Detail, "no docker dependabot entry") {
		t.Errorf("expected detail to mention missing docker entry, got: %s", result.Detail)
	}
}

// --- Tests: multi-service repo (api + worker, like lucos_photos) ---

func TestDependabotPythonPrereleaseIgnore_MultiServiceBothCorrect(t *testing.T) {
	// Two Python services (api and worker) each with their own docker entry
	// and ignore rule → should pass.
	compose := `
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
	apiDockerfile := `FROM python:3.14-slim
ARG VERSION
ENV VERSION=$VERSION
COPY . .
`
	workerDockerfile := `FROM python:3.14-slim
ARG VERSION
ENV VERSION=$VERSION
COPY . .
`
	dependabot := `
version: 2
updates:
  - package-ecosystem: docker
    directory: /api
    schedule:
      interval: daily
    allow:
      - dependency-type: all
    ignore:
      - dependency-name: "python"
        versions:
          - "*.pre.slim.a"
          - "*.pre.slim.b"
          - "*.pre.slim.rc*"
    groups:
      minor-and-patch:
        update-types: [minor, patch]
      major:
        update-types: [major]
  - package-ecosystem: docker
    directory: /worker
    schedule:
      interval: daily
    allow:
      - dependency-type: all
    ignore:
      - dependency-name: "python"
        versions:
          - "*.pre.slim.a"
          - "*.pre.slim.b"
          - "*.pre.slim.rc*"
    groups:
      minor-and-patch:
        update-types: [minor, patch]
      major:
        update-types: [major]
  - package-ecosystem: github-actions
    directory: /
    schedule:
      interval: daily
    allow:
      - dependency-type: all
    groups:
      minor-and-patch:
        update-types: [minor, patch]
      major:
        update-types: [major]
`
	server := pythonPrereleaseServer(t, map[string]string{
		"docker-compose.yml":        compose,
		"api/Dockerfile":            apiDockerfile,
		"worker/Dockerfile":         workerDockerfile,
		".github/dependabot.yml":    dependabot,
	})
	defer server.Close()

	result := findConvention(t, "dependabot-python-prerelease-ignore").Check(repoCtx(server))
	if !result.Pass {
		t.Errorf("expected pass with correct ignore rules on both services, got fail: %s", result.Detail)
	}
}

func TestDependabotPythonPrereleaseIgnore_MultiServiceOneMissing(t *testing.T) {
	// Two Python services (api and worker). api has the rule; worker is missing it.
	compose := `
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
	apiDockerfile := `FROM python:3.14-slim
ARG VERSION
ENV VERSION=$VERSION
COPY . .
`
	workerDockerfile := `FROM python:3.14-slim
ARG VERSION
ENV VERSION=$VERSION
COPY . .
`
	dependabot := `
version: 2
updates:
  - package-ecosystem: docker
    directory: /api
    schedule:
      interval: daily
    allow:
      - dependency-type: all
    ignore:
      - dependency-name: "python"
        versions:
          - "*.pre.slim.a"
          - "*.pre.slim.b"
    groups:
      minor-and-patch:
        update-types: [minor, patch]
      major:
        update-types: [major]
  - package-ecosystem: docker
    directory: /worker
    schedule:
      interval: daily
    allow:
      - dependency-type: all
    groups:
      minor-and-patch:
        update-types: [minor, patch]
      major:
        update-types: [major]
  - package-ecosystem: github-actions
    directory: /
    schedule:
      interval: daily
    allow:
      - dependency-type: all
    groups:
      minor-and-patch:
        update-types: [minor, patch]
      major:
        update-types: [major]
`
	server := pythonPrereleaseServer(t, map[string]string{
		"docker-compose.yml":        compose,
		"api/Dockerfile":            apiDockerfile,
		"worker/Dockerfile":         workerDockerfile,
		".github/dependabot.yml":    dependabot,
	})
	defer server.Close()

	result := findConvention(t, "dependabot-python-prerelease-ignore").Check(repoCtx(server))
	if result.Pass {
		t.Error("expected fail when one of two services is missing the ignore rule")
	}
	if !strings.Contains(result.Detail, "worker") {
		t.Errorf("expected detail to mention the failing directory (worker), got: %s", result.Detail)
	}
	// api should not be mentioned as a failure
	if strings.Contains(result.Detail, "\"/api\"") {
		t.Errorf("api should not appear in failure detail, got: %s", result.Detail)
	}
}

// --- Tests: mixed Python + non-Python services ---

func TestDependabotPythonPrereleaseIgnore_MixedServicesOnlyPythonChecked(t *testing.T) {
	// One Python service and one non-Python service. Only the Python directory
	// needs the ignore rule.
	compose := `
services:
  api:
    build:
      context: .
      dockerfile: api/Dockerfile
    image: lucas42/test_repo_api:${VERSION:-latest}
  web:
    build:
      context: .
      dockerfile: web/Dockerfile
    image: lucas42/test_repo_web:${VERSION:-latest}
`
	apiDockerfile := `FROM python:3.14.6-alpine
ARG VERSION
ENV VERSION=$VERSION
COPY . .
`
	webDockerfile := `FROM node:22-alpine
COPY . .
`
	dependabot := `
version: 2
updates:
  - package-ecosystem: docker
    directory: /api
    schedule:
      interval: daily
    allow:
      - dependency-type: all
    ignore:
      - dependency-name: "python"
        versions:
          - "*.pre.alpine.a"
          - "*.pre.alpine.b"
          - "*.pre.alpine.rc*"
    groups:
      minor-and-patch:
        update-types: [minor, patch]
      major:
        update-types: [major]
  - package-ecosystem: docker
    directory: /web
    schedule:
      interval: daily
    allow:
      - dependency-type: all
    groups:
      minor-and-patch:
        update-types: [minor, patch]
      major:
        update-types: [major]
  - package-ecosystem: github-actions
    directory: /
    schedule:
      interval: daily
    allow:
      - dependency-type: all
    groups:
      minor-and-patch:
        update-types: [minor, patch]
      major:
        update-types: [major]
`
	server := pythonPrereleaseServer(t, map[string]string{
		"docker-compose.yml":        compose,
		"api/Dockerfile":            apiDockerfile,
		"web/Dockerfile":            webDockerfile,
		".github/dependabot.yml":    dependabot,
	})
	defer server.Close()

	result := findConvention(t, "dependabot-python-prerelease-ignore").Check(repoCtx(server))
	if !result.Pass {
		t.Errorf("expected pass: Python dir has rule, non-Python dir doesn't need it, got fail: %s", result.Detail)
	}
}

// --- Tests: error cases ---

func TestDependabotPythonPrereleaseIgnore_ComposeAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	result := findConvention(t, "dependabot-python-prerelease-ignore").Check(repoCtx(server))
	if result.Err == nil {
		t.Error("expected Err when GitHub API returns 500")
	}
}
