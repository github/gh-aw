---
"gh-aw": patch
---

Attempt PR-branch pushes optimistically and inspect branch protection only after a push fails, relying on GitHub to enforce permissions and protection rules. Diagnostics use branch metadata and active rules, never the classic-protection API requiring Administration permission. Inspection failures are informational and do not require broader credentials. Preserve the default-branch guard, file allowlists, and the original push failure, retry, and fallback behavior.
