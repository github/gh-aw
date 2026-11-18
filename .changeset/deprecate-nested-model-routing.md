---
"gh-aw": minor
---

Set top-level `model-routing: false` to disable implicit routing without pinning a model. Compilation now warns on `engine.model-routing` in workflows and shared imports. Existing `cost-speed` configurations still select for speed; choose a cost-oriented router level deliberately when migrating.
