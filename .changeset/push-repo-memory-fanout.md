---
"gh-aw": patch
---

Remove the job-level concurrency group from the `push_repo_memory` job. GitHub Actions queues at most one pending job per concurrency group, so when many runs sharing a memory branch finished at once, all but two push jobs were cancelled and their memory writes were silently lost. Pushes now converge optimistically: the push script retries up to 10 times with capped full-jitter exponential backoff, re-reading the remote head and merging before each retry.
