---
"gh-aw": patch
---

Record component execution evidence as `not_started` when AWF fails before the engine harness starts, so the daily AI credits guardrail can prove zero usage for such runs instead of failing closed on missing usage accounting files.
