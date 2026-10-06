# Flagged Items (DeepReport watch list) — updated 2026-10-06 ~18:40Z

| Item | First seen | Status | Note |
|---|---|---|---|
| push_repo_memory nested-subdir glob bug | 2026-10-04 (#65642) | **RE-FILED** (new issue, cycle 8) | #65642/#65657 were closed "completed" 2026-10-05 but fix never landed — `memory_file_eligibility.cjs` unchanged, deep-report.md:73 still slashless. Live-reproduced 2026-10-06: push_repo_memory only sees 3 flat-copy files, 0 patch diff for new nested files. Re-filed as a fresh issue citing live reproduction; watch whether it gets closed again without verification. |
| 3-of-6 memory files uncovered by flat-copy workaround | 2026-10-06 (this cycle) | MITIGATED this cycle | known_patterns.md/trend_data.md/extracted-tasks.md had no flat copy and were stuck since ~2026-09-07. Added flat copies this cycle; remove this workaround once the glob bug is actually fixed upstream. |
| Fleet success rate dip (71% raw, 1h spot-check) | 2026-10-06 (this cycle) | WATCH (1 occurrence) | 8 failures concentrated in PR-review/quality-gate bots, same window as dynamic-workflow-engine changes landing on main. No common root cause confirmed yet — check next cycle before filing. |
| daily-secrets-analysis missing cache-memory | 2026-09-30 (#64554, expired); re-filed 2026-10-04 (#65641) | OPEN | Not re-checked this cycle. |
| `agenticworkflows logs` chronic client-side timeout on unscoped fleet queries | recurring since ~2026-09 | FILED 2026-10-05 (#unknown) | Worked around again this cycle with `--start-date -1h` (scoped query succeeded in 35s). |
| GitHub MCP search_issues redaction on broad dedup queries | chronic, multi-week, still occurring 2026-10-06 | STANDING WORKAROUND | Use local weekly-issues-data/issues.json grep for narrow technical-identifier dedup checks. |
| Daily Team Evolution Insights total failure / Observability permission-denied | 2026-10-04 | DROPPED (no recurrence, not re-checked) | No fresh report from either workflow in recent windows; drop from active watch unless it resurfaces. |
