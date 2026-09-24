# Workflow Health — 2026-09-24T04:37Z

## NEW: 3 root-caused failures filed as one consolidated maintenance issue
Filed a single P1 issue (this run) consolidating 3 distinct, concrete config-level root causes
found via live job-log inspection (not just failing-workflows.json triage):

1. **avenger.md — `/usr/local/bin/npm` symlink bind-mount crash (P1).** `awf` sandbox rejects
   `/usr/local/bin/npm` as a bind mount because it's a symlink on the current runner image
   (`-> ../lib/node_modules/npm/bin/npm-cli.js`, confirmed live). 4 consecutive fresh failures
   (#63036, #63040, #63049, #63065), all self-expiring (`expires: 1d`) with no merged fix. Same
   defect class as the already-fixed `/usr/bin/go` symlink mount (#56398, merged 2026-08-27).
   **Two prior fix PRs (#57946, #58722) were closed unmerged on 2026-09-05** — the diff from
   #57946 (drop the npm mount line, keep node/node_modules/go mounts) is still directly
   applicable. Not yet applied — flagged with exact fix in the new issue.
2. **metrics-collector.md — missing `model-provider: github` (P1).** Root-caused the
   `model_not_supported_error` recurring since 2026-09-17 (latest #63076, 2026-09-24T02:44Z) —
   this is distinct from the already-fixed silent-success masking (#62670/#62731). Every other
   workflow pairing `engine.id: codex` with `model: copilot/gpt-5.3-codex` (daily-go-test-
   parallelizer, api-consumption-report, copilot-centralization-drilldown, github-remote-mcp-
   auth-test, etc.) explicitly sets `model-provider: github`; metrics-collector.md is the outlier
   missing that one line. Confirmed by config diff across all `copilot/gpt-5.3-codex` workflows.
3. **gpclean.md — hardcoded retired model `gpt-5-codex` (P2).** `Model 'gpt-5-codex' is retired
   or unsupported. Did you mean 'gpt-5.3-codex'?` — 3 of last 4 scheduled runs failed
   (#63083, #62857, #62533). Same defect class resolved repo-wide by #46412 but apparently missed
   for this file. Fix: `openai/gpt-5-codex` → `openai/gpt-5.3-codex`.

## failing-workflows.json triage (4 entries)
- **daily-firewall-report**: still intermittently failing — latest run (2026-09-24T02:29Z)
  failed with `copilot` engine terminating unexpectedly after producing a valid discussion
  (chart generation sub-agent model-access error noted inline); prior day succeeded. Tracked via
  auto-filed #63079 (not a new root cause this run, chart sub-agent model access needs separate
  follow-up if it persists).
- **daily-go-test-parallelizer**: fully healthy (5/5 latest scheduled+manual runs successful).
- **lint-monster**: fully healthy (3/3 most recent runs successful).
- **cjs**: plain GH Actions CI workflow, out of `gh aw` scope (approval-gate `action_required`,
  not a failure) — unchanged from last run's assessment.

## Compilation Status
298/298 workflows have lock files (100%), compile-validate clean.

## Carried-over watch items (no new evidence, unchanged)
- **daily-fact** mempalace MCP startup-race (P2, #62868, still open) — no new evidence this run.
- **Daily Observability Report for AWF Firewall/MCP Gateway** (#63051) — long-running (since
  ~2026-07) intermittent-failure pattern (model errors, jq schema-mismatch parsing bugs in its
  own report-generation logic). Not re-diagnosed this run (out of scope vs. the 3 newly
  root-caused items); flagged as a candidate for a dedicated deep-dive if it persists.
- `metrics/latest.json` still stale at 2026-09-01 — unblocked by fixing metrics-collector's
  `model-provider` gap (item 2 above); once that lands, the content-gate from #62670 should let
  fresh collection through on the next scheduled run.

## Tooling limitation (carried over, unresolved)
`workflow-health-manager.md`'s `update-issue` safe-output still lacks `target: '*'`, so this
schedule-triggered workflow cannot update/close existing issues directly. Recommendation
unchanged: add `update-issue: target: '*'` to enable true issue-refresh/close behavior.

## Actions Taken This Run (2026-09-24)
- Live-inspected job logs (not just metrics snapshot) for avenger, metrics-collector, gpclean,
  daily-firewall-report failures — found 3 concrete, previously-mis-set-aside root causes.
- Filed one consolidated P1 maintenance issue with exact per-file diffs for all 3.
- Confirmed avenger's two prior fix PRs (#57946, #58722) were closed unmerged 2026-09-05 —
  flagged so the fix isn't lost a third time.
- Re-verified failing-workflows.json's 4 entries live; only daily-firewall-report remains
  partially affected (intermittent, already tracked via auto-expiring issue).
- No dashboard issue created — material delta (one new consolidated root-cause issue) captured
  via the maintenance issue itself; no compilation/health-category shifts warranting a full
  dashboard refresh.

> Last updated: 2026-09-24T04:37Z
