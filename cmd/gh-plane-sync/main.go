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
	"github.com/mggarofalo/gh-plane-sync/internal/log"
)

// version and commit are set by ldflags at build time.
var (
	version = "dev"
	commit  = "unknown"
)

// syncCycle is the function called each tick. It is a package-level variable
// so that tests can replace it with a stub.
var syncCycle = func(_ context.Context, _ *config.Config, _ bool, _ *log.Logger) {
	// Placeholder: sync logic will be implemented in later issues.
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

	if os.Getenv("GITHUB_TOKEN") == "" {
		fmt.Fprintln(os.Stderr, "error: GITHUB_TOKEN environment variable is required")
		return 1
	}
	if os.Getenv("PLANE_API_KEY") == "" {
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
