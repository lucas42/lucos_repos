package conventions

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- Registration ---

func TestCopyFromDependabotBlind_Registered(t *testing.T) {
	c := findConvention(t, "dockerfile-copy-from-dependabot-blind")
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

func TestCopyFromDependabotBlind_NoComposeFile(t *testing.T) {
	server := serverWithFiles(map[string][]byte{})
	defer server.Close()

	result := findConvention(t, "dockerfile-copy-from-dependabot-blind").Check(repoCtx(server))
	if !result.Pass {
		t.Errorf("expected pass when no docker-compose.yml, got: %s", result.Detail)
	}
}

// --- No built services ---

func TestCopyFromDependabotBlind_NoBuiltServices(t *testing.T) {
	compose := `
services:
  redis:
    image: redis:7-alpine
`
	server := serverWithFiles(map[string][]byte{
		"/repos/lucas42/test_repo/contents/docker-compose.yml": composeFixture(compose),
	})
	defer server.Close()

	result := findConvention(t, "dockerfile-copy-from-dependabot-blind").Check(repoCtx(server))
	if !result.Pass {
		t.Errorf("expected pass when no built services, got: %s", result.Detail)
	}
}

// --- Happy paths ---

// COPY --from=<named-stage> is fine — Dependabot tracks the FROM instruction.
func TestCopyFromDependabotBlind_PassNamedStage(t *testing.T) {
	dockerfile := `FROM alpine:3.24 AS builder
RUN echo building
FROM alpine:3.24
COPY --from=builder /out /app
`
	server := serverWithFiles(map[string][]byte{
		"/repos/lucas42/test_repo/contents/docker-compose.yml": composeFixture(composeWithBuild),
		"/repos/lucas42/test_repo/contents/Dockerfile":         dockerfileFixture(dockerfile),
	})
	defer server.Close()

	result := findConvention(t, "dockerfile-copy-from-dependabot-blind").Check(repoCtx(server))
	if !result.Pass {
		t.Errorf("expected pass for COPY --from=<named-stage>, got: %s", result.Detail)
	}
}

// COPY --from=0 (numeric stage index) is fine.
func TestCopyFromDependabotBlind_PassNumericIndex(t *testing.T) {
	dockerfile := `FROM alpine:3.24
RUN echo stage0
FROM alpine:3.24
COPY --from=0 /out /app
`
	server := serverWithFiles(map[string][]byte{
		"/repos/lucas42/test_repo/contents/docker-compose.yml": composeFixture(composeWithBuild),
		"/repos/lucas42/test_repo/contents/Dockerfile":         dockerfileFixture(dockerfile),
	})
	defer server.Close()

	result := findConvention(t, "dockerfile-copy-from-dependabot-blind").Check(repoCtx(server))
	if !result.Pass {
		t.Errorf("expected pass for COPY --from=0 (numeric index), got: %s", result.Detail)
	}
}

// COPY --from=<simple-name-without-slash-colon-at> is not an image reference.
func TestCopyFromDependabotBlind_PassSimpleName(t *testing.T) {
	dockerfile := `FROM alpine:3.24
COPY --from=build /out /app
`
	server := serverWithFiles(map[string][]byte{
		"/repos/lucas42/test_repo/contents/docker-compose.yml": composeFixture(composeWithBuild),
		"/repos/lucas42/test_repo/contents/Dockerfile":         dockerfileFixture(dockerfile),
	})
	defer server.Close()

	result := findConvention(t, "dockerfile-copy-from-dependabot-blind").Check(repoCtx(server))
	if !result.Pass {
		t.Errorf("expected pass for COPY --from=<simple-name> (no '/', ':', '@'), got: %s", result.Detail)
	}
}

// COPY --from=config where "config" is bound to a local-path additional_context
// (the lucos_configy false-positive case).
func TestCopyFromDependabotBlind_PassLocalAdditionalContext(t *testing.T) {
	compose := `
services:
  api:
    build:
      context: api
      additional_contexts:
      - config=config
    image: lucas42/test_repo_api:${VERSION:-latest}
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://127.0.0.1:8080/_info"]
`
	dockerfile := `FROM rust:1.96.0-alpine3.22 AS build
COPY . .
RUN cargo build --release

FROM alpine:3.24
ARG VERSION
ENV VERSION=$VERSION
COPY --from=build /usr/src/app/target/release/app /usr/local/bin/app
COPY --from=config . config
CMD ["app"]
`
	server := serverWithFiles(map[string][]byte{
		"/repos/lucas42/test_repo/contents/docker-compose.yml": composeFixture(compose),
		"/repos/lucas42/test_repo/contents/api/Dockerfile":     dockerfileFixture(dockerfile),
	})
	defer server.Close()

	result := findConvention(t, "dockerfile-copy-from-dependabot-blind").Check(repoCtx(server))
	if !result.Pass {
		t.Errorf("expected pass for COPY --from=<local-additional-context>, got: %s", result.Detail)
	}
}

// A test-profile service using a *separate* Dockerfile (not shared with any
// non-test service) does not contribute Dockerfiles to the check.
func TestCopyFromDependabotBlind_TestProfileOnlyServiceNotChecked(t *testing.T) {
	compose := `
services:
  test:
    build:
      context: .
      dockerfile: Dockerfile.test
    profiles:
      - test
`
	// Dockerfile.test has a blind COPY --from — but since it's only used by a
	// test-profile service, no built service references it and it should not
	// be checked.
	testDockerfile := `FROM alpine:3.24
COPY --from=registry.example.com/evil:1.0 /out /app
`
	server := serverWithFiles(map[string][]byte{
		"/repos/lucas42/test_repo/contents/docker-compose.yml": composeFixture(compose),
		"/repos/lucas42/test_repo/contents/Dockerfile.test":    dockerfileFixture(testDockerfile),
	})
	defer server.Close()

	result := findConvention(t, "dockerfile-copy-from-dependabot-blind").Check(repoCtx(server))
	if !result.Pass {
		t.Errorf("expected pass when only test-profile service present, got: %s", result.Detail)
	}
}

// --- Failing cases ---

// COPY --from=<external-image-with-slash> without a named stage → fail.
func TestCopyFromDependabotBlind_FailExternalImageWithSlash(t *testing.T) {
	dockerfile := `FROM alpine:3.24
COPY --from=docker.io/lucas42/lukeblaney_cv:1.0.0 /cv /cv
`
	server := serverWithFiles(map[string][]byte{
		"/repos/lucas42/test_repo/contents/docker-compose.yml": composeFixture(composeWithBuild),
		"/repos/lucas42/test_repo/contents/Dockerfile":         dockerfileFixture(dockerfile),
	})
	defer server.Close()

	result := findConvention(t, "dockerfile-copy-from-dependabot-blind").Check(repoCtx(server))
	if result.Pass {
		t.Errorf("expected fail for COPY --from=<external-image>, got pass")
	}
	if !strings.Contains(result.Detail, "docker.io/lucas42/lukeblaney_cv:1.0.0") {
		t.Errorf("expected detail to name the offending image, got: %s", result.Detail)
	}
	if !strings.Contains(strings.ToLower(result.Detail), "dependabot") {
		t.Errorf("expected detail to mention Dependabot, got: %s", result.Detail)
	}
}

// COPY --from=<image:tag@digest> without a named stage → fail.
func TestCopyFromDependabotBlind_FailExternalImageWithDigest(t *testing.T) {
	dockerfile := `FROM alpine:3.24
ARG VERSION
ENV VERSION=$VERSION
COPY --from=ghcr.io/example/data:1.2.3@sha256:abc123 /data /data
`
	server := serverWithFiles(map[string][]byte{
		"/repos/lucas42/test_repo/contents/docker-compose.yml": composeFixture(composeWithBuild),
		"/repos/lucas42/test_repo/contents/Dockerfile":         dockerfileFixture(dockerfile),
	})
	defer server.Close()

	result := findConvention(t, "dockerfile-copy-from-dependabot-blind").Check(repoCtx(server))
	if result.Pass {
		t.Errorf("expected fail for COPY --from=<image@digest>, got pass")
	}
	if !strings.Contains(result.Detail, "ghcr.io/example/data:1.2.3@sha256:abc123") {
		t.Errorf("expected detail to name the offending image, got: %s", result.Detail)
	}
}

// additional_contexts with docker-image:// is a secondary finding → fail.
func TestCopyFromDependabotBlind_FailDockerImageContext(t *testing.T) {
	compose := `
services:
  api:
    build:
      context: .
      additional_contexts:
      - base=docker-image://alpine:3.14
    image: lucas42/test_repo_api:${VERSION:-latest}
    healthcheck:
      test: ["CMD", "curl", "-sf", "http://127.0.0.1:8080/_info"]
`
	dockerfile := `FROM alpine:3.24
ARG VERSION
ENV VERSION=$VERSION
COPY --from=base /usr/share /usr/share
CMD ["sh"]
`
	server := serverWithFiles(map[string][]byte{
		"/repos/lucas42/test_repo/contents/docker-compose.yml": composeFixture(compose),
		"/repos/lucas42/test_repo/contents/Dockerfile":         dockerfileFixture(dockerfile),
	})
	defer server.Close()

	result := findConvention(t, "dockerfile-copy-from-dependabot-blind").Check(repoCtx(server))
	if result.Pass {
		t.Errorf("expected fail for additional_contexts with docker-image://, got pass")
	}
	if !strings.Contains(result.Detail, "docker-image://") {
		t.Errorf("expected detail to mention docker-image://, got: %s", result.Detail)
	}
}

// FROM ${VAR} (ARG-in-FROM) is a secondary finding → fail.
func TestCopyFromDependabotBlind_FailArgInFrom(t *testing.T) {
	dockerfile := `ARG BASE_IMAGE=alpine:3.24
FROM ${BASE_IMAGE}
ARG VERSION
ENV VERSION=$VERSION
COPY . .
CMD ["sh"]
`
	server := serverWithFiles(map[string][]byte{
		"/repos/lucas42/test_repo/contents/docker-compose.yml": composeFixture(composeWithBuild),
		"/repos/lucas42/test_repo/contents/Dockerfile":         dockerfileFixture(dockerfile),
	})
	defer server.Close()

	result := findConvention(t, "dockerfile-copy-from-dependabot-blind").Check(repoCtx(server))
	if result.Pass {
		t.Errorf("expected fail for FROM ${VAR} (ARG-in-FROM), got pass")
	}
	if !strings.Contains(strings.ToLower(result.Detail), "arg") {
		t.Errorf("expected detail to mention ARG variable substitution, got: %s", result.Detail)
	}
}

// --- Error handling ---

func TestCopyFromDependabotBlind_ComposeAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	result := findConvention(t, "dockerfile-copy-from-dependabot-blind").Check(repoCtx(server))
	if result.Err == nil {
		t.Errorf("expected Err when API returns 500, got nil (Pass=%v Detail=%q)", result.Pass, result.Detail)
	}
}

