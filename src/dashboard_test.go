package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"lucos_repos/conventions"
)

// noopSweeper is a test stub that satisfies sweepStatusProvider with a zero timestamp.
type noopSweeper struct{}

func (noopSweeper) Status() (time.Time, error) { return time.Time{}, nil }

// TestBuildDashboardData_Empty verifies the output when no findings exist.
func TestBuildDashboardData_Empty(t *testing.T) {
	report := StatusReport{
		Repos: map[string]RepoStatus{},
		Summary: StatusSummary{
			TotalRepos:      0,
			CompliantRepos:  0,
			TotalViolations: 0,
		},
	}
	data := BuildDashboardData(report)
	if len(data.Conventions) != 0 {
		t.Errorf("expected 0 conventions, got %d", len(data.Conventions))
	}
	if len(data.Repos) != 0 {
		t.Errorf("expected 0 repos, got %d", len(data.Repos))
	}
	if data.CompliancePct != 0 {
		t.Errorf("expected CompliancePct 0, got %d", data.CompliancePct)
	}
}

// TestBuildDashboardData_SortOrder verifies that repos and conventions are sorted alphabetically.
func TestBuildDashboardData_SortOrder(t *testing.T) {
	report := StatusReport{
		Repos: map[string]RepoStatus{
			"lucas42/z_repo": {
				Conventions: map[string]ConventionStatus{
					"conv-b": {Pass: true, Detail: "ok"},
					"conv-a": {Pass: false, Detail: "missing", IssueURL: "https://github.com/lucas42/z_repo/issues/1"},
				},
				Compliant: false,
			},
			"lucas42/a_repo": {
				Conventions: map[string]ConventionStatus{
					"conv-a": {Pass: true, Detail: "ok"},
				},
				Compliant: true,
			},
		},
		Summary: StatusSummary{
			TotalRepos:      2,
			CompliantRepos:  1,
			TotalViolations: 1,
		},
	}
	data := BuildDashboardData(report)

	// Conventions should be sorted: conv-a, conv-b.
	if len(data.Conventions) != 2 {
		t.Fatalf("expected 2 conventions, got %d", len(data.Conventions))
	}
	if data.Conventions[0] != "conv-a" || data.Conventions[1] != "conv-b" {
		t.Errorf("expected conventions [conv-a conv-b], got %v", data.Conventions)
	}

	// Repos should be sorted: a_repo, z_repo.
	if len(data.Repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(data.Repos))
	}
	if data.Repos[0].Name != "lucas42/a_repo" || data.Repos[1].Name != "lucas42/z_repo" {
		t.Errorf("unexpected repo order: %v, %v", data.Repos[0].Name, data.Repos[1].Name)
	}
}

// TestBuildDashboardData_CellMapping verifies cells are correctly mapped per repo.
func TestBuildDashboardData_CellMapping(t *testing.T) {
	report := StatusReport{
		Repos: map[string]RepoStatus{
			"lucas42/repo_a": {
				Conventions: map[string]ConventionStatus{
					"conv-1": {Pass: true, Detail: "ok"},
					"conv-2": {Pass: false, Detail: "missing", IssueURL: "https://github.com/lucas42/repo_a/issues/5"},
				},
				Compliant: false,
			},
		},
		Summary: StatusSummary{TotalRepos: 1},
	}
	data := BuildDashboardData(report)

	if len(data.Repos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(data.Repos))
	}
	row := data.Repos[0]
	if row.Name != "lucas42/repo_a" {
		t.Errorf("unexpected repo name: %q", row.Name)
	}
	if row.Compliant {
		t.Error("expected repo to not be compliant")
	}
	if len(row.Cells) != 2 {
		t.Fatalf("expected 2 cells, got %d", len(row.Cells))
	}

	// Conventions are sorted: conv-1, conv-2.
	cell1 := row.Cells[0]
	if !cell1.Present || !cell1.Pass {
		t.Errorf("cell for conv-1: expected Present=true Pass=true, got Present=%v Pass=%v", cell1.Present, cell1.Pass)
	}
	cell2 := row.Cells[1]
	if !cell2.Present || cell2.Pass {
		t.Errorf("cell for conv-2: expected Present=true Pass=false, got Present=%v Pass=%v", cell2.Present, cell2.Pass)
	}
	if cell2.IssueURL != "https://github.com/lucas42/repo_a/issues/5" {
		t.Errorf("unexpected IssueURL for conv-2: %q", cell2.IssueURL)
	}
}

