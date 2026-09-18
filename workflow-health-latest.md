# Workflow Health — 2026-09-18T04:36Z

## P0 (mutated, still active): cloud-hypervisor sandbox — signature changed to EACCES
The 2026-09-15→17 incident (tracked in #61528, dashboard #61529) did **not** resolve — its failure
mode changed from `exited with code undefined` to `EACCES` (permission denied) executing the same
`/run/awf-cloud-hypervisor/trusted-artifacts/.../cloud-hypervisor --version` binary. First EACCES
occurrence 2026-09-17T14:59Z (#61627), 30 open issues match this new signature as of this run.
Confirmed directly from job logs of **LintMonster** (run 35300195220) and **Daily Firewall Logs
Collector and Reporter** (run 35299593898) — both in this run's `failing-workflows.json`.
**Action:** added evidence comment to #61528 (no new issue filed — same root-cause family, just a
different crash mode). Added a summary comment to dashboard #61529 (update_issue not permitted for
`schedule`-triggered workflow — used add_comment instead).

## failing-workflows.json triage (all 4 entries)
- **lint-monster**: latest failure is the EACCES hypervisor crash, not workflow-specific.
- **daily-firewall-report**: latest failure is the EACCES hypervisor crash, not workflow-specific.
- **daily-go-test-parallelizer**: healthy (9/9 recent completed runs successful).
- **cjs**: plain GH Actions workflow, out of `gh aw` scope — no action (unchanged from prior runs).

## Codex gpt-5.3-codex model_not_supported_error
No change this run — still deferred per prior guidance; avoid re-triage until the hypervisor
incident (now EACCES) is fully resolved to prevent misattribution.

## Compilation Status
299/299 workflows have lock files (100%), compile-validate clean.

## Actions Taken This Run (2026-09-18)
- Added evidence comment to existing P0 tracker #61528 documenting the EACCES root-cause mutation.
- Added summary comment to dashboard issue #61529 (could not use update_issue: schedule-triggered
  workflow lacks issue context for that tool; used add_comment as a fallback).
- No new issues created — same incident family, updated in place.

> Last updated: 2026-09-18T04:36Z
