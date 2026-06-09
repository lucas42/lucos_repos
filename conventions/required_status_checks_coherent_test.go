package conventions

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// coherentChecksServerOpts configures the test server for
// required-status-checks-coherent tests.
type coherentChecksServerOpts struct {
	requiredChecks          []string       // nil/empty = no branch protection
	headStatusContexts      []string       // status contexts on HEAD of main
	headCheckRunNames       []string       // check run names on HEAD of main
	headParentSHA            string         // SHA of the parent commit of HEAD (empty = parent lookup returns 404)
	headParentStatusContexts []string       // status contexts on HEAD's parent commit
	headParentCheckRunNames  []string       // check run names on HEAD's parent commit
	codeqlWorkflowContent    string         // raw YAML content of codeql-analysis.yml (empty = file not found)
	languages                map[string]int // repo languages (nil = empty map)
	hasDependabotYML         bool           // whether .github/dependabot.yml exists
	dependabotPRSHA         string         // empty = no Dependabot PR found
	dependabotPRChecks      []string       // check runs on the Dependabot PR head
	dependabotBaseSHA       string         // SHA of main when the dep PR was opened (empty = no base SHA)
	dependabotBaseChecks    []string       // check runs on the dep PR base commit
}

// coherentChecksServer builds a test HTTP server that covers all GitHub API
// endpoints used by the required-status-checks-coherent convention.
func coherentChecksServer(t *testing.T, opts coherentChecksServerOpts) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		type checkRun struct {
			Name string `json:"name"`
		}
		type checkRunsResp struct {
			CheckRuns []checkRun `json:"check_runs"`
		}

		switch r.URL.Path {
		case "/repos/lucas42/test_repo/branches/main/protection":
			if len(opts.requiredChecks) > 0 {
				w.WriteHeader(http.StatusOK)
				w.Write(branchProtectionFixture(opts.requiredChecks))
			} else {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"message":"Branch not protected"}`))
			}

		case "/repos/lucas42/test_repo/commits/heads/main":
			// Commit metadata — used for the parent look-back in the stale-check test.
			if opts.headParentSHA != "" {
				type parentRef struct {
					SHA string `json:"sha"`
				}
				type commitResp struct {
					Parents []parentRef `json:"parents"`
				}
				json.NewEncoder(w).Encode(commitResp{
					Parents: []parentRef{{SHA: opts.headParentSHA}},
				})
			} else {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"message":"Not Found"}`))
			}

		case "/repos/lucas42/test_repo/commits/heads/main/status":
			resp := combinedStatusResponse{}
			for _, ctx := range opts.headStatusContexts {
				resp.Statuses = append(resp.Statuses, statusEntry{Context: ctx})
			}
			json.NewEncoder(w).Encode(resp)

		case "/repos/lucas42/test_repo/commits/heads/main/check-runs":
			resp := checkRunsResp{}
			for _, name := range opts.headCheckRunNames {
				resp.CheckRuns = append(resp.CheckRuns, checkRun{Name: name})
			}
			json.NewEncoder(w).Encode(resp)

		case "/repos/lucas42/test_repo/languages":
			langs := opts.languages
			if langs == nil {
				langs = map[string]int{}
			}
			json.NewEncoder(w).Encode(langs)

		case "/repos/lucas42/test_repo/contents/.github/workflows/codeql-analysis.yml":
			if opts.codeqlWorkflowContent != "" {
				encoded := base64.StdEncoding.EncodeToString([]byte(opts.codeqlWorkflowContent))
				json.NewEncoder(w).Encode(map[string]string{
					"content":  encoded,
					"encoding": "base64",
				})
			} else {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"message":"Not Found"}`))
			}

		case "/repos/lucas42/test_repo/contents/.github/dependabot.yml":
			if opts.hasDependabotYML {
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]string{"type": "file"})
			} else {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"message":"Not Found"}`))
			}

		case "/repos/lucas42/test_repo/pulls":
			if opts.dependabotPRSHA != "" {
				type prUser struct {
					Login string `json:"login"`
				}
				type prRef struct {
					SHA string `json:"sha"`
				}
				type pr struct {
					Number int    `json:"number"`
					Head   prRef  `json:"head"`
					Base   prRef  `json:"base"`
					User   prUser `json:"user"`
				}
				json.NewEncoder(w).Encode([]pr{
					{
						Number: 5,
						Head:   prRef{SHA: opts.dependabotPRSHA},
						Base:   prRef{SHA: opts.dependabotBaseSHA},
						User:   prUser{Login: "dependabot[bot]"},
					},
				})
			} else {
				json.NewEncoder(w).Encode([]struct{}{})
			}

		default:
			// Handle HEAD parent commit endpoints (dynamic SHA path).
			if opts.headParentSHA != "" {
				if r.URL.Path == "/repos/lucas42/test_repo/commits/"+opts.headParentSHA+"/check-runs" {
					resp := checkRunsResp{}
					for _, name := range opts.headParentCheckRunNames {
						resp.CheckRuns = append(resp.CheckRuns, checkRun{Name: name})
					}
					json.NewEncoder(w).Encode(resp)
					return
				}
				if r.URL.Path == "/repos/lucas42/test_repo/commits/"+opts.headParentSHA+"/status" {
					resp := combinedStatusResponse{}
					for _, ctx := range opts.headParentStatusContexts {
						resp.Statuses = append(resp.Statuses, statusEntry{Context: ctx})
					}
					json.NewEncoder(w).Encode(resp)
					return
				}
			}
			// Handle Dependabot PR head commit endpoints (dynamic SHA path).
			if opts.dependabotPRSHA != "" {
				if r.URL.Path == "/repos/lucas42/test_repo/commits/"+opts.dependabotPRSHA+"/check-runs" {
					resp := checkRunsResp{}
					for _, name := range opts.dependabotPRChecks {
						resp.CheckRuns = append(resp.CheckRuns, checkRun{Name: name})
					}
					json.NewEncoder(w).Encode(resp)
					return
				}
				if r.URL.Path == "/repos/lucas42/test_repo/commits/"+opts.dependabotPRSHA+"/status" {
					json.NewEncoder(w).Encode(combinedStatusResponse{})
					return
				}
			}
			// Handle Dependabot PR base commit endpoints (dynamic SHA path).
			if opts.dependabotBaseSHA != "" {
				if r.URL.Path == "/repos/lucas42/test_repo/commits/"+opts.dependabotBaseSHA+"/check-runs" {
					resp := checkRunsResp{}
					for _, name := range opts.dependabotBaseChecks {
						resp.CheckRuns = append(resp.CheckRuns, checkRun{Name: name})
					}
					json.NewEncoder(w).Encode(resp)
					return
				}
				if r.URL.Path == "/repos/lucas42/test_repo/commits/"+opts.dependabotBaseSHA+"/status" {
					json.NewEncoder(w).Encode(combinedStatusResponse{})
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestRequiredStatusChecksCoherent_Registered(t *testing.T) {
	c := findConvention(t, "required-status-checks-coherent")
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
	if !c.AppliesToType(RepoTypeComponent) {
		t.Error("should apply to RepoTypeComponent")
	}
	if c.AppliesToType(RepoTypeScript) {
		t.Error("should not apply to RepoTypeScript")
	}
	if !c.ScheduledOnly {
		t.Error("should be ScheduledOnly")
	}
}

func TestRequiredStatusChecksCoherent_NoRequiredChecks(t *testing.T) {
	server := coherentChecksServer(t, coherentChecksServerOpts{})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "required-status-checks-coherent").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when no required checks configured, got: %s", result.Detail)
	}
}

