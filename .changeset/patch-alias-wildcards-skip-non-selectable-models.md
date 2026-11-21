---
"gh-aw": patch
---

Alias wildcards now skip models that AWF's `/reflect` marks `automatic_selection_eligible: false`, so `model: auto` and other aliases no longer pick a model that can't run an agent, such as the Decisions-only `gpt-6-luna-decisions` or `gpt-5.6-luna-utility`, which is disabled in the Copilot model picker. Claude's `auto` on the Copilot provider follows the same rule. Exact model ids and alias entries without `*`, such as `evals`, resolve as before, and so do runs on an AWF release that doesn't publish the field (it was added in v0.29.16).
