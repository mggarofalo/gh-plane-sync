# Config Schema

## Example `config.yaml`

```yaml
plane:
  api_url: "https://plane.example.com"
  workspace: "my-workspace"

mappings:
  - github:
      owner: "mggarofalo"
      repo: "my-project"
    plane:
      project_id: "578529fe-dcba-4ec7-a898-5c7d583565d1"

states:
  # GitHub state -> Plane state (used during GitHub->Plane issue sync)
  github_to_plane:
    open: "Backlog"
    closed: "Done"

  # Plane state -> GitHub state (used during Plane->GitHub state sync)
  plane_to_github:
    done: "closed"
    cancelled: "closed"
    backlog: "open"
    todo: "open"
    in_progress: "open"

db_path: "/var/lib/gh-plane-sync/sync.db"
```

## Fields

### `plane`

| Field       | Type   | Required | Description                         |
|-------------|--------|----------|-------------------------------------|
| `api_url`   | string | yes      | Plane instance base URL             |
| `workspace` | string | yes      | Plane workspace slug                |

### `mappings[]`

Each entry maps one GitHub repo to one Plane project. A repo can only appear once.

| Field                  | Type   | Required | Description              |
|------------------------|--------|----------|--------------------------|
| `github.owner`         | string | yes      | GitHub repo owner        |
| `github.repo`          | string | yes      | GitHub repo name         |
| `plane.project_id`     | string | yes      | Plane project UUID       |

### `states`

State mappings control how workflow states translate between systems.

| Field              | Type            | Required | Description                         |
|--------------------|-----------------|----------|-------------------------------------|
| `github_to_plane`  | map[string]str  | yes      | GitHub state name -> Plane state    |
| `plane_to_github`  | map[string]str  | yes      | Plane state name -> GitHub state    |

### `db_path`

SQLite database path. Default: `/var/lib/gh-plane-sync/sync.db`

## Environment Variables

| Variable       | Required | Description                |
|----------------|----------|----------------------------|
| `GITHUB_TOKEN` | yes      | GitHub personal access token (needs `repo` scope) |
| `PLANE_API_KEY`| yes      | Plane API key              |
