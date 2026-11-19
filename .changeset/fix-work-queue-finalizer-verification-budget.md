---
"gh-aw": patch
---

Give each Claim its full delivery-verification budget in the worker's own trusted finalizer, starting after ledger verification and the native-run check. The active worker can retry after the Completion deadline, but it cannot publish a `DeliveryFailure` without terminal run evidence. Maintenance recovery, failure exhaustion, and missing-verifier expiry stay anchored at Completion. Claim delivery diagnostics count every verifier call, including calls whose proofs are later discarded.
