---
name: cracktro-refresh-daily-improvers
description: Recompute the measured data on the cracktro "Daily Improvers" slide and update deck.js.
---

# Refresh: Daily Improvers

Load `.github/skills/cracktro/SKILL.md` first. Never read `docs/public/cracktro/deck.js` whole; use `grep -n` plus `view_range`.

## Workflows mined

| Workflow file | Repo | Role in the slide's flow | Signal mined |
|---|---|---|---|
| `.github/workflows/duplicate-code-detector.md` | gh-aw | Detector: files duplicate-code issues | `[duplicate-code]` issues by github-actions |
| Copilot coding agent (not a workflow) | gh-aw | Fixer: assigned the issues | Copilot assignment (timeline event or current assignee) and merged closing PRs |

## 1. Slide

- Marker: `grep -n "// Daily Improvers (research" docs/public/cracktro/deck.js`. Group `process`, CSS prefix `du-` (styles: `grep -n "\.du-" deck.js`).
- Data lives in the SVG builder just before `slides.push`, under the comment `// Measured: gh-aw [duplicate-code] issues ...`:
  - `const DU_MONTHS = [[filed, merged], ...]`: one pair per calendar month, oldest first (13 pairs, Oct 2025 → Oct 2026). Bars are 40 px apart from x=608; more than 13 months overflow the 552 px card.
  - Card header string: `7 Oct 2025 – 26 Oct 2026 (385 days) · snapshot 14 Nov 2026`.
  - Stat array: `["211", "", "issues", "filed"]`, `["144", "p", "assigned", "to Copilot"]`, `["115", "g", "closed by", "merged PRs"]`, `["3.8 h", "", "median issue", "→ merge"]`.
  - Axis labels: `>Oct 2025</text>` (left) and `>Oct 2026</text>` (right).
  - Comment line above `DU_MONTHS` repeats the window.
- `notes:` repeats every number, the window (twice, in words), "112 of them opened by Copilot", the snapshot date and "Since 27 October 2026 the detector opens its pull requests directly".
- The slide has no caption element; the panel header carries the window.
- Also stated in `.github/skills/cracktro/content-rules.md`, "Exception: the Daily Improvers slide (20, ...)" bullet.

## 2. Window & snapshot

- Current: FROM `2025-10-07` (first detector issue) to TO `2026-10-26` (last issue before the switch), 385 days inclusive; snapshot `2026-11-14`.
- The detector now uses `create-pull-request` (`.github/workflows/duplicate-code-detector.md`, `title-prefix: "[duplicate-code] "`), so it files no new issues after 26 Oct 2026. Keep FROM/TO fixed unless that changes; a refresh mostly updates the snapshot date and late closures (merged count, median, merged bars).
- If issues resume, roll TO to the last full day before the snapshot, keep FROM, and recompute days as `TO − FROM + 1`. If months exceed 13, drop the oldest months from both the window and `DU_MONTHS` (move FROM to the first of a month) rather than resizing bars.

## 3. Sources

