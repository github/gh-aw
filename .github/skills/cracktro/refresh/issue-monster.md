---
name: cracktro-refresh-issue-monster
description: Recompute the measured data on the cracktro "Issue Monster" slide and update deck.js.
---

# Refresh: Issue Monster

Recompute with `issue-monster.sh`, then patch `deck.js`. Read-only on GitHub; edit only the files listed in step 5.

## Workflows mined

| Workflow file | Repo | Role in the slide's flow | Signal mined |
|---|---|---|---|
| `.github/workflows/issue-monster.md` | gh-aw | 15-minute scheduler: pre-activation gates, a filter/score script, then an agent assigns up to three `cookie` issues to Copilot | `Issue Monster selected this for Copilot` comments, the merged Copilot PRs that closed those issues, and the agent job's conclusion per run |

## 1. Slide

- Heading: **Issue Monster** (`group: "data"`, right before Research · Assign · Observe). Kicker: "Issue Monster loves cookie issues".
- Marker: `grep -n "// Issue Monster:" docs/public/cracktro/deck.js`, then use `view_range`. Never read deck.js whole.
- CSS prefix `qga-` (source pills, queue fates, gates, skip stamp, score rows, strip, month bars, token bracket). It reuses `.hff-sl`, `.sdd-panel`/`.sdd-ln` and the `.dhm`, `.dhm-k`, `.dhm-tl` and `.dhm-cap` strip from Docs that heal.
- Data lives in the slide block:
  - `const MONTHS = [["Apr", picked, copilotMerged], ...]`; `BH` scales by the largest `picked` (`755`).
  - The `token expired` bracket spans the Sep and Oct bars (`k0 = 5 * GW + 4`, `k1 = 7 * GW - 4`); move it if the months shift.
  - KPIs: `kpi("2,630","picked",…)`, `kpi("1,416","fixed by",…"Copilot · 54%")`, `kpi("8.1 h","median",…)`, `kpi("1,126","merged",…)`, `kpi("92%","runs",…"567 of 615 · 7 d")`.
  - Caption (`dhm-cap`): `"MEASURED · snapshot 16 Nov 2026 · picks 19 Apr–16 Nov 2026 (211 d) · by issue month · runs 9–15 Nov · loop illustrative"`.
  - The five source pills are the top `prefixes` (title prefixes of picked issues).
  - `notes:` contains the sentences starting "The bottom strip is measured on gh-aw (snapshot …)".

## 2. Window & snapshot

- **Picks: all-time.** The current comment wording starts on 19 Apr 2026; older comments used other wording and are not counted. The search starts at issues created on 1 Apr 2026. Roll the end forward by passing today's date.
- **Runs: the 7 full UTC days before the snapshot** (END-7 to END-1).
- Day counts are differences of UTC dates. Current value: 211 d.

## 3. Sources

- Repository: gh-aw (public).
- Workflow: `.github/workflows/issue-monster.md` (lock `issue-monster.lock.yml`).
- Prompt: PR #4684 body (see `workflow-prompts.md`).

## 4. Queries

```bash
.github/skills/cracktro/refresh/issue-monster.sh 2026-11-16 > /tmp/im.json   # omit date = today; needs gh + jq; ~6 min
```

Output shape:

```json
{ "snapshot", "window": [first,end], "picked", "open", "completed", "notPlanned",
  "copilotMerged", "pct", "medianHoursToMerge", "within24h",
  "MONTHS": [["YYYY-MM",picked,copilotMerged]...],
  "prefixes": [["deep-report",n]...],
  "runs": {"window": [since,until], "total", "agentSkipped", "agentRan"} }
```

Titles are untrusted: the only text printed is a title prefix matching `^\[[a-z0-9-]+\]`.

The script's underlying queries (`REPO` arg, default gh-aw):

1. Picks: GraphQL search `repo:REPO is:issue "Issue Monster selected this for Copilot" in:comments created:A..B`, in half-month slices (search stops at 1,000 results).
2. Fix: the earliest PR in `closedByPullRequestsReferences` that merged on or before END and whose author login matches `/copilot/i`.
3. Median and within-a-day: issue creation to that merge.
4. Months: grouped by the issue's creation month, not the pick date.
5. Runs: `actions/workflows/issue-monster.lock.yml/runs?created=SINCE..UNTIL`, then each run's `agent` job conclusion. `skipped` means pre-activation stopped it (a gate, or nothing left after filtering).

Sanity baseline at the 2026-11-16 snapshot:

| Item | Value |
|---|---|
| picked | 2,630 (17 open, 1,547 completed, 1,058 not planned), 2026-04-19 → 2026-11-16 (211 d) |
| merged Copilot fixes | 1,416 (54%), median 8.1 h, 1,126 within 24 h |
| months (picked/merged) | Apr 55/39, May 378/260, Jun 398/58, Jul 584/387, Aug 755/534, Sep 132/4, Oct 173/27, Nov 155/107 |
| prefixes | deep-report 646, aw-failures 258, lint-monster 124, refactor 103, cli-consistency 98 |
| runs 9–15 Nov | 615, agent skipped 567 (92%), ran 46 |

If the new numbers are lower than the baseline, suspect the query first: the comment wording and the slice size.

## 5. Update steps

1. **KPIs:** set `picked`, `copilotMerged` and `pct`, `medianHoursToMerge`, `within24h`, and the runs share `agentSkipped/total` (rounded %).
2. **Months:** replace `MONTHS` (short month names), rescale `BH` to the largest `picked`, and move or drop the token bracket.
3. **Sources:** if the top prefixes change, update the five pill labels.
4. **Caption and notes:** update the snapshot, both windows and every measured number. Keep "MEASURED ·", "loop illustrative", and "…are illustrative and simplified." so the notes still match `/illustrative|synthetic|simulated/i`.
5. **`.github/skills/cracktro/content-rules.md`:** update the numbers and dates in the "Issue Monster slide" Exception bullet. Do not change other bullets.

## 6. Validation

```bash
node --check docs/public/cracktro/deck.js
python3 -m http.server 4339 --bind 127.0.0.1 --directory docs/public   # async; open http://127.0.0.1:4339/cracktro/#issue-monster
git diff -U0 docs/public/cracktro/deck.js | grep '^+' | grep -nE '[0-9a-f]{40}|github/[a-z]|https?://|who:' | grep -v 'w3.org/2000/svg'   # must print nothing
cd docs && npx playwright test tests/talk.spec.ts
```

- On screen, check that the strip clears the nav-dot minimap and the sound button, that the five KPIs and the month bars fit on one row, and that the PR panel and queue fates do not overlap row text.
- No people appear: role icons only, no mascots. Never show real people, issue or PR numbers, or URLs.
- Stop the server afterwards.

## 7. Pitfalls

- **Picks are not assignments:** in Sep–Oct 2026 the assign-to-agent token had expired and was not refreshed, so Issue Monster kept commenting picks but almost none reached Copilot. The gates do not check the token. Do not read the dip as a loop failure, and do not assert a cause for other dips (June) without evidence.
- **Comment wording:** older picks used other wording; extending `START` earlier adds nothing unless the marker changes too.
- **Search cap:** a slice with more than 1,000 hits is silently truncated; shrink the slice.
- **Copilot author:** match the login with `/copilot/i`; do not hard-code one bot login.
- **Runs cost:** one jobs call per run (~600 calls); expect ~6 min and watch the rate limit.
- **Historical snapshots:** a PR merged after the snapshot date does not count as a fix.
