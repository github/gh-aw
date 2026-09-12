---
"gh-aw": patch
---

Strip the `openai/` provider prefix from Codex model identifiers so `engine: codex` workflows declaring `model: openai/gpt-5.3-codex` pass a bare model name (`gpt-5.3-codex`) to the Codex CLI. Previously the prefixed name was forwarded verbatim and OpenAI rejected it with `model_not_supported_error`.
