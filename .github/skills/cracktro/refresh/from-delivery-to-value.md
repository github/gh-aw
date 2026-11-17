---
name: cracktro-refresh-from-delivery-to-value
description: Recompute the measured data on the cracktro "From Delivery to Value" slide and update deck.js.
---

# Refresh "From Delivery to Value"

Load `.github/skills/cracktro/SKILL.md` first. Never read `docs/public/cracktro/deck.js` whole; `grep -n`, then `view_range`. The same helper output feeds Deliver · Accept · Attribute (`refresh/deliver-accept-attribute.md`); refresh both together.

## Workflows mined

| Workflow file | Repo | Role in the slide's flow | Signal mined |
|---|---|---|---|
| `.github/workflows/schema-feature-coverage.md` | github/gh-aw | every lane: outcome line, opened and merged pull requests, windows | its `[schema-coverage]` pull requests whose body carries the producer footer `workflow_id: schema-feature-coverage`; its own textual check replayed on first-parent main |

## 1. Slide

- Marker: `// From Delivery to Value:`. CSS prefix `dtv-`. Group `platform`, `under: "value"`: the full record, after Deliver · Accept · Attribute in the column under Turn Automation into Value.
- Data lives inside that block only:
  1. `COVER` (`[covered, total]` every Sunday 12:00 UTC from 22 Mar 2026) and `LAST` (final cutoff date).
  2. `OUT` (`[delivered, merged or null]`, UTC minutes) and `WIN` (inspected windows: `[label, merged PRs, first merge, last merge, gaps closed, fields retired, before, after-window end, 7 + 7 samples]`).
  3. `EXCERPT`: the label of the one window that passes every gate, outlined as Deliver · Accept · Attribute's excerpt; tile 4 shows its gaps.
  4. Tiles, per-day counts, the start and end labels and the `+N` labels are computed. The kicker (`the full record, Mar – Nov 2026`), the `.dtv-cap` caption and the `notes:` string (cohort, gap totals, uninspected merges, every date) are text.
- Elsewhere: the `content-rules.md` Exception bullet "the From Delivery to Value slide (`dtv-` classes)".

## 2. Window & snapshot

- Weekly: every Sunday 12:00 UTC from 22 Mar 2026 (fixed start) to END, plus END (baseline END 17 Nov 2026, 36 snapshots). The replay date is the snapshot.
- Windows: same-day merge batches up to EARLY_END (baseline 30 Jun 2026) plus the named witnesses (baseline 63949, 72028, 74061): `[first merge − 7 d, first merge)` and `[last merge, last merge + 7 d)`, daily, no lag.

## 3. Sources

