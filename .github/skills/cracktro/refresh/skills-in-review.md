---
name: cracktro-refresh-skills-in-review
description: Recompute the measured data on the cracktro "Skills in review" slide and update deck.js.
---

# Refresh "Skills in review"

Load `.github/skills/cracktro/SKILL.md` first. Never read `docs/public/cracktro/deck.js` whole; `grep -n`, then `view_range`.

## Workflows mined

| Workflow file | Repo | Role in the slide | Signal mined |
|---|---|---|---|
| `.github/workflows/mattpocock-skills-reviewer.md` | github/gh-aw | skills reviewer on ready for review | reviews with footer `🧠 *Reviewed using Matt Pocock`; inline comments tagged `**[/skill]**` |
| same workflow, run artifacts | github/gh-aw | OTel panel | `skill.*` events in `usage/aw_session.jsonl` (the unified session that `send_otlp_session_logs.cjs` exports as OTLP logs, attribute `gh-aw.session.event_type`) |

## 1. Slide

- Marker: `// Skills in review:` (`grep -n "// Skills in review:" docs/public/cracktro/deck.js`). CSS prefix `sir-`. Group `engineering`. `#skills-in-review` in `slides.md` (no Notes: fully measured).
- Data lives inside that block only:
  1. `TAGS`: `[tag, colour, comments, retired]`; order is the stacking order of `WEEKS`.
  2. `WEEKS`: one row per week starting on the window start (Monday 4 May), values in `TAGS` order (`other` = everything else).
  3. `MS`: milestones `[days after 4 May, label, row, anchor]`, from git history (see Sources).
  4. `TOT` (reviews, comments, % of PRs reviewed), `NEXT` (outcomes), `VERD` (review states), `OTEL` (event counts).
  5. Card D window text `419 run sessions · 6–15 Nov 2026` and the caption `MEASURED · gh-aw PRs created 4 May – 15 Nov 2026 (196 days) · snapshot 16 Nov 2026 · ...`.
  6. `notes:` string starting `"Skills in review, measured:`: repeats every number and date.
- Elsewhere: the `content-rules.md` Exception bullet "the Skills in review slide (`sir-` classes)".

## 2. Window & snapshot

- PRs **created** from 2026-05-04 (workflow added) to yesterday UTC, inclusive; snapshot = run day.
- Baseline: 2026-05-04 → 2026-11-15, snapshot 2026-11-16 (196 days, 29 weeks; the last week is partial).
- OTel panel: the most recent ~10 days of reviewer runs (baseline 6–15 Nov 2026), and only runs after skill events existed (12 Oct 2026) and OTLP session logs (3 Nov 2026).

## 3. Sources

- Comments: review threads whose first comment belongs to a footer review (or a `github-actions` comment with a skill tag). Skill = first `**[/name]**` in the body.
- Outcomes per thread: `isResolved`, `isOutdated` (code changed after), replies (`totalCount > 1`), replies by a login matching `/copilot/i`.
- Milestones: `git log --follow --format='%ad %s' --date=short -- .github/workflows/mattpocock-skills-reviewer.md`, plus gh-aw features (`skills:` front matter 30 Jun, skill events 12 Oct, OTLP session logs 3 Nov, `git log -S`).

## 4. Queries

PRs and comments (read-only, `gh auth status` OK; ~10 min over six months):

```bash
node .github/skills/cracktro/refresh/skills-in-review.mjs 2026-05-04 END > /tmp/sir.json
# RAW=/tmp/sir-raw.json also dumps one row per comment {pr, skill, at, resolved, outdated, replies}
```