// TestBuildDashboardData_MissingConvention verifies that a repo missing a convention gets a not-present cell.
func TestBuildDashboardData_MissingConvention(t *testing.T) {
	report := StatusReport{
		Repos: map[string]RepoStatus{
			"lucas42/repo_a": {
				Conventions: map[string]ConventionStatus{
					"conv-1": {Pass: true, Detail: "ok"},
					"conv-2": {Pass: true, Detail: "ok"},
				},
				Compliant: true,
			},
			"lucas42/repo_b": {
				Conventions: map[string]ConventionStatus{
					"conv-1": {Pass: true, Detail: "ok"},
					// conv-2 is absent for repo_b
				},
				Compliant: true,
			},
		},
		Summary: StatusSummary{TotalRepos: 2, CompliantRepos: 2},
	}
	data := BuildDashboardData(report)

	// Find repo_b row.
	var repoBRow *dashboardRepo
	for i := range data.Repos {
		if data.Repos[i].Name == "lucas42/repo_b" {
			repoBRow = &data.Repos[i]
			break
		}
	}
	if repoBRow == nil {
		t.Fatal("expected entry for repo_b")
	}
	// Conventions are sorted: conv-1, conv-2. conv-2 cell should be not-present.
	if len(repoBRow.Cells) != 2 {
		t.Fatalf("expected 2 cells, got %d", len(repoBRow.Cells))
	}
	if !repoBRow.Cells[0].Present {
		t.Error("expected conv-1 cell to be present for repo_b")
	}
	if repoBRow.Cells[1].Present {
		t.Error("expected conv-2 cell to be not-present for repo_b")
	}
}

// TestBuildDashboardData_CompliancePct verifies percentage calculation.
func TestBuildDashboardData_CompliancePct(t *testing.T) {
	tests := []struct {
		total, compliant, wantPct int
	}{
		{0, 0, 0},
		{4, 4, 100},
		{4, 3, 75},
		{3, 1, 33},
	}
	for _, tc := range tests {
		report := StatusReport{
			Repos: map[string]RepoStatus{},
			Summary: StatusSummary{
				TotalRepos:     tc.total,
				CompliantRepos: tc.compliant,
			},
		}
		data := BuildDashboardData(report)
		if data.CompliancePct != tc.wantPct {
			t.Errorf("total=%d compliant=%d: expected CompliancePct %d, got %d",
				tc.total, tc.compliant, tc.wantPct, data.CompliancePct)
		}
	}
}

// TestDashboardHandler_EmptyDB verifies the handler returns 200 HTML for an empty database.
func TestDashboardHandler_EmptyDB(t *testing.T) {
	db := openTestDB(t)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", newDashboardHandler(db, noopSweeper{}))

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	ct := res.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("expected text/html Content-Type, got %q", ct)
	}
	body := w.Body.String()
	if !strings.Contains(body, "lucos-navbar") {
		t.Error("expected page to contain <lucos-navbar>")
	}
	if !strings.Contains(body, "Total Repos") {
		t.Error("expected page to contain summary stats")
	}
}

// TestDashboardHandler_ShowsLastAuditTimestamp verifies the timestamp of the last successful
// audit is displayed on the dashboard.
func TestDashboardHandler_ShowsLastAuditTimestamp(t *testing.T) {
	db := openTestDB(t)

	fixedTime := time.Date(2026, 3, 19, 10, 0, 0, 0, time.UTC)
	sweeper := &fixedTimeSweeper{completedAt: fixedTime}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	newDashboardHandler(db, sweeper)(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "Last Audit") {
		t.Error("expected page to contain 'Last Audit' label")
	}
	if !strings.Contains(body, "19 Mar 2026") {
		t.Errorf("expected page to contain '19 Mar 2026', body excerpt: %q", body[max(0, strings.Index(body, "Last Audit")-50):min(len(body), strings.Index(body, "Last Audit")+200)])
	}
}

// TestDashboardHandler_ShowsNeverWhenNoAuditCompleted verifies that "Never" is shown
// when no audit has completed yet.
func TestDashboardHandler_ShowsNeverWhenNoAuditCompleted(t *testing.T) {
	db := openTestDB(t)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	newDashboardHandler(db, noopSweeper{})(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "Never") {
		t.Error("expected page to contain 'Never' when no audit has completed")
	}
}

// fixedTimeSweeper is a test stub that returns a fixed completion time.
type fixedTimeSweeper struct {
	completedAt time.Time
}

func (s *fixedTimeSweeper) Status() (time.Time, error) { return s.completedAt, nil }

