---
"gh-aw": minor
---

Add `adaptive-routing: observe`, which records every eligible run of a routed workflow in its routing history without changing which model runs.

A Copilot, Claude, or Codex workflow whose main agent's model is chosen by model routing, and that enables `tools.repo-memory`, can set `adaptive-routing: observe` (or inherit the `adaptive_routing` default from `aw.json`). A new `push_routing_history` job runs after `conclusion` with `contents: write` and `actions: read`. It reads the run's routing verdict, the model and effort AWF selected, and the AWF-priced cost from the unified session. It saves them to `memory/adaptive-routing/<workflow>` through repo-memory's save loop, which gains an option that applies the run again to the newest files when another run saved first. `gh aw routing status` shows the recorded evidence and `gh aw routing reset` starts a history afresh. Repo-memory branches under `memory/adaptive-routing/` are reserved. Workflows that do not opt in compile exactly as before.
