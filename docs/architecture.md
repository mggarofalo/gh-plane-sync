# Architecture

## Sync Model

gh-plane-sync runs as a periodic sync process (via cron or `--once` flag). Each cycle:

```
┌──────────────────────────────────────────────────────────┐
│                      Sync Cycle                          │
│                                                          │
│  1. GitHub -> Plane: Issue Sync                          │
│     - Fetch open GitHub issues from configured repos     │
│     - Create/update Plane work items for unmapped/changed│
│     - Store mapping in SQLite                            │
│                                                          │
│  2. GitHub -> Plane: Comment Sync                        │
│     - Fetch comments on mapped GitHub issues             │
│     - Create Plane comments for unsynced ones            │
│     - Track synced comment IDs in SQLite                 │
│                                                          │
│  3. Plane -> GitHub: State Sync                          │
│     - Check Plane state for all mapped issues            │
│     - Close/reopen GitHub issues per state mapping       │
│     - Add sync comment on GitHub issue                   │
│                                                          │
│  4. Summary                                              │
│     - Log: X issues synced, Y comments, Z state changes  │
└──────────────────────────────────────────────────────────┘
```

## Data Flow

```
GitHub Issues API ──────► sync engine ──────► Plane API
        ▲                     │                   │
        │                     ▼                   │
        │               SQLite Store              │
        │            (issue_map, synced_           │
        │             comments, sync_state)        │
        │                     │                   │
        └─────────────────────┘                   │
         state changes (close/reopen)             │
                                                  ▼
                                          Plane Work Items
```

## Source of Truth

| Data           | Source of Truth | Direction        |
|----------------|-----------------|------------------|
| Issue content  | GitHub          | GitHub -> Plane  |
| Comments       | GitHub          | GitHub -> Plane  |
| Workflow state | Plane           | Plane -> GitHub  |

## SQLite Schema

### `issue_map`

Maps GitHub issues to Plane work items.

| Column              | Type    | Notes                    |
|---------------------|---------|--------------------------|
| github_owner        | TEXT    | Repository owner         |
| github_repo         | TEXT    | Repository name          |
| github_issue_number | INTEGER | Issue number             |
| plane_project_id    | TEXT    | Plane project UUID       |
| plane_issue_id      | TEXT    | Plane work item UUID     |
| created_at          | TEXT    | ISO 8601 timestamp       |
| updated_at          | TEXT    | ISO 8601 timestamp       |

### `synced_comments`

Tracks which GitHub comments have been synced to Plane.

| Column              | Type    | Notes                    |
|---------------------|---------|--------------------------|
| github_comment_id   | INTEGER | GitHub comment ID        |
| plane_comment_id    | TEXT    | Plane comment UUID       |
| github_issue_number | INTEGER | Parent issue number      |
| github_owner        | TEXT    | Repository owner         |
| github_repo         | TEXT    | Repository name          |
| synced_at           | TEXT    | ISO 8601 timestamp       |

### `sync_state`

Tracks incremental sync progress per repo.

| Column         | Type | Notes              |
|----------------|------|--------------------|
| github_owner   | TEXT | Repository owner   |
| github_repo    | TEXT | Repository name    |
| last_synced_at | TEXT | ISO 8601 timestamp |

## Package Dependency Graph

```
cmd/gh-plane-sync
    └── internal/config
    └── internal/sync
            ├── internal/github
            ├── internal/plane
            ├── internal/store
            └── internal/log
```

No circular dependencies. Each package depends only downward.
