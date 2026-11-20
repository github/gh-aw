---
"gh-aw": patch
---

Move the Code Scanning Fixer onto the native work queue. A scheduled dispatcher admits up to three severity-ranked open alerts per run into a dedicated `code-scanning` pool, and an assignment-only worker fixes one alert per Claim. Queue identity and retries replace the cache-memory ledger, checkpoints, and stalled-alert diagnostics.
