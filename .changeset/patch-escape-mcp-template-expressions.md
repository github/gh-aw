---
"gh-aw": patch
---

Escape MCP gateway template expressions in the generated heredocs so GitHub expressions are deferred to the MCP server instead of being expanded by the shell, preventing template-injection risks.
