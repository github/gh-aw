# ADR-80318: Rely on AWF Runtime Resolution for Expression-Valued BYOK Base URLs

**Date**: 2026-11-23
**Status**: Draft

## Context

An Azure AI Gateway BYOK pilot reported (F3) that `engine.env.COPILOT_PROVIDER_BASE_URL` set from `${{ vars.* }}` or `${{ secrets.* }}` was not added to the AWF firewall allowlist. Users worked around this by adding hosts to `network.allowed` by hand, often with broad entries such as `openai.azure.com`.

The compiler cannot allowlist a host it cannot read at compile time. Per the PR description, AWF v0.25.41 and later resolve `COPILOT_PROVIDER_BASE_URL` at runtime and allow exactly that `scheme://host`, for both the agent run and the threat-detection run. The pinned AWF default is v0.29.18. The AWF runtime behavior comes from the PR description and is not exercised by tests in this repository.

Forces:

- Broad `network.allowed` entries widen the egress policy beyond the configured provider host.
- Existing workflows with expression-valued URLs already work on the default AWF and must not start failing compilation.
- Workflows that pin an AWF version older than v0.25.41 are blocked at runtime, and nothing at compile time signals that.

## Decision

Rely on AWF runtime host resolution for expression-valued `COPILOT_PROVIDER_BASE_URL` on AWF v0.25.41 or later. Do not add compile-time allowlisting for expressions. Concretely:

- Docs (`engines/copilot.md`, `glossary.md`, `azure-ai-gateway.md`, `auth.mdx`) state that the provider host is allowed at runtime, warn against broad `network.allowed` entries, and note the AWF v0.25.41 minimum.
- `pkg/constants/version_constants.go` adds `AWFCopilotProviderHostAllowlistMinVersion = "v0.25.41"`.
- `pkg/workflow/byok_provider_host_validation.go` adds `byokProviderHostAllowlistGap` and `validateBYOKProviderHostAllowlist`. It is called from `validateCoreToolConfiguration` in `pkg/workflow/compiler_validators.go`.
- The validator warns, or fails in strict mode, only when all four hold: the base URL is an expression; the firewall is enabled; the pinned AWF version is below v0.25.41; and `network.allowed` has no custom literal domain.

## Alternatives Considered

- **Keep the manual guidance and tell users to add the provider host to `network.allowed`.** Rejected. This is the status quo that produced the broad entries reported by the pilot.
- **Fail compilation for every expression-valued base URL.** Rejected. It would break working workflows in this repository and push users toward broader allowlists. The narrow check is expected to leave `make recompile` lock-file output unchanged.
- **Allowlist the provider host at compile time.** Rejected. The compiler sees only the expression, and it can allowlist only literal hosts.

## Consequences

Positive:

- Users no longer need broad `network.allowed` entries, so egress stays limited to the exact configured provider host.
- Workflows on the default AWF get no new warnings and no lock-file changes.
- Workflows that pin AWF below v0.25.41 get a compile-time signal instead of a silent runtime block.

Negative:

- Workflows that pin AWF below v0.25.41 still compile with only a warning in non-strict mode, and their model requests stay blocked until the pin is raised or the host is added to `network.allowed`.
- The validator depends on `AWFCopilotProviderHostAllowlistMinVersion`, which must track AWF releases.
- Known gaps remain and are out of scope: `gemini_engine.go` allowlists `GEMINI_API_BASE_URL` even when it is an expression; `GH_AW_ENGINE_API_HOSTS` receives the raw expression; and AWF does not mark a secret-derived host as sensitive, so the hostname may appear in AWF logs.
