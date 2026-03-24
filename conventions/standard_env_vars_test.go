package conventions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/lucos_test/contents/docker-compose.yml" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(composeFixture(compose))
			return
		}
		// Search should not be called when var is already declared,
		// but return 0 results just in case.
		if strings.HasPrefix(r.URL.Path, "/search/code") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]int{"total_count": 1})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/lucos_test/contents/docker-compose.yml" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(composeFixture(compose))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/search/code") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]int{"total_count": 1})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/lucos_test/contents/docker-compose.yml" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(composeFixture(compose))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/search/code") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]int{"total_count": 3})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/lucos_test/contents/docker-compose.yml" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(composeFixture(compose))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/search/code") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]int{"total_count": 0})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
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

func TestStandardEnvVars_SearchAPIError(t *testing.T) {
	compose := `
services:
  app:
    build: .
    environment:
      - PORT
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/lucos_test/contents/docker-compose.yml" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(composeFixture(compose))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/search/code") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_test",
		GitHubToken:   "fake-token",
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "standard-env-vars-in-compose").Check(repo)
	if result.Err == nil {
		t.Errorf("expected Err when search API returns 403")
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/lucos_test/contents/docker-compose.yml" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(composeFixture(compose))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/search/code") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]int{"total_count": 1})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
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
