---
"gh-aw": patch
---

The Atomic sample engine (`shared/atomic.md`) now keeps Atomic's JSONL session transcripts across runs when a workflow declares a `tools.drive-memory` entry with `id: atomic`. The harness restores well-formed saved sessions, runs Atomic with `--session-dir`, and writes back only redacted, validated transcripts within a 32 MiB budget. Drive memory publishes them only after threat detection accepts the run. Workflows without that drive keep running with `--no-session`.
