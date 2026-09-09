---
"gh-aw": major
---

Remove built-in Playwright MCP support. The built-in `tools.playwright` integration now always uses `@playwright/cli`, and `mode: mcp` is a compile error with migration guidance. Workflows that still require Playwright MCP must configure it explicitly under `mcp-servers`.
