package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// SyncState tracks the last successful sync time for a GitHub repository.
type SyncState struct {
	GitHubOwner  string
	GitHubRepo   string
	LastSyncedAt time.Time
}

// UpsertSyncState inserts or updates the sync state for a repository.
// On conflict (same owner/repo), last_synced_at is overwritten.
func (s *Store) UpsertSyncState(st SyncState) error {
	lastSynced := time.Now().UTC().Format(time.RFC3339)
	if !st.LastSyncedAt.IsZero() {
		lastSynced = st.LastSyncedAt.UTC().Format(time.RFC3339)
	}

	const query = `
INSERT INTO sync_state (github_owner, github_repo, last_synced_at)
VALUES (?, ?, ?)
ON CONFLICT (github_owner, github_repo)
DO UPDATE SET last_synced_at = excluded.last_synced_at
`
	_, err := s.db.Exec(query, st.GitHubOwner, st.GitHubRepo, lastSynced)
	if err != nil {
		return fmt.Errorf("upserting sync state for %s/%s: %w", st.GitHubOwner, st.GitHubRepo, err)
	}
	return nil
}

// GetSyncState retrieves the sync state for the given owner and repo.
// Returns sql.ErrNoRows if no state has been recorded.
func (s *Store) GetSyncState(owner, repo string) (SyncState, error) {
	const query = `
SELECT github_owner, github_repo, last_synced_at
FROM sync_state
WHERE github_owner = ? AND github_repo = ?
`
	var st SyncState
	var lastSynced string
	err := s.db.QueryRow(query, owner, repo).Scan(
		&st.GitHubOwner, &st.GitHubRepo, &lastSynced,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return SyncState{}, fmt.Errorf("sync state for %s/%s: %w", owner, repo, err)
	}
	if err != nil {
		return SyncState{}, fmt.Errorf("querying sync state for %s/%s: %w", owner, repo, err)
	}

	st.LastSyncedAt, err = time.Parse(time.RFC3339, lastSynced)
	if err != nil {
		return SyncState{}, fmt.Errorf("parsing last_synced_at for %s/%s: %w", owner, repo, err)
	}
	return st, nil
}

// DeleteSyncState removes the sync state for the given owner and repo.
func (s *Store) DeleteSyncState(owner, repo string) error {
	const query = `DELETE FROM sync_state WHERE github_owner = ? AND github_repo = ?`
	result, err := s.db.Exec(query, owner, repo)
	if err != nil {
		return fmt.Errorf("deleting sync state for %s/%s: %w", owner, repo, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected for %s/%s: %w", owner, repo, err)
	}
	if n == 0 {
		return fmt.Errorf("deleting sync state for %s/%s: %w", owner, repo, sql.ErrNoRows)
	}
	return nil
}
