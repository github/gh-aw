# Workflow Health — 2026-09-16T04:37Z

Score: P0 unresolved (5th re-discovery cycle) | Run: workflow-run 35056132501

## P0 — gpt-5.3-codex model_not_supported_error, still unresolved
Tracker #61030 (open, filed 2026-09-15) was due to auto-expire today via its own `expires: 1d`
marker. Added a refresh comment with fresh evidence instead of letting it lapse:
Daily Go Test Parallelizer #61269 (2026-09-16T04:36Z), LintMonster #61250 (2026-09-16T02:44Z) —
same "Invalid or Unsupported Model" signature. `CodexDefaultModel = "gpt-5.4"` unchanged in
`pkg/constants/engine_constants.go`; 74 workflow files still hardcode `gpt-5.3-codex`. No fix
merged since #60423 (prefix-strip only, insufficient). **DO NOT** file new per-workflow issues
for this signature — track via #61030.

## NEW: Root cause of tracker self-expiry loop diagnosed
Attempted `update_issue` on #61030 to refresh its expiry — **failed**: `workflow-health-manager.md`
configures `update-issue` with default `target: triggering`, which only works when an
issue/PR triggered the run. On `schedule` triggers there is no triggering issue, so
`update_issue` with an explicit `issue_number` is rejected outright ("update_issue requires an
issue context... running on a schedule event"). This is the concrete mechanism behind the
repeated re-discovery cycles (#60416 → #60563 → #61030). **Fix needed:** set
`update-issue: target: '*'` in `.github/workflows/workflow-health-manager.md`, or exempt
`priority-p0` issues from `expires` entirely. Documented in dashboard issue this run.

## Compilation Status
299/299 workflows have lock files (100%), compile-validate clean.

## Actions Taken This Run (2026-09-16)
- Added evidence-refresh comment to existing tracker #61030 (no new per-workflow issue).
- Created "Workflow Health Dashboard - 2026-09-16" issue documenting the update-issue
  target-scoping defect as the true root cause of the P0's repeated re-discovery.
- No new issues filed for #61269/#61250 — both are known instances of tracked root cause.

> Last updated: 2026-09-16T04:37Z
