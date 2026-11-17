---
name: cracktro-refresh-human-agent-processes
description: Recompute the measured data on the cracktro "Human Agent Processes" slide and update deck.js.
---

# Refresh: Human Agent Processes

Plan for recomputing the MEASURED column. Run from the repository root. Read `../SKILL.md` (preview loop) and `../content-rules.md` first. Never read `deck.js` whole.

## Workflows mined

| Workflow file | Repo | Role in the slide's flow | Signal mined |
|---|---|---|---|
| `.github/workflows/auto-triage-issues.md` | gh-aw | IssueOps: triages new issues | successful runs on `issues` events |
| `.github/workflows/daily-cli-tools-tester.md` | gh-aw | DailyOps: daily tester | `[cli-tools-test]` issues, split by state_reason |
| `.github/workflows/pr-code-quality-reviewer.md` | gh-aw | ReviewOps: reviews ready PRs | distinct head branches of successful `pull_request` runs |
| `.github/workflows/changeset.md` | gh-aw | LabelOps: runs on label | distinct head branches of successful `pull_request` (labeled) runs |

## 1. Slide

- Heading: `Human Agent Processes`. Kicker: `Patterns · measured on gh-aw`. Group: `process`.
- Marker: `grep -n "// Human Agent Processes" docs/public/cracktro/deck.js`.
- CSS prefix: `hap-` (styles block: `grep -n "\.hap-" docs/public/cracktro/deck.js`).
- Data lives in:
  - `const HAP = [...]`: the 5th element of each row is `[number, label, workflow, "1 of N …"]`, for example `["366", "triage runs", "auto-triage-issues", "1 of 3 issue workflows"]`.
  - The header `MEASURED<tspan class="hap-lw" dx="12">30 DAYS</tspan>`.
  - The caption `<text class="hap-cap" …>measured on gh-aw · 15 Oct – 14 Nov 2026 (30 days) · "1 of N": workflow files, snapshot 14 Nov 2026 · flows illustrative</text>`.
  - The `notes:` string beginning `"Human Agent Processes: build the factory…"`.
- The flows (`HAP[r][3]`) are illustrative. Do not change them.

## 2. Window and snapshot

- Current window: 15 Oct to 14 Nov 2026, 30 days, snapshot 14 Nov 2026.
- Roll the window forward: use a rolling 30 days ending on the refresh date (`END` = today, `START` = END − 30 days).
- "1 of N" totals are a snapshot of `.github/workflows/*.md` on `END` (at the `origin/main` checkout), not windowed.

## 3. Sources

Everything comes from the public repo `github/gh-aw`.

| Pattern | Workflow (`.github/workflows/`) | Metric |
|---|---|---|
| IssueOps | `auto-triage-issues.md` | successful runs with event `issues` |
| DailyOps | `daily-cli-tools-tester.md` | issues titled `[cli-tools-test]…`, split by state_reason |
| ReviewOps | `pr-code-quality-reviewer.md` (ready_for_review) | distinct head branches of successful `pull_request` runs (= PRs reviewed) |
| LabelOps | `changeset.md` (`pull_request` labeled) | distinct head branches of successful `pull_request` runs |

## 4. Queries

The helper runs every query and prints JSON (read-only, about 1 minute):

```bash
git fetch -q origin main && git switch -C <branch> origin/main
node .github/skills/cracktro/refresh/human-agent-processes.mjs            # END=today, 30 days
node .github/skills/cracktro/refresh/human-agent-processes.mjs 2026-11-14 30
```

Output shape: `{window:{start,end,days}, issueops:{success_runs,distinct_branches}, dailyops:{issues,completed,not_planned,open}, reviewops:{…}, labelops:{…}, triggers:{issues_opened,schedule,ready_for_review,label}}`.

Equivalent manual queries:

```bash
# Runs: query 5-day chunks, because each query returns at most 1000 runs.
gh api --paginate --slurp "repos/github/gh-aw/actions/workflows/auto-triage-issues.lock.yml/runs?created=2026-10-15..2026-10-19&per_page=100" \
  | jq '[.[].workflow_runs[] | select(.conclusion=="success" and .event=="issues")] | length'
# Issues by exact title prefix (search is fuzzy, so filter with startswith).
gh api --paginate --slurp -X GET search/issues -f per_page=100 \
  -f q='repo:github/gh-aw is:issue in:title "[cli-tools-test]" created:2026-10-15..2026-11-14' \
  | jq '[.[].items[] | select(.title|startswith("[cli-tools-test]"))] | group_by(.state_reason) | map({(.[0].state_reason // "open"): length}) | add'
```

