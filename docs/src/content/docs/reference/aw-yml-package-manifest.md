---
title: Package Manifest (aw.yml)
description: Reference for the aw.yml package manifest used by gh aw add and gh aw compile.
sidebar:
  order: 320
---

Use `aw.yml` to describe an installable agentic workflow package.
`gh aw add` uses this manifest when installing packages, and
`gh aw compile` validates repository-root manifests before compilation.

For the normative file-format definition, see the
[Package Management (Spec)](/gh-aw/specs/repository-package-manifest-specification/).

## Package reference formats

Repository references support two forms:

- `OWNER/REPO`
- `OWNER/REPO/PATH/TO/PACKAGE`

The package root is the folder that contains `aw.yml`.

## Fields

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `manifest-version` | string | No | Current supported value: `"1"`. Defaults to `"1"` when omitted. |
| `min-version` | string | No | Minimum compatible `gh aw` version in `vMAJOR.minor.patch` form, such as `v0.38.0`. |
| `name` | string | Yes | Human-readable package name. Must be non-empty after trimming whitespace. |
| `emoji` | string | No | Optional package emoji for display in package metadata. |
| `icon` | string | No | Optional package icon: an emoji, a GitHub primer octicon name in `:name:` format (e.g. `:check-circle:`), or a package resource path to an SVG file. |
| `description` | string | No | Optional package description. `gh aw add` warns when it exceeds 255 characters. |
| `private` | boolean | No | Marks the package as unavailable for installation. Defaults to `false`; `gh aw add` refuses packages set to `true`. |
| `experimental` | boolean | No | Marks the package as experimental. Defaults to `false`; `gh aw add` displays a warning when set to `true`. |
| `files` | array of strings | No | Deprecated; use `includes`. Package-root-relative paths. Agentic markdown workflows under `workflows/` or `.github/workflows/`; raw GitHub Actions YAML (`.yml`) is also accepted as direct children of `.github/workflows/`. |
| `includes` | array | No | Installable entries, or paths to other `aw.yml` manifests whose installable files are included recursively. Each entry is either a path string (same rules as `files`, plus skill and agent paths), a path ending in `/*` that matches supported direct children, or a source-to-destination mapping. |
| `resources` | array | No | Repository assets copied from package-relative `source` paths to allowlisted repository-relative `destination` paths. |
| `config` | array | No | Experimental ordered repository setup actions applied by `gh aw add-wizard`. |

### Repository labels

Use a `repo-label` config action to create a repository label or reconcile an
existing label's description and color:

```yaml
config:
  - type: repo-label
    name: automation
    description: Managed by an agentic workflow
    color: 1f6feb
```

The `name` must be non-empty and at most 50 characters. The `description` must
be non-empty and at most 100 characters. `color` must be a six-character
hexadecimal value without a leading `#`.

## Imported manifests

Use `includes` entries naming `aw.yml` files to compose a package from manifests in the same repository:

```yaml
name: Central Agentic Ops
includes:
  - activity/aw.yml
  - ambient-context/aw.yml
  - dashboard/aw.yml
```

These paths are resolved relative to the manifest that declares them and must name an
`aw.yml` file within the top-level package root. Imports are recursive. The imported
manifests' workflows, resources, skills, and agents are combined into one install list;
metadata and `config` continue to come from the top-level manifest.

`gh aw` rejects import cycles and any files that would install to the same destination,
including case-insensitive destination clashes. A manifest that only declares imports does
not auto-discover workflows from its own directory.

## Installable workflows

If `files` is present, valid entries become the install bundle. Two entry kinds are supported:

- **Agentic workflow markdown** — paths ending in `.md` under `workflows/` or `.github/workflows/`. `gh aw add` compiles these to lock files and fetches their dependencies.
- **Raw GitHub Actions YAML** — paths ending in `.yml` (but not `.lock.yml`) that are direct children of `.github/workflows/`. `gh aw add` copies these verbatim to `.github/workflows/<name>.yml` with no frontmatter processing, no dependency fetch, and no compilation. Nested subdirectories under `.github/workflows/` and `.yml` files under `workflows/` are not accepted.

