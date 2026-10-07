---
"gh-aw": patch
---

Update the DeepSeek Harness sample to dsh 0.2.0-rc.2 with Cordis configuration patches, stdin prompts, isolated run state, and strict provider endpoint selection.

Preserve configured GitHub toolsets, cache memory, and MCP CLI proxies for engines without native MCP support instead of replacing them with default GitHub tools. Include Go dependency domains in the DeepSeek smoke test's build network.
