---
title: Package Management (Spec)
description: Normative specification for the aw.yml repository package manifest format.
sidebar:
  order: 321
---

# aw.yml Repository Package Manifest Specification

**Version**: 0.3.0
**Status**: Draft

## Abstract

This specification defines the `aw.yml` repository package manifest format used by `gh aw` to identify, validate, and install repository packages.

## 1. Introduction

The `aw.yml` manifest describes an installable Agentic Workflow package located either at a repository root or within a nested package folder.

Package references use one of these forms:

- `owner/repo`
- `owner/repo/path/to/package`

The package root is the directory containing `aw.yml`.

## 2. Conformance

The key words MUST, MUST NOT, SHOULD, SHOULD NOT, and MAY are to be interpreted as described in RFC 2119.

## 3. Manifest location and naming

The canonical manifest filename is `aw.yml`.

## 4. Manifest format

The manifest document MUST be a YAML mapping. Unknown top-level fields MUST be rejected.

### 4.1 Fields

| Field | Type | Required | Meaning |
| --- | --- | --- | --- |
| `manifest-version` | string | No | Manifest format version. Defaults to `"1"`. |
| `min-version` | string | No | Minimum supported `gh-aw` version. |
| `name` | string | Yes | Human-readable package name. |
| `emoji` | string | No | Optional package emoji for display in package metadata. |
| `icon` | string | No | Optional package icon: an emoji, a GitHub primer octicon name (`:...:`), or an SVG resource path. |
| `description` | string | No | Human-readable package description. |
| `license` | string | No | SPDX license identifier or license name for the package. |
| `private` | boolean | No | Whether the package is unavailable for installation. Defaults to `false`. |
| `experimental` | boolean | No | Whether the package is experimental. Defaults to `false`. |
| `imports` | array of strings | No | Paths to package manifests included recursively in the install set. |
| `files` | array of strings | No | Deprecated. Explicit installable workflow file list. Use `includes` instead. |
| `includes` | array of strings or mappings | No | Explicit installable package entries. String entries use path conventions; mapping entries declare an explicit source-to-destination install path. |
| `resources` | array of mappings | No | Declarative repository assets copied as-is to allowlisted destinations. |
| `config` | array of mappings | No | Ordered repository setup actions applied by the interactive installer. |

### 4.2 `manifest-version`

If omitted, `manifest-version` defaults to `"1"`.

For this version of the format, the only valid value is `"1"`.

### 4.3 `min-version`

If present, `min-version` MUST use the exact `vMAJOR.minor.patch` form, such as:

- `v1.2.3`

If the running compiler version is lower than `min-version`, validation MUST fail.

### 4.4 `name`

`name` MUST be present and MUST be a non-empty string after trimming surrounding whitespace.

### 4.5 `emoji`

If present, `emoji` MUST be a string.

### 4.5.1 `icon`

If present, `icon` MUST be a non-empty string that matches one of the following formats:

1. **Emoji**: A single or sequence of Unicode emoji characters.
2. **GitHub Primer Octicon**: A GitHub Primer octicon name enclosed in colons using `:name:` format (for example, `:check-circle:`).
3. **SVG Package Resource**: A path to an `.svg` file that is declared as a package resource in the `resources` section of `aw.yml`.

### 4.6 `description`

If present, `description` MUST be a string.

Implementations SHOULD warn if `description` exceeds 255 characters.

### 4.7 `license`

