package sync

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mggarofalo/gh-plane-sync/internal/config"
	"github.com/mggarofalo/gh-plane-sync/internal/log"
	"github.com/mggarofalo/gh-plane-sync/internal/store"
)

// mockGitHubClient implements GitHubClient for testing.
type mockGitHubClient struct {
	issues        map[string][]GitHubIssue   // key: "owner/repo"
	issuesByNum   map[string]GitHubIssue     // key: "owner/repo#number"
	closedIssues  []string                   // "owner/repo#number"
	reopened      []string                   // "owner/repo#number"
	comments      map[string][]GitHubComment // key: "owner/repo#number" (for ListComments)
	stateComments []commentCall              // (for CreateComment tracking)
	err           error
	getIssueErr   error
	closeErr      error
	reopenErr     error
	commentErr    error // used by CreateComment (state sync)
	listCommentErr error // used by ListComments (comment sync)
}

type commentCall struct {
	owner  string
	repo   string
	number int
	body   string
}

func (m *mockGitHubClient) ListIssues(_ context.Context, owner, repo string, _ time.Time) ([]GitHubIssue, error) {
	if m.err != nil {
		return nil, m.err
	}
	key := owner + "/" + repo
	return m.issues[key], nil
}

func (m *mockGitHubClient) GetIssue(_ context.Context, owner, repo string, number int) (GitHubIssue, error) {
	if m.getIssueErr != nil {
		return GitHubIssue{}, m.getIssueErr
	}
	key := fmt.Sprintf("%s/%s#%d", owner, repo, number)
	issue, ok := m.issuesByNum[key]
	if !ok {
		return GitHubIssue{}, fmt.Errorf("issue not found: %s", key)
	}
	return issue, nil
}

func (m *mockGitHubClient) CloseIssue(_ context.Context, owner, repo string, number int) error {
	if m.closeErr != nil {
		return m.closeErr
	}
	key := fmt.Sprintf("%s/%s#%d", owner, repo, number)
	m.closedIssues = append(m.closedIssues, key)
	return nil
}

func (m *mockGitHubClient) ReopenIssue(_ context.Context, owner, repo string, number int) error {
	if m.reopenErr != nil {
		return m.reopenErr
	}
	key := fmt.Sprintf("%s/%s#%d", owner, repo, number)
	m.reopened = append(m.reopened, key)
	return nil
}

func (m *mockGitHubClient) CreateComment(_ context.Context, owner, repo string, number int, body string) error {
	if m.commentErr != nil {
		return m.commentErr
	}
	m.stateComments = append(m.stateComments, commentCall{owner: owner, repo: repo, number: number, body: body})
	return nil
}

func (m *mockGitHubClient) ListComments(_ context.Context, owner, repo string, issueNumber int) ([]GitHubComment, error) {
	if m.listCommentErr != nil {
		return nil, m.listCommentErr
	}
	key := fmt.Sprintf("%s/%s#%d", owner, repo, issueNumber)
	return m.comments[key], nil
}

// mockPlaneClient implements PlaneClient for testing.
type mockPlaneClient struct {
	created         []CreatePlaneIssueRequest
	updated         []updateCall
	createdComments []createCommentCall
	nextID          int
	nextCommentID   int
	err             error
	commentErr      error
	getIssueErr     error
	planeIssues     map[string]PlaneIssue // key: issueID
}

type updateCall struct {
	issueID string
	req     UpdatePlaneIssueRequest
}

type createCommentCall struct {
	issueID string
	req     CreatePlaneCommentRequest
}

func (m *mockPlaneClient) GetIssue(_ context.Context, _, _, issueID string) (PlaneIssue, error) {
	if m.getIssueErr != nil {
		return PlaneIssue{}, m.getIssueErr
	}
	issue, ok := m.planeIssues[issueID]
	if !ok {
		return PlaneIssue{}, fmt.Errorf("plane issue not found: %s", issueID)
	}
	return issue, nil
}

func (m *mockPlaneClient) CreateIssue(_ context.Context, _, _ string, req CreatePlaneIssueRequest) (PlaneIssue, error) {
	if m.err != nil {
		return PlaneIssue{}, m.err
	}
	m.nextID++
	id := fmt.Sprintf("plane-issue-%d", m.nextID)
	m.created = append(m.created, req)
	return PlaneIssue{
		ID:              id,
		Name:            req.Name,
		DescriptionHTML: req.DescriptionHTML,
	}, nil
}

func (m *mockPlaneClient) UpdateIssue(_ context.Context, _, _, issueID string, req UpdatePlaneIssueRequest) (PlaneIssue, error) {
	if m.err != nil {
		return PlaneIssue{}, m.err
	}
	m.updated = append(m.updated, updateCall{issueID: issueID, req: req})
	return PlaneIssue{
		ID:              issueID,
		Name:            req.Name,
		DescriptionHTML: req.DescriptionHTML,
	}, nil
}

func (m *mockPlaneClient) CreateComment(_ context.Context, _, _, issueID string, req CreatePlaneCommentRequest) (PlaneComment, error) {
	if m.commentErr != nil {
		return PlaneComment{}, m.commentErr
	}
	m.nextCommentID++
	id := fmt.Sprintf("plane-comment-%d", m.nextCommentID)
	m.createdComments = append(m.createdComments, createCommentCall{issueID: issueID, req: req})
	return PlaneComment{ID: id}, nil
}

// testStore creates a temporary SQLite store for testing.
func testStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// testLogger creates a logger that discards output for testing.
func testLogger(dryRun bool) *log.Logger {
	var buf bytes.Buffer
	return log.New(log.Options{
		Writer: &buf,
		Level:  log.LevelDebug,
		DryRun: dryRun,
	})
}

