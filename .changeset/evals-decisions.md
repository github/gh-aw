---
"gh-aw": patch
---

Run BinEval evaluations through a standalone OpenAI-compatible Decisions client
instead of installing and invoking the main agentic engine. All questions share
one POST and use named YES/NO/UNKNOWN choices. The reusable JavaScript client also
supports predicate and score questions.

Evals now default to `copilot/gpt-6-luna-decisions`, independent of the agent model. Configure
`evals.model` with a Decisions-compatible model; prefix it with `openai/` to use
OpenAI credentials. Per-question model overrides are rejected, and each batch is
limited to 400 questions. Existing result artifacts, failure markers, redaction,
and experiment assignments are preserved. Evals call the controlled endpoint
directly without installing AWF or starting a proxy or sandbox. Response token
usage, including cache and reasoning tokens, is recorded with catalog-derived
AIC and USD costs for summaries and daily accounting. AWF credit limits and
sandbox target overrides no longer apply to evals.

Both providers use `/v1/decisions`. Configure the actual model ID:
`copilot/gpt-6-luna-decisions` for Copilot or `openai/gpt-6-luna` for OpenAI.
Model IDs are sent without provider-specific name mapping.
