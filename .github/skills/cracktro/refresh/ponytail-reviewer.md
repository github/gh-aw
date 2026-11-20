---
name: cracktro-refresh-ponytail-reviewer
description: Recompute the measured data on the cracktro "Ponytail Reviewer" slide and update deck.js.
---

# Refresh "Ponytail Reviewer"

Load `.github/skills/cracktro/SKILL.md` first. Never read `docs/public/cracktro/deck.js` whole; `grep -n`, then `view_range`.

## Workflows mined

| Workflow file | Repo | Role in the slide | Signal mined |
|---|---|---|---|
| `.github/workflows/ponytail-reviewer.md` | github/gh-aw | complexity reviewer on ready for review, `/ponytail`, `/review` | reviews whose body contains `workflow_id: ponytail-reviewer`; their inline threads; `Lean: -N lines` / `net: -N lines` and `· N AIC` in the body |
| same workflow, Actions runs | github/gh-aw | run count, noop share, per-run AIC | runs API; `agent-output-fallback` artifact (`agent_output.json` item types, `agent_usage.json` `ai_credits`) |

Never name the author of the external `ponytail-review` skill (a personal handle) on the slide, in notes, in `slides.md` or here.

## 1. Slide

- Marker: `// Ponytail Reviewer (ready` (`grep -n "// Ponytail Reviewer (ready" docs/public/cracktro/deck.js`). CSS prefix `pnt-` (strip `pnt-m`, built on the shared `dhm` strip classes). Group `process`, `under: "prr"`. `#ponytail-reviewer` in `slides.md` (Notes `illustrative`: the diff, findings and verdict animation stay synthetic).
- Data lives inside that block only:
  1. `KINDS`: `[label, class, count]` for tagged findings (`yagni`, `shrink`, `delete`, `stdlib/native` summed) plus `untagged`.
  2. `AICM`: `[month, median AIC per review]` from review footers.
  3. The `kpi(...)` calls in `strip`: reviews / PRs, findings, noop share (`run sample`), acted share, lines claimed / median.
  4. Caption `MEASURED · snapshot 17 Nov 2026 · reviews 8 Aug–16 Nov 2026 (101 d) · run sample 19 Aug–16 Nov · diff and findings illustrative`.
  5. `notes:` from `The bottom strip is measured on gh-aw`: repeats every number and date and ends saying the animation is illustrative.
- Elsewhere: the `content-rules.md` Exception bullet "the Ponytail Reviewer slide (`pnt-` classes)".

## 2. Window & snapshot

- PRs **created**, reviews **submitted** and runs **created** from 2026-08-06 (two days before the first run) to END (default yesterday UTC); snapshot = run day. Thread state (resolved, outdated, replies) is read at the snapshot.
- Republish the baseline window with the explicit dates below; the script rejects anything but `YYYY-MM-DD`.
- Baseline: first run 8 Aug 2026, last counted review 16 Nov 2026 (101 days), snapshot 17 Nov 2026.
- Run sample: every 5th run of the window (newest first, any conclusion); only runs whose artifact has not expired have output (baseline: 19 Aug to 16 Nov 2026). State the sample window separately in the caption.

## 3. Sources

- Reviews: GraphQL PR search in 2-day windows; keep reviews whose body contains `workflow_id: ponytail-reviewer`. All are `COMMENTED`.
- Findings: review threads whose first comment is in one of those reviews. Kind = first `yagni:`/`shrink:`/`delete:`/`stdlib:`/`native:` tag in the first 260 chars (scanning the whole body finds no more). `untagged` is a format proxy, not a kind: almost all are November prose findings ("What this is / Problem / Fix") and mix every kind.
- Acted = thread resolved **or** outdated (code changed under it) **or** replied to (replies are mostly the Copilot coding agent applying the fix).
- Lines claimed: `Lean: -N lines` or `net: -N lines` in the review body; median over reviews that state one.
- AIC: `· N AIC` in the review footer; median per month. Models change monthly (`modelByMonth`), which explains most of the AIC swings; say so in notes, not on the slide.
- Noop share: `classify()` sorts each sampled run's `agent_output.json`: `review` (any review item), `lean` (noop saying "Lean already"), `clean` (other nothing-to-cut noop), `skip` (noop for missing or empty pre-fetched input), `other`. Share = (lean + clean) / (lean + clean + review); `skip`, `other`, `noArtifact` (expired) are excluded. `downloadErrors` lists failed run IDs and exits 1: rerun, never publish a partial sample. `--self-test` checks the classifier offline.
- Medians average the two middle values for even counts.

