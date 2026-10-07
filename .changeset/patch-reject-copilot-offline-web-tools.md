---
"gh-aw": patch
---

Reject unavailable native Copilot web tools instead of silently granting permissions to tools absent in offline BYOK mode. `tools.web-search` now reports a compilation error for Copilot, as does `tools.web-fetch` in CLI mode. Remove these declarations and configure an MCP server, or use Claude or Codex. Copilot SDK mode retains its custom proxy-aware `web-fetch` implementation.