// testConfig returns a minimal config for testing.
func testConfig() *config.Config {
	return &config.Config{
		Plane: config.PlaneConfig{
			APIURL:    "https://plane.example.com",
			Workspace: "test-ws",
		},
		States: config.StateMappings{
			GitHubToPlane: map[string]string{"open": "Backlog"},
			PlaneToGitHub: map[string]string{"backlog": "open"},
		},
		Mappings: []config.Mapping{
			{
				GitHub: config.GitHubRepo{Owner: "owner", Repo: "repo"},
				Plane:  config.PlaneProject{ProjectID: "proj-1"},
			},
		},
	}
}

func TestSyncIssues(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name        string
		ghIssues    map[string][]GitHubIssue
		seedMaps    []store.IssueMap
		cfg         *config.Config
		dryRun      bool
		wantCreated int
		wantUpdated int
		wantErr     bool
		ghErr       error
		planeErr    error
	}{
		{
			name: "creates new issue",
			ghIssues: map[string][]GitHubIssue{
				"owner/repo": {
					{Number: 1, Title: "Bug", Body: "Fix it", State: "open", HTMLURL: "https://github.com/owner/repo/issues/1", UpdatedAt: now},
				},
			},
			cfg:         testConfig(),
			wantCreated: 1,
		},
		{
			name: "updates existing issue when changed",
			ghIssues: map[string][]GitHubIssue{
				"owner/repo": {
					{Number: 1, Title: "Updated Bug", Body: "Fix it v2", State: "open", HTMLURL: "https://github.com/owner/repo/issues/1", UpdatedAt: now},
				},
			},
			seedMaps: []store.IssueMap{
				{
					GitHubOwner:       "owner",
					GitHubRepo:        "repo",
					GitHubIssueNumber: 1,
					PlaneProjectID:    "proj-1",
					PlaneIssueID:      "existing-plane-id",
					CreatedAt:         now.Add(-2 * time.Hour),
				},
			},
			cfg:         testConfig(),
			wantUpdated: 1,
		},
		{
			name: "skips unchanged issue",
			ghIssues: map[string][]GitHubIssue{
				"owner/repo": {
					{Number: 1, Title: "Same", Body: "Same", State: "open", HTMLURL: "https://github.com/owner/repo/issues/1", UpdatedAt: now.Add(-1 * time.Hour)},
				},
			},
			seedMaps: []store.IssueMap{
				{
					GitHubOwner:       "owner",
					GitHubRepo:        "repo",
					GitHubIssueNumber: 1,
					PlaneProjectID:    "proj-1",
					PlaneIssueID:      "existing-plane-id",
				},
			},
			cfg: testConfig(),
		},
		{
			name: "creates multiple issues",
			ghIssues: map[string][]GitHubIssue{
				"owner/repo": {
					{Number: 1, Title: "Bug 1", Body: "", State: "open", HTMLURL: "https://github.com/owner/repo/issues/1", UpdatedAt: now},
					{Number: 2, Title: "Bug 2", Body: "", State: "open", HTMLURL: "https://github.com/owner/repo/issues/2", UpdatedAt: now},
					{Number: 3, Title: "Bug 3", Body: "", State: "open", HTMLURL: "https://github.com/owner/repo/issues/3", UpdatedAt: now},
				},
			},
			cfg:         testConfig(),
			wantCreated: 3,
		},
		{
			name: "no issues returns without error",
			ghIssues: map[string][]GitHubIssue{
				"owner/repo": {},
			},
			cfg: testConfig(),
		},
		{
			name: "dry run does not create issues",
			ghIssues: map[string][]GitHubIssue{
				"owner/repo": {
					{Number: 1, Title: "Bug", Body: "Fix it", State: "open", HTMLURL: "https://github.com/owner/repo/issues/1", UpdatedAt: now},
				},
			},
			cfg:    testConfig(),
			dryRun: true,
		},
		{
			name:    "github error returns error",
			cfg:     testConfig(),
			ghErr:   fmt.Errorf("API rate limited"),
			wantErr: true,
		},
		{
			name: "plane error continues to next issue",
			ghIssues: map[string][]GitHubIssue{
				"owner/repo": {
					{Number: 1, Title: "Bug", Body: "", State: "open", HTMLURL: "https://github.com/owner/repo/issues/1", UpdatedAt: now},
				},
			},
			cfg:      testConfig(),
			planeErr: fmt.Errorf("Plane API error"),
			// Should not return error; the engine logs and continues.
		},
		{
			name: "multiple mappings",
			ghIssues: map[string][]GitHubIssue{
				"owner/repo-a": {
					{Number: 1, Title: "Issue A", Body: "", State: "open", HTMLURL: "https://github.com/owner/repo-a/issues/1", UpdatedAt: now},
				},
				"owner/repo-b": {
					{Number: 1, Title: "Issue B", Body: "", State: "open", HTMLURL: "https://github.com/owner/repo-b/issues/1", UpdatedAt: now},
				},
			},
			cfg: &config.Config{
				Plane: config.PlaneConfig{
					APIURL:    "https://plane.example.com",
					Workspace: "test-ws",
				},
				States: config.StateMappings{
					GitHubToPlane: map[string]string{"open": "Backlog"},
					PlaneToGitHub: map[string]string{"backlog": "open"},
				},
				Mappings: []config.Mapping{
					{
						GitHub: config.GitHubRepo{Owner: "owner", Repo: "repo-a"},
						Plane:  config.PlaneProject{ProjectID: "proj-a"},
					},
					{
						GitHub: config.GitHubRepo{Owner: "owner", Repo: "repo-b"},
						Plane:  config.PlaneProject{ProjectID: "proj-b"},
					},
				},
			},
			wantCreated: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := testStore(t)
			logger := testLogger(tc.dryRun)
			gh := &mockGitHubClient{issues: tc.ghIssues, err: tc.ghErr}
			pl := &mockPlaneClient{err: tc.planeErr}

			// Seed existing mappings.
			for _, m := range tc.seedMaps {
				if err := st.UpsertIssueMap(m); err != nil {
					t.Fatalf("seeding issue map: %v", err)
				}
			}

			engine := NewEngine(gh, pl, st, logger, tc.cfg)
			err := engine.SyncIssues(context.Background())

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("SyncIssues: %v", err)
			}

			if len(pl.created) != tc.wantCreated {
				t.Errorf("created %d issues, want %d", len(pl.created), tc.wantCreated)
			}
			if len(pl.updated) != tc.wantUpdated {
				t.Errorf("updated %d issues, want %d", len(pl.updated), tc.wantUpdated)
			}
		})
	}
}

