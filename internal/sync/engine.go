// Package sync orchestrates the GitHub-to-Plane issue synchronization cycle.
// It fetches open issues from configured GitHub repositories and creates or
// updates corresponding Plane work items, persisting mappings in SQLite.
package sync

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html"
	"time"

	"github.com/mggarofalo/gh-plane-sync/internal/config"
	"github.com/mggarofalo/gh-plane-sync/internal/log"
	"github.com/mggarofalo/gh-plane-sync/internal/store"
)

// GitHubClient is the subset of the GitHub API needed by the sync engine.
type GitHubClient interface {
	ListIssues(ctx context.Context, owner, repo string, since time.Time) ([]GitHubIssue, error)
	GetIssue(ctx context.Context, owner, repo string, number int) (GitHubIssue, error)
	CloseIssue(ctx context.Context, owner, repo string, number int) error
	ReopenIssue(ctx context.Context, owner, repo string, number int) error
	CreateComment(ctx context.Context, owner, repo string, number int, body string) error
}

// GitHubIssue mirrors the fields the engine needs from a GitHub issue.
type GitHubIssue struct {
	Number    int
	Title     string
	Body      string
	State     string
	HTMLURL   string
	UpdatedAt time.Time
}

// PlaneClient is the subset of the Plane API needed by the sync engine.
type PlaneClient interface {
	GetIssue(ctx context.Context, workspaceSlug, projectID, issueID string) (PlaneIssue, error)
	CreateIssue(ctx context.Context, workspaceSlug, projectID string, req CreatePlaneIssueRequest) (PlaneIssue, error)
	UpdateIssue(ctx context.Context, workspaceSlug, projectID, issueID string, req UpdatePlaneIssueRequest) (PlaneIssue, error)
}

// PlaneIssue mirrors the fields the engine needs from a Plane work item.
type PlaneIssue struct {
	ID              string
	Name            string
	DescriptionHTML string
	StateName       string
}

// CreatePlaneIssueRequest contains the fields for creating a Plane work item.
type CreatePlaneIssueRequest struct {
	Name            string `json:"name"`
	DescriptionHTML string `json:"description_html,omitempty"`
}

// UpdatePlaneIssueRequest contains the fields for updating a Plane work item.
type UpdatePlaneIssueRequest struct {
	Name            string `json:"name,omitempty"`
	DescriptionHTML string `json:"description_html,omitempty"`
}

// Engine orchestrates the sync cycle between GitHub and Plane.
type Engine struct {
	github GitHubClient
	plane  PlaneClient
	store  *store.Store
	logger *log.Logger
	cfg    *config.Config
}

// NewEngine creates a sync engine with the given dependencies.
func NewEngine(gh GitHubClient, pl PlaneClient, st *store.Store, logger *log.Logger, cfg *config.Config) *Engine {
	return &Engine{
		github: gh,
		plane:  pl,
		store:  st,
		logger: logger,
		cfg:    cfg,
	}
}

// SyncIssues runs one cycle of GitHub-to-Plane issue synchronization.
// For each configured mapping it:
//  1. Fetches open GitHub issues (using the stored since timestamp).
//  2. Creates Plane work items for unmapped issues.
//  3. Updates Plane work items for mapped issues whose title or body changed.
//  4. Persists mappings and updates the sync-state timestamp.
func (e *Engine) SyncIssues(ctx context.Context) error {
	for _, mapping := range e.cfg.Mappings {
		if err := e.syncRepo(ctx, mapping); err != nil {
			return fmt.Errorf("syncing %s/%s: %w", mapping.GitHub.Owner, mapping.GitHub.Repo, err)
		}
	}
	return nil
}

