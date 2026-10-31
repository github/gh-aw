# ADR-73534: Guard Behavior-Defined Engine Network Defaults

**Date**: 2026-10-31
**Status**: Draft
**Deciders**: gh-aw maintainers (review pending)

---

### Context

Behavior-defined engines declare network defaults and provider domains in shared workflow definitions, but the existing package-registry and vendor-domain overlap checks covered only static engine sets. As a result, Aider's default PyPI hosts bypassed the explicit Python ecosystem opt-in, while Kiro's defaults included optional telemetry, an unexplained asset host, and a broad `*.kiro.dev` wildcard. The generated allow-lists therefore granted network access that was not consistently justified or required for engine correctness. The change must preserve the concrete API, authentication, and CLI-download endpoints needed by Kiro while making optional services and package registries explicit.

### Decision

We will apply the same ecosystem-overlap guard to behavior-defined engine `network.defaults` and `provider-domains` that applies to static engine defaults. Engine definitions will keep only concrete hosts required for correctness by default; package registries, telemetry, and other optional services require explicit `network.allowed` or runtime opt-in. The compiler/test suite will load shared behavior-defined engine definitions and regression-test both the default deny behavior and the explicit opt-in paths.

### Alternatives Considered

#### Alternative 1: Keep the existing defaults and document each domain

This would minimize immediate behavior changes and could explain why each host is present. It was not chosen because the PR evidence shows that Kiro telemetry is not a correctness dependency, the asset host is unexplained, the wildcard is broader than the concrete API/download hosts, and Aider's PyPI access conflicts with the existing explicit package-ecosystem opt-in rule.

#### Alternative 2: Remove all engine-specific network defaults

This would make every additional host an importing-workflow decision and maximize least privilege. It was not chosen because behavior-defined engines still need concrete engine endpoints for normal operation, including Kiro's API, authentication, and pinned CLI download hosts; requiring those on every import would make the engine definitions needlessly difficult to use.

### Consequences

#### Positive

- Behavior-defined engines can no longer bypass package-registry and related domain guards through shared workflow frontmatter.
- Default allow-lists are narrower and auditable: required Kiro endpoints remain available, while telemetry, optional assets, broad provider wildcards, and Aider's PyPI hosts are not granted implicitly.
- Explicit `network.allowed` and runtime opt-ins preserve deliberate access to package registries and Kiro telemetry when a workflow needs them.

#### Negative

- Workflows that implicitly relied on removed optional domains may fail until they declare the corresponding explicit opt-in.
- The guard and regression tests must parse every shared behavior-defined engine definition, adding test maintenance whenever the frontmatter schema changes.
- Kiro smoke and conformance workflows need recompilation so generated lock files reflect the narrower defaults.

#### Neutral

- Generated `.lock.yml` files change as a consequence of recompiling their source workflow definitions; they remain derived artifacts rather than independent configuration.
- The policy distinguishes correctness dependencies from telemetry and other optional services, so future engine additions must justify which category each new domain belongs to.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
