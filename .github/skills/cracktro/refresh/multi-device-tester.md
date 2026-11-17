---
name: cracktro-refresh-multi-device-tester
description: Recompute the measured data on the cracktro "Multi-device tester" slide and update deck.js.
---

# Refresh: Multi-device tester

Recompute with `multi-device-tester.mjs`, then patch `deck.js`. Read-only on GitHub; edit only the files listed in step 5.

## Workflows mined

| Workflow file | Repo | Role in the slide's flow | Signal mined |
|---|---|---|---|
| `.github/workflows/daily-multi-device-docs-tester.md` | gh-aw | Tester: browses the docs on 10 viewports and files one report | `🔍 Multi-Device Doc…` issues; their closing PRs, classified by the files they change |

## 1. Slide

- Heading: **Multi-device tester** (`group: "data"`, AW factory band).
- Marker: `grep -n "// Multi-device tester" docs/public/cracktro/deck.js`, then use `view_range`. Never read deck.js whole.
- CSS prefixes: `mdt-` (the illustrative loop: devices, badges, overflow, Developer review) and `mdm-` (bar colours of the measured timeline). The strip reuses `.dhm`, `.dhm-k`, `.dhm-tl` and `.dhm-cap` from Docs that heal.
- Data lives in the slide block:
  - `const REPORTS = [[day, "s"|"t"|"o"|"x"], ...]`: one entry per report issue. `day` is days since the first report. `s` means a merged PR changed the docs site, `t` the tester workflow, `o` something else; `x` means no merged PR.
  - `TX = d => 10 + (d * (TW - 20)) / 346`: `346` is the axis span in days. Set it to `max(day) + ~3`.
  - Month ticks: `[[25,"Jan"],[84,"Mar"],[145,"May"],[206,"Jul"],[268,"Sep"],[329,"Nov"]]`, as day offsets from the first report.
  - Timeline title: `"61 REPORTS · 28 SITE · 9 TESTER · 2 OTHER FIXES"`.
  - KPIs: `kpi("61","daily reports",…)`, `kpi("39","fixed by",…"35 by Copilot")`, `kpi("28","docs site",…,"g")`, `kpi("6.5 h","median",…)`.
  - Caption (`dhm-cap`): `"MEASURED · snapshot 15 Nov 2026 · reports 7 Dec 2025–15 Nov 2026 (343 d) · loop illustrative"`.
  - `notes:` contains the sentence starting "The bottom strip is measured on gh-aw (snapshot …)".

## 2. Window & snapshot

- **All-time.** The window runs from the first titled report (7 Dec 2025) to the snapshot date. Roll it forward by passing today's date; the start stays fixed.
- Day counts are inclusive-exclusive differences of UTC dates. Current value: 343 d (the newest report falls on the snapshot day).

## 3. Sources

- Repository: gh-aw (public).
- Workflow: `.github/workflows/daily-multi-device-docs-tester.md` (originally `multi-device-docs-tester.md`).
- Earlier reports, if any, used other titles and are not counted.

## 4. Queries

```bash
node .github/skills/cracktro/refresh/multi-device-tester.mjs 2026-11-15 > /tmp/mdt.json   # omit date = today; ~1 min
```

Output shape:

```json
{ "snapshot": "YYYY-MM-DD", "window": [first,end], "days", "reports", "copilotAssigned",
  "fixed", "site", "tester", "other", "notPlanned", "noMergedPr", "open",
  "mergedPrs", "copilotPrs", "medianHoursToMerge",
  "REPORTS": [[day,"s|t|o|x"]...], "detail": [...] }
```

The script's underlying queries (`REPO` env var, default gh-aw):

1. Reports, keeping only titles that match `/^🔍 Multi-Device Doc/`:
   `gh issue list --state all --limit 1000 --search '"Multi-Device" in:title created:<=END' --json number,title,createdAt,closedAt,state,stateReason,assignees,closedByPullRequestsReferences`
2. For each closing PR: `gh pr view N --json number,author,mergedAt,title,files`. Only merged PRs count. The class is `s` if any file matches `^docs/(src|public)/` or `^docs/astro.config`, else `t` if it changes `daily-multi-device-docs-tester.(md|lock.yml)`, else `o`.
3. Median hours from report creation to the first merged closing PR.

Sanity baseline at the 2026-11-15 snapshot:

| Item | Value |
|---|---|
| reports | 61, 0 open, 2025-12-07 → 2026-11-15 |
| assigned to Copilot | 46 |
| fixed by a merged PR | 39 (28 `s`, 9 `t`, 2 `o`) |
| merged PRs / by Copilot | 39 / 35 |
| no merged PR | 22 (19 NOT_PLANNED, mostly 2-day expiries) |
| median report → merge | 6.5 h |

If the new numbers are lower than the baseline, suspect the query, not the data: check the title filter and pagination first.

## 5. Update steps

1. **KPIs:** set the four `kpi()` values from `reports`, `fixed` (with `copilotPrs`), `site` and `medianHoursToMerge`.
2. **Timeline:** replace `REPORTS`, update the `346` axis span in `TX`, and recompute the month ticks as day offsets of the 1st of each month from `window[0]` (keep about 6 labels). Update the title counts from `reports`, `site`, `tester` and `other`.
3. **Caption:** update the snapshot date, the window end and its day count. Keep "MEASURED ·" and "loop illustrative", and keep the caption ending left of the sound button.
4. **Notes:** update the snapshot date, the window and every number in the measured sentence, including the not-merged breakdown. Keep "…illustrative, simplified example…" so the notes still match `/illustrative|synthetic|simulated/i`.
5. **`.github/skills/cracktro/content-rules.md`:** update the numbers and dates in the "Multi-device tester slide" Exception bullet. Do not change other bullets.

## 6. Validation

```bash
node --check docs/public/cracktro/deck.js
python3 -m http.server 4339 --bind 127.0.0.1 --directory docs/public   # async; pick a free port; open http://127.0.0.1:4339/cracktro/#multi-device-tester
git diff -U0 docs/public/cracktro/deck.js | grep '^+' | grep -nE '[0-9a-f]{40}|github/[a-z]|https?://|who:'   # must print nothing
cd docs && npx playwright test tests/talk.spec.ts
```

- On screen, check that the strip clears the left nav-dot minimap and the sound button, that the Developer pill does not touch the strip, and that the bars stay inside `TW`.
- Use agent and role names only; never show people, issue or PR numbers, or URLs on the slide.
- Stop the server afterwards.

## 7. Pitfalls

- **Search matches loosely:** the search tokenises the emoji and also matches bodies, so always filter on the title prefix. Unfiltered searches return 62 to 127 hits; the filtered count is 61.
- **Date filter:** `created:..END` returns nothing; use `created:<=END`.
- **Classify by files, not titles:** some PRs titled after the site bug (for example a "hamburger breakpoint" fix) change only the tester's lock file, so they count as `t`.
- **Authors:** Copilot PRs are `app/copilot-swe-agent`; a few fixes come from `github-actions` or the gh-aw bot and are not counted in `copilotPrs`.
- **Expiry:** reports expire after 2 days, so most NOT_PLANNED closes are expiries, not rejections.
- **Day offsets:** compute them from UTC dates.