// syncRepo processes a single GitHub-repo-to-Plane-project mapping.
func (e *Engine) syncRepo(ctx context.Context, mapping config.Mapping) error {
	owner := mapping.GitHub.Owner
	repo := mapping.GitHub.Repo

	e.logger.Info("starting issue sync",
		"owner", owner,
		"repo", repo,
		"plane_project", mapping.Plane.ProjectID,
	)

	// Determine the "since" timestamp from the last successful sync.
	since, err := e.lastSyncedAt(owner, repo)
	if err != nil {
		return fmt.Errorf("getting last sync time: %w", err)
	}

	if !since.IsZero() {
		e.logger.Debug("fetching issues since last sync",
			"owner", owner,
			"repo", repo,
			"since", since.Format(time.RFC3339),
		)
	}

	issues, err := e.github.ListIssues(ctx, owner, repo, since)
	if err != nil {
		return fmt.Errorf("listing GitHub issues: %w", err)
	}

	e.logger.Info("fetched GitHub issues",
		"owner", owner,
		"repo", repo,
		"count", len(issues),
	)

	syncTime := time.Now().UTC()
	hadFailure := false

	for _, issue := range issues {
		if err := e.syncIssue(ctx, mapping, issue); err != nil {
			e.logger.Error("failed to sync issue",
				"owner", owner,
				"repo", repo,
				"issue_number", issue.Number,
				"error", err,
			)
			hadFailure = true
			// Continue syncing other issues rather than aborting the whole repo.
			continue
		}
	}

	// Only advance the sync-state timestamp when all issues were processed
	// successfully. If any issue failed, we leave the timestamp unchanged
	// so that the next cycle re-fetches those issues from GitHub.
	if hadFailure {
		e.logger.Warn("skipping sync state update due to failures",
			"owner", owner,
			"repo", repo,
		)
		return nil
	}

	if err := e.store.UpsertSyncState(store.SyncState{
		GitHubOwner:  owner,
		GitHubRepo:   repo,
		LastSyncedAt: syncTime,
	}); err != nil {
		return fmt.Errorf("updating sync state: %w", err)
	}

	return nil
}

// syncIssue handles a single GitHub issue: creates or updates the
// corresponding Plane work item.
func (e *Engine) syncIssue(ctx context.Context, mapping config.Mapping, issue GitHubIssue) error {
	owner := mapping.GitHub.Owner
	repo := mapping.GitHub.Repo

	existing, err := e.store.GetIssueMap(owner, repo, issue.Number)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("looking up issue map: %w", err)
	}

	if errors.Is(err, sql.ErrNoRows) {
		return e.createPlaneIssue(ctx, mapping, issue)
	}

	return e.updatePlaneIssue(ctx, mapping, issue, existing)
}

// createPlaneIssue creates a new Plane work item for an unmapped GitHub issue.
func (e *Engine) createPlaneIssue(ctx context.Context, mapping config.Mapping, issue GitHubIssue) error {
	owner := mapping.GitHub.Owner
	repo := mapping.GitHub.Repo
	descHTML := buildDescription(issue.Body, issue.HTMLURL)

	e.logger.LogAction(log.ActionIssueCreated, "creating Plane issue",
		"owner", owner,
		"repo", repo,
		"issue_number", issue.Number,
		"title", issue.Title,
	)

	if e.logger.DryRun() {
		return nil
	}

	planeIssue, err := e.plane.CreateIssue(ctx, e.cfg.Plane.Workspace, mapping.Plane.ProjectID, CreatePlaneIssueRequest{
		Name:            issue.Title,
		DescriptionHTML: descHTML,
	})
	if err != nil {
		return fmt.Errorf("creating Plane issue for %s/%s#%d: %w", owner, repo, issue.Number, err)
	}

	if err := e.store.UpsertIssueMap(store.IssueMap{
		GitHubOwner:       owner,
		GitHubRepo:        repo,
		GitHubIssueNumber: issue.Number,
		PlaneProjectID:    mapping.Plane.ProjectID,
		PlaneIssueID:      planeIssue.ID,
	}); err != nil {
		return fmt.Errorf("storing issue map for %s/%s#%d: %w", owner, repo, issue.Number, err)
	}

	e.logger.Debug("created Plane issue",
		"owner", owner,
		"repo", repo,
		"issue_number", issue.Number,
		"plane_issue_id", planeIssue.ID,
	)

	return nil
}

