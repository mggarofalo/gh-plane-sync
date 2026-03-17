package store

import (
	"os"
	"path/filepath"
	"testing"
)

// testOpen creates a temporary store for testing and returns it along with
// a cleanup function.
func testOpen(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open(%q): %v", path, err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return s
}

func TestOpen(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T) string
		wantErr bool
	}{
		{
			name: "creates new database",
			setup: func(t *testing.T) string {
				t.Helper()
				return filepath.Join(t.TempDir(), "new.db")
			},
		},
		{
			name: "creates nested directories",
			setup: func(t *testing.T) string {
				t.Helper()
				return filepath.Join(t.TempDir(), "a", "b", "c", "nested.db")
			},
		},
		{
			name: "opens existing database",
			setup: func(t *testing.T) string {
				t.Helper()
				path := filepath.Join(t.TempDir(), "existing.db")
				s, err := Open(path)
				if err != nil {
					t.Fatalf("initial Open: %v", err)
				}
				if err := s.Close(); err != nil {
					t.Fatalf("Close: %v", err)
				}
				return path
			},
		},
		{
			name: "fails on invalid path",
			setup: func(_ *testing.T) string {
				// NUL device cannot be a directory parent on any OS.
				return filepath.Join(string([]byte{0}), "bad.db")
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := tc.setup(t)
			s, err := Open(path)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer func() {
				if err := s.Close(); err != nil {
					t.Errorf("Close: %v", err)
				}
			}()

			// Verify tables exist by querying sqlite_master.
			tables := []string{"issue_map", "synced_comments", "sync_state"}
			for _, table := range tables {
				var name string
				err := s.db.QueryRow(
					"SELECT name FROM sqlite_master WHERE type='table' AND name=?",
					table,
				).Scan(&name)
				if err != nil {
					t.Errorf("table %q not found: %v", table, err)
				}
			}
		})
	}
}

func TestOpenIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "idem.db")

	// Open twice — second open should succeed (migrations are idempotent).
	s1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	if err := s2.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestDefaultDBPath(t *testing.T) {
	if DefaultDBPath != "/var/lib/gh-plane-sync/sync.db" {
		t.Errorf("DefaultDBPath = %q, want /var/lib/gh-plane-sync/sync.db", DefaultDBPath)
	}
}

func TestClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "close.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Verify the database file still exists after close.
	if _, err := os.Stat(path); err != nil {
		t.Errorf("database file missing after close: %v", err)
	}
}
