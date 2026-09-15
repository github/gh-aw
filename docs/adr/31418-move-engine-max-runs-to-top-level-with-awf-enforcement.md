---
name: ADR 31418 — Move engine.max-runs to top-level max-runs with AWF enforcement
description: Canonicalize run-cap configuration as top-level `max-runs` and route enforcement through AWF (`apiProxy.maxRuns`) for all engines.
type: project
---

# ADR-31418: Move `engine.max-runs` to Top-Level `max-runs` with AWF Enforcement

**Date**: 2026-05-11
**Status**: Draft
**Deciders**: Unknown

---

## Part 1 — Narrative (Human-Friendly)

### Context

Run-cap configuration was previously expressed as the nested frontmatter field `engine.max-runs`, and enforcement was handled inconsistently across engines (Copilot/Claude/Codex/Gemini/Crush/OpenCode). The new AWF (Agent Workflow Framework) API proxy already centralizes other invocation-time policies (for example, `apiProxy.maxAiCredits` and `apiProxy.modelMultipliers`), so it is the natural enforcement point for invocation caps as well. Without a single canonical place to declare the cap and a single enforcement point, behavior drifts per engine and the field is effectively unsupported on engines that have not implemented it. The repository already ships a codemod framework that can rewrite deprecated frontmatter automatically, which makes a deprecation path low-friction for existing workflows.

### Decision

We will canonicalize the run-cap configuration as a **top-level** frontmatter field `max-runs` and **route enforcement through AWF** by emitting it as `apiProxy.maxRuns` in the AWF config. The value is parsed once at the top level (accepting either an integer or a numeric string), stored on `EngineConfig.MaxRuns`, and forwarded to AWF; `engine.max-runs` is deprecated and migrated automatically via a new codemod (`engine-max-runs-to-top-level`). The field is omitted from `apiProxy` when unset so that no implicit cap is written.

### Alternatives Considered

#### Alternative 1: Keep `engine.max-runs` and implement per-engine enforcement

Continue to expose the cap as a nested `engine.max-runs` field and have each engine wrapper enforce it. Rejected because this is the status quo that motivated the change: enforcement diverges between engines, several engines do not implement the cap at all, and there is no single place to reason about invocation limits.

#### Alternative 2: Dual-support both `engine.max-runs` and top-level `max-runs`

Accept both forms indefinitely and have the compiler merge them. Rejected because long-term dual support adds documentation surface, parsing precedence rules, and ongoing user confusion. The existing codemod infrastructure (see `pkg/cli/fix_codemods.go`) lets us migrate in place automatically, so the cost of deprecation is small.

#### Alternative 3: Introduce a new name (e.g., `awf.maxRuns` or `invocation-cap`) at the top level

Pick a different top-level name to signal the AWF-routed semantics. Rejected because users already think of the value as `max-runs`; renaming would force a larger documentation churn and a more disruptive codemod without changing behavior. The internal AWF field is already named `maxRuns`, so the mapping is transparent.

### Consequences

#### Positive
- Run-cap enforcement is uniform across all engines because it is applied by the AWF API proxy, eliminating per-engine drift.
- Workflow authors have a single, discoverable top-level field (`max-runs`) instead of a nested engine option.
- Existing workflows are migrated automatically by the `engine-max-runs-to-top-level` codemod, so the deprecation is low-friction.
- The schema, autocomplete metadata, and engine-feature table now advertise the cap as supported by every engine, which matches the new reality.

#### Negative
- `engine.max-runs` is now deprecated; users on older workflow files who do not run `fix` will see the nested field silently removed by the codemod next time it runs, which is a behavior change.
- A new top-level frontmatter key expands the public configuration surface and must be kept in sync across schema, autocomplete, docs, parser, AWF config emitter, and codemods (six artifacts updated in this PR).
- Enforcement now depends on AWF being in the request path; engines that bypass AWF would silently ignore `max-runs`. The PR assumes AWF is always present for runs that need the cap.

