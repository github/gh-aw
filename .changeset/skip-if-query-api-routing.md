---
"gh-aw": patch
---

Evaluate simple `skip-if-match`/`skip-if-no-match` queries through GraphQL or REST list endpoints instead of the rate-limited search API, choosing the backend from current rate-limit pressure and falling back to search for queries that need it.
