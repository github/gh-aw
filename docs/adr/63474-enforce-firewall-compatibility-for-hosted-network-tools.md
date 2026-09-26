# ADR-63474: Enforce firewall compatibility for hosted network tools

**Date**: 2026-09-25
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

The compiler currently allows workflows to combine restricted `network` policies with the `copilot` engine and built-in `web-fetch` or `web-search` tools, even though the PR description states those capabilities can reach network resources outside the configured firewall boundary. This creates a misleading security contract: authors can believe firewall restrictions are enforced while hosted components still retain broader network access. The diff shows the required behavior is mode-dependent: strict workflows must reject incompatible configurations, while non-strict workflows must warn and continue. The implementation also needs to evaluate merged imports and keep smoke and fixture coverage aligned with the new policy behavior.

### Decision

We will treat the `copilot` engine and the `web-fetch` and `web-search` tools as incompatible with restricted firewall policies, and validate that incompatibility during workflow compilation. In strict mode, compilation will fail with targeted validation errors; in non-strict mode, compilation will emit warnings and continue. We will apply the same validation after merging imported network policy and tool configuration so imported workflows cannot bypass the check. We chose this because the PR evidence shows these hosted capabilities are not firewall-bound, so the compiler must surface that limitation explicitly instead of silently accepting an unsafe configuration.

### Alternatives Considered

#### Alternative 1: Continue allowing these configurations without validation

This was the status quo and would avoid compiler changes, fixture updates, and additional warnings. It was not chosen because the PR description explicitly identifies a policy gap where users can configure network restrictions that do not actually constrain the Copilot engine or hosted web tools, which would leave the product with an inaccurate security guarantee.

#### Alternative 2: Reject incompatible configurations in all modes

This was plausible because it would provide the strongest enforcement model and the simplest rule to explain. It was not chosen because the PR description and tests require different behavior for strict and non-strict workflows, preserving backward compatibility by warning in non-strict mode while still allowing teams to opt into hard enforcement.

### Consequences

#### Positive
- Restricted-network workflows now get explicit feedback when they use hosted capabilities that are not bound by firewall policy.
- Strict mode becomes a reliable enforcement mechanism for this compatibility rule instead of allowing silently unsafe combinations.
- Imported tool and network settings are validated together, reducing the risk that composition hides the policy mismatch.

#### Negative
- Existing Copilot-based restricted-network tests and smoke fixtures must be updated to use `strict: false` or a firewall-compatible engine, increasing maintenance effort.
- Users must understand why some engines and tools are warning-only in non-strict mode but fatal in strict mode, which adds policy complexity.
- The compiler now owns another compatibility matrix between workflow network settings and specific engines/tools.

#### Neutral
- Warning counts become part of expected behavior for certain non-strict workflows and tests.
- Reusable smoke workflows and generated lock files need to reflect whether they are compiled in strict or non-strict mode.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
