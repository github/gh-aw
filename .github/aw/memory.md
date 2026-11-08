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
| Bounded append-only structured event history and built-in current-state projections | Experimental `tools.ledger` — see [ledger.md](ledger.md) |
| Reusable lessons need semantic consolidation beyond deterministic cleanup | [Scheduled dreaming](#scheduled-dreaming) only when cheaper options cannot meet the need |
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

Declare required per-file JSON and JSONL schemas with `validation.json-schemas`; this runs without a custom script for cache-memory, repo-memory, and drive-memory:

```yaml
tools:
  cache-memory:
    validation:
      json-schemas:
        - file: state.json
          schema:
            type: object
            required: [version, items]
            additionalProperties: false
            properties:
              version: { enum: [1] }
              items:
                type: array
                items: { type: string }
      timeout-minutes: 1
      script: |
        if (memoryKind !== "cache") throw new Error("unexpected memory kind");
```

Each declaration has an inline schema object and may have a relative `file` path or glob (using `*` within a path segment and `**` across segments). When `file` is omitted, the schema applies to every eligible `.json` and `.jsonl` file in the memory directory, excluding `.git`; the format is inferred for each file and `format` must also be omitted. Otherwise, the format is inferred from each matched file's `.json` or `.jsonl` extension (case-insensitive); optional `format: json` or `format: jsonl` overrides inference and is required for other extensions. A path must match configured file filters and exist; a glob must match at least one file, and every match is validated. JSON is one non-empty document; JSONL validates records individually, accepts LF/CRLF and an optional final newline, allows an existing empty file, and rejects blank lines or malformed records with physical line numbers. Schemas validate without coercion or rewriting and run after filtering/normalization, before an optional `validation.script`; schema and script failures both reject persistence.

Supported schema keywords are `type` (including arrays of types and `null`), primitive `enum`, `required`, nested `properties`, `additionalProperties: false`, object-schema `items`, and standalone `oneOf`/`anyOf`, up to 32 levels. Numeric enum integers must be exactly representable by JavaScript, and values checked against numeric enums must not lose precision during JSON parsing. This is not full JSON Schema: `$ref`, `const`, `format`, patterns, bounds, `uniqueItems`, and other unsupported keywords are rejected. Use the optional Node.js script (globals: `fs`, `path`, `memoryRoot`, `memoryId`, `memoryKind`) for cross-file uniqueness, timestamps, positive-ID checks, and other domain rules. Its timeout defaults to 1 minute (`validation.timeout-minutes`, max 5). See [cache-memory reference](https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/cache-memory.md#custom-validation), [repo-memory reference](https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/repo-memory.md#custom-validation), and [drive-memory reference](https://github.com/github/gh-aw/blob/main/docs/src/content/docs/experimental/drive-memory.md#validation).
### Storage path

- Single cache: `/tmp/gh-aw/cache-memory/`
- Multiple caches: `/tmp/gh-aw/cache-memory/{id}/`

### Branch scoping

Caches are **branch-scoped** with default-branch fallback restore. A feature branch's first restore can come from the default branch; subsequent saves fork a branch-local lineage. Caches saved on a PR merge ref cannot be restored by a default-branch scheduled workflow. Default-branch fallback does not publish PR-local updates back to that branch. For warmed-state workflows, schedule on the default branch to reuse one lineage instead of fragmenting state.

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

Uses a dedicated Git branch (default: `memory/agent-notes`) to store long-lived files. Omission or local deletion during a workflow does not delete backing-branch files. The directory lives at `/tmp/gh-aw/repo-memory/`.

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

To push to ruleset-protected memory branches, add `github-app: {client-id, private-key}` (app needs **Contents: read and write**). Use `github-app-read` / `github-app-write` to split read and write apps; only the write app needs the ruleset bypass. Without an app, `github.token` is used.

### Structured event history: standalone ledger (experimental)

For immutable, structured events where each run appends records and later runs
query them by a stable key, use experimental `tools.ledger` instead — configured
independently of repo-memory. See [ledger.md](ledger.md) for configuration,
state-model types, limits, and compaction.

### Tradeoffs

| ✅ Pros | ❌ Cons |
|---|---|
| Persists indefinitely (no expiry) | Produces Git commits — repository noise |
| Auditable: Git history shows every change | Slower: requires Git clone + push |
| Survives cache invalidation | Shared-file writers need explicit ownership |
| Human-readable via GitHub branch UI | More complex setup |
| Can target a different repository | |

---

## Scheduled dreaming

Dreaming is a separate, scheduled workflow that turns accumulated observations
into a small, evidence-backed knowledge view for later runs. It is not a new
frontmatter field, ledger compaction, or permission to rewrite task state.
Use existing `tools.repo-memory` and, when immutable lessons and corrections
are useful, experimental `tools.ledger` with `type: notes`.

Use the cheapest sufficient option, in this order:

| Need | First choice |
|---|---|
| Exact-key deduplication, sorting, hash-based invalidation, retention of disposable state, rebuilding an index | Deterministic cleanup in the existing workflow; no model invocation |
| Meaning-based reconciliation or synthesis that fits an existing run's budget | Bounded review by the already-running agent; no additional workflow |
| Recurring semantic consolidation that the cheaper options cannot cover economically | A separate dreamer, only with primary evidence, a consumer, and a credible benefit |

Deterministic cleanup is preferable whenever the rules reliably express the
operation. Invalidating a changed citation by hash does not verify the new
assertion's meaning. Never prune canonical ledger evidence or producer task
state under the guise of retention; clean disposable state and derived views.
Keep semantic review by the already-running producer within its budget and
ownership; do not use it to rewrite another workflow's files.

Inspect the producer workflow and its actual memory format first. Repeated
investigations, tested strategies, environment gotchas, and recurring explicit
corrections are good inputs. A deduplication list, backlog cursor, numeric
baseline, one-off task, or raw transcript alone does not justify dreaming.
Preserve successful techniques as well as failures; distinguish observed
outcomes from hypotheses.

Recommend dreaming only when a later consumer will reuse the knowledge and
the expected reduction in repeated reasoning or stale advice justifies the
extra workflow, model invocation, and maintenance after cheaper options are
ruled out. Compare total cost, not just reduced online tokens; do not invent
savings. If the benefit is uncertain, do not add the dreamer.
Identify the observed problem, primary evidence source, later consumer, and
why an additional workflow is worth its total cost. Keep this a quick design
decision, not a separate investigation; defer the dreamer when these are unknown.
A recurring schedule or a growing note count
alone is not enough. Prefer direct recall and producer-owned corrections when
the existing view is already small, current, and cheap to use.

For `cache-memory`, scheduled and manual dreamer runs must use the repository's
actual default branch, not a hard-coded `main`. Scheduled dreaming is eligible
for default-branch history only, not PR-local caches. A PR merge-ref run may
restore an eligible default-branch cache, but its updates do not flow back to the
default branch. Keep PR-local cleanup inside the PR workflow.

Separate workflows do not automatically share cache-memory: default keys are
workflow-scoped. Verify compatible restore/save key prefixes, cache versions and
paths, schema, and integrity/access policy. `scope: repo` broadens eligible
restore keys, not GitHub's branch visibility; a shared key does not remove PR
isolation. Cache eviction remains possible, so define a rebuildable cold start.
If knowledge must outlive or cross PR boundaries, require explicitly authorized,
evidence-validated publication to durable repo-memory or a ledger. Do not silently
export PR content into shared memory to bypass cache isolation.

Once the need, cost, and input scope justify a separate dreamer, load
[dreaming.md](dreaming.md) for configuration, ownership, bounded consolidation,
publication, consumer recall, and examples. Do not load implementation details
merely because a workflow uses memory.

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

Persist a baseline JSON file between runs to alert only on *new* findings — vulnerability scans, dependency audits, licence checks. Unlike `cache-memory`, the baseline survives cache expiry, so a missed cycle won't flood the repo with duplicate issues. Store only stable identifiers (advisory IDs), cap output with `max:`, treat missing baseline as `[]`.

Keep this as a replaceable repo-memory snapshot by default. Use the experimental
ledger only when the audit needs an append-only record of each scan or finding
and its expected writer count fits the default 1024-shard limit; it does not replace,
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
