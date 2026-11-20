---
name: cracktro-refresh-deliver-accept-attribute
description: Recompute the measured data on the cracktro "Deliver · Accept · Attribute" slide and update deck.js.
---

# Refresh "Deliver · Accept · Attribute"

Load `.github/skills/cracktro/SKILL.md` first. Never read `docs/public/cracktro/deck.js` whole; `grep -n`, then `view_range`. This slide is the excerpt of From Delivery to Value: run that plan's helper first (`refresh/from-delivery-to-value.md`, section 4) and reuse `$TMPDIR/dtv.json`.

## Workflows mined

| Workflow file | Repo | Role in the slide's flow | Signal mined |
|---|---|---|---|
| `.github/workflows/schema-feature-coverage.md` | github/gh-aw | the one pull request followed from delivery to attributed value | its 28 Sep 2026 pull request (producer run 36389625388): GitHub metadata, native adoption record, daily samples and full-window survival of its merge |

## 1. Slide

- Marker: `// Deliver · Accept · Attribute:`. CSS prefix `daa-` plus the shared `vg-` parts. Group `engineering`, `under: "value"`: the first example in the column under Turn Intent into Value.
- Data lives in `CASE` only: `title`, `labels`, `diff` (`[additions, deletions, files]`), `delivered` and `accepted` (UTC minutes), `parent` (`[covered, total]` at the exact first parent), `samples` (7 before, then day 0 = the merge commit to day 6), `survival` (`[first-parent commits, files in the merge, commits touching them]`) and `through` (window end). The timeline text, the squares, the excerpt pill and `+N` are computed.
- Also: the `.vg-cap` caption and the `notes:` string (every number and date) and the `content-rules.md` Exception bullet "the Deliver · Accept · Attribute slide (`daa-` classes)".

## 2. Window & snapshot

- Acceptance T is the merge time (baseline 28 Sep 2026 16:37 UTC). Before `[T − 7 d, T)`, after `[T, T + 7 d)`, sampled daily at T's time of day with no lag; survival over `(T, T + 7 d]` (baseline through 5 Oct 2026 16:37 UTC). The replay date is the snapshot (17 Nov 2026).
- The case is historical: a refresh must reproduce the baseline exactly. Only change the case if every gate below still passes for it.

## 3. Sources

- Samples, parent and survival: the helper's window `Sep 28` (`before`, `samples`, `survival`, `delivered`, `first`). Survival counts first-parent main commits after the merge through the window end that touch any file the merge changed.
- Pull request: `gh api repos/github/gh-aw/pulls/63949` (`title`, `labels`, `additions`, `deletions`, `changed_files`, `created_at`, `merged_at`, `merged_by.type`).
- Adoption: `gh aw audit 36389625388 --adoption --json` (main build; gh aw v1.2.3 lacks it): `.adoption[0].adoption.acceptance_kind` must be `explicit_human`. Its native `survival.coverage` is only `snapshot`; full-window survival comes from the helper, never from the snapshot.
- Not shared: no other merge in the helper's `merges` within 7 days of T.

## 4. Queries

```bash
jq -c '.windows[] | select(.label == "Sep 28") | {delivered: .delivered[0][0:16], accepted: .first[0:16], parent: .before, samples, end, survival}' "$TMPDIR/dtv.json"
jq -r '.merges[].merged' "$TMPDIR/dtv.json"
gh api repos/github/gh-aw/pulls/63949 --jq '{title, labels: [.labels[].name], additions, deletions, changed_files, created_at, merged_at, by: .merged_by.type}'
gh api repos/github/gh-aw/pulls/63949/reviews --jq '[.[] | [.user.type, .state]]'
go build -o "$TMPDIR/gh-aw-main" ./cmd/gh-aw
"$TMPDIR/gh-aw-main" audit 36389625388 --repo github/gh-aw --adoption --json > "$TMPDIR/daa-audit.json"
jq -c '.adoption[0] | {kind: .adoption.acceptance_kind, human: .adoption.human_verified, at: .adoption.at, survival: .survival.coverage}' "$TMPDIR/daa-audit.json"
```

| Field | Baseline (17 Nov 2026) | Slide |
|---|---|---|
| delivered / accepted | `2026-09-28T07:08` / `2026-09-28T16:37` | Delivered and Accepted events |
| `parent` → day 0 | `63/68` → `68/68` | `+5 fields`, Value event |
| `samples` | 7 × `63/68`; `68/68` × 4, `68/69`, `69/69` × 2 | squares `5` × 7; `0 0 0 0 1 0 0` |
| `survival` | 257 commits, 5 files, 0 touched, through `2026-10-05T16:37` | Kept event |
| pull request | title, `automation` and `schema-coverage`, +150 −0, 5 files, merged by a `User` | header |
| reviews | 4 `Bot` `COMMENTED`, after the merge | wording: merged, not approved |
| adoption | `explicit_human`, `human_verified` true | Accepted event |
| other merges | nearest 17 Aug and 19 Oct | not shared |

## 5. Update steps

1. Run the queries and confirm every baseline value. Replace changed `CASE` values with a node splice, never by hand.
2. If any gate fails (no native human record, a different first parent, a touched file, an overlapping merge), drop the attributed card and Value event, say why in the notes, and update From Delivery to Value's `EXCERPT`.
3. Update the caption, the notes and the Exception bullet together.

## 6. Validation

```bash
node --check docs/public/cracktro/deck.js
python3 -m http.server 4329 --bind 127.0.0.1 --directory docs/public   # async
# browser canvas: http://127.0.0.1:4329/cracktro/#deliver-accept-attribute, press → to finish, screenshot .stage
cd docs && npx playwright test tests/talk.spec.ts                       # if node_modules exists
```

- Check the header, the five events, the 14 squares and the caption fit, and the badge turns from Open to Merged. No PR numbers, real logins or URLs on the slide or in notes.

## 7. Pitfalls

- The pull request had no human approval review (only bot review comments after the merge): say merged, never approved.
- The maintainer is shown as Mona; never show the real login. `github-actions[bot]` is a bot, not a person.
- The one after-window day with a missing field is a new schema field, covered the next day by others: keep it visible, explain it in notes, and never credit or debit it to this pull request.
- Attribution is event-aligned, not exclusive causation, and the check is textual presence, not working examples, fewer defects or saved hours.
