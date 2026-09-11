---
"gh-aw": patch
---

Fix `--count` and `--timeout` wiring for multi-target `gh aw logs` downloads. Each target now bounds its pagination and artifact downloads by the runs the shared count budget can still report, instead of fetching and downloading as if it alone had to satisfy the full `--count` and discarding the surplus after download. Targets also keep the caller's timeout value (while still inheriting the single shared deadline), so continuations emitted by a multi-target run can be replayed with the same timeout instead of reporting `timeout: 0`.
