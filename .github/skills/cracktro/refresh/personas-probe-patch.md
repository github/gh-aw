---
name: cracktro-refresh-personas-probe-patch
description: Recompute the measured data on the cracktro "Personas · Probe · Patch" slide and update deck.js.
---

# Refresh: Personas · Probe · Patch

Recompute with `personas-probe-patch.mjs`, then patch `deck.js`. Read-only on GitHub; edit only the files listed in step 5.

## Workflows mined

| Workflow file | Repo | Role in the slide's flow | Signal mined |
|---|---|---|---|
| `.github/workflows/agent-persona-explorer.md` | gh-aw | Explorer: role-plays personas, scores the `agentic-workflows` agent, runs the 8,000-byte prefix probe, files one report | `Agent Persona Exploration - ` issues (label `agent-research`); their closing PRs, classified by the files they change |
| `.github/agents/agentic-workflows.md` | gh-aw | The agent under test | File size in bytes (the probe reads the first 8,000) |

## 1. Slide

- Heading: **Personas · Probe · Patch** (`group: "data"`, AW factory band).
- Marker: `grep -n "// Personas · Probe · Patch" docs/public/cracktro/deck.js`, then use `view_range`. Never read deck.js whole.
- CSS prefix: `ppx-` (persona chips, guidance file, cut line, rule, stamps, timeline colours). The strip reuses `.dhm`, `.dhm-k`, `.dhm-tl` and `.dhm-cap` from Docs that heal; panels reuse `sdd-`.
- Data lives in the slide block:
  - `const DAYS = [...]`: one entry per report issue, as days since the first report (repeats allowed).
  - `const CLS = "gxo…"`: one character per report, in `DAYS` order (bars are classified by index, so repeated days can differ). `g` = closed by a merged PR that changed agent guidance, `o` = merged PR elsewhere, `x` = no merged PR (drawn grey).
  - `TX = d => 10 + (d * (TW - 20)) / 160`: `160` is the axis span in days. Set it to `max(day)`.
  - Month ticks: `[[8,"Jun"],[38,"Jul"],[69,"Aug"],[100,"Sep"],[130,"Oct"],[155,"Nov"]]`, day offsets near mid-month (`Jun` sits right of the first bar).
  - Timeline title: `"133 REPORTS · 23 GUIDANCE · 2 OTHER FIXES"`.
  - KPIs: `kpi("133","daily reports",…)`, `kpi("25","fixed by",…"22 by Copilot")`, `kpi("23","guidance",…,"g")`, `kpi("17,035 B","agent file",…,"r")`.
  - Caption (`dhm-cap`): `"MEASURED · snapshot 15 Nov 2026 · reports 8 Jun–15 Nov 2026 (160 d) · loop illustrative"`.
  - `notes:` contains the sentence starting "The bottom strip is measured on gh-aw (snapshot …)".
- The loop is illustrative. `triggers.md · 16 KB`, the cut (`CUT`) and the rule (`RULE`) are drawn to scale from the file at the snapshot (rule heading at byte 11,225 of 16,120). Only re-scale them if you change those labels.

## 2. Window & snapshot

- **All-time since issues.** Until 7 Jun 2026 the workflow posted discussions; the window starts at the first issue report (8 Jun 2026) and ends at the snapshot date. Roll it forward by passing today's date; the start stays fixed.
- Day counts are differences of UTC dates. Current value: 160 d (the newest report falls on the snapshot day).
- **Cutoff:** everything is measured as of the end of the snapshot day (UTC). PRs merged later do not count as fixes or in author totals, and an issue counts as open unless its `closedAt` is before the cutoff, so re-running an old snapshot gives the same numbers.

## 3. Sources

- Repository: gh-aw (public).
- Workflow: `.github/workflows/agent-persona-explorer.md`, safe output `create-issue` with title prefix `Agent Persona Exploration - `, labels `agent-research`, `cookie`, `close-older-issues: true`.
- Agent file: `.github/agents/agentic-workflows.md` in the current checkout (pull `main` first).
- The persona count comes from the numbered `**Role** - …` list in the workflow.

## 4. Queries

```bash
node .github/skills/cracktro/refresh/personas-probe-patch.mjs 2026-11-15 > /tmp/ppx.json   # omit date = today; ~1 min; run from the repo root
```

Output shape:

