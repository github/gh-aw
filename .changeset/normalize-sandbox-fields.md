---
"gh-aw": major
---

Normalize camel-cased `sandbox` frontmatter fields to kebab-case.

**Breaking change:** Camel-cased sandbox fields such as `allowWrite`, `authHeader`, and `entrypointArgs` are no longer accepted by the parser. Run `gh aw fix` to migrate existing workflows to fields such as `allow-write`, `auth-header`, and `entrypoint-args`.