func TestSyncIssuesStoresMappings(t *testing.T) {
	now := time.Now().UTC()
	st := testStore(t)
	logger := testLogger(false)
	gh := &mockGitHubClient{
		issues: map[string][]GitHubIssue{
			"owner/repo": {
				{Number: 42, Title: "New issue", Body: "Body text", State: "open", HTMLURL: "https://github.com/owner/repo/issues/42", UpdatedAt: now},
			},
		},
	}
	pl := &mockPlaneClient{}

	engine := NewEngine(gh, pl, st, logger, testConfig())
	if err := engine.SyncIssues(context.Background()); err != nil {
		t.Fatalf("SyncIssues: %v", err)
	}

	// Verify the mapping was stored.
	m, err := st.GetIssueMap("owner", "repo", 42)
	if err != nil {
		t.Fatalf("GetIssueMap: %v", err)
	}
	if m.PlaneProjectID != "proj-1" {
		t.Errorf("PlaneProjectID = %q, want %q", m.PlaneProjectID, "proj-1")
	}
	if m.PlaneIssueID == "" {
		t.Error("PlaneIssueID should not be empty")
	}
}

func TestSyncIssuesUpdatesSyncState(t *testing.T) {
	st := testStore(t)
	logger := testLogger(false)
	gh := &mockGitHubClient{
		issues: map[string][]GitHubIssue{
			"owner/repo": {},
		},
	}
	pl := &mockPlaneClient{}

	engine := NewEngine(gh, pl, st, logger, testConfig())
	if err := engine.SyncIssues(context.Background()); err != nil {
		t.Fatalf("SyncIssues: %v", err)
	}

	// Verify sync state was recorded.
	state, err := st.GetSyncState("owner", "repo")
	if err != nil {
		t.Fatalf("GetSyncState: %v", err)
	}
	if state.LastSyncedAt.IsZero() {
		t.Error("LastSyncedAt should not be zero")
	}
}

func TestSyncIssuesDryRunNoSideEffects(t *testing.T) {
	now := time.Now().UTC()
	st := testStore(t)
	logger := testLogger(true)
	gh := &mockGitHubClient{
		issues: map[string][]GitHubIssue{
			"owner/repo": {
				{Number: 1, Title: "Bug", Body: "Fix", State: "open", HTMLURL: "https://github.com/owner/repo/issues/1", UpdatedAt: now},
			},
		},
	}
	pl := &mockPlaneClient{}

	engine := NewEngine(gh, pl, st, logger, testConfig())
	if err := engine.SyncIssues(context.Background()); err != nil {
		t.Fatalf("SyncIssues: %v", err)
	}

	// Verify no Plane API calls were made.
	if len(pl.created) != 0 {
		t.Errorf("dry run should not create issues, got %d", len(pl.created))
	}

	// Verify no mapping was stored.
	_, err := st.GetIssueMap("owner", "repo", 1)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected ErrNoRows in dry run, got %v", err)
	}
}

func TestBuildDescription(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		htmlURL string
		want    string
	}{
		{
			name:    "with body",
			body:    "Fix the bug",
			htmlURL: "https://github.com/o/r/issues/1",
			want:    "<p>Fix the bug</p>\n<p><a href=\"https://github.com/o/r/issues/1\">View on GitHub</a></p>",
		},
		{
			name:    "empty body",
			body:    "",
			htmlURL: "https://github.com/o/r/issues/2",
			want:    "<p><a href=\"https://github.com/o/r/issues/2\">View on GitHub</a></p>",
		},
		{
			name:    "body with HTML is escaped",
			body:    `</p><script>alert("xss")</script><p>`,
			htmlURL: "https://github.com/o/r/issues/3",
			want:    "<p>&lt;/p&gt;&lt;script&gt;alert(&#34;xss&#34;)&lt;/script&gt;&lt;p&gt;</p>\n<p><a href=\"https://github.com/o/r/issues/3\">View on GitHub</a></p>",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildDescription(tc.body, tc.htmlURL)
			if got != tc.want {
				t.Errorf("buildDescription(%q, %q) = %q, want %q", tc.body, tc.htmlURL, got, tc.want)
			}
		})
	}
}

func TestSyncIssuesUsesLastSyncedTimestamp(t *testing.T) {
	st := testStore(t)
	logger := testLogger(false)

	lastSync := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	if err := st.UpsertSyncState(store.SyncState{
		GitHubOwner:  "owner",
		GitHubRepo:   "repo",
		LastSyncedAt: lastSync,
	}); err != nil {
		t.Fatalf("seeding sync state: %v", err)
	}

	var capturedSince time.Time
	gh := &mockGitHubClientWithSince{
		captureSince: &capturedSince,
	}
	pl := &mockPlaneClient{}

	engine := NewEngine(gh, pl, st, logger, testConfig())
	if err := engine.SyncIssues(context.Background()); err != nil {
		t.Fatalf("SyncIssues: %v", err)
	}

	if !capturedSince.Equal(lastSync) {
		t.Errorf("since = %v, want %v", capturedSince, lastSync)
	}
}

