---
"gh-aw": patch
---

Ignore package manifest self-imports (for example `./aw.yml` inside a nested `child/aw.yml`) with a warning instead of failing with a confusing import cycle error, so `gh aw add` still installs the package root files.
