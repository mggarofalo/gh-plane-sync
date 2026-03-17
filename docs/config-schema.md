# Config Schema

## Example `config.yaml`

```yaml
plane:
  api_url: "https://plane.example.com"
  workspace: "my-workspace"

# Global default state mappings (apply to all mappings unless overridden)
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

interval: "30m"  # sync interval (Go duration string, default: 30m, minimum: 1m)

mappings:
  # Uses global state defaults
  - github:
      owner: "mggarofalo"
      repo: "project-a"
    plane:
      project_id: "578529fe-dcba-4ec7-a898-5c7d583565d1"

  # Overrides state mappings for a project with custom Plane states
  - github:
      owner: "mggarofalo"
      repo: "project-b"
    plane:
      project_id: "aaaa-bbbb-cccc-dddd"
    states:
      github_to_plane:
        open: "Triage"
        closed: "Shipped"
      plane_to_github:
        shipped: "closed"
        wont_fix: "closed"
        triage: "open"
        in_dev: "open"

db_path: "/var/lib/gh-plane-sync/sync.db"
```

## Fields

### `plane`

| Field       | Type   | Required | Description                         |
|-------------|--------|----------|-------------------------------------|
| `api_url`   | string | yes      | Plane instance base URL             |
| `workspace` | string | yes      | Plane workspace slug                |

### `states` (top-level)

Global default state mappings. Applied to any mapping that doesn't define its own `states` block.

| Field              | Type            | Required | Description                         |
|--------------------|-----------------|----------|-------------------------------------|
| `github_to_plane`  | map[string]str  | yes      | GitHub state name -> Plane state    |
| `plane_to_github`  | map[string]str  | yes      | Plane state name -> GitHub state    |

### `mappings[]`

Each entry maps one GitHub repo to one Plane project. A repo can only appear once.

| Field                  | Type           | Required | Description                                          |
|------------------------|----------------|----------|------------------------------------------------------|
| `github.owner`         | string         | yes      | GitHub repo owner                                    |
| `github.repo`          | string         | yes      | GitHub repo name                                     |
| `plane.project_id`     | string         | yes      | Plane project UUID                                   |
| `states`               | states object  | no       | Per-mapping state overrides (replaces global defaults)|

### `interval`

Sync interval as a Go duration string (e.g. `30m`, `1h`, `1h30m`, `90s`). Controls how often the daemon runs sync cycles when not using `--once`.

| Type   | Required | Default | Minimum |
|--------|----------|---------|---------|
| string | no       | `30m`   | `1m`    |

### `db_path`

SQLite database path. Default: `/var/lib/gh-plane-sync/sync.db`

## Environment Variables

| Variable       | Required | Description                |
|----------------|----------|----------------------------|
| `GITHUB_TOKEN` | yes      | GitHub personal access token (needs `repo` scope) |
| `PLANE_API_KEY`| yes      | Plane API key              |
