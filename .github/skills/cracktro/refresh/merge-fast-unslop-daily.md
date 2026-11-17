---
name: cracktro-refresh-merge-fast-unslop-daily
description: Recompute the measured data on the cracktro "Merge fast, unslop daily" slide and update deck.js.
---

# Refresh: Merge fast, unslop daily

Plan for recomputing this slide's measured data. Load `.github/skills/cracktro/SKILL.md` first. Never read `deck.js` whole: grep, then `view_range`.

## Workflows mined

| Workflow file | Repo | Role in the slide's flow | Signal mined |
|---|---|---|---|
| `.github/workflows/jsweep.md` | gh-aw | Improver | merged `[jsweep]` PRs |
| `.github/workflows/dead-code-remover.md` | gh-aw | Improver | merged `[dead-code]` PRs |
| `.github/workflows/code-simplifier.md` | gh-aw | Improver | merged `[code-simplifier]` PRs |
| `.github/workflows/tidy.md` | gh-aw | Improver | merged `[tidy]` PRs |
| `.github/workflows/daily-file-diet.md` | gh-aw | Improver | merged `[file-diet]` PRs |
| `.github/workflows/duplicate-code-detector.md` | gh-aw | Improver | merged `[duplicate-code]` PRs |
| Copilot coding agent (not a workflow) | gh-aw | Author of many merged PRs | all merged PRs in the 30-day window (rate, size, time to merge) |

## 1. Slide

- Heading: "Merge fast, unslop daily" (band `process`).
- Marker: `// Merge fast, unslop daily:` in `docs/public/cracktro/deck.js`.
- CSS prefixes: `mfu-` (measured panel, top right) and `us-` (illustrative lanes and the bottom caption).
- Data lives only in these places (grep each):
  - `const UNSLOP = [[name, count], ...]`: all-time merged clean-up PRs per workflow, sorted descending. Display names: `jsweep`, `dead-code-remover`, `code-simplifier`, `tidy`, `daily-file-diet`, `duplicate-code-detector`.
  - `const DAY_ALL = [...]` (30 ints): merged PRs per UTC day, oldest first.
  - `DAY_UN = [...]` (30 ints): improver PRs merged per UTC day; a dot is drawn where the value is non-zero.
  - `mfu-win`: `15 Oct – 13 Nov 2026 · snapshot 14 Nov 2026`.
  - `mfu-big`: `1,263 <small>PRs merged · 42/day</small>`.
  - `mfu-row`: `median <b>166</b> lines · <b>1h54m</b> to merge`.
  - `mfu-leg`: `unslop PRs merged on 25 of 30 days`.
  - `us-meas` caption: `MEASURED · improver PRs, all time · 26 Sep 2025 – 12 Nov 2026 · 427 merged, +34,691 −44,700 · lanes illustrative`.
  - `notes:` string: repeats every number above, plus `261 days` and the bar scale.
- The bar height is `v * 0.6` px in a 64 px SVG. If any day exceeds about 80, lower the 0.6 scale so the bars still fit.

## 2. Window & snapshot

| Data | Kind | Current | Roll forward |
|---|---|---|---|
| Panel and bars | rolling 30 UTC days | 2026-10-15..2026-11-13 | END = last full UTC day; START = END − 29 |
| UNSLOP and caption | all time | 2025-09-26..2026-11-12 | first and last merged dates of the matched PRs |
| Snapshot | date queried | 2026-11-14 | today (UTC) |

Always show both windows on the slide, as in `mfu-k`/`mfu-win` and the `MEASURED · … all time · …` caption.

## 3. Sources

- Repo: `github/gh-aw` (public), merged pull requests only.
- Improver workflows (`.github/workflows/`) and their `title-prefix`:
  `jsweep.md` `[jsweep]`, `dead-code-remover.md` `[dead-code]`, `code-simplifier.md` `[code-simplifier]`,
  `tidy.md` `[tidy]`, `daily-file-diet.md` `[file-diet]`, `duplicate-code-detector.md` `[duplicate-code]`.
- Before each refresh, re-check the prefixes: `grep -n "title-prefix" .github/workflows/{jsweep,dead-code-remover,code-simplifier,tidy,daily-file-diet,duplicate-code-detector}.md`.

## 4. Queries

Preferred: run the read-only helper. It prints JSON.

```bash
.github/skills/cracktro/refresh/merge-fast-unslop-daily.sh            # END = yesterday (UTC)
.github/skills/cracktro/refresh/merge-fast-unslop-daily.sh 2026-11-13 # reproduce the current slide
```

Output shape:

