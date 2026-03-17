// Package main is the entrypoint for gh-plane-sync.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mggarofalo/gh-plane-sync/internal/config"
	"github.com/mggarofalo/gh-plane-sync/internal/github"
	"github.com/mggarofalo/gh-plane-sync/internal/log"
	"github.com/mggarofalo/gh-plane-sync/internal/plane"
	"github.com/mggarofalo/gh-plane-sync/internal/store"
	"github.com/mggarofalo/gh-plane-sync/internal/sync"
)

// version and commit are set by ldflags at build time.
var (
	version = "dev"
	commit  = "unknown"
)

// syncCycle is the function called each tick. It is a package-level variable
// so that tests can replace it with a stub.
var syncCycle = func(_ context.Context, _ *config.Config, _ bool, _ *log.Logger) {
	// Placeholder: replaced at runtime in run().
}

// run encapsulates the application logic so it can be tested. It returns an
// exit code: 0 for success, 1 for failure.
func run(args []string) int {
	fs := flag.NewFlagSet("gh-plane-sync", flag.ContinueOnError)

	configPath := fs.String("config", config.DefaultConfigPath, "path to YAML config file")
	dryRun := fs.Bool("dry-run", false, "log actions without making API writes")
	once := fs.Bool("once", false, "run one sync cycle and exit")
	showVersion := fs.Bool("version", false, "print version and exit")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	if *showVersion {
		fmt.Printf("gh-plane-sync %s (%s)\n", version, commit)
		return 0
	}

	ghToken := os.Getenv("GITHUB_TOKEN")
	if ghToken == "" {
		fmt.Fprintln(os.Stderr, "error: GITHUB_TOKEN environment variable is required")
		return 1
	}
	planeAPIKey := os.Getenv("PLANE_API_KEY")
	if planeAPIKey == "" {
		fmt.Fprintln(os.Stderr, "error: PLANE_API_KEY environment variable is required")
		return 1
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	logger := log.New(log.Options{
		Writer: os.Stdout,
		Level:  log.LevelInfo,
		DryRun: *dryRun,
	})

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	defer func() { _ = db.Close() }()

	ghBaseURL := os.Getenv("GITHUB_API_BASE_URL") // empty → default (api.github.com)
	ghClient := github.NewHTTPClient(ghToken, ghBaseURL)
	planeClient := plane.NewHTTPClient(planeAPIKey, cfg.Plane.APIURL)

	engine := sync.NewEngine(
		sync.NewGitHubAdapter(ghClient),
		sync.NewPlaneAdapter(planeClient),
		db,
		logger,
		cfg,
	)

	// Wire real sync cycle using the engine.
	syncCycle = func(ctx context.Context, _ *config.Config, _ bool, l *log.Logger) {
		if err := engine.SyncIssues(ctx); err != nil {
			l.Error("issue sync failed", "error", err)
		}
		if err := engine.SyncComments(ctx); err != nil {
			l.Error("comment sync failed", "error", err)
		}
		l.LogSummary()
		l.Reset()
	}

	// --once: run a single sync cycle and exit.
	if *once {
		logger.Info("running single sync cycle", "version", version)
		ctx := context.Background()
		syncCycle(ctx, cfg, *dryRun, logger)
		return 0
	}

	// Daemon mode: set up signal handling and run sync cycles on interval.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	return runDaemon(ctx, cfg, *dryRun, logger)
}

// runDaemon runs the sync loop, ticking at the configured interval. It
// watches the provided context for cancellation to perform graceful shutdown:
// the current sync cycle is allowed to finish before the function returns.
func runDaemon(ctx context.Context, cfg *config.Config, dryRun bool, logger *log.Logger) int {
	logger.Info("starting daemon",
		"version", version,
		"interval", cfg.Interval.String(),
	)

	// Run the first sync cycle immediately.
	syncCycle(ctx, cfg, dryRun, logger)

	if ctx.Err() != nil {
		logger.Info("shutdown requested, exiting after initial sync")
		return 0
	}

	logNextSync(logger, cfg.Interval.Duration)

	ticker := time.NewTicker(cfg.Interval.Duration)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("shutdown signal received, exiting gracefully")
			return 0
		case <-ticker.C:
			syncCycle(ctx, cfg, dryRun, logger)

			if ctx.Err() != nil {
				logger.Info("shutdown requested, exiting after sync cycle")
				return 0
			}

			logNextSync(logger, cfg.Interval.Duration)
		}
	}
}

// logNextSync logs the next scheduled sync time.
func logNextSync(logger *log.Logger, interval time.Duration) {
	next := time.Now().Add(interval)
	logger.Info("next sync scheduled",
		"next_sync", next.Format(time.RFC3339),
		"interval", interval.String(),
	)
}

func main() {
	os.Exit(run(os.Args[1:]))
}
