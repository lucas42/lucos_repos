package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lucos_repos/conventions"
)

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
	mux.HandleFunc("GET /", newDashboardHandler(db))

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

// TestDashboardHandler_WithFindings verifies the handler shows repos and conventions.
func TestDashboardHandler_WithFindings(t *testing.T) {
	db := openTestDB(t)

	if err := db.UpsertRepo("lucas42/lucos_test"); err != nil {
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
	newDashboardHandler(db)(w, req)

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
}

// TestDashboardHandler_MethodNotAllowed verifies non-GET requests are rejected.
func TestDashboardHandler_MethodNotAllowed(t *testing.T) {
	db := openTestDB(t)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", newDashboardHandler(db))

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Result().StatusCode)
	}
}