func TestRequiredStatusChecksCoherent_AllCoherent(t *testing.T) {
	// Required: "ci/circleci: test" and "Analyze (go)"
	// HEAD reports both; repo has Go; Dependabot has a PR that reports both.
	server := coherentChecksServer(t, coherentChecksServerOpts{
		requiredChecks:     []string{"ci/circleci: test", "Analyze (go)"},
		headStatusContexts: []string{"ci/circleci: test"},
		headCheckRunNames:  []string{"Analyze (go)"},
		languages:          map[string]int{"Go": 10000},
		hasDependabotYML:   true,
		dependabotPRSHA:    "dep123",
		dependabotPRChecks: []string{"ci/circleci: test", "Analyze (go)"},
	})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "required-status-checks-coherent").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when all checks are coherent, got: %s", result.Detail)
	}
}

func TestRequiredStatusChecksCoherent_StaleCheck(t *testing.T) {
	// "CodeQL" is required but HEAD only reports "Analyze (go)".
	server := coherentChecksServer(t, coherentChecksServerOpts{
		requiredChecks:    []string{"ci/circleci: test", "CodeQL"},
		headCheckRunNames: []string{"ci/circleci: test", "Analyze (go)"},
		languages:         map[string]int{"Go": 10000},
	})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "required-status-checks-coherent").Check(repo)
	if result.Pass {
		t.Errorf("expected fail when stale check is required, got pass: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "CodeQL") {
		t.Errorf("expected Detail to mention stale check name 'CodeQL', got: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "stale") {
		t.Errorf("expected Detail to mention 'stale', got: %s", result.Detail)
	}
}

func TestRequiredStatusChecksCoherent_NoAnalyzeCheck_CodeQLLanguage(t *testing.T) {
	// Repo has Go (CodeQL-supported) but no Analyze (X) in required checks.
	server := coherentChecksServer(t, coherentChecksServerOpts{
		requiredChecks:    []string{"ci/circleci: test"},
		headCheckRunNames: []string{"ci/circleci: test"},
		languages:         map[string]int{"Go": 10000},
	})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "required-status-checks-coherent").Check(repo)
	if result.Pass {
		t.Errorf("expected fail when no CodeQL Analyze check required for CodeQL-enabled repo, got pass: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "Analyze") {
		t.Errorf("expected Detail to mention Analyze, got: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "CodeQL") {
		t.Errorf("expected Detail to mention CodeQL, got: %s", result.Detail)
	}
}

func TestRequiredStatusChecksCoherent_NoCodeQLLanguage_SkipsAnalyzeCheck(t *testing.T) {
	// Repo has only PHP (not CodeQL-supported) — no CodeQL Analyze check needed.
	server := coherentChecksServer(t, coherentChecksServerOpts{
		requiredChecks:    []string{"ci/circleci: test"},
		headCheckRunNames: []string{"ci/circleci: test"},
		languages:         map[string]int{"PHP": 10000},
	})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "required-status-checks-coherent").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when repo has no CodeQL-supported languages, got: %s", result.Detail)
	}
}

