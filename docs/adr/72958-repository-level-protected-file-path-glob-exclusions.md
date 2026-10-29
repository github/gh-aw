# ADR-72958: Repository-Level Protected-File Path Glob Exclusions in aw.json

**Date**: 2026-05-14
**Status**: Draft
**Deciders**: pelikhan, Unknown

---

### Context

Safe-output handlers such as `create-pull-request`, `push-to-pull-request-branch`, and `approve-workflow-run` block or reroute patches that touch protected files: manifest basenames such as `package.json`, protected path prefixes such as `.github/`, and top-level dot-folders. The only existing escape hatch was a per-handler `protected-files.exclude` list in each workflow's frontmatter. That list supported basenames, path prefixes, and exact rooted paths, but not globs. Some repositories need the same narrow exception in every workflow. In this repository, agents routinely add `.changeset/*.md` files. Repeating that exception in every workflow is error-prone, and disabling file protection entirely is unsafe.

### Decision

We will add a repository-wide `protected_files.exclude` setting to `.github/workflows/aw.json`. It accepts repository-root path globs, where `*` matches within a single path segment and `**` crosses `/`. At compile time, the compiler reads it into `WorkflowData.RepoProtectedFilesExclude` and appends it to each handler's `protected_files_path_exclude` in both the MCP safe-outputs config and the runtime handler config. At runtime, `manifest_file_helpers.cjs` filters changed paths through a shared `filterProtectedPaths` glob matcher before the manifest-file, protected-prefix, and top-level dot-folder checks run in both patch and post-apply modes. The JSON schemas for `aw.json` and frontmatter reject wildcard-only catch-all patterns such as `*`, `**`, and `/**`, so every exclusion must contain a literal segment. Exclusions never bypass `allowed-files`.

### Alternatives Considered

#### Alternative 1: Keep per-workflow `protected-files.exclude` only

Each workflow would continue to declare its own exclusions in frontmatter. This avoids new repository-level configuration, but it requires repeating the same entry across hundreds of workflows. Any workflow that misses the entry fails inconsistently. This option was rejected because the exception is a repository policy, not a workflow-specific one.

#### Alternative 2: Add `.changeset/` as a default exclusion or as a basename or prefix exclusion

Adding a directory prefix such as `.changeset/` would remove protection from `.changeset/config.json` and nested files too. Making it a built-in default would also apply it to every gh-aw user. Repository-root globs allow a narrower rule, `.changeset/*.md`, that leaves configuration files protected. This option was rejected because it is broader than needed.

#### Alternative 3: Resolve the repository exclusions at runtime instead of compile time

The runtime could read `aw.json` directly when the safe-output handler runs. This approach would not require recompilation, but it would make lock files depend on mutable repository state and diverge from how other `aw.json` settings are applied. [TODO: verify whether this option was considered]

### Consequences

#### Positive
- Repositories can declare one narrow, auditable exception that applies to all compiled workflows. Patch and post-apply checks handle it the same way.
- Glob support allows precise rules such as `.changeset/*.md` without exposing sibling configuration files or nested paths.
- Schema validation rejects wildcard-only catch-all patterns. This prevents accidentally disabling protection entirely.
- `allowed-files` restrictions and the `protected-files` policy (`fallback-to-issue` or `request-review`) remain enforced for all other files.

#### Negative
- Changing `aw.json` requires recompiling every workflow. In this PR, that regenerates many `.lock.yml` files.
- Handler-level exclusions now use glob matching instead of exact equality for rooted paths. Rooted entries containing `*` change meaning, which is a subtle semantic expansion of an existing field.
- The rule that patterns must not be wildcard-only is enforced only by a regex in the JSON schema. Patterns such as `.*/**` can still be broad.

#### Neutral
- Repository exclusions are appended after handler exclusions in `protected_files_path_exclude`. They do not remove basenames from the `protected_files` list. For example, `README.md` stays protected when nested elsewhere.
- This repository opts in with `".changeset/*.md"` in `.github/workflows/aw.json`.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
