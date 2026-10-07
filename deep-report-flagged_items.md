# Flagged Items (DeepReport watch list) — updated 2026-10-07 ~12:50Z

| Item | First seen | Status | Note |
|---|---|---|---|
| 4 Terminal Stylist findings (stderr/stdout formatter mismatch, non-TTY table, Huh stdin-TTY detection, duplicated prefixes) | 2026-10-07 (#66520) | **FILED** (cycle 11, 4 new issues) | All live-verified against current source. Watch pickup. |
| push_repo_memory nested-subdir glob bug (#66253) | 2026-10-04 (#65642) | OPEN, unfixed | Re-confirmed still open this cycle, no new evidence to add. This workflow continues the flat-file workaround at repo-memory root. |
| `cascade-suspected` label volume | 2026-09-06 (96) → 2026-10-06 (180) → 2026-10-07 (184) | WATCH, plateauing | 2 consecutive samples now in the 180-184 range, vs. a sharp climb before that. Possibly stabilizing rather than still trending up — needs 1-2 more samples to confirm either way. |
| Fleet success rate dip (60% raw, 50-run/~3.6h spot-check) | 2026-10-07 (this cycle) | WATCH (1 occurrence) | 19 failures spread across many distinct workflows, 8 driver_exit-class, 0 agent_logic failures. Matches Daily Status's same-day "reliability wave" narrative, already tracked via AW Top 10 issues #66485-66493 + cascade rollup #56767. Not independently fileable — watch whether it's a transient dip or recurs next spot-check. |
| MCP Structural Analysis chronic findings (get_file_contents size blowout, list_issues/list_code_scanning_alerts redaction ambiguity, icon-metadata overhead) | recurring, multi-week | STANDING DECLINE | External github-mcp-server limitations, not an in-repo fix surface (same class as the prior "list_label MCP pagination" decline). Not re-filed without evidence of an in-repo fix path. |
| GitHub MCP search_issues redaction on broad dedup queries | chronic, multi-week | STANDING WORKAROUND | Use local weekly-issues-data/issues.json grep for narrow technical-identifier dedup checks; confirmed again this cycle (9 and 24 items filtered on 2 separate narrow queries). |
| daily-secrets-analysis missing cache-memory | 2026-09-30 (#64554, expired); re-filed 2026-10-04 (#65641) | OPEN | Not re-checked this cycle. |
