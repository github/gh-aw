---
"gh-aw": major
---

`gh aw logs` no longer accepts the `--cached-logs` flag alias.

**⚠️ Breaking Change**: The `--cached-logs` alias has been removed. Cached JSONL reuse, including trailing-wildcard cache prefixes such as `logs-*`, is now available only through `--cached-jsonl`.

**Migration guide:**
- Replace `--cached-logs <path>` with `--cached-jsonl <path>` in scripts, workflows, and automation that invoke `gh aw logs`.
