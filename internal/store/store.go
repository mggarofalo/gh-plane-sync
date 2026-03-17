// Package store provides a SQLite-backed mapping store for tracking
// the relationship between GitHub issues and Plane work items.
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // Pure-Go SQLite driver.
)

// DefaultDBPath is the default location for the SQLite database.
const DefaultDBPath = "/var/lib/gh-plane-sync/sync.db"

// Store wraps a SQLite database connection for persisting sync mappings.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) a SQLite database at the given path, creates
// parent directories as needed, and runs schema migrations.
func Open(path string) (*Store, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("creating database directory %s: %w", dir, err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening database at %s: %w", path, err)
	}

	// Enable WAL mode for better concurrent read performance.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enabling WAL mode: %w", err)
	}

	// Enable foreign keys.
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enabling foreign keys: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}
	return s, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("closing database: %w", err)
	}
	return nil
}

// migrate creates tables if they do not already exist.
func (s *Store) migrate() error {
	const ddl = `
CREATE TABLE IF NOT EXISTS issue_map (
	github_owner        TEXT    NOT NULL,
	github_repo         TEXT    NOT NULL,
	github_issue_number INTEGER NOT NULL,
	plane_project_id    TEXT    NOT NULL,
	plane_issue_id      TEXT    NOT NULL,
	created_at          TEXT    NOT NULL,
	updated_at          TEXT    NOT NULL,
	PRIMARY KEY (github_owner, github_repo, github_issue_number)
);

CREATE TABLE IF NOT EXISTS synced_comments (
	github_comment_id   INTEGER NOT NULL,
	plane_comment_id    TEXT    NOT NULL,
	github_issue_number INTEGER NOT NULL,
	github_owner        TEXT    NOT NULL,
	github_repo         TEXT    NOT NULL,
	synced_at           TEXT    NOT NULL,
	PRIMARY KEY (github_comment_id)
);

CREATE TABLE IF NOT EXISTS sync_state (
	github_owner   TEXT NOT NULL,
	github_repo    TEXT NOT NULL,
	last_synced_at TEXT NOT NULL,
	PRIMARY KEY (github_owner, github_repo)
);
`
	if _, err := s.db.Exec(ddl); err != nil {
		return fmt.Errorf("creating tables: %w", err)
	}
	return nil
}
