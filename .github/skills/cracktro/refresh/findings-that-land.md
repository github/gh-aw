---
name: cracktro-refresh-findings-that-land
description: Recompute the measured data on the cracktro "Findings that land" slide and update deck.js.
---

# Refresh: Findings that land

Recompute with `findings-that-land.mjs`, then patch `deck.js`. Read-only on GitHub; edit only the files listed in step 5.

## Workflows mined

| Workflow file | Repo | Role in the slide's flow | Signal mined |
|---|---|---|---|
| `.github/workflows/enterprise-docs-audit.md` | gh-aw | Persona walks the enterprise docs | `[enterprise-docs] ` issues |
| `.github/workflows/spec-librarian.md` | gh-aw | Checks specs against code | `[spec-librarian] ` issues |
| `.github/workflows/daily-multi-device-docs-tester.md` | gh-aw | Browses the docs on 10 viewports | `🔍 Multi-Device Doc…` issues |
| `.github/workflows/cli-consistency-checker.md` | gh-aw | Compares CLI help with the docs | `[cli-consistency] ` issues |
| `.github/workflows/daily-doc-healer.md` | gh-aw | Fixes docs the updater missed | `[doc-healer] ` issues |
| `.github/workflows/daily-spec-coverage-kiro.md` | gh-aw | Rolling spec coverage report | `[spec-coverage] ` issues |
| `.github/workflows/daily-github-docs-seo-optimizer.md` | gh-aw | Rolling SEO report on the docs | `[github-docs-seo] ` issues |
| `.github/workflows/designer-drift-audit.md` | gh-aw | Reference docs vs designer skill | `Designer Drift Audit…` issues |

For every issue: merged closing PRs (`closedByPullRequestsReferences`), their author, and whether `Copilot` is an assignee.

## 1. Slide

- Heading: **Findings that land** (`group: "data"`), right after Multi-device tester.
- Marker: `grep -n "// Findings that land" docs/public/cracktro/deck.js`, then use `view_range`. Never read deck.js whole.
- CSS prefix `ftl-` (table, bars, cards). The KPI strip reuses `.dhm`, `.dhm-k` and `.dhm-cap` from Docs that heal.
- Data lives in the slide block:
  - `const AUD = [[name, fixed, filed, assignedPct], ...]`, ranked by fixed share. `name` drops the `daily-` prefix. A dashed line is drawn above the first row with `assignedPct` 0.
  - KPIs: `kpi("583","findings filed","by 8 docs auditors")`, `kpi("273","fixed by",…"255 by Copilot")`, `kpi("79%","fixed when",…"256 of 323","g")`, `kpi("7%","fixed when",…"17 of 260","r")`.
  - Caption (`dhm-cap`): `"MEASURED · snapshot 15 Nov 2026 · issues 12 Nov 2025–15 Nov 2026 (368 d) · cards illustrative"`.
  - `notes:` contains the sentences starting "The table is measured on gh-aw (snapshot …)" through "…were."

## 2. Window & snapshot

- **All-time.** The window runs from the earliest issue of any auditor (12 Nov 2025, cli-consistency) to the snapshot date. Roll it forward by passing today's date; the start stays fixed unless an auditor is added.
- Day counts are differences of UTC dates. Current value: 368 d.

## 3. Sources

- Repository: gh-aw (public), issues only. Discussion-only docs workflows (schema-consistency-checker, docs-noob-tester, claude-code-user-docs-review) file no issues and are out of scope.
- The workflows' own `[aw] … failed` reports carry the same body marker but not the title prefix, so the title filter drops them.

## 4. Queries

```bash
node .github/skills/cracktro/refresh/findings-that-land.mjs 2026-11-15 > /tmp/ftl.json   # omit date = today; ~1 min
jq -c '.rows[]|[.id,.fixed,.filed,.rate,.copilotAssigned]' /tmp/ftl.json
```

Output shape:

```json
{ "snapshot", "window": [first,end], "days", "filed", "fixed", "copilotFix", "copilotAssigned", "assignedFixed",
  "rows": [{ "id", "filed", "fixed", "rate", "copilotFix", "copilotAssigned", "assignedFixed",
             "open", "notPlanned", "completedNoPr", "medianHoursToMerge", "window", "days" }] }
```

Per auditor the script runs one paginated GraphQL search, `repo:REPO is:issue "<phrase>" in:title created:<=END`, and keeps titles that start with the prefix. An issue is **fixed** if a closing PR is merged on or before END. `copilotFix` counts fixes with a PR by `copilot-swe-agent`; `assignedFixed` counts fixed issues that have `Copilot` as an assignee.

Sanity baseline at the 2026-11-15 snapshot:

| Auditor | Filed | Fixed | % | → Copilot | Median h |
|---|---|---|---|---|---|
| enterprise-docs-audit | 4 | 4 | 100 | 4 | 10 |
| spec-librarian | 69 | 44 | 64 | 52 | 1.1 |
| daily-multi-device-docs-tester | 61 | 39 | 64 | 46 | 6.5 |
| cli-consistency-checker | 287 | 168 | 59 | 201 | 3.8 |
| daily-doc-healer | 29 | 13 | 45 | 20 | 0.5 |
| daily-spec-coverage-kiro | 42 | 4 | 10 | 0 | 90.5 |
| daily-github-docs-seo-optimizer | 87 | 1 | 1 | 0 | 474 |
| designer-drift-audit | 4 | 0 | 0 | 0 | — |
| **Total** | 583 | 273 (255 Copilot PRs) | 47 | 323 (256 fixed) | |

Multi-device (61/39/35) and doc-healer (13 fixed) must agree with `multi-device-tester.md` and `docs-that-heal.md`; if they don't, suspect the query.

## 5. Update steps

1. **Table:** replace `AUD` from `rows` (`fixed`, `filed`, `round(100*copilotAssigned/filed)`), keeping the rank order by `rate`.
2. **KPIs:** `filed`; `fixed` with `copilotFix`; `assignedFixed/copilotAssigned` as a %; `(fixed-assignedFixed)/(filed-copilotAssigned)` as a %. Keep the auditor count in "by N docs auditors".
3. **Caption:** update the snapshot date, the window end and its day count. Keep "MEASURED ·" and "cards illustrative".
4. **Notes:** update the snapshot date, the window and every number in the measured sentences. Keep "…illustrative, simplified example." so the notes still match `/illustrative|synthetic|simulated/i`, and keep the correlation caveat.
5. **`.github/skills/cracktro/content-rules.md`:** update the numbers and dates in the "Findings that land slide" Exception bullet. Do not change other bullets.

## 6. Validation

```bash
node --check docs/public/cracktro/deck.js
python3 -m http.server 4339 --bind 127.0.0.1 --directory docs/public   # async; open http://127.0.0.1:4339/cracktro/#findings-that-land
git diff -U0 docs/public/cracktro/deck.js | grep '^+' | grep -nE '[0-9a-f]{40}|github/[a-z]|https?://|who:'   # must print nothing
cd docs && npx playwright test tests/talk.spec.ts
```

- On screen, check that the table clears the left nav-dot minimap, the cards stay inside the stage, and the caption ends left of the sound button.
- Show workflow and agent names only; never people, issue or PR numbers, or URLs.
- Stop the server afterwards.

## 7. Pitfalls

- **Loose search:** phrases match bodies and tokenise brackets and emoji; always filter on the title prefix.
- **Failure reports:** `[aw] <Workflow> failed` issues share the body marker; never count by marker alone.
- **Designer drift prefix** has no trailing space (`Designer Drift AuditWorkflow…`).
- **Author login** in GraphQL is `copilot-swe-agent` (REST/`gh pr view` shows `app/copilot-swe-agent`); the assignee login is `Copilot`.
- **Correlation, not causation:** maintainers assign what they judge worth fixing, and the unassigned auditors expire their reports by design (`close-older-issues`, 2–7 day `expires`). Keep that caveat in the notes.
- **Date filter:** use `created:<=END`, not `created:..END`.
