---
"gh-aw": patch
---

Custom jobs auto-wired as prerequisites of built-in jobs are no longer forced to succeed when `jobs.agent.if` (or another built-in job condition) uses a status function such as `always()`. Compiler-owned prerequisites like `activation` stay guarded, so an agent can now analyze a failing custom job in the same run.