func TestRequiredStatusChecksCoherent_NoDependabotYML(t *testing.T) {
	// No .github/dependabot.yml — Dependabot aspect should be skipped entirely.
	server := coherentChecksServer(t, coherentChecksServerOpts{
		requiredChecks:    []string{"ci/circleci: test", "Analyze (go)"},
		headCheckRunNames: []string{"ci/circleci: test", "Analyze (go)"},
		languages:         map[string]int{"Go": 10000},
		hasDependabotYML:  false,
	})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "required-status-checks-coherent").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when no dependabot.yml (Dependabot aspect skipped), got: %s", result.Detail)
	}
}

func TestRequiredStatusChecksCoherent_NoDependabotPRs(t *testing.T) {
	// .github/dependabot.yml exists but no Dependabot PRs to sample — skip
	// the satisfiability check rather than failing.
	server := coherentChecksServer(t, coherentChecksServerOpts{
		requiredChecks:    []string{"ci/circleci: test", "Analyze (go)"},
		headCheckRunNames: []string{"ci/circleci: test", "Analyze (go)"},
		languages:         map[string]int{"Go": 10000},
		hasDependabotYML:  true,
		dependabotPRSHA:   "", // no Dependabot PR
	})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "required-status-checks-coherent").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when no recent Dependabot PRs (can't verify), got: %s", result.Detail)
	}
}

