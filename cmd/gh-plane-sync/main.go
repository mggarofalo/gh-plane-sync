// Package main is the entrypoint for gh-plane-sync.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/mggarofalo/gh-plane-sync/internal/config"
)

// version and commit are set by ldflags at build time.
var (
	version = "dev"
	commit  = "unknown"
)

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

	// Placeholder: sync logic will be implemented in later issues.
	_ = cfg
	_ = dryRun
	_ = once

	return 0
}

func main() {
	os.Exit(run(os.Args[1:]))
}
