---
"gh-aw": patch
---

Fix `safe-outputs.staged: true` combined with a `safe-outputs.github-app` compiling a dangling `steps.safe-outputs-app-token` reference. When no enabled handler consumes the global GitHub App (all handlers staged, or a Linear-only configuration) the app token minting step is skipped, so token expressions now fall back to the regular safe-output token instead of resolving to an empty value and failing with "Input required and not supplied: github-token".
