package conventions

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// circleCIResponse builds a fake GitHub Contents API JSON response containing
// the given YAML string as base64-encoded content.
func circleCIResponse(yaml string) []byte {
	encoded := base64.StdEncoding.EncodeToString([]byte(yaml))
	body, _ := json.Marshal(map[string]string{
		"encoding": "base64",
		"content":  encoded,
	})
	return body
}

// TestAll_HasCircleCIConventionsRegistered verifies all five CI conventions are present.
func TestAll_HasCircleCIConventionsRegistered(t *testing.T) {
	ids := []string{
		"circleci-config-exists",
		"circleci-uses-lucos-orb",
		"circleci-has-release-job",
		"circleci-system-deploy-jobs",
		"circleci-no-forbidden-jobs",
	}
	all := All()
	for _, id := range ids {
		id := id
		t.Run(id, func(t *testing.T) {
			found := false
			for _, c := range all {
				if c.ID == id {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("convention %q not found in registry", id)
			}
		})
	}
}

// --- parseCIConfig: version key handling ---

// TestParseCIConfig_HandlesWorkflowsVersionKey verifies that parseCIConfig does
// not error when the workflows block contains the scalar `version: 2` key that
// is present in virtually every real CircleCI config. This was previously broken
// because yaml.v3 cannot unmarshal an integer into a ciWorkflow struct.
func TestParseCIConfig_HandlesWorkflowsVersionKey(t *testing.T) {
	yamlContent := `
version: 2.1
orbs:
  lucos: lucos/deploy@0
workflows:
  version: 2
  build-deploy:
    jobs:
      - lucos/build-amd64
      - lucos/deploy-avalon:
          requires:
            - lucos/build-amd64
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/lucos_photos/contents/docker-compose.yml" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"type":"file"}`))
			return
		}
		if r.URL.Path == "/repos/lucas42/lucos_photos/contents/.circleci/config.yml" {
			w.WriteHeader(http.StatusOK)
			w.Write(circleCIResponse(yamlContent))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_photos",
		Type:          RepoTypeSystem,
		Hosts:         []string{"avalon"},
		GitHubBaseURL: server.URL,
	}

	// All checks should pass — the version key must not cause a parse error.
	for _, id := range []string{
		"circleci-uses-lucos-orb",
		"circleci-system-deploy-jobs",
	} {
		t.Run(id, func(t *testing.T) {
			result := findConvention(t, id).Check(repo)
			if !result.Pass {
				t.Errorf("expected pass with workflows.version: 2 present, got fail: %s", result.Detail)
			}
		})
	}
}

// --- circleci-config-exists ---

// TestCircleCIConfigExists_PassesForSystem verifies the convention passes when
// the file exists for a system repo.
func TestCircleCIConfigExists_PassesForSystem(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/lucos_photos/contents/.circleci/config.yml" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"type":"file"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_photos",
		Type:          RepoTypeSystem,
		GitHubBaseURL: server.URL,
	}
	result := findConvention(t, "circleci-config-exists").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass, got fail: %s", result.Detail)
	}
}

// TestCircleCIConfigExists_FailsForSystemWhenMissing verifies the convention
// fails when the file is absent for a system repo.
func TestCircleCIConfigExists_FailsForSystemWhenMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_photos",
		Type:          RepoTypeSystem,
		GitHubBaseURL: server.URL,
	}
	result := findConvention(t, "circleci-config-exists").Check(repo)
	if result.Pass {
		t.Errorf("expected fail, got pass: %s", result.Detail)
	}
}

// TestCircleCIConfigExists_PassesForComponent verifies the convention passes
// for a component repo with the file present.
func TestCircleCIConfigExists_PassesForComponent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/lucos_navbar/contents/.circleci/config.yml" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"type":"file"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_navbar",
		Type:          RepoTypeComponent,
		GitHubBaseURL: server.URL,
	}
	result := findConvention(t, "circleci-config-exists").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass, got fail: %s", result.Detail)
	}
}