- Outcome (all authors): keys of `.properties` in `pkg/parser/schemas/main_workflow_schema.json`; a field is covered when a `.md` file under `.github/workflows` or `schema-demos` (any depth) has a line starting `<field>:` (the workflow's check; it also matches code blocks). Commit = `git rev-list --first-parent -1 --before=<cutoff> origin/main`.
- Delivery: GitHub search `repo:github/gh-aw is:pr author:app/github-actions "workflow_id: schema-feature-coverage" in:body`, footer and bot author checked. Search is not native lifecycle history.
- Adoption proxy: `pull_request.merged_at`. Native record: `gh aw audit RUN --adoption --json` (main build) gives `explicit_human` for run 36389625388 (the 28 Sep pull request); runs 40283474974 and 41401517986 fail with "lifecycle session is missing its format or execution identity".
- Merge witness: each merge commit against its actual first parent (`sha^1`); a batch uses the first member's parent and the last member's merge.
- Survival (single merges with a complete window): first-parent main commits in `(merge, merge + 7 d]` that touch any file the merge changed (helper `survival`); attribution needs 0. See `refresh/deliver-accept-attribute.md`.

## 4. Queries

Read-only, about 20 s (git plus about 20 gh API calls):

```bash
git fetch origin main
node .github/skills/cracktro/refresh/from-delivery-to-value.mjs 2026-11-17 origin/main 63949,72028,74061 2026-06-30 > "$TMPDIR/dtv.json"
jq -c '.window, .first, .last, .cohort, {delivered: .outputs.delivered, merged: .outputs.merged}' "$TMPDIR/dtv.json"
jq -c '.windows[] | {label, prs: (.prs | length), gaps, removed, before, end}' "$TMPDIR/dtv.json"
jq -c '.merges[] | select(.inspected | not) | {pr, merged, before, after, closed}' "$TMPDIR/dtv.json"
# deck literals
jq -c '.weekly' "$TMPDIR/dtv.json"; jq -c '.outputs.items' "$TMPDIR/dtv.json"
jq -c '.windows[] | [.label, (.prs | length), .first[0:16], .last[0:16], .gaps, (.removed | length), .before, .end, .samples]' "$TMPDIR/dtv.json"
```

| Field | Baseline (17 Nov 2026) | Slide |
|---|---|---|
| `weekly` | 36 rows, `32/45` (71.1%) → `75/75` | `COVER`, tile 3, line |
| `cohort` | 39 fields: 30 → 39 covered | notes |
| `outputs` | 44 delivered, 19 merged; per day 10, 9, 10, 5, 1, 4, 1, 1, 1, 1, 1 opened | `OUT`, tiles 1–2, OPENED and MERGED lanes |
| `windows` | May 4 (2 PRs, +2), May 18 (8 PRs, +7, `dependencies` retired), Jun 15 (4 PRs, +4), Sep 28 (+5), Oct 26 (+1), Nov 2 (+1); 6 × 14 samples | `WIN`, WINDOWS lane |
| attributed | Sep 28 only, +5 fields | `EXCERPT`, tile 4, gold outline |
| uninspected merges | Aug 17 +1 (`enclaves`), Oct 19 +1 (`docker-image-pull-policy`) | notes |

The baseline reproduces the original value audit exactly (weekly values, all 84 samples, endpoints, members and the 44 outputs).

## 5. Update steps

1. Run the queries. Replace `COVER`, `OUT` and `WIN` with the printed literals (splice with a node script, never retype); set `LAST`.
2. Re-check attribution with `refresh/deliver-accept-attribute.md`. Point `EXCERPT` only at a window that passes every gate there; never attribute value to a batch, a window shared with another merge, or a merge without a native human adoption record.
3. Update the kicker dates, the caption, the notes (cohort, gap totals, uninspected merges) and the Exception bullet.
4. Add a witness only when its merge keeps the schema population (`before.total == after.total`) and closes gaps. Each window adds a 16 px row to the WINDOWS lane; check it stays above OPENED.

## 6. Validation

```bash
node --check docs/public/cracktro/deck.js
python3 -m http.server 4329 --bind 127.0.0.1 --directory docs/public   # async
# browser canvas: http://127.0.0.1:4329/cracktro/#N (N = this slide's row in slides.md), press → to finish, screenshot .stage
cd docs && npx playwright test tests/talk.spec.ts                       # if node_modules exists
```

- Check the four tiles and the caption each stay on one line, per-day counts do not collide, and the gold outline wraps the excerpt window. No PR numbers, logins or URLs on the slide or in notes.

## 7. Pitfalls

- The helper throws when search results are incomplete, a footer or bot author is missing, or a witness changes the schema population. Fix the input; never relax the check.
- Six windows are an inspected subset: two more merges closed one gap each without a window. Do not call the windows complete.
- A merge is an adoption proxy; only one native explicit-human record exists. A per-day count is separate pull requests from one run, not runs or units of value.
- Coverage dips when the schema gains fields (April, August): denominator growth. The cohort line separates churn from real gains.
- The check is textual presence, not compilation, correctness, fewer defects or saved hours.
- Samples after the run time are `null` (pending); a pending window must stay visibly pending.