- GitHub issue search on `github/gh-aw`: issues with title prefix `[duplicate-code]` authored by `app/github-actions` (the detector's safe outputs).
- Each issue's timeline: `AssignedEvent` (Copilot) and `ClosedEvent.closer` (merged PR and its `mergedAt`, author).
- Each issue's current `assignees` (some issues were assigned to Copilot at creation and have no event; for example #4615).

## 4. Queries

Run from the repo root with an authenticated `gh` (read-only, prints JSON, no secrets):

```bash
node .github/skills/cracktro/refresh/daily-improvers.mjs 2025-10-07 2026-10-26
```

Per month it runs this GraphQL search (≤1,000 hits per query, hence monthly paging) and keeps titles that start with `[duplicate-code]`:

```text
repo:github/gh-aw is:issue in:title "[duplicate-code]" author:app/github-actions created:<a>..<b>
```

Definitions:
- `filed`: prefix-matching issues created in the window.
- `assignedToCopilot`: an `AssignedEvent` to a login matching `/^copilot/i` (`copilot-swe-agent`) OR a current assignee `Copilot`.
- `closedByMergedPR`: first `ClosedEvent` whose closer is a merged `PullRequest`; `mergedPRsByCopilot` counts those PRs authored by Copilot.
- `medianIssueToMergeHours`: median of `PR.mergedAt − issue.createdAt`, one decimal.
- `months`: `[YYYY-MM, filed by issue createdAt, merged by PR mergedAt]`.

Output shape and baseline (snapshot 2026-11-14):

```json
{"window":{"from":"2025-10-07","to":"2026-10-26","days":385},"snapshot":"2026-11-14",
 "filed":211,"assignedToCopilot":144,"closedByMergedPR":115,"mergedPRsByCopilot":112,
 "medianIssueToMergeHours":3.8,
 "months":[["2025-10",30,17],["2025-11",49,30],["2025-12",31,24],["2026-01",0,0],["2026-02",0,0],
  ["2026-03",0,0],["2026-04",0,0],["2026-05",5,2],["2026-06",15,4],["2026-07",19,11],
  ["2026-08",30,17],["2026-09",0,0],["2026-10",32,10]]}
```

Run it once on an unchanged window first; if it does not reproduce the baseline (apart from late closures), stop and investigate before editing.

## 5. Update steps

1. Save the JSON to the session `files/` folder (not the repo).
2. `DU_MONTHS`: replace pairs with `months[i][1..2]`, keeping 7 pairs per line style.
3. Stat array: `filed`, `assignedToCopilot`, `closedByMergedPR`, `"<median> h"`.
4. Panel header: `D Mon YYYY – D Mon YYYY (N days) · snapshot D Mon YYYY` (en dash, middle dot); axis labels to first/last month; update the `// Measured:` comment window.
5. `notes:`: update every number, "112 of them opened by Copilot", both window mentions, "385 days", and "Snapshot taken ...". Keep the sentences "The flow is illustrative, with synthetic files" (must still match `/illustrative|synthetic|simulated/i`) and the 27 October switch sentence.
6. `.github/skills/cracktro/content-rules.md`: update the Daily Improvers Exception bullet (window, 211/144/115/3.8 h, snapshot month).
7. Keep `docs/README.md` in sync only if it states these numbers (`grep -n "duplicate-code" docs/README.md`).

## 6. Validation

- `node --check docs/public/cracktro/deck.js`.
- Preview: `python3 -m http.server 4329 --bind 127.0.0.1 --directory docs/public` (async, detached), open `http://127.0.0.1:4329/cracktro/#daily-improvers` in the browser canvas, reload, screenshot. Check the four stats fit their 138 px columns, bars stay inside the card (tallest bar ≤ ~110 px; the current max is 49), and the header fits.
- Grep that old values are gone: `grep -n "211\|3.8 h\|26 Oct 2026" docs/public/cracktro/deck.js` should only show intended lines.
- Content rules: no people, no `github/<repo>` refs, no 40-hex SHAs, no new URLs. Tests (from `docs/`, after `npm ci`): `npx playwright test tests/talk.spec.ts`; say so if not run.

## 7. Pitfalls

- Search without `author:app/github-actions` and the `startsWith` check returns 218 prefix matches (some human-filed) and ~400 hits overall, including `[duplicate-code]` PRs; both filters are required.
- Counting Copilot only from timeline events gives 143; include current assignees.
- Use only merged closers; issues closed by unmerged PRs or manually do not count.
- Bucket merged bars by `mergedAt`, not issue creation; a refresh can move bars in old months.
- Link the slide by its stable id `#daily-improvers`; locate it in `deck.js` by marker.
- `HOLD`, `nIssue`, `nDev` repeat across slides; make `edit` `old_str` unique with `du-`/`DU_MONTHS` context.
- Do not commit the JSON output or touch the illustrative flow (`SEQ`, `EDGES`, files).
