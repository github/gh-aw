# ADR-58545: Support Trailing Wildcards in aw.yml Includes

**Date**: 2026-09-04
**Status**: Draft
**Deciders**: Unknown, adr-writer agent

---

### Context

This pull request changes `aw.yml` package manifests so `includes` entries can select multiple installable items with a single trailing `/*` wildcard. The diff shows that package authors previously had to enumerate each workflow, agent, or skill entry explicitly, even when they wanted all supported direct children of a directory. The implementation adds wildcard parsing, expands matches for both local and remote packages, preserves deterministic lexical ordering, deduplicates overlapping includes, and skips unsupported matches such as nested paths and local symlinks. Because this changes how package manifests express install bundles and how the resolver interprets those manifests across local and remote sources, the behavior should be captured as an explicit architectural decision.

### Decision

We will allow `aw.yml` `includes` string entries to end with one non-recursive trailing `/*` wildcard that selects supported direct children of a single directory. We will reject wildcard use in any other position, apply the existing workflow, agent, and skill validation rules to each expanded match, and preserve deterministic lexical ordering with duplicate removal. We will implement the same wildcard semantics for both local and remote repository package resolution so package manifests behave consistently regardless of source.

### Alternatives Considered

#### Alternative 1: Require explicit include entries only

This keeps `aw.yml` manifests fully explicit by listing every workflow, agent, and skill path one by one. It was considered because it avoids adding pattern parsing and expansion logic to manifest resolution. It was not chosen because the PR evidence shows a need to reduce repetitive manifest maintenance when a package wants all supported direct children from directories like `workflows/`, `agents/`, or `skills/`.

#### Alternative 2: Support broader glob patterns such as recursive `**` or wildcards in arbitrary positions

This would provide more flexible pattern matching for package authors and could cover deeper directory trees or filename-based matching. It was considered because glob syntax is familiar and more expressive than a single trailing wildcard. It was not chosen because the diff explicitly constrains behavior to one trailing `/*`, and broader globbing would add ambiguity, more edge cases, and more risk of unintentionally including unsupported or nested content.

### Consequences

#### Positive
- Package authors can include supported direct children of a directory with less manifest duplication.
- Local and remote package resolution now share the same wildcard behavior, reducing surprises between development and installed package usage.
- Deterministic lexical ordering and deduplication keep installs reproducible even when explicit entries and wildcards overlap.

#### Negative
- Manifest resolution becomes more complex because it now performs wildcard parsing, directory enumeration, filtering, and duplicate removal.
- Future maintainers must preserve the intentionally narrow wildcard contract and its edge-case handling across multiple code paths.
- Package authors may expect more general glob support than the implementation allows and encounter rejected patterns.

#### Neutral
- Unsupported matches such as nested paths, invalid wildcard forms, and local symbolic links are ignored or rejected according to the existing validation model.
- Documentation, schema descriptions, and tests now define wildcard semantics as part of the package manifest contract.
- The PR adds separate expansion paths for local filesystem resolution and remote repository listing, both of which must remain behaviorally aligned.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
