package store

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestUpsertSyncedComment(t *testing.T) {
	tests := []struct {
		name     string
		comments []SyncedComment
		check    func(t *testing.T, s *Store)
		wantErr  bool
	}{
		{
			name: "insert single comment",
			comments: []SyncedComment{
				{
					GitHubCommentID:   100,
					PlaneCommentID:    "plane-100",
					GitHubIssueNumber: 1,
					GitHubOwner:       "owner",
					GitHubRepo:        "repo",
				},
			},
			check: func(t *testing.T, s *Store) {
				t.Helper()
				c, err := s.GetSyncedComment(100)
				if err != nil {
					t.Fatalf("GetSyncedComment: %v", err)
				}
				if c.PlaneCommentID != "plane-100" {
					t.Errorf("PlaneCommentID = %q, want %q", c.PlaneCommentID, "plane-100")
				}
				if c.SyncedAt.IsZero() {
					t.Error("SyncedAt should not be zero")
				}
			},
		},
		{
			name: "upsert updates existing comment",
			comments: []SyncedComment{
				{
					GitHubCommentID:   200,
					PlaneCommentID:    "old-plane",
					GitHubIssueNumber: 1,
					GitHubOwner:       "owner",
					GitHubRepo:        "repo",
				},
				{
					GitHubCommentID:   200,
					PlaneCommentID:    "new-plane",
					GitHubIssueNumber: 1,
					GitHubOwner:       "owner",
					GitHubRepo:        "repo",
				},
			},
			check: func(t *testing.T, s *Store) {
				t.Helper()
				c, err := s.GetSyncedComment(200)
				if err != nil {
					t.Fatalf("GetSyncedComment: %v", err)
				}
				if c.PlaneCommentID != "new-plane" {
					t.Errorf("PlaneCommentID = %q, want %q", c.PlaneCommentID, "new-plane")
				}
			},
		},
		{
			name: "preserves explicit synced_at",
			comments: []SyncedComment{
				{
					GitHubCommentID:   300,
					PlaneCommentID:    "plane-300",
					GitHubIssueNumber: 5,
					GitHubOwner:       "owner",
					GitHubRepo:        "repo",
					SyncedAt:          time.Date(2025, 6, 1, 10, 0, 0, 0, time.UTC),
				},
			},
			check: func(t *testing.T, s *Store) {
				t.Helper()
				c, err := s.GetSyncedComment(300)
				if err != nil {
					t.Fatalf("GetSyncedComment: %v", err)
				}
				want := time.Date(2025, 6, 1, 10, 0, 0, 0, time.UTC)
				if !c.SyncedAt.Equal(want) {
					t.Errorf("SyncedAt = %v, want %v", c.SyncedAt, want)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := testOpen(t)
			for _, c := range tc.comments {
				err := s.UpsertSyncedComment(c)
				if tc.wantErr {
					if err == nil {
						t.Fatal("expected error, got nil")
					}
					return
				}
				if err != nil {
					t.Fatalf("UpsertSyncedComment: %v", err)
				}
			}
			if tc.check != nil {
				tc.check(t, s)
			}
		})
	}
}

func TestGetSyncedComment(t *testing.T) {
	tests := []struct {
		name    string
		seed    []SyncedComment
		id      int
		wantErr error
	}{
		{
			name: "found",
			seed: []SyncedComment{
				{GitHubCommentID: 1, PlaneCommentID: "p1", GitHubIssueNumber: 1, GitHubOwner: "o", GitHubRepo: "r"},
			},
			id: 1,
		},
		{
			name:    "not found",
			seed:    nil,
			id:      999,
			wantErr: sql.ErrNoRows,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := testOpen(t)
			for _, c := range tc.seed {
				if err := s.UpsertSyncedComment(c); err != nil {
					t.Fatalf("seed UpsertSyncedComment: %v", err)
				}
			}
			_, err := s.GetSyncedComment(tc.id)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("error = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("GetSyncedComment: %v", err)
			}
		})
	}
}

func TestListSyncedComments(t *testing.T) {
	tests := []struct {
		name        string
		seed        []SyncedComment
		owner       string
		repo        string
		issueNumber int
		want        int
	}{
		{
			name:  "empty",
			seed:  nil,
			owner: "o", repo: "r", issueNumber: 1,
			want: 0,
		},
		{
			name: "returns matching issue only",
			seed: []SyncedComment{
				{GitHubCommentID: 1, PlaneCommentID: "p1", GitHubIssueNumber: 1, GitHubOwner: "o", GitHubRepo: "r"},
				{GitHubCommentID: 2, PlaneCommentID: "p2", GitHubIssueNumber: 1, GitHubOwner: "o", GitHubRepo: "r"},
				{GitHubCommentID: 3, PlaneCommentID: "p3", GitHubIssueNumber: 2, GitHubOwner: "o", GitHubRepo: "r"},
				{GitHubCommentID: 4, PlaneCommentID: "p4", GitHubIssueNumber: 1, GitHubOwner: "o", GitHubRepo: "other"},
			},
			owner: "o", repo: "r", issueNumber: 1,
			want: 2,
		},
		{
			name: "ordered by comment id",
			seed: []SyncedComment{
				{GitHubCommentID: 30, PlaneCommentID: "p30", GitHubIssueNumber: 1, GitHubOwner: "o", GitHubRepo: "r"},
				{GitHubCommentID: 10, PlaneCommentID: "p10", GitHubIssueNumber: 1, GitHubOwner: "o", GitHubRepo: "r"},
				{GitHubCommentID: 20, PlaneCommentID: "p20", GitHubIssueNumber: 1, GitHubOwner: "o", GitHubRepo: "r"},
			},
			owner: "o", repo: "r", issueNumber: 1,
			want: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := testOpen(t)
			for _, c := range tc.seed {
				if err := s.UpsertSyncedComment(c); err != nil {
					t.Fatalf("seed UpsertSyncedComment: %v", err)
				}
			}
			got, err := s.ListSyncedComments(tc.owner, tc.repo, tc.issueNumber)
			if err != nil {
				t.Fatalf("ListSyncedComments: %v", err)
			}
			if len(got) != tc.want {
				t.Errorf("len = %d, want %d", len(got), tc.want)
			}
			// Verify ordering.
			for i := 1; i < len(got); i++ {
				if got[i].GitHubCommentID <= got[i-1].GitHubCommentID {
					t.Errorf("not sorted: comment %d at index %d comes after comment %d",
						got[i].GitHubCommentID, i, got[i-1].GitHubCommentID)
				}
			}
		})
	}
}

func TestDeleteSyncedComment(t *testing.T) {
	tests := []struct {
		name    string
		seed    []SyncedComment
		id      int
		wantErr error
	}{
		{
			name: "deletes existing",
			seed: []SyncedComment{
				{GitHubCommentID: 1, PlaneCommentID: "p1", GitHubIssueNumber: 1, GitHubOwner: "o", GitHubRepo: "r"},
			},
			id: 1,
		},
		{
			name:    "not found returns error",
			seed:    nil,
			id:      999,
			wantErr: sql.ErrNoRows,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := testOpen(t)
			for _, c := range tc.seed {
				if err := s.UpsertSyncedComment(c); err != nil {
					t.Fatalf("seed UpsertSyncedComment: %v", err)
				}
			}
			err := s.DeleteSyncedComment(tc.id)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("error = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("DeleteSyncedComment: %v", err)
			}
			// Verify deletion.
			_, err = s.GetSyncedComment(tc.id)
			if !errors.Is(err, sql.ErrNoRows) {
				t.Errorf("expected ErrNoRows after delete, got %v", err)
			}
		})
	}
}