// --- Helper function unit tests ---

func TestDockerfileNamedStages_Basic(t *testing.T) {
	dockerfile := `FROM alpine:3.24 AS builder
RUN echo build
FROM alpine:3.24 AS runner
COPY --from=builder /out /app
`
	stages := dockerfileNamedStages([]byte(dockerfile))
	if !stages["builder"] {
		t.Error("expected stage 'builder' to be recognised")
	}
	if !stages["runner"] {
		t.Error("expected stage 'runner' to be recognised")
	}
	// Numeric indices should also be present.
	if !stages["0"] {
		t.Error("expected numeric stage '0' to be recognised")
	}
	if !stages["1"] {
		t.Error("expected numeric stage '1' to be recognised")
	}
}

func TestDockerfileNamedStages_CaseInsensitive(t *testing.T) {
	dockerfile := `FROM alpine:3.24 AS Builder
COPY . .
`
	stages := dockerfileNamedStages([]byte(dockerfile))
	if !stages["builder"] {
		t.Error("expected stage name to be lowercased")
	}
}

func TestDockerfileNamedStages_NoNamedStages(t *testing.T) {
	dockerfile := `FROM alpine:3.24
COPY . .
`
	stages := dockerfileNamedStages([]byte(dockerfile))
	// Only numeric index 0.
	if !stages["0"] {
		t.Error("expected numeric stage '0' even without AS clause")
	}
	if stages["builder"] {
		t.Error("should not have 'builder' in unnamed Dockerfile")
	}
}

