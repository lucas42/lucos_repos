package main

import (
	"database/sql"
	"fmt"
	"log/slog"
	"time"

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

// createSchema creates the database tables if they do not already exist.
func (db *DB) createSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS repos (
		name          TEXT PRIMARY KEY,
		last_audited  DATETIME
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
func (db *DB) UpsertRepo(name string) error {
	_, err := db.conn.Exec(
		`INSERT INTO repos (name, last_audited) VALUES (?, ?)
		 ON CONFLICT(name) DO UPDATE SET last_audited = excluded.last_audited`,
		name, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("failed to upsert repo %s: %w", name, err)
	}
	return nil
}

// SaveFinding inserts or updates a finding for a repo + convention pair.
func (db *DB) SaveFinding(result ConventionResult, repo string, issueURL string) error {
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
			f.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05.999999999-07:00", updatedAtStr)
		}
		findings = append(findings, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating finding rows: %w", err)
	}
	return findings, nil
}
