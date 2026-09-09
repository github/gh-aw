# ADR-29483: Automate Removal of Deprecated Sandbox Keys via Codemods

**Date**: 2026-05-01
**Status**: Superseded
**Deciders**: Unknown

> Superseded by the restored non-strict opt-out policy: `sandbox.agent: false` is
> supported when `features.dangerously-disable-sandbox-agent: true` is set, and
> the removal codemod is no longer registered.

---

## Part 1 — Narrative (Human-Friendly)

### Context

The `gh-aw` platform introduced strict mode validation that rejects three sandbox configuration keys that were previously supported: `sandbox.mcp.container`, `sandbox.mcp.version`, and `sandbox.agent: false`. The MCP gateway container and version are now managed internally by the platform, making user-supplied values obsolete. Setting `sandbox.agent: false` was an escape hatch to disable agent sandbox firewall protections, which is no longer permitted in strict mode for security reasons. These keys were already flagged as deprecated, but the `gh aw fix` command could not remove them automatically, forcing maintainers to perform manual edits across 28+ files in official repositories.

### Decision

We will add three new codemods (`sandbox-mcp-container-removal`, `sandbox-mcp-version-removal`, `sandbox-agent-false-removal`) to the `GetAllCodemods()` registry so that `gh aw fix --write` can automatically remove these deprecated keys from workflow frontmatter. Each codemod applies a targeted line-level transform that removes only the specific key, leaving all sibling keys (e.g., `sandbox.mcp.port`, non-`false` `agent` values) intact. The `sandbox.agent: false` codemod is deliberately scoped to the boolean `false` value to avoid accidentally removing valid `agent` configurations such as object or string values.

### Alternatives Considered

#### Alternative 1: Documentation-Only Migration Guide

Provide a migration guide asking users to manually remove the deprecated keys. This was already the de-facto approach and produced the 28+ manual edits problem that motivated this PR. Manual migration is error-prone, time-consuming, and blocks users from enabling strict mode until edits are complete. It does not scale as more repos adopt the platform.

#### Alternative 2: Soft Deprecation with Warnings Only

Continue to accept the deprecated keys but emit deprecation warnings rather than errors. This was rejected because it would contradict the intent of strict mode — the keys were already classified as rejected in strict mode, and softening the enforcement would delay cleanup indefinitely. It would also leave security-sensitive behavior (`sandbox.agent: false` disabling the firewall) silently in place.

### Consequences

#### Positive
- Users can run `gh aw fix --write` to migrate all three deprecated keys in a single automated pass, eliminating manual edits across potentially dozens of files.
- Enforces the security posture of strict mode: `sandbox.agent: false` previously disabled the agent sandbox firewall; automated removal restores the secure default.
- The codemod pattern is surgical — sibling keys (e.g., `sandbox.mcp.port`) are preserved, reducing the risk of unintended data loss.

#### Negative
- Codemods make permanent file modifications; if applied without review, authors may lose context about why a key was present (e.g., a temporary workaround still in use).
- The `sandbox.agent: false` codemod only handles the boolean `false` case; any other representation of disabling the agent (e.g., a string `"false"`) would require a separate codemod or manual action.

#### Neutral
- The three codemods are added to the end of the `GetAllCodemods()` slice; the order is stable but appended, which shifts the total codemod count from 38 to 41 (reflected in the registry count test).
- Each codemod carries an `IntroducedIn: "0.26.0"` field that records the version in which the corresponding keys became deprecated.

---

## Part 2 — Normative Specification (RFC 2119)

> The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**, **SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **MAY**, and **OPTIONAL** in this section are to be interpreted as described in [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119).

### Codemod Registration

1. Each codemod for removing a deprecated sandbox key **MUST** be registered in `GetAllCodemods()` so that it runs as part of `gh aw fix --write`.
2. Codemods **MUST** be implemented as `Codemod` struct values with non-empty `ID`, `Name`, `Description`, and `IntroducedIn` fields.
3. Codemod `ID` values **MUST** be unique within the registry and **MUST** use kebab-case (e.g., `sandbox-mcp-container-removal`).

### Codemod Behavior

1. A codemod **MUST** return `(content, false, nil)` unchanged when the deprecated key is not present in the frontmatter — it **MUST NOT** modify files that do not need migration.
2. The `sandbox-mcp-container-removal` codemod **MUST** remove only the `container` key within `sandbox.mcp` and **MUST NOT** remove any sibling keys (e.g., `port`, `api-key`).
3. The `sandbox-mcp-version-removal` codemod **MUST** remove only the `version` key within `sandbox.mcp` and **MUST NOT** remove any sibling keys.
4. The `sandbox-agent-false-removal` codemod **MUST** remove the `agent` key from the `sandbox` block only when its value is the boolean `false`. It **MUST NOT** remove `agent` keys whose value is `true`, a string, or an object.
5. Codemods **MUST** preserve all content outside the removed key line, including markdown body content below the frontmatter delimiter.

### Conformance

An implementation is considered conformant with this ADR if it satisfies all **MUST** and **MUST NOT** requirements above. Failure to meet any **MUST** or **MUST NOT** requirement constitutes non-conformance.

---

*This is a DRAFT ADR generated by the [Design Decision Gate](https://github.com/github/gh-aw/actions/runs/25210508659) workflow. The PR author must review, complete, and finalize this document before the PR can merge.*
