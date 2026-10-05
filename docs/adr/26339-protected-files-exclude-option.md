# ADR-26339: Extend `protected-files` to Support Exclusion Overrides

**Date**: 2026-04-15
**Status**: Draft
**Deciders**: pelikhan, Copilot

---

## Part 1 — Narrative (Human-Friendly)

### Context

The `protected-files` configuration field on `create-pull-request` and `push-to-pull-request-branch` handlers controls whether patches that touch protected files (package manifests, engine instruction files such as `AGENTS.md` / `CLAUDE.md`, and `.github/` paths) are blocked, allowed, or redirected to a review issue. The list of protected files is compiled from a hardcoded manifest and cannot be narrowed per workflow. Workflows that legitimately need to update `AGENTS.md` or similar instruction files have no escape hatch: setting `protected-files: allowed` disables all protection (too permissive), while `blocked` and `fallback-to-issue` still reject the targeted file alongside every other protected file.

### Decision

We extend the `protected-files` field to accept either the existing string enum (`"blocked"`, `"allowed"`, `"fallback-to-issue"`) or an object form `{ policy: string, exclude: []string }`. Exclusions without a leading slash retain basename or path-prefix matching and are applied at compile time. Exclusions beginning with `/` identify exact repository-relative paths; these are carried to the runtime handler so patch-time and post-apply checks can skip only that exact path. The `allowed-files` allowlist remains an independent check and is never bypassed by a protected-file exclusion.

### Alternatives Considered

#### Alternative 1: Global `allowed` policy

Setting `protected-files: allowed` already allows agents to touch any file. It was rejected because it is all-or-nothing: a workflow that only needs to modify `AGENTS.md` would also lose protection for `go.mod`, `package.json`, and `.github/` files, undermining the purpose of the guard entirely.

#### Alternative 2: Separate top-level `protected-files-exclude` field

Adding a sibling YAML key (e.g., `protected-files-exclude: [AGENTS.md]`) would have been schema-additive without touching `protected-files`. It was rejected in favour of the object form because grouping `policy` and `exclude` under a single key makes the relationship explicit: the two sub-fields configure the same gate and should be read together by workflow authors.

#### Alternative 3: `allowed-files` allowlist

The existing `allowed-files` field is a strict *allowlist* of patterns that must pass for any file the agent touches. Its semantics are orthogonal to `protected-files`' *blocklist* semantics, and the two checks are evaluated independently. Using `allowed-files` to carve out exceptions from the protected set is unintuitive and would not actually suppress the protected-files check — it would only add a second, unrelated gate.

#### Alternative 4: Apply every exclusion at compile time

Filtering every exclusion from the compile-time basename and prefix lists was considered. It cannot represent an exact-path exception: removing a manifest basename would also unprotect files with that basename at every directory depth. Exact-path exclusions therefore use the runtime `protected_files_path_exclude` key, while the existing compile-time sentinel continues to carry exclusions through handler generation and is stripped before serialization.

### Consequences

#### Positive
- Workflow authors can unblock specific AI-modifiable instruction files (e.g., `AGENTS.md`) without degrading protection for dependency manifests or CI configuration.
- The string form is fully backward-compatible — no existing workflow configuration changes are required.
- Import set-merge semantics ensure that a base workflow imported via `imports:` can contribute exclusions without overwriting the importing workflow's full handler configuration.
- Exact-path exclusions permit a single repository path to be unprotected without removing protection for same-named files elsewhere.
- The internal `_protected_files_exclude` sentinel is stripped before serialization; only normalized exact paths are added to the runtime handler configuration.

#### Negative
- `protected-files` is now a polymorphic `oneOf` field (string | object). JSON Schema validators and tooling that previously relied on it being a simple string enum require schema updates.
- `preprocessProtectedFilesField` mutates `configData` in-place as a side-effect, which is non-obvious for future maintainers reading the parsing code path.
- The `ProtectedFilesExclude []string` field on `CreatePullRequestsConfig` and `PushToPullRequestBranchConfig` uses `yaml:"-"` to prevent direct YAML unmarshaling, relying on the pre-processing step. Forgetting this contract during refactors could silently result in exclusions being ignored.

