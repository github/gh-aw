---
"gh-aw": patch
---

Add a typed `OnStopAfter` field to `FrontmatterConfig` so `on.stop-after` is no longer read only from the dynamic frontmatter map, and allow `on.stop-after` to be a GitHub Actions expression (e.g. `${{ inputs.stop-after }}`) that is resolved at workflow runtime.
