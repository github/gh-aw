---
name: cracktro-refresh-spec-model-test
description: Recompute the measured data on the cracktro "Spec · Model · Test" slide and update deck.js.
---

# Refresh: Spec · Model · Test

Plan only. Recompute, then edit `docs/public/cracktro/deck.js` per the `cracktro` skill (never read deck.js whole; grep + `view_range`).

## Workflows mined

| Workflow file | Repo | Role in the slide's flow | Signal mined |
|---|---|---|---|
| `.github/workflows/daily-work-queue-formal-verification.md` | gh-aw | Model checker: daily TLC matrix (FairDAGGitHub, QueueOrdering) | run conclusions and `work-queue-formal-*/result.json` artifacts |

## 1. Slide

- Heading: `Spec · Model · Test` (AW factory band, `group: "data"`).
- Marker: `// Spec → Model → Test: spec requirements become TLA+ invariants`.
- CSS prefixes: `fv-` (flow, cards), `smt-` (measured ledger).
- Data strings, all inside the slide block:
  - Ledger: `smt-h` `"MEASURED · 2 Oct – 14 Nov 2026 (43 days)"`; `smt-k` array `[["16","TLA+ modules"],["218","TLC configs"],["99","mutants"],["55","witnesses"]]`; `smt-run` lines `"11 Nov · QueueOrdering open: 178.9M states, 4 h 40 m"` and `"2 Oct – 14 Nov · 35 merged PRs changed the spec"`; `smt-cap` `"specs/work-queue · TLC 7 Oct, 11 Nov · flow illustrative"`.
  - Cards: `"✓ Safety · 1,055,182 states"`, `"11 Nov · depth 20 · 16 min 33 s"`, `"735 states · 7 Oct record"`.
  - `tick()`: `1055182` (phase 1 ramp, depth cap `20`), `735` (mutant), `"756 distinct states · 1 trace"` (witness).
  - `notes:` sentence starting `Measured, data window …`.

## 2. Window & snapshot

- Current: window 2 Oct – 14 Nov 2026 (43 days), snapshot 14 Nov 2026.
- Fixed start (2 Oct 2026 = first commit of the standardised `specs/work-queue`); roll the end to the snapshot date and recompute the day count.
- Spec file counts: snapshot at HEAD (all-time).
- Daily TLC: latest **successful** run date (currently 11 Nov 2026).
- Mutant/witness: newest `specs/work-queue/verification-*.json` record date (currently 7 Oct 2026).

## 3. Sources

- Repo `github/gh-aw`, folder `specs/work-queue` (`*.tla`, `*.cfg`, `verification-*.json`, `README.md`).
- Workflow `.github/workflows/daily-work-queue-formal-verification.md` (daily matrix FairDAGGitHub + QueueOrdering; artifacts only, no issues).
- Artifacts `work-queue-formal-<Config>-<run>-<attempt>/result.json`.

## 4. Queries

One-shot (read-only; prints JSON): `bash .github/skills/cracktro/refresh/spec-model-test.sh [SINCE]` (default `2026-10-02`).

Manual equivalents, from `specs/work-queue`:

```bash
ls *.tla | wc -l; ls *.cfg | wc -l; ls Broken*.cfg | wc -l; ls *Witness*.cfg | wc -l   # 16 218 99 55
git log --since=2026-10-02 --format=%s -- . | grep -oE '\(#[0-9]+\)$' | sort -u | wc -l  # 35
gh api --paginate "repos/github/gh-aw/actions/workflows/daily-work-queue-formal-verification.lock.yml/runs?per_page=100" \
  --jq '.workflow_runs[] | [.id,.conclusion,.created_at] | @tsv'                         # 43 runs: 15 success, 28 failure
gh run download <last-success-id> -R github/gh-aw -D "$(mktemp -d)" -p 'work-queue-formal-*'
jq '{config,status,exhausted,elapsed_seconds,depth,final_counts,latest_progress}' */result.json
jq '.results[] | select(.config=="BrokenCAS" or .config=="WeakOrderingWitness")' verification-*.json
```

Output shapes: `result.json.final_counts` = `{states_generated, distinct_states, states_remaining}` (strings; `null` when timed out, then use `latest_progress`). Record `final_counts` = `[generated, distinct, queue]`.

Baseline (14 Nov 2026): FairDAGGitHub passed, exhausted, 1,055,182 distinct, depth 20, 994 s; QueueOrdering timed_out, 178,912,211 distinct at depth 20, 16,803 s; BrokenCAS → TerminalPersistence 735; WeakOrderingWitness → NoOutOfOrderClaim 756.

## 5. Update steps

1. Ledger: header window `<start> – <end> <year> (<N> days)`, the four `smt-k` counts, both `smt-run` lines (QueueOrdering date/state count/duration; PR window/count). If QueueOrdering passes, change icon `◔`/class `o` to `✓`/`g` and say "exhausted".
2. Cards: Safety state count, date, depth, `elapsed_seconds` as `M min S s`; mutant count and record date.
3. `tick()`: replace `1055182`, depth cap, `735`, `756`.
4. Caption `smt-cap`: record date and daily run date.
5. Notes: update every number and date; keep the "Illustrative:" sentence (must match `/illustrative|synthetic|simulated/i`).
6. `.github/skills/cracktro/content-rules.md`: update the Spec · Model · Test "Exception:" bullet numbers and window.
7. Keep invariant/mutant names in sync with `WorkQueue.tla` if renamed.

## 6. Validation

- `node --check docs/public/cracktro/deck.js`.
- Serve `docs/public` (`python3 -m http.server <port> --bind 127.0.0.1 --directory docs/public`), open `/cracktro/#spec-model-test` in the browser canvas, screenshot during all three phases. Check that no ledger line wraps (each `.smt-run`/`.smt-cap` `scrollWidth <= clientWidth`) and that card subtitles stay inside their cards.
- Content rules: no 40-hex SHAs (artifacts contain `sha`; never copy it), no URLs, no `github/<repo>` strings, no people, agent/role names only.
- `cd docs && npx playwright test tests/talk.spec.ts`.

## 7. Pitfalls

- Mutant/witness globs overlap other names: count with `Broken*.cfg` and `*Witness*.cfg` exactly; never `grep -i broken`.
- PR count relies on squash-merge subjects ending `(#N)`; merge commits or PRs without suffixes are missed. Dedupe numbers.
- Runs API pages are capped at 100; always use `--paginate`. Count only `success`/`failure` conclusions; `cancelled`/in-progress runs are excluded from both.
- A successful workflow run can still have QueueOrdering `timed_out`: report it as "open", never as a pass.
- Artifacts expire (retention); if the last success has none, use the newest success that does and update the run date.
- Rate limits: one paginated runs call plus one download is enough.
- Large-state numbers: round QueueOrdering to one decimal in millions (`178.9M`); keep exact counts elsewhere with `en-US` separators.
