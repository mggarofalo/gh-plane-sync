package sync

import (
	"context"
	"time"

	"github.com/mggarofalo/gh-plane-sync/internal/github"
	"github.com/mggarofalo/gh-plane-sync/internal/plane"
)

// GitHubAdapter adapts a github.Client to the sync.GitHubClient interface.
type GitHubAdapter struct {
	client github.Client
}

// NewGitHubAdapter wraps a github.Client for use with the sync engine.
func NewGitHubAdapter(c github.Client) *GitHubAdapter {
	return &GitHubAdapter{client: c}
}

// ListIssues fetches issues via the underlying GitHub client and converts
// them to the sync package's GitHubIssue type.
func (a *GitHubAdapter) ListIssues(ctx context.Context, owner, repo string, since time.Time) ([]GitHubIssue, error) {
	issues, err := a.client.ListIssues(ctx, owner, repo, since)
	if err != nil {
		return nil, err
	}

	result := make([]GitHubIssue, len(issues))
	for i, issue := range issues {
		result[i] = GitHubIssue{
			Number:    issue.Number,
			Title:     issue.Title,
			Body:      issue.Body,
			State:     issue.State,
			HTMLURL:   issue.HTMLURL,
			UpdatedAt: issue.UpdatedAt,
		}
	}

	return result, nil
}

// PlaneAdapter adapts a plane.Client to the sync.PlaneClient interface.
type PlaneAdapter struct {
	client plane.Client
}

// NewPlaneAdapter wraps a plane.Client for use with the sync engine.
func NewPlaneAdapter(c plane.Client) *PlaneAdapter {
	return &PlaneAdapter{client: c}
}

// CreateIssue creates an issue via the underlying Plane client and converts
// the response to the sync package's PlaneIssue type.
func (a *PlaneAdapter) CreateIssue(ctx context.Context, workspaceSlug, projectID string, req CreatePlaneIssueRequest) (PlaneIssue, error) {
	issue, err := a.client.CreateIssue(ctx, workspaceSlug, projectID, plane.CreateIssueRequest{
		Name:            req.Name,
		DescriptionHTML: req.DescriptionHTML,
	})
	if err != nil {
		return PlaneIssue{}, err
	}

	return PlaneIssue{
		ID:              issue.ID,
		Name:            issue.Name,
		DescriptionHTML: issue.DescriptionHTML,
	}, nil
}

// UpdateIssue updates an issue via the underlying Plane client and converts
// the response to the sync package's PlaneIssue type.
func (a *PlaneAdapter) UpdateIssue(ctx context.Context, workspaceSlug, projectID, issueID string, req UpdatePlaneIssueRequest) (PlaneIssue, error) {
	issue, err := a.client.UpdateIssue(ctx, workspaceSlug, projectID, issueID, plane.UpdateIssueRequest{
		Name:            req.Name,
		DescriptionHTML: req.DescriptionHTML,
	})
	if err != nil {
		return PlaneIssue{}, err
	}

	return PlaneIssue{
		ID:              issue.ID,
		Name:            issue.Name,
		DescriptionHTML: issue.DescriptionHTML,
	}, nil
}
