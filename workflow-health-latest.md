# Workflow Health — 2026-09-19T04:34Z

## P0 (still active, worse): cloud-hypervisor EACCES sandbox regression
Predecessor #61528 auto-expired 2026-09-18T04:46Z (`expires: 1d`) without a real fix — the incident
continued and **grew from 30 → 44 open occurrences** across 40+ distinct workflows by this run.
Confirmed via `gh api search/issues` (search: `EACCES state:open`) and direct issue-body reads of
several recent occurrences (#61933 LintMonster, #61944 jsweep, #61890 checked and ruled a different
issue — model config, not EACCES). AWF was bumped to v0.28.20 via PR #61527 (merged 2026-09-17)
but EACCES failures continued after that merge (e.g. #61944 at 2026-09-19T03:48Z), so this is not
a stale-pin problem — the trusted-artifact permission/staging step itself is broken.
**Action:** Filed new consolidated P0 tracker this run (did not attempt update-issue on the expired
#61528 since it's closed and update-issue targeting on schedule-triggered runs has no default
issue context — same limitation noted in the 2026-09-16 finding). Body lists all 44 open occurrence
numbers to prevent re-filing duplicates.

## codex gpt-5.3-codex model_not_supported_error — appears RESOLVED
0 open issues match `model_not_supported_error` this run (search:
`model_not_supported_error in:body state:open`), down from the long-running P0 chain
(#60563→#61030, 6 consecutive re-discoveries). Not re-escalating; will re-check next run.

## failing-workflows.json triage (all 4 entries)
- **lint-monster**: latest failure (#61933) is the EACCES hypervisor crash, not workflow-specific.
- **daily-firewall-report**: latest failure (#61925) is the EACCES hypervisor crash, not
  workflow-specific.
- **daily-go-test-parallelizer**: healthy (9/10 recent runs successful).
- **cjs**: plain GH Actions workflow, out of `gh aw` scope — 9/10 recent runs successful.

## Compilation Status
299/299 workflows have lock files (100%), compile-validate clean.

## Metrics staleness
`metrics/latest.json` still dated 2026-09-01 — 18 days stale, 7th consecutive affected run.
Metrics Collector itself is among the 44 EACCES-affected workflows — likely root cause of
staleness; expect self-resolution once the EACCES P0 is fixed.

## Actions Taken This Run (2026-09-19)
- Created new consolidated P0 issue for the cloud-hypervisor EACCES regression (44 open dupes
  cited, DO NOT RE-FILE).
- Created Workflow Health Dashboard issue - 2026-09-19 with full findings.
- No update-issue calls this run (predecessor already closed; no addressable open tracker existed).

> Last updated: 2026-09-19T04:34Z
