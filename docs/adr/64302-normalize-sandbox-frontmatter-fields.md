# ADR-64302: Normalize sandbox frontmatter fields to kebab-case

**Date**: 2026-09-29
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

This pull request changes gh-aw so sandbox frontmatter keys use kebab-case consistently across parser validation, codemods, generated metadata, examples, and reference documentation. The PR description explicitly states that camel-cased fields such as `allowWrite`, `authHeader`, and `entrypointArgs` will now be rejected, while `gh aw fix` provides migration support. The implementation spans parser schema tests, a new codemod, workflow examples, autocomplete metadata, and documentation, which makes this a repository-wide authoring contract change rather than a local refactor. The decision is how gh-aw should represent sandbox frontmatter fields in user-authored workflow markdown while preserving a guided migration path.

### Decision

We will standardize user-authored sandbox frontmatter fields on kebab-case and reject the older camelCase spellings during schema validation. We will provide an idempotent `gh aw fix` codemod that rewrites supported camel-cased sandbox keys to their kebab-case equivalents while preserving formatting and comments. We chose this because a single naming convention reduces ambiguity across docs, examples, and validation, while the codemod gives existing workflows a predictable migration path.

### Alternatives Considered

#### Alternative 1: Keep accepting both camelCase and kebab-case indefinitely

This was a realistic option because it would minimize breaking changes for existing workflows and avoid forcing migration. It was not chosen because the PR description and schema tests show the goal is to make sandbox frontmatter naming consistent; indefinite aliases would preserve ambiguity in docs, examples, and validation behavior.

#### Alternative 2: Normalize only in documentation while preserving camelCase parser compatibility

This was considered because it would align examples and reference docs without changing runtime compatibility. It was not chosen because the PR adds schema tests that reject camel-cased fields and a dedicated codemod for migration, indicating the intent is an actual contract change rather than a documentation-only cleanup.

### Consequences

#### Positive
- Workflow authors get one canonical sandbox field style across documentation, examples, autocomplete data, and validation.
- Schema validation can reject outdated camel-cased fields early, producing clearer authoring feedback.
- `gh aw fix` offers an automated migration path that preserves formatting and comments for supported sandbox keys.

#### Negative
- This is a breaking change for existing workflows that still use camel-cased sandbox fields.
- The codemod and validation logic add maintenance cost because renamed sandbox fields must stay synchronized across parser, CLI, and docs.
- Users may need to recompile or update workflow sources and examples after migration to keep generated artifacts in sync.

#### Neutral
- The change affects author-facing markdown keys, while generated runtime JSON may still use existing internal naming where required.
- Multiple repository areas now encode the same naming contract, including tests, examples, docs, and autocomplete metadata.
- Future sandbox field additions should follow kebab-case by default to remain consistent with this decision.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
