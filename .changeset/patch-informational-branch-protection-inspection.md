---
"gh-aw": patch
---

Keep the configurable pre-push branch-protection gate while using only branch metadata and active rules, never the classic-protection API requiring Administration permission. When the gate is enabled, detected restrictive protection and inspection failures block the push. Setting `check-branch-protection: false` skips the protection gate and leaves enforcement to GitHub. Preserve the default-branch guard, file allowlists, and the original push failure, retry, and fallback behavior.

Disable the protection gate explicitly for the PR Specification Reviewer while retaining its Markdown specification-only allowlist.
