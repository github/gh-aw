# Agent Performance Analyzer — Latest Run

**Run:** 2026-09-09T13:02Z | **Workflow:** agent-performance-analyzer

## Summary

Full agent ranking deferred this run: `metrics/latest.json` (2026-09-01) shows `active_workflows: 41`, an 83% drop vs. 08-22 (247), with the collector noting a "GitHub API fallback after paginated logs were truncated." Ranking agents against unreliable single-day data would produce noise, not signal.

## Prompt Audit: Redesign-vs-Deprecation Candidates

Audited `mattpocock-skills-reviewer.md`, `impeccable-skills-reviewer.md`, `design-decision-gate.md` directly against source. **No deprecation evidence found in any of the three** — all have explicit Success Criteria/Gate Quality Bar rubrics, noop-vs-act guidance, and turn budgets. Design Decision Gate is the strongest-structured prompt in the set (built-in `evals`: decision-justified, action-taken, adr-check-performed).

**Recommend:** Drop the stale "deprecation candidate" label for these three agents (predates the 2026-09-08 shared-alerts correction). Minor gaps noted (fallback-vs-legitimate-noop logging not distinguished) — process/logging fixes, not redesigns.

## GitHub MCP Read Limitation (this run)

`search_issues`, `search_pull_requests`, `list_issues`, `list_pull_requests` all returned empty with "[Filtered]...lower integrity than agent requires" warnings in this sandbox session. Only `list_tags` returned real data. Live PR merge-rate/coverage analysis was not possible this run — relied on `metrics/latest.json` + daily snapshots instead.

## Actions Taken

- Created discussion: "Agent Performance Report — Week of 2026-09-09"
- No new issues filed (no new systemic finding beyond what WHM/shared-alerts already track)

## Recommendations for Next Run

1. Re-run full ranking once ≥3 consecutive clean daily metric snapshots exist.
2. Confirm Metrics Collector adds a sanity guard against >50% day-over-day `active_workflows` swings.
3. Verify GitHub MCP integrity-filtering issue is sandbox-specific (retest next run) before escalating as a tooling defect.
