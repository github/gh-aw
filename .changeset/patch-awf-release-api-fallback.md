---
"gh-aw": patch
---

Recover from persistent HTTP 5xx errors downloading AWF release assets by trying the public release-assets API with bounded retries when jq is available. Preserve pinned versions, checksum verification, release identity checks, and attestations.