func TestRequiredStatusChecksCoherent_DependabotUnsatisfiable(t *testing.T) {
	// "Analyze (go)" fires on HEAD of main but not on the Dependabot PR.
	// It was also present on the dep PR's base commit (push-triggered CodeQL
	// runs on every main commit), confirming this is a genuine structural issue.
	server := coherentChecksServer(t, coherentChecksServerOpts{
		requiredChecks:       []string{"ci/circleci: test", "Analyze (go)"},
		headCheckRunNames:    []string{"ci/circleci: test", "Analyze (go)"},
		languages:            map[string]int{"Go": 10000},
		hasDependabotYML:     true,
		dependabotPRSHA:      "dep123",
		dependabotPRChecks:   []string{"ci/circleci: test"},
		dependabotBaseSHA:    "base456",
		dependabotBaseChecks: []string{"ci/circleci: test", "Analyze (go)"},
	})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "required-status-checks-coherent").Check(repo)
	if result.Pass {
		t.Errorf("expected fail when required check doesn't fire on Dependabot PR, got pass: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "Analyze (go)") {
		t.Errorf("expected Detail to mention 'Analyze (go)', got: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "Dependabot") {
		t.Errorf("expected Detail to mention 'Dependabot', got: %s", result.Detail)
	}
}

func TestRequiredStatusChecksCoherent_DependabotTimingArtefact(t *testing.T) {
	// A new required check ("ci/circleci: new-job") was added to main after the
	// most recent Dependabot PR was created. The dep PR head doesn't have it,
	// but the dep PR base also doesn't have it — so it's a timing artefact, not
	// a permanent block. The convention should pass rather than flagging a false
	// positive.
	server := coherentChecksServer(t, coherentChecksServerOpts{
		requiredChecks:       []string{"ci/circleci: test", "ci/circleci: new-job", "Analyze (go)"},
		headCheckRunNames:    []string{"ci/circleci: test", "ci/circleci: new-job", "Analyze (go)"},
		languages:            map[string]int{"Go": 10000},
		hasDependabotYML:     true,
		dependabotPRSHA:      "dep123",
		dependabotPRChecks:   []string{"ci/circleci: test", "Analyze (go)"},
		dependabotBaseSHA:    "base456",
		dependabotBaseChecks: []string{"ci/circleci: test", "Analyze (go)"},
		// Note: "ci/circleci: new-job" is absent from both dep head AND dep base —
		// it was added to main after the dep PR was opened.
	})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "required-status-checks-coherent").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass for timing artefact (new check added after dep PR), got: %s", result.Detail)
	}
}

func TestRequiredStatusChecksCoherent_DependabotTimingArtefact_EmptyBase(t *testing.T) {
	// A new required check ("ci/circleci: new-job") is missing from the dep PR,
	// and the dep PR base commit has zero checks (e.g. a repo whose oldest
	// commits predate any recorded CI run). The base SHA is available but empty
	// — this should still be treated as a timing artefact, not a false positive.
	server := coherentChecksServer(t, coherentChecksServerOpts{
		requiredChecks:       []string{"ci/circleci: test", "ci/circleci: new-job"},
		headCheckRunNames:    []string{"ci/circleci: test", "ci/circleci: new-job"},
		hasDependabotYML:     true,
		dependabotPRSHA:      "dep123",
		dependabotPRChecks:   []string{"ci/circleci: test"},
		dependabotBaseSHA:    "base456",
		dependabotBaseChecks: nil, // base SHA present but no checks recorded
	})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "required-status-checks-coherent").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when base SHA is set but has zero checks (timing artefact), got: %s", result.Detail)
	}
}

