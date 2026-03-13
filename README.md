# gh-plane-sync

Two-way sync bridge between GitHub Issues and [Plane](https://plane.so) work items.

- **GitHub -> Plane:** Issues and comments sync into Plane work items
- **Plane -> GitHub:** Workflow state changes (Done/Cancelled) close GitHub issues

## How it Works

gh-plane-sync runs as a periodic job (via cron or container scheduler). Each cycle:

1. Fetches open GitHub issues from configured repos
2. Creates or updates corresponding Plane work items
3. Syncs new GitHub comments to Plane (append-only)
4. Checks Plane workflow states and closes/reopens GitHub issues accordingly

Issue content comes from GitHub. Workflow state comes from Plane.

## Install

```bash
go install github.com/mggarofalo/gh-plane-sync/cmd/gh-plane-sync@latest
```

Or use the container image:

```bash
docker pull ghcr.io/mggarofalo/gh-plane-sync:latest
```

## Configuration

Create a `config.yaml`:

```yaml
plane:
  api_url: "https://plane.example.com"
  workspace: "my-workspace"

mappings:
  - github:
      owner: "myorg"
      repo: "my-project"
    plane:
      project_id: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"

states:
  github_to_plane:
    open: "Backlog"
    closed: "Done"
  plane_to_github:
    done: "closed"
    cancelled: "closed"
    backlog: "open"
    todo: "open"
    in_progress: "open"

db_path: "./sync.db"
```

Set environment variables:

```bash
export GITHUB_TOKEN="ghp_..."
export PLANE_API_KEY="plane_..."
```

## Usage

```bash
# Run one sync cycle
gh-plane-sync --config config.yaml --once

# Dry run (log actions without making API calls)
gh-plane-sync --config config.yaml --once --dry-run
```

## Docker Compose

```yaml
services:
  gh-plane-sync:
    image: ghcr.io/mggarofalo/gh-plane-sync:latest
    restart: "no"
    environment:
      - GITHUB_TOKEN=${GITHUB_TOKEN}
      - PLANE_API_KEY=${PLANE_API_KEY}
    volumes:
      - ./config.yaml:/etc/gh-plane-sync/config.yaml:ro
      - sync-data:/var/lib/gh-plane-sync

volumes:
  sync-data:
```

Run on a schedule with cron:

```cron
*/30 * * * * docker compose -f /path/to/docker-compose.yaml run --rm gh-plane-sync
```

## Development

```bash
make build    # Build binary
make test     # Run tests
make lint     # Lint with golangci-lint
```

## License

MIT
