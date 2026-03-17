package store

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestUpsertSyncState(t *testing.T) {
	tests := []struct {
		name    string
		states  []SyncState
		check   func(t *testing.T, s *Store)
		wantErr bool
	}{
		{
			name: "insert new state",
			states: []SyncState{
				{
					GitHubOwner: "owner",
					GitHubRepo:  "repo",
				},
			},
			check: func(t *testing.T, s *Store) {
				t.Helper()
				st, err := s.GetSyncState("owner", "repo")
				if err != nil {
					t.Fatalf("GetSyncState: %v", err)
				}
				if st.LastSyncedAt.IsZero() {
					t.Error("LastSyncedAt should not be zero")
				}
			},
		},
		{
			name: "upsert updates existing state",
			states: []SyncState{
				{
					GitHubOwner:  "owner",
					GitHubRepo:   "repo",
					LastSyncedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
				},
				{
					GitHubOwner:  "owner",
					GitHubRepo:   "repo",
					LastSyncedAt: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
				},
			},
			check: func(t *testing.T, s *Store) {
				t.Helper()
				st, err := s.GetSyncState("owner", "repo")
				if err != nil {
					t.Fatalf("GetSyncState: %v", err)
				}
				want := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
				if !st.LastSyncedAt.Equal(want) {
					t.Errorf("LastSyncedAt = %v, want %v", st.LastSyncedAt, want)
				}
			},
		},
		{
			name: "preserves explicit timestamp",
			states: []SyncState{
				{
					GitHubOwner:  "owner",
					GitHubRepo:   "repo",
					LastSyncedAt: time.Date(2025, 3, 15, 8, 30, 0, 0, time.UTC),
				},
			},
			check: func(t *testing.T, s *Store) {
				t.Helper()
				st, err := s.GetSyncState("owner", "repo")
				if err != nil {
					t.Fatalf("GetSyncState: %v", err)
				}
				want := time.Date(2025, 3, 15, 8, 30, 0, 0, time.UTC)
				if !st.LastSyncedAt.Equal(want) {
					t.Errorf("LastSyncedAt = %v, want %v", st.LastSyncedAt, want)
				}
			},
		},
		{
			name: "multiple repos tracked independently",
			states: []SyncState{
				{
					GitHubOwner:  "owner",
					GitHubRepo:   "repo-a",
					LastSyncedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
				},
				{
					GitHubOwner:  "owner",
					GitHubRepo:   "repo-b",
					LastSyncedAt: time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC),
				},
			},
			check: func(t *testing.T, s *Store) {
				t.Helper()
				a, err := s.GetSyncState("owner", "repo-a")
				if err != nil {
					t.Fatalf("GetSyncState repo-a: %v", err)
				}
				b, err := s.GetSyncState("owner", "repo-b")
				if err != nil {
					t.Fatalf("GetSyncState repo-b: %v", err)
				}
				if a.LastSyncedAt.Equal(b.LastSyncedAt) {
					t.Error("repos should have different timestamps")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := testOpen(t)
			for _, st := range tc.states {
				err := s.UpsertSyncState(st)
				if tc.wantErr {
					if err == nil {
						t.Fatal("expected error, got nil")
					}
					return
				}
				if err != nil {
					t.Fatalf("UpsertSyncState: %v", err)
				}
			}
			if tc.check != nil {
				tc.check(t, s)
			}
		})
	}
}

func TestGetSyncState(t *testing.T) {
	tests := []struct {
		name    string
		seed    []SyncState
		owner   string
		repo    string
		wantErr error
	}{
		{
			name: "found",
			seed: []SyncState{
				{GitHubOwner: "o", GitHubRepo: "r", LastSyncedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)},
			},
			owner: "o", repo: "r",
		},
		{
			name:  "not found",
			seed:  nil,
			owner: "missing", repo: "missing",
			wantErr: sql.ErrNoRows,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := testOpen(t)
			for _, st := range tc.seed {
				if err := s.UpsertSyncState(st); err != nil {
					t.Fatalf("seed UpsertSyncState: %v", err)
				}
			}
			_, err := s.GetSyncState(tc.owner, tc.repo)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("error = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("GetSyncState: %v", err)
			}
		})
	}
}

func TestDeleteSyncState(t *testing.T) {
	tests := []struct {
		name    string
		seed    []SyncState
		owner   string
		repo    string
		wantErr error
	}{
		{
			name: "deletes existing",
			seed: []SyncState{
				{GitHubOwner: "o", GitHubRepo: "r", LastSyncedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)},
			},
			owner: "o", repo: "r",
		},
		{
			name:  "not found returns error",
			seed:  nil,
			owner: "o", repo: "r",
			wantErr: sql.ErrNoRows,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := testOpen(t)
			for _, st := range tc.seed {
				if err := s.UpsertSyncState(st); err != nil {
					t.Fatalf("seed UpsertSyncState: %v", err)
				}
			}
			err := s.DeleteSyncState(tc.owner, tc.repo)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("error = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("DeleteSyncState: %v", err)
			}
			// Verify deletion.
			_, err = s.GetSyncState(tc.owner, tc.repo)
			if !errors.Is(err, sql.ErrNoRows) {
				t.Errorf("expected ErrNoRows after delete, got %v", err)
			}
		})
	}
}
