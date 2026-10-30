---
title: Cache Memory
description: Guide to using cache-memory for persistent file storage across workflow runs with GitHub Actions cache.
sidebar:
  order: 1500
---

Cache memory provides persistent file storage across workflow runs via GitHub Actions cache. GitHub Actions evicts unused caches after 7 days. The compiler automatically configures the cache directory, restore/save operations, and progressive fallback keys at `/tmp/gh-aw/cache-memory/` (default) or `/tmp/gh-aw/cache-memory-{id}/` (additional caches).

## Enabling Cache Memory

```aw wrap
---
tools:
  cache-memory: true
---
```

Files are stored at `/tmp/gh-aw/cache-memory/` using a workflow-scoped cache key, via standard file operations.

## Advanced Configuration

```aw wrap
---
tools:
  cache-memory:
    key: custom-memory-${{ github.repository_owner }}
    retention-days: 30  # 1-90 days; controls uploaded artifact retention only
    allowed-extensions: [".json", ".txt", ".md"]  # Restrict file types (default: .json, .jsonl, .txt, .md, .csv)
    validation:
      timeout-minutes: 1
      json-schemas:
        - file: index.json
          schema:
            type: object
            required: [entries]
            additionalProperties: false
            properties:
              entries:
                type: array
                items: { type: string }
      script: |
        const index = JSON.parse(fs.readFileSync(path.join(memoryRoot, "index.json"), "utf8"));
        if (!Array.isArray(index.entries)) throw new Error("index.json entries must be an array");
---
```

> [!NOTE]
> Do not include `${{ github.run_id }}` in a user-supplied key — the compiler appends it automatically to the save key and generates stable restore-keys from the prefix.

### File Type Restrictions

The `allowed-extensions` field restricts which file types can be written to cache-memory (default: `.json`, `.jsonl`, `.txt`, `.md`, `.csv`).

```aw wrap
---
tools:
  cache-memory:
    allowed-extensions: [".json", ".jsonl", ".txt"]  # Only these extensions allowed
---
```

If files with disallowed extensions are found, the workflow will report validation failures.

