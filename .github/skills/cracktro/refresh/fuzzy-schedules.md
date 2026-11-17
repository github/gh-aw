---
name: cracktro-refresh-fuzzy-schedules
description: Recompute the measured data on the cracktro "Fuzzy schedules" slide and update deck.js.
---

# Refresh: Fuzzy schedules

Recompute with `fuzzy-schedules.sh`, then patch `deck.js`. Reads the local checkout only; edit only the files listed in step 5.

## Workflows mined

| Workflow file | Repo | Role in the slide's flow | Signal mined |
|---|---|---|---|
| every `.github/workflows/*.md` whose front matter has `schedule: daily` / `daily on weekdays` (string or list `- cron:` form) | gh-aw | one dot each in the left-half histogram | the first `cron:` in its compiled `.lock.yml` (UTC minute and hour) |
| `.github/workflows/daily-trajectory-grader-implementer.md` | gh-aw | real-world reference for the All You Can Eat loop (not counted; the right half is illustrative) | none |

## 1. Slide

- Heading: **Fuzzy schedules** (`group: "engineering"`, right after Don’t Run the Agent). Kicker: "schedule: daily · hashed scatter · skip-if-match · All You Can Eat".
- Marker: `grep -n "// Fuzzy schedules:" docs/public/cracktro/deck.js`, then `view_range`. Never read deck.js whole.
- CSS prefix `fzs-`; reuses `.aex-svg`, `.aex-foot`, `.aex-c`, `.aex-d`.
- Data lives in the slide block:
  - `const HOURS = [24 counts]` (UTC hour 00–23) and `const MINS = [60 counts]` (minute 0–59). `N` is their sum; the label, spike text and caption use `N`.
  - Minute bars are `n * 6` px tall; rescale if a minute exceeds 8.
  - Fuzzy dots stack two per column, 13 px per row; columns above ~25 overflow into the bands' labels.
  - `const SYN = [[source, compiled cron, what scatters, fixed?]]`: the "more syntax → compiled" table shown in the fuzzy state (x 290–846, y 150–330). Each cron is copied from a real gh-aw lock file; rows beyond seven run into the hour stacks.
  - Caption (`.fzs-cap`): `"MEASURED · gh-aw .github/workflows · snapshot 17 Nov 2026 · ${N} fuzzy daily workflows by compiled UTC hour and minute · spike and plates illustrative"`.
  - `notes:` sentences "The left half is measured on gh-aw itself, snapshot …" and "Measured: 45 of 111 land in 02 to 05, none at 00 or 01, none within three minutes of a quarter hour, 98 distinct times."

## 2. Window & snapshot

- Point-in-time: the workflows on `main` at the snapshot commit. The script reports that commit's date as `snapshot`.
- Roll forward by running on an up-to-date `main` checkout, or pass a ref.

## 3. Sources

- Repository: gh-aw (public), local checkout.
- Front matter of `.github/workflows/*.md` and the matching `.lock.yml`.
- Algorithm (for the bands and notes): `pkg/parser/schedule_fuzzy_scatter.go`. Hours 00–01 never chosen; 02–05 weighted ×3; minutes avoid 0–4, 12–18, 27–33, 42–48, 55–59. If the code changes, update the bands, the minute hatching and the notes together.
- All You Can Eat pattern: `.github/aw/workflow-patterns.md`.

## 4. Queries

```bash
git fetch origin main && .github/skills/cracktro/refresh/fuzzy-schedules.sh origin/main > "$TMPDIR/fzs.json"   # omit ref = HEAD; needs git + jq; seconds
```

Output shape:

```json
{ "snapshot": "YYYY-MM-DD", "workflows", "hours": [24], "minutes": [60],
  "preferred_02_05", "at_00_01", "on_quarter_minutes", "distinct_times" }
```

Selection: front matter between the first two `---` lines, matching `^\s+(schedule|- cron):\s*"?daily( on weekdays)?"?\s*(#.*)?$`; then the first `cron: "m h …"` in the lock file. `on_quarter_minutes` counts minutes within three of :00/:15/:30/:45.

