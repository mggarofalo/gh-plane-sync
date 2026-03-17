package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// IssueMap represents a mapping between a GitHub issue and a Plane work item.
type IssueMap struct {
	GitHubOwner       string
	GitHubRepo        string
	GitHubIssueNumber int
	PlaneProjectID    string
	PlaneIssueID      string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// UpsertIssueMap inserts or updates an issue mapping. On conflict (same
// owner/repo/issue_number), the Plane IDs and updated_at are overwritten.
func (s *Store) UpsertIssueMap(m IssueMap) error {
	now := time.Now().UTC().Format(time.RFC3339)
	createdAt := m.CreatedAt.UTC().Format(time.RFC3339)
	if m.CreatedAt.IsZero() {
		createdAt = now
	}

	const query = `
INSERT INTO issue_map (github_owner, github_repo, github_issue_number, plane_project_id, plane_issue_id, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (github_owner, github_repo, github_issue_number)
DO UPDATE SET plane_project_id = excluded.plane_project_id,
              plane_issue_id   = excluded.plane_issue_id,
              updated_at       = excluded.updated_at
`
	_, err := s.db.Exec(query,
		m.GitHubOwner, m.GitHubRepo, m.GitHubIssueNumber,
		m.PlaneProjectID, m.PlaneIssueID,
		createdAt, now,
	)
	if err != nil {
		return fmt.Errorf("upserting issue map for %s/%s#%d: %w",
			m.GitHubOwner, m.GitHubRepo, m.GitHubIssueNumber, err)
	}
	return nil
}

// GetIssueMap retrieves a mapping by GitHub owner, repo, and issue number.
// Returns sql.ErrNoRows if no mapping exists.
func (s *Store) GetIssueMap(owner, repo string, issueNumber int) (IssueMap, error) {
	const query = `
SELECT github_owner, github_repo, github_issue_number,
       plane_project_id, plane_issue_id, created_at, updated_at
FROM issue_map
WHERE github_owner = ? AND github_repo = ? AND github_issue_number = ?
`
	var m IssueMap
	var createdAt, updatedAt string
	err := s.db.QueryRow(query, owner, repo, issueNumber).Scan(
		&m.GitHubOwner, &m.GitHubRepo, &m.GitHubIssueNumber,
		&m.PlaneProjectID, &m.PlaneIssueID,
		&createdAt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return IssueMap{}, fmt.Errorf("issue map for %s/%s#%d: %w", owner, repo, issueNumber, err)
	}
	if err != nil {
		return IssueMap{}, fmt.Errorf("querying issue map for %s/%s#%d: %w", owner, repo, issueNumber, err)
	}

	m.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return IssueMap{}, fmt.Errorf("parsing created_at for %s/%s#%d: %w", owner, repo, issueNumber, err)
	}
	m.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return IssueMap{}, fmt.Errorf("parsing updated_at for %s/%s#%d: %w", owner, repo, issueNumber, err)
	}
	return m, nil
}

// ListIssueMaps returns all issue mappings for the given GitHub owner and repo.
func (s *Store) ListIssueMaps(owner, repo string) ([]IssueMap, error) {
	const query = `
SELECT github_owner, github_repo, github_issue_number,
       plane_project_id, plane_issue_id, created_at, updated_at
FROM issue_map
WHERE github_owner = ? AND github_repo = ?
ORDER BY github_issue_number
`
	rows, err := s.db.Query(query, owner, repo)
	if err != nil {
		return nil, fmt.Errorf("listing issue maps for %s/%s: %w", owner, repo, err)
	}
	defer func() { _ = rows.Close() }()

	var maps []IssueMap
	for rows.Next() {
		var m IssueMap
		var createdAt, updatedAt string
		if err := rows.Scan(
			&m.GitHubOwner, &m.GitHubRepo, &m.GitHubIssueNumber,
			&m.PlaneProjectID, &m.PlaneIssueID,
			&createdAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning issue map row: %w", err)
		}
		m.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
		if err != nil {
			return nil, fmt.Errorf("parsing created_at: %w", err)
		}
		m.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
		if err != nil {
			return nil, fmt.Errorf("parsing updated_at: %w", err)
		}
		maps = append(maps, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating issue map rows: %w", err)
	}
	return maps, nil
}

// DeleteIssueMap removes a mapping by GitHub owner, repo, and issue number.
func (s *Store) DeleteIssueMap(owner, repo string, issueNumber int) error {
	const query = `DELETE FROM issue_map WHERE github_owner = ? AND github_repo = ? AND github_issue_number = ?`
	result, err := s.db.Exec(query, owner, repo, issueNumber)
	if err != nil {
		return fmt.Errorf("deleting issue map for %s/%s#%d: %w", owner, repo, issueNumber, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected for %s/%s#%d: %w", owner, repo, issueNumber, err)
	}
	if n == 0 {
		return fmt.Errorf("deleting issue map for %s/%s#%d: %w", owner, repo, issueNumber, sql.ErrNoRows)
	}
	return nil
}
