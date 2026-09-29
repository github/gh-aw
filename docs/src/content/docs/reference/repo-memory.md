---
title: Repo Memory
description: Guide to using repo-memory for persistent file storage via Git branches with unlimited retention.
sidebar:
  order: 1510
---

Repo memory provides persistent file storage via Git branches with unlimited retention. The compiler auto-configures branch cloning/creation, file access at `/tmp/gh-aw/repo-memory-{id}/`, commits/pushes, and merge conflict resolution (your changes win).

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
    branch-name: memory/custom-agent-for-aw
    branch-prefix: tracking  # Custom prefix instead of "memory"
    description: "Long-term insights"
    file-glob: ["*.md", "*.json"]
    max-file-size: 1048576  # 1MB (default 100KB)
    max-file-count: 50      # default 100
    max-patch-size: 1048576  # 1MB max (default 10KB)
    target-repo: "owner/repository"
    create-orphan: true     # default
    allowed-extensions: [".json", ".txt", ".md"]  # Restrict file types (default: empty/all files allowed)
    format-json: true       # Pretty-print .json files (default: false)
    validation:
      timeout-minutes: 1
      script: |
        const data = JSON.parse(fs.readFileSync(path.join(memoryRoot, "state.json"), "utf8"));
        if (!Array.isArray(data.items)) throw new Error("state.json must contain an items array");
---
```

`branch-prefix` changes the default `memory` prefix and must be 4-32 alphanumeric, hyphen, or underscore characters; it cannot be `copilot`. `allowed-extensions` limits which file types can be stored, `format-json: true` pretty-prints `.json` files before commit, `validation.script` runs a custom JavaScript domain validator before persistence, and `max-patch-size` caps the total diff size for one push (default 10KB, max 1MB) to prevent oversized updates.

## Structured Ledger

:::caution[Experimental]
The repo-memory ledger is experimental and its configuration or behavior may change. Enabling it emits a compile-time warning.
:::

Enable an optional, domain-neutral ledger within repo memory:

```aw wrap
---
tools:
  repo-memory:
    ledger:
---
```

The ledger tools append immutable structured records and retrieve or query them without exposing storage paths or SQL to the agent. Queries return records in stable SHA order and support bounded cursor pagination: pass the previous response's `nextCursor` as `after` and continue while `hasMore` is true. A cursor is exclusive and applies to the same query filters; concurrent writes may change the matching set between pages, so pagination is not a snapshot. Repo memory persists the append-only records in Git; an ephemeral local index is reconstructed from those records on each run. Independent concurrent writers can append records, and their histories converge when repo memory merges them. A missing parent is reported as incomplete rather than discarding its record; malformed records are isolated and reported in ledger status.

To validate application records as well as the built-in envelope, specify a repository-relative local schema using the supported simplified JSON Schema vocabulary:

```yaml
tools:
  repo-memory:
    ledger:
      schema: .github/schemas/ledger.schema.json
      max-shards: 256       # default 1024
      max-segment-kb: 100   # default 100 KiB (repo-memory max-file-size default)
      max-record-kb: 32     # default 32 KiB
      max-patch-kb: 10      # default 10 KiB (repo-memory max-patch-size default)
      compaction:
        min-segments: 32     # default 32 stable closed segments
        max-segments: 32     # default 32 segments compacted per run