// TestCircleCIConfigExists_DoesNotApplyToScript verifies the convention does
// not apply to script repos.
func TestCircleCIConfigExists_DoesNotApplyToScript(t *testing.T) {
	c := findConvention(t, "circleci-config-exists")
	if c.AppliesToType(RepoTypeScript) {
		t.Error("expected circleci-config-exists NOT to apply to script repos")
	}
}

// TestCircleCIConfigExists_DoesNotApplyToUnconfigured verifies the convention
// does not apply to unconfigured repos.
func TestCircleCIConfigExists_DoesNotApplyToUnconfigured(t *testing.T) {
	c := findConvention(t, "circleci-config-exists")
	if c.AppliesToType(RepoTypeUnconfigured) {
		t.Error("expected circleci-config-exists NOT to apply to unconfigured repos")
	}
}

// TestCircleCIConfigExists_ExcludesGitHubRepo verifies the convention does
// not apply to the lucas42/.github repo, since it is the org-level shared
// workflows repository and does not build or deploy via CircleCI.
func TestCircleCIConfigExists_ExcludesGitHubRepo(t *testing.T) {
	c := findConvention(t, "circleci-config-exists")
	if c.AppliesToRepo("lucas42/.github") {
		t.Error("expected circleci-config-exists NOT to apply to lucas42/.github")
	}
}

// TestCircleCIConfigExists_AppliesToOtherRepos verifies the convention still
// applies to regular repos that are not excluded.
func TestCircleCIConfigExists_AppliesToOtherRepos(t *testing.T) {
	c := findConvention(t, "circleci-config-exists")
	if !c.AppliesToRepo("lucas42/lucos_photos") {
		t.Error("expected circleci-config-exists to apply to lucas42/lucos_photos")
	}
}

// --- circleci-uses-lucos-orb ---

// TestCircleCIUsesLucosOrb_PassesWhenOrbPresent verifies the convention passes
// when the config declares the lucos orb correctly.
func TestCircleCIUsesLucosOrb_PassesWhenOrbPresent(t *testing.T) {
	yaml := `
version: 2.1
orbs:
  lucos: lucos/deploy@0
workflows:
  build:
    jobs:
      - lucos/build-amd64
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/lucos_photos/contents/docker-compose.yml" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"type":"file"}`))
			return
		}
		if r.URL.Path == "/repos/lucas42/lucos_photos/contents/.circleci/config.yml" {
			w.WriteHeader(http.StatusOK)
			w.Write(circleCIResponse(yaml))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_photos",
		Type:          RepoTypeSystem,
		GitHubBaseURL: server.URL,
	}
	result := findConvention(t, "circleci-uses-lucos-orb").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass, got fail: %s", result.Detail)
	}
}

// TestCircleCIUsesLucosOrb_FailsWhenOrbAbsent verifies the convention fails
// when the lucos orb is not declared.
func TestCircleCIUsesLucosOrb_FailsWhenOrbAbsent(t *testing.T) {
	yaml := `
version: 2.1
workflows:
  build:
    jobs:
      - build
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/lucos_photos/contents/docker-compose.yml" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"type":"file"}`))
			return
		}
		if r.URL.Path == "/repos/lucas42/lucos_photos/contents/.circleci/config.yml" {
			w.WriteHeader(http.StatusOK)
			w.Write(circleCIResponse(yaml))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_photos",
		Type:          RepoTypeSystem,
		GitHubBaseURL: server.URL,
	}
	result := findConvention(t, "circleci-uses-lucos-orb").Check(repo)
	if result.Pass {
		t.Errorf("expected fail, got pass: %s", result.Detail)
	}
}

// TestCircleCIUsesLucosOrb_FailsWhenWrongVersion verifies the convention fails
// when the orb version doesn't match exactly.
func TestCircleCIUsesLucosOrb_FailsWhenWrongVersion(t *testing.T) {
	yaml := `
version: 2.1
orbs:
  lucos: lucos/deploy@1
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/lucos_photos/contents/docker-compose.yml" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"type":"file"}`))
			return
		}
		if r.URL.Path == "/repos/lucas42/lucos_photos/contents/.circleci/config.yml" {
			w.WriteHeader(http.StatusOK)
			w.Write(circleCIResponse(yaml))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_photos",
		Type:          RepoTypeSystem,
		GitHubBaseURL: server.URL,
	}
	result := findConvention(t, "circleci-uses-lucos-orb").Check(repo)
	if result.Pass {
		t.Errorf("expected fail for wrong orb version, got pass: %s", result.Detail)
	}
}

