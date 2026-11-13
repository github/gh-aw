---
"gh-aw": patch
---

The Atomic sample engine (`shared/atomic.md`) now keeps Atomic's JSONL session transcripts across runs when a workflow declares a `tools.drive-memory` entry with `id: atomic`. The harness restores well-formed saved sessions, runs Atomic with `--session-dir`, and writes back only redacted, validated transcripts within a 32 MiB budget. Drive memory publishes them only after threat detection accepts the run. With Atomic 0.9.83 or newer, it also keeps workflow runs: it imports validated `workflows/*.jsonl` exports into a private journal with `atomic storage import` before the run, and afterwards saves redacted `atomic storage export` files (unfinished runs first, then newest, within another 32 MiB), so an interrupted Atomic workflow can be resumed in a later run. Older Atomic versions keep sessions only. Persistence problems are logged as warnings and never fail the agent step. Workflows without that drive keep running with `--no-session`.
