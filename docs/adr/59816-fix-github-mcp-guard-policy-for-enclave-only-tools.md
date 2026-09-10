# ADR-59816: Fix GitHub MCP guard policy for enclave-only tools

**Date**: 2026-09-09
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

Compiled `gh aw` workflows can disable primary-agent GitHub access with `tools.github: false` while still enabling GitHub tools for an enclave agent. In that configuration, the compiler still renders a GitHub MCP server for the enclave identity, but the `determine-automatic-lockdown` step is not generated for the primary agent. The PR description and tests show that some compiler paths still referenced that missing step's outputs for server guard policies and environment variables, causing empty guard values and an `mcpg` startup failure requiring `min-integrity`. The implementation needs a consistent rule for when lockdown-step outputs may be referenced and how server-level guard policies should be produced for enclave-only GitHub access.

### Decision

We will centralize the predicate for generating and consuming GitHub lockdown-step outputs, and only reference those outputs when the `determine-automatic-lockdown` step is actually generated. For workflows that render a GitHub MCP server solely for a static enclave identity, we will emit a server-level guard policy that mirrors the enclave identity's allow-only policy instead of depending on absent primary-agent step outputs. We chose this approach because it preserves enclave-only GitHub access without broadening permissions, while eliminating runtime expansion to empty guard values.

### Alternatives Considered

#### Alternative 1: Always generate the lockdown detection step whenever any GitHub MCP server is rendered

The compiler could emit the `determine-automatic-lockdown` step for both primary-agent and enclave-only GitHub configurations. This was considered because it would let all downstream guard-policy consumers keep referencing the same step outputs. It was not chosen because enclave-only rendering does not need primary-agent lockdown detection semantics, and generating an extra step would preserve an accidental coupling instead of making the step-reference contract explicit.

#### Alternative 2: Remove server-level guard policies for enclave-only GitHub backends

Another option would be to omit the GitHub server's server-level guard block whenever only an enclave identity can access that backend, relying entirely on gateway agent policies. This was considered because it avoids step-output references and minimizes compiler logic. It was not chosen because the PR explicitly preserves a matching server-level guard policy for static enclave delegation, which provides a defense-in-depth constraint and prevents the server configuration from becoming broader than the enclave identity policy.

#### Alternative 3: Keep existing behavior and special-case empty guard values at runtime

The runtime could tolerate empty `repos` or `min-integrity` values, or the compiler could emit placeholder defaults when the step is absent. This was considered because it might be a smaller patch around the immediate crash. It was not chosen because the root problem is an invalid compile-time contract between step generation and step consumers, and silently defaulting guard values could hide misconfiguration or accidentally weaken access controls.

### Consequences

#### Positive
- Enclave-only GitHub tool configurations no longer produce invalid empty guard-policy values at runtime.
- The compiler uses one shared predicate for deciding when lockdown-step outputs may be generated and consumed, reducing internal inconsistency.
- Static enclave-only GitHub backends get a server-level guard policy that matches the enclave identity policy, preserving least privilege.

#### Negative
- The compiler now carries additional branching for primary-agent, dynamic-enclave, and static-enclave GitHub rendering modes.
- Guard-policy behavior is spread across several helper functions, which increases the number of internal contracts maintainers must understand.
- Future GitHub MCP changes must preserve the shared predicate or risk reintroducing mismatches between step generation and guard consumers.

#### Neutral
- The change extracts helper functions for configured guard values and parse-guard-vars inputs without changing their user-facing configuration model.
- Test coverage now includes an enclave-only compilation case and helper-level assertions about when step-based guard policies apply.
- The generated server-level policy for static enclave delegation mirrors existing enclave `allow-only` fields, including `repos` and `min-integrity`.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
