package conventions

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
)

// encodedWorkflowConfig returns a base64-encoded CircleCI config with test and build jobs.
func encodedWorkflowConfig() string {
	yaml := `version: 2.1
orbs:
  lucos: lucos/deploy@0
workflows:
  build-test-deploy:
    jobs:
      - test
      - lucos/build-amd64:
          name: build
      - lucos/deploy-avalon:
          requires:
            - test
            - build
`
	encoded := base64.StdEncoding.EncodeToString([]byte(yaml))
	return `{"content":"` + encoded + `","encoding":"base64"}`
}

// encodedProtectionWithChecks returns a fake branch protection response with
// named required status checks.
func encodedProtectionWithChecks(checks []string) string {
	contextsJSON := "["
	for i, c := range checks {
		if i > 0 {
			contextsJSON += ","
		}
		contextsJSON += `"` + c + `"`
	}
	contextsJSON += "]"
	return `{"required_status_checks":{"contexts":` + contextsJSON + `}}`
}

// TestCircleCIJobsInRequiredChecks_AllPresent verifies a pass when all test/build
// jobs are in the required status checks.
func TestCircleCIJobsInRequiredChecks_AllPresent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/lucas42/test_repo/contents/.circleci/config.yml":
			w.Write([]byte(encodedWorkflowConfig()))
		case "/repos/lucas42/test_repo/branches/main/protection":
			// "lucos/build-amd64" is the check name GitHub sees for the orb job.
			w.Write([]byte(encodedProtectionWithChecks([]string{"test", "lucos/build-amd64"})))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/test_repo",
		GitHubToken:   "fake-token",
		Type:          RepoTypeSystem,
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "circleci-jobs-in-required-checks").Check(repo)
	if !result.Pass {
		t.Errorf("expected Pass=true, got Detail=%q", result.Detail)
	}
}

// TestCircleCIJobsInRequiredChecks_MissingJob verifies a failure when a test/build
// job is not in the required status checks.
// The config has jobs "test" and "lucos/build-amd64" (the orb job name as GitHub
// sees it); both must be required. We provide only "test" as a required check.
func TestCircleCIJobsInRequiredChecks_MissingJob(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/lucas42/test_repo/contents/.circleci/config.yml":
			w.Write([]byte(encodedWorkflowConfig()))
		case "/repos/lucas42/test_repo/branches/main/protection":
			// Only "test" is required — "lucos/build-amd64" is missing.
			w.Write([]byte(encodedProtectionWithChecks([]string{"test"})))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/test_repo",
		GitHubToken:   "fake-token",
		Type:          RepoTypeSystem,
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "circleci-jobs-in-required-checks").Check(repo)
	if result.Pass {
		t.Errorf("expected Pass=false, got Detail=%q", result.Detail)
	}
	if result.Err != nil {
		t.Errorf("expected Err=nil, got %v", result.Err)
	}
}

// TestCircleCIJobsInRequiredChecks_NoCIConfig verifies a pass when there is no
// CircleCI config (convention does not apply).
func TestCircleCIJobsInRequiredChecks_NoCIConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/test_repo",
		GitHubToken:   "fake-token",
		Type:          RepoTypeSystem,
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "circleci-jobs-in-required-checks").Check(repo)
	if !result.Pass {
		t.Errorf("expected Pass=true (no CI config), got Detail=%q", result.Detail)
	}
}

// TestCircleCIJobsInRequiredChecks_APIError verifies that an API error on the
// CI config fetch sets Err.
func TestCircleCIJobsInRequiredChecks_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/test_repo",
		GitHubToken:   "fake-token",
		Type:          RepoTypeSystem,
		GitHubBaseURL: server.URL,
	}

	result := findConvention(t, "circleci-jobs-in-required-checks").Check(repo)
	if result.Err == nil {
		t.Errorf("expected Err!=nil for API error, got Pass=%v Detail=%q", result.Pass, result.Detail)
	}
}
