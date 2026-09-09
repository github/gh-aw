# ADR-57814: Add First-Class Jira Safe Outputs

**Date**: 2026-09-02
**Status**: Draft
**Deciders**: pelikhan, adr-writer agent

---

### Context

This pull request extends `gh-aw` safe outputs so workflows and agents can perform a limited set of Jira write operations through the same controlled execution path already used for GitHub writes. The PR adds new safe-output tool schemas, dispatcher wiring, runtime handlers, tests, and documentation for creating Jira issues, updating Jira issues, adding Jira comments, and adding Jira labels. The PR body also states explicit constraints: Jira credentials must exist only in privileged post-processing, agent-facing inputs should remain simple text, and the initial integration excludes transitions, assignment, custom fields, label removal, JQL, bulk operations, and same-run references from newly created issues. Because this introduces a new external write surface and a new integration pattern for privileged side effects, the architectural choice should be recorded explicitly.

### Decision

We will add first-class Jira safe outputs as explicitly namespaced operations that run only through privileged safe-output processing rather than exposing arbitrary Jira API access to agents. The integration will accept bounded plain-text inputs, convert descriptions and comments to Atlassian Document Format internally, and execute a small supported set of Jira Cloud REST v3 mutations with sanitized validation and error handling. We chose this approach because it gives workflows a useful Jira automation surface while preserving the repository's existing safe-output security model and limiting the blast radius of a new third-party integration.

### Alternatives Considered

#### Alternative 1: Reuse Existing Generic GitHub-Oriented Safe Outputs or Unnamespaced Issue Tools

One option was to keep using the existing issue/comment/label concepts without adding Jira-specific namespacing or dedicated Jira handlers.

This was considered because it would reduce the number of new tools and avoid expanding the safe-output surface. It was not chosen because the PR evidence shows Jira requires separate credentials, a different API, different payload formats such as ADF, and explicit targeting guidance so GitHub and Jira operations are not confused.

#### Alternative 2: Expose a More General Jira REST Capability

Another option was to provide a generic Jira request tool or a broader set of Jira mutations such as transitions, assignments, custom fields, JQL, or same-run references to newly created issues.

This was considered because it would be more flexible and could reduce future incremental additions. It was not chosen because the PR explicitly scopes the integration to four bounded operations, adds strict schemas and sanitization, and avoids a broad privileged API surface that would be harder to validate, document, and secure.

### Consequences

#### Positive
- Workflows gain a supported way to create and update Jira content through the same controlled safe-output path used for other privileged writes.
- Jira credentials stay confined to privileged safe-output processing and are kept out of agent tools, prompts, output, and diagnostics.
- The integration is easier for agents to use because they provide ordinary text while the runtime handles Jira REST v3 details, ADF conversion, and safe error formatting.

#### Negative
- The codebase takes on a new external integration surface, including Jira-specific client logic, schemas, handlers, tests, and maintenance burden.
- The initial Jira capability is intentionally limited, so users needing transitions, assignments, custom fields, label removal, bulk operations, or same-run references will still need future follow-on work.
- Safe-output threat modeling and review complexity increase because the system now mediates writes to both GitHub and Jira with different semantics.

#### Neutral
- Jira operations are explicitly namespaced separately from GitHub issue and comment tools, making the distinction part of the product surface.
- Plain-text descriptions and comments are normalized into Atlassian Document Format internally instead of exposing ADF directly to agents.
- Detection, dispatch, documentation, and conformance coverage expand to include the new Jira-specific tool family.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