#### Neutral
- The omission semantics are preserved: when `max-runs` is unset, no `apiProxy.maxRuns` key is emitted (verified by `TestBuildAWFConfigJSON/max-runs omitted when not configured`).
- The codemod preserves an already-present top-level value when both nested and top-level forms exist, choosing the top-level value (verified by `TestEngineMaxRunsToTopLevelCodemod_RespectsExistingTopLevel`).
- Both integer and numeric-string inputs are accepted, matching the existing convention used for `max-ai-credits`.

---

## Part 2 — Normative Specification (RFC 2119)

> The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**, **SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **MAY**, and **OPTIONAL** in this section are to be interpreted as described in [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119).

### Frontmatter Contract

1. The canonical run-cap field **MUST** be the top-level frontmatter key `max-runs`.
2. The value of `max-runs` **MUST** be either a positive integer or a numeric string that parses to a positive integer (minimum `1`).
3. The compiler **MUST** reject or ignore values of `max-runs` that are not positive integers; non-positive or non-numeric values **MUST NOT** be propagated to `EngineConfig.MaxRuns`.
4. The compiler **MUST NOT** require `max-runs` to be set; it is **OPTIONAL** frontmatter.

### Engine Configuration Wiring

1. `EngineConfig` **MUST** carry the parsed run-cap value on the `MaxRuns` field.
2. `EngineConfig.MaxRuns` **MUST** be sourced from the top-level frontmatter `max-runs`, not from any nested `engine.max-runs` field.
3. `EngineConfig.GetMaxRuns()` **MUST** return `0` when `MaxRuns` is unset or non-positive.
4. The compiler **SHOULD** populate `MaxRuns` for every engine extraction path (string form, inline form, and named-engine form) so that the field is available regardless of how the engine is declared.

### AWF API Proxy Emission

1. When `EngineConfig.GetMaxRuns()` returns a positive integer, `BuildAWFConfigJSON` **MUST** emit `apiProxy.maxRuns` with that value.
2. When `EngineConfig.GetMaxRuns()` returns `0`, `BuildAWFConfigJSON` **MUST NOT** emit the `maxRuns` key in the `apiProxy` section.
3. The emitted JSON key **MUST** be exactly `maxRuns` (camelCase), matching the existing `apiProxy` field naming convention.

### Deprecation and Migration

1. The nested field `engine.max-runs` **MUST** be treated as deprecated.
2. The codemod `engine-max-runs-to-top-level` **MUST** be registered in `GetAllCodemods()` and **MUST** run after `engine-steps-to-top-level` and before `steps-run-secrets-to-env` to preserve the established codemod ordering.
3. When applied, the codemod **MUST** remove `engine.max-runs` from the nested `engine` block.
4. When a top-level `max-runs` is absent, the codemod **MUST** insert a top-level `max-runs` line carrying the migrated value.
5. When a top-level `max-runs` already exists, the codemod **MUST** preserve the existing top-level value and **MUST NOT** overwrite it with the nested value.
6. The codemod **MUST** be a no-op when neither `engine.max-runs` nor a top-level `max-runs` is present.

### Documentation and Tooling

1. The workflow JSON schema (`pkg/parser/schemas/main_workflow_schema.json`) **MUST** declare `max-runs` as a top-level property with `oneOf` integer-or-numeric-string typing and a minimum of `1`.
2. The autocomplete metadata (`docs/public/editor/autocomplete-data.json`) **MUST** advertise `max-runs` as a top-level leaf field.
3. The engine feature table in `docs/src/content/docs/reference/engines.md` **MUST** mark `max-runs` as supported (`✅`) for every engine listed.
4. New or updated documentation **SHOULD** describe `max-runs` as mapping to `awf.maxRuns` / `apiProxy.maxRuns`.

### Conformance

An implementation is considered conformant with this ADR if it satisfies all **MUST** and **MUST NOT** requirements above. Failure to meet any **MUST** or **MUST NOT** requirement constitutes non-conformance. In particular: emitting `apiProxy.maxRuns` when `max-runs` is unset, sourcing `MaxRuns` from `engine.max-runs` at runtime, or skipping codemod migration when both forms are present all constitute non-conformance.

---

*This is a DRAFT ADR generated by the [Design Decision Gate](https://github.com/github/gh-aw/actions/runs/25650796734) workflow. The PR author must review, complete, and finalize this document before the PR can merge.*
