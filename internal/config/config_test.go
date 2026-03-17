package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		yaml    string
		wantErr string
		check   func(t *testing.T, cfg *Config)
	}{
		{
			name: "valid minimal config",
			yaml: `
plane:
  api_url: "https://plane.example.com"
  workspace: "my-workspace"
states:
  github_to_plane:
    open: "Backlog"
    closed: "Done"
  plane_to_github:
    done: "closed"
    backlog: "open"
mappings:
  - github:
      owner: "org"
      repo: "repo-a"
    plane:
      project_id: "aaaa-bbbb"
`,
			check: func(t *testing.T, cfg *Config) {
				t.Helper()
				if cfg.Plane.APIURL != "https://plane.example.com" {
					t.Errorf("APIURL = %q, want %q", cfg.Plane.APIURL, "https://plane.example.com")
				}
				if cfg.Plane.Workspace != "my-workspace" {
					t.Errorf("Workspace = %q, want %q", cfg.Plane.Workspace, "my-workspace")
				}
				if cfg.DBPath != DefaultDBPath {
					t.Errorf("DBPath = %q, want default %q", cfg.DBPath, DefaultDBPath)
				}
				if cfg.Interval.Duration != DefaultInterval {
					t.Errorf("Interval = %v, want default %v", cfg.Interval.Duration, DefaultInterval)
				}
				if len(cfg.Mappings) != 1 {
					t.Fatalf("len(Mappings) = %d, want 1", len(cfg.Mappings))
				}
				if cfg.Mappings[0].GitHub.Owner != "org" {
					t.Errorf("Mappings[0].GitHub.Owner = %q, want %q", cfg.Mappings[0].GitHub.Owner, "org")
				}
			},
		},
		{
			name: "custom db_path",
			yaml: `
plane:
  api_url: "https://plane.example.com"
  workspace: "ws"
states:
  github_to_plane:
    open: "Backlog"
  plane_to_github:
    backlog: "open"
mappings:
  - github:
      owner: "o"
      repo: "r"
    plane:
      project_id: "id"
db_path: "/custom/path/sync.db"
`,
			check: func(t *testing.T, cfg *Config) {
				t.Helper()
				if cfg.DBPath != "/custom/path/sync.db" {
					t.Errorf("DBPath = %q, want %q", cfg.DBPath, "/custom/path/sync.db")
				}
			},
		},
		{
			name: "multiple mappings",
			yaml: `
plane:
  api_url: "https://plane.example.com"
  workspace: "ws"
states:
  github_to_plane:
    open: "Backlog"
  plane_to_github:
    backlog: "open"
mappings:
  - github:
      owner: "org"
      repo: "repo-a"
    plane:
      project_id: "id-a"
  - github:
      owner: "org"
      repo: "repo-b"
    plane:
      project_id: "id-b"
`,
			check: func(t *testing.T, cfg *Config) {
				t.Helper()
				if len(cfg.Mappings) != 2 {
					t.Fatalf("len(Mappings) = %d, want 2", len(cfg.Mappings))
				}
				if cfg.Mappings[1].GitHub.Repo != "repo-b" {
					t.Errorf("Mappings[1].GitHub.Repo = %q, want %q", cfg.Mappings[1].GitHub.Repo, "repo-b")
				}
			},
		},
		{
			name: "per-mapping state overrides",
			yaml: `
plane:
  api_url: "https://plane.example.com"
  workspace: "ws"
states:
  github_to_plane:
    open: "Backlog"
    closed: "Done"
  plane_to_github:
    done: "closed"
    backlog: "open"
mappings:
  - github:
      owner: "org"
      repo: "repo-a"
    plane:
      project_id: "id-a"
  - github:
      owner: "org"
      repo: "repo-b"
    plane:
      project_id: "id-b"
    states:
      github_to_plane:
        open: "Triage"
        closed: "Shipped"
      plane_to_github:
        shipped: "closed"
        triage: "open"
`,
			check: func(t *testing.T, cfg *Config) {
				t.Helper()

				// First mapping should use global defaults.
				es0 := cfg.Mappings[0].EffectiveStates(cfg.States)
				if es0.GitHubToPlane["open"] != "Backlog" {
					t.Errorf("Mappings[0] GitHubToPlane[open] = %q, want %q", es0.GitHubToPlane["open"], "Backlog")
				}

				// Second mapping should use its own overrides.
				es1 := cfg.Mappings[1].EffectiveStates(cfg.States)
				if es1.GitHubToPlane["open"] != "Triage" {
					t.Errorf("Mappings[1] GitHubToPlane[open] = %q, want %q", es1.GitHubToPlane["open"], "Triage")
				}
				if es1.PlaneToGitHub["shipped"] != "closed" {
					t.Errorf("Mappings[1] PlaneToGitHub[shipped] = %q, want %q", es1.PlaneToGitHub["shipped"], "closed")
				}
			},
		},
		{
			name:    "missing plane.api_url",
			yaml:    `plane: {workspace: "ws"}` + "\n" + statesAndMappingYAML(),
			wantErr: "plane.api_url is required",
		},
		{
			name:    "missing plane.workspace",
			yaml:    `plane: {api_url: "https://example.com"}` + "\n" + statesAndMappingYAML(),
			wantErr: "plane.workspace is required",
		},
		{
			name: "missing states.github_to_plane",
			yaml: `
plane:
  api_url: "https://example.com"
  workspace: "ws"
states:
  plane_to_github:
    done: "closed"
mappings:
  - github: {owner: "o", repo: "r"}
    plane: {project_id: "id"}
`,
			wantErr: "states.github_to_plane is required",
		},
		{
			name: "missing states.plane_to_github",
			yaml: `
plane:
  api_url: "https://example.com"
  workspace: "ws"
states:
  github_to_plane:
    open: "Backlog"
mappings:
  - github: {owner: "o", repo: "r"}
    plane: {project_id: "id"}
`,
			wantErr: "states.plane_to_github is required",
		},
		{
			name: "no mappings",
			yaml: `
plane:
  api_url: "https://example.com"
  workspace: "ws"
states:
  github_to_plane:
    open: "Backlog"
  plane_to_github:
    backlog: "open"
mappings: []
`,
			wantErr: "at least one mapping is required",
		},
		{
			name: "missing github.owner",
			yaml: `
plane:
  api_url: "https://example.com"
  workspace: "ws"
states:
  github_to_plane:
    open: "Backlog"
  plane_to_github:
    backlog: "open"
mappings:
  - github: {repo: "r"}
    plane: {project_id: "id"}
`,
			wantErr: "mappings[0].github.owner is required",
		},
		{
			name: "missing github.repo",
			yaml: `
plane:
  api_url: "https://example.com"
  workspace: "ws"
states:
  github_to_plane:
    open: "Backlog"
  plane_to_github:
    backlog: "open"
mappings:
  - github: {owner: "o"}
    plane: {project_id: "id"}
`,
			wantErr: "mappings[0].github.repo is required",
		},
		{
			name: "missing plane.project_id",
			yaml: `
plane:
  api_url: "https://example.com"
  workspace: "ws"
states:
  github_to_plane:
    open: "Backlog"
  plane_to_github:
    backlog: "open"
mappings:
  - github: {owner: "o", repo: "r"}
    plane: {}
`,
			wantErr: "mappings[0].plane.project_id is required",
		},
		{
			name: "duplicate repo mapping",
			yaml: `
plane:
  api_url: "https://example.com"
  workspace: "ws"
states:
  github_to_plane:
    open: "Backlog"
  plane_to_github:
    backlog: "open"
mappings:
  - github: {owner: "o", repo: "r"}
    plane: {project_id: "id-1"}
  - github: {owner: "o", repo: "r"}
    plane: {project_id: "id-2"}
`,
			wantErr: "duplicate mapping for repository o/r",
		},
		{
			name: "per-mapping states with empty github_to_plane",
			yaml: `
plane:
  api_url: "https://example.com"
  workspace: "ws"
states:
  github_to_plane:
    open: "Backlog"
  plane_to_github:
    backlog: "open"
mappings:
  - github: {owner: "o", repo: "r"}
    plane: {project_id: "id"}
    states:
      github_to_plane: {}
      plane_to_github:
        done: "closed"
`,
			wantErr: "mappings[0].states.github_to_plane must not be empty when states is specified",
		},
		{
			name: "per-mapping states with empty plane_to_github",
			yaml: `
plane:
  api_url: "https://example.com"
  workspace: "ws"
states:
  github_to_plane:
    open: "Backlog"
  plane_to_github:
    backlog: "open"
mappings:
  - github: {owner: "o", repo: "r"}
    plane: {project_id: "id"}
    states:
      github_to_plane:
        open: "Triage"
      plane_to_github: {}
`,
			wantErr: "mappings[0].states.plane_to_github must not be empty when states is specified",
		},
		{
			name: "custom interval",
			yaml: `
plane:
  api_url: "https://plane.example.com"
  workspace: "ws"
interval: "15m"
states:
  github_to_plane:
    open: "Backlog"
  plane_to_github:
    backlog: "open"
mappings:
  - github: {owner: "o", repo: "r"}
    plane: {project_id: "id"}
`,
			check: func(t *testing.T, cfg *Config) {
				t.Helper()
				if cfg.Interval.Duration != 15*time.Minute {
					t.Errorf("Interval = %v, want %v", cfg.Interval.Duration, 15*time.Minute)
				}
			},
		},
		{
			name: "interval with hours",
			yaml: `
plane:
  api_url: "https://plane.example.com"
  workspace: "ws"
interval: "1h30m"
states:
  github_to_plane:
    open: "Backlog"
  plane_to_github:
    backlog: "open"
mappings:
  - github: {owner: "o", repo: "r"}
    plane: {project_id: "id"}
`,
			check: func(t *testing.T, cfg *Config) {
				t.Helper()
				want := time.Hour + 30*time.Minute
				if cfg.Interval.Duration != want {
					t.Errorf("Interval = %v, want %v", cfg.Interval.Duration, want)
				}
			},
		},
		{
			name: "interval below minimum",
			yaml: `
plane:
  api_url: "https://example.com"
  workspace: "ws"
interval: "30s"
states:
  github_to_plane:
    open: "Backlog"
  plane_to_github:
    backlog: "open"
mappings:
  - github: {owner: "o", repo: "r"}
    plane: {project_id: "id"}
`,
			wantErr: "interval 30s is below minimum 1m0s",
		},
		{
			name: "interval zero is rejected",
			yaml: `
plane:
  api_url: "https://example.com"
  workspace: "ws"
interval: "0s"
states:
  github_to_plane:
    open: "Backlog"
  plane_to_github:
    backlog: "open"
mappings:
  - github: {owner: "o", repo: "r"}
    plane: {project_id: "id"}
`,
			wantErr: "interval 0s is below minimum 1m0s",
		},
		{
			name: "invalid interval format",
			yaml: `
plane:
  api_url: "https://example.com"
  workspace: "ws"
interval: "not-a-duration"
states:
  github_to_plane:
    open: "Backlog"
  plane_to_github:
    backlog: "open"
mappings:
  - github: {owner: "o", repo: "r"}
    plane: {project_id: "id"}
`,
			wantErr: "parsing duration",
		},
		{
			name:    "invalid yaml syntax",
			yaml:    `plane: [[[`,
			wantErr: "parsing config file:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := writeTemp(t, tt.yaml)

			cfg, err := Load(path)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !containsSubstring(err.Error(), tt.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	t.Parallel()

	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
	if !containsSubstring(err.Error(), "reading config file:") {
		t.Errorf("error %q does not contain %q", err.Error(), "reading config file:")
	}
}

func TestMapping_EffectiveStates(t *testing.T) {
	t.Parallel()

	global := StateMappings{
		GitHubToPlane: map[string]string{"open": "Backlog"},
		PlaneToGitHub: map[string]string{"backlog": "open"},
	}

	override := StateMappings{
		GitHubToPlane: map[string]string{"open": "Triage"},
		PlaneToGitHub: map[string]string{"triage": "open"},
	}

	tests := []struct {
		name   string
		states *StateMappings
		want   string // expected value of GitHubToPlane["open"]
	}{
		{
			name:   "nil states uses global",
			states: nil,
			want:   "Backlog",
		},
		{
			name:   "non-nil states uses override",
			states: &override,
			want:   "Triage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := Mapping{States: tt.states}
			got := m.EffectiveStates(global)
			if got.GitHubToPlane["open"] != tt.want {
				t.Errorf("EffectiveStates().GitHubToPlane[open] = %q, want %q", got.GitHubToPlane["open"], tt.want)
			}
		})
	}
}

// statesAndMappingYAML returns a YAML fragment with valid states and mappings
// blocks for use in tests that focus on other validation errors.
func statesAndMappingYAML() string {
	return `states:
  github_to_plane:
    open: "Backlog"
  plane_to_github:
    backlog: "open"
mappings:
  - github: {owner: "o", repo: "r"}
    plane: {project_id: "id"}`
}

// writeTemp writes content to a temporary YAML file and returns its path.
func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}
	return path
}

// containsSubstring reports whether s contains substr.
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
