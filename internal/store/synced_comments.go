package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// SyncedComment represents a synced comment mapping between GitHub and Plane.
type SyncedComment struct {
	GitHubCommentID   int
	PlaneCommentID    string
	GitHubIssueNumber int
	GitHubOwner       string
	GitHubRepo        string
	SyncedAt          time.Time
}

// UpsertSyncedComment inserts or updates a synced comment record.
// On conflict (same github_comment_id), the Plane comment ID and synced_at
// are overwritten.
func (s *Store) UpsertSyncedComment(c SyncedComment) error {
	syncedAt := time.Now().UTC().Format(time.RFC3339)
	if !c.SyncedAt.IsZero() {
		syncedAt = c.SyncedAt.UTC().Format(time.RFC3339)
	}

	const query = `
INSERT INTO synced_comments (github_comment_id, plane_comment_id, github_issue_number, github_owner, github_repo, synced_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (github_comment_id)
DO UPDATE SET plane_comment_id    = excluded.plane_comment_id,
              github_issue_number = excluded.github_issue_number,
              github_owner        = excluded.github_owner,
              github_repo         = excluded.github_repo,
              synced_at           = excluded.synced_at
`
	_, err := s.db.Exec(query,
		c.GitHubCommentID, c.PlaneCommentID,
		c.GitHubIssueNumber, c.GitHubOwner, c.GitHubRepo,
		syncedAt,
	)
	if err != nil {
		return fmt.Errorf("upserting synced comment %d: %w", c.GitHubCommentID, err)
	}
	return nil
}

// GetSyncedComment retrieves a synced comment by its GitHub comment ID.
// Returns sql.ErrNoRows if no record exists.
func (s *Store) GetSyncedComment(githubCommentID int) (SyncedComment, error) {
	const query = `
SELECT github_comment_id, plane_comment_id, github_issue_number,
       github_owner, github_repo, synced_at
FROM synced_comments
WHERE github_comment_id = ?
`
	var c SyncedComment
	var syncedAt string
	err := s.db.QueryRow(query, githubCommentID).Scan(
		&c.GitHubCommentID, &c.PlaneCommentID,
		&c.GitHubIssueNumber, &c.GitHubOwner, &c.GitHubRepo,
		&syncedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return SyncedComment{}, fmt.Errorf("synced comment %d: %w", githubCommentID, err)
	}
	if err != nil {
		return SyncedComment{}, fmt.Errorf("querying synced comment %d: %w", githubCommentID, err)
	}

	c.SyncedAt, err = time.Parse(time.RFC3339, syncedAt)
	if err != nil {
		return SyncedComment{}, fmt.Errorf("parsing synced_at for comment %d: %w", githubCommentID, err)
	}
	return c, nil
}

// ListSyncedComments returns all synced comments for a given GitHub issue.
func (s *Store) ListSyncedComments(owner, repo string, issueNumber int) ([]SyncedComment, error) {
	const query = `
SELECT github_comment_id, plane_comment_id, github_issue_number,
       github_owner, github_repo, synced_at
FROM synced_comments
WHERE github_owner = ? AND github_repo = ? AND github_issue_number = ?
ORDER BY github_comment_id
`
	rows, err := s.db.Query(query, owner, repo, issueNumber)
	if err != nil {
		return nil, fmt.Errorf("listing synced comments for %s/%s#%d: %w", owner, repo, issueNumber, err)
	}
	defer func() { _ = rows.Close() }()

	var comments []SyncedComment
	for rows.Next() {
		var c SyncedComment
		var syncedAt string
		if err := rows.Scan(
			&c.GitHubCommentID, &c.PlaneCommentID,
			&c.GitHubIssueNumber, &c.GitHubOwner, &c.GitHubRepo,
			&syncedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning synced comment row: %w", err)
		}
		c.SyncedAt, err = time.Parse(time.RFC3339, syncedAt)
		if err != nil {
			return nil, fmt.Errorf("parsing synced_at: %w", err)
		}
		comments = append(comments, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating synced comment rows: %w", err)
	}
	return comments, nil
}

// DeleteSyncedComment removes a synced comment record by GitHub comment ID.
func (s *Store) DeleteSyncedComment(githubCommentID int) error {
	const query = `DELETE FROM synced_comments WHERE github_comment_id = ?`
	result, err := s.db.Exec(query, githubCommentID)
	if err != nil {
		return fmt.Errorf("deleting synced comment %d: %w", githubCommentID, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected for comment %d: %w", githubCommentID, err)
	}
	if n == 0 {
		return fmt.Errorf("deleting synced comment %d: %w", githubCommentID, sql.ErrNoRows)
	}
	return nil
}
