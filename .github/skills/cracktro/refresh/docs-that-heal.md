---
name: cracktro-refresh-docs-that-heal
description: Recompute the measured data on the cracktro "Docs that heal" slide and update deck.js.
---

# Refresh: Docs that heal

Recompute with `docs-that-heal.mjs`, then patch `deck.js`. Read-only on GitHub; edit only the files listed in step 5.

## Workflows mined

| Workflow file | Repo | Role in the slide's flow | Signal mined |
|---|---|---|---|
| `.github/workflows/daily-doc-updater.md` | gh-aw | Updater: edits docs to match code | `[docs]` PRs attributed by `gh-aw-workflow-id` marker or footer |
| `.github/workflows/unbloat-docs.md` | gh-aw | Unbloater | `[docs]` PRs attributed as above; net lines |
| `.github/workflows/glossary-maintainer.md` | gh-aw | Glossary | `[docs]` PRs attributed as above |
| `.github/workflows/daily-doc-healer.md` | gh-aw | Healer: fixes the updater's prompt | `[doc-healer]` issues and `[docs]` PRs; closing PRs that change `daily-doc-updater.md` |

## 1. Slide

- Heading: **Docs that heal** (`group: "data"`, AW factory band).
- Marker: `grep -n "// Docs that heal" docs/public/cracktro/deck.js`, then use `view_range`. Never read deck.js whole.
- CSS prefixes: `dh-` (the illustrative loop) and `dhm-` (the measured strip: `.dhm`, `.dhm-k`, `.dhm-tl`, `.dhm-cap`).
- Data lives in the slide block:
  - `const RULES = [[day, "u"|"h"|"x"], ...]`: one entry per `[doc-healer]` issue. `day` is days since the first issue; `u` means a merged PR changed the updater prompt, `h` merged elsewhere, `x` not merged.
  - `TX = d => 10 + (d * (TW - 20)) / 250`: `250` is the axis span in days. Set it to `max(day) + ~4`.
  - Month ticks: `[[3,"Mar"],[64,"May"],[125,"Jul"],[187,"Sep"],[248,"Nov"]]`, as day offsets from the first issue.
  - Timeline title: `"29 HEALER RULES · 13 MERGED · 9 IN UPDATER PROMPT"`.
  - KPIs: `kpi("189","daily-doc-updater",…)`, `kpi("250","unbloat-docs",…"−22,043 net lines")`, `kpi("146","glossary-maintainer",…)`, `kpi("64","daily-doc-healer",…,"g")`.
  - Caption (`dhm-cap`): `"MEASURED · snapshot 14 Nov 2026 · PRs 10 Oct 2025–14 Nov 2026 (400 d) · healer issues 26 Feb–30 Oct 2026 (246 d) · loop illustrative"`.
  - `notes:` contains the sentence starting "The bottom strip is measured on gh-aw (snapshot …)".

## 2. Window & snapshot

- **PR KPIs are all-time.** The window runs from the first agent PR (unbloat, 10 Oct 2025) to the snapshot date. Roll it forward by setting END to today; START stays fixed.
- **The healer timeline is all-time.** It runs from the first `[doc-healer]` issue (26 Feb 2026) to the latest one (currently 30 Oct 2026); the window end is the newest issue, not the snapshot date.
- Day counts are inclusive-exclusive differences. Current values: 400 d and 246 d.

## 3. Sources

- Repository: gh-aw (public).
- Workflows: `.github/workflows/daily-doc-updater.md`, `unbloat-docs.md`, `glossary-maintainer.md`, `daily-doc-healer.md`.
- The noob tester and multi-device tester are excluded: they post discussions that expire after 1 day, and their older issues have no consistent prefix.

## 4. Queries

Run all of them at once:

```bash
node .github/skills/cracktro/refresh/docs-that-heal.mjs 2026-11-14 > /tmp/dh.json   # omit date = today; ~1–2 min
```

Output shape:

```json
{ "snapshot": "YYYY-MM-DD",
  "prs": { "total": 954, "window": [start,end],
           "agents": { "<updater|unbloat|glossary|healer|consolidator|other|unknown>":
                       { "opened", "merged", "additions", "deletions", "net", "first" } } },
  "healer": { "issues", "open", "window": [first,last], "merged", "inUpdaterPrompt",
              "RULES": [[day,"u|h|x"]...], "detail": [[day,s,number,stateReason]...] } }
```

The script's underlying queries (`REPO` env var, default gh-aw):

1. PRs, one search per calendar month, keeping only titles that start with `[docs]`:
   `gh pr list --state all --limit 1000 --search "in:title docs author:app/github-actions created:A..B" --json number,title,body,state,mergedAt,createdAt,additions,deletions`
