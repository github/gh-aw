---
name: cracktro-refresh-unknown-not-zero
description: Recompute the measured data on the cracktro "Unknown, not zero" slide and update deck.js.
---

# Refresh "Unknown, not zero"

Load `.github/skills/cracktro/SKILL.md` first. Never read `docs/public/cracktro/deck.js` whole; `grep -n`, then `view_range`.

## Workflows mined

| Workflow file | Repo | Role in the slide's flow | Signal mined |
|---|---|---|---|
| `.github/workflows/mattpocock-skills-reviewer.md` | github/gh-aw | reviewer whose value gates are assessed (left column) | native delivery/adoption items from `gh aw logs --delivery --adoption --json` |

## 1. Slide

- Marker: `// Unknown, not zero:`. CSS prefix `unz-`. Group `platform`, `under: "value"`: an example in the column below Turn Automation into Value, after From Delivery to Value.
- Data lives inside that block only:
  1. `GATES`: Delivered (`127 outputs`), Adopted (`115 · 0 human`, `12 reviews dismissed`), Windows (`7 + 7 days`, `adopted 9 Nov`), and the four `?` gates (lineage, witness, survival, population).
  2. The `.unz-pos` contrast box restates From Delivery to Value (`71% to 100%`, gate chips); refresh it with `refresh/from-delivery-to-value.md`.
  3. The `.unz-cap` caption and the `notes:` string (every number and date).
- Elsewhere: the `content-rules.md` Exception bullet "the Unknown, not zero slide (`unz-` classes)".
- Frozen, not refreshed here: the pilot's primary outcome, rules and window choices (`.unz-goal`, `rules`) and the pre-workflow witness (pull request 25709, merged 10 Apr 2026: two existing regression tests fail on its actual first parent and pass on the accepted revision under Go 1.25.8). A new contract means a new slide message.

## 2. Window & snapshot

- Skills Reviewer: the latest 100 runs at the snapshot (baseline: runs 15 Nov 21:04 to 17 Nov 15:34 UTC, snapshot 17 Nov 2026). The Windows gate needs an adoption whose 7-day post-window is complete (baseline: reviews adopted 9 Nov 2026, windows 2–9 and 9–16 Nov).

## 3. Sources

- `gh aw logs ... --json`: `.adoption[]` (one report per run; `.coverage` `complete`/`unavailable`); `.adoption[].items[]` per delivered output: `type`, `adoption_status`, `adoption.acceptance_kind` (`fallback_delivery` = accepted by delivery, not a person), `adoption.human_verified`, `reversal_signal`, `survival.coverage`. `.runs[].created_at` gives the run window.
- Mature reviews: `gh aw audit RUN --adoption` on a run whose review was adopted at least 7 days before the snapshot (baseline producer run 42718572645, review on pull request 76330, fallback adoption 2026-11-09T23:25:46Z, survival `unsupported`).

## 4. Queries

Skills Reviewer lifecycle (gh aw v1.2.3 lacks these `logs` flags; use a main build):

```bash
go build -o "$TMPDIR/gh-aw-main" ./cmd/gh-aw
"$TMPDIR/gh-aw-main" logs mattpocock-skills-reviewer --repo github/gh-aw -c 100 --delivery --adoption --json \
  -o "$TMPDIR/unz-logs" > "$TMPDIR/unz-logs.json"
jq '{reports: (.adoption | length), coverage: ([.adoption[].coverage] | group_by(.) | map({(.[0]): length}) | add),
  runs: ([.runs[].created_at] | [min, max])} + ([.adoption[].items[]] | {delivered: length,
  by_type: (group_by(.type) | map({(.[0].type): length}) | add),
  accepted: map(select(.adoption_status == "accepted")) | length,
  fallback: map(select(.adoption_status == "accepted" and .adoption.acceptance_kind == "fallback_delivery")) | length,
  human: map(select(.adoption.human_verified == true)) | length,
  rejected: (map(select(.adoption_status == "rejected") | .reversal_signal) | group_by(.) | map({(.[0]): length}) | add),
  survival: (map(.survival.coverage) | group_by(.) | map({(.[0]): length}) | add)})' "$TMPDIR/unz-logs.json"
```

| Field | Baseline | Slide |
|---|---|---|
| `reports` / `coverage` | 95 / complete 93, unavailable 2 (5 of 100 runs returned no report) | caption `95 with native reports` |
| `delivered` / `by_type` | 127 / 93 `create_pull_request_review_comment`, 34 `submit_pull_request_review` | Delivered chip, notes |
| `accepted` / `fallback` / `human` | 115 / 115 / 0 | Adopted chip `115 · 0 human` |
| `rejected` | `review_dismissed` 12 | Adopted detail |
| `survival` | `unsupported` 115, `snapshot` 12 | Survival gate stays `?` |

## 5. Update steps

1. Run the query. Update the Delivered and Adopted chips and details, the caption window and report count, the notes and the Exception bullet.
2. Move the Windows gate to the newest adoption cohort whose 7-day post-window is complete; re-audit its survival.
3. Flip a `?` gate only when the frozen evidence rule is met (explicit finding → corrective commit → accepted merge; one regression test failing on the unfixed and passing on the accepted revision; complete-window survival; a complete defect population). Merges, comments or resolved threads never flip a gate. If contribution becomes available, rewrite the result, summary and heading.
4. When From Delivery to Value is refreshed, keep the contrast box (`.unz-pos`) consistent with it.

## 6. Validation

```bash
node --check docs/public/cracktro/deck.js
python3 -m http.server 4329 --bind 127.0.0.1 --directory docs/public   # async
# browser canvas: http://127.0.0.1:4329/cracktro/#N (N = this slide's row in slides.md), press → to finish, screenshot .stage
grep -nE '\b[0-9a-f]{40}\b|https?://' docs/public/cracktro/deck.js | grep -v 'introduction/overview\|agentics-beyond-code\|api.github.com/repositories/\|www.w3.org/2000/svg'   # must be empty
cd docs && npx playwright test tests/talk.spec.ts                       # if node_modules exists
```

- Check every gate row, chip and the caption fit; no PR numbers, logins or URLs on the slide or in notes. Stop the preview server afterwards.

## 7. Pitfalls

- The latest 100 runs move several times a day: counts will not reproduce exactly; report the actual run window.
- `fallback_delivery` acceptance is not human approval, and a `snapshot` survival is not full-window survival. Never relabel either.
- Unknown or unavailable is not zero: never show 0% or a delta for the pilot's primary outcome.
