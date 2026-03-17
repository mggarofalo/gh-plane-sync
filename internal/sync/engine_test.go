package sync

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/mggarofalo/gh-plane-sync/internal/config"
	"github.com/mggarofalo/gh-plane-sync/internal/log"
	"github.com/mggarofalo/gh-plane-sync/internal/store"
)

// mockGitHubClient implements GitHubClient for testing.
type mockGitHubClient struct {
	issues map[string][]GitHubIssue // key: "owner/repo"
	err    error
}

func (m *mockGitHubClient) ListIssues(_ context.Context, owner, repo string, _ time.Time) ([]GitHubIssue, error) {
	if m.err != nil {
		return nil, m.err
	}
	key := owner + "/" + repo
	return m.issues[key], nil
}

// mockPlaneClient implements PlaneClient for testing.
type mockPlaneClient struct {
	created []CreatePlaneIssueRequest
	updated []updateCall
	nextID  int
	err     error
}

type updateCall struct {
	issueID string
	req     UpdatePlaneIssueRequest
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

// mockGitHubClientWithSince captures the since parameter.
type mockGitHubClientWithSince struct {
	captureSince *time.Time
}

func (m *mockGitHubClientWithSince) ListIssues(_ context.Context, _, _ string, since time.Time) ([]GitHubIssue, error) {
	*m.captureSince = since
	return nil, nil
}
