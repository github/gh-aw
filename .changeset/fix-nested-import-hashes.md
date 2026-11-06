---
"gh-aw": patch
---

Resolve nested local imports consistently in compiler and activation hashes.
Qualified paths use the root workflow directory; bare names and `./` paths remain
relative to the importing file. This avoids requests for nonexistent doubled
directories such as `shared/shared` and includes previously omitted nested
frontmatter, template expressions, and bodies in lock verification. Recompile
affected workflows when upgrading to refresh their corrected lock hashes.
