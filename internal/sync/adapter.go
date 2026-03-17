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

// GetIssue fetches a single issue via the underlying GitHub client and
// converts it to the sync package's GitHubIssue type.
func (a *GitHubAdapter) GetIssue(ctx context.Context, owner, repo string, number int) (GitHubIssue, error) {
	issue, err := a.client.GetIssue(ctx, owner, repo, number)
	if err != nil {
		return GitHubIssue{}, err
	}
	return GitHubIssue{
		Number:    issue.Number,
		Title:     issue.Title,
		Body:      issue.Body,
		State:     issue.State,
		HTMLURL:   issue.HTMLURL,
		UpdatedAt: issue.UpdatedAt,
	}, nil
}

// CloseIssue closes a GitHub issue via the underlying client.
func (a *GitHubAdapter) CloseIssue(ctx context.Context, owner, repo string, number int) error {
	return a.client.CloseIssue(ctx, owner, repo, number)
}

// ReopenIssue reopens a GitHub issue via the underlying client.
func (a *GitHubAdapter) ReopenIssue(ctx context.Context, owner, repo string, number int) error {
	return a.client.ReopenIssue(ctx, owner, repo, number)
}

// CreateComment adds a comment to a GitHub issue via the underlying client.
func (a *GitHubAdapter) CreateComment(ctx context.Context, owner, repo string, number int, body string) error {
	return a.client.CreateComment(ctx, owner, repo, number, body)
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

// ListComments fetches comments via the underlying GitHub client and converts
// them to the sync package's GitHubComment type.
func (a *GitHubAdapter) ListComments(ctx context.Context, owner, repo string, issueNumber int) ([]GitHubComment, error) {
	comments, err := a.client.ListComments(ctx, owner, repo, issueNumber)
	if err != nil {
		return nil, err
	}

	result := make([]GitHubComment, len(comments))
	for i, c := range comments {
		result[i] = GitHubComment{
			ID:        c.ID,
			Body:      c.Body,
			User:      c.User.Login,
			CreatedAt: c.CreatedAt,
			HTMLURL:   c.HTMLURL,
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

// GetIssue fetches a single issue via the underlying Plane client and
// converts it to the sync package's PlaneIssue type.
func (a *PlaneAdapter) GetIssue(ctx context.Context, workspaceSlug, projectID, issueID string) (PlaneIssue, error) {
	issue, err := a.client.GetIssue(ctx, workspaceSlug, projectID, issueID)
	if err != nil {
		return PlaneIssue{}, err
	}
	return PlaneIssue{
		ID:              issue.ID,
		Name:            issue.Name,
		DescriptionHTML: issue.DescriptionHTML,
		StateName:       issue.State.Name,
	}, nil
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

// CreateComment creates a comment via the underlying Plane client and converts
// the response to the sync package's PlaneComment type.
func (a *PlaneAdapter) CreateComment(ctx context.Context, workspaceSlug, projectID, issueID string, req CreatePlaneCommentRequest) (PlaneComment, error) {
	comment, err := a.client.CreateComment(ctx, workspaceSlug, projectID, issueID, plane.CreateCommentRequest{
		CommentHTML: req.CommentHTML,
	})
	if err != nil {
		return PlaneComment{}, err
	}

	return PlaneComment{
		ID: comment.ID,
	}, nil
}
