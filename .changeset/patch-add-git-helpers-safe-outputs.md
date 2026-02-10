---
"gh-aw": patch
---

Ensure `git_helpers.cjs` is included in `SAFE_OUTPUTS_FILES` so `generate_git_patch.cjs` and its dependencies load within the safe outputs MCP server.
