# Flagged Items (DeepReport watch list) — updated 2026-10-05 ~01:15Z

| Item | First seen | Status | Note |
|---|---|---|---|
| push_repo_memory nested-subdir glob bug | 2026-10-04 (#65642) | OPEN, filed, AW Top 10 #65657 | Root cause confirmed; deep-report.md:73 still slashless as of this cycle. Explains this memory folder's own ~26-day staleness. |
| daily-secrets-analysis missing cache-memory | 2026-09-30 (#64554, expired); re-filed 2026-10-04 (#65641) | OPEN | 2nd filing after 1st auto-expired unpicked. |
| Daily Team Evolution Insights total failure | 2026-10-04 (#65663) | WATCH (1 occurrence) | "GitHub MCP tools are not available" despite github:mode:local in frontmatter. Only goose+mode:local workflow in fleet — no sibling to cross-check. File if it recurs. |
| Daily Observability Report permission-denied on aw-mcp/logs | 2026-10-04 (#65685) | WATCH (1 occurrence) | Root cause unconfirmed — code comment nearby describes a different cache dir's design rationale, not a documented restriction on this path. File if it recurs with a confirmed root cause. |
| `agenticworkflows logs` chronic client-side timeout on unscoped fleet queries | recurring across many cycles since ~2026-09 | FILED 2026-10-05 (this cycle) | Root cause: no-workflow-filter queries hit a 5-min server floor vs ~50-60s typical wait budget. First time filed as its own issue rather than worked around. |
| #65173 protected-files.exclude depth bug | 2026-10-04 | BLOCKED (integrity policy) | Still inaccessible this cycle (`issue:github/gh-aw#65173` filtered, integrity below "approved"). Re-check next cycle. |
| GitHub MCP search_issues redaction on broad dedup queries | chronic, multi-week | STANDING WORKAROUND | Use local weekly-issues-data/issues.json grep for narrow technical-identifier dedup checks. |
