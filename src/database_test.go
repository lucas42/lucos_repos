package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"lucos_repos/conventions"
)

// openTestDB opens an in-memory SQLite database for testing.
func openTestDB(t *testing.T) *DB {
	t.Helper()
	// Use a temp file to avoid sharing state between tests.
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestOpenDB_CreatesSchema verifies that OpenDB creates the required tables.
func TestOpenDB_CreatesSchema(t *testing.T) {
	db := openTestDB(t)

	// Verify all three tables exist by inserting a row into each.
	if _, err := db.conn.Exec(`INSERT INTO repos (name, last_audited, repo_type) VALUES (?, ?, ?)`, "test/repo", time.Now(), "system"); err != nil {
		t.Errorf("repos table not created properly: %v", err)
	}
	if _, err := db.conn.Exec(`INSERT INTO conventions (id, description) VALUES (?, ?)`, "test-convention", "A test convention"); err != nil {
		t.Errorf("conventions table not created properly: %v", err)
	}
	if _, err := db.conn.Exec(`INSERT INTO findings (repo, convention, pass, detail, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"test/repo", "test-convention", 1, "passed", time.Now()); err != nil {
		t.Errorf("findings table not created properly: %v", err)
	}
}

// TestOpenDB_Idempotent verifies that calling OpenDB twice on the same path does not fail
// (schema creation uses IF NOT EXISTS).
func TestOpenDB_Idempotent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db1, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("first OpenDB failed: %v", err)
	}
	db1.Close()

	db2, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("second OpenDB failed: %v", err)
	}
	db2.Close()
}

// TestPing_Open verifies that Ping succeeds on an open database.
func TestPing_Open(t *testing.T) {
	db := openTestDB(t)
	if err := db.Ping(); err != nil {
		t.Errorf("Ping on open database returned error: %v", err)
	}
}

// TestPing_Closed verifies that Ping fails after the database is closed.
func TestPing_Closed(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(dir + "/test.db")
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	db.Close()
	if err := db.Ping(); err == nil {
		t.Error("expected Ping to fail after Close, got nil")
	}
}

// TestUpsertConvention_Insert verifies inserting a new convention.
func TestUpsertConvention_Insert(t *testing.T) {
	db := openTestDB(t)

	if err := db.UpsertConvention("my-convention", "My description"); err != nil {
		t.Fatalf("UpsertConvention failed: %v", err)
	}

	var desc string
	if err := db.conn.QueryRow(`SELECT description FROM conventions WHERE id = ?`, "my-convention").Scan(&desc); err != nil {
		t.Fatalf("failed to query convention: %v", err)
	}
	if desc != "My description" {
		t.Errorf("expected description 'My description', got %q", desc)
	}
}

// TestUpsertConvention_UpdatesDescription verifies that upserting updates the description.
func TestUpsertConvention_UpdatesDescription(t *testing.T) {
	db := openTestDB(t)

	if err := db.UpsertConvention("my-convention", "Old description"); err != nil {
		t.Fatalf("initial UpsertConvention failed: %v", err)
	}
	if err := db.UpsertConvention("my-convention", "New description"); err != nil {
		t.Fatalf("update UpsertConvention failed: %v", err)
	}

	var desc string
	if err := db.conn.QueryRow(`SELECT description FROM conventions WHERE id = ?`, "my-convention").Scan(&desc); err != nil {
		t.Fatalf("failed to query convention: %v", err)
	}
	if desc != "New description" {
		t.Errorf("expected 'New description', got %q", desc)
	}
}

// TestUpsertRepo verifies that a repo can be inserted and has a last_audited timestamp.
func TestUpsertRepo(t *testing.T) {
	db := openTestDB(t)

	before := time.Now()
	if err := db.UpsertRepo("lucas42/test_repo", conventions.RepoTypeUnconfigured, false); err != nil {
		t.Fatalf("UpsertRepo failed: %v", err)
	}
	after := time.Now()

	var lastAuditedStr string
	if err := db.conn.QueryRow(`SELECT last_audited FROM repos WHERE name = ?`, "lucas42/test_repo").Scan(&lastAuditedStr); err != nil {
		t.Fatalf("failed to query repo: %v", err)
	}

	// Verify last_audited is set to approximately now.
	lastAudited, err := time.Parse(time.RFC3339Nano, lastAuditedStr)
	if err != nil {
		// Try alternative format.
		lastAudited, err = time.Parse("2006-01-02 15:04:05.999999999-07:00", lastAuditedStr)
		if err != nil {
			t.Fatalf("failed to parse last_audited %q: %v", lastAuditedStr, err)
		}
	}
	if lastAudited.Before(before) || lastAudited.After(after) {
		t.Errorf("last_audited %v not between %v and %v", lastAudited, before, after)
	}
}

// TestUpsertRepo_StoresType verifies that repo_type is persisted and updated.
func TestUpsertRepo_StoresType(t *testing.T) {
	db := openTestDB(t)

	if err := db.UpsertRepo("lucas42/test_repo", conventions.RepoTypeSystem, false); err != nil {
		t.Fatalf("UpsertRepo (system) failed: %v", err)
	}

	var repoType string
	if err := db.conn.QueryRow(`SELECT repo_type FROM repos WHERE name = ?`, "lucas42/test_repo").Scan(&repoType); err != nil {
		t.Fatalf("failed to query repo_type: %v", err)
	}
	if repoType != "system" {
		t.Errorf("expected repo_type 'system', got %q", repoType)
	}

	// Update the type.
	if err := db.UpsertRepo("lucas42/test_repo", conventions.RepoTypeComponent, false); err != nil {
		t.Fatalf("UpsertRepo (component) failed: %v", err)
	}
	if err := db.conn.QueryRow(`SELECT repo_type FROM repos WHERE name = ?`, "lucas42/test_repo").Scan(&repoType); err != nil {
		t.Fatalf("failed to query repo_type after update: %v", err)
	}
	if repoType != "component" {
		t.Errorf("expected repo_type 'component' after update, got %q", repoType)
	}
}

// TestUpsertRepo_StoresHasCodeQLLanguage verifies that has_codeql_language is persisted and updated.
func TestUpsertRepo_StoresHasCodeQLLanguage(t *testing.T) {
	db := openTestDB(t)

	if err := db.UpsertRepo("lucas42/test_repo", conventions.RepoTypeSystem, true); err != nil {
		t.Fatalf("UpsertRepo (app) failed: %v", err)
	}

	var hasCodeQL int
	if err := db.conn.QueryRow(`SELECT has_codeql_language FROM repos WHERE name = ?`, "lucas42/test_repo").Scan(&hasCodeQL); err != nil {
		t.Fatalf("failed to query has_codeql_language: %v", err)
	}
	if hasCodeQL != 1 {
		t.Errorf("expected has_codeql_language 1, got %d", hasCodeQL)
	}

	// Update to infra.
	if err := db.UpsertRepo("lucas42/test_repo", conventions.RepoTypeSystem, false); err != nil {
		t.Fatalf("UpsertRepo (infra) failed: %v", err)
	}
	if err := db.conn.QueryRow(`SELECT has_codeql_language FROM repos WHERE name = ?`, "lucas42/test_repo").Scan(&hasCodeQL); err != nil {
		t.Fatalf("failed to query has_codeql_language after update: %v", err)
	}
	if hasCodeQL != 0 {
		t.Errorf("expected has_codeql_language 0 after update, got %d", hasCodeQL)
	}
}

// TestGetStatusReport_IncludesHasCodeQLLanguage verifies that the has_codeql_language flag is propagated into the status report.
func TestGetStatusReport_IncludesHasCodeQLLanguage(t *testing.T) {
	db := openTestDB(t)

	if err := db.UpsertRepo("lucas42/app_repo", conventions.RepoTypeSystem, true); err != nil {
		t.Fatalf("UpsertRepo (app) failed: %v", err)
	}
	if err := db.UpsertRepo("lucas42/infra_repo", conventions.RepoTypeSystem, false); err != nil {
		t.Fatalf("UpsertRepo (infra) failed: %v", err)
	}
	if err := db.UpsertConvention("conv-1", "A convention"); err != nil {
		t.Fatalf("UpsertConvention failed: %v", err)
	}
	if err := db.SaveFinding(conventions.ConventionResult{Convention: "conv-1", Pass: true, Detail: "ok"}, "lucas42/app_repo", ""); err != nil {
		t.Fatalf("SaveFinding failed: %v", err)
	}
	if err := db.SaveFinding(conventions.ConventionResult{Convention: "conv-1", Pass: true, Detail: "ok"}, "lucas42/infra_repo", ""); err != nil {
		t.Fatalf("SaveFinding failed: %v", err)
	}

	report, err := db.GetStatusReport()
	if err != nil {
		t.Fatalf("GetStatusReport failed: %v", err)
	}

	appRepo, ok := report.Repos["lucas42/app_repo"]
	if !ok {
		t.Fatal("expected entry for 'lucas42/app_repo' in report")
	}
	if !appRepo.HasCodeQLLanguage {
		t.Error("expected HasCodeQLLanguage=true for app_repo")
	}

	infraRepo, ok := report.Repos["lucas42/infra_repo"]
	if !ok {
		t.Fatal("expected entry for 'lucas42/infra_repo' in report")
	}
	if infraRepo.HasCodeQLLanguage {
		t.Error("expected HasCodeQLLanguage=false for infra_repo")
	}
}

// TestGetStatusReport_IncludesRepoType verifies that the type is propagated into the status report.
func TestGetStatusReport_IncludesRepoType(t *testing.T) {
	db := openTestDB(t)

	if err := db.UpsertRepo("lucas42/repo_a", conventions.RepoTypeSystem, false); err != nil {
		t.Fatalf("UpsertRepo failed: %v", err)
	}
	if err := db.UpsertConvention("conv-1", "A convention"); err != nil {
		t.Fatalf("UpsertConvention failed: %v", err)
	}
	if err := db.SaveFinding(conventions.ConventionResult{Convention: "conv-1", Pass: true, Detail: "ok"}, "lucas42/repo_a", ""); err != nil {
		t.Fatalf("SaveFinding failed: %v", err)
	}

	report, err := db.GetStatusReport()
	if err != nil {
		t.Fatalf("GetStatusReport failed: %v", err)
	}

	rs, ok := report.Repos["lucas42/repo_a"]
	if !ok {
		t.Fatal("expected entry for 'lucas42/repo_a' in report")
	}
	if rs.Type != conventions.RepoTypeSystem {
		t.Errorf("expected type 'system', got %q", rs.Type)
	}
}

// TestSaveFinding_Pass verifies that a passing finding is stored correctly.
func TestSaveFinding_Pass(t *testing.T) {
	db := openTestDB(t)

	// Set up prerequisite rows.
	if err := db.UpsertRepo("lucas42/test_repo", conventions.RepoTypeUnconfigured, false); err != nil {
		t.Fatalf("UpsertRepo failed: %v", err)
	}
	if err := db.UpsertConvention("test-convention", "A test"); err != nil {
		t.Fatalf("UpsertConvention failed: %v", err)
	}

	result := conventions.ConventionResult{
		Convention: "test-convention",
		Pass:       true,
		Detail:     "file found",
	}
	if err := db.SaveFinding(result, "lucas42/test_repo", ""); err != nil {
		t.Fatalf("SaveFinding failed: %v", err)
	}

	findings, err := db.GetFindings()
	if err != nil {
		t.Fatalf("GetFindings failed: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	f := findings[0]
	if f.Repo != "lucas42/test_repo" {
		t.Errorf("expected repo 'lucas42/test_repo', got %q", f.Repo)
	}
	if f.Convention != "test-convention" {
		t.Errorf("expected convention 'test-convention', got %q", f.Convention)
	}
	if !f.Pass {
		t.Error("expected Pass to be true")
	}
	if f.Detail != "file found" {
		t.Errorf("expected detail 'file found', got %q", f.Detail)
	}
	if f.IssueURL != "" {
		t.Errorf("expected empty IssueURL, got %q", f.IssueURL)
	}
}

// TestSaveFinding_Fail verifies that a failing finding with an issue URL is stored correctly.
func TestSaveFinding_Fail(t *testing.T) {
	db := openTestDB(t)

	if err := db.UpsertRepo("lucas42/test_repo", conventions.RepoTypeUnconfigured, false); err != nil {
		t.Fatalf("UpsertRepo failed: %v", err)
	}
	if err := db.UpsertConvention("test-convention", "A test"); err != nil {
		t.Fatalf("UpsertConvention failed: %v", err)
	}

	result := conventions.ConventionResult{
		Convention: "test-convention",
		Pass:       false,
		Detail:     "file missing",
	}
	issueURL := "https://github.com/lucas42/test_repo/issues/99"
	if err := db.SaveFinding(result, "lucas42/test_repo", issueURL); err != nil {
		t.Fatalf("SaveFinding failed: %v", err)
	}

	findings, err := db.GetFindings()
	if err != nil {
		t.Fatalf("GetFindings failed: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	f := findings[0]
	if f.Pass {
		t.Error("expected Pass to be false")
	}
	if f.IssueURL != issueURL {
		t.Errorf("expected IssueURL %q, got %q", issueURL, f.IssueURL)
	}
}

// TestSaveFinding_Upsert verifies that saving the same repo+convention pair updates in place.
func TestSaveFinding_Upsert(t *testing.T) {
	db := openTestDB(t)

	if err := db.UpsertRepo("lucas42/test_repo", conventions.RepoTypeUnconfigured, false); err != nil {
		t.Fatalf("UpsertRepo failed: %v", err)
	}
	if err := db.UpsertConvention("test-convention", "A test"); err != nil {
		t.Fatalf("UpsertConvention failed: %v", err)
	}

	failing := conventions.ConventionResult{
		Convention: "test-convention",
		Pass:       false,
		Detail:     "file missing",
	}
	if err := db.SaveFinding(failing, "lucas42/test_repo", "https://github.com/lucas42/test_repo/issues/99"); err != nil {
		t.Fatalf("SaveFinding (fail) failed: %v", err)
	}

	passing := conventions.ConventionResult{
		Convention: "test-convention",
		Pass:       true,
		Detail:     "file found",
	}
	if err := db.SaveFinding(passing, "lucas42/test_repo", ""); err != nil {
		t.Fatalf("SaveFinding (pass) failed: %v", err)
	}

	findings, err := db.GetFindings()
	if err != nil {
		t.Fatalf("GetFindings failed: %v", err)
	}
	// Should still only have one row, not two.
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding after upsert, got %d", len(findings))
	}
	f := findings[0]
	if !f.Pass {
		t.Error("expected Pass to be true after upsert")
	}
	if f.IssueURL != "" {
		t.Errorf("expected empty IssueURL after upsert, got %q", f.IssueURL)
	}
}

// TestOpenDB_BadPath verifies that an invalid path returns an error.
func TestOpenDB_BadPath(t *testing.T) {
	_, err := OpenDB("/nonexistent/path/to/db.sqlite")
	if err == nil {
		t.Error("expected error for bad path, got nil")
	}
}

// TestOpenDB_InvalidPath verifies that a path pointing to a non-writable directory fails.
func TestOpenDB_InvalidPath(t *testing.T) {
	// Use a path that definitely can't be created.
	_, err := OpenDB(filepath.Join(os.DevNull, "subdir", "test.db"))
	if err == nil {
		t.Error("expected error for invalid path, got nil")
	}
}

// TestGetStatusReport_Empty verifies that an empty database returns a zeroed summary.
func TestGetStatusReport_Empty(t *testing.T) {
	db := openTestDB(t)

	report, err := db.GetStatusReport()
	if err != nil {
		t.Fatalf("GetStatusReport failed: %v", err)
	}
	if len(report.Repos) != 0 {
		t.Errorf("expected empty repos map, got %d entries", len(report.Repos))
	}
	if report.Summary.TotalRepos != 0 {
		t.Errorf("expected TotalRepos 0, got %d", report.Summary.TotalRepos)
	}
	if report.Summary.CompliantRepos != 0 {
		t.Errorf("expected CompliantRepos 0, got %d", report.Summary.CompliantRepos)
	}
	if report.Summary.TotalViolations != 0 {
		t.Errorf("expected TotalViolations 0, got %d", report.Summary.TotalViolations)
	}
}

// TestGetStatusReport_AllPassing verifies a fully compliant report.
func TestGetStatusReport_AllPassing(t *testing.T) {
	db := openTestDB(t)

	if err := db.UpsertRepo("lucas42/repo_a", conventions.RepoTypeUnconfigured, false); err != nil {
		t.Fatalf("UpsertRepo failed: %v", err)
	}
	if err := db.UpsertConvention("conv-1", "First convention"); err != nil {
		t.Fatalf("UpsertConvention failed: %v", err)
	}
	if err := db.SaveFinding(conventions.ConventionResult{Convention: "conv-1", Pass: true, Detail: "ok"}, "lucas42/repo_a", ""); err != nil {
		t.Fatalf("SaveFinding failed: %v", err)
	}

	report, err := db.GetStatusReport()
	if err != nil {
		t.Fatalf("GetStatusReport failed: %v", err)
	}

	if report.Summary.TotalRepos != 1 {
		t.Errorf("expected TotalRepos 1, got %d", report.Summary.TotalRepos)
	}
	if report.Summary.CompliantRepos != 1 {
		t.Errorf("expected CompliantRepos 1, got %d", report.Summary.CompliantRepos)
	}
	if report.Summary.TotalViolations != 0 {
		t.Errorf("expected TotalViolations 0, got %d", report.Summary.TotalViolations)
	}

	rs, ok := report.Repos["lucas42/repo_a"]
	if !ok {
		t.Fatal("expected entry for 'lucas42/repo_a' in report")
	}
	if !rs.Compliant {
		t.Error("expected repo to be compliant")
	}
	cs, ok := rs.Conventions["conv-1"]
	if !ok {
		t.Fatal("expected 'conv-1' convention entry")
	}
	if !cs.Pass {
		t.Error("expected convention to pass")
	}
	if cs.Detail != "ok" {
		t.Errorf("expected detail 'ok', got %q", cs.Detail)
	}
}

// TestGetStatusReport_WithViolations verifies counts when some repos fail conventions.
func TestGetStatusReport_WithViolations(t *testing.T) {
	db := openTestDB(t)

	for _, repo := range []string{"lucas42/repo_a", "lucas42/repo_b"} {
		if err := db.UpsertRepo(repo, conventions.RepoTypeUnconfigured, false); err != nil {
			t.Fatalf("UpsertRepo failed: %v", err)
		}
	}
	for _, conv := range []string{"conv-1", "conv-2"} {
		if err := db.UpsertConvention(conv, conv+" description"); err != nil {
			t.Fatalf("UpsertConvention failed: %v", err)
		}
	}

	// repo_a: conv-1 passes, conv-2 fails.
	if err := db.SaveFinding(conventions.ConventionResult{Convention: "conv-1", Pass: true, Detail: "ok"}, "lucas42/repo_a", ""); err != nil {
		t.Fatalf("SaveFinding failed: %v", err)
	}
	if err := db.SaveFinding(conventions.ConventionResult{Convention: "conv-2", Pass: false, Detail: "missing"}, "lucas42/repo_a", "https://github.com/lucas42/repo_a/issues/1"); err != nil {
		t.Fatalf("SaveFinding failed: %v", err)
	}

	// repo_b: both pass.
	if err := db.SaveFinding(conventions.ConventionResult{Convention: "conv-1", Pass: true, Detail: "ok"}, "lucas42/repo_b", ""); err != nil {
		t.Fatalf("SaveFinding failed: %v", err)
	}
	if err := db.SaveFinding(conventions.ConventionResult{Convention: "conv-2", Pass: true, Detail: "ok"}, "lucas42/repo_b", ""); err != nil {
		t.Fatalf("SaveFinding failed: %v", err)
	}

	report, err := db.GetStatusReport()
	if err != nil {
		t.Fatalf("GetStatusReport failed: %v", err)
	}

	if report.Summary.TotalRepos != 2 {
		t.Errorf("expected TotalRepos 2, got %d", report.Summary.TotalRepos)
	}
	if report.Summary.CompliantRepos != 1 {
		t.Errorf("expected CompliantRepos 1, got %d", report.Summary.CompliantRepos)
	}
	if report.Summary.TotalViolations != 1 {
		t.Errorf("expected TotalViolations 1, got %d", report.Summary.TotalViolations)
	}

	repoA := report.Repos["lucas42/repo_a"]
	if repoA.Compliant {
		t.Error("repo_a should not be compliant")
	}
	csA2 := repoA.Conventions["conv-2"]
	if csA2.IssueURL != "https://github.com/lucas42/repo_a/issues/1" {
		t.Errorf("expected issue_url, got %q", csA2.IssueURL)
	}

	repoB := report.Repos["lucas42/repo_b"]
	if !repoB.Compliant {
		t.Error("repo_b should be compliant")
	}
}

// TestDeleteStaleFindings_DeletesOldRows verifies that findings older than the
// cutoff are removed while newer ones are preserved.
func TestDeleteStaleFindings_DeletesOldRows(t *testing.T) {
	db := openTestDB(t)

	if err := db.UpsertRepo("lucas42/repo_a", conventions.RepoTypeUnconfigured, false); err != nil {
		t.Fatalf("UpsertRepo failed: %v", err)
	}
	if err := db.UpsertConvention("conv-old", "Old convention"); err != nil {
		t.Fatalf("UpsertConvention failed: %v", err)
	}
	if err := db.UpsertConvention("conv-new", "New convention"); err != nil {
		t.Fatalf("UpsertConvention failed: %v", err)
	}

	// Insert a stale finding directly with a timestamp in the past.
	past := time.Now().Add(-10 * time.Minute)
	if _, err := db.conn.Exec(
		`INSERT INTO findings (repo, convention, pass, detail, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"lucas42/repo_a", "conv-old", 1, "was passing", past.UTC(),
	); err != nil {
		t.Fatalf("failed to insert stale finding: %v", err)
	}

	// Save a fresh finding via SaveFinding (which sets updated_at = now).
	cutoff := time.Now()
	result := conventions.ConventionResult{Convention: "conv-new", Pass: true, Detail: "still in scope"}
	if err := db.SaveFinding(result, "lucas42/repo_a", ""); err != nil {
		t.Fatalf("SaveFinding failed: %v", err)
	}

	if err := db.DeleteStaleFindings(cutoff); err != nil {
		t.Fatalf("DeleteStaleFindings failed: %v", err)
	}

	findings, err := db.GetFindings()
	if err != nil {
		t.Fatalf("GetFindings failed: %v", err)
	}

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding after cleanup, got %d", len(findings))
	}
	if findings[0].Convention != "conv-new" {
		t.Errorf("expected conv-new to survive, got %q", findings[0].Convention)
	}
}

// TestDeleteStaleFindings_NoRows verifies that calling DeleteStaleFindings on an
// empty table does not return an error.
func TestDeleteStaleFindings_NoRows(t *testing.T) {
	db := openTestDB(t)

	if err := db.DeleteStaleFindings(time.Now()); err != nil {
		t.Errorf("DeleteStaleFindings on empty table returned error: %v", err)
	}
}

// TestDeleteStaleFindings_PreservesAll verifies that when all findings are newer
// than the cutoff, none are deleted.
func TestDeleteStaleFindings_PreservesAll(t *testing.T) {
	db := openTestDB(t)

	if err := db.UpsertRepo("lucas42/repo_a", conventions.RepoTypeUnconfigured, false); err != nil {
		t.Fatalf("UpsertRepo failed: %v", err)
	}
	if err := db.UpsertConvention("conv-1", "A convention"); err != nil {
		t.Fatalf("UpsertConvention failed: %v", err)
	}

	// Use a cutoff in the past so all findings are "newer".
	cutoff := time.Now().Add(-1 * time.Hour)

	result := conventions.ConventionResult{Convention: "conv-1", Pass: true, Detail: "ok"}
	if err := db.SaveFinding(result, "lucas42/repo_a", ""); err != nil {
		t.Fatalf("SaveFinding failed: %v", err)
	}

	if err := db.DeleteStaleFindings(cutoff); err != nil {
		t.Fatalf("DeleteStaleFindings failed: %v", err)
	}

	findings, err := db.GetFindings()
	if err != nil {
		t.Fatalf("GetFindings failed: %v", err)
	}
	if len(findings) != 1 {
		t.Errorf("expected 1 finding to survive, got %d", len(findings))
	}
}
