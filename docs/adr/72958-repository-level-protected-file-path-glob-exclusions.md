# ADR-72958: Repository-Level Protected-File Path Glob Exclusions in aw.json

**Date**: 2026-10-29
**Status**: Proposed
**Decision provenance**: Completed at the user's request for PR #72958. Maintainer acceptance and decision-maker identities are not recorded.

---

### Context

Safe-output handlers such as `create-pull-request`, `push-to-pull-request-branch`, and `approve-workflow-run` block or reroute patches that touch protected files: manifest basenames such as `package.json`, protected path prefixes such as `.github/`, and top-level dot-folders. The only existing escape hatch was a per-handler `protected-files.exclude` list in each workflow's frontmatter. That list supported basenames, path prefixes, and exact rooted paths, but not globs. Some repositories need the same narrow exception in every workflow. In this repository, agents routinely add `.changeset/*.md` files. Repeating that exception in every workflow is error-prone, and disabling file protection entirely is unsafe.

### Decision

Add a repository-wide `protected_files.exclude` setting to `.github/workflows/aw.json`. It accepts repository-root-relative path globs, where `*` matches within a single path segment and `**` crosses `/`. Matching is case-sensitive and anchored to the entire path; all other characters are literal. Glob conversion must preserve literal text without collisions with internal wildcard placeholders.

At compile time, the compiler reads the setting into `WorkflowData.RepoProtectedFilesExclude` and appends it to each applicable handler's `protected_files_path_exclude` in both the MCP safe-outputs config and the runtime handler config. At runtime, `manifest_file_helpers.cjs` filters changed paths through `filterProtectedPaths` before the manifest-file, protected-prefix, and top-level dot-folder checks run in both patch and post-apply modes.

The JSON schemas for `aw.json` and frontmatter require at least one literal ASCII letter, digit, `_`, or `-` in every exclusion. Wildcards, dots, and separators alone do not provide scope: `*`, `**`, `/**`, and `.*/**` are rejected. Repository-level patterns additionally reject leading `/` and whitespace. This is a minimum scope check, not a guarantee that every accepted pattern is narrow. Exclusions never bypass `allowed-files`.

### Alternatives Considered

#### Alternative 1: Keep per-workflow `protected-files.exclude` only

Each workflow would continue to declare its own exclusions in frontmatter. This avoids new repository-level configuration, but it requires repeating the same entry across hundreds of workflows. Any workflow that misses the entry fails inconsistently. This option was rejected because the exception is a repository policy, not a workflow-specific one.

#### Alternative 2: Add `.changeset/` as a default exclusion or as a basename or prefix exclusion

Adding a directory prefix such as `.changeset/` would remove protection from `.changeset/config.json` and nested files too. Making it a built-in default would also apply it to every gh-aw user. Repository-root globs allow a narrower rule, `.changeset/*.md`, that leaves configuration files protected. This option was rejected because it is broader than needed.

#### Alternative 3: Resolve the repository exclusions at runtime instead of compile time

This alternative is considered and rejected in this record; there is no claim that it was discussed before the draft. The compiler already loads repository configuration in [`compiler_yaml.go`](../../pkg/workflow/compiler_yaml.go), then embeds the exclusions through [`safe_outputs_config_generation.go`](../../pkg/workflow/safe_outputs_config_generation.go) and [`safe_outputs_config_runtime.go`](../../pkg/workflow/safe_outputs_config_runtime.go). The runtime loader in [`safe_outputs_config.cjs`](../../actions/setup/js/safe_outputs_config.cjs) reads the generated safe-output `config.json` via `GH_AW_SAFE_OUTPUTS_CONFIG_PATH` (or its runner default), rather than resolving `aw.json` from a checkout.

Reading `aw.json` directly could avoid recompilation for this setting, but would introduce a second configuration-loading path, require choosing a checkout and revision, and allow effective protection policy to change without regenerating the lock file. Keeping resolution at compile time preserves the reviewed lock file's policy and gives both configuration consumers the same exclusions.

### Consequences

#### Positive
- Repositories can declare one narrow, auditable exception that applies to all compiled workflows. Patch and post-apply checks handle it the same way.
- Glob support allows precise rules such as `.changeset/*.md` without exposing sibling configuration files or nested paths.
- Schema validation rejects unscoped wildcard and punctuation-only patterns, reducing accidental catch-all exclusions.
- `allowed-files` restrictions and the `protected-files` policy (`fallback-to-issue` or `request-review`) remain enforced for all other files.

#### Negative
- Changing `aw.json` requires recompiling every workflow. In this PR, that regenerates many `.lock.yml` files.
- Handler-level exclusions now use glob matching instead of exact equality for rooted paths. Rooted entries containing `*` change meaning, which is a subtle semantic expansion of an existing field.
- The minimum literal-character scope rule is enforced by JSON schema regexes. Accepted patterns such as `**a**` can still be broad and require policy review.

#### Neutral
- Repository exclusions are appended after handler exclusions in `protected_files_path_exclude`. They do not remove basenames from the `protected_files` list. For example, `README.md` stays protected when nested elsewhere.
- This repository opts in with `".changeset/*.md"` in `.github/workflows/aw.json`.

---

This record completes the Design Decision Gate draft for PR #72958. Its proposed status does not assert maintainer approval.