If present, `license` MUST be a string. Use an [SPDX license identifier](https://spdx.org/licenses/) such as `MIT` or `Apache-2.0`, or a license name. Non-string values MUST be rejected.

### 4.8 `private`

If omitted, `private` defaults to `false`. When `private` is `true`, `gh aw add` MUST refuse to install the package.

### 4.9 `experimental`

If omitted, `experimental` defaults to `false`. When `experimental` is `true`, `gh aw add` MUST warn before installing the package.

### 4.10 `files`

If present, `files` MUST be an array of strings.

Each entry MUST be resolved relative to the package root and MUST match one of the following kinds:

- **Agentic workflow markdown** — the path MUST end in `.md` (case-insensitive) and MUST begin with either `workflows/` or `.github/workflows/`.
- **Raw GitHub Actions YAML** — the path MUST end in `.yml` (case-insensitive) but MUST NOT end in `.lock.yml`. It MUST be a direct child of `.github/workflows/` (no nested subdirectories) and MUST NOT appear under `workflows/`.

Duplicate entries SHOULD be ignored after normalization.

**Path-traversal safety**: Each entry in `files` MUST NOT contain a path-traversal sequence. Specifically, any entry that contains `../` (or `..\` on Windows-style paths), begins with `../`, or resolves to a path outside the package root after normalization MUST be rejected with a validation error. Implementations MUST NOT follow symlinks that would escape the package root during file resolution. This rule applies regardless of the number of traversal components in the path (e.g., `../../etc/passwd` and `workflows/../../hidden` are both prohibited).

### 4.10.1 `imports`

If present, `imports` MUST be an array of strings. Each entry MUST resolve relative to the
manifest containing it, MUST name an `aw.yml` file, and MUST remain within the top-level
package root after normalization. Absolute paths and paths that escape that root MUST be
rejected.

Imports are recursive. Implementations MUST detect import cycles and report the manifest
path chain forming the cycle. Each manifest MUST be parsed and validated before its package
assets are added to a unified install list. Imported workflows, resources, skills, and
agents MUST retain paths relative to the directory containing their manifest. Top-level
package metadata and `config` MUST NOT be replaced by imported manifest values.

The unified install list MUST be checked before installation. Two files that resolve to the
same case-insensitive destination MUST cause resolution to fail. A manifest that declares
imports but no direct installable files MUST NOT trigger workflow auto-discovery in its own
directory.

### 4.11 `includes`

If present, `includes` MUST be an array whose entries are either strings or mappings.

**String entries** follow the same rules as `files` (§4.10), with one special case that MUST be preserved for backward compatibility: a string entry beginning with `.github/` is resolved relative to the **consuming repository root**, not relative to the package root, even for nested packages. All other string entries (for example `workflows/review.md`) are resolved relative to the package root.

String entries MAY use one wildcard as their final path segment (for example, `workflows/*`). The wildcard MUST be exactly `*`, MUST be preceded by `/`, and MUST NOT appear elsewhere in the path. It matches supported direct children of the named directory and MUST NOT recurse into nested directories. Implementations MUST apply the same workflow, skill, and agent path validation used for explicit string entries to every match, MUST ignore unsupported matches, and MUST preserve deterministic lexical ordering. A `.github/` wildcard retains the repository-root-relative behavior described above.

**Mapping entries** declare an explicit source-to-destination install mapping and MUST contain:

| Key | Type | Required | Meaning |
| --- | --- | --- | --- |
| `source` | string | Yes | Path of the file to install, always resolved relative to the package root, including for nested packages. The `.github/` special case of string entries MUST NOT apply. |
| `destination` | string | Yes | Install path, resolved relative to the consuming repository root. |
| `kind` | string | No | Either `agentic-workflow` or `action-workflow`. When present, it MUST match the file extension of `source`. |

Mapping entries let a distribution repository keep executable workflow assets inert outside its own `.github/workflows/` directory and still install them into the consuming repository's `.github/workflows/`.

Implementations MUST reject a mapping entry when any of the following holds:

- `source` or `destination` is empty, absolute, or contains a path-traversal sequence that escapes its root;
- `source` resolves to a symbolic link or to a path outside the package root;
- `source` or `destination` does not end in `.md` or `.yml`, or ends in `.lock.yml`;
- `destination` is not a direct child of `.github/workflows/`;
- the file extension of `destination` differs from the file extension of `source`;
- `kind` is present and does not match the kind implied by the `source` extension.

Implementations MUST detect two entries that resolve to the same `destination` and MUST fail before writing any file.

Mapping entries follow the same install semantics as string entries: `.md` sources are compiled under their destination file name, and `.yml` sources are copied verbatim. Package-provided post-install shell code MUST NOT be executed.

`gh aw add`, `gh aw add-wizard`, and `gh aw update` MUST use identical mapping semantics, and `gh aw update` MUST continue to track the manifest source of installed files.

### 4.12 `resources`

If present, `resources` MUST be an array of mappings. Each mapping MUST contain:

| Key | Type | Required | Meaning |
| --- | --- | --- | --- |
| `source` | string | Yes | Package-relative path of the asset to copy. |
| `destination` | string | Yes | Repository-root-relative destination path. |

Resource `source` and `destination` values MUST NOT be absolute paths and MUST NOT escape their roots through path traversal. Local package sources MUST NOT be symbolic links, directories, or other non-regular file replacements.

Resource destinations are restricted to non-hook repository asset namespaces:

- `.github/ISSUE_TEMPLATE/*.yml`
- `.github/ISSUE_TEMPLATE/*.yaml`
- `.github/CODEOWNERS`
- `.github/aw/**`

Implementations MUST reject duplicate or case-insensitive duplicate resource destinations before writing files. Resources are copied as inert content from the selected package ref; installers MUST NOT execute package-provided scripts or expose configured secrets to package content during installation.

For each package installation, implementations MUST record package-scoped ownership metadata under `.github/aw/packages/`. The record MUST identify the package source, resolved immutable commit/ref, installed destination paths, source paths, and SHA-256 content digests. Implementations MUST refuse to overwrite existing resource files unless they are unchanged files owned by the same package, or unless the user explicitly passes `--force`.

### 4.13 `config`

The experimental `config` field MAY contain `repo-label` actions. A `repo-label`
action MUST contain a non-empty `name` string of at most 50 characters, a
non-empty `description` string of at most 100 characters, and a `color` matching
exactly six hexadecimal characters without a leading `#`.

When applied, the installer MUST create a missing label. If a label with the
same name already exists, the installer MUST update its description or color
when either differs. If all declared values already match, the action MUST have
no effect.

## 5. Installable file resolution

Supported installable paths are:

- `workflows/<name>.md`
- `.github/workflows/<name>.md`
- `.github/workflows/<name>.yml` (raw GitHub Actions YAML; direct children only, `.lock.yml` excluded)

Mapping entries in `includes` (§4.11) may declare any package-relative `source`; their `destination` MUST be a direct child of `.github/workflows/`.

Nested descendants under the markdown directories are also valid when referenced explicitly in `files`. Raw `.yml` action workflows MUST be direct children of `.github/workflows/`; nested `.yml` files are rejected.

Raw `.yml` action workflows are installed verbatim: `gh aw add` copies the file to `.github/workflows/<name>.yml` and performs no frontmatter parsing, no dependency resolution, and no compilation. No `.lock.yml` is produced.

If `files` is present, valid entries are used as the installable workflow set. Invalid entries MUST be ignored with a warning.

If `files` is omitted, or if no valid entries remain after filtering, the implementation MUST attempt discovery under:

- `workflows/`
- `.github/workflows/`

Auto-discovery considers only agentic workflow markdown (`.md`); raw `.yml` action workflows MUST be referenced explicitly in `files` to be installed.

If no installable package assets are resolved (workflows, resources, skills, or agents), package validation MUST fail.

### 5.1 Install

The install lifecycle (invoked by `gh aw add`) MUST proceed in the following order:

1. **Resolve** the package manifest and validate it per §4 and §7.
2. **Resolve** the installable file list per §5.
3. **Download** each resolved file from the remote package source.
4. **Compile** each agentic workflow markdown file into the target repository's workflow directory. Raw `.yml` files are copied verbatim without compilation.
5. **Copy** declared `resources` as inert repository assets without executing them.
6. **Write** all output files and package ownership metadata atomically before reporting success.

If any step fails, the implementation MUST abort and MUST NOT leave partial output files in the target directory. The implementation SHOULD emit an actionable error identifying the failing step. See §10 (Safeguards) for the normative rollback and permission-error requirements that apply to this lifecycle (R-PKG-003, R-PKG-004, R-PKG-006, R-PKG-007).

### 5.2 Update

The update lifecycle re-installs a package at a newer (or specified) version, overwriting existing files from the previous installation.

**R-PKG-U001**: `gh aw add` with a version specifier (e.g., `owner/repo@v2.0.0`) MUST overwrite previously installed files from the same package with the new version's files, following the same install ordering defined in §5.1.

**R-PKG-U002**: Files that were present in the previous installation but are absent from the new version's resolved package-managed file list MUST be removed only when all of the following hold: (a) they are owned by the same package, (b) they are unchanged from the recorded digest, and (c) no replacement from the new version maps to the same path. When a new version entry maps to the same path, overwrite behavior is governed by R-PKG-U001. Implementations SHOULD warn when stale files are preserved because they were modified or ownership cannot be proven.

**R-PKG-U003**: If overwriting a file fails (for example, due to a filesystem permission error or a locked file), the implementation MUST abort the update and MUST NOT leave the target directory in a mixed state combining old and new file versions. The implementation MUST emit an error identifying the file that could not be overwritten and the reason.

**R-PKG-U004**: A failed update MUST leave the previously installed files intact. Implementations SHOULD NOT delete the old files before confirming the new files can be written successfully.

### 5.3 Remove

The remove lifecycle uninstalls a previously installed package by deleting its installed files.

**R-PKG-R001**: Removal MUST delete only files that were installed by the package being removed. Files that were installed by other packages or created manually by the user MUST NOT be deleted.

**R-PKG-R002**: If a file to be removed has been modified since installation (detected by checksum or modification timestamp comparison), the implementation SHOULD warn the user and MUST NOT delete the file without explicit confirmation.

**R-PKG-R003**: If deletion of any installed file fails (for example, due to a filesystem permission error), the implementation MUST emit an error identifying the file and reason, and MUST continue attempting to remove the remaining files rather than aborting immediately. The implementation MUST report a final summary listing all files that could not be removed. See §10.4 (Safeguards — Filesystem Permission Errors) for the normative requirement on permission-error reporting (R-PKG-007).

**R-PKG-R004**: After removal, if the target workflow directory is empty, the implementation MAY remove the empty directory. The implementation MUST NOT remove non-empty directories.

> **Note**: Package-installed documentation files (for example `README.md`) are within the scope of R-PKG-R001 and follow modified-file protection in R-PKG-R002.

## 6. Documentation

Package documentation is `README.md` in the package root.

Examples:

- Repository-root package: `README.md`
- Nested package: `path/to/package/README.md`

If `README.md` is absent, package validation MUST fail.

## 7. Validation and errors

Validation MUST fail for at least the following conditions:

- manifest file not found at the resolved package root;
- malformed YAML;
- top-level document is not a mapping;
- missing or empty `name`;
- unsupported `manifest-version`;
- invalid `min-version`;
- current compiler version is lower than `min-version`;
- unknown top-level fields, including `docs`; or
- missing required `README.md`; or
- no installable package assets (workflows, resources, skills, or agents) resolved.

Implementations SHOULD emit warnings for at least the following conditions:

- a `files` entry is ignored because it is not a supported installable path; or
- `description` exceeds 255 characters.

## 8. Compile validation

When `gh aw compile` encounters a repository-root `aw.yml`, it validates that manifest before compiling workflows.

A conforming compiler:

- MUST parse and validate the manifest according to this specification;
- MUST fail compilation on manifest errors;
- SHOULD surface warnings as `manifest_warning`; and
- SHOULD surface errors as `manifest_error`.

If JSON output is requested, manifest validation failure still causes an overall compilation failure result.

## 9. Examples

### 9.1 Repository-root package

```yaml
min-version: v0.38.0
name: Repo Assist
emoji: 🤖
description: Friendly repository automation for review and issue triage
files:
  - workflows/review.md                # agentic workflow — compiled on install
  - .github/workflows/nightly-review.md
  - .github/workflows/ci.yml           # raw Actions YAML — copied verbatim
```

### 9.2 Nested package folder

Package reference:

```text
owner/repo/packages/repo-assist
```

Manifest location:

```text
packages/repo-assist/aw.yml
```

Manifest:

```yaml
name: Repo Assist
files:
  - workflows/review.md
```

Documentation file:

```text
packages/repo-assist/README.md
```

---

## 10. Safeguards

This section defines normative safeguards that conforming implementations MUST apply to protect against configuration errors, filesystem failures, and partial-installation states.

### 10.1 Name Collision

**R-PKG-001**: If installing a package would overwrite a file in the target directory that was not installed by any tracked package, the implementation MUST warn the user and MUST NOT overwrite the file without explicit confirmation. This prevents silent clobbering of user-created or manually placed workflow files.

**R-PKG-002**: If two packages being installed in the same operation resolve to overlapping output file paths (name collision between packages), the implementation MUST abort the installation of both conflicting packages with an error identifying the conflicting paths and package names.

### 10.2 Partial-Install Failure Recovery

**R-PKG-003**: If any file write during the install lifecycle (§5.1) fails, the implementation MUST abort and MUST NOT leave partial output in the target directory. The implementation MUST attempt to roll back any files already written in the current install operation before reporting failure.

**R-PKG-004**: If rollback itself fails (for example, because a partially written file cannot be deleted), the implementation MUST report both the original install failure and the rollback failure in the error output, identifying each affected file by path.

### 10.3 Absent `README.md` During Install

**R-PKG-005**: If `README.md` is absent at package validation time (§6), the implementation MUST fail validation before any files are downloaded or written to the target directory. A missing `README.md` discovered mid-install (after file resolution has begun) is treated as a validation failure; the install MUST be aborted and any files already written MUST be rolled back per R-PKG-003.

### 10.4 Filesystem Permission Errors

**R-PKG-006**: Before writing any output files, the implementation SHOULD verify that the target directory is writable. If a write-permission check indicates that installation will fail, the implementation MUST report the permission error before beginning any file writes.

**R-PKG-007**: If a filesystem permission error occurs during file writing after the install has begun, the implementation MUST treat it as a partial-install failure per R-PKG-003 and MUST include the permission-denied path in the error message.

---

## 11. Norms

This section provides a normative reference table for all MUST/SHALL requirements defined in §§4–10 of this specification. Requirements that have been assigned an explicit `R-PKG-*` identifier are listed with that identifier; requirements that do not yet carry an explicit identifier are shown with `—` in the ID column and may be assigned identifiers in a future revision.

### 11.1 Manifest Format Norms (§4)

| ID | Section | Normative Requirement |
|---|---|---|
| — | §4.2 | `manifest-version` MUST equal `"1"`; any other value MUST be rejected |
| — | §4.3 | `min-version` MUST use `vMAJOR.minor.patch` form; MUST fail if compiler version is lower |
| — | §4.4 | `name` MUST be present and non-empty after trimming whitespace |
| — | §4.10 | Each `files` entry MUST be resolved relative to the package root and MUST match a supported installable path |
| — | §4.10 | Each `files` entry MUST NOT contain a path-traversal sequence (`../`); entries that escape the package root MUST be rejected |
| — | §4 (preamble) | Unknown top-level fields MUST be rejected |

### 11.2 File Resolution Norms (§5)

| ID | Section | Normative Requirement |
|---|---|---|
| — | §5 | Invalid `files` entries MUST be ignored with a warning |
| — | §5 | If no installable workflow files are resolved, package validation MUST fail |
| — | §5 | Raw `.yml` files MUST be direct children of `.github/workflows/`; nested `.yml` files are rejected |
| R-PKG-U001 | §5.2 | Update MUST overwrite previously installed files with the new version |
| R-PKG-U002 | §5.2 | Orphaned files (present in old version, absent in new) MUST be left in place with a warning |
| R-PKG-U003 | §5.2 | Failed overwrite MUST abort update; MUST NOT leave a mixed-version directory |
| R-PKG-U004 | §5.2 | Failed update MUST leave previously installed files intact |
| R-PKG-R001 | §5.3 | Removal MUST delete only package-installed files |
| R-PKG-R002 | §5.3 | Modified files SHOULD be warned about; MUST NOT be deleted without confirmation |
| R-PKG-R003 | §5.3 | Per-file deletion failures MUST be reported; remaining removals MUST continue |
| R-PKG-R004 | §5.3 | Empty directories MAY be removed after removal; non-empty directories MUST NOT be removed |

### 11.3 Documentation Norms (§6)

| ID | Section | Normative Requirement |
|---|---|---|
| — | §6 | If `README.md` is absent, package validation MUST fail |

### 11.4 Validation and Error Norms (§7)

| ID | Section | Normative Requirement |
|---|---|---|
| — | §7 | Malformed YAML MUST cause validation failure |
| — | §7 | Missing or empty `name` MUST cause validation failure |
| — | §7 | Unsupported `manifest-version` MUST cause validation failure |
| — | §7 | Invalid `min-version` MUST cause validation failure |
| — | §7 | Compiler version below `min-version` MUST cause validation failure |
| — | §7 | Unknown top-level fields (including `docs`) MUST cause validation failure |

### 11.5 Compile Validation Norms (§8)

| ID | Section | Normative Requirement |
|---|---|---|
| — | §8 | Conforming compiler MUST parse and validate the manifest before compiling workflows |
| — | §8 | Conforming compiler MUST fail compilation on manifest errors |

### 11.6 Safeguard Norms (§10)

| ID | Section | Normative Requirement |
|---|---|---|
| R-PKG-001 | §10.1 | Untracked file collision MUST warn and require confirmation before overwrite |
| R-PKG-002 | §10.1 | Cross-package name collision MUST abort installation of both conflicting packages |
| R-PKG-003 | §10.2 | Write failure MUST abort install and roll back already-written files |
| R-PKG-004 | §10.2 | Rollback failure MUST be reported alongside the original install failure |
| R-PKG-005 | §10.3 | Absent `README.md` discovered mid-install MUST abort and roll back |
| R-PKG-006 | §10.4 | Target directory write-permission SHOULD be checked before writing any files |
| R-PKG-007 | §10.4 | Permission error during file writing MUST trigger partial-install failure handling per R-PKG-003 |

---

## 12. Sync Notes

This section maps normative sections of this specification to the implementation files in `pkg/cli/` and `pkg/parser/` that realize each requirement.

**Last verified**: 2026-07-08

### §4 Manifest Format — Implementation Mapping

| Spec Section | Description | Implementation File(s) |
|---|---|---|
| §4.1 Fields | Manifest YAML field definitions and required-field validation | `pkg/cli/add_package_manifest.go` (`parseRepositoryPackageManifest`) |
| §4.2 `manifest-version` | Version equality check (`"1"` only); any other value rejected | `pkg/cli/add_package_manifest.go` (constant `repositoryPackageManifestVersion = "1"`) |
| §4.3 `min-version` | Semver form validation (`vMAJOR.minor.patch`); compiler version comparison via `semverutil.Compare` | `pkg/cli/add_package_manifest.go` (`isSupportedManifestMinVersion`, `semverutil.Compare`) |
| §4.4 `name` | Non-empty-after-trim check | `pkg/cli/add_package_manifest.go` (name validation) |
| §4 Unknown fields | Unknown top-level keys rejected | `pkg/cli/add_package_manifest.go` (unknown-field guard) |

### §5 Installable File Resolution — Implementation Mapping

| Spec Section | Description | Implementation File(s) |
|---|---|---|
| §5 File resolution | Resolving `files` list vs. auto-discovery under `workflows/` and `.github/workflows/` | `pkg/cli/add_package_manifest.go` (`resolveRepositoryPackage`) |
| §5 Install ordering | Download → compile → write per-file sequencing | `pkg/cli/add_package_manifest.go`, `pkg/cli/add_command.go` |
| §5 Install rollback on write failure (R-PKG-003) | Write-failure abort plus rollback of files written earlier in the same add operation | `pkg/cli/add_command.go` (`addWorkflowsWithTracking`), `pkg/cli/add_command_test.go` (`TestAddWorkflowsWithTracking_RollsBackWrittenFilesOnWriteFailure`) |

### §4.2 and §4.3 Verification Findings

**`manifest-version` (§4.2)**: `pkg/cli/add_package_manifest.go` declares `const repositoryPackageManifestVersion = "1"` and rejects any string value other than `"1"` with an error. The check is applied in `parseRepositoryPackageManifest`. ✅ Conforming.

**`min-version` semver comparison (§4.3)**: `pkg/cli/add_package_manifest.go` calls `isSupportedManifestMinVersion` (which validates the `vMAJOR.minor.patch` form using `semverutil.IsActionVersionTag`) and then `semverutil.Compare(currentVersion, manifest.MinVersion)` to reject compilers that are older than `min-version`. The comparison is a strict semantic version comparison, not a string comparison. ✅ Conforming.

### §6 Documentation — Implementation Mapping

| Spec Section | Description | Implementation File(s) |
|---|---|---|
| §6 `README.md` requirement | Absent `README.md` fails validation | `pkg/cli/add_package_manifest.go` (README presence check) |

---

## Change Log

### Version 0.2.0 (Draft)

- **Added**: §5.1 Install ordering sub-section defining the five-step install lifecycle and rollback requirement.
- **Added**: §5.2 Update lifecycle with normative rules R-PKG-U001 through R-PKG-U004 covering re-install, orphaned files, failure handling, and old-file preservation.
- **Added**: §5.3 Remove lifecycle with normative rules R-PKG-R001 through R-PKG-R004 covering file scope, modified-file handling, per-file error continuation, and directory cleanup.
- **Added**: §10 Safeguards with normative rules R-PKG-001 through R-PKG-007 covering name collision, partial-install failure recovery, absent `README.md` mid-install, and filesystem permission errors.
- **Added**: §11 Norms reference table (`R-PKG-*` IDs) mapping all MUST/SHALL requirements in §§4–10.
- **Added**: §12 Sync Notes mapping §§4–6 to implementation files in `pkg/cli/` with verification findings for `manifest-version` and `min-version` handling (last verified 2026-06-01).

### Version 0.1.0 (Draft)

- Initial specification defining manifest format, file resolution, documentation, validation, compile validation, and examples.