func TestSyncIssuesDoesNotAdvanceSyncStateOnFailure(t *testing.T) {
	now := time.Now().UTC()
	st := testStore(t)
	logger := testLogger(false)

	// Seed a sync state so we can verify it doesn't advance.
	originalSync := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := st.UpsertSyncState(store.SyncState{
		GitHubOwner:  "owner",
		GitHubRepo:   "repo",
		LastSyncedAt: originalSync,
	}); err != nil {
		t.Fatalf("seeding sync state: %v", err)
	}

	gh := &mockGitHubClient{
		issues: map[string][]GitHubIssue{
			"owner/repo": {
				{Number: 1, Title: "Bug", Body: "", State: "open", HTMLURL: "https://github.com/owner/repo/issues/1", UpdatedAt: now},
			},
		},
	}
	pl := &mockPlaneClient{err: fmt.Errorf("Plane API unavailable")}

	engine := NewEngine(gh, pl, st, logger, testConfig())
	// Should not return error (per-issue failures are logged, not propagated).
	if err := engine.SyncIssues(context.Background()); err != nil {
		t.Fatalf("SyncIssues: %v", err)
	}

	// Verify sync state was NOT advanced past the original timestamp.
	state, err := st.GetSyncState("owner", "repo")
	if err != nil {
		t.Fatalf("GetSyncState: %v", err)
	}
	if !state.LastSyncedAt.Equal(originalSync) {
		t.Errorf("LastSyncedAt = %v, want %v (should not advance on failure)", state.LastSyncedAt, originalSync)
	}
}

// mockGitHubClientWithSince captures the since parameter.
type mockGitHubClientWithSince struct {
	captureSince *time.Time
}

func (m *mockGitHubClientWithSince) ListIssues(_ context.Context, _, _ string, since time.Time) ([]GitHubIssue, error) {
	*m.captureSince = since
	return nil, nil
}

func (m *mockGitHubClientWithSince) GetIssue(_ context.Context, _, _ string, _ int) (GitHubIssue, error) {
	return GitHubIssue{}, nil
}

func (m *mockGitHubClientWithSince) CloseIssue(_ context.Context, _, _ string, _ int) error {
	return nil
}

func (m *mockGitHubClientWithSince) ReopenIssue(_ context.Context, _, _ string, _ int) error {
	return nil
}

func (m *mockGitHubClientWithSince) CreateComment(_ context.Context, _, _ string, _ int, _ string) error {
	return nil
}

func (m *mockGitHubClientWithSince) ListComments(_ context.Context, _, _ string, _ int) ([]GitHubComment, error) {
	return nil, nil
}

// testStateConfig returns a config with full state mappings for state sync tests.
func testStateConfig() *config.Config {
	return &config.Config{
		Plane: config.PlaneConfig{
			APIURL:    "https://plane.example.com",
			Workspace: "test-ws",
		},
		States: config.StateMappings{
			GitHubToPlane: map[string]string{
				"open":   "In Progress",
				"closed": "Done",
			},
			PlaneToGitHub: map[string]string{
				"Done":        "closed",
				"Cancelled":   "closed",
				"In Progress": "open",
				"Backlog":     "open",
			},
		},
		Mappings: []config.Mapping{
			{
				GitHub: config.GitHubRepo{Owner: "owner", Repo: "repo"},
				Plane:  config.PlaneProject{ProjectID: "proj-1"},
			},
		},
	}
}

