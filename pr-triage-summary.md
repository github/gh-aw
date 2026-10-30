# PR Triage Summary

- Run: 2026-10-30T00:44:17Z
- Open Copilot PRs: 15
- Eligible PRs: 13
- Skipped by cooldown: 1

## Selected candidates
- #72938 — Keep safe-output credentials out of agent configuration — bug / high risk / priority high / action fast_track / score 82
- #72273 — Support Claude on Vertex AI with GCP WIF — feature / high risk / priority high / action fast_track / score 74
- #72970 — Retry MCP gateway startup and identify pre-agent checkout failures — bug / medium risk / priority medium / action batch_review / score 67
- #72940 — Preserve agent config permissions and restore only after PR checkout — bug / medium risk / priority medium / action batch_review / score 67
- #72969 — Surface final engine errors in crash and outage reports — bug / medium risk / priority medium / action batch_review / score 66
- #72968 — Surface incomplete-result reasons in failure issues and conclusion summaries — bug / medium risk / priority medium / action batch_review / score 64
- #73034 — Route Safe Output Health Monitor with model routing (goal: cost, mode: economy) — feature / medium risk / priority medium / action fast_track / score 62
- #72941 — Clarify feature-branch dispatch and document safe pre-merge validation — docs / low risk / priority low / action auto_merge / score 42

## Batch
- pr-batch:agentic-safety-fixes — #72970, #72969, #72968, #72940, #72939, #72938, #72936, #72932, #72929

## Notes
- The strongest action is to prioritize the safety fix for secret exposure and the related reliability hardening batch.
- One docs PR is safe to auto-merge; no stale or invalid PRs were found in the eligible set.
