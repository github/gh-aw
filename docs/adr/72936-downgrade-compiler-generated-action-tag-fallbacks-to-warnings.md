# ADR-72936: Downgrade Compiler-Generated Action Tag Fallbacks to Lint Warnings

**Date**: 2026-10-30
**Status**: Draft
**Deciders**: Unknown (PR #72936)

---

### Context

The `action-pin` lint rule (`pkg/workflow/lint/integrity.go`) requires every `uses:` reference in a compiled lock file to be pinned to a full commit SHA. The compiler itself emits action references (for example `github/gh-aw-actions/setup@<tag>` in action mode or `github/gh-aw/actions/setup@<tag>` in release mode) and tries to resolve them to a SHA. When SHA resolution is unavailable, the compiler falls back to the version tag. In v0.93.5 this fallback was rejected as a lint error, so existing workflows and the documented triage example failed to compile even though the user did not author the reference (issue #72493). Any fix must preserve strict pinning for references authored by users, because an unpinned authored action is a supply-chain risk.

### Decision

We will record the exact references the compiler emits from `resolveActionReference` in a private `WorkflowData.generatedActionRefs` set. We will pass that set to the linter through a new `lint.Input.GeneratedActionRefs` field. Well-formed tag fallbacks in that set will be reported as **warnings** rather than errors. Before linting, `compilerGeneratedActionRefs` removes from the set any reference that matches a recorded `ActionResolutionFailures` entry or that appears as a `uses:` value in the user-authored `data.Jobs` tree. This means that authored references remain errors even when they match a generated fallback exactly. Malformed references remain errors whatever their origin. SHA pinning is still preferred and is used whenever resolution succeeds. Generated references and resolution failures are carried across the `WorkflowData` copies created for artifact-retention and dry-run rendering.

### Alternatives Considered

#### Alternative 1: Allow mutable references globally (reuse `AllowActionRefs`)

The existing `AllowActionRefs` compiler option already turns well-formed mutable references into warnings. Enabling it automatically when SHA resolution fails would fix the reported compilation failure with minimal code. We did not choose it because it would also relax enforcement for user-authored actions, which is the supply-chain risk the `action-pin` rule exists to prevent.

#### Alternative 2: Exempt by repository prefix (e.g., any `github/gh-aw-actions/*` reference)

The linter could treat any reference whose repository belongs to the gh-aw action org as compiler-owned. This is simpler than tracking exact references. We did not choose it because users can author the same repositories, including in safe-output `actions:` subdirectory steps, and those references would silently lose strict pinning. Exact-reference tracking, minus authored matches, keeps the exemption limited to what the compiler emitted.

#### Alternative 3: Fail hard and require SHA resolution to succeed

The compiler could keep the error and require network or cache access so the setup action always resolves to a SHA. This gives the strongest pinning guarantee. We did not choose it because it blocks compilation in offline or degraded environments and for releases whose tags cannot yet be resolved, which is the regression this PR fixes.

### Consequences

#### Positive
- Workflows whose compiler-generated setup reference falls back to a tag compile again, with the unpinned reference still reported as a warning.
- Strict `action-pin` errors are kept for every authored reference, including authored references identical to generated fallbacks, safe-output subdirectory actions, and malformed references.
- The exemption is based on exact references, so it does not widen when new action repositories are added.

#### Negative
- Lock files may now contain mutable tag references for compiler-owned actions. These are only warnings, so a CI that ignores warnings could ship them unnoticed.
- `WorkflowData` gains hidden mutable state (`generatedActionRefs`) that must be propagated by hand across rendering copies (see the deferred copy-back in `compiler_yaml.go`). Future copy paths could drop it without any error.
- `removeAuthoredActionRefs` walks the whole `data.Jobs` tree and removes any matching `uses` string. This depends on authored content staying reachable through that structure.

#### Neutral
- `resolveActionReference` now uses a named return value with a `defer` to record every emitted reference. `resolveSetupActionModeRef` builds the repository path with `path.Join`.
- Regression tests cover action and release modes, artifact rendering copies, and several authored-reference shapes (`pre-steps`, safe-output `actions`, custom `jobs`).

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
