# ADR-57796: Add Azure DevOps Work-Item Safe Outputs

**Date**: 2026-09-02
**Status**: Draft
**Deciders**: pelikhan, adr-writer agent

---

### Context

This pull request adds a new family of `ado_`-namespaced safe-output tools to `gh-aw` for creating, updating, commenting on, assigning, linking, and attaching files to Azure DevOps work items. The diff introduces a shared Azure DevOps handler, registers new safe-output message types, preserves namespaced public tool names, stages attachments through the existing artifact channel, and adds validation around allowed fields, tags, assignees, link types, area paths, iteration paths, URLs, symlinks, and Azure Pipelines command sequences. Because these changes extend the cross-system mutation surface of the safe-output pipeline beyond GitHub resources, the integration model and its guardrails should be recorded explicitly. The PR description also shows that the intended contract is configuration-driven policy enforcement rather than unconstrained arbitrary Azure DevOps API access.

### Decision

We will add Azure DevOps work-item operations to the safe-output pipeline as explicit `ado_`-namespaced tools backed by a shared handler and guarded by workflow configuration. The implementation will perform trusted REST calls through the existing action runtime, use temporary `#aw_` identifiers for same-run references, stage attachments before upload, and enforce configuration-scoped limits on which work items, fields, users, tags, links, and files may be mutated. We chose this approach because it extends `gh-aw`'s safe-output model to Azure DevOps while preserving the same explicit-tool, policy-first, and audit-friendly control surface used for other downstream side effects.

### Alternatives Considered

#### Alternative 1: Reuse Generic GitHub-Oriented Safe-Output Naming and Routing

Expose Azure DevOps work-item mutations through existing generic tool-routing patterns without dedicated `ado_` namespaced public tools.

This was considered because it would reduce the number of new public tool names and might reuse more of the existing registration flow. It was not chosen because the diff explicitly adds Azure DevOps-specific handlers, validation, and manifest metadata, and preserving namespaced tool names avoids normalization collisions while making the non-GitHub target system explicit in configuration and runtime behavior.

#### Alternative 2: Allow Arbitrary Azure DevOps API Access Through a More Generic Escape Hatch

Provide a looser integration that lets workflows send general Azure DevOps requests or mutate work items without per-operation configuration gates.

This was considered because it would be more flexible and could cover more Azure DevOps scenarios with less repository code. It was not chosen because the PR evidence emphasizes constrained operations: target enforcement, allowed tags, allowed assignees, allowed link types, trusted organization URLs, staged attachments, reserved identities, and rejection of unsafe paths and pipeline command sequences.

### Consequences

#### Positive
- `gh-aw` can now express Azure DevOps work-item side effects through first-class safe-output tools instead of ad hoc downstream scripting.
- The `ado_` namespace and shared handler make Azure DevOps operations explicit, auditable, and consistent across create, update, comment, assign, link, and attachment flows.
- Configuration-based restrictions limit the mutation surface and align the new integration with the repository's existing safe-output threat model.

#### Negative
- The safe-output system becomes more complex because it now has to preserve public names, manage Azure DevOps temporary IDs, and handle a second external work-tracking platform.
- The repository takes on ongoing maintenance for Azure DevOps-specific validation, request semantics, and attachment handling behavior.
- Misconfiguration risk increases because incorrect target, prefix, field, or file-policy settings could block legitimate work-item operations or create confusing failure modes.

#### Neutral
- Attachment uploads reuse the existing artifact staging path rather than introducing a separate transport channel.
- Tool registration now distinguishes normalized lookup keys from user-facing public names to support namespaced tools safely.
- Threat-review policy is extended so some Azure DevOps operations are reviewable while others are abort-on-warning, matching the differing mutation risk of each action.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
