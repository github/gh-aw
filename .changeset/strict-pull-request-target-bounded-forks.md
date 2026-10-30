---
"gh-aw": major
---

Default `pull_request_target` to same-repository PRs only and warn about unrestricted forks in strict mode.

Migration: omit `on.pull_request_target.forks` for same-repository PRs only (`forks: []` remains equivalent but is not required). Workflows that need fork PRs must explicitly configure admission and recompile. Prefer exact `owner/repo` entries or deliberately trusted `owner/*` patterns. Explicit `"*"` allows all forks in both modes; strict mode warns and recommends restricting forks instead of rejecting compilation. Strict mode still rejects expressions and unsupported pattern forms. Actor authorization and trusted-checkout restrictions remain mandatory in strict mode; `acknowledge-risk`, `checkout: false`, and trusted checkouts neither bypass them nor suppress the unrestricted-forks warning.
