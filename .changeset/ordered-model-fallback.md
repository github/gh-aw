---
"gh-aw": major
---

Add ordered `engine.fallback-models` with recovery handled entirely by AWF. Provision credentials for referenced providers, preserve guardrails, and attribute outputs to the model actually used. Require compatible AWF and API-proxy versions and concrete same-provider chains; reject unsupported configurations instead of restarting the agent through a harness.

Upgrade gh-aw-firewall from v0.28.37 to v0.28.44.

**⚠️ Breaking change:** Remove the implicit Codex `gpt-5.4` default. Workflows that relied on gh-aw to select `gpt-5.4` now use the Codex CLI/provider default unless configured otherwise.

**Migration guide:** To keep selecting `gpt-5.4` (or another fixed model), configure `engine.model`, a phase-specific `GH_AW_MODEL_*_CODEX` variable, or `GH_AW_DEFAULT_MODEL_CODEX`.