// TestCircleCIUsesLucosOrb_ExcludesDeployOrbRepo verifies the convention does
// not apply to lucos_deploy_orb, since that repo defines the orb itself and
// cannot consume it without creating a circular dependency.
func TestCircleCIUsesLucosOrb_ExcludesDeployOrbRepo(t *testing.T) {
	c := findConvention(t, "circleci-uses-lucos-orb")
	if c.AppliesToRepo("lucas42/lucos_deploy_orb") {
		t.Error("expected circleci-uses-lucos-orb NOT to apply to lucas42/lucos_deploy_orb")
	}
}

// TestCircleCIUsesLucosOrb_AppliesToOtherRepos verifies the convention still
// applies to all other repos (e.g. a regular system repo).
func TestCircleCIUsesLucosOrb_AppliesToOtherRepos(t *testing.T) {
	c := findConvention(t, "circleci-uses-lucos-orb")
	if !c.AppliesToRepo("lucas42/lucos_photos") {
		t.Error("expected circleci-uses-lucos-orb to apply to lucas42/lucos_photos")
	}
}

// TestCircleCIUsesLucosOrb_PassesWhenFileAbsent verifies the convention passes
// when the CircleCI config file doesn't exist (that case is handled by circleci-config-exists).
// The repo does have a docker-compose.yml, so the non-Docker exclusion does not apply.
func TestCircleCIUsesLucosOrb_PassesWhenFileAbsent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/lucos_photos/contents/docker-compose.yml" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"type":"file"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_photos",
		Type:          RepoTypeSystem,
		GitHubBaseURL: server.URL,
	}
	result := findConvention(t, "circleci-uses-lucos-orb").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when file absent (circleci-config-exists handles this), got fail: %s", result.Detail)
	}
}

// TestCircleCIUsesLucosOrb_PassesForNonDockerRepo verifies the convention passes
// for a repo that has no docker-compose.yml, since the lucos deploy orb only
// provides Docker-based build and deploy jobs.
func TestCircleCIUsesLucosOrb_PassesForNonDockerRepo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No docker-compose.yml — all requests return 404.
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_photos_android",
		Type:          RepoTypeSystem,
		GitHubBaseURL: server.URL,
	}
	result := findConvention(t, "circleci-uses-lucos-orb").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass for non-Docker repo (no docker-compose.yml), got fail: %s", result.Detail)
	}
}

// --- circleci-has-release-job ---

// TestCircleCIHasReleaseJob_PassesWhenPresent verifies the convention passes
// when at least one lucos/release-* job is present.
func TestCircleCIHasReleaseJob_PassesWhenPresent(t *testing.T) {
	yaml := `
version: 2.1
orbs:
  lucos: lucos/deploy@0
workflows:
  build:
    jobs:
      - lucos/build-amd64
      - lucos/release-npm:
          requires:
            - lucos/build-amd64
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/lucos_navbar/contents/.circleci/config.yml" {
			w.WriteHeader(http.StatusOK)
			w.Write(circleCIResponse(yaml))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_navbar",
		Type:          RepoTypeComponent,
		GitHubBaseURL: server.URL,
	}
	result := findConvention(t, "circleci-has-release-job").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass, got fail: %s", result.Detail)
	}
}

// TestCircleCIHasReleaseJob_FailsWhenAbsent verifies the convention fails when
// no lucos/release-* job is present.
func TestCircleCIHasReleaseJob_FailsWhenAbsent(t *testing.T) {
	yaml := `
version: 2.1
orbs:
  lucos: lucos/deploy@0
workflows:
  build:
    jobs:
      - lucos/build-amd64
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/lucos_navbar/contents/.circleci/config.yml" {
			w.WriteHeader(http.StatusOK)
			w.Write(circleCIResponse(yaml))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_navbar",
		Type:          RepoTypeComponent,
		GitHubBaseURL: server.URL,
	}
	result := findConvention(t, "circleci-has-release-job").Check(repo)
	if result.Pass {
		t.Errorf("expected fail, got pass: %s", result.Detail)
	}
}

