# Workflow Health — 2026-09-23T04:37Z

## RESOLVED: Metrics Collector "success-but-empty" defect (PR #62670 merged, gate verified working)
Predecessor P0/finding (#62731, filed 2026-09-22 by Agent Performance Analyzer) diagnosed that
Metrics Collector's post-EACCES-fix run reported `success` while performing only 3 exploratory
tool calls and writing no fresh data. PR github/gh-aw#62670 (merged 2026-09-22T16:55:53Z, fixes
#62656) added a content-based post-run gate to `metrics-collector.md`: it now fails loudly if
`metrics/latest.json`'s timestamp is unchanged/missing/stale or no `metrics/daily/*.json` was
written. **Verified working as designed** — the very next scheduled run (35811175614,
2026-09-23T02:38Z) correctly failed with `ERROR: metrics/latest.json timestamp is unchanged
(2026-09-01T02:50:27Z). No new metrics were collected.` #62731 (deeper root cause: why codex
terminates after ~3 tool calls) remains open — **do not close it**, only the silent-success
masking is fixed, not the underlying early-termination behavior.

## NEW P2: daily-fact 100% failure (15/15) — mempalace MCP server startup race
Root-caused the residual `daily-fact` watch item (previously mis-attributed as a possible
cloud-hypervisor EACCES straggler) as a **distinct, unrelated** defect: `shared/mcp/mempalace.md`
backgrounds a Python MCP server on port 8765 with no readiness probe, and the MCP gateway's
connectivity check consistently fails with `dial tcp 172.17.0.1:8765: connect: connection
refused` ~30-50s later. Confirmed via direct job-log inspection of 5 runs spanning 2026-09-16 to
2026-09-22, all identical signature; the auto-generated `[aw] Daily Fact failed` issues (#62666,
#62383, #61617, ...) self-expire without this diagnosis. Filed a new maintenance issue this run
with root cause, evidence, and a suggested fix (add a readiness probe/retry loop to the "Start
MemPalace MCP Server" step, or surface `/tmp/gh-aw/mcp-logs/mempalace/server.log` on failure).
Single-workflow scope (only `daily-fact.md` imports mempalace) — P2, not a systemic P0.

## failing-workflows.json triage (4 entries, re-verified live)
- **lint-monster**: fully recovered (2/2 most recent scheduled runs successful post-#62406).
- **daily-firewall-report**: fully recovered — latest run (2026-09-23T02:29Z) succeeded end-to-end
  (agent, safe_outputs, evals, cache_memory all green); prior day's run was `cancelled` (not a
  failure, different signal).
- **daily-go-test-parallelizer**: remains fully healthy, unaffected throughout.
- **cjs**: plain GH Actions workflow (path-triggered CI job), out of `gh aw` scope — the single
  `action_required` was an approval gate, not a failure.

## Compilation Status
299/299 workflows have lock files (100%), compile-validate clean.

## cloud-hypervisor EACCES P0 chain — remains resolved
No new evidence contradicting the 2026-09-21 fix (PR #62406). No action needed this run.

## Tooling limitation (carried over, unresolved)
`workflow-health-manager.md`'s `update-issue` safe-output still lacks `target: '*'`, so this
schedule-triggered workflow cannot update/close existing issues directly on scheduled runs.
Recommendation unchanged: add `update-issue: target: '*'` to enable true issue-refresh/close
behavior.

## Actions Taken This Run (2026-09-23)
- Verified PR github/gh-aw#62670's Metrics Collector gate is functioning correctly (caught the
  exact staleness it was built to catch on its first scheduled run).
- Root-caused and filed one new maintenance issue for `daily-fact`'s 15/15 mempalace
  startup-race failures (previously an unattributed residual watch item).
- Re-verified all 4 `failing-workflows.json` entries live via `gh run list`/job logs; 3 of 4 fully
  recovered, 1 (`cjs`) confirmed out of scope.
- No dashboard issue created — material delta this run (one new root-caused issue) did not meet
  the bar for a full dashboard refresh; #62310/#62311 remain correctly closed, #62731 correctly
  left open.

> Last updated: 2026-09-23T04:37Z