2. Attribute each PR to an agent:
   1. The body marker `gh-aw-workflow-id: <id>`.
   2. Otherwise the footer name ("Documentation Unbloat", "Daily Documentation Updater", "Glossary Maintainer", "Documentation Healer").
   3. Otherwise a title regex.
3. Healer issues, keeping only titles that start with `[doc-healer]`:
   `gh issue list --state all --limit 500 --search '"[doc-healer]" in:title' --json number,title,createdAt,closedAt,state,stateReason,closedByPullRequestsReferences`
4. For each closing PR: `gh pr view N --json files,mergedAt`. The class is `u` if it was merged and changed `.github/workflows/daily-doc-updater.md`.

Sanity baseline at the 2026-11-14 snapshot:

| Item | Value |
|---|---|
| `[docs]` PRs | 954 |
| updater merged | 189 |
| unbloat merged | 250 (net −22,043 lines) |
| glossary merged | 146 |
| healer merged | 64 (77 opened) |
| consolidator merged (not shown) | 104 |
| unknown | 6 |
| healer issues | 29, 0 open, 2026-02-26 → 2026-10-30 |
| healer issues merged | 13 (9 `u`) |
| healer issues not merged | 16 (15 NOT_PLANNED) |

If the new numbers are lower than the baseline, suspect the query, not the data: check pagination and title-format changes first.

## 5. Update steps

1. **KPIs:** set the four `kpi()` numbers to `agents.{updater,unbloat,glossary,healer}.merged`. Set unbloat's `em` to `−` plus `abs(agents.unbloat.net)`, with a comma separator and a Unicode minus.
2. **Timeline:** replace `RULES` with `healer.RULES`, update the `250` axis span in `TX`, and recompute the month ticks as day offsets of the 1st of each month from `healer.window[0]` (keep about 5 labels). Update the title counts from `issues`, `merged` and `inUpdaterPrompt`.
3. **Caption:** update the snapshot date, the PR window end and its day count, and the healer window and its day count. Keep "MEASURED ·" and "loop illustrative". Keep the caption ending left of the sound button (about x=1700 at 1920 wide); shorten it before you shrink the font.
4. **Notes:** update the snapshot date, the windows and every number in the measured sentence. Also update the not-merged breakdown, which you get from `detail` stateReason plus reading the issue titles. Keep "…illustrative, simplified example…" so the notes still match `/illustrative|synthetic|simulated/i`.
5. **`.github/skills/cracktro/content-rules.md`:** update the numbers and dates in the "Docs that heal slide" Exception bullet. Do not change other bullets.

## 6. Validation

```bash
node --check docs/public/cracktro/deck.js
python3 -m http.server 4339 --bind 127.0.0.1 --directory docs/public   # async; open http://127.0.0.1:4339/cracktro/#docs-that-heal in the browser canvas
git diff -U0 docs/public/cracktro/deck.js | grep '^+' | grep -nE '[0-9a-f]{40}|github/[a-z]|https?://|who:'   # must print nothing
cd docs && npx playwright test tests/talk.spec.ts
```

- Link the slide by its stable id `#docs-that-heal`.
- On screen, check that the strip clears the left nav-dot minimap (left ≥ 470px) and the sound button, and that the timeline bars do not overflow `TW`.
- Use agent and role names only; never show people, issue or PR numbers, or URLs on the slide.
- Stop the server afterwards.

## 7. Pitfalls

- **Search cap:** GitHub search returns at most 1000 results per query, so keep the per-month windows. If a single month approaches 1000, split it.
- **Shared prefix:** `[docs]` is shared by about 7 agents (consolidator, dictation, spellcheck, Astro updates). Never count by prefix alone; always attribute.
- **Overwritten bodies:** the PR Description Updater overwrites some bodies, which removes the marker and the footer. The title regex is the fallback. Watch the `unknown` count, and add rules if it grows beyond about 1%.
- **Search matches loosely:** the `"[doc-healer]" in:title` search also matches bodies and tokenises the brackets, so filter on `title.startsWith("[doc-healer]")`. The raw search returns 32; the filtered count is 29.
- **Squash merges:** `closedByPullRequestsReferences` lists only linked PRs. An issue closed as completed with no PR (for example a manual close) counts as `x`. Copilot PRs are authored by `app/copilot-swe-agent`, not github-actions, so they are not in the PR query.
- **Renamed workflows:** if a workflow ID changes, add it to `IDS` in the script.
- **Rate limits:** the run makes about 14 searches plus about 15 `pr view` calls. On a 403, wait and re-run; the script holds no state.
- **Day offsets:** compute them from UTC dates. Local time zones shift entries by one day.