func TestRequiredStatusChecksCoherent_EmptyHeadChecks_NoStaleFlag(t *testing.T) {
	// HEAD reports no checks at all (e.g. docs-only commit with path filters).
	// The stale check aspect should be skipped rather than flagging everything
	// as stale.
	server := coherentChecksServer(t, coherentChecksServerOpts{
		requiredChecks:     []string{"ci/circleci: test", "Analyze (go)"},
		headStatusContexts: nil,
		headCheckRunNames:  nil,
		languages:          map[string]int{"Go": 10000},
		hasDependabotYML:   true,
		dependabotPRSHA:    "dep123",
		dependabotPRChecks: []string{"ci/circleci: test", "Analyze (go)"},
	})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "required-status-checks-coherent").Check(repo)
	// The stale-check aspect is skipped (empty HEAD), CodeQL is covered,
	// Dependabot is satisfiable — should pass.
	if !result.Pass {
		t.Errorf("expected pass when HEAD reports no checks (docs-only commit), got: %s", result.Detail)
	}
}

func TestGitHubRecentDependabotPRInfoFromBase_SkipsZeroCheckPR(t *testing.T) {
	// Most-recent Dependabot PR (#10) has zero check runs due to a transient
	// glitch. The next one (#9) has correct check runs. The function should
	// skip #10 and return #9's info.
	const zeroCheckSHA = "zero-check-sha"
	const goodCheckSHA = "good-check-sha"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		type checkRun struct {
			Name string `json:"name"`
		}
		type checkRunsResp struct {
			CheckRuns []checkRun `json:"check_runs"`
		}

		switch r.URL.Path {
		case "/repos/lucas42/test_repo/pulls":
			type prRef struct {
				SHA string `json:"sha"`
			}
			type prUser struct {
				Login string `json:"login"`
			}
			type pr struct {
				Number int    `json:"number"`
				Head   prRef  `json:"head"`
				Base   prRef  `json:"base"`
				User   prUser `json:"user"`
			}
			json.NewEncoder(w).Encode([]pr{
				{Number: 10, Head: prRef{SHA: zeroCheckSHA}, User: prUser{Login: "dependabot[bot]"}},
				{Number: 9, Head: prRef{SHA: goodCheckSHA}, User: prUser{Login: "dependabot[bot]"}},
			})
		case "/repos/lucas42/test_repo/commits/" + zeroCheckSHA + "/check-runs":
			json.NewEncoder(w).Encode(checkRunsResp{}) // zero check runs
		case "/repos/lucas42/test_repo/commits/" + zeroCheckSHA + "/status":
			json.NewEncoder(w).Encode(combinedStatusResponse{}) // zero statuses
		case "/repos/lucas42/test_repo/commits/" + goodCheckSHA + "/check-runs":
			json.NewEncoder(w).Encode(checkRunsResp{
				CheckRuns: []checkRun{{Name: "ci/circleci: test"}},
			})
		case "/repos/lucas42/test_repo/commits/" + goodCheckSHA + "/status":
			json.NewEncoder(w).Encode(combinedStatusResponse{})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	info, err := GitHubRecentDependabotPRInfoFromBase(server.URL, "fake-token", "lucas42/test_repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatal("expected non-nil info — function should have fallen through to the second PR")
	}
	found := false
	for _, name := range info.HeadCheckNames {
		if name == "ci/circleci: test" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'ci/circleci: test' from second PR, got head check names: %v", info.HeadCheckNames)
	}
}

func TestRequiredStatusChecksCoherent_MultipleIssues(t *testing.T) {
	// Three issues at once:
	// 1. "CodeQL" is stale (not on HEAD)
	// 2. No Analyze (X) check (so CodeQL coverage missing)
	// 3. "ci/circleci: test" is not on the Dependabot PR
	// The dep PR base confirms "ci/circleci: test" existed when the PR was opened
	// (it's a genuine Dependabot-unsatisfiable check, not a timing artefact).
	server := coherentChecksServer(t, coherentChecksServerOpts{
		requiredChecks:       []string{"CodeQL", "ci/circleci: test"},
		headCheckRunNames:    []string{"Analyze (go)", "ci/circleci: test"},
		languages:            map[string]int{"Go": 10000},
		hasDependabotYML:     true,
		dependabotPRSHA:      "dep123",
		dependabotPRChecks:   []string{"Analyze (go)"},
		dependabotBaseSHA:    "base456",
		dependabotBaseChecks: []string{"ci/circleci: test", "Analyze (go)"},
	})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "required-status-checks-coherent").Check(repo)
	if result.Pass {
		t.Errorf("expected fail with multiple issues, got pass: %s", result.Detail)
	}
	// Stale check
	if !strings.Contains(result.Detail, "CodeQL") {
		t.Errorf("expected Detail to mention stale 'CodeQL' check, got: %s", result.Detail)
	}
	// Missing Analyze (X)
	if !strings.Contains(result.Detail, "Analyze") {
		t.Errorf("expected Detail to mention missing Analyze check, got: %s", result.Detail)
	}
	// Dependabot unsatisfiable
	if !strings.Contains(result.Detail, "Dependabot") {
		t.Errorf("expected Detail to mention Dependabot issue, got: %s", result.Detail)
	}
}

