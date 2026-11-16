---
"gh-aw": patch
---

Keep multiple asset uploads independently verifiable as content-addressed files in their original Claim namespace on the existing `work-queue` branch. Recover bounded, definitively rejected publication conflicts without forcing ref updates, overwriting concurrent ledger changes, or replaying uncertain writes. JavaScript and Go queue readers retain complete metadata history with blob-filtered fetches and authenticated exact-object reads instead of downloading unrelated assets; unsupported filtering fails closed. Report temporary Git-store cleanup failures with sanitized diagnostics. Start projection lock API deadlines after mutation pacing, preserve pre-dispatch rejection errors, and retain uncertain-write fencing.
