---
"gh-aw": patch
---

Added specialized agent-failure guidance for Copilot authorization errors in workflows that use `permissions.copilot-requests: write`, including organization billing checks and the PAT fallback. Such failures are now reported under the `copilot_org_billing_error` category with a dedicated failure issue title.
