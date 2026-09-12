---
"gh-aw": patch
---

Fix the `logs` MCP tool reporting zero token usage for every run: the compact `usage` artifact set is now downloaded by default (and added to any explicit artifact selection), and each run record always includes `token_usage` and `aic`.
