---
"gh-aw": patch
---

Add `engine: none` with a top-level `program:` to run a deterministic TypeScript or JavaScript program instead of an agent. Programs receive a typed SDK (`@github/gh-aw-program`) that wraps the work-queue tools; dispatcher and producer programs publish queue controls directly without an agent or `safe_outputs` job.
