---
"gh-aw": patch
---

Terminally cancel obsolete unpinned work during queue maintenance, preserving compatible deployments, exact execution pins, delivery barriers and ledger history. Recheck `worker_contract_obsolete` cancellations against the current approved active contract in both queue replay implementations.
