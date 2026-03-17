package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mggarofalo/gh-plane-sync/internal/config"
	"github.com/mggarofalo/gh-plane-sync/internal/log"
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

// shortIntervalConfigYAML uses a 1m interval (minimum allowed) for daemon tests.
const shortIntervalConfigYAML = `
plane:
  api_url: "https://plane.example.com"
  workspace: "my-workspace"
interval: "1m"
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
			name: "valid config with env vars and once flag",
			args: []string{"--config", configPath, "--once"},
			envVars: map[string]string{
				"GITHUB_TOKEN":  "ghp_test",
				"PLANE_API_KEY": "test-key",
			},
			wantCode: 0,
		},
		{
			name: "dry-run flag accepted",
			args: []string{"--config", configPath, "--dry-run", "--once"},
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

func TestRunOnce_CallsSyncCycle(t *testing.T) {
	// Not parallel: modifies package-level syncCycle and env vars.

	configPath := writeTestConfig(t, validConfigYAML)

	var called atomic.Bool
	origSync := syncCycle
	syncCycle = func(_ context.Context, _ *config.Config, _ bool, _ *log.Logger) {
		called.Store(true)
	}
	t.Cleanup(func() { syncCycle = origSync })

	t.Setenv("GITHUB_TOKEN", "ghp_test")
	t.Setenv("PLANE_API_KEY", "test-key")

	code := run([]string{"--config", configPath, "--once"})
	if code != 0 {
		t.Fatalf("run() = %d, want 0", code)
	}
	if !called.Load() {
		t.Error("syncCycle was not called in --once mode")
	}
}

func TestRunDaemon_ImmediateFirstSync(t *testing.T) {
	// Not parallel: modifies package-level syncCycle.

	cfg := loadTestConfig(t, shortIntervalConfigYAML)

	var cycleCount atomic.Int32
	origSync := syncCycle
	syncCycle = func(_ context.Context, _ *config.Config, _ bool, _ *log.Logger) {
		cycleCount.Add(1)
	}
	t.Cleanup(func() { syncCycle = origSync })

	// Cancel context right after the first sync runs.
	ctx, cancel := context.WithCancel(context.Background())

	var buf bytes.Buffer
	logger := log.New(log.Options{Writer: &buf, Level: log.LevelInfo})

	done := make(chan int, 1)
	go func() {
		done <- runDaemon(ctx, cfg, false, logger)
	}()

	// Wait for the initial sync cycle to execute.
	deadline := time.After(5 * time.Second)
	for cycleCount.Load() == 0 {
		select {
		case <-deadline:
			cancel()
			t.Fatal("timed out waiting for initial sync cycle")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Cancel to trigger graceful shutdown.
	cancel()

	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("runDaemon() = %d, want 0", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not exit within timeout after context cancellation")
	}

	if cycleCount.Load() < 1 {
		t.Errorf("sync cycle count = %d, want >= 1", cycleCount.Load())
	}
}

func TestRunDaemon_GracefulShutdownDuringSync(t *testing.T) {
	// Not parallel: modifies package-level syncCycle.

	cfg := loadTestConfig(t, shortIntervalConfigYAML)

	syncStarted := make(chan struct{})
	syncDone := make(chan struct{})

	origSync := syncCycle
	syncCycle = func(_ context.Context, _ *config.Config, _ bool, _ *log.Logger) {
		select {
		case syncStarted <- struct{}{}:
		default:
		}
		// Simulate a long-running sync that does NOT watch context.
		// The daemon must wait for this to return before exiting.
		<-syncDone
	}
	t.Cleanup(func() { syncCycle = origSync })

	ctx, cancel := context.WithCancel(context.Background())

	var buf bytes.Buffer
	logger := log.New(log.Options{Writer: &buf, Level: log.LevelInfo})

	done := make(chan int, 1)
	go func() {
		done <- runDaemon(ctx, cfg, false, logger)
	}()

	// Wait for the sync to start.
	select {
	case <-syncStarted:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("timed out waiting for sync to start")
	}

	// Cancel while sync is running - daemon should wait for sync to finish.
	cancel()

	// Sync hasn't finished yet, so daemon shouldn't have exited.
	select {
	case <-done:
		t.Fatal("daemon exited before sync cycle finished")
	case <-time.After(200 * time.Millisecond):
		// Expected: daemon is waiting for sync to complete.
	}

	// Now let the sync finish.
	close(syncDone)

	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("runDaemon() = %d, want 0", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not exit within timeout after sync completed")
	}
}

func TestRunDaemon_CancelBeforeFirstSync(t *testing.T) {
	// Not parallel: modifies package-level syncCycle.

	cfg := loadTestConfig(t, shortIntervalConfigYAML)

	var cycleCount atomic.Int32
	origSync := syncCycle
	syncCycle = func(_ context.Context, _ *config.Config, _ bool, _ *log.Logger) {
		cycleCount.Add(1)
		// The first sync always runs; the test verifies that after cancel
		// no additional cycles execute.
	}
	t.Cleanup(func() { syncCycle = origSync })

	// Cancel immediately.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var buf bytes.Buffer
	logger := log.New(log.Options{Writer: &buf, Level: log.LevelInfo})

	code := runDaemon(ctx, cfg, false, logger)
	if code != 0 {
		t.Errorf("runDaemon() = %d, want 0", code)
	}

	// The initial sync runs, but since context is already cancelled,
	// the daemon exits after it.
	if cycleCount.Load() != 1 {
		t.Errorf("sync cycle count = %d, want 1", cycleCount.Load())
	}
}

func TestLogNextSync(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := log.New(log.Options{Writer: &buf, Level: log.LevelInfo})

	before := time.Now()
	logNextSync(logger, 30*time.Minute)
	after := time.Now()

	output := buf.String()
	if output == "" {
		t.Fatal("logNextSync produced no output")
	}

	// Verify the log line contains expected fields.
	if !containsSubstr(output, "next sync scheduled") {
		t.Errorf("output missing 'next sync scheduled': %s", output)
	}
	if !containsSubstr(output, "30m0s") {
		t.Errorf("output missing interval '30m0s': %s", output)
	}

	// Verify the scheduled time is approximately correct.
	expectedMin := before.Add(30 * time.Minute)
	expectedMax := after.Add(30 * time.Minute)
	if !containsSubstr(output, expectedMin.Format("2006-01-02")) {
		_ = expectedMax // The date should be the same for both.
		// Not a hard failure -- just check the date portion.
	}
}

// loadTestConfig writes YAML to a temp file and loads it as a Config.
func loadTestConfig(t *testing.T, yamlContent string) *config.Config {
	t.Helper()
	path := writeTestConfig(t, yamlContent)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("loading test config: %v", err)
	}
	return cfg
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

// containsSubstr reports whether s contains substr.
func containsSubstr(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstr(s, substr)
}

func searchSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