func TestRequiredStatusChecksCoherent_InFlightCheck(t *testing.T) {
	// Reproduces the lucos_arachne false-positive (lucas42/lucos_repos#413):
	// the sweep fires while CI is still in-flight on a freshly-merged HEAD.
	// The fast checks ("ci/circleci: test") have already posted; the slow
	// workflow-rollup check ("ci/circleci: lucos/build") has not appeared yet.
	// Both checks ARE present on the immediately-preceding parent commit.
	// The convention should pass — the missing check is in-flight, not stale.
	// Using Erlang (no CodeQL support) to keep focus on the in-flight scenario
	// and avoid an unrelated CodeQL-coverage finding from Step 3.
	server := coherentChecksServer(t, coherentChecksServerOpts{
		requiredChecks:           []string{"ci/circleci: test", "ci/circleci: lucos/build"},
		headStatusContexts:       []string{"ci/circleci: test"}, // rollup not posted yet
		headCheckRunNames:        nil,
		headParentSHA:            "parent_abc",
		headParentStatusContexts: []string{"ci/circleci: test", "ci/circleci: lucos/build"},
		languages:                map[string]int{"Erlang": 10000}, // not CodeQL-supported
		hasDependabotYML:         false,
	})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "required-status-checks-coherent").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass for in-flight check (present on parent, absent on freshly-merged HEAD), got: %s", result.Detail)
	}
}