```

The ledger is an eventually convergent append-only store, not a distributed transactional database. Applications must define their own deterministic conflict resolution for concurrent records.

Ledger segment and record limits should stay within repo-memory's `max-file-size` limit, and the per-run ledger patch limit should stay within `max-patch-size`. If a ledger limit exceeds its corresponding repo-memory limit, compilation emits a warning because persistence may reject the ledger files or patch.

Compaction is declarative: when the number of stable closed shards reaches `min-segments`, the trusted persistence job deterministically selects up to `max-segments` shards in lexical segment-ID order. The runtime deduplicates records by SHA, validates and sorts them, writes an immutable replacement, and retires sources only after verifying that the replacement contains each source record. It excludes the current run's shard. Compaction failures are fail-open, and selection, record, replacement, retirement, normalization, and save details appear in the persistence step summary. Custom JavaScript compactor scripts are disabled because an in-process Node VM is not a security boundary.

Ledger workflows require AWF's Cloud Hypervisor runtime. The compiler withholds repo-memory ledger paths from `filesystem.allowWrite` and rejects allow-write paths that overlap the ledger directory; agents append only through ledger MCP tools. The persistence job also ignores agent artifacts that overwrite existing trusted shards or supply coverage declarations. Record SHA-256 values detect accidental corruption, not malicious forgery, so do not treat the ledger as tamper-proof if its write boundary is bypassed. Cloud Hypervisor is a preview runtime and is limited to supported GitHub-hosted Linux x86_64 runners.

Each successful agent append also emits a redacted `ledger_mutation` audit entry (operation, record ID and type, timestamp, parent hashes, record SHA, payload SHA) to a dedicated ledger transaction log. A trusted post-agent step revalidates, redacts, deduplicates, and bounds those entries before merging them into the safe-output file, where threat detection can review them. The matching safe-output handler is log-only: it reports the audit metadata and performs no side effects, because the append is already durable and the transaction log alone does not prove which writer produced an entry.

Concurrent/retried appends are at-least-once, not exactly-once: record UUIDs are not application idempotency keys, and the ledger does not provide transactions or uniqueness constraints. Include stable application keys and resolve duplicates/conflicts deterministically. SHA-256 is an unkeyed integrity checksum, not authentication.

**File Glob Matching Rules**:

- Patterns are matched against the **relative path** within the artifact directory — do **not** include the branch name.
- **Slashless patterns** (no `/` in the pattern, e.g. `*.json`, `*.md`) are matched against the full relative path, so a single `*` only matches files at the **artifact root (depth 0)**. They do _not_ match files inside subfolders (depth 1+) — use a pattern containing `/` (e.g. `**/*.json`) for that.
- **Patterns containing `/`** (e.g. `metrics/**`, `data/*.csv`) are matched against the full relative path from the artifact root and work as standard glob expressions.
- **Absolute paths** (patterns starting with `/`) are **not supported** and are rejected at compile time and runtime.

Example: with the default filter `["*.json", "*.md"]`, the root-level file `processed-discussions.json` is persisted (depth 0 ✓), but `discussion-task-miner/processed-discussions.json` (depth 1) is not — use `["**/*.json", "**/*.md"]` to also match files nested in subfolders.

## Multiple Configurations

```aw wrap
---
tools:
  repo-memory:
    - id: insights
      branch-prefix: daily  # Creates daily/insights branch
      file-glob: ["*.md"]
    - id: state
      file-glob: ["*.json"]
      max-file-size: 524288  # 512KB
---
```

Mounts at `/tmp/gh-aw/repo-memory-{id}/` during workflow execution. The required `id` determines the folder name, and `branch-name` defaults to `{branch-prefix}/{id}` with `memory` as the default prefix. Files are stored inside the branch under that branch-name path. File globs always match the relative path within the artifact directory, so never include the branch name; slashless patterns such as `*.json` match only files at the artifact root (depth 0).

## Behavior

Branches auto-create as orphans by default, or clone with `--depth 1`. After validating `file-glob`, `max-file-size`, and `max-file-count`, gh-aw auto-commits and pushes when changes are present and threat detection passes.

### Custom validation

Use `validation.script` when generic storage limits are not enough. The script is a JavaScript body executed with Node.js over the complete configured memory directory after `format-json` normalization and before artifact upload or branch commit. It runs in the agent job and is re-run in the repo-memory push job as defense in depth.

Available globals are Node.js `fs` and `path`, plus `memoryRoot`/`memoryDir`, `memoryId`, and `memoryKind` (`"repo"`). The working directory is the memory root. Environment variables available to the validator are intentionally limited to basic runner paths plus `GH_AW_MEMORY_ROOT`, `GH_AW_MEMORY_DIR`, `GH_AW_MEMORY_ID`, and `GH_AW_MEMORY_KIND`; GitHub tokens and write credentials are not passed to the validator subprocess. Network access follows the workflow runner's normal network policy. The default timeout is 1 minute and may be set with `validation.timeout-minutes` (1-5 minutes).

Throw an exception, return `false`, time out, exit nonzero, or modify a memory file to reject persistence. Validator stdout and stderr are reported separately from built-in storage validation output so agents can distinguish domain-schema validation from size/count checks.

Commits use the [GitHub GraphQL `createCommitOnBranch` mutation](https://docs.github.com/en/graphql/reference/mutations#createcommitonbranch), so they are automatically **Verified** with GitHub's GPG key and satisfy rulesets that require signed commits.

:::note[Signed-commit fallback limitation]
The GraphQL mutation does not support symlinks, executable files (`chmod +x`), or submodule entries. If your memory artifact contains any of these, the helper falls back to a plain `git push`, which signed-commit rulesets usually reject. Keep memory artifacts as regular plain-text files such as `.json`, `.jsonl`, `.txt`, `.md`, and `.csv`.
:::

## Comparison with Cache Memory

| Feature | Cache Memory | Repo Memory |
|---------|--------------|-------------|
| Storage | GitHub Actions Cache | Git Branches |
| Retention | 7 days | Unlimited |
| Size Limit | 10GB/repo | Repository limits |
| Version Control | No | Yes |
| Performance | Fast | Slower |
| Best For | Temporary/sessions | Long-term/history |

For fast 7-day caching without version control, see [Cache Memory](/gh-aw/reference/cache-memory/).

## Troubleshooting

- **Branch not created**: Ensure `create-orphan: true` is enabled, or create the branch manually.
- **Validation or patch-size failures**: Keep changes within `file-glob`, `max-file-size` (100KB default), `max-file-count` (100 default), and `max-patch-size` (10KB default).
- **Changes not persisting**: Confirm the directory path, let the workflow finish, and check the logs for push errors.
- **Merge conflicts**: Concurrent pushes are replayed onto the latest remote state, so your file changes win. `.jsonl` files are merged with `merge=union`, so rows written by concurrent runs are all kept; other file types keep the local version.
- **Many runs finishing at once**: Pushes are not serialized. Each run commits with a compare-and-swap on the remote branch head and, if another run wins the race, re-reads the head, rebases its change with JSONL union merging, and retries with exponential backoff. To avoid conflicts entirely, have each run write its own file instead of all runs editing the same file, for example `runs/${{ matrix.worker }}.jsonl` or `targets/${{ matrix.target }}.jsonl`.
- **GH013 — Commits must have verified signatures**: This usually means the artifact included a symlink, executable file, or submodule entry, which forced a fallback to plain `git push`. Remove the unsupported file type and re-run.

## Security

Do not store sensitive data in repo memory. It follows repository permissions, so use private repositories when appropriate, avoid secrets, set constraints such as `file-glob`, `max-file-size`, `max-file-count`, and `max-patch-size`, consider branch protection, and use `target-repo` when you want isolation.

## Examples

See [Deep Report](https://github.com/github/gh-aw/blob/main/.github/workflows/deep-report.md) and [Daily Firewall Report](https://github.com/github/gh-aw/blob/main/.github/workflows/daily-firewall-report.md) for long-term insights and historical data tracking.

## Learn More

See [Cache Memory](/gh-aw/reference/cache-memory/) for 7-day cache storage, [Frontmatter](/gh-aw/reference/frontmatter/) for full configuration details, and [Safe Outputs](/gh-aw/reference/safe-outputs/) for output automation.
