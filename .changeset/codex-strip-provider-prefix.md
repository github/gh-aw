---
"gh-aw": patch
---

Strip any known LLM provider prefix (such as `openai/`) from the model passed to the Codex CLI, not just `copilot/`. Codex rejected provider-scoped identifiers like `openai/gpt-5.3-codex`, silently fell back to generic model metadata, and ran with an unintended model.