// updatePlaneIssue updates an existing Plane work item if the GitHub issue's
// title or body has changed.
func (e *Engine) updatePlaneIssue(ctx context.Context, mapping config.Mapping, issue GitHubIssue, existing store.IssueMap) error {
	owner := mapping.GitHub.Owner
	repo := mapping.GitHub.Repo
	descHTML := buildDescription(issue.Body, issue.HTMLURL)

	// Check if the issue was updated after our last sync of this mapping.
	if !issue.UpdatedAt.After(existing.UpdatedAt) {
		e.logger.LogAction(log.ActionSkipped, "issue unchanged since last sync",
			"owner", owner,
			"repo", repo,
			"issue_number", issue.Number,
		)
		return nil
	}

	e.logger.LogAction(log.ActionIssueUpdated, "updating Plane issue",
		"owner", owner,
		"repo", repo,
		"issue_number", issue.Number,
		"plane_issue_id", existing.PlaneIssueID,
		"title", issue.Title,
	)

	if e.logger.DryRun() {
		return nil
	}

	_, err := e.plane.UpdateIssue(ctx, e.cfg.Plane.Workspace, mapping.Plane.ProjectID, existing.PlaneIssueID, UpdatePlaneIssueRequest{
		Name:            issue.Title,
		DescriptionHTML: descHTML,
	})
	if err != nil {
		return fmt.Errorf("updating Plane issue for %s/%s#%d: %w", owner, repo, issue.Number, err)
	}

	// Touch the mapping's updated_at so we can skip it next cycle if unchanged.
	if err := e.store.UpsertIssueMap(store.IssueMap{
		GitHubOwner:       owner,
		GitHubRepo:        repo,
		GitHubIssueNumber: issue.Number,
		PlaneProjectID:    mapping.Plane.ProjectID,
		PlaneIssueID:      existing.PlaneIssueID,
		CreatedAt:         existing.CreatedAt,
	}); err != nil {
		return fmt.Errorf("updating issue map for %s/%s#%d: %w", owner, repo, issue.Number, err)
	}

	return nil
}

// SyncStates runs one cycle of Plane-to-GitHub state synchronization.
// For each configured mapping it:
//  1. Lists all mapped issues from the store.
//  2. Fetches the current Plane state for each mapped issue.
//  3. Looks up the desired GitHub state in the plane_to_github mapping.
//  4. Closes or reopens the GitHub issue as needed.
//  5. Adds a comment on the GitHub issue explaining the state change.
func (e *Engine) SyncStates(ctx context.Context) error {
	for _, mapping := range e.cfg.Mappings {
		if err := e.syncRepoStates(ctx, mapping); err != nil {
			return fmt.Errorf("syncing states for %s/%s: %w", mapping.GitHub.Owner, mapping.GitHub.Repo, err)
		}
	}
	return nil
}

// syncRepoStates processes state synchronization for a single mapping.
func (e *Engine) syncRepoStates(ctx context.Context, mapping config.Mapping) error {
	owner := mapping.GitHub.Owner
	repo := mapping.GitHub.Repo
	states := mapping.EffectiveStates(e.cfg.States)

	e.logger.Info("starting state sync",
		"owner", owner,
		"repo", repo,
		"plane_project", mapping.Plane.ProjectID,
	)

	issueMaps, err := e.store.ListIssueMaps(owner, repo)
	if err != nil {
		return fmt.Errorf("listing issue maps: %w", err)
	}

	e.logger.Debug("found mapped issues for state sync",
		"owner", owner,
		"repo", repo,
		"count", len(issueMaps),
	)

	for _, im := range issueMaps {
		if err := e.syncIssueState(ctx, mapping, states, im); err != nil {
			e.logger.Error("failed to sync issue state",
				"owner", owner,
				"repo", repo,
				"issue_number", im.GitHubIssueNumber,
				"error", err,
			)
			// Continue syncing other issues rather than aborting the whole repo.
			continue
		}
	}

	return nil
}

