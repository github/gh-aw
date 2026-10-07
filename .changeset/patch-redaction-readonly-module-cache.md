---
"gh-aw": patch
---

Fix secret redaction for read-only files in read-only directories, including Go module caches created by agents. Preserve fail-closed handling when files cannot be sanitized or removed without exposing file paths in cleanup failure diagnostics.
