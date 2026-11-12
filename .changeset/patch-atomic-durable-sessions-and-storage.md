---
"gh-aw": patch
---

Save Atomic sessions, workflow journals, and workflow run artifacts in `/tmp/gh-aw/agent/atomic-store`, declare `behaviors.durable` in the experimental Atomic sample so `engine.durable` restores that store across runs and continues the saved root session, enable `engine.durable` on `smoke-atomic`, and add opt-in libSQL storage (`@bastani/atomic-storage-libsql@0.9.79`) for Turso or self-hosted `sqld` servers.
