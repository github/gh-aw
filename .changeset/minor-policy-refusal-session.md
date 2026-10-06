---
"gh-aw": minor
---

Normalize structured OpenAI and Anthropic policy refusals as `assistant.refusal` session events, including content-filtered responses with no answer text. Preserve refusal reasons, available text and policy details in unified sessions and display them separately from ordinary answers and execution errors.
