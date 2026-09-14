# ADR-60860: Support Upward Manifest Imports and Ignore Self-Imports

**Date**: 2026-09-14
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

This pull request changes package manifest import resolution in `pkg/cli/` so nested `aw.yml` manifests can import a manifest above their package directory, such as `../aw.yml`, while still preventing imports from escaping the enclosing repository. The current behavior treated `./aw.yml` inside a nested manifest as a cycle against the child manifest itself, which produced a misleading error and blocked installation of root workflows that package authors intended to reference. The PR updates both local and GitHub-backed package resolution, adds regression tests for self-imports, upward imports, and true cycles, and aligns the reference/spec documentation with the new behavior. The architectural question is what boundary and semantics `includes` imports should use when manifests are nested inside one repository.

### Decision

We will resolve manifest imports relative to the declaring manifest but bound them by the repository root instead of the package directory, allowing nested packages to import manifests above them within the same repository. We will also treat an import that resolves to the declaring manifest itself, such as `./aw.yml` from `child/aw.yml`, as a self-import that is ignored with a warning rather than reported as a cycle. We chose this because the PR evidence shows package authors need root manifest reuse from nested packages, while genuine cycles and repository escape attempts must still be rejected clearly.

### Alternatives Considered

#### Alternative 1: Keep package-directory-bounded imports

The resolver could continue requiring all imported manifests to stay within the package directory being installed. This was considered because it is the simplest containment rule and minimizes traversal logic. It was not chosen because the PR adds explicit support and tests for nested packages importing a parent manifest within the same repository, which this rule would keep rejecting.

#### Alternative 2: Treat self-imports as normal import cycles

Another option would be to preserve the current cycle detection and fail when `./aw.yml` resolves back to the declaring manifest. This was considered because it reuses existing graph-cycle logic and keeps import semantics strict. It was not chosen because the PR shows this behavior is misleading for authors, prevents intended root-file installation, and is better represented as a warning for an ineffective import entry.

#### Alternative 3: Allow imports to escape the repository root

The resolver could allow arbitrary relative imports above the repository root as long as the local filesystem path exists. This was considered because it would maximize flexibility for local package layouts. It was not chosen because the implementation and tests intentionally keep the repository root as the trust boundary for both local and remote package resolution, preventing unexpected cross-repository file inclusion.

### Consequences

#### Positive
- Nested packages can reuse root manifest content directly, which makes package composition within one repository more flexible.
- Self-import mistakes now produce a targeted warning instead of a misleading cycle error, improving user diagnostics.
- Local and remote package resolution share the same repository-root import model, and the new tests lock in that behavior.

#### Negative
- Import resolution becomes more complex because the boundary is now the enclosing git repository rather than only the installed package directory.
- The resolver must distinguish self-imports from true cycles, adding another special case to manifest graph traversal.
- Package authors may unintentionally couple nested packages to repository-root manifests more often, increasing cross-directory dependencies.

#### Neutral
- Documentation and specification text now define repository-root-bounded imports and warning-only self-imports explicitly.
- Error messages change from "outside the package root" to "outside the repository root" for affected validation failures.
- Additional regression tests create committed local git fixtures to exercise both `resolveLocalRepositoryPackage` and `AddWorkflows` behavior.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
