package conventions

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestContainerNaming_Registered(t *testing.T) {
	c := findConvention(t, "container-naming")
	if c.Description == "" {
		t.Error("container-naming has empty description")
	}
	if c.Rationale == "" {
		t.Error("container-naming has empty rationale")
	}
	if c.Guidance == "" {
		t.Error("container-naming has empty guidance")
	}
	if c.Check == nil {
		t.Error("container-naming has nil Check function")
	}
	if !c.AppliesToType(RepoTypeSystem) {
		t.Error("container-naming should apply to RepoTypeSystem")
	}
	if c.AppliesToType(RepoTypeComponent) {
		t.Error("container-naming should not apply to RepoTypeComponent")
	}
}

func TestContainerNaming_NoComposeFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_photos",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "container-naming").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when no docker-compose.yml, got fail: %s", result.Detail)
	}
}

func TestContainerNaming_AllConforming(t *testing.T) {
	compose := `
services:
  api:
    container_name: lucos_photos_api
    build: .
  worker:
    container_name: lucos_photos_worker
    build: .
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/lucos_photos/contents/docker-compose.yml" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(composeFixture(compose))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_photos",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "container-naming").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when all container names conform, got fail: %s", result.Detail)
	}
}

func TestContainerNaming_ExactRepoNamePasses(t *testing.T) {
	compose := `
services:
  app:
    container_name: lucos_configy
    build: .
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/lucos_configy/contents/docker-compose.yml" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(composeFixture(compose))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_configy",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "container-naming").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when container_name equals repo name exactly, got fail: %s", result.Detail)
	}
}

func TestContainerNaming_ViolatingName(t *testing.T) {
	compose := `
services:
  app:
    container_name: monitoring
    build: .
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/lucos_monitoring/contents/docker-compose.yml" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(composeFixture(compose))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_monitoring",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "container-naming").Check(repo)
	if result.Pass {
		t.Errorf("expected fail for non-conforming container name, got pass")
	}
	if !strings.Contains(result.Detail, "monitoring") {
		t.Errorf("expected detail to mention 'monitoring', got: %s", result.Detail)
	}
}

func TestContainerNaming_NoExplicitContainerName(t *testing.T) {
	compose := `
services:
  app:
    build: .
  redis:
    image: redis:7-alpine
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/lucos_photos/contents/docker-compose.yml" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(composeFixture(compose))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_photos",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "container-naming").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when no explicit container_name set, got fail: %s", result.Detail)
	}
}

func TestContainerNaming_TestProfileSkipped(t *testing.T) {
	compose := `
services:
  app:
    container_name: lucos_photos
    build: .
  test:
    container_name: photos_test
    build: .
    profiles:
      - test
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/lucos_photos/contents/docker-compose.yml" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(composeFixture(compose))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_photos",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "container-naming").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when only test-profile service violates naming, got fail: %s", result.Detail)
	}
}

func TestContainerNaming_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_photos",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "container-naming").Check(repo)
	if result.Err == nil {
		t.Errorf("expected Err to be set when GitHub API returns 500, got nil")
	}
}

func TestContainerNaming_MultipleViolations(t *testing.T) {
	compose := `
services:
  web:
    container_name: monitoring
    build: .
  api:
    container_name: monitoring_api
    build: .
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/lucos_monitoring/contents/docker-compose.yml" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(composeFixture(compose))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_monitoring",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "container-naming").Check(repo)
	if result.Pass {
		t.Errorf("expected fail for multiple non-conforming names, got pass")
	}
	if !strings.Contains(result.Detail, "monitoring") {
		t.Errorf("expected detail to mention 'monitoring', got: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "monitoring_api") {
		t.Errorf("expected detail to mention 'monitoring_api', got: %s", result.Detail)
	}
}
