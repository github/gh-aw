---
description: Private-preview GitHub Drives memory backend — experimental, gated, do not configure unless explicitly enrolled.
---

# Drive Memory (Private Preview)

Use these instructions only when a repository has been explicitly enrolled in the [GitHub Drives preview](https://github.com/actions/gh-drives-preview). Do not recommend or configure `drive-memory` otherwise — prefer [`cache-memory` or `repo-memory`](memory.md).

## What it is

- `tools.drive-memory` mounts a persistent GitHub Drive into the agent at `/tmp/gh-aw/drive-memory/` (or `/tmp/gh-aw/drive-memory-{id}/` for a named entry in a multi-drive array).
- Backed by an experimental service, not general `cache-memory`/`repo-memory`. GitHub Drives allows only one active writer per drive; overlapping runs writing the same drive can contend for the writer lease.
- The compiler checks out each drive before the agent runs and commits validated changes afterward. With threat detection enabled, it stages drive contents as an artifact and a separate `update_drive_memory` job publishes them only after detection succeeds and the drive hasn't changed since checkout.

## Configuration

```yaml
tools:
  drive-memory: true   # default drive, default config
```

```yaml
tools:
  drive-memory:
    drive-name: my-drive        # optional, defaults to "default"
    description: "..."          # optional, shown in agent prompt
    disk-size: 100M              # number + K/M/G/T suffix; ignored for existing drives
    prefetch: false               # optional, eagerly fetch existing contents
    restore-only: false           # optional, mount without committing changes
    allowed-extensions: [".json"] # optional
    validation:
      script: |
        // Node.js body; globals: fs, path, memoryRoot, memoryId, memoryKind
      timeout-minutes: 1
```

Multiple named drives (array form, each needs `id`):

```yaml
tools:
  drive-memory:
    - id: notes
      drive-name: agent-notes
    - id: cache
      drive-name: agent-cache
      restore-only: true
```

Atomic workflows (`imports: [shared/atomic.md]`) keep their sessions (`sessions/`) and, with Atomic 0.9.83 or newer, their workflow runs (`workflows/`) only through a drive entry with `id: atomic` declared in the workflow's own frontmatter. Add it when the workflow should resume earlier Atomic sessions or interrupted Atomic workflow runs; the shared engine definition never adds it. See [Atomic sessions on drive memory](https://github.github.com/gh-aw/reference/third-party-agent/#atomic-sessions-on-drive-memory) for the recommended entry.

## Compiler effects

- Grants the generated job `contents: read`, `id-token: write`, and the required `drives` permission.
- Adds a `push_drive_memory` (or threat-detection-gated `update_drive_memory`) job analogous to `push_repo_memory` for `repo-memory`.

## Limitations

- GitHub-hosted `ubuntu-latest` only; not supported inside job containers.
- Upstream actions have no versioned release — gh-aw pins the preview `main` commit.
- Do not store secrets in drive memory.
