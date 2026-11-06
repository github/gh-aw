---
title: APM Dependencies
description: Install and manage APM (Agent Package Manager) packages in your agentic workflows, including skills, prompts, instructions, agents, hooks, and plugins.
sidebar:
  order: 330
---

[APM (Agent Package Manager)](https://microsoft.github.io/apm/) manages AI agent primitives such as skills, prompts, instructions, agents, hooks, and plugins (including the Claude `plugin.json` specification). Packages can depend on other packages and APM resolves the full dependency tree.

APM is configured by importing the `shared/apm.md` workflow, which creates a dedicated `apm` job that packs packages and uploads the bundle as a GitHub Actions artifact. The agent job then downloads and unpacks the bundle for deterministic startup.

## Native manifest-backed skills

The `apm:` frontmatter field consumes a committed `apm.yml` and its adjacent `apm.lock.yaml` directly in Go. It does not install or invoke `microsoft/apm` or `microsoft/apm-action`. The path is relative to the workflow file and must remain inside the repository, including through symlinks. A root manifest is referenced as:

```aw wrap
engine: copilot
apm: ../../apm.yml
```

The compiler converts supported dependencies into SHA-pinned native `skills:` installations during activation. Object-form `path` and lockfile `virtual_path` fields must contain paths only; `#ref` fragments are rejected rather than discarded. Existing explicit skill authentication is preserved when the same exact reference is also declared in `skills:`. Conflicting explicit pins are rejected.

```yaml title="apm.yml"
name: code-review
version: 1.0.0
targets: copilot
dependencies:
  apm:
    - octo-org/octo-skills/skills/review#main
```

```yaml title="apm.lock.yaml"
lockfile_version: "1"
dependencies:
  - repo_url: https://github.com/octo-org/octo-skills
    virtual_path: skills/review
    resolved_ref: main
    resolved_commit: 0123456789abcdef0123456789abcdef01234567
    depth: 1
```

The SHA above is illustrative; a lockfile must record the actual approved commit. Native compilation never resolves mutable refs or creates or rewrites either document. Missing locks, missing or undeclared direct dependencies, changed manifest refs, and mismatched full-SHA pins fail compilation. Both documents participate in the stale-check hash, so a lock-only Dependabot branch update requires recompilation.

| Surface | Native compiler support |
|---------|-------------------------|
| Direct GitHub skill-directory dependencies | String shorthand with an explicit `#ref`, or `git` objects with `path` and `ref` |
| Repository-wide skill bundles | Requires `package_type: skill_bundle` in the lockfile; installs native skills only |
| Manifest `target` / `targets` | Must allow the workflow engine |
| `devDependencies` | Validated but not installed in workflow runs |
| Registry/local sources, transitive packages, aliases, subsets, Git SemVer constraints | Rejected in native compiler mode |
| MCP/LSP deployment, APM scripts, includes, compilation settings, policy enforcement, marketplace authoring, replacements | Not implemented by native compiler mode; declarations requiring these behaviors are rejected |
| Wasm compiler | Manifest-backed compilation is unavailable |

The Go document reader embeds the [OpenAPM manifest schema revision 0.1.48](https://microsoft.github.io/apm/specs/schemas/manifest-v0.1.48.schema.json) and [lockfile schema v0.1](https://microsoft.github.io/apm/specs/schemas/lockfile-v0.1.schema.json). These are draft contracts, not a claim of full APM resolver/deployer conformance. SemVer metadata is advisory, following the normative prose.

`pkg/apm` is a separate document library with no gh-aw internal dependencies. `ParseManifest`, `ParseLockfile`, typed `APMDependencies`, `ServiceDependencies`, `TargetRestrictions`, lossless field access, and `Bytes` can be reused by other compiler surfaces. The library accepts standard registry, local, Git, and MCP declaration forms; the narrower native skill-consumption policy lives in `pkg/workflow`, not in the library. It performs no filesystem, network, process, or deployment operations.

Schema-less documents accept conventional YAML 1.2 scalar types for compatibility with existing manifests and Dependabot-generated locks. An explicit `$schema` selects the pinned document's stricter OpenAPM string-scalar profile: numeric and boolean fields require explicit canonical tags such as `depth: !!int 1` and `is_dev: !!bool false`. Unknown schema identities, anchors, aliases, merge keys, custom tags, duplicate keys, and multiple documents are rejected. Unknown fields and extensions are retained without loss by the reader. Unsupported newer draft features are not enabled implicitly.

For Dependabot configuration and update behavior, see [Dependabot Manifest Generation](/gh-aw/reference/dependabot/#apm-version-updates). Full APM package materialization remains available through the shared component described below; its isolated inline-package mode does not consume the repository manifest or lockfile.

## Where `shared/apm.md` comes from

`shared/apm.md` is a **local workflow file** that gh-aw resolves at `.github/workflows/shared/apm.md` in your repository — it is not a remote import (the `uses:` syntax inside `imports:` is gh-aw's local-import shape, not GitHub Actions' `uses: owner/repo@ref`).

The canonical source is maintained in [microsoft/apm](https://github.com/microsoft/apm/blob/main/.github/workflows/shared/apm.md). Add it to your repository with:

```bash
gh aw add microsoft/apm/.github/workflows/shared/apm.md --dir shared
```

Running `gh aw update` will keep your vendored copy in sync with the canonical source.

The canonical version pins `microsoft/apm-action@v1.5.0` and supports multi-org GitHub App authentication (`apps:[]`) and multi-bundle restore.

## Usage

Import `shared/apm.md` and supply the list of packages via the `packages` parameter:

```aw wrap
imports:
  - uses: shared/apm.md
    with:
      packages:
        - microsoft/apm-sample-package
        - github/awesome-copilot/skills/review-and-refactor
        - anthropics/skills/skills/frontend-design
```

## Reproducibility and governance

Committed APM lock files (`apm.lock.yaml`) pin resolved packages to exact commit SHAs. Native manifest-backed compilation consumes these pins. The shared component's isolated inline-package mode creates its own temporary manifest and resolution state; a repository lockfile does not govern that mode. See the [APM governance guide](https://microsoft.github.io/apm/enterprise/governance/) for policy enforcement and access controls.

## Package reference formats

Each entry in `packages` is an APM package reference. Supported formats:

| Format | Description |
|--------|-------------|
| `owner/repo` | Full APM package |
| `owner/repo/path/to/primitive` | Individual primitive (skill, instruction, plugin, etc.) from a repository |
| `owner/repo#ref` | Package pinned to a tag, branch, or commit SHA |

### Examples

```aw wrap
imports:
  - uses: shared/apm.md
    with:
      packages:
        # Full APM package
        - microsoft/apm-sample-package
        # Individual primitive from any repository
        - github/awesome-copilot/skills/review-and-refactor
        # Plugin (Claude plugin.json format)
        - github/awesome-copilot/plugins/context-engineering
        # Version-pinned to a tag
        - microsoft/apm-sample-package#v2.0
        # Version-pinned to a branch
        - microsoft/apm-sample-package#main
```

## How it works

The `shared/apm.md` import adds a dedicated `apm` job to the compiled workflow. This job runs `microsoft/apm-action` to install packages and create a bundle archive, which is uploaded as a GitHub Actions artifact. The agent job downloads and restores the bundle as pre-steps, making all skills and tools available at runtime.

Packages are fetched using the cascading token fallback: `GH_AW_PLUGINS_TOKEN` → `GH_AW_GITHUB_TOKEN` → `GITHUB_TOKEN`.

To reproduce or debug the pack/unpack flow locally, run `apm pack` and `apm unpack` directly. See the [pack and distribute guide](https://microsoft.github.io/apm/guides/pack-distribute/) for instructions.

## Pull request runs

For pull request triggers, the agent job replaces agent config folders (`.agents`, `.github`, and the engine folder such as `.claude`) with a base-branch snapshot so a fork PR cannot inject skill or instruction files.

## Reference

| Resource | URL |
|----------|-----|
| APM documentation | https://microsoft.github.io/apm/ |
| APM governance guide | https://microsoft.github.io/apm/enterprise/governance/ |
| Pack and distribute guide | https://microsoft.github.io/apm/guides/pack-distribute/ |
| gh-aw integration (APM docs) | https://microsoft.github.io/apm/integrations/gh-aw/ |
| apm-action (GitHub) | https://github.com/microsoft/apm-action |
| microsoft/apm (GitHub) | https://github.com/microsoft/apm |
| shared/apm.md (canonical) | https://github.com/microsoft/apm/blob/main/.github/workflows/shared/apm.md |
