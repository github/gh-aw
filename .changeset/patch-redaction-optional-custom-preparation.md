---
"gh-aw": patch
---

Avoid unnecessary write-permission failures when scanning secret-free, read-only logs without custom masking hooks. Preserve writable-file preparation for configured custom hooks and fail-closed cleanup for actual redaction failures.
