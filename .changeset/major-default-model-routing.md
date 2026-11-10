---
"gh-aw": major
---

Default eligible workflows with no model or routing choice to `goal: cost`,
`mode: balanced` model routing on GitHub Copilot inference.

**⚠️ Breaking Change**: After recompilation, eligible workflows may select a
different model, changing their output and cost.

**Migration guide:**
- To keep the previous non-routed model selection, set
  `engine.model-routing: false` and recompile.
- To keep a stable model, explicitly configure the intended model and recompile.
- Explicit routing configurations remain unchanged. Review generated locks and
  audit model and cost changes after recompilation.
