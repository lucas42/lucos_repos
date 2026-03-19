package main

import (
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"lucos_repos/conventions"
	_ "modernc.org/sqlite"
)

// DB wraps a SQLite database connection and provides methods for persisting
// audit findings.
type DB struct {
	conn *sql.DB
}

// OpenDB opens (or creates) a SQLite database at the given path and initialises
// the schema. It returns an error if the database cannot be opened or the
// schema cannot be created.
func OpenDB(path string) (*DB, error) {
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite database at %s: %w", path, err)
	}

	// Enable WAL mode for better concurrent read/write performance.
	if _, err := conn.Exec("PRAGMA journal_mode=WAL"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	db := &DB{conn: conn}
	if err := db.createSchema(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	return db, nil
}

// Close closes the underlying database connection.
func (db *DB) Close() error {
	return db.conn.Close()
}

// Ping confirms the database is accessible with a minimal query.
// It is suitable for use in health checks where a full table scan would be wasteful.
func (db *DB) Ping() error {
	var dummy int
	if err := db.conn.QueryRow("SELECT 1").Scan(&dummy); err != nil {
		return fmt.Errorf("database ping failed: %w", err)
	}
	return nil
}

// createSchema creates the database tables if they do not already exist.
func (db *DB) createSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS repos (
		name          TEXT PRIMARY KEY,
		last_audited  DATETIME,
		repo_type     TEXT NOT NULL DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS conventions (
		id          TEXT PRIMARY KEY,
		description TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS findings (
		repo        TEXT NOT NULL,
		convention  TEXT NOT NULL,
		pass        INTEGER NOT NULL,
		detail      TEXT NOT NULL,
		issue_url   TEXT,
		updated_at  DATETIME NOT NULL,
		PRIMARY KEY (repo, convention),
		FOREIGN KEY (repo) REFERENCES repos(name),
		FOREIGN KEY (convention) REFERENCES conventions(id)
	);
	`

	if _, err := db.conn.Exec(schema); err != nil {
		return fmt.Errorf("failed to execute schema DDL: %w", err)
	}

	// Migration: add repo_type column to existing databases.
	// SQLite does not support IF NOT EXISTS on ALTER TABLE, so we attempt the
	// migration and ignore the "duplicate column name" error silently.
	if _, err := db.conn.Exec(`ALTER TABLE repos ADD COLUMN repo_type TEXT NOT NULL DEFAULT ''`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("failed to migrate repos table: %w", err)
		}
	}

	slog.Info("Database schema initialised")
	return nil
}

// UpsertConvention inserts or updates a convention record.
func (db *DB) UpsertConvention(id, description string) error {
	_, err := db.conn.Exec(
		`INSERT INTO conventions (id, description) VALUES (?, ?)
		 ON CONFLICT(id) DO UPDATE SET description = excluded.description`,
		id, description,
	)
	if err != nil {
		return fmt.Errorf("failed to upsert convention %s: %w", id, err)
	}
	return nil
}

// UpsertRepo inserts or updates a repo record, setting last_audited to now.
func (db *DB) UpsertRepo(name string, repoType conventions.RepoType) error {
	_, err := db.conn.Exec(
		`INSERT INTO repos (name, last_audited, repo_type) VALUES (?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET last_audited = excluded.last_audited, repo_type = excluded.repo_type`,
		name, time.Now().UTC(), string(repoType),
	)
	if err != nil {
		return fmt.Errorf("failed to upsert repo %s: %w", name, err)
	}
	return nil
}

// SaveFinding inserts or updates a finding for a repo + convention pair.
func (db *DB) SaveFinding(result conventions.ConventionResult, repo string, issueURL string) error {
	passInt := 0
	if result.Pass {
		passInt = 1
	}

	_, err := db.conn.Exec(
		`INSERT INTO findings (repo, convention, pass, detail, issue_url, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(repo, convention) DO UPDATE SET
		   pass       = excluded.pass,
		   detail     = excluded.detail,
		   issue_url  = excluded.issue_url,
		   updated_at = excluded.updated_at`,
		repo, result.Convention, passInt, result.Detail, nullableString(issueURL), time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("failed to save finding for repo=%s convention=%s: %w", repo, result.Convention, err)
	}
	return nil
}

// DeleteStaleFindings deletes all findings whose updated_at is before the given
// cutoff time. This is called at the end of a successful audit sweep to remove
// findings for repo+convention pairs that are no longer in scope (archived repos,
// removed conventions, newly-excluded repos).
func (db *DB) DeleteStaleFindings(cutoff time.Time) error {
	result, err := db.conn.Exec(
		`DELETE FROM findings WHERE updated_at < ?`,
		cutoff.UTC(),
	)
	if err != nil {
		return fmt.Errorf("failed to delete stale findings: %w", err)
	}
	n, _ := result.RowsAffected()
	if n > 0 {
		slog.Info("Deleted stale findings", "count", n)
	}
	return nil
}

// nullableString converts an empty string to nil (stored as NULL in SQLite).
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// FindingRow is a row from the findings table.
type FindingRow struct {
	Repo       string
	Convention string
	Pass       bool
	Detail     string
	IssueURL   string
	UpdatedAt  time.Time
}

// GetFindings returns all findings, ordered by repo and convention.
func (db *DB) GetFindings() ([]FindingRow, error) {
	rows, err := db.conn.Query(
		`SELECT repo, convention, pass, detail, COALESCE(issue_url, ''), updated_at
		 FROM findings
		 ORDER BY repo, convention`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query findings: %w", err)
	}
	defer rows.Close()

	var findings []FindingRow
	for rows.Next() {
		var f FindingRow
		var passInt int
		var updatedAtStr string
		if err := rows.Scan(&f.Repo, &f.Convention, &passInt, &f.Detail, &f.IssueURL, &updatedAtStr); err != nil {
			return nil, fmt.Errorf("failed to scan finding row: %w", err)
		}
		f.Pass = passInt != 0
		f.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAtStr)
		if err != nil {
			// Try alternative format SQLite may use.
			var err2 error
			f.UpdatedAt, err2 = time.Parse("2006-01-02 15:04:05.999999999-07:00", updatedAtStr)
			if err2 != nil {
				return nil, fmt.Errorf("failed to parse updated_at %q: %w", updatedAtStr, err)
			}
		}
		findings = append(findings, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating finding rows: %w", err)
	}
	return findings, nil
}

// ConventionStatus is the per-convention entry in a StatusReport.
type ConventionStatus struct {
	Pass     bool   `json:"pass"`
	Detail   string `json:"detail"`
	IssueURL string `json:"issue_url,omitempty"`
}

// RepoStatus is the per-repo entry in a StatusReport.
type RepoStatus struct {
	Type        conventions.RepoType        `json:"type"`
	Conventions map[string]ConventionStatus `json:"conventions"`
	Compliant   bool                        `json:"compliant"`
}

// StatusSummary holds aggregate counts across all repos.
type StatusSummary struct {
	TotalRepos      int `json:"total_repos"`
	CompliantRepos  int `json:"compliant_repos"`
	TotalViolations int `json:"total_violations"`
}

// StatusReport is the full compliance status returned by GET /api/status.
type StatusReport struct {
	Repos   map[string]RepoStatus `json:"repos"`
	Summary StatusSummary         `json:"summary"`
}

// getRepoTypes returns a map of repo name to RepoType for all repos in the database.
func (db *DB) getRepoTypes() (map[string]conventions.RepoType, error) {
	rows, err := db.conn.Query(`SELECT name, repo_type FROM repos`)
	if err != nil {
		return nil, fmt.Errorf("failed to query repo types: %w", err)
	}
	defer rows.Close()

	result := map[string]conventions.RepoType{}
	for rows.Next() {
		var name, repoType string
		if err := rows.Scan(&name, &repoType); err != nil {
			return nil, fmt.Errorf("failed to scan repo type row: %w", err)
		}
		result[name] = conventions.RepoType(repoType)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating repo type rows: %w", err)
	}
	return result, nil
}

// GetStatusReport builds a StatusReport from the cached findings in the database.
// It returns an empty report (not an error) if no findings have been stored yet.
func (db *DB) GetStatusReport() (StatusReport, error) {
	// First, load the repo_type for every known repo.
	repoTypes, err := db.getRepoTypes()
	if err != nil {
		return StatusReport{}, fmt.Errorf("failed to get repo types: %w", err)
	}

	findings, err := db.GetFindings()
	if err != nil {
		return StatusReport{}, fmt.Errorf("failed to get findings for status report: %w", err)
	}

	repos := map[string]RepoStatus{}
	for _, f := range findings {
		rs, ok := repos[f.Repo]
		if !ok {
			rs = RepoStatus{
				Type:        repoTypes[f.Repo],
				Conventions: map[string]ConventionStatus{},
				Compliant:   true,
			}
		}
		rs.Conventions[f.Convention] = ConventionStatus{
			Pass:     f.Pass,
			Detail:   f.Detail,
			IssueURL: f.IssueURL,
		}
		if !f.Pass {
			rs.Compliant = false
		}
		repos[f.Repo] = rs
	}

	var totalViolations, compliantRepos int
	for _, rs := range repos {
		if rs.Compliant {
			compliantRepos++
		}
		for _, cs := range rs.Conventions {
			if !cs.Pass {
				totalViolations++
			}
		}
	}

	return StatusReport{
		Repos: repos,
		Summary: StatusSummary{
			TotalRepos:      len(repos),
			CompliantRepos:  compliantRepos,
			TotalViolations: totalViolations,
		},
	}, nil
}