#### Neutral
- The `excludeFromSlice` and `mergeUnique` utilities added in `runtime_definitions.go` are general-purpose helpers that may prove reusable for future protected-path filtering work.
- Basename and path-prefix exclusions continue to be applied by the compiler; exact-path exclusions are checked by the runtime handler in both patch-time and post-apply checks.
- The `allowed-files` check remains independent, so protected-file exclusions do not authorize paths outside that allowlist.
- Tests cover the sentinel stripping, the import-merge set semantics, and the preprocessing helper, providing regression coverage for the non-obvious two-phase design.

---

## Part 2 — Normative Specification (RFC 2119)

> The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**, **SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **MAY**, and **OPTIONAL** in this section are to be interpreted as described in [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119).

### Configuration Schema

1. The `protected-files` field **MUST** accept either a plain string enum value (`"blocked"`, `"allowed"`, or `"fallback-to-issue"`) or an object with an optional `policy` string and an optional `exclude` string array.
2. When the object form is used, the `policy` sub-field **MUST** be one of the same three enum values if present; an absent or empty `policy` **MUST** be treated as equivalent to `"blocked"`.
3. The `exclude` sub-field **MUST** be treated as a list of filenames, path prefixes, or exact repository-relative paths. Entries without a leading slash **MUST** retain basename matching (e.g., `"AGENTS.md"`) or path-prefix matching (e.g., `".agents/"`). An entry beginning with `/` **MUST** match only the exact repository-relative path after removing that leading slash (e.g., `"/pyproject.toml"`).
4. Additional properties on the object form **MUST NOT** be accepted; the JSON Schema `additionalProperties: false` constraint **MUST** be enforced.

### Compile-Time Preprocessing

1. The compiler **MUST** invoke `preprocessProtectedFilesField` before YAML unmarshaling of the handler config struct so that the object form is normalized to a plain policy string.
2. When the object form is encountered, the compiler **MUST** replace the `"protected-files"` key with the extracted policy string (or delete it when no policy is specified) so that downstream `validateStringEnumField` calls see only a string or an absent key.
3. The compiler **MUST** propagate the extracted exclude list to the handler config struct via `ProtectedFilesExclude`; it **MUST NOT** forward the exclusions as part of the serialized YAML.
4. The compiler **MUST** emit the sentinel key `_protected_files_exclude` in the handler registry config map to carry exclusions from the handler builder to `addHandlerManagerConfigEnvVar`.
5. `addHandlerManagerConfigEnvVar` **MUST** read and delete the `_protected_files_exclude` sentinel before serializing the runtime config so that the sentinel **MUST NOT** appear in the environment variable or in `config.json`.
6. `generateSafeOutputsConfig` **MUST** also delete `_protected_files_exclude` from any handler config before writing `config.json`.
7. Exact-path exclusions **MUST** be emitted as `protected_files_path_exclude` in runtime handler configuration. The internal `_protected_files_exclude` sentinel **MUST NOT** appear in runtime configuration.

### Runtime Enforcement

1. Runtime patch-time and post-apply protection checks **MUST** skip a protected file only when its normalized repository-relative path exactly matches a `protected_files_path_exclude` entry.
2. The runtime **MUST** decode Git C-style quoted path escapes before exact-path comparison, including octal UTF-8 byte escapes.
3. The runtime **MUST** check `allowed-files` independently; an exact-path protected-file exclusion **MUST NOT** bypass that allowlist.
4. Existing basename and path-prefix exclusions **MUST** preserve their established behavior.

### Import Merge Semantics

1. When `MergeSafeOutputs` processes an imported config whose handler type conflicts with the top-level config, it **MUST** extract and accumulate the `protected-files.exclude` list from the imported config before discarding that handler entry.
2. Accumulated exclusion lists **MUST** be merged as a deduplicated set into the top-level handler config's `ProtectedFilesExclude` field after all imports are processed.
3. `mergeSafeOutputConfig` **MUST** also merge `ProtectedFilesExclude` as a deduplicated set when both the top-level and imported config define the same handler type.

### Conformance

An implementation is considered conformant with this ADR if it satisfies all **MUST** and **MUST NOT** requirements above, including a regression test proving that an exact exclusion for a quoted non-ASCII path does not unprotect a same-named nested file and does not bypass `allowed-files`.

---

*This is a DRAFT ADR generated by the [Design Decision Gate](https://github.com/github/gh-aw/actions/runs/24431500261) workflow. The PR author must review, complete, and finalize this document before the PR can merge.*
