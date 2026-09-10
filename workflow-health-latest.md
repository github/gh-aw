# Workflow Health — 2026-09-10T04:41Z

Score: stable (no material delta) | Run: §34437675576

## Compilation Status
- **299/299 workflows have lock files (100% ✅)**. compile-validate clean, no errors/warnings.

## Follow-up on failing-workflows.json (4 flagged, metrics snapshot dated 2026-09-01, now stale)
- **lint-monster**: still failing intermittently — latest run (09-10 02:37) succeeded, but 3
  consecutive prior daily runs (09-06 to 09-08) failed with `model_not_supported_error` +
  `missing_safe_outputs` (invalid/unsupported `model: openai/gpt-5.3-codex` for engine `codex`,
  per issue #59853 body). Previously-tracked issue **#59609 auto-expired/closed 2026-09-09**;
  a **new** auto-filed issue **#59853** "[aw] LintMonster produced no safe outputs" (open, created
  2026-09-10T02:45Z) already covers this occurrence. DO NOT RE-FILE — but note the root cause
  (model name/policy mismatch) is unresolved and will keep recurring until the workflow's `model:`
  or engine model-provider config is fixed. Backlog issue #58126 (function-length refactoring)
  remains open and unrelated to this failure mode.
- **daily-go-test-parallelizer**: healthy — 9 of last 10 runs successful, 1 failure (09-09 10:31),
  1 in-progress. Auto-filed issue **#59879** "[WIP] Daily Go Test Parallelizer: work in progress"
  opened 2026-09-10T04:35Z tracks the currently in-progress run; issues **#59847** and **#59790**
  (both open, "produced no safe outputs") track the isolated failures. Previously-cited #59631 is
  now CLOSED (auto-expired 2026-09-09T16:43Z). DO NOT RE-FILE.
- **daily-firewall-report**: healthy — last 2 runs (09-09, 09-10) both succeeded after 2 prior
  failures (09-07, 09-08). No open issue currently needed; monitor for recurrence.
- **cjs**: this is a plain GitHub Actions workflow (`.github/workflows/cjs.yml`, not an agentic
  `.md` workflow) so it is out of `workflow-list.txt` scope and not compiled by `gh aw`. Recent
  history (last 6 runs) shows 5/6 success — the `failing-workflows.json` snapshot's "4
  action_required" figure is from the stale 2026-09-01 collection window and does not reflect
  current state. No action needed.

## Data-quality note
`failing-workflows.json` / `metrics/latest.json` is dated 2026-09-01 and self-reports a "GitHub
API fallback after paginated logs were truncated" collection method (see Agent Performance
Analyzer's 2026-09-09 caveat in shared-alerts.md) — over a week stale relative to this run
(2026-09-10). All four flagged workflows were independently re-verified against live `gh run list`
data in this run rather than trusted at face value.

## Actions Taken This Run
- No new issues created — all findings already tracked (open: #59853, #59879, #59847, #59790,
  #58126) or self-resolved (cjs, daily-firewall-report, daily-go-test-parallelizer largely healthy).
- Verified: no missing lock files, no compile errors, 299/299 workflows compiled.
- Confirmed lint-monster's underlying root cause (unsupported model config) is still unresolved
  despite issue churn/auto-expiry — flagging for future runs to watch for a fix PR rather than
  re-filing.

> Last updated: 2026-09-10T04:41Z

---

# Workflow Health — 2026-09-09T04:37Z

Score: stable (no material delta) | Run: §34311466837

## Compilation Status
- **299/299 workflows have lock files (100% ✅)**. compile-validate clean, no errors/warnings.

## Follow-up on 2026-09-01 unverified single-run flags
Checked recent run history (last 5 runs each) for the three workflows flagged as
"unverified pattern, single-run sample" in the 2026-09-08 shared-alerts correction:

- **lint-monster**: 4/5 recent scheduled runs failed (09-06 through 09-09), 1 success (09-05).
  Already tracked by open issue **#59609** "[aw] LintMonster failed" (and long-running backlog
  issue #58126). DO NOT RE-FILE.
- **daily-go-test-parallelizer**: recovered — last 4 completed runs successful, 1 in progress.
  Already tracked by open WIP issue **#59631** "Daily Go Test Parallelizer: work in progress"
  (superseding closed #58772/#58808/#58821 etc). DO NOT RE-FILE.
- **daily-firewall-report**: recovered — most recent run (09-09) succeeded after 2 failures
  (09-07, 09-08). Recurring pattern historically tracked via numerous closed issues
  (#59348, #59107, #57586, #53023, etc.) and cache-strategy fix #57219. No currently-open issue;
  today's success suggests transient. Continue monitoring — file only if failures resume for 3+
  consecutive days.

## Actions Taken This Run
- No new issues created — all findings already tracked or resolved.
- Verified: no missing lock files, no compile errors.
- Updated shared-alerts.md to close out the "unverified" flags from 2026-09-08.

> Last updated: 2026-09-09T04:37Z
