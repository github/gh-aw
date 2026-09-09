---
"gh-aw": minor
---

Allow `engine: codex` workflows using a `copilot/` model to route inference through GitHub using Codex's BYOK support. Recommend Codex-capable models such as `copilot/gpt-5.3-codex`, and warn when a general-purpose Copilot model is selected because it may not support the capabilities Codex requires.
