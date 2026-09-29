---
description: Guide for choosing the right persistent memory strategy in agentic workflows — cache-memory, repo-memory, and repo-memory with wiki. Covers deduplication, stateful baseline comparison (metrics/coverage), and stateful scanning ("alert on new X").
---

# Persistent Memory in Agentic Workflows

For workflows that **persist state across runs** — deduplication, incremental processing, cross-run context, or knowledge accumulation.

> ⚠️ **`repo-memory` is NOT a synonym for `cache-memory`**. Different backends, different tradeoffs. `cache-memory` is almost always the right first choice.

---

## Quick Decision Guide

| Need | Use |
|---|---|
| Skip already-processed items (deduplication) | `cache-memory` ✅ first choice |
| Round-robin processing across runs | `cache-memory` ✅ first choice |
| Store ephemeral run state, analysis notes, or intermediate results | `cache-memory` ✅ first choice |
| Track a numeric metric and compare current vs. baseline (runs at least every 7 days) | `cache-memory` ✅ first choice |
| Long-lived knowledge base visible in PRs and code reviews | `repo-memory` |
| Baselines that must survive cache expiry (e.g. security findings, dedup lists) | `repo-memory` |
| Bounded append-only structured event history with record-level queries | `repo-memory` with the experimental ledger |
| Human-readable wiki pages for knowledge accumulation | `repo-memory` with `wiki: true` |
| Persist notes/state inline on the triggering issue or PR | `comment-memory` |
| Private-preview GitHub Drives backend (enrolled repos only) | `drive-memory` — see [drive-memory.md](drive-memory.md) |

**Default to `cache-memory` unless you have a specific reason to use `repo-memory`.** Do not suggest `drive-memory` unless the repository is confirmed enrolled in the GitHub Drives private preview.

---

## Built-in Memory via GitHub Graph and Git History

Before writing new persistent files, check whether GitHub and Git already expose the state you need.

### Practical strategies

| Goal | Built-in source | Caching strategy |
|---|---|---|
| Skip stale files in docs/code scans | Git history (`git log` / last modified commit per file) | Cache a repo watermark SHA or per-file SHAs; compare changed paths in newer commits |
| Avoid reopening known incidents | Issue/PR history (open + closed by label/title prefix) | Cache only canonical identifiers (issue numbers, advisory IDs) |
| Process incrementally across repo activity | PR merge history (`merged_at`, base branch) | Cache last merged PR number/timestamp; fetch only newer merges |
| Keep nightly triage focused | Issue timeline (`updated_at`, comments) | Cache last scan cursor (`updated_at` watermark); inspect only newer updates |
| Reuse expensive relationship lookups | GitHub graph links (issue ↔ PR ↔ commit) | Cache normalized link maps keyed by stable node IDs |

### Design guidance

- Prefer **stable identifiers** (`node_id`, issue/PR number, commit SHA) over mutable text.
- Persist **watermarks** (timestamp, commit SHA, PR number) instead of full snapshots.
- Treat built-in history as source of truth; store only incremental resume state.
- For cheap bounded queries (latest 20-100 items), recompute from GitHub/git instead of storing derived datasets.

---

## `cache-memory` — First Choice

GitHub Actions cache (`actions/cache`) persisting `/tmp/gh-aw/cache-memory/` via `@modelcontextprotocol/server-memory` MCP.

### When to use

