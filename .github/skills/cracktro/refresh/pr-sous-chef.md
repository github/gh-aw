---
name: cracktro-refresh-pr-sous-chef
description: Recompute the measured data on the cracktro "PR Sous Chef" slide and update deck.js.
---

# Refresh: PR Sous Chef

Recompute with `pr-sous-chef.mjs`, then patch `deck.js`. Read-only (GitHub GraphQL search through `gh`); edit only the files listed in step 5.

## Workflows mined

| Workflow file | Repo | Role in the slide's flow | Signal mined |
|---|---|---|---|
| `.github/workflows/pr-sous-chef.md` | gh-aw | Prefilter plus agent: picks stuck PRs and posts the `@copilot` prep list | PR comments by `gh-aw-bot`/`github-actions` containing `pr-sous-chef` that mention `@copilot` first or carry a prep list |
| Copilot coding agent (not a workflow) | gh-aw | Cooks the prep list | `copilot-swe-agent` comments after a nudge and before the next one |

## 1. Slide

- Heading: **PR Sous Chef** (`group: "process"`, `under: "prr"`): the last kid of the Pull Request Review² column (after Decide · Draft · Record; its code sits just before `// Patch to PR`).
- Marker: `grep -n "// PR Sous Chef" docs/public/cracktro/deck.js`, then use `view_range`. Never read deck.js whole.
- CSS prefix: `psc-` (`psc-hd` column headers, `psc-c` idle/work chips, `psc-v` nudge or skip verdicts, `psc-m` strip width). It reuses `.sdd-*` panels, `.fm-*` flow, `.dhm*` strip and `.ddm-tl` bars.
- Data lives in the slide block:
  - `const MONTHS = [["May",211], ...]`: nudges per month; the last bar (class `h`) is the partial snapshot month.
  - KPIs: `kpi("5,826","@copilot","nudges")`, `kpi("2,623","pull requests","nudged")`, `kpi("71%","answered","by Copilot","g")`, `kpi("90%","merged, median","1.8 h after a nudge","g")`.
  - Caption (`dhm-cap`): `"MEASURED · snapshot 17 Nov 2026 · PR comments 13 May–16 Nov 2026 (188 d) · flow illustrative"`.
  - `notes:` contains the sentence starting "The bottom strip is measured from gh-aw pull request comments carrying the Sous Chef marker, …" and the prep-list sentence after it.

## 2. Window & snapshot

- **All-time.** From the first nudge (13 May 2026) to yesterday UTC; the snapshot is the run day. Roll forward by running with no arguments.
- Day count = inclusive UTC days (188 d for 13 May–16 Nov 2026).

## 3. Sources

- GraphQL issue search: `repo:github/gh-aw is:pr "gh-aw-workflow-call-id: github/gh-aw/pr-sous-chef" in:comments created:<week>` from 2026-05-01, in 7-day PR-creation windows, with `comments(last:100)` per PR.
- Nudge = a Sous Chef comment whose body, after an optional leading ```` ```hidden ```` block, starts with `@copilot` (backticks allowed), or that contains "Copilot assignment requested for the following outstanding work" (the prep list).
- Answered = a `copilot-swe-agent` comment after the nudge and before the PR's next nudge.
- Merged = nudged PRs with `mergedAt`; hours are measured from the first nudge in the window.

## 4. Queries

```bash
RAW=/tmp/psc-raw.json node .github/skills/cracktro/refresh/pr-sous-chef.mjs > /tmp/psc.json      # ~8 min; default window 2026-05-13..yesterday UTC
IN=/tmp/psc-raw.json node .github/skills/cracktro/refresh/pr-sous-chef.mjs 2026-05-13 2026-11-16 # recompute from the dump, no API calls
```

Output: `{ window: {start, end, days}, nudges, nudgedPRs, prepLists, prepItems: {branch, review, check, other}, ciFollowUps, answered, merged, nudgeToMergeHours: {median, n}, months: {"YYYY-MM": {nudges, prs}}, truncated: [PR numbers] }`.

Sanity baseline (window 2026-05-13..2026-11-16, run 17 Nov 2026):

| Item | Value |
|---|---|
| nudges / nudged PRs | 5,826 / 2,623 |
| answered | 4,109 (71%) |
| merged / median hours | 2,360 (90%) / 1.8 |
| prep lists / review / check / branch items | 550 / 2,105 / 580 / 550 |
| CI follow-ups | 33 |
| months (nudges) | May 211, Jun 509, Jul 1,414, Aug 1,610, Sep 832, Oct 662, Nov 588 |
| truncated PRs (>100 comments) | 7 |

## 5. Update steps

1. **KPIs:** `nudges`, `nudgedPRs`, `round(100 * answered / nudges)`%, `round(100 * merged / nudgedPRs)`% and `nudgeToMergeHours.median`.
2. **Bars:** replace `MONTHS` with `months[*].nudges` (short month names; keep about 7 bars, dropping the oldest).
3. **Caption:** snapshot date, window end and day count. Keep "MEASURED ·" and "flow illustrative".
4. **Notes:** every number and date in the measured sentences (prep lists use `prepLists`, `prepItems.review`, `prepItems.check`). Keep "…illustrative, synthetic example." so the notes still match `/illustrative|synthetic|simulated/i`.
5. **`.github/skills/cracktro/content-rules.md`:** update the "PR Sous Chef slide" Exception bullet. Do not change other bullets.

## 6. Validation

```bash
node --check docs/public/cracktro/deck.js
python3 -m http.server 4359 --bind 127.0.0.1 --directory docs/public   # async; open http://127.0.0.1:4359/cracktro/#pr-sous-chef
git diff -U0 docs/public/cracktro/deck.js | grep '^+' | grep -nE '[0-9a-f]{40}|github/[a-z]|https?://|who:'   # must print nothing
cd docs && npx playwright test tests/talk.spec.ts
```

- On screen, check the strip clears the nav-dot minimap and the sound button, and that bar value labels do not touch the title.
- Never show PR numbers, file paths from real reviews, or handles: prep lists quote real review comments.
- Stop the server afterwards.

## 7. Pitfalls

- **Search cap:** search stops at 1,000 results; the script throws if a 7-day window reaches it (peak about 190 PRs per week).
- **Run counts:** the Actions runs API caps `total_count` at 2,500 and ignores `conclusion`; do not use it for run totals.
- **Bot logins:** May 2026 nudges come from `github-actions`, later ones from `gh-aw-bot`; `commenter:gh-aw-bot` alone misses May. GraphQL reports Copilot as `copilot-swe-agent`.
- **Format drift:** free-text nudges (May to Oct), ```` ```hidden ```` prefixes (4 to 9 Sep), prep lists without `@copilot` (4 to 8 Nov) and with it (from 5 Nov). Check the newest comment format before trusting the classifier.
- **Branch item:** every prep list starts with "Update this branch…" (unconditional in `scripts/pr-sous-chef.mjs`), so do not present `prepItems.branch` as a finding.
- **Merge rate is not causal:** most nudged PRs would merge anyway; say "merged", not "merged because of".
- **Truncation:** PRs with more than 100 comments are read from their last 100; nudges before that are undercounted (7 PRs at the baseline).
