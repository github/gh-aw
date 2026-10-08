---
"gh-aw": major
---

Remove `tools.work-queue.storage`. Work queues always use Git, with no backend
selection, legacy compatibility, or storage-specific validation.
Remove the field from existing workflows and recompile them.
