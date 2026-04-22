package conventions

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCircleCIDeploySerialGroup_PassesWithSerialGroup verifies the convention
// passes when the build job has the required serial-group set.
func TestCircleCIDeploySerialGroup_PassesWithSerialGroup(t *testing.T) {
	yaml := `
version: 2.1
orbs:
  lucos: lucos/deploy@0
workflows:
  build:
    jobs:
      - lucos/build-amd64:
          serial-group: << pipeline.project.slug >>/build
      - lucos/deploy-avalon:
          serial-group: deploy-avalon
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
		GitHubBaseURL: server.URL,
	}
	result := findConvention(t, "circleci-deploy-serial-group").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass, got fail: %s", result.Detail)
	}
}

// TestCircleCIDeploySerialGroup_PassesWithNewBuildJob verifies the convention
// passes when using the new parameterised lucos/build job.
func TestCircleCIDeploySerialGroup_PassesWithNewBuildJob(t *testing.T) {
	yaml := `
version: 2.1
orbs:
  lucos: lucos/deploy@0
workflows:
  build:
    jobs:
      - lucos/build:
          serial-group: << pipeline.project.slug >>/build
      - lucos/deploy-avalon:
          serial-group: deploy-avalon
          requires:
            - lucos/build
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
		GitHubBaseURL: server.URL,
	}
	result := findConvention(t, "circleci-deploy-serial-group").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass, got fail: %s", result.Detail)
	}
}

// TestCircleCIDeploySerialGroup_FailsWhenBuildMissing verifies the convention
// fails when the build job has no serial-group.
func TestCircleCIDeploySerialGroup_FailsWhenBuildMissing(t *testing.T) {
	yaml := `
version: 2.1
orbs:
  lucos: lucos/deploy@0
workflows:
  build:
    jobs:
      - lucos/build-amd64
      - lucos/deploy-avalon:
          serial-group: deploy-avalon
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
		GitHubBaseURL: server.URL,
	}
	result := findConvention(t, "circleci-deploy-serial-group").Check(repo)
	if result.Pass {
		t.Errorf("expected fail, got pass: %s", result.Detail)
	}
}

// TestCircleCIDeploySerialGroup_FailsWhenBuildWrongValue verifies the convention
// fails when serial-group is set to a non-standard value.
func TestCircleCIDeploySerialGroup_FailsWhenBuildWrongValue(t *testing.T) {
	yaml := `
version: 2.1
orbs:
  lucos: lucos/deploy@0
workflows:
  build:
    jobs:
      - lucos/build-amd64:
          serial-group: my-custom-group
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
		GitHubBaseURL: server.URL,
	}
	result := findConvention(t, "circleci-deploy-serial-group").Check(repo)
	if result.Pass {
		t.Errorf("expected fail, got pass: %s", result.Detail)
	}
}

// TestCircleCIDeploySerialGroup_FailsWhenDeployMissing verifies the convention
// fails when a deploy job has no serial-group.
func TestCircleCIDeploySerialGroup_FailsWhenDeployMissing(t *testing.T) {
	yaml := `
version: 2.1
orbs:
  lucos: lucos/deploy@0
workflows:
  build:
    jobs:
      - lucos/build:
          serial-group: << pipeline.project.slug >>/build
      - lucos/deploy-avalon:
          requires:
            - lucos/build
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
		GitHubBaseURL: server.URL,
	}
	result := findConvention(t, "circleci-deploy-serial-group").Check(repo)
	if result.Pass {
		t.Errorf("expected fail, got pass: %s", result.Detail)
	}
}

// TestCircleCIDeploySerialGroup_FailsWhenDeployHasOldProjectSlugFormat verifies
// the convention fails when a deploy job uses the old project-slug-prefixed format.
func TestCircleCIDeploySerialGroup_FailsWhenDeployHasOldProjectSlugFormat(t *testing.T) {
	yaml := `
version: 2.1
orbs:
  lucos: lucos/deploy@0
workflows:
  build:
    jobs:
      - lucos/build:
          serial-group: << pipeline.project.slug >>/build
      - lucos/deploy-avalon:
          serial-group: << pipeline.project.slug >>/deploy-avalon
          requires:
            - lucos/build
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
		GitHubBaseURL: server.URL,
	}
	result := findConvention(t, "circleci-deploy-serial-group").Check(repo)
	if result.Pass {
		t.Errorf("expected fail, got pass: %s", result.Detail)
	}
}

// TestCircleCIDeploySerialGroup_PassesWhenNoConfig verifies the convention
// passes gracefully when there is no .circleci/config.yml.
func TestCircleCIDeploySerialGroup_PassesWhenNoConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo := RepoContext{
		Name:          "lucas42/lucos_photos",
		Type:          RepoTypeSystem,
		GitHubBaseURL: server.URL,
	}
	result := findConvention(t, "circleci-deploy-serial-group").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass, got fail: %s", result.Detail)
	}
}

// TestCircleCIDeploySerialGroup_ExcludesDeployOrb verifies the convention
// does not apply to the lucos_deploy_orb repo itself.
func TestCircleCIDeploySerialGroup_ExcludesDeployOrb(t *testing.T) {
	c := findConvention(t, "circleci-deploy-serial-group")
	if c.AppliesToRepo("lucas42/lucos_deploy_orb") {
		t.Error("expected circleci-deploy-serial-group NOT to apply to lucas42/lucos_deploy_orb")
	}
}

// TestCircleCIDeploySerialGroup_PassesForComponent verifies the convention
// also applies to and passes for component repos.
func TestCircleCIDeploySerialGroup_PassesForComponent(t *testing.T) {
	yaml := `
version: 2.1
orbs:
  lucos: lucos/deploy@0
workflows:
  build:
    jobs:
      - lucos/build-amd64:
          serial-group: << pipeline.project.slug >>/build
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
	result := findConvention(t, "circleci-deploy-serial-group").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass, got fail: %s", result.Detail)
	}
}

// TestCircleCIDeploySerialGroup_PassesMultipleDeployHosts verifies the convention
// passes when multiple deploy jobs each have the correct host-specific serial-group.
func TestCircleCIDeploySerialGroup_PassesMultipleDeployHosts(t *testing.T) {
	yaml := `
version: 2.1
orbs:
  lucos: lucos/deploy@0
workflows:
  build:
    jobs:
      - lucos/build:
          serial-group: << pipeline.project.slug >>/build
      - lucos/deploy-avalon:
          serial-group: deploy-avalon
          requires:
            - lucos/build
      - lucos/deploy-xwing:
          serial-group: deploy-xwing
          requires:
            - lucos/build
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
		GitHubBaseURL: server.URL,
	}
	result := findConvention(t, "circleci-deploy-serial-group").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass, got fail: %s", result.Detail)
	}
}