// TestCircleCIHasReleaseJob_OnlyAppliesToComponent verifies the convention
// only applies to component repos.
func TestCircleCIHasReleaseJob_OnlyAppliesToComponent(t *testing.T) {
	c := findConvention(t, "circleci-has-release-job")
	if c.AppliesToType(RepoTypeSystem) {
		t.Error("expected circleci-has-release-job NOT to apply to system repos")
	}
	if !c.AppliesToType(RepoTypeComponent) {
		t.Error("expected circleci-has-release-job to apply to component repos")
	}
	if c.AppliesToType(RepoTypeScript) {
		t.Error("expected circleci-has-release-job NOT to apply to script repos")
	}
}

// --- circleci-system-deploy-jobs ---

// TestCircleCISystemDeployJobs_PassesWhenJobsMatchHosts verifies the convention
// passes when deploy jobs exactly match configured hosts.
func TestCircleCISystemDeployJobs_PassesWhenJobsMatchHosts(t *testing.T) {
	yaml := `
version: 2.1
orbs:
  lucos: lucos/deploy@0
workflows:
  build-deploy:
    jobs:
      - lucos/build-amd64
      - lucos/deploy-avalon:
          requires:
            - lucos/build-amd64
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/lucos_photos/contents/.circleci/config.yml" {
			w.WriteHeader(http.StatusOK)
			w.Write(circleCIResponse(yaml))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_photos",
		Type:          RepoTypeSystem,
		Hosts:         []string{"avalon"},
		GitHubBaseURL: server.URL,
	}
	result := findConvention(t, "circleci-system-deploy-jobs").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass, got fail: %s", result.Detail)
	}
}

// TestCircleCISystemDeployJobs_PassesForMultipleHosts verifies the convention
// passes when deploy jobs exactly match multiple configured hosts.
func TestCircleCISystemDeployJobs_PassesForMultipleHosts(t *testing.T) {
	yaml := `
version: 2.1
orbs:
  lucos: lucos/deploy@0
workflows:
  build-deploy:
    jobs:
      - lucos/build-amd64
      - lucos/deploy-xwing:
          requires:
            - lucos/build-amd64
      - lucos/deploy-salvare:
          requires:
            - lucos/build-amd64
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/lucos_media_linuxplayer/contents/.circleci/config.yml" {
			w.WriteHeader(http.StatusOK)
			w.Write(circleCIResponse(yaml))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_media_linuxplayer",
		Type:          RepoTypeSystem,
		Hosts:         []string{"xwing", "salvare"},
		GitHubBaseURL: server.URL,
	}
	result := findConvention(t, "circleci-system-deploy-jobs").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass, got fail: %s", result.Detail)
	}
}

// TestCircleCISystemDeployJobs_FailsWhenMissingDeployJob verifies the convention
// fails when a configured host has no corresponding deploy job.
func TestCircleCISystemDeployJobs_FailsWhenMissingDeployJob(t *testing.T) {
	yaml := `
version: 2.1
orbs:
  lucos: lucos/deploy@0
workflows:
  build-deploy:
    jobs:
      - lucos/build-amd64
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/lucos_photos/contents/.circleci/config.yml" {
			w.WriteHeader(http.StatusOK)
			w.Write(circleCIResponse(yaml))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_photos",
		Type:          RepoTypeSystem,
		Hosts:         []string{"avalon"},
		GitHubBaseURL: server.URL,
	}
	result := findConvention(t, "circleci-system-deploy-jobs").Check(repo)
	if result.Pass {
		t.Errorf("expected fail when deploy job missing, got pass: %s", result.Detail)
	}
}

// TestCircleCISystemDeployJobs_FailsWhenExtraDeployJob verifies the convention
// fails when the config has a deploy job for a host not in configy.
func TestCircleCISystemDeployJobs_FailsWhenExtraDeployJob(t *testing.T) {
	yaml := `
version: 2.1
orbs:
  lucos: lucos/deploy@0
workflows:
  build-deploy:
    jobs:
      - lucos/build-amd64
      - lucos/deploy-avalon:
          requires:
            - lucos/build-amd64
      - lucos/deploy-xwing:
          requires:
            - lucos/build-amd64
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/lucos_photos/contents/.circleci/config.yml" {
			w.WriteHeader(http.StatusOK)
			w.Write(circleCIResponse(yaml))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_photos",
		Type:          RepoTypeSystem,
		Hosts:         []string{"avalon"}, // xwing not in hosts
		GitHubBaseURL: server.URL,
	}
	result := findConvention(t, "circleci-system-deploy-jobs").Check(repo)
	if result.Pass {
		t.Errorf("expected fail when extra deploy job present, got pass: %s", result.Detail)
	}
}

