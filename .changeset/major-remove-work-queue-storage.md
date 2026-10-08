---
"gh-aw": major
---

Remove `tools.work-queue.storage`. Work queues always use Git; the compiler rejects
the field for every value, including `git`, with no legacy compatibility.
Remove the field from existing workflows and recompile them.
The runtime also rejects the removed `storage` option and
`GH_AW_WORK_QUEUE_STORAGE` environment variable for every value; remove stale
selectors rather than implicitly switching queues.
