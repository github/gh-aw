---
name: cracktro-refresh-spec-driven-testing
description: Recompute the measured data on the cracktro "Spec-driven testing" slide and update deck.js.
---

# Refresh: Spec-driven testing

Plan only. Follow `.github/skills/cracktro/SKILL.md` (grep markers, never read deck.js whole).

## Workflows mined

| Workflow file | Repo | Role in the slide's flow | Signal mined |
|---|---|---|---|
| `.github/workflows/spec-extractor.md` | gh-aw | Writes package specs | `[spec-extractor]` PRs |
| `.github/workflows/spec-enforcer.md` | gh-aw | Adds spec tests | `[spec-enforcer]` PRs; `TestSpec_` count on main |
| `.github/workflows/spec-librarian.md` | gh-aw | Audits specs | `[spec-librarian]` issues |
| `.github/workflows/weekly-safe-outputs-spec-review.md` | gh-aw | Reviews the safe-outputs spec | `[spec-review]` PRs |
| Copilot coding agent (not a workflow) | gh-aw | Fixer for librarian audits | PRs on head branches `copilot/spec-librarian*` |

## 1. Slide

- Heading: "Spec-driven testing" (kicker: humans state intent, agents write the RFC spec…).
- Marker in `docs/public/cracktro/deck.js`: `// Spec-driven testing:`.
- CSS prefixes: `sdd-` (illustrative flow, do not touch), `sdm-` (measured strip + caption).
- Data holders (grep each):
  - `const SDM_STATS = [` — 4 cards `[workflow, number-html, label]`:
    - `["spec-extractor", "83", "README spec PRs merged"]`
    - `["spec-enforcer", "69", "test PRs · +12,731 lines"]`
    - `["spec-librarian", "68<i>→38</i>", "audits → merged fixes"]`
    - `["safe-outputs-spec-review", "22", "weekly spec PRs merged"]`
  - `const SDM_TESTS = [` — `[dayOffsetFrom1Apr, TestSpec_count]` pairs; last is `[227, 415]`.
  - `sdmX`/`sdmY` scale constants `227` (last day offset) and `415` (max), plus `sdmX(227)`, `sdmY(415)`, `>415<` and the `Aug`/`Nov` axis labels in the `spark.innerHTML` template.
  - Fifth card literals: `"0 → 415"`, `"48 spec_test.go"`.
  - Caption: `h("div", { class: "sdm-cap" }, … "all-time · 30 Mar – 14 Nov 2026 (229 days) · snapshot 14 Nov 2026 · …")`.
  - Comment above `SDM_STATS`: `// measured on gh-aw (snapshot 14 Nov 2026)…`.
  - Speaker notes: the `notes:` string of this slide (grep `measured on gh-aw's public history`).

## 2. Window & snapshot

- Current: all-time for these workflows, 30 Mar – 14 Nov 2026 (229 days), snapshot 14 Nov 2026.
- Roll forward: keep the start (30 Mar 2026, first `[spec-review]` PR); set end = snapshot = refresh date; recompute days as end − start (30 Mar → 14 Nov = 229).
- Sparkline: monthly points from 1 Apr 2026 plus the snapshot date. Add one point per new month; relabel axis (first/middle/last month).

## 3. Sources

- Repo: gh-aw (public).
- Workflows (`.github/workflows/`): `spec-extractor.md`, `spec-enforcer.md`, `spec-librarian.md`, `weekly-safe-outputs-spec-review.md` (title prefix `[spec-review]`).
- Copilot fix PRs for librarian audits: head branches `copilot/spec-librarian*`.
- Spec tests: `func TestSpec_` in `pkg/**/spec_test.go` on `origin/main`.

## 4. Queries

One-shot (read-only, prints JSON):

```bash
.github/skills/cracktro/refresh/spec-driven-testing.sh            # default SINCE=2026-04-01
```

Equivalent manual commands:

```bash
# PRs by exact title prefix (repeat for "[spec-enforcer]", "[spec-review]")
gh pr list -R github/gh-aw --state all --limit 1000 --search '"[spec-extractor]" in:title' \
  --json title,state,additions | jq '[.[]|select(.title|startswith("[spec-extractor]"))]
  | {total:length, merged:map(select(.state=="MERGED"))|length,
     merged_additions:(map(select(.state=="MERGED")|.additions)|add)}'
# Librarian audit issues
gh issue list -R github/gh-aw --state all --limit 1000 --search '"[spec-librarian]" in:title' \
  --json title | jq '[.[]|select(.title|startswith("[spec-librarian]"))]|length'
# Copilot fix PRs for librarian audits
gh pr list -R github/gh-aw --state all --limit 1000 --search 'head:copilot/spec-librarian' \
  --json state | jq '{total:length, merged:map(select(.state=="MERGED"))|length}'
# TestSpec_ on main at a date, and spec_test.go file count
git grep -h '^func TestSpec_' "$(git rev-list -1 --before=2026-11-01 origin/main)" -- 'pkg/**/*spec_test.go' | wc -l
git ls-tree -r --name-only origin/main pkg | grep -c 'spec_test\.go$'
```

Output shape and baseline (14 Nov 2026):

```json
{"spec_extractor":{"total":84,"merged":83},"spec_enforcer":{"total":72,"merged":69,"merged_additions":12731},
 "spec_review":{"total":23,"merged":22},"spec_librarian_issues":68,"librarian_fix_prs":{"total":41,"merged":38},
 "testspec_by_month":[["2026-04-01",0],["2026-05-01",225],["2026-06-01",296],["2026-07-01",355],["2026-08-01",374],
 ["2026-09-01",386],["2026-10-01",386],["2026-11-01",377],["2026-11-14",415]],"spec_test_files":48}
```

## 5. Update steps

1. Run the script; save the JSON in the session folder (not the repo).
2. `SDM_STATS`: extractor `merged`; enforcer `merged` + `merged_additions` (comma-grouped); librarian `issues<i>→merged fixes</i>`; spec-review `merged`.
3. `SDM_TESTS`: convert each date to day offset from 2026-04-01; replace `227`/`415` scale and label literals with the new last offset/max; update `"0 → N"` and `"F spec_test.go"`.
4. Caption: new end date, day count, snapshot date; keep `MEASURED`, `PRs/issues by title prefix`, `flow illustrative`. Must stay one line (`white-space: nowrap`, ends at x≈1810).
5. Comment above `SDM_STATS`: new snapshot date.
6. Notes: update every number, window and snapshot; keep the sentence saying the flow is an illustrative excerpt (must match `/illustrative|synthetic|simulated/i`).
7. `content-rules.md`: update the "Exception: the Spec-driven testing slide" bullet numbers and window.

## 6. Validation

- `node --check docs/public/cracktro/deck.js`.
- Preview: `python3 -m http.server 4341` in `docs/public` (background, record pid, `kill <pid>` after); open the browser canvas at `/cracktro/?v=<n>#spec-driven-testing`; wait ~22 s; screenshot. Check the strip and the one-line caption clear the dot minimap.
- `cd docs && npx playwright test tests/talk.spec.ts`.
- Content rules: no 40-hex SHAs, no URLs, no `github/<repo>` strings, no `who:`, no people's names — agent/workflow names only.

## 7. Pitfalls

- Title prefixes: `--search "…" in:title` is fuzzy; always filter with `startswith`. `[spec-review]` ≠ `[spec-extractor]`.
- `gh … list --limit 1000` caps search results at 1000; split by `created:` ranges if a count nears it.
- Squash merges: count PRs by title/head branch, not by commit author; `merged_additions` is the PR diff, not post-squash churn.
- Month-boundary samples use the last commit before the date; a rerun can shift by ±1 (June: 297 on slide vs 296 now). Prefer the rerun value and update both.
- `git grep` exits 1 on a revision with no matches (April = 0); the script guards it under `pipefail`.
- Rate limits: ~6 search calls per run; unauthenticated `gh` fails — run `gh auth status` first.
- macOS vs GNU `date`: the script tries `date -j -v+1m` then `date -d`.