## 4. Queries

Read-only; `gh auth status` must pass. ~10 min for PRs and runs; the sample adds ~10 min per 500 runs.

```bash
node .github/skills/cracktro/refresh/ponytail-reviewer.mjs --self-test
SAMPLE=5 node .github/skills/cracktro/refresh/ponytail-reviewer.mjs 2026-08-06 2026-11-16 > "$TMPDIR/pnt.json"   # END: last full day
```

| JSON | Baseline | Slide |
|---|---|---|
| `runs.total` (`byEvent`, `byConclusion`) | 2928 (2920 PR, 8 dispatch; 2289 success, 624 failure) | notes |
| `prsScanned` / `reviewedPRs` / `reviewedMerged` | 5344 / 467 / 424 | KPI `on 467 PRs`, notes |
| `reviews.total` / `byMonth` | 469 / Aug 174, Sep 69, Oct 105, Nov 121 | KPI `469` |
| `findings.total` / `perPR` | 580 / 0:35, 1:319, 2:88, 3:18, 4:4, 5:3 | KPI `580`, notes (35 PRs without inline findings) |
| `findings.kinds` | yagni 203, shrink 135, delete 68, stdlib 10 + native 6, untagged 158 | `KINDS` |
| `findings.acted` (`resolved`, `outdated`, `replied`, `copilotReplied`) | 332 = 57% (118, 227, 283, 215) | KPI `57%` |
| `linesClaimed` (`reviews`, `total`, `median`) | 446, 10219, 12 | KPI `10,219`, `median 12` |
| `aicPerReview.medianByMonth` | Aug 19.95, Sep 5.19, Oct 11.6, Nov 5 | `AICM` (one decimal; Aug shown as 20) |
| `sample` (`every` 5) `noopShare` (`lean`+`clean` / `verdicts`, `withOutput`) | 0.789 (297+2 of 379, 421 of 585; 164 expired) | KPI `79%` |
| `sample.aicMedianPerRun` | ~5.7 | notes only |

## 5. Update steps

1. Run the query; keep `$TMPDIR/pnt.json`.
2. Update `KINDS`, `AICM` (append a month; keep at most five bars so the SVG stays 190 px wide), and the five `kpi(...)` values.
3. Update the caption dates and day count, every number in `notes`, and the content-rules.md Exception bullet.
4. If the noop share is under 50%, swap the KPI colour class (`g`) for the default; never change the illustrative verdict text.
5. Refresh the repo widget's default stars (`repoWidget(1266797999, …)`) from `gh api repositories/1266797999 --jq .stargazers_count` and its date in the content-rules bullet; the slide fetches the live count on https.

## 6. Validation

```bash
node --check docs/public/cracktro/deck.js
python3 -m http.server 4369 --bind 127.0.0.1 --directory docs/public   # async
# browser canvas: http://127.0.0.1:4369/cracktro/#ponytail-reviewer, screenshot after ~3 s (findings) and ~10 s (end)
grep -nE '\b[0-9a-f]{40}\b|https?://' docs/public/cracktro/deck.js | grep -v 'introduction/overview\|agentics-beyond-code\|api.github.com/repositories/\|www.w3.org/2000/svg'   # must be empty
cd docs && npx playwright test tests/talk.spec.ts                       # if node_modules exists
```

- The strip must stay between x 470 (nav dots) and the chrome buttons (x ≥ 1600 at y ≥ 1000); the diff, cards and verdict must not overlap it.
- No PR numbers, logins or URLs on the slide or in notes. Stop the preview server afterwards.

## 7. Pitfalls

- The runs API `created=` filter caps at 1,000 results; the script pages without it and filters locally. Deep pages sometimes return 502: rerun.
- `safe-output-outcomes.jsonl` is missing from older runs; `agent_output.json` item types are the reliable outcome source.
- Artifacts expire after ~90 days, so the run sample window starts later than the review window.
- Bracket escaping and en/em dashes vary across months; the `LINES` and `AIC` regexes accept `-`, `−`, `–` and `\~`.
- The reviewer can post two reviews on one PR (ready for review, then `/ponytail`), so reviews > PRs is expected.
- Link the slide by its stable id `#ponytail-reviewer` and find it in `deck.js` by its marker.
