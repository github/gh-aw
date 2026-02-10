---
"gh-aw": patch
---

Ensure MCP-related API keys are masked immediately after generation to close the timing window where they could leak into logs or artifacts.
