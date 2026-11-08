---
description: Repository overrides for workflow authoring, validation, and investigation.
---

# Repository Instructions

Apply to workflow creation, editing, review, diagnosis, debugging, and upgrades.
These rules override conflicting upstream guidance.

## Repository Rules

- After any `.github/workflows/*.md` edit, run one-shot `gh aw compile`; never `--watch`.
- Follow [debug-agentic-workflow.md](debug-agentic-workflow.md) for diagnosis,
  patching, mode selection, and live gates. Logs/audits do not authorize live runs.
- For ruleset-required workflows, keep workflow/job names stable; set
  `inlined-imports: true` when imports exist.
- Explain Copilot authentication accurately: `permissions: { copilot-requests: write }`
  uses `${{ github.token }}` for inference, without a PAT or `COPILOT_GITHUB_TOKEN`.
- Before inventing workflow/shared-component/tool/safe-output patterns, inspect
  `https://github.com/gm3dmo/the-power` through GitHub APIs or `gh` for prior art.

## Indexing and Search

Follow [DeterministicOps: Indexing and read-only search](../../docs/src/content/docs/patterns/deterministic-ops.md#indexing-and-read-only-search)
when integrating an existing indexer or search tool, including MCP.
Keep indexing in a separate deterministic job for cheap/incremental work, or
in a dedicated trusted workflow/action for expensive, slow-changing, shared data.
Run publishers only from trusted `main` workflow definitions and code, with
job-scoped indexing credentials; never execute PR code in that context.
Publish versioned Actions caches and restore only compatible, visible indexes.
Agents may search only: enforce read-only access, expose no build/refresh tools,
and never save agent-modified indexes or let PR runs mutate shared caches.
Require provenance, schema, and freshness checks before search; fail explicitly
on a missing/stale index or use the workflow's documented read-only fallback.
Do not use writable `cache-memory` as a shared search-index store.

## Session Graders

Ten fast built-ins run by default in conclusion after session collection.
Configure them with `graders.<builtin-id>.threshold`, `enabled`, or data-only
`config`; expensive built-ins require opt-in.

For custom outcomes, use `graders.<distinct-id>.script` with a synchronous
JavaScript body over immutable `session`, `config`, and `helpers` inputs.
Reuse `helpers.createSessionLookups(session)` for stats and event/tool lookups.
Return a finite number, `null` for unavailable evidence, or an object with
`value`; do not emit legacy metric arrays. The script limit is 256 KiB.

For repository-owned Bash compatibility, use `graders.operational-value.run`
with a confined `.sh` path. Read session JSONL from `$1` / `GH_AW_SESSION_FILE`
and JSON config from `GH_AW_GRADER_CONFIG`; retain `[{id,value}, ...]` output.
Remove file writes and live API/checkout reads; compile/lint rejects detected
writes. This trusted shell path is not yet microVM-isolated.

The compiler owns the isolated downstream `graders` job for both custom forms.
Never override built-in implementations, customize `jobs.graders`, or inject
user code or steps into `jobs.conclusion`.
Collect required evidence before session publication; graders cannot fetch it.
Use [operational-value-designer](../skills/operational-value-designer/SKILL.md)
for executable examples and evidence/fixture design.

Audit optionally retrieves registered custom results from the `graders`
artifact; `logs --graders` and `--artifacts graders` include both it and `usage`.
Custom results never replace conclusion's published session.

## Repository-Wide Debugging and Optimization

General "debug/optimize agentic workflows" requests start with failure clustering,
not a request to choose a workflow. Supplied workflow/run/failure scope takes
precedence. Follow [analyzing-runs.md](analyzing-runs.md) for collection, selection,
cached evidence, audits, and uncertainty reporting. Do not dispatch workflows for evidence.
Use single-run `audit --search` for BM25 evidence retrieval from the actual
unified session. Repetition grouping activates automatically when useful; do not
configure Drain3. Grouping never changes ranking and keeps structured outcomes
separate. These optional run-local groups are distinct from
fleet failure clusters and `audit --group` finding codes; relevance/anomaly scores
never replace full-stream findings or establish threat absence.
