---
"gh-aw": minor
---

Promote `tools.ledger` out of experimental status. Remove its compile-time experimental warning and move ledger replay and compaction documentation into the reference section, preserving redirects from the former URLs. Existing ledger behavior, limits, and isolation requirements are unchanged. Legacy `ledger` configuration remains accepted in a single-entry `tools.repo-memory` array, while the object-form `tools.repo-memory.ledger` declaration is rejected during compilation. New workflows should use `tools.ledger`.