// syncIssueState handles state synchronization for a single mapped issue.
func (e *Engine) syncIssueState(ctx context.Context, mapping config.Mapping, states config.StateMappings, im store.IssueMap) error {
	owner := mapping.GitHub.Owner
	repo := mapping.GitHub.Repo

	// Fetch current Plane state.
	planeIssue, err := e.plane.GetIssue(ctx, e.cfg.Plane.Workspace, mapping.Plane.ProjectID, im.PlaneIssueID)
	if err != nil {
		return fmt.Errorf("fetching Plane issue %s: %w", im.PlaneIssueID, err)
	}

	// Look up the desired GitHub state from the Plane state name.
	desiredGHState, ok := states.PlaneToGitHub[planeIssue.StateName]
	if !ok {
		e.logger.Debug("no plane_to_github mapping for state, skipping",
			"owner", owner,
			"repo", repo,
			"issue_number", im.GitHubIssueNumber,
			"plane_state", planeIssue.StateName,
		)
		return nil
	}

	// Fetch current GitHub issue state.
	ghIssue, err := e.github.GetIssue(ctx, owner, repo, im.GitHubIssueNumber)
	if err != nil {
		return fmt.Errorf("fetching GitHub issue %s/%s#%d: %w", owner, repo, im.GitHubIssueNumber, err)
	}

	// If the states already match, nothing to do.
	if ghIssue.State == desiredGHState {
		e.logger.LogAction(log.ActionSkipped, "GitHub issue already in desired state",
			"owner", owner,
			"repo", repo,
			"issue_number", im.GitHubIssueNumber,
			"state", ghIssue.State,
		)
		return nil
	}

	e.logger.LogAction(log.ActionStateChanged, "syncing state from Plane to GitHub",
		"owner", owner,
		"repo", repo,
		"issue_number", im.GitHubIssueNumber,
		"plane_state", planeIssue.StateName,
		"github_current_state", ghIssue.State,
		"github_desired_state", desiredGHState,
	)

	if e.logger.DryRun() {
		return nil
	}

	// Close or reopen the GitHub issue.
	switch desiredGHState {
	case "closed":
		comment := fmt.Sprintf("Closed by Plane workflow (state: %s)", planeIssue.StateName)
		if err := e.github.CreateComment(ctx, owner, repo, im.GitHubIssueNumber, comment); err != nil {
			return fmt.Errorf("commenting on %s/%s#%d: %w", owner, repo, im.GitHubIssueNumber, err)
		}
		if err := e.github.CloseIssue(ctx, owner, repo, im.GitHubIssueNumber); err != nil {
			return fmt.Errorf("closing %s/%s#%d: %w", owner, repo, im.GitHubIssueNumber, err)
		}
	case "open":
		comment := fmt.Sprintf("Reopened by Plane workflow (state: %s)", planeIssue.StateName)
		if err := e.github.CreateComment(ctx, owner, repo, im.GitHubIssueNumber, comment); err != nil {
			return fmt.Errorf("commenting on %s/%s#%d: %w", owner, repo, im.GitHubIssueNumber, err)
		}
		if err := e.github.ReopenIssue(ctx, owner, repo, im.GitHubIssueNumber); err != nil {
			return fmt.Errorf("reopening %s/%s#%d: %w", owner, repo, im.GitHubIssueNumber, err)
		}
	default:
		e.logger.Warn("unknown desired GitHub state, skipping",
			"owner", owner,
			"repo", repo,
			"issue_number", im.GitHubIssueNumber,
			"desired_state", desiredGHState,
		)
	}

	return nil
}

// lastSyncedAt retrieves the last-synced timestamp for a repo, returning
// the zero time if no prior sync has been recorded.
func (e *Engine) lastSyncedAt(owner, repo string) (time.Time, error) {
	state, err := e.store.GetSyncState(owner, repo)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return state.LastSyncedAt, nil
}

// buildDescription creates the Plane issue description HTML, incorporating
// the original GitHub issue body and a link back to the GitHub issue.
// The body is HTML-escaped to prevent XSS from user-controlled content.
func buildDescription(body, htmlURL string) string {
	link := fmt.Sprintf(`<p><a href="%s">View on GitHub</a></p>`, html.EscapeString(htmlURL))
	if body == "" {
		return link
	}
	return fmt.Sprintf("<p>%s</p>\n%s", html.EscapeString(body), link)
}