func TestSyncStates(t *testing.T) {
	tests := []struct {
		name         string
		seedMaps     []store.IssueMap
		ghIssues     map[string]GitHubIssue // key: "owner/repo#number"
		planeIssues  map[string]PlaneIssue  // key: plane issue ID
		cfg          *config.Config
		dryRun       bool
		wantClosed   int
		wantReopened int
		wantComments int
		wantErr      bool
		ghGetErr     error
		ghCloseErr   error
		ghReopenErr  error
		ghCommentErr error
		plGetErr     error
	}{
		{
			name: "closes GitHub issue when Plane state is Done",
			seedMaps: []store.IssueMap{
				{
					GitHubOwner:       "owner",
					GitHubRepo:        "repo",
					GitHubIssueNumber: 1,
					PlaneProjectID:    "proj-1",
					PlaneIssueID:      "plane-1",
				},
			},
			ghIssues: map[string]GitHubIssue{
				"owner/repo#1": {Number: 1, State: "open"},
			},
			planeIssues: map[string]PlaneIssue{
				"plane-1": {ID: "plane-1", StateName: "Done"},
			},
			cfg:          testStateConfig(),
			wantClosed:   1,
			wantComments: 1,
		},
		{
			name: "closes GitHub issue when Plane state is Cancelled",
			seedMaps: []store.IssueMap{
				{
					GitHubOwner:       "owner",
					GitHubRepo:        "repo",
					GitHubIssueNumber: 2,
					PlaneProjectID:    "proj-1",
					PlaneIssueID:      "plane-2",
				},
			},
			ghIssues: map[string]GitHubIssue{
				"owner/repo#2": {Number: 2, State: "open"},
			},
			planeIssues: map[string]PlaneIssue{
				"plane-2": {ID: "plane-2", StateName: "Cancelled"},
			},
			cfg:          testStateConfig(),
			wantClosed:   1,
			wantComments: 1,
		},
		{
			name: "reopens GitHub issue when Plane state maps to open",
			seedMaps: []store.IssueMap{
				{
					GitHubOwner:       "owner",
					GitHubRepo:        "repo",
					GitHubIssueNumber: 3,
					PlaneProjectID:    "proj-1",
					PlaneIssueID:      "plane-3",
				},
			},
			ghIssues: map[string]GitHubIssue{
				"owner/repo#3": {Number: 3, State: "closed"},
			},
			planeIssues: map[string]PlaneIssue{
				"plane-3": {ID: "plane-3", StateName: "In Progress"},
			},
			cfg:          testStateConfig(),
			wantReopened: 1,
			wantComments: 1,
		},
		{
			name: "skips when GitHub state already matches",
			seedMaps: []store.IssueMap{
				{
					GitHubOwner:       "owner",
					GitHubRepo:        "repo",
					GitHubIssueNumber: 4,
					PlaneProjectID:    "proj-1",
					PlaneIssueID:      "plane-4",
				},
			},
			ghIssues: map[string]GitHubIssue{
				"owner/repo#4": {Number: 4, State: "open"},
			},
			planeIssues: map[string]PlaneIssue{
				"plane-4": {ID: "plane-4", StateName: "In Progress"},
			},
			cfg: testStateConfig(),
		},
		{
			name: "skips when Plane state has no mapping",
			seedMaps: []store.IssueMap{
				{
					GitHubOwner:       "owner",
					GitHubRepo:        "repo",
					GitHubIssueNumber: 5,
					PlaneProjectID:    "proj-1",
					PlaneIssueID:      "plane-5",
				},
			},
			ghIssues: map[string]GitHubIssue{
				"owner/repo#5": {Number: 5, State: "open"},
			},
			planeIssues: map[string]PlaneIssue{
				"plane-5": {ID: "plane-5", StateName: "UnmappedState"},
			},
			cfg: testStateConfig(),
		},
		{
			name:     "no mapped issues returns without error",
			cfg:      testStateConfig(),
			ghIssues: map[string]GitHubIssue{},
		},
		{
			name: "dry run does not close or reopen",
			seedMaps: []store.IssueMap{
				{
					GitHubOwner:       "owner",
					GitHubRepo:        "repo",
					GitHubIssueNumber: 6,
					PlaneProjectID:    "proj-1",
					PlaneIssueID:      "plane-6",
				},
			},
			ghIssues: map[string]GitHubIssue{
				"owner/repo#6": {Number: 6, State: "open"},
			},
			planeIssues: map[string]PlaneIssue{
				"plane-6": {ID: "plane-6", StateName: "Done"},
			},
			cfg:    testStateConfig(),
			dryRun: true,
		},
		{
			name: "uses per-mapping state overrides",
			seedMaps: []store.IssueMap{
				{
					GitHubOwner:       "owner",
					GitHubRepo:        "repo",
					GitHubIssueNumber: 7,
					PlaneProjectID:    "proj-1",
					PlaneIssueID:      "plane-7",
				},
			},
			ghIssues: map[string]GitHubIssue{
				"owner/repo#7": {Number: 7, State: "open"},
			},
			planeIssues: map[string]PlaneIssue{
				"plane-7": {ID: "plane-7", StateName: "Deployed"},
			},
			cfg: &config.Config{
				Plane: config.PlaneConfig{
					APIURL:    "https://plane.example.com",
					Workspace: "test-ws",
				},
				States: config.StateMappings{
					GitHubToPlane: map[string]string{"open": "Backlog"},
					PlaneToGitHub: map[string]string{"Backlog": "open"},
				},
				Mappings: []config.Mapping{
					{
						GitHub: config.GitHubRepo{Owner: "owner", Repo: "repo"},
						Plane:  config.PlaneProject{ProjectID: "proj-1"},
						States: &config.StateMappings{
							GitHubToPlane: map[string]string{"closed": "Deployed"},
							PlaneToGitHub: map[string]string{"Deployed": "closed"},
						},
					},
				},
			},
			wantClosed:   1,
			wantComments: 1,
		},
		{
			name: "plane get error continues to next issue",
			seedMaps: []store.IssueMap{
				{
					GitHubOwner:       "owner",
					GitHubRepo:        "repo",
					GitHubIssueNumber: 8,
					PlaneProjectID:    "proj-1",
					PlaneIssueID:      "plane-8",
				},
				{
					GitHubOwner:       "owner",
					GitHubRepo:        "repo",
					GitHubIssueNumber: 9,
					PlaneProjectID:    "proj-1",
					PlaneIssueID:      "plane-9",
				},
			},
			ghIssues: map[string]GitHubIssue{
				"owner/repo#9": {Number: 9, State: "open"},
			},
			planeIssues: map[string]PlaneIssue{
				// plane-8 is missing, will cause error via mock
				"plane-9": {ID: "plane-9", StateName: "Done"},
			},
			cfg:          testStateConfig(),
			wantClosed:   1,
			wantComments: 1,
		},
		{
			name: "multiple issues synced",
			seedMaps: []store.IssueMap{
				{
					GitHubOwner:       "owner",
					GitHubRepo:        "repo",
					GitHubIssueNumber: 10,
					PlaneProjectID:    "proj-1",
					PlaneIssueID:      "plane-10",
				},
				{
					GitHubOwner:       "owner",
					GitHubRepo:        "repo",
					GitHubIssueNumber: 11,
					PlaneProjectID:    "proj-1",
					PlaneIssueID:      "plane-11",
				},
			},
			ghIssues: map[string]GitHubIssue{
				"owner/repo#10": {Number: 10, State: "open"},
				"owner/repo#11": {Number: 11, State: "closed"},
			},
			planeIssues: map[string]PlaneIssue{
				"plane-10": {ID: "plane-10", StateName: "Done"},
				"plane-11": {ID: "plane-11", StateName: "In Progress"},
			},
			cfg:          testStateConfig(),
			wantClosed:   1,
			wantReopened: 1,
			wantComments: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := testStore(t)
			logger := testLogger(tc.dryRun)
			gh := &mockGitHubClient{
				issuesByNum: tc.ghIssues,
				getIssueErr: tc.ghGetErr,
				closeErr:    tc.ghCloseErr,
				reopenErr:   tc.ghReopenErr,
				commentErr:  tc.ghCommentErr,
			}
			pl := &mockPlaneClient{
				planeIssues: tc.planeIssues,
				getIssueErr: tc.plGetErr,
			}

			// Seed existing mappings.
			for _, m := range tc.seedMaps {
				if err := st.UpsertIssueMap(m); err != nil {
					t.Fatalf("seeding issue map: %v", err)
				}
			}

			engine := NewEngine(gh, pl, st, logger, tc.cfg)
			err := engine.SyncStates(context.Background())

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("SyncStates: %v", err)
			}

			if len(gh.closedIssues) != tc.wantClosed {
				t.Errorf("closed %d issues, want %d", len(gh.closedIssues), tc.wantClosed)
			}
			if len(gh.reopened) != tc.wantReopened {
				t.Errorf("reopened %d issues, want %d", len(gh.reopened), tc.wantReopened)
			}
			if len(gh.stateComments) != tc.wantComments {
				t.Errorf("created %d comments, want %d", len(gh.stateComments), tc.wantComments)
			}
		})
	}
}