### Path resolution rules

- A **string entry** that starts with `.github/` is resolved relative to the **consuming repository root**, even inside a nested package. For example, `.github/workflows/nightly.md` in `factory/aw.yml` refers to the repository-root file, not to `factory/.github/workflows/nightly.md`.
- Every other string entry (such as `workflows/review.md`) is resolved relative to the package root.
- A string entry may end in a single `/*` wildcard (such as `workflows/*`) to include supported direct children of that directory. The wildcard does not recurse, and `*` is not supported in any other position. Matches are filtered by the same workflow, skill, and agent path rules as explicit entries.
- A **mapping entry** always resolves `source` relative to the package root and `destination` relative to the consuming repository root.
- A mapping `source` may end in `/*`. For wildcard mappings, `destination` must be the `.github/workflows/` folder, and each matching file keeps its source filename.

### Source-to-destination mappings

Use mapping entries to keep workflow assets inert in the distribution repository while still installing them into the consuming repository's `.github/workflows/`:

```yaml
name: Factory
includes:
  - source: payload/workflows/reviewer.md
    destination: .github/workflows/reviewer.md
    kind: agentic-workflow
  - source: payload/workflows/controller.yml
    destination: .github/workflows/controller.yml
    kind: action-workflow
  - source: payload/extra-workflows/*
    destination: .github/workflows/
```

With a nested package reference such as `owner/repo/factory`, the files above are fetched from `factory/payload/workflows/` and installed to `.github/workflows/`. Because the sources live outside `.github/workflows/` in the distribution repository, they never run there.

The optional `kind` field is either `agentic-workflow` (`.md`) or `action-workflow` (`.yml`) and must match the source extension.

Mappings are rejected when `source` or `destination` is absolute, contains `..`, points at a symbolic link, uses an unsupported extension (or `.lock.yml`), changes the file extension between source and destination, or targets anything other than a direct child of `.github/workflows/`. A wildcard mapping is the exception: it targets the `.github/workflows/` folder, then preserves each matching source filename. Two entries installing to the same destination are rejected before any file is written.

`gh aw add`, `gh aw add-wizard`, and `gh aw update` all use these same mapping rules.

String entries can also install skill directories under `skills/` or `.github/skills/` when they contain `SKILL.md`, and agent Markdown files under `agents/` or `.github/agents/`.

If `files` is omitted, or no valid entries remain after filtering,
`gh aw add` discovers installable markdown files under:

- `workflows/`
- `.github/workflows/`

If no installable workflow files are resolved, validation fails.

## Resources

The `resources` field installs inert repository assets. Each entry maps a package-relative `source` to a repository-relative `destination`. Supported destinations are:

- Direct children of `.github/ISSUE_TEMPLATE/` with a `.yml` or `.yaml` extension
- `.github/CODEOWNERS`
- Files under `.github/aw/`

Resource destinations must be unique, including case-insensitive comparisons. Path traversal, symbolic links, non-regular local files, and destinations outside the allowlist are rejected. Installed resources are tracked with package-scoped ownership metadata in `.github/aw/packages/*.json`.

## Package documentation

Package documentation must be `README.md` at the package root.
The manifest does not support a `docs` field.

Missing `README.md` causes package validation to fail.

The embedded JSON schema source of truth is `pkg/parser/schemas/aw_manifest_schema.json`.

## Example

```yaml
name: Repo Assist
emoji: 🤖
description: Friendly repository automation for review and issue triage
includes:
  - packages/common/aw.yml
  - workflows/review.md                # agentic workflow — compiled on install
  - .github/workflows/nightly-review.md # repository-root-relative string entry
  - .github/workflows/ci.yml           # raw Actions YAML — copied verbatim
  - source: payload/workflows/reviewer.md   # package-relative source
    destination: .github/workflows/reviewer.md
```
