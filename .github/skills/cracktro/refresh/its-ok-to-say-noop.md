---
name: cracktro-refresh-its-ok-to-say-noop
description: Recompute the measured data on the cracktro "It’s ok to say no(op)" slide and update deck.js.
---

# Refresh: It’s ok to say no(op)

Recompute with `its-ok-to-say-noop.sh` (wraps `gh aw audit`), then patch `deck.js`. Read-only against GitHub; edit only the files listed in step 5.

## Workflows mined

| Workflow file | Repo | Role in the slide's flow | Signal mined |
|---|---|---|---|
| every `.github/workflows/*.lock.yml` with a completed (success or failure) run in the window | gh-aw | one run each in the denominator; a count on a "WHERE IT LANDS" row when it emitted that type | `noops`, `missing_tools`, `missing_data`, `report_incomplete` arrays and `metrics.turns` of `gh aw audit --json --artifacts agent` |

## 1. Slide

- Heading: **It’s ok to say no(op)** (`group: "engineering"`, after Don’t Run the Agent, before Fuzzy schedules). Id `its-ok-to-noop`.
- Marker: `grep -n "// It’s ok to say no(op):" docs/public/cracktro/deck.js`, then `view_range`. Never read deck.js whole.
- CSS prefix `nop-`. The four run scenarios (`RUNS`) are illustrative; only the side rows and caption are measured.
- Data lives in the slide block:
  - `const ROWS = [[type, where it lands, runs]]`: `runs` is `runs_with.<type>` (rendered right-aligned as `.nop-row em`).
  - `const M = { agent, start, end, snap }`: `agent` is `agent_runs`; dates feed the caption.
  - Caption (`.nop-cap`): `` `MEASURED · gh aw audit · ${M.agent} gh-aw agent runs ${M.start}–${M.end} · snapshot ${M.snap} · runs per type · scenarios illustrative` ``.
  - `notes:` sentence "Measured on gh-aw itself with gh aw audit: …" with the same numbers.

## 2. Window & snapshot

- Seven complete UTC days ending the day before the snapshot (run `created` date). Snapshot = the day you run the script (`snapshot` field, UTC).
- Roll forward by passing new `START END`. Each audit JSON is saved as `$OUT/json/<id>.json`; reruns with the same `OUT` skip saved runs.

## 3. Sources

- Repository: gh-aw (public), GitHub Actions runs API (listing) and run artifacts via `gh aw audit` (agent artifact only).
- Audit fields: `pkg/cli/audit_report_types.go` (`AuditData`: `noops`, `missing_tools`, `missing_data`, `incomplete` for `report_incomplete`); extractors in `pkg/cli/logs_metrics.go`.
- Where each type lands (side-row text): `docs/src/content/docs/reference/safe-outputs.md` (noop, missing tool/data, report_incomplete sections).

## 4. Queries

```bash
OUT="$(mktemp -d "$TMPDIR/nop-audit.XXXXXX")"
GH_AW="gh aw" OUT="$OUT" .github/skills/cracktro/refresh/its-ok-to-say-noop.sh 2026-11-15 2026-11-21 12 > "$TMPDIR/nop.json"
```

- `OUT`: audit cache. Unset, the helper creates a fresh private `mktemp -d` dir (printed on stderr), so pass `OUT` to reuse audits across reruns. A supplied `OUT` must be a directory you own (not a symlink); it is set to mode 700.
- Failed audits: stderr stays in `$OUT/json/<id>.err`, IDs in `$OUT/missing.txt`, and the helper exits non-zero without output. Rerun with the same `OUT` to retry only those. `ALLOW_PARTIAL=1` prints anyway with `missing` > 0; never publish such a result.

- `GH_AW`: audit command (`gh aw`, or a local build: `go build -o "$TMPDIR/gh-aw-bin" ./cmd/gh-aw`; `$TMPDIR/gh-aw` may already be a directory). Needs `gh` auth, `jq`, `xargs`. `SAMPLE` (default 1000) audits evenly spaced runs by run ID; about 200 audits/min at 16 jobs. A week is ~7,000 runs.
- Listing is per workflow: the repo-wide runs list caps filtered results at 1,000, under one day here.
- `agent_runs` counts audited runs with `metrics.turns > 0`; gated runs (agent job skipped) have no turns and drop out. Counts use only the audit JSON; never download or parse artifacts or logs yourself.

Output shape:

```json
{ "window": {"start","end"}, "snapshot", "runs", "sampled", "audited", "missing", "agent_runs",
  "runs_with": {"noop","missing_tool","missing_data","report_incomplete"},
  "items": {…same keys…}, "quiet", "top_noop": [{"w","n"}] }
```

Sanity baseline for 2026-11-15..2026-11-21 (snapshot 2026-11-23):

| Item | Value |
|---|---|
| runs listed / audited / agent runs | 6,781 / 1,747 / 712 |
| runs with noop | 168 |
| runs with missing_tool / missing_data | 37 / 4 |
| runs with report_incomplete | 266 (about 110 system-added: `engine_driver_failure`, `missing_terminal_safe_output`, `agent_startup_failure`) |
| quiet runs (noop only) | 166 |

The published baseline audited the first 1,747 listed runs (109 workflows) before the run was stopped; the helper now picks `SAMPLE` evenly spaced runs instead. About 1,000 runs is enough.

## 5. Update steps

1. **Data:** set `M` and the `runs` column of `ROWS` from `agent_runs` and `runs_with`. Format with `toLocaleString("en-US")`-style commas in the source strings.
2. **Caption and notes:** update the window, snapshot and numbers. Keep "scenarios illustrative" in the caption and "illustrative" in the notes so they still match `/illustrative|synthetic|simulated/i`.
3. **`.github/skills/cracktro/content-rules.md`:** update the numbers and dates in the "It’s ok to say no(op) slide" Exception bullet. Do not change other bullets.

## 6. Validation

```bash
node --check docs/public/cracktro/deck.js
python3 -m http.server 4329 --bind 127.0.0.1 --directory docs/public   # async; open http://127.0.0.1:4329/cracktro/#its-ok-to-noop
git diff -U0 docs/public/cracktro/deck.js | grep '^+' | grep -nE '[0-9a-f]{40}|(^|[^.[:alnum:]_])github/[a-z]|https?://|who:' | grep -v 'w3.org/2000/svg'   # must print nothing
cd docs && npx playwright test tests/talk.spec.ts
```

- Screenshot the stage after the last scenario (~25 s). Check the row counts do not collide with the row text and the caption clears the foot.
- No workflow names, run IDs, issue or PR numbers on the slide.
- Stop the server afterwards.

## 7. Pitfalls

- **Truncated listing:** never use the repo-wide `actions/runs?created=` list; it silently stops at 1,000.
- **Partial sample:** rate limits or transient errors fail single audits; the helper stops rather than shrink the denominator. Publish only with `missing: 0`.
- **Diff mode:** `gh aw audit` with several IDs diffs them against the first; the helper audits one run per call.
- **Audit version:** the `incomplete` field (`report_incomplete` signals) in the audit JSON needs a gh-aw build that includes it; older builds omit the field and report 0.
- **No `safe_output_summary`:** with `--artifacts agent` the audit JSON leaves it null; count from the per-type arrays instead.
- **Stale cache:** audit reuses `$OUT/run-<id>/run_summary.json`, and a local `dev` build never invalidates it. Use a fresh `OUT` after changing gh-aw.
- **Cache size:** each run caches its agent artifact; delete `$OUT` when done.