// TestCircleCISystemDeployJobs_PassesWithNoHosts verifies the convention passes
// for a system with no configured hosts when no deploy jobs are present.
func TestCircleCISystemDeployJobs_PassesWithNoHosts(t *testing.T) {
	yaml := `
version: 2.1
orbs:
  lucos: lucos/deploy@0
workflows:
  build:
    jobs:
      - lucos/build-amd64
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/lucos_deploy_orb/contents/.circleci/config.yml" {
			w.WriteHeader(http.StatusOK)
			w.Write(circleCIResponse(yaml))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_deploy_orb",
		Type:          RepoTypeSystem,
		Hosts:         []string{}, // no hosts
		GitHubBaseURL: server.URL,
	}
	result := findConvention(t, "circleci-system-deploy-jobs").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass for system with no hosts and no deploy jobs, got fail: %s", result.Detail)
	}
}

// TestCircleCISystemDeployJobs_PassesWhenFileAbsent verifies the convention
// passes when no config file exists (handled by circleci-config-exists).
func TestCircleCISystemDeployJobs_PassesWhenFileAbsent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_photos",
		Type:          RepoTypeSystem,
		Hosts:         []string{"avalon"},
		GitHubBaseURL: server.URL,
	}
	result := findConvention(t, "circleci-system-deploy-jobs").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when file absent, got fail: %s", result.Detail)
	}
}

// TestCircleCISystemDeployJobs_OnlyAppliesToSystem verifies the convention only
// applies to system repos.
func TestCircleCISystemDeployJobs_OnlyAppliesToSystem(t *testing.T) {
	c := findConvention(t, "circleci-system-deploy-jobs")
	if !c.AppliesToType(RepoTypeSystem) {
		t.Error("expected circleci-system-deploy-jobs to apply to system repos")
	}
	if c.AppliesToType(RepoTypeComponent) {
		t.Error("expected circleci-system-deploy-jobs NOT to apply to component repos")
	}
}

// --- circleci-no-forbidden-jobs ---

// TestCircleCINoForbiddenJobs_PassesForUnconfigured verifies the convention
// passes trivially for unconfigured repos.
func TestCircleCINoForbiddenJobs_PassesForUnconfigured(t *testing.T) {
	repo := RepoContext{Name: "lucas42/lucos_new", Type: RepoTypeUnconfigured}
	result := findConvention(t, "circleci-no-forbidden-jobs").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass for unconfigured repo, got fail: %s", result.Detail)
	}
}

// TestCircleCINoForbiddenJobs_PassesForSystem verifies the convention passes
// trivially for system repos (they have their own targeted checks).
func TestCircleCINoForbiddenJobs_PassesForSystem(t *testing.T) {
	repo := RepoContext{Name: "lucas42/lucos_photos", Type: RepoTypeSystem}
	result := findConvention(t, "circleci-no-forbidden-jobs").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass for system repo (not targeted by this check), got fail: %s", result.Detail)
	}
}

// TestCircleCINoForbiddenJobs_PassesForComponent verifies the convention passes
// trivially for component repos.
func TestCircleCINoForbiddenJobs_PassesForComponent(t *testing.T) {
	repo := RepoContext{Name: "lucas42/lucos_navbar", Type: RepoTypeComponent}
	result := findConvention(t, "circleci-no-forbidden-jobs").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass for component repo (not targeted by this check), got fail: %s", result.Detail)
	}
}

// TestCircleCINoForbiddenJobs_PassesForScriptWithNoCIConfig verifies the
// convention passes when a script repo has no CircleCI config file.
func TestCircleCINoForbiddenJobs_PassesForScriptWithNoCIConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_agent",
		Type:          RepoTypeScript,
		GitHubBaseURL: server.URL,
	}
	result := findConvention(t, "circleci-no-forbidden-jobs").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass for script with no config file, got fail: %s", result.Detail)
	}
}

// TestCircleCINoForbiddenJobs_PassesForScriptWithCleanCIConfig verifies the
// convention passes for a script repo whose CI config has no forbidden jobs.
func TestCircleCINoForbiddenJobs_PassesForScriptWithCleanCIConfig(t *testing.T) {
	yaml := `
version: 2.1
orbs:
  lucos: lucos/deploy@0
workflows:
  build:
    jobs:
      - lucos/build-amd64
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/lucos_agent/contents/.circleci/config.yml" {
			w.WriteHeader(http.StatusOK)
			w.Write(circleCIResponse(yaml))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_agent",
		Type:          RepoTypeScript,
		GitHubBaseURL: server.URL,
	}
	result := findConvention(t, "circleci-no-forbidden-jobs").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass for script with no forbidden jobs, got fail: %s", result.Detail)
	}
}

