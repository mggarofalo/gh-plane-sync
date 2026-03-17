// Package log provides structured JSON logging with dry-run support
// for gh-plane-sync. It wraps log/slog to produce machine-readable
// output and tracks sync actions for end-of-cycle summaries.
package log

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
)

// Level represents a log level that can be parsed from configuration.
type Level = slog.Level

// Predefined log levels matching slog conventions.
const (
	LevelDebug = slog.LevelDebug
	LevelInfo  = slog.LevelInfo
	LevelWarn  = slog.LevelWarn
	LevelError = slog.LevelError
)

// ParseLevel converts a string log level name to its slog.Level value.
// Recognised inputs (case-insensitive): debug, info, warn, error.
func ParseLevel(s string) (Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return LevelDebug, nil
	case "info":
		return LevelInfo, nil
	case "warn", "warning":
		return LevelWarn, nil
	case "error":
		return LevelError, nil
	default:
		return LevelInfo, fmt.Errorf("parse log level: %w",
			fmt.Errorf("unknown level %q", s))
	}
}

// Action describes the kind of sync operation that was performed (or
// would be performed in dry-run mode).
type Action string

// Recognised sync actions.
const (
	ActionIssueCreated  Action = "issue_created"
	ActionIssueUpdated  Action = "issue_updated"
	ActionCommentSynced Action = "comment_synced"
	ActionStateChanged  Action = "state_changed"
	ActionSkipped       Action = "skipped"
)

// Logger wraps slog.Logger with dry-run awareness and action counting.
type Logger struct {
	slog   *slog.Logger
	dryRun bool

	mu       sync.Mutex
	counters map[Action]int
}

// Options configures a new Logger.
type Options struct {
	// Writer is the destination for log output. Must not be nil.
	Writer io.Writer
	// Level is the minimum log level to emit.
	Level Level
	// DryRun, when true, prefixes action messages with [DRY-RUN] and
	// adds a "dry_run" attribute to every action log line.
	DryRun bool
}

// New creates a Logger from the given options.
func New(opts Options) *Logger {
	h := slog.NewJSONHandler(opts.Writer, &slog.HandlerOptions{
		Level: opts.Level,
	})
	return &Logger{
		slog:     slog.New(h),
		dryRun:   opts.DryRun,
		counters: make(map[Action]int),
	}
}

// DryRun reports whether the logger is in dry-run mode.
func (l *Logger) DryRun() bool {
	return l.dryRun
}

// Debug logs a message at debug level.
func (l *Logger) Debug(msg string, args ...any) {
	l.slog.Debug(msg, args...)
}

// Info logs a message at info level.
func (l *Logger) Info(msg string, args ...any) {
	l.slog.Info(msg, args...)
}

// Warn logs a message at warn level.
func (l *Logger) Warn(msg string, args ...any) {
	l.slog.Warn(msg, args...)
}

// Error logs a message at error level.
func (l *Logger) Error(msg string, args ...any) {
	l.slog.Error(msg, args...)
}

// LogAction records a sync action and emits a structured log line at
// info level. In dry-run mode the message is prefixed with "[DRY-RUN]"
// and a dry_run=true attribute is added.
//
// Each call increments the internal counter for the given action,
// which is reported by Summary.
func (l *Logger) LogAction(action Action, msg string, args ...any) {
	l.mu.Lock()
	l.counters[action]++
	l.mu.Unlock()

	attrs := make([]any, 0, len(args)+4)
	attrs = append(attrs, "action", string(action))
	if l.dryRun {
		attrs = append(attrs, "dry_run", true)
		msg = "[DRY-RUN] " + msg
	}
	attrs = append(attrs, args...)

	l.slog.Info(msg, attrs...)
}

// snapshot reads all action counters under a single lock.
func (l *Logger) snapshot() (issues, comments, states, skipped int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	issues = l.counters[ActionIssueCreated] + l.counters[ActionIssueUpdated]
	comments = l.counters[ActionCommentSynced]
	states = l.counters[ActionStateChanged]
	skipped = l.counters[ActionSkipped]
	return
}

// Summary returns a pre-formatted summary of all actions recorded
// since the Logger was created (or since the last Reset).
func (l *Logger) Summary() string {
	issues, comments, states, skipped := l.snapshot()
	return fmt.Sprintf(
		"%d issues synced, %d comments synced, %d state changes, %d skipped",
		issues, comments, states, skipped,
	)
}

// LogSummary emits the cycle summary at info level. In dry-run mode
// the message is prefixed with "[DRY-RUN]".
func (l *Logger) LogSummary() {
	issues, comments, states, skipped := l.snapshot()
	summary := fmt.Sprintf(
		"%d issues synced, %d comments synced, %d state changes, %d skipped",
		issues, comments, states, skipped,
	)

	msg := "sync cycle complete"
	if l.dryRun {
		msg = "[DRY-RUN] " + msg
	}

	attrs := []any{
		"summary", summary,
		"issues_synced", issues,
		"comments_synced", comments,
		"state_changes", states,
		"skipped", skipped,
	}
	if l.dryRun {
		attrs = append(attrs, "dry_run", true)
	}

	l.slog.Info(msg, attrs...)
}

// Reset zeroes all action counters, preparing the Logger for a new
// sync cycle.
func (l *Logger) Reset() {
	l.mu.Lock()
	l.counters = make(map[Action]int)
	l.mu.Unlock()
}

// Counters returns a snapshot of the current action counts. Useful
// for testing.
func (l *Logger) Counters() map[Action]int {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make(map[Action]int, len(l.counters))
	for k, v := range l.counters {
		out[k] = v
	}
	return out
}
