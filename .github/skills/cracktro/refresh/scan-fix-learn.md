---
name: cracktro-refresh-scan-fix-learn
description: Recompute the measured data on the cracktro "Scan · Fix · Learn" slide and update deck.js.
---

# Refresh: Scan · Fix · Learn

Plan to recompute the slide's measured panel. Follow `.github/skills/cracktro/SKILL.md`: never read `deck.js` whole; grep the marker, then `view_range`.

## Workflows mined

| Workflow file | Repo | Role in the slide's flow | Signal mined |
|---|---|---|---|
| `.github/workflows/ci-failure-scan.md` | .NET runtime | Scanner (every 12 h) | `[ci-scan]` issues, Known Build Error label |
| `.github/workflows/ci-failure-fix.md` | .NET runtime | Fixer (weekdays) | `[ci-fix]` PRs |
| `.github/workflows/ci-failure-scan-feedback.md` | .NET runtime | Learner (bi-weekly): edits the scanner prompt | merged `[ci-scan-feedback]` PRs; KPI tracker issue |
| Copilot coding agent (not a workflow) | .NET runtime | Author of some prompt-edit PRs | `[ci-scan-feedback]` PRs authored by Copilot |

## 1. Slide

- Heading: `Scan · Fix · Learn` (process band). Kicker: "Continuous CI health · as built by the .NET runtime team".
- Marker: `grep -n "// Scan → Fix → Learn" docs/public/cracktro/deck.js`.
- CSS prefixes: `sfm-` (measured panel, the only data); `sfl-` (illustrative loop, do not touch).
- Data holders inside the slide block:
  - `const KBE_MONTHS = [["May", 65], ...]`: scanner issues per month (bars).
  - Bar scale `(n / 71) * 24`: `71` is the max of `KBE_MONTHS`; update it to the new max.
  - `const KPIS = [[number, label, sub], ...]`: `305/270 closed`, `22/5 merged`, `12 prompt edits merged`, `882 workflow runs`.
  - `h("div", { class: "sfm-t" }, "MEASURED · .NET RUNTIME · 5 MAY – 14 NOV 2026 · 193 DAYS")`: window title.
  - `h("div", { class: "sfm-c" }, "totals over the window · bars: issues/month · snapshot 14 Nov · flow illustrative")`: caption.
  - `notes:` string: first sentences after "The panel at the top right is measured…" repeat every number.

## 2. Window & snapshot

- Current: fixed start `2026-05-05` (first scanner issue) to snapshot `2026-11-14`, 193 days. All counts are totals over the window (all-time since launch).
- Roll forward: keep start `2026-05-05`, set snapshot to today; days = snapshot − start. Append one month to `KBE_MONTHS` per new month.
- If the panel gets crowded (>8 bars), switch to a fixed recent window (e.g. last 6 months): pass a new start to the script and relabel title, caption and notes.

## 3. Sources

- Repo: public `dotnet/runtime` (write ".NET runtime" in deck text, never `org/repo`).
- Workflows: `.github/workflows/ci-failure-scan.md` (scanner, every 12h, `[ci-scan]` issues), `ci-failure-fix.md` (weekday fixer, `[ci-fix]` PRs), `ci-failure-scan-feedback.md` (bi-weekly, `[ci-scan-feedback]` prompt-edit PRs, KPI tracker issue).

## 4. Queries

One shot (read-only, prints JSON, no secrets beyond the user's `gh` auth):

```bash
.github/skills/cracktro/refresh/scan-fix-learn.sh            # start 2026-05-05
.github/skills/cracktro/refresh/scan-fix-learn.sh 2026-06-01 # other start
```

Output shape and 14 Nov 2026 baseline:

```json
{ "window": {"start":"2026-05-05","snapshot":"2026-11-14","days":193},
  "scan": {"issues":305,"closed":270,"kbeLabelled":280,"medianDaysOpen":4,
           "perMonth":[{"month":"2026-05","n":65},{"month":"2026-06","n":56},{"month":"2026-07","n":36},
                       {"month":"2026-08","n":54},{"month":"2026-09","n":71},{"month":"2026-10","n":4},{"month":"2026-11","n":19}]},
  "fix": {"prs":22,"merged":5,"helpWanted":19},
  "feedback": {"mergedPromptEdits":12,"byCopilot":7},
  "runs": {"scan":429,"fix":292,"feedback":161,"total":882} }
```

Underlying queries (for manual checks):

```bash
R=dotnet/runtime
# scanner issues: title prefix + bot author
gh search issues --repo $R --match title "[ci-scan]" --created ">=2026-05-05" --limit 1000 \
  --json number,title,state,createdAt,closedAt,author,labels \
  | jq '[.[]|select((.title|startswith("[ci-scan]")) and .author.login=="github-actions[bot]")]|length'
# fixer PRs (merged = state MERGED; help-wanted = title contains "Needs review")
gh pr list -R $R --state all --limit 1000 --search 'author:app/github-actions "[ci-fix]" in:title' --json number,state,title
# feedback prompt-edit PRs, merged (humans and the Copilot coding agent)
gh pr list -R $R --state merged --limit 500 --search '"[ci-scan-feedback]" in:title' --json number,title,author
# runs per workflow
gh api "repos/$R/actions/workflows/ci-failure-scan.lock.yml/runs?per_page=1" --jq .total_count
```

## 5. Update steps

1. Run the script; compare with the baseline; investigate any decrease (deleted/transferred items).
2. `KPIS`: `[issues, "issues filed", "<closed> closed"]`, `[prs, "fixer PRs", "<merged> merged"]`, `[mergedPromptEdits, "prompt edits", "merged"]`, `[runs.total, "workflow runs", "3 workflows"]`.
3. `KBE_MONTHS`: one `["Mon", n]` per `perMonth` entry; set the bar-scale divisor to the new max. Mark the current month partial in notes.
4. Title: `MEASURED · .NET RUNTIME · <D MON> – <D MON YYYY> · <days> DAYS`. Caption: update `snapshot <D Mon>`; keep "flow illustrative".
5. Notes: rewrite the measured sentences (window, 305/280/270/median, monthly list, 22/19/5, 12/7, 882 = 429+292+161). Keep "synthetic and illustrative" for the loop so notes match `/illustrative|synthetic|simulated/i`.
6. content-rules.md: update the Scan · Fix · Learn "Exception:" bullet (window and counts).

## 6. Validation

- `node --check docs/public/cracktro/deck.js`.
- Preview per SKILL.md (static server on a free port, browser canvas, slide hash). Confirm title and caption each fit on one line (`white-space: nowrap`) and the panel stays above the lane header (y < ~300 px).
- Content rules: no `dotnet/runtime` or other `org/repo` strings, no 40-hex SHAs, no URLs, no people's names (agents and roles only); `cd docs && npx playwright test tests/talk.spec.ts`.

## 7. Pitfalls

- `gh search --match title "[ci-scan]"` is fuzzy: it also returns human issues and `[ci-scan-feedback]`/other prefixes. Always filter `startswith("[ci-scan]")` and `author.login=="github-actions[bot]"`.
- `[ci-fix]` search also matches old human PRs back to 2019; keep the `author:app/github-actions` filter. One human-authored `[ci-fix]` PR exists; it is excluded.
- Search API caps at 1000 results; if `issues` nears 1000, split by `--created` ranges.
- Feedback PRs are authored by humans and by `app/copilot-swe-agent`; count Copilot by login, never name people on the slide.
- Run counts include failed and cancelled runs; say "runs", not "successful runs".
- Search API rate limit is 30 req/min; the script makes 3 search calls.
- Month buckets use UTC `createdAt`.
