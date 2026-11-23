# ADR-46207: Default-On Issue Intent Metadata for Handler Flags

**Date**: 2026-07-18
**Status**: Superseded by [ADR-80402](80402-make-issue-intent-experimental-opt-in.md) — `issue-intent` is now an experimental, opt-in feature (`issue-intent: true`) that is disabled by default and emits a compiler warning when enabled.
**Deciders**: Unknown

---

### Context

Three GitHub Action handlers — `set_issue_type`, `set_issue_field`, and `add_labels` — guarded intent metadata forwarding behind an explicit opt-in (`config.issue_intent === true`). When workflow frontmatter omitted `issue_intent`, the handlers silently discarded agent-supplied `rationale`, `confidence`, and `suggest` fields. Two other handlers (`close_issue`, `assign_to_agent`) already used the opposite convention: default-on with an explicit opt-out (`!== false`). This inconsistency meant agents relying on a minimal workflow config had their intent metadata silently dropped depending on which handler they invoked.

### Decision

We will change the `issue_intent` gate in `set_issue_type`, `set_issue_field`, and `add_labels` from opt-in (`=== true`) to opt-out (`!== false`). Omitting `issue_intent` from workflow frontmatter now forwards intent metadata through the GraphQL/intent-aware path by default, matching `close_issue` and `assign_to_agent`. For `add_labels` only, we will introduce an `issueIntentStrict` mode (`config.issue_intent === true`) that rejects plain-string label inputs and requires structured objects with metadata fields, enabling stricter enforcement when the feature is explicitly enabled. We will also add the `issue-intent` boolean property to the JSON schema for all three handlers so that `issue_intent: false` becomes a valid opt-out in workflow frontmatter.

### Alternatives Considered

#### Alternative 1: Keep Explicit Opt-In, Improve Documentation

Leave the `=== true` gate unchanged and add documentation or linter warnings reminding authors to set `issue_intent: true` when they want metadata forwarded. This avoids any breaking change risk but perpetuates the inconsistency — agents that omit `issue_intent` continue to lose metadata silently, and different handlers continue to behave differently for the same omitted config key.

#### Alternative 2: Global Default Configuration Key

Introduce a top-level `issue_intent_default` key in the workflow config that governs the default for all handlers, rather than changing per-handler logic. This provides configurability but adds a second layer of indirection, increases schema complexity, and still requires existing workflows to migrate if they want the new default without setting the global key.

### Consequences

#### Positive
- Intent metadata (rationale, confidence, suggest) is now forwarded by default across all supported handlers, eliminating silent data loss when frontmatter is minimal.
- Handler behavior is consistent: all intent-aware handlers share the same `!== false` opt-out convention, reducing cognitive load for workflow authors.
- The `issue-intent` schema addition makes opt-out (`issue_intent: false`) valid syntax in workflow frontmatter, closing a gap where opting out was impossible through standard schema-validated config.

#### Negative
- Existing workflows that relied on the `issue_intent` feature being off by default will now forward metadata unless they explicitly add `issue_intent: false` — a breaking behavior change requiring a migration step.
- The `issueIntentStrict` mode for `add_labels` (enabled when `issue_intent: true`) introduces a new failure path: plain-string label inputs are rejected outright, which may break callers that pass string labels while setting `issue_intent: true` for other reasons.

#### Neutral
- The `pkg/parser/schemas/main_workflow_schema.json` diff also reformats existing JSON object properties from compact single-line form to multi-line form; this is a cosmetic change with no behavioral effect but increases diff noise.
- Tests covering the new default-on behavior and strict-mode rejection were added inline alongside the handler changes.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