- **Deduplication**: track processed items (issues, PRs, URLs, IDs)
- **Round-robin / incremental**: remember position across scheduled runs
- **Ephemeral structured state**: JSON blobs, queues, intermediate results
- **Metric baseline comparison**: store coverage/score/count, compare next run (see [Stateful Analysis](#stateful-analysis--baseline-comparison))
- **Visual regression baselines**: screenshots between PR runs (see `visual-regression.md`)
- **Tool call caching**: avoid redundant API calls

### Configuration

```yaml
tools:
  cache-memory: true
```

Custom key:

```yaml
tools:
  cache-memory:
    key: dedup-${{ github.event.schedule }}-${{ github.run_id }}
    retention-days: 30
    allowed-extensions: [".json"]
```

Multiple named caches:

```yaml
tools:
  cache-memory:
    - id: processed
      key: processed-items-${{ github.run_id }}
    - id: results
      key: results-${{ github.run_id }}
      retention-days: 14
```

### Custom validation

For domain-specific constraints beyond `allowed-extensions` (schema checks, cross-file uniqueness, timestamp policies), add `validation.script` — a Node.js script body (globals: `fs`, `path`, `memoryRoot`, `memoryId`, `memoryKind`) run over the memory directory after agent execution and before persistence. Throwing, returning `false`, timing out, or exiting nonzero rejects the save. Default timeout 1 minute (`validation.timeout-minutes`, max 5). Same mechanism for `repo-memory`. See [cache-memory reference](https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/cache-memory.md#custom-validation) and [repo-memory reference](https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/repo-memory.md#custom-validation).

```yaml
tools:
  cache-memory:
    validation:
      timeout-minutes: 1
      script: |
        const index = JSON.parse(fs.readFileSync(path.join(memoryRoot, "index.json"), "utf8"));
        if (!Array.isArray(index.entries)) throw new Error("index.json entries must be an array");
```

### Storage path

- Single cache: `/tmp/gh-aw/cache-memory/`
- Multiple caches: `/tmp/gh-aw/cache-memory/{id}/`

### Branch scoping

Caches are **branch-scoped** with default-branch fallback restore. A feature branch's first restore comes from `main`; subsequent saves fork a branch-local lineage. For warmed-state workflows, schedule on the default branch to reuse one lineage instead of fragmenting state.

### Deduplication example (scheduled workflow)

```markdown
---
on:
  schedule:
    - cron: "0 9 * * *"
permissions:
  issues: read
engine: copilot
tools:
  github:
    toolsets: [issues]
  cache-memory: true
safe-outputs:
  create-issue:
    title-prefix: "[daily-digest] "
    close-older-issues: true
    labels: [automation]
timeout-minutes: 15
---

Fetch the 20 most recently updated open issues.

Load `/tmp/gh-aw/cache-memory/processed.json` if it exists; it contains issue numbers from past digests. Skip any whose number already appears.

Summarize remaining (new) issues. If none, use the `noop` safe output.

Before finishing, write the updated processed-issue list back to `/tmp/gh-aw/cache-memory/processed.json` using filesystem-safe timestamp `YYYY-MM-DD-HH-MM-SS` (no colons, no `T`, no `Z`).
```

### Stateful Analysis / Baseline Comparison

Persist a baseline metric (coverage %, build time, benchmark score, audit count) and alert on regression. Cache miss → "no comparison this run" and baseline refreshes silently — tolerable for short-lived quality gates. If a lost baseline causes serious side-effects (e.g. duplicate security issues), use `repo-memory` instead (see [Stateful Scanning Pattern (repo-memory)](#stateful-scanning-pattern-repo-memory)).

> **Worked example** (coverage delta on every PR, with key design decisions): [memory-stateful-patterns.md](memory-stateful-patterns.md#baseline-comparison-cache-memory).

### Tradeoffs

| ✅ Pros | ❌ Cons |
|---|---|
| Zero repository noise — no commits, no PRs | Evicted when cache expires (default 7 days; use `retention-days` to extend up to 90) |
| Fast: no Git operations required | Not human-readable in GitHub UI |
| Works with Copilot, Claude, and custom engines | Data loss if cache is invalidated or expires |
| Supports multiple isolated caches per workflow | Files are uploaded as GitHub Actions artifacts — **no colons in filenames** |
| Scoped to workflow by default | |

### Filename safety

Cache-memory files upload as Actions artifacts. **Filenames must not contain colons** (NTFS limitation).

```bash
# ✅ GOOD
/tmp/gh-aw/cache-memory/state-2026-02-12-11-20-45.json

# ❌ BAD — colon breaks artifact upload
/tmp/gh-aw/cache-memory/state-2026-02-12T11:20:45Z.json
```

When instructing the agent for timestamped files, say: "Use `YYYY-MM-DD-HH-MM-SS` (no colons, no `T`, no `Z`)."

---

## `repo-memory` — Long-lived Repository Knowledge

Uses a dedicated Git branch (default: `memory/agent-notes`) to store files that persist indefinitely until explicitly deleted. The directory lives at `/tmp/gh-aw/repo-memory/`.

### When to use

- Knowledge must survive cache expiration
- Memory should be **visible in the repository** (auditable via Git history)
- Knowledge base grows over time (architecture notes, known issues)
- Changes need to appear in diffs and be reviewable

### Configuration

```yaml
tools:
  repo-memory:
    branch-name: memory/agent-notes   # Optional
    target-repo: owner/other-repo     # Optional: store in another repo
    allowed-extensions: [".json", ".md"]
    format-json: true                 # Optional: pretty-print .json (default: false)
    max-file-size: 10240              # bytes
    max-file-count: 100
```

Compiler creates a separate `push_repo_memory` job with `contents: write`; main agent job stays read-only.

### Structured event history: repo-memory ledger (experimental)

Use the ledger for immutable, structured events when each run should add records
and later runs need to query them. For example, a low-volume audit can append
one result per scan and query prior results by a stable application-level key.
The ledger exposes `ledger_append`, `ledger_get`, `ledger_query`, and
`ledger_status`; its SQLite query projection is disposable and rebuilt from the
persisted JSONL shards.

```yaml
tools:
  repo-memory:
    branch-name: memory/audit-history
    ledger:
      compaction:
        min-segments: 32
        max-segments: 32
```

Optionally set `ledger.schema` to a repository-relative JSON Schema file to
validate payloads. The supported schema vocabulary is intentionally limited;
see the [repo-memory reference](https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/repo-memory.md#structured-ledger).

Ledger records are append-only and concurrent histories converge when their
repo-memory branches merge, but this is not a transactional database. Include
stable event keys and timestamps in payloads, deduplicate and resolve
conflicting application events deterministically, and use `ledger_status` to
check for malformed or incomplete records. Do not edit ledger shard files
directly.

The ledger is experimental and bounded: it inspects at most 1024 shard files
(configurable with `ledger.max-shards`), each record is limited to 32 KiB, each
shard defaults to 100 KiB, each run defaults to a 10 KiB append budget, and each
query returns at most 500 records. Configure limits with
`max-segment-kb`, `max-record-kb`, and `max-patch-kb`; defaults align with
repo-memory's 100 KiB per-file and 10 KiB per-push defaults. Compilation warns
when ledger limits exceed the corresponding repo-memory persistence limits.
Each writing workflow invocation creates a shard, so a frequently running
workflow can exhaust the shard limit. Use ordinary repo-memory files for
replaceable snapshots, pruned baselines, or histories that need more than 1024
writer shards. Use bounded declarative `ledger.compaction` options to compact
stable segments; custom JavaScript compactor scripts are disabled because Node's
in-process VM is not a security boundary. Ledger workflows require AWF's
Cloud Hypervisor runtime, which keeps ledger storage outside the agent's
`filesystem.allowWrite` paths; only the MCP ledger server can append records.
The persistence job ignores agent-supplied coverage files and overwrites of
trusted shards, then verifies replacement coverage before retirement.

The ledger is eventually convergent, not transactional or exactly-once. Record
SHA-256 values are unkeyed checksums, not authentication; rely on the AWF write
boundary, use stable application keys, and deterministically resolve duplicates
and concurrent conflicts. Compaction, normalization, and save details appear in
the persistence step summary. Every successful append also emits a redacted
`ledger_mutation` audit entry to a dedicated ledger transaction log; a trusted
post-agent step revalidates and merges those entries into the safe outputs for
threat detection, and their safe-output handler only logs that metadata and
performs no side effects.

### Tradeoffs

| ✅ Pros | ❌ Cons |
|---|---|
| Persists indefinitely (no expiry) | Produces Git commits — repository noise |
| Auditable: Git history shows every change | Slower: requires Git clone + push |
| Survives cache invalidation | Not available for Copilot engine (requires GitHub tools) |
| Human-readable via GitHub branch UI | More complex setup |
| Can target a different repository | |
| Ledger mode provides immutable structured records and queries | Experimental, append-only, and limited to 1024 shard files by default |

---

## `repo-memory` with `wiki: true` — GitHub Wiki Backend

`repo-memory` variant that stores files in the **GitHub Wiki** (`<repo>.wiki.git`) instead of a branch.

### When to use

- Structured, human-readable documentation pages
- Knowledge for **human consumption** (browsable wikis)
- Living knowledge base or FAQ

### Configuration

```yaml
tools:
  repo-memory:
    wiki: true
    allowed-extensions: [".md"]
```

The compiler creates a separate `push_repo_memory` job with `contents: write`; the main agent job stays read-only.

Use GitHub Wiki conventions: `[[Page Name]]` for internal links, hyphens instead of spaces in filenames.

### Tradeoffs

| ✅ Pros | ❌ Cons |
|---|---|
| Browsable in the GitHub Wiki UI | Produces Git commits to wiki repo |
| Great for human-readable knowledge bases | Restricted to `.md` files in practice |
| Standard Markdown with wiki link syntax | Less suitable for structured JSON state |
| Separate from main repo history | |

---

## `comment-memory` — Managed Comment Persistence

Uses a `<gh-aw-comment-memory>` XML block in an issue/PR comment as persistent memory. The agent edits markdown files under `/tmp/gh-aw/comment-memory/`; the safe-output processor syncs changes back to the managed comment.

### When to use

- Workflow notes/statuses visible inline on the triggering issue or PR
- State tied to a specific issue or PR lifecycle
- Running track records (status tables, checklists, summaries) readable without leaving the issue

Do NOT use for high-volume ephemeral state (use `cache-memory`), long-lived knowledge bases (use `repo-memory`), or cross-issue data.

### Configuration

```yaml
tools:
  comment-memory: true   # enable with defaults
```

Advanced:

```yaml
tools:
  comment-memory:
    memory-id: status          # Optional: identifier in XML marker (default: "default")
    target: triggering         # Optional: "triggering" (default), "*", or explicit number
    target-repo: owner/other   # Optional: cross-repository
    max: 1                     # Optional: max updates per run (default: 1)
    footer: false              # Optional: omit AI-generated footer (default: true)
```

### How it works

1. **Pre-agent**: reads `<gh-aw-comment-memory id="<memory-id>">` and writes to `/tmp/gh-aw/comment-memory/<memory_id>.md`.
2. **Agent**: edits the markdown file directly — no safe-output tool call needed.
3. **Post-agent**: processor reads edited file and upserts the managed comment, replacing only the XML-fenced block.

Multiple memory IDs in one comment are supported; each maps to a separate `*.md` file.

### Tradeoffs

| ✅ Pros | ❌ Cons |
|---|---|
| Visible in GitHub UI inline on the issue/PR | Requires `issues:write` or `pull-requests:write` |
| No separate branch or cache | One comment block per `memory-id` per target |
| Agent edits plain markdown — no tool call needed | Not suited for large structured data |
| Tied to issue/PR lifecycle | Not available without a triggering issue or PR |

---

## Stateful Scanning Pattern (repo-memory)

Persist a baseline JSON file between runs to alert only on *new* findings — vulnerability scans, dependency audits, licence checks. Unlike `cache-memory`, the baseline survives cache expiry, so a missed cycle won't flood the repo with duplicate issues. Store only stable identifiers (advisory IDs), cap output with `max:`, treat missing baseline as `[]`. Requires Claude or custom engine — not Copilot.

Keep this as a replaceable repo-memory snapshot by default. Use the experimental
ledger only when the audit needs an append-only record of each scan or finding
and its expected writer count fits the 100-shard limit; it does not replace,
prune, or compact an existing baseline.

> **Worked example** (nightly npm vulnerability scan, with key design decisions): [memory-stateful-patterns.md](memory-stateful-patterns.md#stateful-scanning-repo-memory).

---

## Summary Comparison

| Feature | `cache-memory` | `repo-memory` | `repo-memory` + wiki | `comment-memory` |
|---|---|---|---|---|
| **First choice** | ✅ Yes | No | No | No |
| **Storage backend** | GitHub Actions cache | Git branch | GitHub Wiki | Issue/PR comment |
| **Persistence** | Up to 90 days | Indefinite | Indefinite | Issue/PR lifetime |
| **Compiler adds `contents: write`** | No | Yes (push job) | Yes (push job) | No |
| **Repository noise** | None | Git commits | Wiki commits | Comment updates |
| **Human-readable in GitHub** | No | Via branch UI | Via Wiki UI | ✅ Inline on issue/PR |
| **Structured data (JSON)** | ✅ Ideal | Possible | Not recommended | Not recommended |
| **Filename restrictions** | No colons in names | None | Hyphens for spaces | None |
| **Engine compatibility** | Copilot, Claude, custom | Claude, custom | Claude, custom | Claude, custom |

---

## Anti-patterns

- ❌ **Do not invent `repo-memory` as a synonym for `cache-memory`** — they are different tools
- ❌ **Do not use `repo-memory` for ephemeral per-run state** — use `cache-memory`
- ❌ **Do not use `cache-memory` when you need indefinite persistence** — use `repo-memory`
- ❌ **Do not include colons in cache-memory filenames** — artifact upload will fail
