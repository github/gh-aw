---
"gh-aw": patch
---

Prepare non-writable log files for custom secret-masking hooks even when built-in redaction finds no secrets. Tolerate files and directories that disappear during redaction, and remove unsanitized artifact sources when processing fails.
