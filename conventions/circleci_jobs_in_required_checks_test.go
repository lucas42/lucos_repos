package conventions

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
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
			// GitHub prefixes CircleCI check names with "ci/circleci: ".
			// The orb job "lucos/build-amd64" (named via its key) also gets this prefix.
			w.Write([]byte(encodedProtectionWithChecks([]string{"ci/circleci: test", "ci/circleci: lucos/build-amd64"})))
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
			// Only "test" is required (with prefix) — "build" is missing.
			w.Write([]byte(encodedProtectionWithChecks([]string{"ci/circleci: test"})))
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

// TestCircleCIJobsInRequiredChecks_LegacyOrbNamespaceDropped verifies that
// "ci/circleci: build-amd64" (legacy format without orb namespace) is accepted
// as matching the CI config job "lucos/build-amd64".
func TestCircleCIJobsInRequiredChecks_LegacyOrbNamespaceDropped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/lucas42/test_repo/contents/.circleci/config.yml":
			w.Write([]byte(encodedWorkflowConfig()))
		case "/repos/lucas42/test_repo/branches/main/protection":
			// Legacy format: orb namespace dropped, bare job segment only.
			w.Write([]byte(encodedProtectionWithChecks([]string{"ci/circleci: test", "ci/circleci: build-amd64"})))
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
		t.Errorf("expected Pass=true for legacy bare segment format, got Detail=%q", result.Detail)
	}
}

// TestCircleCIJobsInRequiredChecks_DuplicateChecksNoDuplicateDetail verifies that
// when both the legacy contexts and modern checks arrays are populated with the
// same entries, the detail string does not list duplicates.
func TestCircleCIJobsInRequiredChecks_DuplicateChecksNoDuplicateDetail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/lucas42/test_repo/contents/.circleci/config.yml":
			w.Write([]byte(encodedWorkflowConfig()))
		case "/repos/lucas42/test_repo/branches/main/protection":
			// Both contexts and checks arrays populated — simulates the duplicate bug.
			w.Write([]byte(`{"required_status_checks":{"contexts":["ci/circleci: test"],"checks":[{"context":"ci/circleci: test"}]}}`))
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

	// "build" is still missing — but "test" should only appear once in detail.
	result := findConvention(t, "circleci-jobs-in-required-checks").Check(repo)
	if result.Err != nil {
		t.Fatalf("unexpected Err: %v", result.Err)
	}
	// Count occurrences of "ci/circleci: test" in the detail string.
	count := 0
	for i := 0; i < len(result.Detail); i++ {
		if i+len("ci/circleci: test") <= len(result.Detail) &&
			result.Detail[i:i+len("ci/circleci: test")] == "ci/circleci: test" {
			count++
		}
	}
	if count > 1 {
		t.Errorf("expected 'ci/circleci: test' to appear once in detail, appeared %d times: %q", count, result.Detail)
	}
}

// TestCircleCIJobsInRequiredChecks_MissingJobShowsActualFormat verifies that
// when a job is missing from required checks, the Detail includes the actual
// check name format from the commit status API (e.g. "ci/circleci: build-amd64"
// rather than the bare CI config name "lucos/build-amd64").
func TestCircleCIJobsInRequiredChecks_MissingJobShowsActualFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/lucas42/test_repo/contents/.circleci/config.yml":
			w.Write([]byte(encodedWorkflowConfig()))
		case "/repos/lucas42/test_repo/branches/main/protection":
			// Only "test" is required — "build" is missing.
			w.Write([]byte(encodedProtectionWithChecks([]string{"ci/circleci: test"})))
		case "/repos/lucas42/test_repo/commits/heads/main/status":
			// Commit status shows legacy format for both jobs.
			w.Write([]byte(`{"statuses":[{"context":"ci/circleci: test"},{"context":"ci/circleci: build-amd64"}]}`))
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
		t.Fatalf("expected Pass=false, got Pass=true")
	}
	// The detail should suggest the legacy-format name, not the bare CI config name.
	if !strings.Contains(result.Detail, "ci/circleci: build-amd64") {
		t.Errorf("expected Detail to contain legacy format 'ci/circleci: build-amd64', got: %s", result.Detail)
	}
}

