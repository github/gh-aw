---
"gh-aw": patch
---

Add `gh aw work cancel-work --obsolete [--limit N] [--dry-run]` using the JavaScript maintenance selection and checked cancellation semantics. Preserve execution pins, ownership, delivery barriers, native reservations and queue history; recover original targets on stable-request retries.