Sanity baseline at the 2026-11-17 snapshot:

| Item | Value |
|---|---|
| workflows | 111 |
| hours 00–23 | 0,0,7,13,8,17,4,3,7,4,3,6,3,2,4,5,0,3,5,3,6,1,3,4 |
| minutes 0–59 | 0,0,0,0,0,2,4,7,3,4,0,6,0,0,0,0,0,0,0,5,5,6,1,5,4,2,8,0,0,0,0,0,0,0,1,3,4,5,5,4,5,5,0,0,0,0,0,0,0,4,3,4,1,2,3,0,0,0,0,0 |
| preferred 02–05 | 45 |
| at 00–01 / on quarter minutes | 0 / 0 |
| distinct times | 98 |

Syntax examples (`SYN`), each from one workflow's lock file; the hashed values change only if that workflow is renamed or removed:

| Source | Compiled |
|---|---|
| `every 30 minutes` | `27/30 * * * *` |
| `hourly` | `24 */1 * * *` |
| `every 6h` | `25 */6 * * *` |
| `daily on weekdays` | `11 14 * * 1-5` |
| `daily around 10:00` | `22 10 * * *` |
| `weekly on monday` | `22 3 * * 1` |
| `every 7 days` (fixed) | `0 0 */7 * *` |

Re-derive one: `f=$(grep -lE '^\s+(schedule|- cron):\s*"?<source>"?\s*$' .github/workflows/*.md | head -1); grep -oE 'cron: "[^"]+"' "${f%.md}.lock.yml" | head -1`. Which forms scatter is in `pkg/parser/schedule_parser.go` (`FUZZY:` prefixes); only `every N days` and raw cron compile without one.

A non-zero `at_00_01` or `on_quarter_minutes` means the selection picked up a non-fuzzy source or the algorithm changed: check before publishing.

## 5. Update steps

1. **Data:** replace `HOURS` and `MINS`; check the bar and dot scales above. Re-derive each `SYN` cron; if one changed, update the table and its row in the notes.
2. **Caption and notes:** update the snapshot date and every measured number (111, 45, 98). Keep "spike and plates illustrative" in the caption and "The spike is illustrative" / "illustrative All You Can Eat loop" in the notes so they still match `/illustrative|synthetic|simulated/i`.
3. **`.github/skills/cracktro/content-rules.md`:** update the numbers and date in the "Fuzzy schedules slide" Exception bullet. Do not change other bullets.

## 6. Validation

```bash
node --check docs/public/cracktro/deck.js
python3 -m http.server 4329 --bind 127.0.0.1 --directory docs/public   # async; open http://127.0.0.1:4329/cracktro/#fuzzy-schedules
git diff -U0 docs/public/cracktro/deck.js | grep '^+' | grep -nE '[0-9a-f]{40}|(^|[^.[:alnum:]_])github/[a-z]|https?://|who:' | grep -v 'w3.org/2000/svg'   # must print nothing
cd docs && npx playwright test tests/talk.spec.ts
```

- Screenshot after ~1.5 s (spike) and ~8 s (fuzzy). Check that the tallest hour count clears the band labels, that the minute legend clears the hour labels, and that the foot and caption do not collide.
- Mona is the only person; no real names, issue or PR numbers, or URLs.
- Stop the server afterwards.

## 7. Pitfalls

- **List form:** some workflows write `schedule:` as a list (`- cron: daily`); the regex covers both.
- **Windows excluded:** `daily around HH:MM` and `daily between A and B` are fuzzy but constrained; they are not counted, so the histogram shows the unconstrained scatter.
- **Fixed lookalikes:** `every 2 days` compiles to a fixed `0 0 */2 * *` (many smoke tests use it); raw cron is fixed too. `every N minutes` is not fixed: it gets a hashed start minute (`27/30 * * * *`). None are counted.
- **First cron only:** a workflow with several schedules contributes its first compiled cron.
- **Hash identity:** the scatter hashes `owner/repo/<path>`; a fork or rename moves every time, so counts per hour change even when no workflow did.
