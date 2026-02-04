---
title: Memory
description: Complete guide to using cache-memory and repo-memory for persistent file storage across workflow runs.
sidebar:
  order: 1500
---

Agentic workflows maintain persistent memory through **cache-memory** (GitHub Actions cache, 7-day retention) or **repo-memory** (Git branches, unlimited retention).

## Cache Memory

Provides persistent file storage across workflow runs via GitHub Actions cache. The compiler automatically configures the cache directory, restore/save operations, and progressive fallback keys at `/tmp/gh-aw/cache-memory/` (default) or `/tmp/gh-aw/cache-memory-{id}/` (additional caches).

## Enabling Cache Memory

```aw wrap
---
tools:
  cache-memory: true
---
```

Stores files at `/tmp/gh-aw/cache-memory/` using default key `memory-${{ github.workflow }}-${{ github.run_id }}`. Use standard file operations to store/retrieve JSON/YAML, text files, or subdirectories.

## Advanced Configuration

```aw wrap
---
tools:
  cache-memory:
    key: custom-memory-${{ github.workflow }}-${{ github.run_id }}
    retention-days: 30  # 1-90 days, extends access beyond cache expiration
---
```

## Multiple Cache Configurations

```aw wrap
---
tools:
  cache-memory:
    - id: default
      key: memory-default
    - id: session
      key: memory-session-${{ github.run_id }}
    - id: logs
      retention-days: 7
---
```

Mounts at `/tmp/gh-aw/cache-memory/` (default) or `/tmp/gh-aw/cache-memory-{id}/`. The `id` determines folder name; `key` defaults to `memory-{id}-${{ github.workflow }}-${{ github.run_id }}`.

## Cache Merging from Shared Workflows

```aw wrap
---
imports:
  - shared/mcp/server-memory.md
tools:
  cache-memory: true
---
```

Merge rules: **Single→Single** (local overrides), **Single→Multiple** (local converts to array), **Multiple→Multiple** (merge by `id`, local wins).

## Cache Behavior

GitHub Actions cache: 7-day retention, 10GB per repo, LRU eviction. Add `retention-days` to upload artifacts (1-90 days) for extended access.

Caches accessible across branches with unique per-run keys. Custom keys auto-append `-${{ github.run_id }}`. Progressive restore splits on dashes: `custom-memory-project-v1-${{ github.run_id }}` tries `custom-memory-project-v1-`, `custom-memory-project-`, `custom-memory-`, `custom-`.

## Best Practices

Use descriptive file/directory names, hierarchical cache keys (`project-${{ github.repository_owner }}-${{ github.workflow }}`), and appropriate scope (workflow-specific default or repository/user-wide). Monitor growth within 10GB limit.

## Troubleshooting

**Files not persisting**: Check cache key consistency and logs for restore/save messages.
**File access issues**: Create subdirectories first, verify permissions, use absolute paths.
**Cache size issues**: Track growth, clear periodically, or use time-based keys for auto-expiration.

## Security

Don't store sensitive data. Cache follows repository permissions, logs access. With [threat detection](/gh-aw/reference/safe-outputs/#threat-detection), cache saves only after validation succeeds (restore→modify→upload artifact→validate→save).

## Examples

