---
name: cracktro-refresh-explore-reuse-lint
description: Recompute the measured data on the cracktro "Explore · Reuse · Lint" slide and update deck.js.
---

# Refresh: Explore · Reuse · Lint

Recompute → compare with the baseline → edit constants → validate. Never read `deck.js` whole.

## Workflows mined

| Workflow file | Repo | Role in the slide's flow | Signal mined |
|---|---|---|---|
| `.github/workflows/sergo.md` | gh-aw | Explorer: daily Go static analysis with a strategy cache | repo-memory branch `memory/sergo` (`sergo-stats.json`, `sergo-strategies.jsonl`); issues and PRs labelled `sergo` |
| Copilot coding agent (not a workflow) | gh-aw | Fixer | merged PRs closing `sergo` issues |

## 1. Slide

- Heading `Explore · Reuse · Lint` (AW factory band, `group: "data"`). Find it with `grep -n "// Explore → Reuse → Lint" docs/public/cracktro/deck.js`.
- CSS prefixes: `exr-` for the illustrative grid (do not touch it) and `exm-` for the measured strip (the `.exm-svg` bottom panel).
- The data lives in the slide block after `svg.innerHTML = g;`:
  - `FPR`: `[month, findingsPerRun]`. Bar height is `v * 12`, so 0 means no runs that month (shown as `·`).
  - `PRS`: `[dayOffset, kind]`. The offset is from 28 Oct 2026. Kind `v` = cached `paren_unwrap_gap` vein, `o` = other merged, `x` = closed unmerged. x = `480 + d*30`, so the maximum offset that fits is ~17.
  - `CACHE`: `[MM-DD, cached strategy, new strategy]`, 3 short rows.
  - `STATS`: `[number, label, window]`, 4 entries in a 2×2 grid.
  - String literals:
    - `FINDINGS PER RUN · 6 MAY – 14 NOV`
    - The vein title `ONE CACHED VEIN · paren_unwrap_gap` with axis labels `28 Oct`/`14 Nov`
    - The legend `● 13 merged from the vein  ● 4 other  ○ closed`
    - The caption `MEASURED · Sergo repo memory + gh-aw issues/PRs labelled sergo · 16 Apr – 14 Nov 2026 (213 days), snapshot 14 Nov 2026 · grid is illustrative`
  - The `meas` `aria-label` and the slide `notes` repeat these numbers and windows.

## 2. Window & snapshot

Current values, with snapshot 2026-11-14:

| Panel | Window | Policy |
|---|---|---|
| Findings per run | 6 May – 14 Nov | all-time (strategy-cache span); append months, max ~8 bars |
| Vein timeline | 28 Oct – 14 Nov | fixed; pick the latest dominant cached strategy, 18-day axis |
| runs · findings | 6 May – 14 Nov | all-time (`sergo-stats.json`) |
| issues filed | 16 Apr – 29 Oct | all-time (label `sergo`; Sergo stopped filing issues on 29 Oct) |
| fix PRs merged | 16 Apr – 30 Oct | all-time (merged PRs closing those issues) |
| own PRs merged | 28 Oct – 14 Nov | all-time (PRs labelled `sergo`) |

Caption window = earliest start → snapshot date, with the day count (both ends inclusive: `(end - start)/86400000 + 1`; 16 Apr → 14 Nov = 213).

## 3. Sources

- Workflow: `.github/workflows/sergo.md` (daily, Go static-analysis agent with a strategy cache).
- Repo `github/gh-aw`:
  - Repo-memory branch `memory/sergo`: `sergo-stats.json` (`total_runs`, `total_findings`) and `sergo-strategies.jsonl` (`date, strategy, findings, tasks_created, success_score, notes`).
  - Issues and PRs labelled `sergo`.

## 4. Queries

The fastest route is the read-only helper, which needs git with `origin` and an authenticated `gh` (~25 s):

```bash
node .github/skills/cracktro/refresh/explore-reuse-lint.mjs > /tmp/erl.json
```

Its output keys:

- `snapshot`
- `repo_memory{total_runs,total_findings}`
- `strategy_cache{entries,dates,window}`
- `findings_per_run_by_month{YYYY-MM:avg}`
- `latest_cache_entries[]`
- `issues{total,completed,open,window}`
- `fix_prs{merged,issues_closed,by_copilot,lines,window}`
- `own_prs{total,merged,window,merged_list[{merged,branch}]}`

The equivalent manual queries:

```bash
git fetch origin memory/sergo
git show origin/memory/sergo:sergo-stats.json | jq '{total_runs,total_findings}'
# union every revision of the strategy cache (runs rewrite/trim it)
for r in $(git rev-list origin/memory/sergo); do git show $r:sergo-strategies.jsonl 2>/dev/null; done \
  | jq -sc 'unique_by(.date+.strategy) | group_by(.date[0:7]) | map({m:.[0].date[0:7], fpr:((map(.findings)|add)/length*10|round/10)})'
gh issue list -R github/gh-aw --label sergo --state all --limit 2000 --json number,createdAt,state,stateReason
gh pr list -R github/gh-aw --label sergo --state all --limit 500 --json number,createdAt,mergedAt,state,headRefName
# closers: GraphQL issue(number:N){closedByPullRequestsReferences(first:10,includeClosedPrs:false){nodes{number merged mergedAt additions deletions author{login}}}}
```

Sanity baseline (2026-11-14):

- 128 runs, 667 findings.
- Strategy cache: 145 entries over 126 dates, 2026-05-06 → 2026-11-14.
- Findings per run: May 8.4 (slide 8.3), Jun 5.9, Jul 5.8, Aug none, Sep 4.5, Oct 3.4 (slide 3.3), Nov 3.7. The ±0.1 drift comes from the dedupe key; keep the slide's values unless a new month is added.
- Issues: 287 (192 completed, 0 open), 2026-04-16 → 2026-10-29.
- Fix PRs: 164 merged PRs closed 172 issues; 158 by Copilot; 22,728 lines; 2026-04-16 → 2026-10-30.
- Own PRs: 18 (17 merged), 2026-10-28 → 2026-11-14. Of the merged PRs, 13 have a `paren-unwrap` branch.

## 5. Update steps

1. `FPR`: add the new month(s) and drop the oldest when there are more than 8 bars. Update the `FINDINGS PER RUN · <start> – <end>` title.
2. `STATS`: update the numbers and each window string. Keep 4 entries, with labels of 22 characters or fewer.
3. Vein panel:
   - If a new cached strategy dominates `merged_list` (branches sharing a stem), rebuild `PRS` from 28 Oct, or from a new start date with the axis labels updated, and keep it within 18 days.
   - Update the legend counts and the vein name.
   - Refresh `CACHE` from the `cached:`/`new:` parts of `latest_cache_entries[].strategy`, shortened to ≤ 24 chars each.
4. Caption: set the overall window, the day count and the snapshot date. Keep the `MEASURED ·` prefix and `· grid is illustrative`.
5. `meas` `aria-label`: update the numbers and dates.
6. Notes: update the numbers, the overall window and the per-stat windows. Keep the sentence saying the grid/loop is illustrative, so the notes still match `/illustrative|synthetic|simulated/i`.
7. `.github/skills/cracktro/content-rules.md`: update the Explore · Reuse · Lint Exception bullet (numbers and window).

## 6. Validation

- Run `node --check docs/public/cracktro/deck.js`.
- Preview the slide: run `python3 -m http.server 4331 --bind 127.0.0.1 --directory docs/public` (async), open `http://127.0.0.1:4331/cracktro/#explore-reuse-lint` in the browser canvas, and take a screenshot. Check that nothing overlaps and that the bars fit in 0–150 px (`v*12`; rescale if any value is over 12).
- Content rules: no 40-hex SHAs (branch suffixes are 16-hex, but do not render branches), no URLs, no `github/<repo>` strings in `deck.js`, and only agent names (Sergo, Copilot), never people.
- Run `cd docs && npx playwright test tests/talk.spec.ts`. If the hardcoded chromium path or `AGENTICS_WORKFLOWS_DIR` blocks it, use a local config, or say the tests were not run.
- Stop the preview server.

## 7. Pitfalls

- **Label vs title:** match on the label `sergo`, not the `[sergo]` title prefix; other agents quote "sergo" in titles.
- **Strategy cache rewrites:** runs rewrite and trim `sergo-strategies.jsonl`, so always union all revisions. `rev-list` is about 130 revisions and growing.
- **Repo-memory fetch:** `memory/sergo` is not fetched by default; fetch it explicitly or the script fails.
- **August gap:** no runs were cached in August, so keep the `Aug` 0 slot (or drop it with a note), not an average.
- **Pagination:** `gh ... --limit` silently caps the results, so raise it if a total equals the limit. GraphQL closers are capped at `first:10` per issue.
- **Squash-merge attribution:** fix PRs are mostly authored by `copilot-swe-agent` and merged by maintainers, so count by PR author and never name the mergers. `closedByPullRequestsReferences` misses PRs that closed an issue without a closing keyword, so treat the count as a lower bound.
- **Rate limits:** the script makes about 6 GraphQL calls per 300 issues. On `secondary rate limit`, wait 60 s and rerun; do not parallelise.
- **Dates:** `createdAt` and `mergedAt` are UTC, so a late-evening PR can land on the next day.
