---
"gh-aw": patch
---

Grant repo-memory directories write access under the cloud-hypervisor sandbox runtime, so agents can update `/tmp/gh-aw/repo-memory/<id>` instead of failing with a read-only filesystem error.
