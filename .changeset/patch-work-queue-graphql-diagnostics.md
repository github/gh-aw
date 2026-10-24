---
"gh-aw": patch
---

Improve work queue GraphQL diagnostics with validated request IDs, caller-scoped operation and alias categories, and partial-result presence without exposing provider messages or task data. Preserve valid top-level request-ID fallback when response headers omit or contain an invalid ID. Keep detailed records debug-only and limited to five per failure, with omitted counts in the failure summary. Explicitly distinguish skipped summary type inspection from diagnostic-only sampled types for oversized error collections. Preserve existing retry, receipt-verification, and lock-retention behavior.