When a cache is restored for agent execution, gh-aw also strips execute bits from restored working-tree files and removes disallowed file types before the agent can read them. See [ADR-26587](https://github.com/github/gh-aw/blob/main/docs/adr/26587-pre-agent-cache-memory-working-tree-sanitization.md) for the pre-agent sanitization contract behind `allowed-extensions`.

### Custom validation

Use `validation.json-schemas` for required JSON/JSONL structure checks. Each declaration has an inline schema and may have a relative `file` path or glob (`*` matches within a path segment; `**` spans segments). If `file` is omitted, the schema applies to every eligible `.json` and `.jsonl` file in the memory directory, excluding `.git`; omit `format` too so the format is inferred for each file. Otherwise, paths must match the configured `allowed-extensions` policy and exist in the candidate; globs must match at least one file, and every match is validated. JSON must contain one non-empty document. JSONL validates each record independently, accepts LF/CRLF and an optional final newline, permits an existing empty file, and rejects blank physical lines or malformed records with a line number. Validation does not rewrite data. Schemas run after filtering and configured normalization, before any optional `validation.script`, and before cache save; when threat detection is enabled, they also run in the `update_cache_memory` job before `actions/cache/save`.

The file format is inferred from each matched file's `.json` or `.jsonl` extension (case-insensitive), so a glob may match both formats. Optional `format: json` or `format: jsonl` overrides inference and is required for other extensions.

The supported vocabulary is limited to `type` (including type lists and `null`), primitive `enum`, `required`, nested `properties`, `additionalProperties: false`, object-schema `items`, and standalone `oneOf`/`anyOf`, with a maximum nesting depth of 32 and no sibling constraints beside alternatives. Numeric enum integers must be exactly representable by JavaScript, and values checked against numeric enums must not lose precision during JSON parsing. This is not full JSON Schema: `$ref`, `const`, schema `format`, `pattern`, numeric/string/array bounds, and `uniqueItems` are rejected. Use `validation.script` for domain-specific constraints such as cross-file uniqueness, positive-ID checks, or timestamp policies.

The optional JavaScript body runs with Node.js over the whole cache-memory directory after the agent and before the cache is saved (again in `update_cache_memory` when threat detection is enabled). Schemas always run first.

Available globals are Node.js `fs` and `path`, plus `memoryRoot`/`memoryDir`, `memoryId`, and `memoryKind` (`"cache"`). The working directory is the cache root. Environment variables available to the validator are intentionally limited to basic runner paths plus `GH_AW_MEMORY_ROOT`, `GH_AW_MEMORY_DIR`, `GH_AW_MEMORY_ID`, and `GH_AW_MEMORY_KIND`; GitHub tokens and write credentials are not passed to the validator subprocess. Network access follows the workflow runner's normal network policy. The default timeout is 1 minute and may be set with `validation.timeout-minutes` (1-5 minutes).

Throw an exception, return `false`, time out, exit nonzero, or modify a memory file to reject persistence. Validator stdout and stderr are reported separately from built-in storage validation output.

## Multiple Configurations

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

Mounts at `/tmp/gh-aw/cache-memory/` (default) or `/tmp/gh-aw/cache-memory-{id}/`. The `id` determines the folder name; `key` defaults to a workflow-scoped prefix derived from the sanitized workflow name.

## Using with MCP Servers

With `tools.cache-memory` enabled, MCP servers can persist temporary state under `/tmp/gh-aw/cache-memory/`. Configure the server in the workflow and point it at a subdirectory such as `/tmp/gh-aw/cache-memory/<server-name>/`.

## Behavior

GitHub Actions cache provides a 10GB per-repository limit with LRU eviction, and evicts unused entries after 7 days. `retention-days` controls only uploaded artifact retention (1-90 days); it does not extend cache lifetime. The repository-wide [artifact retention policy](/gh-aw/reference/compiler-enterprise-environment-controls/#artifact-retention) overrides this per-cache artifact setting when configured.

Cache memory is branch-scoped: runs restore from the same branch first, then can fall back to the default branch. On a non-default branch, the first restore often comes from the default branch; later saves create a branch-local cache lineage.

Absent cache files are expected on cold starts or after eviction. When the workflow defines initialization or a full-analysis fallback, the agent follows that path without reporting `report_missing_data`. Required cached inputs with no fallback still produce `report_missing_data`; malformed or unreadable required state also remains a failure unless the workflow explicitly defines recovery.

The compiler strips `${{ github.run_id }}` from restore keys so each run can fall back to earlier runs. For `scope: repo`, it also adds a broader restore key for cross-workflow sharing within the same branch scope.

## Best Practices

Use cache-memory for short-lived, branch-local state. Prefer scheduled runs on the default branch when a workflow depends on warmed caches. Use descriptive file names, hierarchical keys such as `project-${{ github.repository_owner }}-${{ github.workflow }}`, and the narrowest practical scope, then monitor total cache growth within the 10GB repository limit.

## Comparison with Repo Memory

| Feature | Cache Memory | Repo Memory |
|---------|--------------|-------------|
| Storage | GitHub Actions Cache | Git Branches |
| Retention | 7 days | Unlimited |
| Size Limit | 10GB/repo | Repository limits |
| Version Control | No | Yes |
| Performance | Fast | Slower |
| Best For | Temporary/sessions | Long-term/history |

## Automatic Cleanup

The [agentic maintenance](/gh-aw/reference/ephemerals/#cache-memory-cleanup) workflow periodically groups caches by key prefix (everything before the run ID) and keeps only the latest entry per group. Run the `Agentic Maintenance` workflow with the `clean_cache_memories` operation to trigger it manually.

## Troubleshooting

If files are not persisting, check cache key consistency and the restore/save log messages. For file access issues, create subdirectories first, verify permissions, and use absolute paths. If cache growth becomes a problem, clear old entries periodically or use time-based keys for auto-expiration.

When an agent calls `report_missing_data` with `reason: "cache_memory_miss"`, the conclusion handler can open a failure issue. A restore match does not guarantee that every expected file exists. Verify the required-input contract, the cache path (`/tmp/gh-aw/cache-memory/` by default or `/tmp/gh-aw/cache-memory-{id}/` for named caches), and key consistency before diagnosing a path problem. Workflows using cache only to speed up analysis should explicitly define their cold-start fallback.

## Integrity-Aware Caching

When a workflow uses `tools.github.min-integrity`, cache-memory automatically applies integrity-level isolation. Cache keys include the workflow's integrity level and a hash of the guard policy so that changing any policy field forces a cache miss.

The compiler generates git-backed branching steps around the agent. Before the agent runs, it checks out the matching integrity branch and merges down from all higher-integrity branches (higher integrity always wins conflicts). After the agent runs, changes are committed to that branch. The agent itself sees only plain files — the `.git/` directory rides along transparently in the Actions cache tarball.

### Merge semantics

| Run integrity | Sees data written by | Cannot see |
|---|---|---|
| `merged` | `merged` only | `approved`, `unapproved`, `none` |
| `approved` | `approved` + `merged` | `unapproved`, `none` |
| `unapproved` | `unapproved` + `approved` + `merged` | `none` |
| `none` | all levels | — |

This prevents a lower-integrity agent from poisoning data that a higher-integrity run would later read.

> [!NOTE]
> Existing caches will get a cache miss on first run after upgrading to a version that includes this feature — intentional, as legacy data has no integrity provenance.

## Security

Do not store sensitive data in cache memory. It follows repository permissions, and with [threat detection](/gh-aw/reference/threat-detection/) enabled, caches save only after validation succeeds (`restore → modify → upload artifact → validate → save`).

## Examples

See [Grumpy Code Reviewer](https://github.com/github/gh-aw/blob/main/.github/workflows/grumpy-reviewer.md) for tracking PR review history.

## Learn More

See [Repo Memory](/gh-aw/reference/repo-memory/) for git branch-based storage with unlimited retention, [Frontmatter](/gh-aw/reference/frontmatter/) for the full configuration model, and [Safe Outputs](/gh-aw/reference/safe-outputs/) for output processing and automation. For cache mechanics, refer to the [GitHub Actions cache documentation](https://docs.github.com/en/actions/using-workflows/caching-dependencies-to-speed-up-workflows).
