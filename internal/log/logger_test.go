package log

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

// logLine is a minimal representation of a JSON log line used for
// assertions. We intentionally keep this loose (map) so new fields
// do not break existing tests.
type logLine map[string]any

func parseLines(t *testing.T, buf *bytes.Buffer) []logLine {
	t.Helper()
	var lines []logLine
	for _, raw := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if raw == "" {
			continue
		}
		var m logLine
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			t.Fatalf("unmarshal log line: %v\nraw: %s", err, raw)
		}
		lines = append(lines, m)
	}
	return lines
}

// ---------- ParseLevel ----------

func TestParseLevel(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Level
		wantErr bool
	}{
		{name: "debug", input: "debug", want: LevelDebug},
		{name: "info", input: "info", want: LevelInfo},
		{name: "warn", input: "warn", want: LevelWarn},
		{name: "warning", input: "warning", want: LevelWarn},
		{name: "error", input: "error", want: LevelError},
		{name: "case insensitive", input: "DEBUG", want: LevelDebug},
		{name: "with whitespace", input: "  info  ", want: LevelInfo},
		{name: "unknown", input: "trace", want: LevelInfo, wantErr: true},
		{name: "empty", input: "", want: LevelInfo, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseLevel(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseLevel(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ParseLevel(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// ---------- Log levels ----------

func TestLogLevelFiltering(t *testing.T) {
	tests := []struct {
		name       string
		level      Level
		logFn      func(l *Logger)
		wantLines  int
		wantFields map[string]string // checked against first line
	}{
		{
			name:  "info shown at info level",
			level: LevelInfo,
			logFn: func(l *Logger) {
				l.Info("hello")
			},
			wantLines:  1,
			wantFields: map[string]string{"msg": "hello", "level": "INFO"},
		},
		{
			name:  "debug hidden at info level",
			level: LevelInfo,
			logFn: func(l *Logger) {
				l.Debug("hidden")
			},
			wantLines: 0,
		},
		{
			name:  "debug shown at debug level",
			level: LevelDebug,
			logFn: func(l *Logger) {
				l.Debug("visible")
			},
			wantLines:  1,
			wantFields: map[string]string{"msg": "visible", "level": "DEBUG"},
		},
		{
			name:  "warn shown at info level",
			level: LevelInfo,
			logFn: func(l *Logger) {
				l.Warn("caution")
			},
			wantLines:  1,
			wantFields: map[string]string{"msg": "caution", "level": "WARN"},
		},
		{
			name:  "error shown at error level",
			level: LevelError,
			logFn: func(l *Logger) {
				l.Error("failure")
			},
			wantLines:  1,
			wantFields: map[string]string{"msg": "failure", "level": "ERROR"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			l := New(Options{Writer: &buf, Level: tt.level})
			tt.logFn(l)

			lines := parseLines(t, &buf)
			if len(lines) != tt.wantLines {
				t.Fatalf("got %d lines, want %d\noutput:\n%s", len(lines), tt.wantLines, buf.String())
			}
			if tt.wantFields != nil && len(lines) > 0 {
				for k, v := range tt.wantFields {
					if got, ok := lines[0][k]; !ok || got != v {
						t.Errorf("field %q = %v, want %q", k, got, v)
					}
				}
			}
		})
	}
}

// ---------- JSON output ----------

func TestJSONOutput(t *testing.T) {
	var buf bytes.Buffer
	l := New(Options{Writer: &buf, Level: LevelInfo})
	l.Info("test message", "key", "value")

	lines := parseLines(t, &buf)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	line := lines[0]

	requiredFields := []string{"time", "level", "msg"}
	for _, f := range requiredFields {
		if _, ok := line[f]; !ok {
			t.Errorf("missing required field %q in JSON output", f)
		}
	}

	if got, ok := line["key"]; !ok || got != "value" {
		t.Errorf("custom attribute key = %v, want %q", got, "value")
	}
}

// ---------- LogAction ----------

func TestLogAction(t *testing.T) {
	tests := []struct {
		name       string
		dryRun     bool
		action     Action
		msg        string
		extraArgs  []any
		wantPrefix string
		wantAction string
		wantDryRun bool
	}{
		{
			name:       "issue created normal mode",
			dryRun:     false,
			action:     ActionIssueCreated,
			msg:        "created issue",
			wantPrefix: "created issue",
			wantAction: "issue_created",
			wantDryRun: false,
		},
		{
			name:       "issue updated dry-run",
			dryRun:     true,
			action:     ActionIssueUpdated,
			msg:        "updated issue",
			wantPrefix: "[DRY-RUN] updated issue",
			wantAction: "issue_updated",
			wantDryRun: true,
		},
		{
			name:       "comment synced with extra attrs",
			dryRun:     false,
			action:     ActionCommentSynced,
			msg:        "synced comment",
			extraArgs:  []any{"github_id", 42},
			wantPrefix: "synced comment",
			wantAction: "comment_synced",
			wantDryRun: false,
		},
		{
			name:       "state changed",
			dryRun:     false,
			action:     ActionStateChanged,
			msg:        "closed github issue",
			wantPrefix: "closed github issue",
			wantAction: "state_changed",
			wantDryRun: false,
		},
		{
			name:       "skipped no changes",
			dryRun:     false,
			action:     ActionSkipped,
			msg:        "no changes detected",
			wantPrefix: "no changes detected",
			wantAction: "skipped",
			wantDryRun: false,
		},
		{
			name:       "skipped dry-run",
			dryRun:     true,
			action:     ActionSkipped,
			msg:        "no changes detected",
			wantPrefix: "[DRY-RUN] no changes detected",
			wantAction: "skipped",
			wantDryRun: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			l := New(Options{Writer: &buf, Level: LevelInfo, DryRun: tt.dryRun})
			l.LogAction(tt.action, tt.msg, tt.extraArgs...)

			lines := parseLines(t, &buf)
			if len(lines) != 1 {
				t.Fatalf("expected 1 line, got %d", len(lines))
			}
			line := lines[0]

			if msg, _ := line["msg"].(string); msg != tt.wantPrefix {
				t.Errorf("msg = %q, want %q", msg, tt.wantPrefix)
			}
			if act, _ := line["action"].(string); act != tt.wantAction {
				t.Errorf("action = %q, want %q", act, tt.wantAction)
			}
			if tt.wantDryRun {
				if dr, ok := line["dry_run"]; !ok || dr != true {
					t.Errorf("dry_run = %v, want true", dr)
				}
			} else {
				if _, ok := line["dry_run"]; ok {
					t.Error("dry_run should not be present in non-dry-run mode")
				}
			}
		})
	}
}

// ---------- Counters and Summary ----------

func TestCountersAndSummary(t *testing.T) {
	tests := []struct {
		name        string
		actions     []Action
		wantSummary string
		wantCounts  map[Action]int
	}{
		{
			name:        "no actions",
			actions:     nil,
			wantSummary: "0 issues synced, 0 comments synced, 0 state changes, 0 skipped",
			wantCounts:  map[Action]int{},
		},
		{
			name: "mixed actions",
			actions: []Action{
				ActionIssueCreated,
				ActionIssueCreated,
				ActionIssueUpdated,
				ActionCommentSynced,
				ActionCommentSynced,
				ActionCommentSynced,
				ActionStateChanged,
				ActionSkipped,
				ActionSkipped,
			},
			wantSummary: "3 issues synced, 3 comments synced, 1 state changes, 2 skipped",
			wantCounts: map[Action]int{
				ActionIssueCreated:  2,
				ActionIssueUpdated:  1,
				ActionCommentSynced: 3,
				ActionStateChanged:  1,
				ActionSkipped:       2,
			},
		},
		{
			name: "only issues",
			actions: []Action{
				ActionIssueCreated,
				ActionIssueUpdated,
			},
			wantSummary: "2 issues synced, 0 comments synced, 0 state changes, 0 skipped",
			wantCounts: map[Action]int{
				ActionIssueCreated: 1,
				ActionIssueUpdated: 1,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			l := New(Options{Writer: &buf, Level: LevelInfo})
			for _, a := range tt.actions {
				l.LogAction(a, "test")
			}

			if got := l.Summary(); got != tt.wantSummary {
				t.Errorf("Summary() = %q, want %q", got, tt.wantSummary)
			}

			counters := l.Counters()
			for action, want := range tt.wantCounts {
				if got := counters[action]; got != want {
					t.Errorf("counter[%s] = %d, want %d", action, got, want)
				}
			}
		})
	}
}

// ---------- LogSummary ----------

func TestLogSummary(t *testing.T) {
	tests := []struct {
		name       string
		dryRun     bool
		actions    []Action
		wantDryRun bool
		wantMsg    string
	}{
		{
			name:       "normal mode summary",
			dryRun:     false,
			actions:    []Action{ActionIssueCreated, ActionCommentSynced},
			wantDryRun: false,
			wantMsg:    "sync cycle complete",
		},
		{
			name:       "dry-run mode summary",
			dryRun:     true,
			actions:    []Action{ActionIssueCreated},
			wantDryRun: true,
			wantMsg:    "[DRY-RUN] sync cycle complete",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			l := New(Options{Writer: &buf, Level: LevelInfo, DryRun: tt.dryRun})
			for _, a := range tt.actions {
				l.LogAction(a, "test")
			}
			// Clear action lines
			buf.Reset()

			l.LogSummary()

			lines := parseLines(t, &buf)
			if len(lines) != 1 {
				t.Fatalf("expected 1 summary line, got %d", len(lines))
			}
			line := lines[0]

			if msg, _ := line["msg"].(string); msg != tt.wantMsg {
				t.Errorf("msg = %q, want %q", msg, tt.wantMsg)
			}
			if _, ok := line["issues_synced"]; !ok {
				t.Error("missing issues_synced field in summary")
			}
			if _, ok := line["comments_synced"]; !ok {
				t.Error("missing comments_synced field in summary")
			}
			if _, ok := line["state_changes"]; !ok {
				t.Error("missing state_changes field in summary")
			}
			if _, ok := line["skipped"]; !ok {
				t.Error("missing skipped field in summary")
			}
			if tt.wantDryRun {
				if dr, ok := line["dry_run"]; !ok || dr != true {
					t.Errorf("dry_run = %v, want true", dr)
				}
			}
		})
	}
}

// ---------- Reset ----------

func TestReset(t *testing.T) {
	var buf bytes.Buffer
	l := New(Options{Writer: &buf, Level: LevelInfo})
	l.LogAction(ActionIssueCreated, "test")
	l.LogAction(ActionCommentSynced, "test")

	l.Reset()

	counters := l.Counters()
	for action, count := range counters {
		if count != 0 {
			t.Errorf("counter[%s] = %d after Reset, want 0", action, count)
		}
	}

	if got := l.Summary(); got != "0 issues synced, 0 comments synced, 0 state changes, 0 skipped" {
		t.Errorf("Summary after Reset = %q, want zeroes", got)
	}
}

// ---------- DryRun accessor ----------

func TestDryRunAccessor(t *testing.T) {
	tests := []struct {
		name   string
		dryRun bool
	}{
		{name: "dry-run enabled", dryRun: true},
		{name: "dry-run disabled", dryRun: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			l := New(Options{Writer: &buf, Level: LevelInfo, DryRun: tt.dryRun})
			if got := l.DryRun(); got != tt.dryRun {
				t.Errorf("DryRun() = %v, want %v", got, tt.dryRun)
			}
		})
	}
}

// ---------- Concurrency safety ----------

func TestConcurrentLogAction(t *testing.T) {
	var buf bytes.Buffer
	l := New(Options{Writer: &buf, Level: LevelInfo})

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			l.LogAction(ActionIssueCreated, "concurrent")
		}()
	}
	wg.Wait()

	counters := l.Counters()
	if got := counters[ActionIssueCreated]; got != goroutines {
		t.Errorf("counter[issue_created] = %d, want %d", got, goroutines)
	}
}

// ---------- Extra attributes ----------

func TestLogActionExtraAttributes(t *testing.T) {
	var buf bytes.Buffer
	l := New(Options{Writer: &buf, Level: LevelInfo})
	l.LogAction(ActionIssueCreated, "created",
		"owner", "mggarofalo",
		"repo", "project-a",
		"issue_number", 42,
	)

	lines := parseLines(t, &buf)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	line := lines[0]

	if got, _ := line["owner"].(string); got != "mggarofalo" {
		t.Errorf("owner = %q, want %q", got, "mggarofalo")
	}
	if got, _ := line["repo"].(string); got != "project-a" {
		t.Errorf("repo = %q, want %q", got, "project-a")
	}
	// JSON numbers decode as float64
	if got, _ := line["issue_number"].(float64); got != 42 {
		t.Errorf("issue_number = %v, want 42", got)
	}
}
