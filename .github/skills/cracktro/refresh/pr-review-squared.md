---
name: cracktro-refresh-pr-review-squared
description: Recompute the measured data on the cracktro "PR Review²" slide and update deck.js.
---

# Refresh "PR Review²"

Load `.github/skills/cracktro/SKILL.md` first. Never read `docs/public/cracktro/deck.js` whole; `grep -n`, then `view_range`.

## Workflows mined

| Workflow file | Repo | Role in the slide's flow | Signal mined |
|---|---|---|---|
| `.github/workflows/mattpocock-skills-reviewer.md` | github/gh-aw | agentic reviewer on ready for review | reviews with footer `🧠 *Reviewed using Matt Pocock` |
| `.github/workflows/pr-code-quality-reviewer.md` | github/gh-aw | agentic reviewer on ready for review | reviews with footer `🔎 *Code quality review by` |
| `.github/workflows/test-quality-sentinel.md` | github/gh-aw | agentic reviewer on ready for review | reviews with footer `🧪 *Test quality analysis by` |
| `.github/workflows/pr-sous-chef.md` | github/gh-aw | nudges Copilot to fix open threads | `@copilot` comments with marker `gh-aw-workflow-call-id: github/gh-aw/pr-sous-chef` |
| Copilot coding agent (not a workflow) | github/gh-aw | pushes fixes, triggering another review round | PR commits with first author `Copilot` after the first nudge |

## 1. Slide

- Marker: `// PR Review²: the classic review gains a ready-for-review step` (`grep -n "// PR Review²" docs/public/cracktro/deck.js`).
- CSS prefix: `prq-` (strip: `prq-card`, `prq-cap`, `prq-stat`, `prq-v`, `prq-sl`). Group `process`.
- Data lives in three places inside that slide block only:
  1. `const STATS = [ ... ]`: six `[edgeId, value, [label1, label2]]` rows. Keep edge ids and order (`p1, p1, p2, p3, p4, p6`).
     - `"938"`, `"of 1,263 PRs merged"`, `"in 30 days checked"`
     - `"1,337"` agentic reviews · `"1,612"` inline review threads
     - `"517"`, `"on 336 PRs"` · `"965"`, `"on 305 PRs"` · `"1.8 h"` median ready → merge
     - Comment above it: `// measured on gh-aw merged PRs, 15 Oct to 13 Nov 2026; ...`
  2. Caption `<text class="prq-cap" ...>MEASURED · only PRs merged 15 Oct – 13 Nov 2026 (30 days), snapshot 14 Nov · flow above is illustrative</text>`.
  3. `notes:` string starting `"Pull Request Review squared.`: repeats every number plus `660 of the 682 reviewed pull requests` (single round) and the window/snapshot dates.
- Elsewhere: `.github/skills/cracktro/content-rules.md` Exception bullet "the Pull Request Review² slide (`prq-` classes)". `1,263` also appears on the "Merge fast, unslop daily" slide (`mfu-big`); keep both consistent if the windows match.
- The open-thread counter in the flow is synthetic; do not touch it.

## 2. Window & snapshot

- Rolling 30 days of **merge date**, inclusive, ending the day before the snapshot (UTC).
- Roll forward: `END = snapshot − 1 day`, `START = END − 29 days`. Snapshot = the day you run the query.
- Baseline: START 2026-10-15, END 2026-11-13, snapshot 2026-11-14.

## 3. Sources

- Repo `github/gh-aw`, merged PRs only.
- Reviewers (all trigger on `pull_request: ready_for_review`), identified by the review-body footer in their `.md`:
  - `.github/workflows/mattpocock-skills-reviewer.md` → `🧠 *Reviewed using Matt Pocock`
  - `.github/workflows/pr-code-quality-reviewer.md` → `🔎 *Code quality review by`
  - `.github/workflows/test-quality-sentinel.md` → `🧪 *Test quality analysis by`
- Nudges: `.github/workflows/pr-sous-chef.md`; PR comments (author `gh-aw-bot`) starting `@copilot` and containing `gh-aw-workflow-call-id: github/gh-aw/pr-sous-chef`.
- Fixes: PR commits whose first author login is `Copilot` and `committedDate` is after the PR's first nudge.

## 4. Queries

Run the read-only helper (needs `gh auth status` OK; ~4 min, ~45 GraphQL calls):

