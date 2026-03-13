# AGENTS.md

## Project Overview

**gh-plane-sync** is a two-way sync bridge between GitHub Issues and [Plane](https://plane.so) work items.

- **GitHub is the source of truth** for issue content (title, description)
- **Plane is the source of truth** for workflow state (open/closed)
- Comments sync one-way: GitHub -> Plane (append-only)
- Mapping state persisted in SQLite

## Prerequisites

| Tool            | Version | Notes                        |
|-----------------|---------|------------------------------|
| Go              | 1.26+   | See `go.mod`                 |
| golangci-lint   | v2.11+  | `go install` or via Homebrew |
| Docker          | 24+     | For container builds         |

## Quick Start

```bash
make build        # Build binary
make test         # Run tests with race detector
make lint         # Run golangci-lint
make vet          # Run go vet
make fmt          # Format with gofumpt
make docker       # Build Docker image
```

## Project Structure

```
cmd/
  gh-plane-sync/    # CLI entrypoint
internal/           # Private packages (sync engine, config, store, clients)
```

Planned internal packages (see [Architecture](docs/architecture.md)):

| Package    | Purpose                                      |
|------------|----------------------------------------------|
| `config`   | YAML config parsing, CLI flag binding         |
| `github`   | GitHub API client (issues, comments)          |
| `plane`    | Plane API client (issues, comments, states)   |
| `store`    | SQLite mapping store (issue map, comments)    |
| `sync`     | Sync engine orchestrating the full cycle      |
| `log`      | Structured JSON logging                       |

## Issue Tracking

This project uses **Plane** (not Linear) for issue tracking.

- **Project identifier:** `GHSYNC`
- **Workspace:** managed via `plane` CLI
- **States:** Backlog -> Todo -> In Progress -> Done / Cancelled

### Working on Issues

1. Check assigned issues: `plane issue list -p GHSYNC --all`
2. Branch from `main`: `git checkout -b ghsync-<sequence_id>-<short-desc>`
3. Move issue to "In Progress": `plane issue update -p GHSYNC <issue-id> --state "In Progress"`
4. Implement, commit, push, open PR
5. After merge, move to "Done": `plane issue update -p GHSYNC <issue-id> --state Done`

## Branching Strategy

Simple feature-branch model off `main`:

```
main
 └── ghsync-1-config-schema
 └── ghsync-2-issue-sync
 └── ...
```

- Branch naming: `ghsync-<sequence_id>-<short-description>`
- All PRs target `main`
- Squash merge preferred
- `main` is protected: requires PR, passing CI, no force push

## Commit Convention

[Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <description>

Types: feat, fix, refactor, test, docs, chore, ci, build
Scopes: config, github, plane, store, sync, log, docker, ci
```

Examples:
- `feat(sync): implement GitHub->Plane issue sync`
- `fix(store): handle duplicate comment mappings`
- `ci: add release-please workflow`

## Configuration

Secrets via environment variables (never in config files):
- `GITHUB_TOKEN` — GitHub personal access token
- `PLANE_API_KEY` — Plane API key

Config file: YAML at `--config` path (default: `/etc/gh-plane-sync/config.yaml`)

See [Config Schema](docs/config-schema.md) for full specification.

## CLI Flags

```
--config <path>    Config file path (default: /etc/gh-plane-sync/config.yaml)
--dry-run          Log actions without making API writes
--once             Run one sync cycle and exit (for cron usage)
```

## CI/CD

- **CI:** lint, test, build on every PR and push to main
- **Release:** [release-please](https://github.com/googleapis/release-please) manages versioning and changelogs
- **Docker:** Container published to `ghcr.io/mggarofalo/gh-plane-sync` on each release

## Agent Rules

- Always run `make test` before committing
- Never commit secrets or config files containing credentials
- Keep `internal/` packages unexported — no `pkg/` directory
- Tests are required for all new packages
- Use table-driven tests
- Error wrapping: use `fmt.Errorf("context: %w", err)`
