---
"gh-aw": patch
---

Allow a nested package manifest to import a manifest above it (for example `../aw.yml`), bounded by the repository root, and ignore self-imports such as `./aw.yml` with a warning instead of reporting an import cycle.
