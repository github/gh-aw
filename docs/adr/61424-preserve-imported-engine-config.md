# ADR-61424: Preserve imported engine config

**Date**: 2026-09-17
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

This pull request fixes workflow compilation when a workflow imports a shared `engine:` block and also sets root-level engine override keys such as `model:` or budget fields. The PR description identifies a bug where `ExtractEngineConfig` returned a non-nil config for top-level-only keys, causing `resolveEngineFromIncludesAndImports` to skip extraction of the imported engine configuration entirely. As a result, imported settings such as `engine.auth` were silently dropped and workflows fell back to API-key authentication instead of preserving OIDC/WIF configuration. The implementation question is how imported engine configuration and root-level workflow overrides should compose when both are present.

### Decision

We will always merge imported engine configuration when the main workflow does not actually select an engine, even if root-level keys like `model:` or budget limits already produced a partial engine config. A main workflow selects an engine only with a string `engine:` value or an `engine:` object carrying `id` or `runtime`; preference-only objects (`engine.model` or `engine.mcp` alone) are treated as overrides on top of the imported engine, matching how included preference-only engine specs are handled during engine-conflict validation. We will then re-apply the main workflow's top-level override fields on top of the imported engine config so root-level values continue to take precedence while imported settings such as `engine.auth` are preserved. We chose this because the PR evidence shows the current behavior silently discards imported authentication and budget-related settings, which breaks shared engine reuse and changes runtime auth behavior without warning.

### Alternatives Considered

#### Alternative 1: Keep the existing extraction behavior

The prior logic extracted engine configuration from imports only when the main workflow's extracted engine config was `nil`, and otherwise only backfilled the imported model in some cases. This was considered because it preserves the previous simple control flow and treats any non-nil main config as authoritative. It was not chosen because the PR demonstrates that top-level-only fields create a partial config rather than a full `engine:` declaration, so this behavior silently drops imported auth and other engine settings.

#### Alternative 2: Require all shared engine users to duplicate auth and budget settings locally

Another option was to document that workflows using imported engines must avoid top-level overrides or restate all important engine fields in the main workflow. This was considered because it avoids changing merge semantics in the compiler. It was not chosen because it defeats the purpose of shared engine imports, increases duplication, and leaves a silent footgun where valid workflow syntax changes authentication behavior unexpectedly.

### Consequences

#### Positive
- Imported engine settings such as OIDC/WIF authentication are preserved when workflows also set root-level `model:` or budget override fields.
- Root-level workflow overrides still win, so existing precedence expectations for `model` and budget-related keys are maintained.
- The new integration test makes this composition behavior explicit by asserting emitted `AWF_AUTH_*` variables and the absence of an API-key validation step.

#### Negative
- Engine resolution logic becomes more complex by distinguishing an actual main-workflow engine selection from a preference-only or top-level-only partial config.
- The compiler now depends on explicit field-by-field reapplication of top-level overrides, which may require maintenance if new override fields are added later.
- Future changes to engine merge behavior will need careful regression coverage to avoid reintroducing silent precedence bugs.

#### Neutral
- Workflows that select their own engine (`engine: claude`, `engine.id`, `engine.runtime`) remain unchanged and still take precedence over imported engine definitions.
- Imported `engine.model` continues to act only as a fallback when the main workflow does not set a model.
- The decision affects compiler internals and generated workflow auth environment variables without changing the external workflow authoring syntax.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
