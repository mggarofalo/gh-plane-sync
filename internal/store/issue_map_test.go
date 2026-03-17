package store

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestUpsertIssueMap(t *testing.T) {
	tests := []struct {
		name    string
		maps    []IssueMap // inserted in order
		check   func(t *testing.T, s *Store)
		wantErr bool
	}{
		{
			name: "insert single mapping",
			maps: []IssueMap{
				{
					GitHubOwner:       "owner",
					GitHubRepo:        "repo",
					GitHubIssueNumber: 1,
					PlaneProjectID:    "proj-1",
					PlaneIssueID:      "issue-1",
				},
			},
			check: func(t *testing.T, s *Store) {
				t.Helper()
				m, err := s.GetIssueMap("owner", "repo", 1)
				if err != nil {
					t.Fatalf("GetIssueMap: %v", err)
				}
				if m.PlaneIssueID != "issue-1" {
					t.Errorf("PlaneIssueID = %q, want %q", m.PlaneIssueID, "issue-1")
				}
				if m.CreatedAt.IsZero() {
					t.Error("CreatedAt should not be zero")
				}
			},
		},
		{
			name: "upsert updates existing mapping",
			maps: []IssueMap{
				{
					GitHubOwner:       "owner",
					GitHubRepo:        "repo",
					GitHubIssueNumber: 2,
					PlaneProjectID:    "proj-1",
					PlaneIssueID:      "old-id",
				},
				{
					GitHubOwner:       "owner",
					GitHubRepo:        "repo",
					GitHubIssueNumber: 2,
					PlaneProjectID:    "proj-2",
					PlaneIssueID:      "new-id",
				},
			},
			check: func(t *testing.T, s *Store) {
				t.Helper()
				m, err := s.GetIssueMap("owner", "repo", 2)
				if err != nil {
					t.Fatalf("GetIssueMap: %v", err)
				}
				if m.PlaneProjectID != "proj-2" {
					t.Errorf("PlaneProjectID = %q, want %q", m.PlaneProjectID, "proj-2")
				}
				if m.PlaneIssueID != "new-id" {
					t.Errorf("PlaneIssueID = %q, want %q", m.PlaneIssueID, "new-id")
				}
			},
		},
		{
			name: "preserves explicit created_at",
			maps: []IssueMap{
				{
					GitHubOwner:       "owner",
					GitHubRepo:        "repo",
					GitHubIssueNumber: 3,
					PlaneProjectID:    "proj-1",
					PlaneIssueID:      "issue-3",
					CreatedAt:         time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC),
				},
			},
			check: func(t *testing.T, s *Store) {
				t.Helper()
				m, err := s.GetIssueMap("owner", "repo", 3)
				if err != nil {
					t.Fatalf("GetIssueMap: %v", err)
				}
				want := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
				if !m.CreatedAt.Equal(want) {
					t.Errorf("CreatedAt = %v, want %v", m.CreatedAt, want)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := testOpen(t)
			for _, m := range tc.maps {
				err := s.UpsertIssueMap(m)
				if tc.wantErr {
					if err == nil {
						t.Fatal("expected error, got nil")
					}
					return
				}
				if err != nil {
					t.Fatalf("UpsertIssueMap: %v", err)
				}
			}
			if tc.check != nil {
				tc.check(t, s)
			}
		})
	}
}

func TestGetIssueMap(t *testing.T) {
	tests := []struct {
		name        string
		seed        []IssueMap
		owner       string
		repo        string
		issueNumber int
		wantErr     error
	}{
		{
			name: "found",
			seed: []IssueMap{
				{
					GitHubOwner: "o", GitHubRepo: "r", GitHubIssueNumber: 1,
					PlaneProjectID: "p", PlaneIssueID: "i",
				},
			},
			owner: "o", repo: "r", issueNumber: 1,
		},
		{
			name:  "not found",
			seed:  nil,
			owner: "missing", repo: "missing", issueNumber: 999,
			wantErr: sql.ErrNoRows,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := testOpen(t)
			for _, m := range tc.seed {
				if err := s.UpsertIssueMap(m); err != nil {
					t.Fatalf("seed UpsertIssueMap: %v", err)
				}
			}
			_, err := s.GetIssueMap(tc.owner, tc.repo, tc.issueNumber)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("error = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("GetIssueMap: %v", err)
			}
		})
	}
}

