package conventions

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- Registration ---

func TestDockerDependabotUpdaterPresent_Registered(t *testing.T) {
	c := findConvention(t, "docker-dependabot-updater-present")
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

func dockerDependabotUpdaterServer(t *testing.T, files map[string]string) *httptest.Server {
	t.Helper()
	encoded := make(map[string][]byte, len(files))
	for path, content := range files {
		encoded["/repos/lucas42/test_repo/contents/"+path] = composeFixture(content)
	}
	return serverWithFiles(encoded)
}

// --- Tests: no Docker setup ---

func TestDockerDependabotUpdaterPresent_NoCompose(t *testing.T) {
	// Repo with no docker-compose.yml → convention does not apply → pass.
	server := serverWithFiles(map[string][]byte{})
	defer server.Close()

	result := findConvention(t, "docker-dependabot-updater-present").Check(repoCtx(server))
	if !result.Pass {
		t.Errorf("expected pass when no docker-compose.yml, got fail: %s", result.Detail)
	}
}

func TestDockerDependabotUpdaterPresent_NoBuiltServices(t *testing.T) {
	// docker-compose.yml with only image-based services → convention does not apply.
	compose := `
services:
  redis:
    image: redis:7-alpine
`
	server := dockerDependabotUpdaterServer(t, map[string]string{"docker-compose.yml": compose})
	defer server.Close()

	result := findConvention(t, "docker-dependabot-updater-present").Check(repoCtx(server))
	if !result.Pass {
		t.Errorf("expected pass when no built services, got fail: %s", result.Detail)
	}
}

// --- Tests: built service, docker entry present ---

func TestDockerDependabotUpdaterPresent_SingleServiceWithEntry(t *testing.T) {
	// Single built service at root with a docker entry → pass.
	compose := `
services:
  app:
    build: .
    image: lucas42/test_repo_app:${VERSION:-latest}
`
	dependabot := `
version: 2
updates:
  - package-ecosystem: docker
    directory: /
    schedule:
      interval: weekly
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
      interval: weekly
    allow:
      - dependency-type: all
    groups:
      minor-and-patch:
        update-types: [minor, patch]
      major:
        update-types: [major]
`
	server := dockerDependabotUpdaterServer(t, map[string]string{
		"docker-compose.yml":     compose,
		".github/dependabot.yml": dependabot,
	})
	defer server.Close()

	result := findConvention(t, "docker-dependabot-updater-present").Check(repoCtx(server))
	if !result.Pass {
		t.Errorf("expected pass with docker entry present, got fail: %s", result.Detail)
	}
}

func TestDockerDependabotUpdaterPresent_MultiServiceBothPresent(t *testing.T) {
	// Two built services (api + worker), both have docker entries → pass.
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
	dependabot := `
version: 2
updates:
  - package-ecosystem: docker
    directory: /api
    schedule:
      interval: weekly
    allow:
      - dependency-type: all
    groups:
      minor-and-patch:
        update-types: [minor, patch]
      major:
        update-types: [major]
  - package-ecosystem: docker
    directory: /worker
    schedule:
      interval: weekly
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
      interval: weekly
    allow:
      - dependency-type: all
    groups:
      minor-and-patch:
        update-types: [minor, patch]
      major:
        update-types: [major]
`
	server := dockerDependabotUpdaterServer(t, map[string]string{
		"docker-compose.yml":     compose,
		".github/dependabot.yml": dependabot,
	})
	defer server.Close()

	result := findConvention(t, "docker-dependabot-updater-present").Check(repoCtx(server))
	if !result.Pass {
		t.Errorf("expected pass with both docker entries present, got fail: %s", result.Detail)
	}
}

func TestDockerDependabotUpdaterPresent_PluralDirectoriesForm(t *testing.T) {
	// Two services covered by a single entry using directories: ["/api", "/worker"] → pass.
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
	dependabot := `
version: 2
updates:
  - package-ecosystem: docker
    directories: ["/api", "/worker"]
    schedule:
      interval: weekly
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
      interval: weekly
    allow:
      - dependency-type: all
    groups:
      minor-and-patch:
        update-types: [minor, patch]
      major:
        update-types: [major]
`
	server := dockerDependabotUpdaterServer(t, map[string]string{
		"docker-compose.yml":     compose,
		".github/dependabot.yml": dependabot,
	})
	defer server.Close()

	result := findConvention(t, "docker-dependabot-updater-present").Check(repoCtx(server))
	if !result.Pass {
		t.Errorf("expected pass with plural directories: form, got fail: %s", result.Detail)
	}
}

// --- Tests: built service, docker entry MISSING ---

func TestDockerDependabotUpdaterPresent_NoDependabotYml(t *testing.T) {
	// Built service but no dependabot.yml at all → fail.
	compose := `
services:
  app:
    build: .
    image: lucas42/test_repo_app:${VERSION:-latest}
`
	server := dockerDependabotUpdaterServer(t, map[string]string{"docker-compose.yml": compose})
	defer server.Close()

	result := findConvention(t, "docker-dependabot-updater-present").Check(repoCtx(server))
	if result.Pass {
		t.Error("expected fail when no dependabot.yml and built service present")
	}
	if !strings.Contains(result.Detail, "dependabot.yml not found") {
		t.Errorf("expected detail to mention missing dependabot.yml, got: %s", result.Detail)
	}
}

func TestDockerDependabotUpdaterPresent_MissingDockerEntry_NodeService(t *testing.T) {
	// Node-based built service with no docker entry → fail.
	// Verifies that non-Python repos (Go/Node/etc.) are caught too.
	compose := `
services:
  app:
    build: .
    image: lucas42/test_repo_app:${VERSION:-latest}
`
	dependabot := `
version: 2
updates:
  - package-ecosystem: github-actions
    directory: /
    schedule:
      interval: weekly
    allow:
      - dependency-type: all
    groups:
      minor-and-patch:
        update-types: [minor, patch]
      major:
        update-types: [major]
`
	server := dockerDependabotUpdaterServer(t, map[string]string{
		"docker-compose.yml":     compose,
		".github/dependabot.yml": dependabot,
	})
	defer server.Close()

	result := findConvention(t, "docker-dependabot-updater-present").Check(repoCtx(server))
	if result.Pass {
		t.Error("expected fail when no docker entry for built service (Node/non-Python)")
	}
	if !strings.Contains(result.Detail, `"/"`) {
		t.Errorf(`expected detail to mention missing "/" directory, got: %s`, result.Detail)
	}
}

func TestDockerDependabotUpdaterPresent_MultiServiceOneMissing(t *testing.T) {
	// Two built services; only api has a docker entry. Worker is missing → fail.
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
	dependabot := `
version: 2
updates:
  - package-ecosystem: docker
    directory: /api
    schedule:
      interval: weekly
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
      interval: weekly
    allow:
      - dependency-type: all
    groups:
      minor-and-patch:
        update-types: [minor, patch]
      major:
        update-types: [major]
`
	server := dockerDependabotUpdaterServer(t, map[string]string{
		"docker-compose.yml":     compose,
		".github/dependabot.yml": dependabot,
	})
	defer server.Close()

	result := findConvention(t, "docker-dependabot-updater-present").Check(repoCtx(server))
	if result.Pass {
		t.Error("expected fail when one of two services is missing a docker entry")
	}
	if !strings.Contains(result.Detail, `"/worker"`) {
		t.Errorf(`expected detail to mention missing "/worker" directory, got: %s`, result.Detail)
	}
	// /api should NOT appear in failures
	if strings.Contains(result.Detail, `"/api"`) {
		t.Errorf(`/api should not appear in failure detail, got: %s`, result.Detail)
	}
}

func TestDockerDependabotUpdaterPresent_PluralDirectoriesOneMissing(t *testing.T) {
	// Two services; dependabot uses directories: ["/api"] but worker is absent → fail.
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
	dependabot := `
version: 2
updates:
  - package-ecosystem: docker
    directories: ["/api"]
    schedule:
      interval: weekly
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
      interval: weekly
    allow:
      - dependency-type: all
    groups:
      minor-and-patch:
        update-types: [minor, patch]
      major:
        update-types: [major]
`
	server := dockerDependabotUpdaterServer(t, map[string]string{
		"docker-compose.yml":     compose,
		".github/dependabot.yml": dependabot,
	})
	defer server.Close()

	result := findConvention(t, "docker-dependabot-updater-present").Check(repoCtx(server))
	if result.Pass {
		t.Error("expected fail when plural directories: form is missing one service directory")
	}
	if !strings.Contains(result.Detail, `"/worker"`) {
		t.Errorf(`expected detail to mention missing "/worker" directory, got: %s`, result.Detail)
	}
}

// --- Tests: test-profile services excluded ---

func TestDockerDependabotUpdaterPresent_TestProfileServiceExcluded(t *testing.T) {
	// A built service in the "test" profile should be ignored — no docker entry required.
	compose := `
services:
  app:
    build: .
    image: lucas42/test_repo_app:${VERSION:-latest}
  tests:
    build:
      context: .
      dockerfile: Dockerfile.test
    profiles: [test]
`
	dependabot := `
version: 2
updates:
  - package-ecosystem: docker
    directory: /
    schedule:
      interval: weekly
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
      interval: weekly
    allow:
      - dependency-type: all
    groups:
      minor-and-patch:
        update-types: [minor, patch]
      major:
        update-types: [major]
`
	server := dockerDependabotUpdaterServer(t, map[string]string{
		"docker-compose.yml":     compose,
		".github/dependabot.yml": dependabot,
	})
	defer server.Close()

	result := findConvention(t, "docker-dependabot-updater-present").Check(repoCtx(server))
	if !result.Pass {
		t.Errorf("expected pass: test-profile service should not require a docker entry, got fail: %s", result.Detail)
	}
}

// --- Tests: error cases ---

func TestDockerDependabotUpdaterPresent_ComposeAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	result := findConvention(t, "docker-dependabot-updater-present").Check(repoCtx(server))
	if result.Err == nil {
		t.Error("expected Err when GitHub API returns 500")
	}
}