| JSON | Baseline | Slide |
|---|---|---|
| `prsScanned` / `reviewedPRs` | 11961 / 3877 | `TOT[2]` 32%, label `of 11,961` |
| `reviews.total` (`APPROVED`/`CHANGES_REQUESTED`/`COMMENTED`/`DISMISSED`) | 3954 (847/492/1969/646) | `TOT[0]`, `VERD` |
| `comments` / `medianPerPR` (`commentedPRs`, `maxPerPR`) | 7696 / 2 (2707, 14) | `TOT[1]` |
| `bySkill` | tdd 3220, codebase-design 1370, diagnosing-bugs 1168, grill-with-docs 694, zoom-out 475, diagnose 458, improve-codebase-architecture 262, rest 49 | `TAGS` |
| `weekly` (per skill, week index from START) | 29 weeks | `WEEKS` (transpose into `TAGS` order) |
| `replied` / `copilotReplied` / `outdated` / `resolved` / `acted` | 2423 / 1956 / 2071 / 1081 / 3540 | `NEXT` |
| `monthly` | Sep 306 comments (trough) | notes only |

OTel skill events (downloads artifacts; stops at the local storage limit, ~430 runs):

```bash
gh aw logs mattpocock-skills-reviewer --repo github/gh-aw --start-date S --end-date E -c 600 --json -o /tmp/sirlogs/out > /tmp/sirlogs/runs.json
cd /tmp/sirlogs/out
ls -d run-*/usage/aw_session.jsonl | wc -l                                   # run sessions (419)
grep -l '"type":"skill.inventory"' run-*/usage/aw_session.jsonl | wc -l     # 418
grep -h '"type":"skill.conflict"' run-*/usage/aw_session.jsonl | wc -l      # 2 (reporting, scope_collision)
grep -h '"type":"skill.invoked"' run-*/usage/aw_session.jsonl | wc -l       # 1
```

Use the actual run dates (`run.json` `createdAt`) for the window text, not the requested range.

## 5. Update steps

1. Run both queries; keep `/tmp/sir.json` and the grep counts.
2. Update `TAGS` counts, `WEEKS` (append weeks; keep `TAGS` order; fold new or rare tags into `other`), `TOT`, `NEXT`, `VERD`, `OTEL` and the OTel window text.
3. If the window grows past ~30 weeks, keep the chart width: `STEP` is `XW / WEEKS.length`; add a `MONTHS` entry and check labels still fit.
4. Add a milestone to `MS` only for a dated, verified change to the workflow or gh-aw skills support; keep labels short (two rows alternate).
5. Update the caption dates and day count, every number in `notes`, and the content-rules.md Exception bullet.

## 6. Validation

```bash
node --check docs/public/cracktro/deck.js
python3 -m http.server 4329 --bind 127.0.0.1 --directory docs/public   # async
# browser canvas: http://127.0.0.1:4329/cracktro/#skills-in-review, wait ~5 s for the sweep, screenshot .sir-svg
grep -nE '\b[0-9a-f]{40}\b|https?://' docs/public/cracktro/deck.js | grep -v 'introduction/overview\|agentics-beyond-code\|api.github.com/repositories/\|www.w3.org/2000/svg'   # must be empty
cd docs && npx playwright test tests/talk.spec.ts                       # if node_modules exists
```

- In the canvas, check every `.sir-svg text` bbox stays inside its `.sir-card` (8 px margin); milestone labels must not overlap.
- No PR numbers, logins or URLs on the slide or in notes. Stop the preview server afterwards.

## 7. Pitfalls

- Safe outputs escape brackets: tags appear as `**\[/tdd\]**`; the script's regex accepts both forms.
- Skill names changed on 29–30 Jun (`diagnose` → `diagnosing-bugs`, `zoom-out` → `codebase-design`); keep retired tags dimmed, don't merge them.
- `skill.invoked` and the usage-activity skill counts only fire for the CLI `skill` tool; this agent mostly reads `SKILL.md` with bash, so the comment tag is the usage signal. Don't present `skill.invoked` as usage.
- The September trough (16–91 comments/week, 31 Aug–27 Sep) has no workflow commit that explains it; don't invent a cause.
- Search caps at 1,000 results per query; the script searches 3-day windows (max ~250 PRs each). If a window nears 1,000, shrink it.
- Threads fetch the first 5 comments; `totalCount` still counts all replies, but the Copilot-reply check only sees the first four replies.
- The reviewer can post more than one review per PR (re-review on ready for review); reviews > PRs is expected.
- Link the slide by its stable id `#skills-in-review` and find it in `deck.js` by its marker.
