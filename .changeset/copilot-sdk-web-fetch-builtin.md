---
"gh-aw": patch
---

Register an overridden `web_fetch` tool as a built-in tool in the Copilot SDK tool config so workflows using `tools.web-fetch` expose it to the model, matching how the SDK classifies the override at runtime.
