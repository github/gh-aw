---
"gh-aw": patch
---

Move the Code Scanning Fixer onto the native work queue. A dispatcher runs every 4 hours and admits up to three AI Scan-first clusters of related open alerts (same scanner, rule and parent directory, at most five alerts each) into a dedicated `code-scanning` pool; an assignment-only worker fixes one cluster per Claim in one pull request. Alerts covered by any open pull request are skipped at admission and again when the worker starts. Queue identity and retries replace the cache-memory ledger, checkpoints, and stalled-alert diagnostics.
