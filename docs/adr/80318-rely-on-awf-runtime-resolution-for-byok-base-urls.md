# ADR-80318: Rely on AWF Runtime Resolution for Expression-Valued BYOK Base URLs

**Date**: 2026-11-23
**Status**: Accepted

## Context

An Azure AI Gateway BYOK pilot reported (finding F3) that `engine.env.COPILOT_PROVIDER_BASE_URL` set from `${{ vars.* }}` or `${{ secrets.* }}` was not added to the compile-time AWF firewall allowlist. The compiler adds only literal hosts. The docs told users to add the provider host to `network.allowed` by hand, which encouraged broad entries such as `openai.azure.com`.

AWF v0.25.41 and later already handle this at runtime. AWF derives the Copilot API target from the runtime value of `COPILOT_PROVIDER_BASE_URL` and adds exactly its `scheme://host` to the allowlist, for both the main agent run and the threat-detection run. `OPENAI_BASE_URL` and `ANTHROPIC_BASE_URL` are passed to the AWF API proxy as target hosts and are also resolved at runtime. The default pinned AWF version is v0.29.18.

Forces:

- Broad `network.allowed` entries widen egress beyond the configured provider host.
- Workflows with expression-valued URLs, such as the `smoke-copilot-aoai-*` workflows, already work on the default AWF and must keep compiling.
- A workflow that pins AWF below v0.25.41 has its model requests blocked at runtime, and nothing at compile time reported this.
- A host known only at runtime cannot be compared with `network.allowed` entries at compile time.

## Decision

Rely on AWF runtime host resolution for expression-valued BYOK base URLs. Fix the docs and add a narrow compile-time diagnostic for the case AWF cannot handle.

- The docs (including `reference/engines/copilot.md`) state that AWF allows exactly the runtime provider host for both runs, warn against broad `network.allowed` entries, and state the AWF v0.25.41 minimum.
- `pkg/constants/version_constants.go` defines `AWFCopilotProviderHostAllowlistMinVersion = "v0.25.41"`.
- `pkg/workflow/byok_provider_host_validation.go` reports a diagnostic only when all three hold:
  1. `engine.env.COPILOT_PROVIDER_BASE_URL` contains a `${{ … }}` expression;
  2. the firewall is enabled; and
  3. the effective AWF version is older than v0.25.41.
- The diagnostic is an error in all modes. It recommends removing the `sandbox.agent.version` pin or raising it to v0.25.41 or later, or using a literal provider URL so the compiler can allow its host.
- `network.allowed` entries do not suppress the diagnostic, because the compiler cannot verify that any entry covers a host known only at runtime. An earlier revision suppressed the diagnostic when any custom domain was present. Review changed this.

## Alternatives Considered

- **Keep telling users to add the provider host to `network.allowed`.** Rejected. This guidance produced the broad entries the pilot reported.
- **Fail compilation for every expression-valued base URL.** Rejected. It would break working workflows such as `smoke-copilot-aoai-*` and push users toward broader allowlists.
- **Resolve the provider host at compile time.** Rejected. The compiler cannot read secret values, so this is impossible.
- **Add a new runtime allowlisting mechanism in gh-aw.** Rejected. AWF already does this.
- **Suppress the diagnostic when `network.allowed` has any custom domain.** Rejected. The compiler cannot verify that an unrelated domain covers the runtime host, so the suppression is unsound.

## Consequences

Positive:

- Users no longer need broad `network.allowed` entries. Egress stays limited to the exact configured provider host.
- Workflows on the default AWF get no new diagnostics, and compiled lock files do not change.
- Workflows that pin AWF below v0.25.41 get a compile-time signal instead of a silent runtime block.

Negative:

- Workflows that pin AWF below v0.25.41 must upgrade AWF or use a literal provider URL, even if they already list the exact provider host in `network.allowed`.
- `AWFCopilotProviderHostAllowlistMinVersion` must stay correct as AWF changes.
- AWF does not mark a secret-derived Copilot target host as sensitive, so the hostname may appear in AWF logs. This is tracked as follow-up work.
- The connector inference path (#80315) is handled separately and is not affected by this decision.