// TestDashboardHandler_WithFindings verifies the handler shows repos, conventions, and repo type.
func TestDashboardHandler_WithFindings(t *testing.T) {
	db := openTestDB(t)

	if err := db.UpsertRepo("lucas42/lucos_test", conventions.RepoTypeSystem, false); err != nil {
		t.Fatalf("UpsertRepo failed: %v", err)
	}
	if err := db.UpsertConvention("has-circleci-config", "Has a CircleCI config"); err != nil {
		t.Fatalf("UpsertConvention failed: %v", err)
	}
	if err := db.SaveFinding(
		conventions.ConventionResult{Convention: "has-circleci-config", Pass: false, Detail: "missing"},
		"lucas42/lucos_test",
		"https://github.com/lucas42/lucos_test/issues/1",
	); err != nil {
		t.Fatalf("SaveFinding failed: %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	newDashboardHandler(db, noopSweeper{})(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	body := w.Body.String()
	if !strings.Contains(body, "lucas42/lucos_test") {
		t.Error("expected page to contain repo name")
	}
	if !strings.Contains(body, "has-circleci-config") {
		t.Error("expected page to contain convention ID")
	}
	if !strings.Contains(body, "https://github.com/lucas42/lucos_test/issues/1") {
		t.Error("expected page to contain issue URL link")
	}
	if !strings.Contains(body, "system") {
		t.Error("expected page to contain repo type 'system'")
	}
}

// TestDashboardHandler_ShowsAppInfraColumn verifies the app/infra column is rendered correctly.
func TestDashboardHandler_ShowsAppInfraColumn(t *testing.T) {
	db := openTestDB(t)

	// app_repo has CodeQL language (app).
	if err := db.UpsertRepo("lucas42/app_repo", conventions.RepoTypeSystem, true); err != nil {
		t.Fatalf("UpsertRepo (app) failed: %v", err)
	}
	// infra_repo does not (infra).
	if err := db.UpsertRepo("lucas42/infra_repo", conventions.RepoTypeSystem, false); err != nil {
		t.Fatalf("UpsertRepo (infra) failed: %v", err)
	}
	if err := db.UpsertConvention("conv-1", "A convention"); err != nil {
		t.Fatalf("UpsertConvention failed: %v", err)
	}
	for _, repo := range []string{"lucas42/app_repo", "lucas42/infra_repo"} {
		if err := db.SaveFinding(conventions.ConventionResult{Convention: "conv-1", Pass: true, Detail: "ok"}, repo, ""); err != nil {
			t.Fatalf("SaveFinding failed: %v", err)
		}
	}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	newDashboardHandler(db, noopSweeper{})(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "Application Code") {
		t.Error("expected page to contain title text 'Application Code' for app repo")
	}
	if !strings.Contains(body, "Infrastructure Only") {
		t.Error("expected page to contain title text 'Infrastructure Only' for infra repo")
	}
	if !strings.Contains(body, ">app<") {
		t.Error("expected page to contain label 'app' for app repo")
	}
	if !strings.Contains(body, ">infra<") {
		t.Error("expected page to contain label 'infra' for infra repo")
	}
}

// TestDashboardHandler_MethodNotAllowed verifies non-GET requests are rejected.
func TestDashboardHandler_MethodNotAllowed(t *testing.T) {
	db := openTestDB(t)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", newDashboardHandler(db, noopSweeper{}))

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Result().StatusCode)
	}
}

// TestWantsJSON verifies Accept header parsing.
func TestWantsJSON(t *testing.T) {
	cases := []struct {
		accept string
		want   bool
	}{
		{"", false},
		{"text/html", false},
		{"text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8", false},
		{"application/json", true},
		{"application/json, text/html", true},
		{"*/*", true},
		{"text/plain, */*", true},
	}
	for _, tc := range cases {
		req := httptest.NewRequest("GET", "/", nil)
		if tc.accept != "" {
			req.Header.Set("Accept", tc.accept)
		}
		got := wantsJSON(req)
		if got != tc.want {
			t.Errorf("Accept=%q: expected wantsJSON=%v, got %v", tc.accept, tc.want, got)
		}
	}
}

