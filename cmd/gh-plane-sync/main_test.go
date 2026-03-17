package main

import (
	"os"
	"path/filepath"
	"testing"
)

// validConfigYAML is a minimal valid config used across CLI tests.
const validConfigYAML = `
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
`

func TestRun(t *testing.T) {
	// Not parallel: subtests use t.Setenv which modifies process-global state.

	configPath := writeTestConfig(t, validConfigYAML)

	tests := []struct {
		name     string
		args     []string
		envVars  map[string]string
		wantCode int
	}{
		{
			name:     "help flag exits cleanly",
			args:     []string{"--help"},
			wantCode: 0,
		},
		{
			name:     "version flag",
			args:     []string{"--version"},
			wantCode: 0,
		},
		{
			name:     "missing GITHUB_TOKEN",
			args:     []string{"--config", configPath},
			envVars:  map[string]string{"PLANE_API_KEY": "test-key"},
			wantCode: 1,
		},
		{
			name:     "missing PLANE_API_KEY",
			args:     []string{"--config", configPath},
			envVars:  map[string]string{"GITHUB_TOKEN": "ghp_test"},
			wantCode: 1,
		},
		{
			name: "valid config with env vars",
			args: []string{"--config", configPath},
			envVars: map[string]string{
				"GITHUB_TOKEN":  "ghp_test",
				"PLANE_API_KEY": "test-key",
			},
			wantCode: 0,
		},
		{
			name: "dry-run flag accepted",
			args: []string{"--config", configPath, "--dry-run"},
			envVars: map[string]string{
				"GITHUB_TOKEN":  "ghp_test",
				"PLANE_API_KEY": "test-key",
			},
			wantCode: 0,
		},
		{
			name: "once flag accepted",
			args: []string{"--config", configPath, "--once"},
			envVars: map[string]string{
				"GITHUB_TOKEN":  "ghp_test",
				"PLANE_API_KEY": "test-key",
			},
			wantCode: 0,
		},
		{
			name: "nonexistent config file",
			args: []string{"--config", "/nonexistent/config.yaml"},
			envVars: map[string]string{
				"GITHUB_TOKEN":  "ghp_test",
				"PLANE_API_KEY": "test-key",
			},
			wantCode: 1,
		},
		{
			name:     "invalid flag",
			args:     []string{"--bogus"},
			wantCode: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Not parallel: t.Setenv modifies process-global state.
			t.Setenv("GITHUB_TOKEN", "")
			t.Setenv("PLANE_API_KEY", "")
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			got := run(tt.args)
			if got != tt.wantCode {
				t.Errorf("run(%v) = %d, want %d", tt.args, got, tt.wantCode)
			}
		})
	}
}

// writeTestConfig writes the given YAML to a temp file and returns its path.
func writeTestConfig(t *testing.T, yaml string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(p, []byte(yaml), 0o600); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
	return p
}