func TestSyncStatesCommentContent(t *testing.T) {
	st := testStore(t)
	logger := testLogger(false)

	if err := st.UpsertIssueMap(store.IssueMap{
		GitHubOwner:       "owner",
		GitHubRepo:        "repo",
		GitHubIssueNumber: 1,
		PlaneProjectID:    "proj-1",
		PlaneIssueID:      "plane-1",
	}); err != nil {
		t.Fatalf("seeding issue map: %v", err)
	}

	gh := &mockGitHubClient{
		issuesByNum: map[string]GitHubIssue{
			"owner/repo#1": {Number: 1, State: "open"},
		},
	}
	pl := &mockPlaneClient{
		planeIssues: map[string]PlaneIssue{
			"plane-1": {ID: "plane-1", StateName: "Done"},
		},
	}

	engine := NewEngine(gh, pl, st, logger, testStateConfig())
	if err := engine.SyncStates(context.Background()); err != nil {
		t.Fatalf("SyncStates: %v", err)
	}

	if len(gh.stateComments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(gh.stateComments))
	}

	want := "Closed by Plane workflow (state: Done)"
	if gh.stateComments[0].body != want {
		t.Errorf("comment body = %q, want %q", gh.stateComments[0].body, want)
	}
}

func TestSyncStatesReopenCommentContent(t *testing.T) {
	st := testStore(t)
	logger := testLogger(false)

	if err := st.UpsertIssueMap(store.IssueMap{
		GitHubOwner:       "owner",
		GitHubRepo:        "repo",
		GitHubIssueNumber: 1,
		PlaneProjectID:    "proj-1",
		PlaneIssueID:      "plane-1",
	}); err != nil {
		t.Fatalf("seeding issue map: %v", err)
	}

	gh := &mockGitHubClient{
		issuesByNum: map[string]GitHubIssue{
			"owner/repo#1": {Number: 1, State: "closed"},
		},
	}
	pl := &mockPlaneClient{
		planeIssues: map[string]PlaneIssue{
			"plane-1": {ID: "plane-1", StateName: "In Progress"},
		},
	}

	engine := NewEngine(gh, pl, st, logger, testStateConfig())
	if err := engine.SyncStates(context.Background()); err != nil {
		t.Fatalf("SyncStates: %v", err)
	}

	if len(gh.stateComments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(gh.stateComments))
	}

	want := "Reopened by Plane workflow (state: In Progress)"
	if gh.stateComments[0].body != want {
		t.Errorf("comment body = %q, want %q", gh.stateComments[0].body, want)
	}
}

func TestSyncStatesDryRunCountsActions(t *testing.T) {
	st := testStore(t)
	logger := testLogger(true)

	if err := st.UpsertIssueMap(store.IssueMap{
		GitHubOwner:       "owner",
		GitHubRepo:        "repo",
		GitHubIssueNumber: 1,
		PlaneProjectID:    "proj-1",
		PlaneIssueID:      "plane-1",
	}); err != nil {
		t.Fatalf("seeding issue map: %v", err)
	}

	gh := &mockGitHubClient{
		issuesByNum: map[string]GitHubIssue{
			"owner/repo#1": {Number: 1, State: "open"},
		},
	}
	pl := &mockPlaneClient{
		planeIssues: map[string]PlaneIssue{
			"plane-1": {ID: "plane-1", StateName: "Done"},
		},
	}

	engine := NewEngine(gh, pl, st, logger, testStateConfig())
	if err := engine.SyncStates(context.Background()); err != nil {
		t.Fatalf("SyncStates: %v", err)
	}

	// Verify the state change action was counted even in dry-run mode.
	counters := logger.Counters()
	if counters[log.ActionStateChanged] != 1 {
		t.Errorf("state_changed counter = %d, want 1", counters[log.ActionStateChanged])
	}

	// Verify no actual API calls were made.
	if len(gh.closedIssues) != 0 {
		t.Errorf("dry run should not close issues, got %d", len(gh.closedIssues))
	}
	if len(gh.stateComments) != 0 {
		t.Errorf("dry run should not create comments, got %d", len(gh.stateComments))
	}
}