// encodedWorkflowConfigWithBranchFilter returns a base64-encoded CircleCI config
// where the build job has a branch filter that ignores main.
func encodedWorkflowConfigWithBranchFilter() string {
	yaml := `version: 2.1
orbs:
  lucos: lucos/deploy@0
workflows:
  build-test-deploy:
    jobs:
      - test
      - lucos/build-amd64:
          name: build
          filters:
            branches:
              ignore: main
      - lucos/deploy-avalon:
          requires:
            - test
`
	encoded := base64.StdEncoding.EncodeToString([]byte(yaml))
	return `{"content":"` + encoded + `","encoding":"base64"}`
}

// encodedWorkflowConfigWithBranchOnlyFilter returns a base64-encoded CircleCI
// config where the build job only runs on specific branches (not main).
func encodedWorkflowConfigWithBranchOnlyFilter() string {
	yaml := `version: 2.1
orbs:
  lucos: lucos/deploy@0
workflows:
  build-test-deploy:
    jobs:
      - test
      - lucos/build-amd64:
          name: build
          filters:
            branches:
              only:
                - develop
                - /feature-.*/
      - lucos/deploy-avalon:
          requires:
            - test
`
	encoded := base64.StdEncoding.EncodeToString([]byte(yaml))
	return `{"content":"` + encoded + `","encoding":"base64"}`
}

// encodedWorkflowConfigAllFilteredFromMain returns a base64-encoded CircleCI
// config where ALL test/build jobs are filtered away from main.
func encodedWorkflowConfigAllFilteredFromMain() string {
	yaml := `version: 2.1
workflows:
  build-test-deploy:
    jobs:
      - test:
          filters:
            branches:
              ignore: main
      - build-android:
          filters:
            branches:
              ignore: main
`
	encoded := base64.StdEncoding.EncodeToString([]byte(yaml))
	return `{"content":"` + encoded + `","encoding":"base64"}`
}

// TestCircleCIJobsInRequiredChecks_BuildFilteredFromMain verifies that a build
// job with branch filters excluding main is not required as a status check.
// This is the core fix for the convention conflict.
func TestCircleCIJobsInRequiredChecks_BuildFilteredFromMain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/lucas42/test_repo/contents/.circleci/config.yml":
			w.Write([]byte(encodedWorkflowConfigWithBranchFilter()))
		case "/repos/lucas42/test_repo/branches/main/protection":
			// Only "test" is required — "build" is filtered from main.
			w.Write([]byte(encodedProtectionWithChecks([]string{"ci/circleci: test"})))
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
		t.Errorf("expected Pass=true when build job is filtered from main, got Detail=%q", result.Detail)
	}
	if !strings.Contains(result.Detail, "do not run on main") {
		t.Errorf("expected Detail to mention skipped jobs, got: %s", result.Detail)
	}
}

// TestCircleCIJobsInRequiredChecks_BuildOnlyFilterExcludesMain verifies that a
// build job with an "only" filter that does not include main is skipped.
func TestCircleCIJobsInRequiredChecks_BuildOnlyFilterExcludesMain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/lucas42/test_repo/contents/.circleci/config.yml":
			w.Write([]byte(encodedWorkflowConfigWithBranchOnlyFilter()))
		case "/repos/lucas42/test_repo/branches/main/protection":
			w.Write([]byte(encodedProtectionWithChecks([]string{"ci/circleci: test"})))
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
		t.Errorf("expected Pass=true when build job only runs on develop/feature, got Detail=%q", result.Detail)
	}
}

// TestCircleCIJobsInRequiredChecks_AllJobsFilteredFromMain verifies a pass when
// all test/build jobs are filtered away from main.
func TestCircleCIJobsInRequiredChecks_AllJobsFilteredFromMain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/lucas42/test_repo/contents/.circleci/config.yml":
			w.Write([]byte(encodedWorkflowConfigAllFilteredFromMain()))
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
		t.Errorf("expected Pass=true when all jobs filtered from main, got Detail=%q", result.Detail)
	}
	if !strings.Contains(result.Detail, "convention does not apply") {
		t.Errorf("expected Detail to say convention does not apply, got: %s", result.Detail)
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
