---
"gh-aw": patch
---

Bound worker source, lock, registration, and default-revision reads with fresh fetch abort signals and retry only failed metadata GETs. Distinguish Octokit's synthetic status 500 without an HTTP response from GitHub server errors, while preserving original reservations and ambiguous-write recovery.