func TestDockerfileBlindCopyFromImages_ExternalImageFlagged(t *testing.T) {
	content := []byte("FROM alpine:3.24\nCOPY --from=ghcr.io/foo/bar:1.0 /src /dst\n")
	findings := dockerfileBlindCopyFromImages(content, map[string]bool{}, map[string]bool{})
	if len(findings) != 1 || findings[0] != "ghcr.io/foo/bar:1.0" {
		t.Errorf("expected one finding 'ghcr.io/foo/bar:1.0', got %v", findings)
	}
}

func TestDockerfileBlindCopyFromImages_NamedStageSkipped(t *testing.T) {
	content := []byte("FROM alpine:3.24 AS base\nCOPY --from=base /src /dst\n")
	stages := dockerfileNamedStages(content)
	findings := dockerfileBlindCopyFromImages(content, stages, map[string]bool{})
	if len(findings) != 0 {
		t.Errorf("expected no findings for named stage, got %v", findings)
	}
}

func TestDockerfileBlindCopyFromImages_LocalContextSkipped(t *testing.T) {
	content := []byte("FROM alpine:3.24\nCOPY --from=config . config\n")
	localCtxs := map[string]bool{"config": true}
	// "config" has no '/', ':', '@' anyway, but test the explicit guard too.
	findings := dockerfileBlindCopyFromImages(content, map[string]bool{}, localCtxs)
	if len(findings) != 0 {
		t.Errorf("expected no findings for local-context name, got %v", findings)
	}
}