// TestBuildJSONResponse verifies the JSON structure produced from DashboardData.
func TestBuildJSONResponse(t *testing.T) {
	data := DashboardData{
		Conventions: []string{"conv-a", "conv-b"},
		Repos: []dashboardRepo{
			{
				Name:      "lucas42/repo_x",
				RepoType:  conventions.RepoTypeSystem,
				Compliant: false,
				Cells: []dashboardCell{
					{Present: true, Pass: true, Detail: "ok"},
					{Present: true, Pass: false, IssueURL: "https://github.com/lucas42/repo_x/issues/7"},
				},
			},
			{
				Name:      "lucas42/repo_y",
				RepoType:  conventions.RepoTypeComponent,
				Compliant: true,
				Cells: []dashboardCell{
					{Present: false},
					{Present: true, Pass: true, Detail: "ok"},
				},
			},
		},
	}

	results := buildJSONResponse(data)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	rx := results[0]
	if rx.Repo != "lucas42/repo_x" {
		t.Errorf("unexpected repo name: %q", rx.Repo)
	}
	if rx.RepoType != "system" {
		t.Errorf("expected repo_type 'system', got %q", rx.RepoType)
	}
	if rx.Checks["conv-a"].Status != "pass" {
		t.Errorf("conv-a: expected pass, got %q", rx.Checks["conv-a"].Status)
	}
	if rx.Checks["conv-a"].Detail != "ok" {
		t.Errorf("conv-a: expected detail 'ok', got %q", rx.Checks["conv-a"].Detail)
	}
	if rx.Checks["conv-b"].Status != "fail" {
		t.Errorf("conv-b: expected fail, got %q", rx.Checks["conv-b"].Status)
	}
	if rx.Checks["conv-b"].Issue != "https://github.com/lucas42/repo_x/issues/7" {
		t.Errorf("conv-b: unexpected issue URL %q", rx.Checks["conv-b"].Issue)
	}

	ry := results[1]
	if ry.RepoType != "component" {
		t.Errorf("expected repo_type 'component', got %q", ry.RepoType)
	}
	if ry.Checks["conv-a"].Status != "na" {
		t.Errorf("conv-a for repo_y: expected na, got %q", ry.Checks["conv-a"].Status)
	}
	if ry.Checks["conv-b"].Status != "pass" {
		t.Errorf("conv-b for repo_y: expected pass, got %q", ry.Checks["conv-b"].Status)
	}
	if ry.Checks["conv-b"].Issue != "" {
		t.Errorf("conv-b for repo_y: expected no issue URL, got %q", ry.Checks["conv-b"].Issue)
	}
}

// TestDashboardHandler_JSON verifies the handler returns JSON when Accept: application/json.
func TestDashboardHandler_JSON(t *testing.T) {
	db := openTestDB(t)

	if err := db.UpsertRepo("lucas42/lucos_test", conventions.RepoTypeSystem, false); err != nil {
		t.Fatalf("UpsertRepo failed: %v", err)
	}
	if err := db.UpsertConvention("has-circleci-config", "Has a CircleCI config"); err != nil {
		t.Fatalf("UpsertConvention failed: %v", err)
	}
	if err := db.SaveFinding(
		conventions.ConventionResult{Convention: "has-circleci-config", Pass: false, Detail: "missing"},
		"lucas42/lucos_test",
		"https://github.com/lucas42/lucos_test/issues/9",
	); err != nil {
		t.Fatalf("SaveFinding failed: %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	newDashboardHandler(db, noopSweeper{})(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	ct := res.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected application/json Content-Type, got %q", ct)
	}
	if vary := res.Header.Get("Vary"); vary != "Accept" {
		t.Errorf("expected Vary: Accept, got %q", vary)
	}

	var results []jsonRepoResult
	if err := json.NewDecoder(w.Body).Decode(&results); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 repo result, got %d", len(results))
	}
	if results[0].Repo != "lucas42/lucos_test" {
		t.Errorf("unexpected repo: %q", results[0].Repo)
	}
	if results[0].RepoType != "system" {
		t.Errorf("expected repo_type 'system', got %q", results[0].RepoType)
	}
	check, ok := results[0].Checks["has-circleci-config"]
	if !ok {
		t.Fatal("expected has-circleci-config check in result")
	}
	if check.Status != "fail" {
		t.Errorf("expected status fail, got %q", check.Status)
	}
	if check.Issue != "https://github.com/lucas42/lucos_test/issues/9" {
		t.Errorf("unexpected issue URL: %q", check.Issue)
	}
}

// TestDashboardHandler_JSONDefault verifies that curl's default Accept (*/*) gets JSON.
func TestDashboardHandler_JSONDefault(t *testing.T) {
	db := openTestDB(t)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept", "*/*")
	w := httptest.NewRecorder()
	newDashboardHandler(db, noopSweeper{})(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	ct := res.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected application/json Content-Type for */* Accept, got %q", ct)
	}
}

// TestDashboardHandler_HTMLExplicit verifies that browser Accept headers still get HTML.
func TestDashboardHandler_HTMLExplicit(t *testing.T) {
	db := openTestDB(t)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	w := httptest.NewRecorder()
	newDashboardHandler(db, noopSweeper{})(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	ct := res.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("expected text/html Content-Type for browser Accept, got %q", ct)
	}
	if vary := res.Header.Get("Vary"); vary != "Accept" {
		t.Errorf("expected Vary: Accept, got %q", vary)
	}
}
