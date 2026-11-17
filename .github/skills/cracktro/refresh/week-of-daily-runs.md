---
name: cracktro-refresh-week-of-daily-runs
description: Recompute the measured data on the cracktro "Week of daily runs" slide and update deck.js.
---

# Refresh: Week of daily runs

Recompute with `week-of-daily-runs.sh`, then patch `deck.js`. Reads the local checkout only; edit only the files listed in step 5.

## Workflows mined

| Workflow file | Repo | Role in the slide's flow | Signal mined |
|---|---|---|---|
| every `.github/workflows/*.md` whose front matter has a `schedule:` or `- cron:` starting with `daily` (plain, `on weekdays`, `around HH:MM`, `between A and B`) | gh-aw | one run per scheduled day in its weekday × UTC hour cell | the first `cron:` in its compiled `.lock.yml` (UTC hour and day-of-week field) |

## 1. Slide

- Heading: **Week of daily runs** (`group: "engineering"`, `under: "schedules"`: hangs below Fuzzy schedules). Kicker: "schedule: daily · 7 days × 24 UTC hours · measured".
- Marker: `grep -n "// Week of daily runs:" docs/public/cracktro/deck.js`, then `view_range`. Never read deck.js whole.
- CSS prefix `fzw-`; reuses `.fm-svg`, `.aex-svg`, `.aex-foot`.
- Data lives in the slide block:
  - `WD` (Mon–Fri row) and `WE` (Sat–Sun row): 24 runs per UTC hour each; `GRID` repeats them. If the helper ever returns different rows for days in the same group (a `dow` other than `*` or `1-5`), give each day its own array.
  - `W` (workflows) and `WKD` (weekday-only workflows). `MAX`, `RUNS`, `TOT` and the busiest hour are derived.
  - Cell shade is `0.12 + 0.88 * v / MAX`; values above `0.55 * MAX` switch to dark text.
  - Caption (`.fzw-cap`): `"MEASURED · gh-aw .github/workflows · snapshot 17 Nov 2026 · ${W} workflows with a daily schedule (plain, on weekdays, around) · first compiled cron · UTC"`. Add `between` back if a workflow starts using it.
  - `notes:` template string, built from the constants except the snapshot date and the sentence about the forms in use.

## 2. Window & snapshot

- Point-in-time: the workflows on `main` at the snapshot commit; the script reports that commit's date as `snapshot`.
- Roll forward by running on an up-to-date `main` checkout, or pass a ref.

## 3. Sources

- Repository: gh-aw (public), local checkout: front matter of `.github/workflows/*.md` and the matching `.lock.yml`.
- Bands: `pkg/parser/schedule_fuzzy_scatter.go` (00–01 never chosen; 02–05 weighted ×3). If the code changes, update the band labels and the notes.

## 4. Queries

```bash
git fetch origin main && .github/skills/cracktro/refresh/week-of-daily-runs.sh origin/main > "$TMPDIR/fzw.json"   # omit ref = HEAD; needs git + jq; seconds
```

Output shape:

```json
{ "snapshot": "YYYY-MM-DD", "workflows", "weekdays_only", "grid": [[24] x7, Mon..Sun],
  "runs_per_week", "weekday_runs", "weekend_runs", "at_00_01", "busiest" }
```

Selection: front matter between the first two `---` lines, first line matching `^\s+(schedule|- cron):\s*"?daily\b`; then the first `cron: "m h dom mon dow"` in the lock file. `dow` must be `*` or `1-5`; anything else stops the script.

Sanity baseline at the 2026-11-17 snapshot:

| Item | Value |
|---|---|
| workflows / weekdays only | 144 / 33 |
| Mon–Fri row | 0,0,8,13,8,17,4,7,9,7,7,11,6,5,11,6,0,3,5,3,6,1,3,4 |
| Sat–Sun row | 0,0,7,11,8,15,4,5,7,6,4,7,4,2,4,4,0,3,5,3,6,1,1,4 |
| runs per week | 942 |
| weekday / weekend runs a day | 144 / 111 |
| at 00–01 | 0 |
| busiest cell | 17 (05 UTC, weekdays) |

A non-zero `at_00_01` means a fixed cron slipped into the selection or the algorithm changed: check before publishing.

## 5. Update steps

1. **Data:** replace `WD`, `WE`, `W` and `WKD`.
2. **Caption and notes:** update the snapshot date in both; check the notes' wording ("busiest", "weekends drop") still matches the data.
3. **`.github/skills/cracktro/content-rules.md`:** update the numbers and date in the "Week of daily runs" Exception bullet. Do not change other bullets.

## 6. Validation

```bash
node --check docs/public/cracktro/deck.js
python3 -m http.server 4329 --bind 127.0.0.1 --directory docs/public   # async; open http://127.0.0.1:4329/cracktro/#week-of-daily-runs
git diff -U0 docs/public/cracktro/deck.js | grep '^+' | grep -nE '[0-9a-f]{40}|(^|[^.[:alnum:]_])github/[a-z]|https?://|who:' | grep -v 'w3.org/2000/svg'   # must print nothing
cd docs && npx playwright test tests/talk.spec.ts
```

- The slide animates in `tick`: a playhead steps one UTC hour every 110 ms, switching that column on and advancing every day's total bar and counter by the same hour; bands, busiest outline, key and caption then switch on one per 320 ms. Screenshot after ~5 s, once the caption is on. Check that two-digit counts fit their cells, the band labels clear the grid and the key lines clear the foot.
- No people, real names, issue or PR numbers, or URLs. Stop the server afterwards.

## 7. Pitfalls

- **Daily only:** hourly, `every Nh`, weekly and `every N days` workflows are excluded by design; they belong to other stories (fixed `every 2 days` piles up at 00:00).
- **First cron only:** a workflow with several schedules contributes its first compiled cron.
- **Hash identity:** the scatter hashes `owner/repo/<path>`; a fork or rename moves every time, so cells change even when no workflow did.
- **Empty hours:** an empty column other than 00–01 (16 UTC at the baseline) is chance, not a rule; do not explain it on the slide.