func TestRequiredStatusChecksCoherent_StaleCheckAbsentFromParentToo(t *testing.T) {
	// A genuinely stale check ("CodeQL") is absent from both HEAD and its parent
	// commit — the parent look-back should not suppress this finding.
	server := coherentChecksServer(t, coherentChecksServerOpts{
		requiredChecks:          []string{"ci/circleci: test", "CodeQL"},
		headCheckRunNames:       []string{"ci/circleci: test", "Analyze (go)"},
		headParentSHA:           "parent_abc",
		headParentCheckRunNames: []string{"ci/circleci: test", "Analyze (go)"}, // CodeQL absent from parent too
		languages:               map[string]int{"Go": 10000},
		hasDependabotYML:        false,
	})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "required-status-checks-coherent").Check(repo)
	if result.Pass {
		t.Errorf("expected fail when stale check absent from both HEAD and parent, got pass: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "CodeQL") {
		t.Errorf("expected Detail to mention stale check 'CodeQL', got: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "stale") {
		t.Errorf("expected Detail to mention 'stale', got: %s", result.Detail)
	}
}

// codeqlWorkflowYAML returns a minimal codeql-analysis.yml with the given
// explicit language matrix. Used by CodeQL language mismatch tests.
func codeqlWorkflowYAML(languages []string) string {
	langList := `["` + strings.Join(languages, `", "`) + `"]`
	return `on:
  push:
  pull_request:
  schedule:
    - cron: '0 6 * * 1'
permissions: {}
jobs:
  analyze:
    permissions:
      security-events: write
    strategy:
      matrix:
        language: ` + langList + `
    steps:
      - uses: github/codeql-action/init@v4
      - uses: github/codeql-action/analyze@v4
`
}

func TestRequiredStatusChecksCoherent_CodeQLLanguageMismatch(t *testing.T) {
	// Reproduces the lucos_firewall scenario (lucas42/lucos_repos#406): the
	// CodeQL workflow was changed from python to go but branch protection still
	// requires Analyze (python). The new Step 3b should flag the mismatch.
	//
	// The parent commit has Analyze (python), so Step 2's look-back does NOT flag
	// it as stale — only Step 3b fires for the language mismatch.
	server := coherentChecksServer(t, coherentChecksServerOpts{
		requiredChecks:           []string{"ci/circleci: test", "Analyze (python)"},
		headStatusContexts:       []string{"ci/circleci: test"},
		headCheckRunNames:        []string{"Analyze (go)"},
		headParentSHA:            "parent_abc",
		headParentStatusContexts: []string{"ci/circleci: test"},
		headParentCheckRunNames:  []string{"Analyze (python)"}, // was on parent → Step 2 passes
		codeqlWorkflowContent:    codeqlWorkflowYAML([]string{"go"}),
		languages:                map[string]int{"Go": 10000},
		hasDependabotYML:         false,
	})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "required-status-checks-coherent").Check(repo)
	if result.Pass {
		t.Errorf("expected fail for CodeQL language mismatch (Analyze (python) required but workflow uses go), got pass: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "python") {
		t.Errorf("expected Detail to mention the mismatched language 'python', got: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "go") {
		t.Errorf("expected Detail to mention the workflow language 'go', got: %s", result.Detail)
	}
	if !strings.Contains(result.Detail, "Analyze (go)") {
		t.Errorf("expected Detail to suggest Analyze (go), got: %s", result.Detail)
	}
}

func TestRequiredStatusChecksCoherent_CodeQLLanguageMatch(t *testing.T) {
	// Required Analyze (go) and workflow explicitly has go — should pass.
	server := coherentChecksServer(t, coherentChecksServerOpts{
		requiredChecks:        []string{"ci/circleci: test", "Analyze (go)"},
		headStatusContexts:    []string{"ci/circleci: test"},
		headCheckRunNames:     []string{"Analyze (go)"},
		codeqlWorkflowContent: codeqlWorkflowYAML([]string{"go"}),
		languages:             map[string]int{"Go": 10000},
		hasDependabotYML:      false,
	})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "required-status-checks-coherent").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when required Analyze (go) matches workflow language go, got: %s", result.Detail)
	}
}

func TestRequiredStatusChecksCoherent_CodeQLNoWorkflow_NoMismatchFlagged(t *testing.T) {
	// No codeql-analysis.yml present — the language mismatch sub-check should
	// not fire (per acceptance criteria: "a repo with no CodeQL workflow is not
	// flagged"). Both the required check and the language appear in HEAD's CI
	// output so Step 2 passes too.
	server := coherentChecksServer(t, coherentChecksServerOpts{
		requiredChecks:        []string{"ci/circleci: test", "Analyze (python)"},
		headStatusContexts:    []string{"ci/circleci: test"},
		headCheckRunNames:     []string{"Analyze (python)"},
		codeqlWorkflowContent: "", // no workflow → 404
		languages:             map[string]int{"Python": 10000},
		hasDependabotYML:      false,
	})
	defer server.Close()

	repo := RepoContext{Name: "lucas42/test_repo", GitHubToken: "fake-token", GitHubBaseURL: server.URL}
	result := findConvention(t, "required-status-checks-coherent").Check(repo)
	if !result.Pass {
		t.Errorf("expected pass when no codeql-analysis.yml exists (language mismatch check not applicable), got: %s", result.Detail)
	}
}
