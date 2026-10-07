---
"gh-aw": patch
---

Upgrade the default Gemini CLI to 0.63.0 and explicitly select API-key or Vertex AI authentication in generated settings, fixing proxy-backed runs that fail with "Invalid auth method selected." Make Smoke Gemini use supported native tools sequentially and allow the Go runtime and module downloads needed by its build check.

Retain configured MCP servers in Gemini's tool allowlist, which now also governs MCP tool visibility.

Support Copilot-hosted Gemini models via `copilot/gemini*`, translating Gemini requests to Chat Completions through the credential-isolated AWF proxy. Smoke Gemini now exercises this route with the GitHub Actions token.
