package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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
	if _, err := db.conn.Exec(`INSERT INTO repos (name, last_audited) VALUES (?, ?)`, "test/repo", time.Now()); err != nil {
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
	if err := db.UpsertRepo("lucas42/test_repo"); err != nil {
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

// TestSaveFinding_Pass verifies that a passing finding is stored correctly.
func TestSaveFinding_Pass(t *testing.T) {
	db := openTestDB(t)

	// Set up prerequisite rows.
	if err := db.UpsertRepo("lucas42/test_repo"); err != nil {
		t.Fatalf("UpsertRepo failed: %v", err)
	}
	if err := db.UpsertConvention("test-convention", "A test"); err != nil {
		t.Fatalf("UpsertConvention failed: %v", err)
	}

	result := ConventionResult{
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

	if err := db.UpsertRepo("lucas42/test_repo"); err != nil {
		t.Fatalf("UpsertRepo failed: %v", err)
	}
	if err := db.UpsertConvention("test-convention", "A test"); err != nil {
		t.Fatalf("UpsertConvention failed: %v", err)
	}

	result := ConventionResult{
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

	if err := db.UpsertRepo("lucas42/test_repo"); err != nil {
		t.Fatalf("UpsertRepo failed: %v", err)
	}
	if err := db.UpsertConvention("test-convention", "A test"); err != nil {
		t.Fatalf("UpsertConvention failed: %v", err)
	}

	failing := ConventionResult{
		Convention: "test-convention",
		Pass:       false,
		Detail:     "file missing",
	}
	if err := db.SaveFinding(failing, "lucas42/test_repo", "https://github.com/lucas42/test_repo/issues/99"); err != nil {
		t.Fatalf("SaveFinding (fail) failed: %v", err)
	}

	passing := ConventionResult{
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