See [Grumpy Code Reviewer](https://github.com/github/gh-aw/blob/main/.github/workflows/grumpy-reviewer.md) for tracking PR review history.

---

# Repo Memory

Persistent file storage via Git branches with unlimited retention. The compiler auto-configures branch cloning/creation, file access at `/tmp/gh-aw/repo-memory-{id}/`, commits/pushes, and merge conflict resolution (your changes win).

## Enabling Repo Memory

```aw wrap
---
tools:
  repo-memory: true
---
```

Creates branch `memory/default` at `/tmp/gh-aw/repo-memory-default/`. Files are stored within the branch at the branch name path (`memory/default/`). Files auto-commit/push after workflow completion.

## Advanced Configuration

```aw wrap
---
tools:
  repo-memory:
    branch-name: memory/custom-agent
    branch-prefix: tracking  # Custom prefix instead of "memory"
    description: "Long-term insights"
    file-glob: ["memory/custom-agent/*.md", "memory/custom-agent/*.json"]
    max-file-size: 1048576  # 1MB (default 10KB)
    max-file-count: 50      # default 100
    target-repo: "owner/repository"
    create-orphan: true     # default
---
```

**Branch Prefix**: Use `branch-prefix` to customize the branch name prefix (default is `memory`). The prefix must be 4-32 characters, alphanumeric with hyphens/underscores, and cannot be `copilot`. When set, branches are created as `{branch-prefix}/{id}` instead of `memory/{id}`.

**Note**: File glob patterns must include the full branch path structure. For branch `memory/custom-agent`, use patterns like `memory/custom-agent/*.json` to match files stored at that path within the branch.

## Multiple Repo Memory Configurations

```aw wrap
---
tools:
  repo-memory:
    - id: insights
      branch-prefix: daily  # Creates daily/insights branch
      file-glob: ["daily/insights/*.md"]
    - id: state
      file-glob: ["memory/state/*.json"]
      max-file-size: 524288  # 512KB
---
```

Mounts at `/tmp/gh-aw/repo-memory-{id}/` during workflow execution. Required `id` determines folder name; `branch-name` defaults to `{branch-prefix}/{id}` (where `branch-prefix` defaults to `memory`). Files are stored within the git branch at the branch name path (e.g., for branch `memory/code-metrics`, files are stored at `memory/code-metrics/` within the branch). **File glob patterns must include the full branch path.**

## Behavior

Branches auto-create as orphans (default) or clone with `--depth 1`. Changes auto-commit after validation (`file-glob`, `max-file-size`, `max-file-count`), pull with `-X ours` (your changes win), and push when changes detected and threat detection passes. Auto-adds `contents: write` permission.

## Best Practices

Use descriptive names, hierarchical branches (`memory/insights`), appropriate scope (workflow-specific, shared, or `target-repo` for cross-repository), and constraints to prevent abuse. Monitor branch size, clean periodically.

## Comparison

| Feature | Cache Memory | Repo Memory |
|---------|--------------|-------------|
| Storage | GitHub Actions Cache | Git Branches |
| Retention | 7 days | Unlimited |
| Size Limit | 10GB/repo | Repository limits |
| Version Control | No | Yes |
| Performance | Fast | Slower |
| Best For | Temporary/sessions | Long-term/history |

## Troubleshooting

**Branch not created**: Ensure `create-orphan: true` or create manually.
**Permission denied**: Compiler auto-adds `contents: write`.
**Validation failures**: Match `file-glob`, stay under `max-file-size` (10KB default) and `max-file-count` (100 default).
**Changes not persisting**: Check directory path, workflow completion, push errors in logs.
**Merge conflicts**: Uses `-X ours` (your changes win). Read before writing to preserve data.

## Security

Memory branches follow repository permissions. Use private repos for sensitive data, avoid storing secrets, set constraints (`file-glob`, `max-file-size`, `max-file-count`), consider branch protection, use `target-repo` to isolate.

## Examples

See [Deep Report](https://github.com/github/gh-aw/blob/main/.github/workflows/deep-report.md) and [Daily Firewall Report](https://github.com/github/gh-aw/blob/main/.github/workflows/daily-firewall-report.md) for long-term insights and historical data tracking.

---

## Related Documentation

- [Frontmatter](/gh-aw/reference/frontmatter/) - Complete frontmatter configuration guide
- [Safe Outputs](/gh-aw/reference/safe-outputs/) - Output processing and automation
- [GitHub Actions Cache Documentation](https://docs.github.com/en/actions/using-workflows/caching-dependencies-to-speed-up-workflows) - Official GitHub cache documentation