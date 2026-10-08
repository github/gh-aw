# ADR-66998: Implement Agy as a Native Go Engine and Scope Dry-Run Dangerous-Feature Checks to Authored Configuration

**Date**: 2026-10-08
**Status**: Proposed
**Deciders**: pelikhan (PR #66998 author and requester of the engine migration and dry-run scope)

---

### Context

The Agy (Google Antigravity CLI) engine was defined through the Markdown engine catalog entry `pkg/workflow/data/engines/agy.md`, which carried roughly 254 lines of executable runtime behavior. It used the compiler's behavior-defined engine infrastructure and already had Go-driven harness tests, but JavaScript embedded in Markdown was outside setup-action type checking. The requester asked for a first-class Go engine similar to Gemini, with explicit engine interfaces and shared compiler defaults rather than executable catalog content.

During development, an Agy-specific dry-run refusal was added because the harness passes `--dangerously-skip-permissions`. The requester subsequently clarified that the dangerous-feature filter should apply only to entries authored in workflow Markdown, not built-in engine implementation flags. Agy supports headless scoped permission grants; the bypass is not intrinsically required for headless mode, but preserves this integration's existing unattended execution profile.

### Decision

We will register Agy as a native Go engine (`AgyEngine` in `pkg/workflow/agy_engine.go`, implementing `CodingAgentEngine`, `HarnessRunner`, and `MCPConfigAdapterProvider`), move its harness (`actions/setup/js/agy_harness.cjs`) and gateway adapter (`actions/setup/js/convert_gateway_config_agy.cjs`) into the setup action, and reduce `pkg/workflow/data/engines/agy.md` to a metadata-only catalog entry. We will also narrow strict dry-run dangerous-feature validation (`validateDryRunFeatures` in `pkg/workflow/features_validation.go`) so that it rejects only enabled `dangerously-*` entries authored in workflow Markdown `features:` configuration — including entries contributed by merged imports — while permitting flags set by trusted built-in engine implementations. The primary driver is consistency: Agy should be subject to the same compiler guarantees, tests, and shared defaults as every other engine, and the dry-run gate should police author intent rather than implementation internals.

### Alternatives Considered

#### Alternative 1: Keep Agy as a Markdown-only catalog engine

Keep the behavior-defined engine and embedded harness/gateway logic in `pkg/workflow/data/engines/agy.md`. This avoids migrating the three Agy workflow locks and retains existing tests. Rejected because the requester explicitly chose the native Go engine architecture, and shipping runtime scripts through the setup action brings them under existing JavaScript tooling.

#### Alternative 2: Reject all `dangerously-*` flags during dry-run, including engine-internal ones

Retain the development-time Agy refusal and replace the native blanket approval flag with scoped permission grants. Rejected for this PR because the requester explicitly scoped the filter to authored Markdown entries and requested preservation of the engine migration. Translating workflow tool restrictions into native permission rules would be a separate behavior change requiring verified mappings and conformance coverage; no follow-up commitment is made here. AWF isolation does not by itself prove that all uses of blanket approvals are safe.

#### Alternative 3: Scope the dry-run check by engine allow-list rather than by authorship

Permit `dangerously-*` only for a named list of engines. Rejected because it couples a generic validation rule to engine identities and would need editing for every new engine, whereas the authored-vs-internal distinction expresses the actual invariant: workflow authors must not enable dangerous features, trusted built-in code may.

### Consequences

#### Positive

- Agy's version/model defaults, API-key routing, AWF defaults, protected manifests, MCP configuration and log parsing are explicitly wired through native engine interfaces. Existing harness tests are retained and native-engine and dry-run regression coverage is added.
- The three Agy workflow locks shrink substantially (~256 removed lines each in `smoke-agy`, `engine-conformance-agy`, `agy-conformance-reusable`) because the runtime logic now lives in the setup action instead of being inlined per workflow.
- Dry-run validation now gives workflow authors an actionable error naming the exact `features.<name>` entries to remove, including those contributed by merged imports.

#### Negative

- Agy behavior is now split across three locations (Go engine, setup-action JS harness, metadata catalog entry), so a change to its runtime may require coordinated edits in all three.
- The authored-feature filter is not a guarantee that generated runtime commands contain no dangerous flags. `--dangerously-skip-permissions` still reaches Agy; runtime isolation, credential flows and reachable effects require separate review.
- Translating workflow tool restrictions into Agy's native scoped permission rules remains unimplemented, leaving a known gap behind the experimental engine.

#### Neutral

- `pkg/workflow/data/engines/agy.md` remains as a metadata-only entry rather than being deleted, preserving catalog discoverability.
- New Agy-related constants, domain entries, and API targets were added (`version_constants.go`, `ecosystem_domains.json`, `domains.go`, `engine_api_targets.go`), following the existing per-engine registration pattern.
- Debugging guidance was updated (`.github/aw/debug-security-review.md`, `.github/skills/debugging-workflows/SKILL.md`) to require concrete source evidence for refusals and to distinguish confirmed defects from incomplete evidence or authorization limits; this is documentation-only and independent of the engine change.
- A successful dry-run of an Agy workflow does not authorize live execution; existing unsupported configurations remain rejected.

---

*Proposed for acceptance with PR #66998. Implementation-internal flags remain subject to runtime security review, and successful compilation does not authorize dispatch.*