func TestListIssueMaps(t *testing.T) {
	tests := []struct {
		name  string
		seed  []IssueMap
		owner string
		repo  string
		want  int
	}{
		{
			name:  "empty",
			seed:  nil,
			owner: "o", repo: "r",
			want: 0,
		},
		{
			name: "returns matching repo only",
			seed: []IssueMap{
				{GitHubOwner: "o", GitHubRepo: "r", GitHubIssueNumber: 1, PlaneProjectID: "p", PlaneIssueID: "i1"},
				{GitHubOwner: "o", GitHubRepo: "r", GitHubIssueNumber: 2, PlaneProjectID: "p", PlaneIssueID: "i2"},
				{GitHubOwner: "o", GitHubRepo: "other", GitHubIssueNumber: 1, PlaneProjectID: "p", PlaneIssueID: "i3"},
			},
			owner: "o", repo: "r",
			want: 2,
		},
		{
			name: "ordered by issue number",
			seed: []IssueMap{
				{GitHubOwner: "o", GitHubRepo: "r", GitHubIssueNumber: 10, PlaneProjectID: "p", PlaneIssueID: "i10"},
				{GitHubOwner: "o", GitHubRepo: "r", GitHubIssueNumber: 3, PlaneProjectID: "p", PlaneIssueID: "i3"},
				{GitHubOwner: "o", GitHubRepo: "r", GitHubIssueNumber: 7, PlaneProjectID: "p", PlaneIssueID: "i7"},
			},
			owner: "o", repo: "r",
			want: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := testOpen(t)
			for _, m := range tc.seed {
				if err := s.UpsertIssueMap(m); err != nil {
					t.Fatalf("seed UpsertIssueMap: %v", err)
				}
			}
			got, err := s.ListIssueMaps(tc.owner, tc.repo)
			if err != nil {
				t.Fatalf("ListIssueMaps: %v", err)
			}
			if len(got) != tc.want {
				t.Errorf("len = %d, want %d", len(got), tc.want)
			}
			// Verify ordering.
			for i := 1; i < len(got); i++ {
				if got[i].GitHubIssueNumber <= got[i-1].GitHubIssueNumber {
					t.Errorf("not sorted: issue %d at index %d comes after issue %d",
						got[i].GitHubIssueNumber, i, got[i-1].GitHubIssueNumber)
				}
			}
		})
	}
}

func TestDeleteIssueMap(t *testing.T) {
	tests := []struct {
		name        string
		seed        []IssueMap
		owner       string
		repo        string
		issueNumber int
		wantErr     error
	}{
		{
			name: "deletes existing",
			seed: []IssueMap{
				{GitHubOwner: "o", GitHubRepo: "r", GitHubIssueNumber: 1, PlaneProjectID: "p", PlaneIssueID: "i"},
			},
			owner: "o", repo: "r", issueNumber: 1,
		},
		{
			name:  "not found returns error",
			seed:  nil,
			owner: "o", repo: "r", issueNumber: 999,
			wantErr: sql.ErrNoRows,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := testOpen(t)
			for _, m := range tc.seed {
				if err := s.UpsertIssueMap(m); err != nil {
					t.Fatalf("seed UpsertIssueMap: %v", err)
				}
			}
			err := s.DeleteIssueMap(tc.owner, tc.repo, tc.issueNumber)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("error = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("DeleteIssueMap: %v", err)
			}
			// Verify deletion.
			_, err = s.GetIssueMap(tc.owner, tc.repo, tc.issueNumber)
			if !errors.Is(err, sql.ErrNoRows) {
				t.Errorf("expected ErrNoRows after delete, got %v", err)
			}
		})
	}
}
