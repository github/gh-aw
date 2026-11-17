---
name: cracktro-refresh-research-assign-observe
description: Recompute the measured data on the cracktro "Research · Assign · Observe" slide and update deck.js.
---

# Refresh: Research · Assign · Observe

Load `.github/skills/cracktro/SKILL.md` first. Never read `deck.js` whole; use `grep -n` plus `view_range`.

## Workflows mined

| Workflow file | Repo | Role in the slide's flow | Signal mined |
|---|---|---|---|
| `.github/workflows/daily-cli-performance.md` | gh-aw | Researcher: benchmarks the CLI daily and files regressions | `[performance] Regression in` issues (label `cookie`); `benchmark_history.jsonl` on repo-memory branch `memory/cli-performance` |
| `.github/workflows/issue-monster.md` | gh-aw | Assigner: picks issues for Copilot | `Issue Monster selected this for Copilot` comments |
| Copilot coding agent (not a workflow) | gh-aw | Fixer | Copilot assignment events and merged closing PRs |

## 1. Slide

- File: `docs/public/cracktro/deck.js`. Marker: `// Research → Assign → Observe: production telemetry`.
- CSS prefix: `rob-` (measured panel). `rao-` is the simulated fleet; do not touch it.
- Find the data: `grep -n 'FUNNEL = \[\|FIXES = \[\|rob-cap\|regression issues filed\|median 6.8 h' docs/public/cracktro/deck.js`.
- Constants to update (baseline 14 Nov 2026):
  - Comment above `const RX`: `// Measured (gh-aw, 13 Mar to 25 Oct 2026): ...`.
  - `FUNNEL` rows `[n, label, cls]`: `116 regression issues filed`, `101 assigned to Copilot`, `72 picked by Issue Monster` (`" m"`), `62 fixes merged`.
  - Bar width divisor `/ 116` in `FUNNEL.forEach`: must equal the first FUNNEL count.
  - `FIXES` rows `[date, benchmark, slowdown, hours]`: `21 Mar CompileComplex +40% 2.9 h`, `25 Apr Validation +276% 5.1 h`, `03 May CompileMCP +77% 1.5 h`, `29 Jul CompileMCP +279% 6.3 h`, `25 Oct ExtractWorkflowName +131% 2.9 h`. Keep exactly 5 rows (panel height is fixed; `tick` cycles `robFix0..4`).
  - Subtitle `rob-s`: `regression issues filed 13 Mar – 25 Oct 2026`.
  - Stat line: `median 6.8 h to merged fix · 55 of 62 &lt; 1 day` (keep `&lt;`).
  - Caption `rob-cap`: `Measured · issues filed 13 Mar – 25 Oct 2026 · 101 daily benchmarks 5 Apr – 13 Nov · snapshot 14 Nov 2026 · fleet and spans simulated`.
  - `notes:` string in the same slide block (repeats every number above).

## 2. Window & snapshot

- Issue window = first to last `createdAt` of matching issues (helper `issues.window`). Do not pick a calendar window; the workflow files issues irregularly.
- Benchmark window = first to last snapshot date (helper `benchmarks.first/last`).
- Snapshot = the day you run the queries (helper `snapshot`). Use `D Mon YYYY` in prose, `DD Mon` in FIXES.
- If new issues are still open, wait or report them; the funnel counts closed outcomes.

## 3. Sources

- Issues: `daily-cli-performance` files `[performance] Regression in <Benchmark>: N% slower`, label `cookie`.
- Assignment: `ASSIGNED_EVENT` timeline items whose assignee login matches `copilot`.
- Issue Monster pick: issue comment containing `Issue Monster selected this for Copilot`.
- Fixes: `closedByPullRequestsReferences(includeClosedPrs:true)`, merged, author login matching `copilot`; earliest merge per issue. Hours = issue `createdAt` → PR `mergedAt`.
- Benchmarks: `benchmark_history.jsonl` at the root of branch `memory/cli-performance` (repo memory of `daily-cli-performance`).

## 4. Queries

Run the read-only helper (gh GraphQL + git, ~10 s, no writes, no secrets):

```bash
.github/skills/cracktro/refresh/research-assign-observe.sh > /tmp/rob.json   # optional arg: owner/repo
jq 'del(.issues.fixes)' /tmp/rob.json
```

Output shape:

```json
{ "snapshot": "YYYY-MM-DD", "repo": "github/gh-aw",
  "issues": { "window": {"first_filed","last_filed"}, "filed", "open", "closed_completed",
    "closed_not_planned", "labelled_cookie", "assigned_copilot", "issue_monster_picked",
    "fixes_merged", "median_hours", "within_24h",
    "fixes": [{"issue","filed","title","pr","mergedAt","hours"}] },
  "benchmarks": { "snapshot_dates", "first", "last", "distinct_commit_days" } }
```

