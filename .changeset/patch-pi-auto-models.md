---
"gh-aw": patch
---

Default Pi's Copilot backend to `copilot/auto` and accept the gateway's `auto` routing sentinel even when model discovery advertises only concrete models.

Support both `auto` and `copilot/auto` with Claude and Codex on GitHub inference. Claude selects an advertised Claude model for its native Messages API, and Codex retains the gateway picker without a spurious compatibility warning.