```bash
node .github/skills/cracktro/refresh/pr-review-squared.mjs START END > /tmp/prq.json
# no args = rolling 30 days ending yesterday UTC; RAW=/tmp/prq-raw.json also dumps raw PR nodes
```

It searches `repo:github/gh-aw is:pr is:merged merged:<3-day window>` (stays far below the 1,000-result search cap) and per PR pulls reviews(60), comments(100), reviewThreads(100, first comment's review id), commits(last 100), last `READY_FOR_REVIEW_EVENT`.

Output shape → slide field:

| JSON | Definition | Baseline (14 Nov) | Slide |
|---|---|---|---|
| `merged` | merged PRs in window | 1263 | `of 1,263 PRs merged` |
| `checked` | footer review **or** `github-actions` "completed" comment naming a reviewer | 931 (slide 938) | STATS[0] |
| `reviews.total` (`quality`/`skills`/`test`) | footer reviews | 1337 (662/656/19) | STATS[1] |
| `threads` / `threadsResolved` | threads whose first comment belongs to a footer review | 1612 / 696 | STATS[2] |
| `nudges` / `nudgedPRs` | Sous Chef `@copilot` comments / PRs | 517 / 336 | STATS[3] |
| `fixCommits` / `fixedPRs` | Copilot commits after first nudge / PRs | 989 / 305 (slide 965) | STATS[4] |
| `readyToMergeHours.median` (`n`) | last ready-for-review → merge, footer-reviewed PRs | 1.8 (678) | STATS[5] |
| `reviewedPRs` / `singleRound` | PRs with a footer review / ≤1 review per reviewer | 682 / 660 | notes |
| `truncated` | PR numbers that hit a page cap | 5 PRs | check only |

`checked` and `fixCommits` differ from the slide's original mining (938, 965); the original definitions were not recorded. Use the script's definitions going forward and say so in the PR.

## 5. Update steps

1. Run the query for the new window; save `/tmp/prq.json`.
2. Edit `STATS` values and label numbers (`of N PRs merged`, `on N PRs`). Format: thousands comma (`1,337`), hours one decimal + ` h`.
3. Edit the `STATS` comment dates and the `prq-cap` caption: `MEASURED · only PRs merged D Mon – D Mon YYYY (30 days), snapshot D Mon · flow above is illustrative`. Keep it on one line; it must fit the 1700-wide card.
4. Rewrite every number and date in `notes`. Keep the words "illustrative" and "synthetic" (must match `/illustrative|synthetic|simulated/i`). Keep the method sentence (footers, workflow marker).
5. Update the content-rules.md Exception bullet for this slide (window and all six counts).
6. If the "Merge fast, unslop daily" slide uses the same window, sync its `1,263` / per-day figures or note the mismatch.

## 6. Validation

```bash
node --check docs/public/cracktro/deck.js
python3 -m http.server 4329 --bind 127.0.0.1 --directory docs/public   # async + detached
# browser canvas: http://127.0.0.1:4329/cracktro/#pull-request-review-squared → step to the end, screenshot the strip
grep -nE '\b[0-9a-f]{40}\b|https?://' docs/public/cracktro/deck.js | grep -v 'introduction/overview'   # must be empty
cd docs && npx playwright test tests/talk.spec.ts                       # if node_modules exists
```

- Strip: six values align on the 280 px grid; no overlap with the caption.
- Content rules: no 40-hex SHAs, no URLs (except the allowed overview link), no people or logins, no PR numbers in the slide.
- Stop the preview server afterwards.

## 7. Pitfalls

- `merged:A..B` is inclusive; windows step 3 days, so the last window is clipped to END.
- Copilot co-author trailers (`Co-authored-by: Copilot`) make `Copilot` a non-first author; only count first-author `Copilot` commits.
- Reviewer workflows also post run-started/completed **comments**; count reviews by footer only, never by comment.
- Other `github-actions` reviewers (Ponytail, spec reviewer) open threads too; link threads to footer reviews by `pullRequestReview.id`, not by author.
- Footers can change: re-read each workflow's `footer:` before running; update `REVIEWERS` in the script if it changed.
- `truncated` PRs may undercount; spot-check them if the list grows.
- GraphQL rate limits: the script retries 3 times with backoff; rerun if it still fails.
- Link the slide by its stable id `#pull-request-review-squared` and find it in `deck.js` by its marker.
