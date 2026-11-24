---
"gh-aw": major
---

Approve Copilot CLI URLs from the finalized AWF network policy, including runtime caller additions, while preserving protocol restrictions, domain blocks, and shell permissions.

**Breaking change:** Compilation now rejects expression-valued Copilot provider URLs with an AWF pin older than v0.25.41 in all modes, even when `network.allowed` contains the provider host.

**Migration:** Remove both `sandbox.agent.version` and `sandbox.agent.checksums-sha256` to use the bundled default, upgrade to a supported AWF version with a matching trusted checksum when required, or use a literal `COPILOT_PROVIDER_BASE_URL`.
