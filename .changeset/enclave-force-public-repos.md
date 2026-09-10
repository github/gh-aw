---
"gh-aw": patch
---

Disable the MCP gateway's `forcePublicRepos` override when the GitHub MCP server is rendered solely for a static agent enclave, so the enclave's configured `allowed-repos` is no longer silently rewritten to `repos: "public"` in public repositories. Compiling a static GitHub enclave alongside primary `tools.github` now warns that the enclave cannot read its declared private repositories.
