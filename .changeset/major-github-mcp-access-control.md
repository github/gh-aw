---
"gh-aw": major
---

Reject `tools.github.roles` and `tools.github.private-repos` instead of silently
ignoring these unsupported GitHub MCP access-control fields.

**Breaking change:** Workflows that configure either key now fail compilation.
Remove them to restore compilation. These fields were not enforced by the local
compiler before; do not rely on them for authorization. No local-compiler
replacement is available while support remains deferred.
