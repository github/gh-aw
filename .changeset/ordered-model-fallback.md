---
"gh-aw": minor
---

Add ordered `engine.fallback-models` with AWF request-level recovery for same-provider models and shared harness recovery for cross-provider models. Provision credentials for every configured provider, preserve guardrails, and attribute outputs to the model actually used.

Upgrade gh-aw-firewall from v0.28.37 to v0.28.44.

Remove the implicit Codex `gpt-5.4` recovery default. Configure `engine.model`, a phase-specific `GH_AW_MODEL_*_CODEX` variable, or `GH_AW_DEFAULT_MODEL_CODEX` explicitly when a fixed model is required.
