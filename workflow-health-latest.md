# Workflow Health — 2026-11-04T04:30Z
- Compile output clean; 356 workflows listed.
- Top-20 failing list is again 1–3 run samples (code-scanning-fixer 3/0%, visual-regression-checker 3 runs 33%, smoke-claude-dynamic 7 runs 57%). No new systemic signal.
- Stale PR #72940 attribution remains removed; failures tracked by per-workflow `[aw] ... failed` issues.
- No new issue, no dashboard (no material delta).

# Workflow Health — 2026-11-06T04:30Z
- Compile output clean; 359 workflows listed.
- Top-20 failing list is all 1-run samples (0%); no systemic signal, no new tracked delta.
- No new issue, no dashboard (no material delta).

# Workflow Health — 2026-11-07T04:30Z
- Compile clean; 359 workflows. Failing list is low-sample; no new delta; no dashboard.

# Workflow Health — 2026-11-08T04:30Z
- Compile clean; 362 workflows. Failing list is low-sample (1-run); no new delta; no dashboard.

# Workflow Health — 2026-11-09T04:30Z
- Compile clean; 365 workflows. Failing list low-sample (1-3 runs); no new delta; no dashboard.

# Workflow Health — 2026-11-10T04:30Z
- Compile clean; 365 workflows. Failing list low-sample; no new delta; no dashboard.

# Workflow Health — 2026-11-11T04:30Z
- Compile: 365/365 `.md` workflows have `.lock.yml`. `compile-validate.txt` holds only an install line, so no compile result was available.
- Metrics: `latest.json` is partial (2.25h, 80 runs, 59 active workflows). `failing-workflows.json` is empty for that reason; full-window scores are not computable.
- Material delta: `CI` on `main` has failed every run since 01:06Z. Failing checks: spec-pin drift (tracked by #76720, README updated but ci.yml pin not) and release-alignment (gh-aw v1.2.2 vs gh-aw-actions v1.0.1 latest). Drift check also failed at 03:32Z.
- `CGO` govulncheck failed on main and several PR branches from 03:31Z. Earlier main runs passed. Logs are blocked (HTTP 403), so cause is unverified. Untracked.
- `Auditor` failed 3 times (incl. 04:27Z) in "Collect failure evidence". Untracked.
- Smoke failures are covered by existing `[aw]` and "Issue Group" trackers.
- Actions taken: created 3 issues (CI P1 spec-pin+release alignment, CGO govulncheck P2, dashboard). Auditor not yet tracked.