func TestSyncComments(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name                string
		ghComments          map[string][]GitHubComment
		seedIssueMaps       []store.IssueMap
		seedSyncedComments  []store.SyncedComment
		cfg                 *config.Config
		dryRun              bool
		wantCreatedComments int
		wantErr             bool
		ghCommentErr        error
		planeCommentErr     error
	}{
		{
			name: "syncs new comments",
			ghComments: map[string][]GitHubComment{
				"owner/repo#1": {
					{ID: 100, Body: "Hello", User: "alice", CreatedAt: now, HTMLURL: "https://github.com/owner/repo/issues/1#issuecomment-100"},
					{ID: 101, Body: "World", User: "bob", CreatedAt: now, HTMLURL: "https://github.com/owner/repo/issues/1#issuecomment-101"},
				},
			},
			seedIssueMaps: []store.IssueMap{
				{GitHubOwner: "owner", GitHubRepo: "repo", GitHubIssueNumber: 1, PlaneProjectID: "proj-1", PlaneIssueID: "plane-issue-1"},
			},
			cfg:                 testConfig(),
			wantCreatedComments: 2,
		},
		{
			name: "skips already synced comments",
			ghComments: map[string][]GitHubComment{
				"owner/repo#1": {
					{ID: 100, Body: "Already synced", User: "alice", CreatedAt: now, HTMLURL: "https://github.com/owner/repo/issues/1#issuecomment-100"},
					{ID: 101, Body: "New comment", User: "bob", CreatedAt: now, HTMLURL: "https://github.com/owner/repo/issues/1#issuecomment-101"},
				},
			},
			seedIssueMaps: []store.IssueMap{
				{GitHubOwner: "owner", GitHubRepo: "repo", GitHubIssueNumber: 1, PlaneProjectID: "proj-1", PlaneIssueID: "plane-issue-1"},
			},
			seedSyncedComments: []store.SyncedComment{
				{GitHubCommentID: 100, PlaneCommentID: "existing-plane-comment", GitHubIssueNumber: 1, GitHubOwner: "owner", GitHubRepo: "repo"},
			},
			cfg:                 testConfig(),
			wantCreatedComments: 1,
		},
		{
			name:       "no mapped issues skips gracefully",
			ghComments: map[string][]GitHubComment{},
			cfg:        testConfig(),
		},
		{
			name: "no comments on issue",
			ghComments: map[string][]GitHubComment{
				"owner/repo#1": {},
			},
			seedIssueMaps: []store.IssueMap{
				{GitHubOwner: "owner", GitHubRepo: "repo", GitHubIssueNumber: 1, PlaneProjectID: "proj-1", PlaneIssueID: "plane-issue-1"},
			},
			cfg: testConfig(),
		},
		{
			name: "dry run does not create comments",
			ghComments: map[string][]GitHubComment{
				"owner/repo#1": {
					{ID: 100, Body: "Hello", User: "alice", CreatedAt: now, HTMLURL: "https://github.com/owner/repo/issues/1#issuecomment-100"},
				},
			},
			seedIssueMaps: []store.IssueMap{
				{GitHubOwner: "owner", GitHubRepo: "repo", GitHubIssueNumber: 1, PlaneProjectID: "proj-1", PlaneIssueID: "plane-issue-1"},
			},
			cfg:    testConfig(),
			dryRun: true,
		},
		{
			name: "github comment error continues to next issue",
			ghComments: map[string][]GitHubComment{
				"owner/repo#1": {
					{ID: 100, Body: "Hello", User: "alice", CreatedAt: now, HTMLURL: "https://github.com/owner/repo/issues/1#issuecomment-100"},
				},
			},
			seedIssueMaps: []store.IssueMap{
				{GitHubOwner: "owner", GitHubRepo: "repo", GitHubIssueNumber: 1, PlaneProjectID: "proj-1", PlaneIssueID: "plane-issue-1"},
			},
			cfg:          testConfig(),
			ghCommentErr: fmt.Errorf("GitHub API error"),
			// Per-issue errors are logged, not propagated.
		},
		{
			name: "plane comment error continues to next issue",
			ghComments: map[string][]GitHubComment{
				"owner/repo#1": {
					{ID: 100, Body: "Hello", User: "alice", CreatedAt: now, HTMLURL: "https://github.com/owner/repo/issues/1#issuecomment-100"},
				},
			},
			seedIssueMaps: []store.IssueMap{
				{GitHubOwner: "owner", GitHubRepo: "repo", GitHubIssueNumber: 1, PlaneProjectID: "proj-1", PlaneIssueID: "plane-issue-1"},
			},
			cfg:             testConfig(),
			planeCommentErr: fmt.Errorf("Plane API error"),
			// Per-issue errors are logged, not propagated.
		},
		{
			name: "multiple issues with comments",
			ghComments: map[string][]GitHubComment{
				"owner/repo#1": {
					{ID: 100, Body: "Comment on 1", User: "alice", CreatedAt: now, HTMLURL: "https://github.com/owner/repo/issues/1#issuecomment-100"},
				},
				"owner/repo#2": {
					{ID: 200, Body: "Comment on 2", User: "bob", CreatedAt: now, HTMLURL: "https://github.com/owner/repo/issues/2#issuecomment-200"},
					{ID: 201, Body: "Another on 2", User: "carol", CreatedAt: now, HTMLURL: "https://github.com/owner/repo/issues/2#issuecomment-201"},
				},
			},
			seedIssueMaps: []store.IssueMap{
				{GitHubOwner: "owner", GitHubRepo: "repo", GitHubIssueNumber: 1, PlaneProjectID: "proj-1", PlaneIssueID: "plane-issue-1"},
				{GitHubOwner: "owner", GitHubRepo: "repo", GitHubIssueNumber: 2, PlaneProjectID: "proj-1", PlaneIssueID: "plane-issue-2"},
			},
			cfg:                 testConfig(),
			wantCreatedComments: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := testStore(t)
			logger := testLogger(tc.dryRun)
			gh := &mockGitHubClient{
				comments:       tc.ghComments,
				listCommentErr: tc.ghCommentErr,
			}
			pl := &mockPlaneClient{commentErr: tc.planeCommentErr}

			// Seed existing issue maps.
			for _, m := range tc.seedIssueMaps {
				if err := st.UpsertIssueMap(m); err != nil {
					t.Fatalf("seeding issue map: %v", err)
				}
			}

			// Seed existing synced comments.
			for _, c := range tc.seedSyncedComments {
				if err := st.UpsertSyncedComment(c); err != nil {
					t.Fatalf("seeding synced comment: %v", err)
				}
			}

			engine := NewEngine(gh, pl, st, logger, tc.cfg)
			err := engine.SyncComments(context.Background())

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("SyncComments: %v", err)
			}

			if len(pl.createdComments) != tc.wantCreatedComments {
				t.Errorf("created %d comments, want %d", len(pl.createdComments), tc.wantCreatedComments)
			}
		})
	}
}

