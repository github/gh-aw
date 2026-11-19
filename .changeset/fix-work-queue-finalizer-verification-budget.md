---
"gh-aw": patch
---

Give the work-queue worker's own trusted finalizer its full delivery-verification budget from the moment it holds the verified ledger, so a slow ledger load in `safe_outputs` no longer leaves a verifiable Claim `delivery_pending` with zero attempts. Maintenance recovery, `DeliveryFailure` exhaustion and missing-verifier expiry stay anchored at the original Completion. Claim delivery diagnostics now report the attempt count.

Reuse ledger checkpoint verification across the trusted steps of one work-queue job through a private file under the job's `RUNNER_TEMP`, so later steps no longer replay every checkpoint generation. Compiled work-queue safe-output jobs opt in with `GH_AW_WORK_QUEUE_VERIFICATION_CACHE`; each read still rechecks repository access over Git, and shared, linked, foreign or malformed cache files are ignored. Recompile work-queue workflows to enable it.
