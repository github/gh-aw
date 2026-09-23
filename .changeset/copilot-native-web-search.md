---
"gh-aw": minor
---

Support `tools: web-search:` natively on the Copilot engine. Declaring web search now grants Copilot CLI its built-in `web_search` tool (`--allow-tool web_search`), keeps the built-in tool schema enabled, advertises the capability to the Copilot SDK runtime, and no longer emits an unsupported-engine warning. Web search can now be enabled without granting GitHub repository tools.