func TestSyncCommentsStoresMappings(t *testing.T) {
	now := time.Now().UTC()
	st := testStore(t)
	logger := testLogger(false)

	// Seed an issue map.
	if err := st.UpsertIssueMap(store.IssueMap{
		GitHubOwner: "owner", GitHubRepo: "repo", GitHubIssueNumber: 1,
		PlaneProjectID: "proj-1", PlaneIssueID: "plane-issue-1",
	}); err != nil {
		t.Fatalf("seeding issue map: %v", err)
	}

	gh := &mockGitHubClient{
		comments: map[string][]GitHubComment{
			"owner/repo#1": {
				{ID: 42, Body: "Test comment", User: "alice", CreatedAt: now, HTMLURL: "https://github.com/owner/repo/issues/1#issuecomment-42"},
			},
		},
	}
	pl := &mockPlaneClient{}

	engine := NewEngine(gh, pl, st, logger, testConfig())
	if err := engine.SyncComments(context.Background()); err != nil {
		t.Fatalf("SyncComments: %v", err)
	}

	// Verify the synced comment was stored.
	sc, err := st.GetSyncedComment(42)
	if err != nil {
		t.Fatalf("GetSyncedComment: %v", err)
	}
	if sc.GitHubIssueNumber != 1 {
		t.Errorf("GitHubIssueNumber = %d, want 1", sc.GitHubIssueNumber)
	}
	if sc.PlaneCommentID == "" {
		t.Error("PlaneCommentID should not be empty")
	}
}

func TestSyncCommentsDryRunNoSideEffects(t *testing.T) {
	now := time.Now().UTC()
	st := testStore(t)
	logger := testLogger(true)

	if err := st.UpsertIssueMap(store.IssueMap{
		GitHubOwner: "owner", GitHubRepo: "repo", GitHubIssueNumber: 1,
		PlaneProjectID: "proj-1", PlaneIssueID: "plane-issue-1",
	}); err != nil {
		t.Fatalf("seeding issue map: %v", err)
	}

	gh := &mockGitHubClient{
		comments: map[string][]GitHubComment{
			"owner/repo#1": {
				{ID: 100, Body: "Should not sync", User: "alice", CreatedAt: now, HTMLURL: "https://github.com/owner/repo/issues/1#issuecomment-100"},
			},
		},
	}
	pl := &mockPlaneClient{}

	engine := NewEngine(gh, pl, st, logger, testConfig())
	if err := engine.SyncComments(context.Background()); err != nil {
		t.Fatalf("SyncComments: %v", err)
	}

	// Verify no Plane API calls were made.
	if len(pl.createdComments) != 0 {
		t.Errorf("dry run should not create comments, got %d", len(pl.createdComments))
	}

	// Verify no synced comment was stored.
	comments, err := st.ListSyncedComments("owner", "repo", 1)
	if err != nil {
		t.Fatalf("ListSyncedComments: %v", err)
	}
	if len(comments) != 0 {
		t.Errorf("dry run should not store synced comments, got %d", len(comments))
	}
}

func TestSyncCommentsIncludesAuthorAndTimestamp(t *testing.T) {
	commentTime := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	st := testStore(t)
	logger := testLogger(false)

	if err := st.UpsertIssueMap(store.IssueMap{
		GitHubOwner: "owner", GitHubRepo: "repo", GitHubIssueNumber: 1,
		PlaneProjectID: "proj-1", PlaneIssueID: "plane-issue-1",
	}); err != nil {
		t.Fatalf("seeding issue map: %v", err)
	}

	gh := &mockGitHubClient{
		comments: map[string][]GitHubComment{
			"owner/repo#1": {
				{ID: 100, Body: "Test body", User: "alice", CreatedAt: commentTime, HTMLURL: "https://github.com/owner/repo/issues/1#issuecomment-100"},
			},
		},
	}
	pl := &mockPlaneClient{}

	engine := NewEngine(gh, pl, st, logger, testConfig())
	if err := engine.SyncComments(context.Background()); err != nil {
		t.Fatalf("SyncComments: %v", err)
	}

	if len(pl.createdComments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(pl.createdComments))
	}

	body := pl.createdComments[0].req.CommentHTML
	// Verify the comment body contains the author.
	if !strings.Contains(body, "alice") {
		t.Errorf("comment body should contain author 'alice': %s", body)
	}
	// Verify the comment body contains the timestamp.
	if !strings.Contains(body, "2025-06-15T10:30:00Z") {
		t.Errorf("comment body should contain timestamp: %s", body)
	}
	// Verify the comment body contains the original text.
	if !strings.Contains(body, "Test body") {
		t.Errorf("comment body should contain original text: %s", body)
	}
	// Verify the comment body contains a link.
	if !strings.Contains(body, "view on GitHub") {
		t.Errorf("comment body should contain GitHub link: %s", body)
	}
}

func TestBuildCommentBody(t *testing.T) {
	tests := []struct {
		name    string
		comment GitHubComment
		want    string
	}{
		{
			name: "standard comment",
			comment: GitHubComment{
				ID:        100,
				Body:      "Hello world",
				User:      "alice",
				CreatedAt: time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC),
				HTMLURL:   "https://github.com/o/r/issues/1#issuecomment-100",
			},
			want: `<p><strong>alice</strong> commented on 2025-06-15T10:30:00Z (<a href="https://github.com/o/r/issues/1#issuecomment-100">view on GitHub</a>):</p>
<blockquote>Hello world</blockquote>`,
		},
		{
			name: "body with HTML is escaped",
			comment: GitHubComment{
				ID:        200,
				Body:      `<script>alert("xss")</script>`,
				User:      "eve",
				CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
				HTMLURL:   "https://github.com/o/r/issues/1#issuecomment-200",
			},
			want: `<p><strong>eve</strong> commented on 2025-01-01T00:00:00Z (<a href="https://github.com/o/r/issues/1#issuecomment-200">view on GitHub</a>):</p>
<blockquote>&lt;script&gt;alert(&#34;xss&#34;)&lt;/script&gt;</blockquote>`,
		},
		{
			name: "empty body",
			comment: GitHubComment{
				ID:        300,
				Body:      "",
				User:      "bob",
				CreatedAt: time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC),
				HTMLURL:   "https://github.com/o/r/issues/1#issuecomment-300",
			},
			want: `<p><strong>bob</strong> commented on 2025-03-01T12:00:00Z (<a href="https://github.com/o/r/issues/1#issuecomment-300">view on GitHub</a>):</p>
<blockquote></blockquote>`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildCommentBody(tc.comment)
			if got != tc.want {
				t.Errorf("buildCommentBody() = %q, want %q", got, tc.want)
			}
		})
	}
}
