---
"gh-aw": major
---

Remove `tools.work-queue.storage`. Work queues always use Git; the compiler rejects
the field for every value, including `git`, with no legacy compatibility.
Remove the field from existing workflows and recompile them.