```json
{ "snapshot": "YYYY-MM-DD", "window": [first,end], "days", "reports", "fixed",
  "guidance", "other", "noMergedPr", "open", "mergedPrs", "copilotPrs", "botPrs",
  "personas", "agentBytes", "REPORTS": [[day,"g|o|x|open"]...],
  "deck": "const DAYS = [...];\nconst CLS = \"...\";", "detail": [...] }
```

The script's underlying queries (`REPO` env var, default gh-aw):

1. Reports, keeping only titles that start with `Agent Persona Exploration - `:
   `gh issue list --state all --limit 1000 --label agent-research --search 'created:<=END' --json number,title,createdAt,closedAt,stateReason,closedByPullRequestsReferences`
2. For each closing PR: `gh pr view N --json number,author,mergedAt,files`. Only PRs merged before the cutoff count. The class is `g` if any file matches `^\.github/(aw|agents|skills/agentic-workflows)/`, else `o`.

Sanity baseline at the 2026-11-15 snapshot:

| Item | Value |
|---|---|
| reports | 133, 0 open, 2026-06-08 → 2026-11-15 |
| fixed by a merged PR | 25 (23 `g`, 2 `o`) |
| merged PRs / by Copilot / by bot | 25 / 22 / 1 |
| no merged PR | 108 (all NOT_PLANNED: replaced by the next report) |
| personas | 9 |
| agent file | 17,035 B (`Buffer.byteLength`, UTF-8) |

If the new numbers are lower than the baseline, suspect the query, not the data: check the label, the title filter and pagination first.

## 5. Update steps

1. **KPIs:** set the four `kpi()` values from `reports`, `fixed` (with `copilotPrs`), `guidance` and `agentBytes`.
2. **Timeline:** replace `DAYS` and `CLS` with the two lines in `deck` (open reports are emitted as `x`); update the `160` axis span in `TX` and the month ticks (day offsets from `window[0]`, about 6 labels). Update the title counts from `reports`, `guidance` and `other`.
3. **Caption:** update the snapshot date, the window end and its day count. Keep "MEASURED ·" and "loop illustrative", and keep the caption ending left of the sound button.
4. **Notes:** update the snapshot date, the window, the persona count, the agent-file size and every number in the measured sentence. Keep "…illustrative, simplified example…" so the notes still match `/illustrative|synthetic|simulated/i`.
5. **`.github/skills/cracktro/content-rules.md`:** update the numbers and dates in the "Personas · Probe · Patch slide" Exception bullet. Do not change other bullets.
6. **Guide row slide `persona-optimization`** (`gdp-` classes): update its KPI values, the prefix bar (`17,035 B`, percentage), the caption snapshot/window and the notes, plus its "Personas test the guide" Exception bullet in `content-rules.md`.

## 6. Validation

```bash
node --check docs/public/cracktro/deck.js
python3 -m http.server 4339 --bind 127.0.0.1 --directory docs/public   # async; pick a free port; open http://127.0.0.1:4339/cracktro/#personas-probe-patch
git diff -U0 docs/public/cracktro/deck.js | grep '^+' | grep -nE '[0-9a-f]{40}|github/[a-z]|https?://|who:'   # must print nothing
cd docs && npx playwright test tests/talk.spec.ts
```

- On screen, wait one full loop (~18 s): the rule must slide above the cut, `completeness` must turn red then green, and the Maintainer pill must not touch the strip.
- Use agent and role names only; never show people, issue or PR numbers, or URLs on the slide.
- Stop the server afterwards.

## 7. Pitfalls

- **Label is shared:** older `agent-research` issues and failure issues exist; always filter on the title prefix.
- **Discussions before June:** reports before 8 Jun 2026 were discussions and are not counted; do not move the window start.
- **Date filter:** `created:..END` returns nothing; use `created:<=END`.
- **Classify by files, not titles:** a docs-only fix (for example under `docs/`) counts as `o`, even when it answers a guidance finding.
- **Authors:** Copilot PRs are `app/copilot-swe-agent`; fixes by `gh-aw-bot` count in `botPrs`. Human authors are counted in `mergedPrs` only and are never named on the slide.
- **Merged but NOT_PLANNED:** a report can close as NOT_PLANNED and still have a merged closing PR; the script counts it as fixed.
- **Self-graded scores:** the reports' average scores shift as the rubric tightens; do not chart them as a quality trend.