// TestCircleCINoForbiddenJobs_FailsForScriptWithDeployJob verifies the convention
// fails for a script repo that has a lucos/deploy-* job.
func TestCircleCINoForbiddenJobs_FailsForScriptWithDeployJob(t *testing.T) {
	yaml := `
version: 2.1
orbs:
  lucos: lucos/deploy@0
workflows:
  build:
    jobs:
      - lucos/build-amd64
      - lucos/deploy-avalon:
          requires:
            - lucos/build-amd64
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/lucos_agent/contents/.circleci/config.yml" {
			w.WriteHeader(http.StatusOK)
			w.Write(circleCIResponse(yaml))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_agent",
		Type:          RepoTypeScript,
		GitHubBaseURL: server.URL,
	}
	result := findConvention(t, "circleci-no-forbidden-jobs").Check(repo)
	if result.Pass {
		t.Errorf("expected fail for script with deploy job, got pass: %s", result.Detail)
	}
}

// TestCircleCINoForbiddenJobs_FailsForScriptWithReleaseJob verifies the convention
// fails for a script repo that has a lucos/release-* job.
func TestCircleCINoForbiddenJobs_FailsForScriptWithReleaseJob(t *testing.T) {
	yaml := `
version: 2.1
orbs:
  lucos: lucos/deploy@0
workflows:
  build:
    jobs:
      - lucos/build-amd64
      - lucos/release-npm:
          requires:
            - lucos/build-amd64
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/lucos_agent/contents/.circleci/config.yml" {
			w.WriteHeader(http.StatusOK)
			w.Write(circleCIResponse(yaml))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_agent",
		Type:          RepoTypeScript,
		GitHubBaseURL: server.URL,
	}
	result := findConvention(t, "circleci-no-forbidden-jobs").Check(repo)
	if result.Pass {
		t.Errorf("expected fail for script with release job, got pass: %s", result.Detail)
	}
}

// TestCircleCINoForbiddenJobs_FailsForDuplicateWithDeployJob verifies the
// convention fails for a duplicate-typed repo with a deploy job.
func TestCircleCINoForbiddenJobs_FailsForDuplicateWithDeployJob(t *testing.T) {
	yaml := `
version: 2.1
orbs:
  lucos: lucos/deploy@0
workflows:
  build:
    jobs:
      - lucos/deploy-avalon
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/lucos_shared/contents/.circleci/config.yml" {
			w.WriteHeader(http.StatusOK)
			w.Write(circleCIResponse(yaml))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_shared",
		Type:          RepoTypeDuplicate,
		GitHubBaseURL: server.URL,
	}
	result := findConvention(t, "circleci-no-forbidden-jobs").Check(repo)
	if result.Pass {
		t.Errorf("expected fail for duplicate repo with deploy job, got pass: %s", result.Detail)
	}
}

