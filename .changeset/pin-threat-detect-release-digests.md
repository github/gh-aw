---
"gh-aw": patch
---

Verify downloaded threat-detect binaries against compiler-embedded release digests, and support HTTPS artifact mirrors without allowing them to override the pinned release or checksum. When installation does not complete verification, detection analysis and the conclude step both fail closed without invoking any detector binary.