func TestDockerfileBlindCopyFromImages_SimpleNameSkipped(t *testing.T) {
	content := []byte("FROM alpine:3.24\nCOPY --from=build /out /app\n")
	findings := dockerfileBlindCopyFromImages(content, map[string]bool{}, map[string]bool{})
	if len(findings) != 0 {
		t.Errorf("expected no findings for simple name without '/', ':', '@', got %v", findings)
	}
}

func TestDockerfileBlindCopyFromImages_NumericIndexSkipped(t *testing.T) {
	content := []byte("FROM alpine:3.24\nCOPY --from=0 /out /app\n")
	stages := dockerfileNamedStages(content)
	findings := dockerfileBlindCopyFromImages(content, stages, map[string]bool{})
	if len(findings) != 0 {
		t.Errorf("expected no findings for COPY --from=0 (numeric index), got %v", findings)
	}
}

func TestDockerfileBlindCopyFromImages_DeduplicatesMultipleOccurrences(t *testing.T) {
	content := []byte(
		"FROM alpine:3.24\n" +
			"COPY --from=ghcr.io/foo/bar:1.0 /a /a\n" +
			"COPY --from=ghcr.io/foo/bar:1.0 /b /b\n",
	)
	findings := dockerfileBlindCopyFromImages(content, map[string]bool{}, map[string]bool{})
	if len(findings) != 1 {
		t.Errorf("expected deduplicated findings (1), got %v", findings)
	}
}

func TestDockerfileHasArgInFrom_Positive(t *testing.T) {
	cases := []string{
		"FROM ${BASE_IMAGE}\n",
		"FROM $BASE_IMAGE\n",
		"FROM ${BASE:-alpine:3.24}\n",
		"FROM ${BASE_IMAGE} AS stage\n",
	}
	for _, c := range cases {
		if !dockerfileHasArgInFrom([]byte(c)) {
			t.Errorf("expected true for %q", c)
		}
	}
}

func TestDockerfileHasArgInFrom_Negative(t *testing.T) {
	cases := []string{
		"FROM alpine:3.24\n",
		"FROM alpine:3.24 AS builder\n",
		"FROM alpine:3.24@sha256:abc\n",
	}
	for _, c := range cases {
		if dockerfileHasArgInFrom([]byte(c)) {
			t.Errorf("expected false for %q", c)
		}
	}
}

func TestComposeBuildAdditionalContexts_ListForm(t *testing.T) {
	build := map[string]interface{}{
		"context": "api",
		"additional_contexts": []interface{}{
			"config=config",
			"data=./data",
		},
	}
	got := composeBuildAdditionalContexts(build)
	if got["config"] != "config" {
		t.Errorf("expected config→config, got %v", got)
	}
	if got["data"] != "./data" {
		t.Errorf("expected data→./data, got %v", got)
	}
}

func TestComposeBuildAdditionalContexts_MapForm(t *testing.T) {
	build := map[string]interface{}{
		"context": ".",
		"additional_contexts": map[string]interface{}{
			"base": "docker-image://alpine:3.14",
		},
	}
	got := composeBuildAdditionalContexts(build)
	if got["base"] != "docker-image://alpine:3.14" {
		t.Errorf("expected base→docker-image://alpine:3.14, got %v", got)
	}
}

func TestComposeBuildAdditionalContexts_StringBuild(t *testing.T) {
	// Plain string build (e.g. build: ".") has no additional_contexts.
	got := composeBuildAdditionalContexts(".")
	if len(got) != 0 {
		t.Errorf("expected empty map for plain-string build, got %v", got)
	}
}

func TestComposeBuildAdditionalContexts_NoAdditionalContexts(t *testing.T) {
	build := map[string]interface{}{
		"context": ".",
	}
	got := composeBuildAdditionalContexts(build)
	if len(got) != 0 {
		t.Errorf("expected empty map when no additional_contexts key, got %v", got)
	}
}

func TestIsDockerImageContext(t *testing.T) {
	if !isDockerImageContext("docker-image://alpine:3.14") {
		t.Error("expected true for docker-image:// value")
	}
	if isDockerImageContext("./local/path") {
		t.Error("expected false for local path")
	}
	if isDockerImageContext("config") {
		t.Error("expected false for bare name")
	}
}
