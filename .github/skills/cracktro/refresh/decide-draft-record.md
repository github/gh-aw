---
name: cracktro-refresh-decide-draft-record
description: Recompute the measured data on the cracktro "Decide · Draft · Record" slide and update deck.js.
---

# Refresh: Decide · Draft · Record

Recompute with `decide-draft-record.sh`, then patch `deck.js`. Read-only (git on `origin/main`); edit only the files listed in step 5.

## Workflows mined

| Workflow file | Repo | Role in the slide's flow | Signal mined |
|---|---|---|---|
| `.github/workflows/design-decision-gate.md` | gh-aw | Gate: the agent drafts an ADR and pushes it to the PR branch | ADR files in `docs/adr` on main, their first-add dates and `Status:` lines |
| `.github/agents/adr-writer.agent.md` (imported by the gate) | gh-aw | ADR template and footer | ADR files containing `ADR created by [adr-writer agent]` |

## 1. Slide

- Heading: **Decide · Draft · Record** (`group: "data"`), right after Go Fan.
- Marker: `grep -n "// Decide · Draft · Record" docs/public/cracktro/deck.js`, then use `view_range`. Never read deck.js whole.
- CSS prefixes: `ddr-` (the illustrative flow: PR, ADR and review panels, docs/adr node) and `ddm-` (monthly bars). The strip reuses `.dhm`, `.dhm-k`, `.dhm-tl` and `.dhm-cap` from Docs that heal.
- Data lives in the slide block:
  - `const MONTHS = [["Apr",101], ...]`: ADR files by month first added; the last bar (class `h`) is the partial snapshot month.
  - Timeline title: `"980 ADRS ON MAIN · BY MONTH"`.
  - KPIs: `kpi("980","ADRs",…)`, `kpi("163","added in","the last 30 days")`, `kpi("582","drafted by","the ADR agent","g")`, `kpi("82%","still","Status: Draft","r")`.
  - Caption (`dhm-cap`): `"MEASURED · snapshot 16 Nov 2026 · ADRs on main 11 Apr–16 Nov 2026 (219 d) · flow illustrative"`.
  - `notes:` contains the sentence starting "The bottom strip is measured on gh-aw's main branch (snapshot …)".

## 2. Window & snapshot

- **All-time.** From the first ADR (11 Apr 2026) to the snapshot date; roll it forward by passing today's date. Day count = UTC date difference (219 d at 16 Nov 2026).
- "Last 30 days" counts first-add dates after snapshot − 30 days.

## 3. Sources

- `docs/adr/[0-9]*-*.md` on `origin/main` (files are named after their PR number; `work-queue-protocol-upgrades.md` is not counted).
- `git log --diff-filter=AR` for first-add dates: the earliest add or rename-in date per file (one ADR arrived by rename).
- The "eight of nine drafts merged" sentence in the notes comes from a one-off `gh aw logs design-decision-gate` sample of runs with `push_to_pull_request_branch` output; it is not refreshed. Drop or re-measure it with `gh aw logs design-decision-gate -c 100 --json` if it ages.

## 4. Queries

```bash
bash .github/skills/cracktro/refresh/decide-draft-record.sh 2026-11-16 > /tmp/ddr.json   # omit date = today UTC; ~40 s
```

Every metric is read from `commit`: the last first-parent commit on `origin/main` on or before the snapshot day (UTC), so re-running an old date reproduces that snapshot.

Output: `{ "snapshot", "ref", "commit", "total", "first", "last30", "agentFooter", "status": {draft, accepted, proposed, superseded, ...}, "monthly": [["YYYY-MM", n], ...] }`.

Sanity baseline at the 2026-11-16 snapshot:

| Item | Value |
|---|---|
| total / first | 980 / 2026-04-11 |
| last 30 days | 163 |
| adr-writer footer | 582 |
| status | 799 draft, 97 accepted, 74 proposed, 6 superseded, 1 implemented, 3 none |
| monthly | Apr 101, May 94, Jun 108, Jul 174, Aug 151, Sep 90, Oct 197, Nov 65 |

The monthly sum must equal `total`. If it is lower, a file arrived by rename or copy: check the `--diff-filter`.

## 5. Update steps

1. **KPIs:** `total`, `last30`, `agentFooter`, and `round(100 * status.draft / total)`%.
2. **Bars:** replace `MONTHS` with `monthly` (short month names; keep about 8 bars, dropping the oldest) and update the title count.
3. **Caption:** snapshot date, window end and day count. Keep "MEASURED ·" and "flow illustrative".
4. **Notes:** every number and date in the measured sentences. Keep "…illustrative, simplified example…" so the notes still match `/illustrative|synthetic|simulated/i`.
5. **`.github/skills/cracktro/content-rules.md`:** update the "Decide · Draft · Record slide" Exception bullet. Do not change other bullets.

## 6. Validation

```bash
node --check docs/public/cracktro/deck.js
python3 -m http.server 4339 --bind 127.0.0.1 --directory docs/public   # async; open http://127.0.0.1:4339/cracktro/#decide-draft-record
git diff -U0 docs/public/cracktro/deck.js | grep '^+' | grep -nE '[0-9a-f]{40}|github/[a-z]|https?://|who:'   # must print nothing
cd docs && npx playwright test tests/talk.spec.ts
```

- On screen, check the strip clears the nav-dot minimap and the sound button, and that bar value labels do not touch the title.
- Never show ADR or PR numbers, `Deciders` names or URLs: ADR files name real people.
- Stop the server afterwards.

## 7. Pitfalls

- **Draft ≠ unreviewed:** drafts are merged with their PR; the status is rarely flipped afterwards. Say "still Draft", not "rejected".
- **Status parsing:** both `**Status**: X` and `Status: X` occur; variants such as "Proposed (…)" are counted by their first word.
- **Footer ≠ gate runs:** the adr-writer agent is also used outside the gate, so the footer count is "drafted by the ADR agent", not "by the gate".
- **Fetch first:** the script reads `origin/main`; a stale fetch undercounts.