Baseline (`END=2026-11-14`, 30 days), slide value vs helper:

| Field | On slide | Helper | Note |
|---|---|---|---|
| IssueOps triage runs | 366 | 366 | |
| DailyOps issues · done | 30 · 16 | 30 · 16 (10 not planned, 4 open) | |
| ReviewOps PRs reviewed | 906 | 906 (935 runs) | corrected from 825; the old value came from one query capped at 1000 runs; use the helper |
| LabelOps PRs labelled | 9 | 9 (11 runs) | |
| triggers issues/schedule/review/label | 3 / 194 / 8 / 6 | 3 / 193 / 7 / 7 | the helper's heuristic differs slightly; use the helper |

## 5. Update steps

1. Edit the 5th elements of `HAP` with the helper values. Keep the strings short; the MEASURED column is about 230 px wide (`HMX` = 1456 of `HVW` = 1700).
   - IssueOps: `issueops.success_runs` and `"1 of ${triggers.issues_opened} issue workflows"`.
   - DailyOps: `dailyops.issues`, `"issues · ${completed} done"` and `"1 of ${triggers.schedule} scheduled"`.
   - ReviewOps: `reviewops.distinct_branches` and `"1 of ${triggers.ready_for_review} review workflows"`.
   - LabelOps: `labelops.distinct_branches` and `"1 of ${triggers.label} label workflows"`.
2. If the run count changes, update the header tspan (`30 DAYS`).
3. Caption: set the dates as `D Mon – D Mon YYYY (N days)` and `snapshot D Mon YYYY`, keeping `flows illustrative`.
4. Notes: update every number, the window and the snapshot date. Keep the sentence "The flows are illustrative patterns with synthetic labels…" so the notes still match `/illustrative|synthetic|simulated/i`.
5. In `../content-rules.md`: update the "Human Agent Processes slide (`hap-` classes)" Exception bullet with the new numbers and dates. Slide numbers shift, so find it by the `hap-` text.
6. If the chosen workflow is renamed, removed, or idle (0 in the window), pick another one with the same trigger. Update `HAP`, the notes, the content-rules.md bullet, the source table above, and the helper.

## 6. Validation

```bash
node --check docs/public/cracktro/deck.js
python3 -m http.server 4391 --bind 127.0.0.1 --directory docs/public   # async; then stop it
```

- Open `http://127.0.0.1:4391/cracktro/#human-agent-processes`.
- Screenshot `.slide.active .hap-svg` and check that the header and caption do not clip at the right or bottom edge.
- Content rules:
  - Workflow and role names only, no people.
  - No `github/<repo>` strings in `deck.js` and no 40-hex SHAs.
  - The only URL is the allowed docs one.
- Run `cd docs && npx playwright test tests/talk.spec.ts` (see `../SKILL.md` for local workarounds).

## 7. Pitfalls

- **Runs API cap:** each query returns at most 1000 runs (`total_count` can be higher), so query in chunks. The helper uses 5-day chunks and throws when a chunk overflows.
- **Run conclusions:** count only `success`. Skipped and cancelled runs (concurrency, gates) are common.
- **ReviewOps branch count:** `pr-code-quality-reviewer` also has other events. Count `event == pull_request` only, and one branch per PR.
- **Shared title prefixes:** search `in:title` is fuzzy. Always filter with `startswith("[cli-tools-test]")`.
- **Search API:** returns at most 1000 results and has a rate limit of 30 requests/min. Use a narrower `created:` range if needed. A 403 means wait and retry.
- **Today's runs:** if `END` is today, the numbers keep moving. State the snapshot date in the caption.
- **Trigger heuristics:**
  - Short forms (`on: daily`, `on: pull_request labeled refine`) and `label_command` count.
  - Comments are stripped, and `smoke-*` workflows are excluded.
  - Re-check any file whose `on:` block changed shape.
