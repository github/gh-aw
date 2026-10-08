---
"gh-aw": patch
---

Read subagent model attribution from unified agent sessions before falling back to legacy logs. Preserve Copilot subagent configuration, request correlation metadata, and exact per-agent accounting snapshots in unified session artifacts, and restrict legacy inference to CLI dispatch lines.

Render subagent identity, hierarchy, model configuration, per-model requests and tokens, and per-agent credits in session step summaries without adding snapshots to session totals. Isolate retry-attempt attribution and continue to valid structured sources when preferred artifacts are malformed.
