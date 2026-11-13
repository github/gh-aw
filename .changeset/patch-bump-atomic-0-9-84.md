---
"gh-aw": patch
---

Bump the experimental Atomic sample from 0.9.79 to 0.9.84. The new version adds `atomic storage export` and `atomic storage import`, which move workflow runs and sessions as JSONL files, plus upstream fixes: `"Connection closed."` provider errors now retry, `bash` output ending in a very long line is no longer lost, and `find` and `search` handle globs consistently. The harness invocation, `--mode json` output, private runtime configuration, AWF-proxied inference, and native HTTP MCP are unchanged.
