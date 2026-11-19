# ADR-78937: Extend Declarative Connectors for Foundry IQ Retrieval

**Date**: 2026-11-18
**Status**: Draft
**Deciders**: Pending maintainer review; no approval recorded

---

### Context

[ADR-74077](74077-declarative-connectors-replace-native-provider-integrations.md)
defines the shared declarative connector architecture. Foundry IQ retrieval
originally used a native `queries` string array and requires a fixed API-version query parameter
on its hosted HTTP MCP endpoint, neither covered by that record's input contract.
[PR #78937](https://github.com/github/gh-aw/pull/78937) extends that contract rather
than introducing a provider-specific runtime handler. The integration must retain
exact authored grants, validation before credential access, and read-only scope.

### Decision

Extend the shared hosted HTTP MCP contract with explicitly bounded string arrays
and one literal `api-version` URL query parameter, as specified in
[Connector Specification 1.1.0](../src/content/docs/specs/connector-specification.md).
Validate every item of a resource-constrained array against its exact authored
grant before credential resolution or forwarding; one approved item cannot
authorize another.
Keep arbitrary objects, nested arrays, other URL query parameters, fragments,
dynamic endpoints and credential fallback unsupported.

Use the experimental
[`azure-foundry-iq` declaration](../../.github/workflows/connectors/azure-foundry-iq.md)
to expose only `knowledge_base_retrieve` on one compile-time-fixed Search service
and knowledge base, with literal API version `2026-11-01-preview`. Require exactly
one approved question per call, bounded to 1 through 150 Unicode code points,
and cap responses at 256 KiB. Token acquisition and renewal remain external;
the connector performs no Azure token exchange, resource provisioning or provider
mutation. Its golden consumer stages a GitHub issue independently of retrieval.

The preview server now advertises `query_variants` instead of `queries`, with a
150-character maximum. The import allowlist remains named `queries`; each native
call maps `query_variants` to that exact grant. The server also requests fewer
than 12 words per question.

### Alternatives Considered

#### Copilot CLI plugin or provider-specific runtime handler

A plugin or native handler could implement the provider's retrieval and
authentication behavior directly. It would also add a separate integration path
instead of reusing the shared compiler, exact grants and hosted MCP adapter.
The declaration fits the existing architecture with bounded contract extensions,
so no plugin installation or provider-specific runtime is introduced.

#### Scalar query alias or JSON-string wrapper

A scalar input could preserve the former primitive-only connector input shape.
It would not match the provider's native array argument without an additional
translation layer. Keep the native typed array rather than inventing a scalar
alias or accepting opaque serialized JSON.

### Consequences

#### Positive

- Provider configuration remains reviewable declaration source and uses the
  existing grant-checked adapter rather than a separate runtime integration.
- Native typed arguments retain explicit collection and item bounds, with
  unauthorized questions rejected before credential access or network calls.

#### Negative

- Shared array and URL validation increase Go/Node compatibility obligations;
  boundary and parity tests must evolve with the contract.
- External token expiry or missing provider permissions can prevent retrieval;
  offline conformance does not establish live compatibility or answer quality.

#### Neutral

- Retrieval can disclose private grounding to the agent and incur Azure/model
  charges even when the subsequent GitHub issue is staged. Disclosure approval,
  source ACLs and service/model configuration remain external responsibilities.
- Returned content remains untrusted evidence. The adapter does not independently
  verify document identity, freshness, completeness or sensitivity-label policy.
- This draft records implementation rationale, not maintainer approval or
  authorization for live execution. Review is required before acceptance.
