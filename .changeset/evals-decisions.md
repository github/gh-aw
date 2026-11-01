---
"gh-aw": patch
---

Run BinEval evaluations through a standalone OpenAI-compatible Decisions client
instead of installing and invoking the main agentic engine. All questions share
one POST and use named YES/NO/UNKNOWN choices. The reusable JavaScript client also
supports predicate and score questions.

Evals now default to `gpt-6-luna`, independent of the agent model. Configure
`evals.model` with a Decisions-compatible model; prefix it with `openai/` to use
OpenAI credentials. Per-question model overrides are rejected, and each batch is
limited to 400 questions. Existing result artifacts, failure markers, redaction,
experiment assignments, and AWF accounting are preserved.