```json
{ "snapshot": "YYYY-MM-DD",
  "window": { "start", "end", "merged", "search_total", "per_day", "median_lines",
              "median_minutes_to_merge", "DAY_ALL": [30], "DAY_UN": [30], "unslop_days" },
  "all_time": { "merged", "additions", "deletions", "days", "first", "last",
                "by_prefix": { "jsweep", "dead-code", "code-simplifier", "tidy", "file-diet", "duplicate-code" } } }
```

Manual equivalents:

```bash
# 30-day window: slice into <=8-day ranges (gh search returns at most 1000 results), merge, dedupe by .number
gh pr list -R github/gh-aw --state merged --search "merged:2026-10-15..2026-10-22" --limit 1000 \
  --json number,title,createdAt,mergedAt,additions,deletions,author
# Cross-check the total (must equal window.merged)
gh api -X GET search/issues -f q="repo:github/gh-aw is:pr is:merged merged:2026-10-15..2026-11-13" --jq .total_count
# All-time per improver; keep only titles that START with the prefix
gh pr list -R github/gh-aw --state merged --search '"[dead-code]" in:title' --limit 1000 \
  --json number,title,createdAt,mergedAt,additions,deletions | jq '[.[]|select(.title|startswith("[dead-code]"))]'
```

Definitions: lines = additions + deletions. Time to merge = `mergedAt − createdAt`. Medians are over all merged PRs in the window. Days are bucketed by the UTC date of `mergedAt`.

Baseline (END 2026-11-13, snapshot 2026-11-14), which the helper reproduces exactly:
- window: merged 1263, search_total 1263, per_day 42, median_lines 166, median_minutes_to_merge 114 (shown as 1h54m), unslop_days 25.
- DAY_ALL `[31,77,25,31,42,41,51,49,17,36,41,24,35,12,26,32,28,38,38,69,72,37,37,64,63,60,46,63,31,47]`.
- DAY_UN `[1,1,2,1,1,1,1,1,1,1,2,0,1,0,3,0,1,2,1,1,1,1,0,2,2,1,1,1,1,0]`.
- all_time: 427 merged, +34,691 −44,700, 261 days, 2025-09-26..2026-11-12; jsweep 144, dead-code 117, code-simplifier 89, tidy 66, file-diet 7, duplicate-code 4.

For context only (do not put it on the slide unless asked): about 25k PRs have ever been opened, 18,954 merged all time, and 17,073 merged in the last 365 days. 42/day over 30 days is consistent with these totals.

## 5. Update steps

1. Run the helper and confirm `window.merged == window.search_total`.
2. Replace the `UNSLOP` counts, re-sort them descending, and keep the display names.
3. Replace the `DAY_ALL` and `DAY_UN` arrays (exactly 30 ints each, oldest first).
4. Update the `mfu-win` dates (`D Mon – D Mon YYYY · snapshot D Mon YYYY`), `mfu-big` (comma thousands, per_day), `mfu-row` (median lines, minutes as `XhYYm`) and `mfu-leg` (`unslop_days of 30`).
5. Update the `us-meas` caption: first–last dates, merged count, `+additions −deletions` (U+2212 minus). Keep `lanes illustrative`.
6. Update the speaker notes: every number and date, both windows, the snapshot date. They must still say the lanes are illustrative and match `/illustrative|synthetic|simulated/i`.
7. Update the slide's `Exception:` bullet in `.github/skills/cracktro/content-rules.md` with the new numbers and windows.
8. Update the baseline in section 4 of this file.

## 6. Validation

- `node --check docs/public/cracktro/deck.js`.
- Preview: `python3 -m http.server <free port> --bind 127.0.0.1 --directory docs/public`, open `/cracktro/#merge-fast-unslop-daily` in the browser canvas and screenshot. Check that the panel fits without wrapping, the caption does not reach the sound button, and the bars stay inside the panel.
- Content rules: no 40-hex SHAs, no URLs, no `github/<repo>` strings in deck.js, no `who:`, no people's names (workflow names only).
- `cd docs && npx playwright test tests/talk.spec.ts`.

## 7. Pitfalls

- Shared prefixes: `[docs]` (unbloat-docs) is used by several workflows, so it is excluded. Match with `startswith`, because search also returns PRs that only mention the prefix.
- Pagination: `gh pr list --search` stops at 1000 results. Slice the window, and the helper fails if a slice hits 1000. If an all-time prefix count reaches 1000, split it by `merged:` year.
- Attribution: improver PRs are authored by `app/github-actions` or `app/copilot-swe-agent`. Count them by prefix, not by author. Squash merges keep the PR title, so the prefix survives.
- duplicate-code-detector mostly files issues (about 218), so its PR count is small. That is expected.
- Rate limits and TLS timeouts: rerun the helper; it is idempotent. Search API: 30 requests/min.
- Dates are UTC. Do not include today (partial day).
- Do not duplicate slide "How PRs land" (time-to-merge and size distributions); keep this slide to cadence plus unslop.
