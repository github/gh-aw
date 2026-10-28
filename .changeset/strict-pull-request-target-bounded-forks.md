---
"gh-aw": major
---

Require explicit bounded fork admission for `pull_request_target` in strict mode.

Migration: replace omitted `on.pull_request_target.forks` or any `"*"` entry with `forks: []` for same-repository PRs only, exact `owner/repo` entries, or deliberately trusted `owner/*` patterns, then recompile. Expressions and other wildcard patterns are rejected. Actor authorization and trusted-checkout restrictions remain mandatory; `acknowledge-risk`, `checkout: false`, and trusted checkouts do not bypass fork admission. Non-strict behavior is unchanged.