Baseline (deck, 14 Nov) vs helper (run 14 Nov 2026):

| Field | Deck | Helper |
|---|---|---|
| filed / completed / not planned / cookie | 116 / 62 / 54 / – | 116 / 62 / 54 / 116 |
| assigned_copilot | 101 | 101 |
| issue_monster_picked | 72 | 72 |
| fixes_merged / median_hours / within_24h | 62 / 6.8 / 55 | 62 / 6.8 / 55 |
| benchmark snapshots | 101, 5 Apr – 13 Nov | 101, 5 Apr – 13 Nov |

Manual cross-checks:

```bash
gh search issues --repo github/gh-aw '"[performance] Regression in"' in:title --limit 300 --json number,title,state \
  | jq '[.[]|select(.title|startswith("[performance] Regression in"))]|length'
# independent assigned_copilot count via REST (~2 min); must equal the helper
gh search issues --repo github/gh-aw '"[performance] Regression in"' in:title --limit 300 --json number,title \
  -q '.[]|select(.title|startswith("[performance] Regression in"))|.number' \
  | while read -r n; do gh api "repos/github/gh-aw/issues/$n/timeline" --paginate \
      -q '.[]|select(.event=="assigned")|.assignee.login' | grep -qi copilot && echo "$n"; done | wc -l
```

Pick FIXES rows from `issues.fixes`: 5 rows spread across the window, sorted by date, distinct or large slowdowns. Benchmark name = title text after `Regression in ` minus `Workflow`/`FromFile` suffixes as space allows; slowdown = `N% slower` rounded to `+N%`.

## 5. Update steps

1. Update `FUNNEL` counts (keep labels and the `" m"` class on the Issue Monster row) and the `/ 116` divisor.
2. Replace `FIXES` with 5 new rows; keep the `["DD Mon", "Bench", "+N%", "H.H h"]` format.
3. Update the subtitle window, the median/within-a-day line, the comment above `const RX`, and the caption (issue window, benchmark count and window, snapshot date, `fleet and spans simulated`).
4. Update `notes:` with the same numbers and windows. Keep the sentence `Illustrative: the production fleet, its numbers and the span stream are simulated in the browser.` so notes still match `/illustrative|synthetic|simulated/i`.
5. Update the `Research · Assign · Observe slide (22, rob- classes)` Exception bullet in `.github/skills/cracktro/content-rules.md` (window, counts, median, snapshots).
6. Do not add issue/PR numbers, SHAs or `github/<repo>` text to deck.js.

## 6. Validation

```bash
node --check docs/public/cracktro/deck.js
grep -c 'Research · Assign · Observe' docs/tests/talk.spec.ts   # heading unchanged → 1
cd docs && npx playwright test tests/talk.spec.ts                 # needs npm ci; say so if not run
```

Preview `http://127.0.0.1:4329/cracktro/#research-assign-observe` (see SKILL.md "Preview loop"): panel bars fit inside the card, the longest FIXES row does not overlap the `+N%` column, the caption fits on one line at 1920 px, and rows highlight in turn.

## 7. Pitfalls

- Count only issues with an `assigned` event to a Copilot login. On 14 Nov 2026, 15 of 116 had none (two were only cross-referenced). GraphQL and the REST timeline both gave 101; an earlier 105 did not reproduce. Cross-check with the REST loop in section 4 before changing this row.
- Benchmark history mixes pretty-printed and one-line versions. Parsing blobs line by line drops entries; that bug gave the earlier 87 snapshots starting 23 Apr. The helper reads `"date"` from added diff lines, giving 101 dates. A stale copy at `memory/cli-performance/benchmark_history.jsonl` on the same branch stops in early April; ignore it.
- `gh search` matches 118 issues; always filter on the exact title prefix.
- Search with `in:title` caps at 1,000 results; fine here, but paginate.
- `stateReason` NOT_PLANNED issues are usually superseded duplicates; they still count as filed.
- Some issues are closed by several PRs; use the earliest merged Copilot PR only.
- Keep the panel at 4 FUNNEL rows and 5 FIXES rows; geometry (y 224 + i·27, 382 + i·19, card height 306) is hard-coded.
- The FIXES `tick` index uses `FIXES.length`; no change needed if the count stays 5.
- Several slides reuse ids and helpers; include `rob`/`RX` context in `old_str` edits.
