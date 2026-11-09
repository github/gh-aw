---
"gh-aw": patch
---

Capture plaintext reasoning from LiteLLM's `provider_specific_fields.reasoning_text` in the Aider sample adapter when top-level reasoning is absent. Preserve explicit empty strings and existing reasoning precedence without promoting opaque provider data to plaintext reasoning events.
