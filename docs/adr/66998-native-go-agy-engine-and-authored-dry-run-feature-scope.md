# ADR-66998: Implement Agy as a Native Go Engine and Scope Dry-Run Dangerous-Feature Checks to Authored Configuration

**Date**: 2026-10-09
**Status**: Draft
**Deciders**: pelikhan (PR #66998 author) [TODO: verify additional deciders]

---

### Context

The Agy (Google Antigravity CLI) engine was defined primarily through the Markdown engine catalog entry `pkg/workflow/data/engines/agy.md`, which carried roughly 254 lines of executable runtime behavior (harness logic and gateway configuration) inside a catalog document. Every other first-class engine in the compiler — Gemini, Claude, Copilot — is implemented as a Go type in `pkg/workflow` with its runtime scripts shipped in `actions/setup/js/`. Keeping executable behavior in the catalog meant Agy bypassed the compiler's shared engine plumbing (version/model defaults, API-key routing, AWF isolation defaults, protected agent manifests, MCP configuration, log parsing) and that its behavior was invisible to Go tests and type checking. Separately, the compiler's strict dry-run validation rejected `dangerously-*` flags without distinguishing between features authored by workflow authors in Markdown and flags set internally by trusted built-in engine implementations; the native Agy harness legitimately needs `--dangerously-skip-permissions` inside the AWF sandbox to preserve its existing unattended execution profile.

### Decision

We will register Agy as a native Go engine (`AgyEngine` in `pkg/workflow/agy_engine.go`, implementing `CodingAgentEngine`, `HarnessRunner`, and `MCPConfigAdapterProvider`), move its harness (`actions/setup/js/agy_harness.cjs`) and gateway adapter (`actions/setup/js/convert_gateway_config_agy.cjs`) into the setup action, and reduce `pkg/workflow/data/engines/agy.md` to a metadata-only catalog entry. We will also narrow strict dry-run dangerous-feature validation (`validateDryRunFeatures` in `pkg/workflow/features_validation.go`) so that it rejects only enabled `dangerously-*` entries authored in workflow Markdown `features:` configuration — including entries contributed by merged imports — while permitting flags set by trusted built-in engine implementations. The primary driver is consistency: Agy should be subject to the same compiler guarantees, tests, and shared defaults as every other engine, and the dry-run gate should police author intent rather than implementation internals.

### Alternatives Considered

#### Alternative 1: Keep Agy as a Markdown-only catalog engine

Leave the harness and gateway logic in `pkg/workflow/data/engines/agy.md` and continue to special-case Agy in the compiler. This was the status quo and required no migration of the three Agy workflow locks. Rejected because the catalog entry had grown into a parallel, untested engine implementation: shared behavior (Gemini API-key routing, AWF isolation defaults, protected manifests, MCP config, streaming log parsing) had to be re-expressed there, and regressions were only observable at lock-generation time rather than in Go unit tests.

#### Alternative 2: Reject all `dangerously-*` flags during dry-run, including engine-internal ones

Keep the dry-run check as a blanket prohibition and change the native Agy harness to avoid `--dangerously-skip-permissions`. This is the strictest option and was a close call. Rejected for this PR because the alternative requires translating workflow tool restrictions into Agy's native scoped permission rules, which is not implemented yet; blanket rejection would have made the experimental Agy harness undryrunnable without delivering any additional real safety, since the flag is confined to the AWF sandbox. [TODO: verify intent to follow up with scoped permission-rule translation]

#### Alternative 3: Scope the dry-run check by engine allow-list rather than by authorship

Permit `dangerously-*` only for a named list of engines. Rejected because it couples a generic validation rule to engine identities and would need editing for every new engine, whereas the authored-vs-internal distinction expresses the actual invariant: workflow authors must not enable dangerous features, trusted built-in code may.

### Consequences

#### Positive

- Agy gains the compiler's shared engine behavior for free (version/model defaults, API-key routing, AWF isolation, protected manifests, MCP configuration, log parsing) and is now covered by Go unit tests (`agy_engine_test.go`, `agy_harness_test.go`, `features_dry_run_test.go`).
- The three Agy workflow locks shrink substantially (~256 removed lines each in `smoke-agy`, `engine-conformance-agy`, `agy-conformance-reusable`) because the runtime logic now lives in the setup action instead of being inlined per workflow.
- Dry-run validation now gives workflow authors an actionable error naming the exact `features.<name>` entries to remove, including those contributed by merged imports.

#### Negative

- Agy behavior is now split across three locations (Go engine, setup-action JS harness, metadata catalog entry), so a change to its runtime may require coordinated edits in all three.
- Permitting trusted built-in engine flags weakens the dry-run gate's blanket guarantee: `--dangerously-skip-permissions` still reaches the Agy harness, and safety now depends on AWF isolation plus reviewer scrutiny of built-in engine code rather than on a single validation rule.
- Translating workflow tool restrictions into Agy's native scoped permission rules remains unimplemented, leaving a known gap behind the experimental engine.

#### Neutral

- `pkg/workflow/data/engines/agy.md` remains as a metadata-only entry rather than being deleted, preserving catalog discoverability.
- New Agy-related constants, domain entries, and API targets were added (`version_constants.go`, `ecosystem_domains.json`, `domains.go`, `engine_api_targets.go`), following the existing per-engine registration pattern.
- Debugging guidance was updated (`.github/aw/debug-security-review.md`, `.github/skills/debugging-workflows/SKILL.md`) to require concrete source evidence for refusals and to distinguish confirmed defects from incomplete evidence or authorization limits; this is documentation-only and independent of the engine change.
- A successful dry-run of an Agy workflow does not authorize live execution; existing unsupported configurations remain rejected.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
