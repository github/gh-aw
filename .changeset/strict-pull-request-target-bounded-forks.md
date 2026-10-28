---
"gh-aw": major
---

Default `pull_request_target` to same-repository PRs only and require bounded fork patterns in strict mode.

Migration: omit `on.pull_request_target.forks` for same-repository PRs only (`forks: []` remains equivalent but is not required). Workflows that need fork PRs must list exact `owner/repo` entries or deliberately trusted `owner/*` patterns, then recompile. Strict mode rejects any `"*"` entry, expressions, and other wildcard patterns; non-strict mode permits explicitly using `"*"` for unrestricted admission. Actor authorization and trusted-checkout restrictions remain mandatory in strict mode; `acknowledge-risk`, `checkout: false`, and trusted checkouts do not bypass fork admission.
