# ADR-79627: Use a Standalone Private Repository for Confidential Security Remediation

**Date**: 2026-11-21
**Status**: Accepted
**Deciders**: pelikhan

---

### Context

Security fixes for code scanning alerts and repository advisories sometimes must be developed privately before disclosure. Agentic remediation needs CI, clone-scoped secrets, durable branches and pull requests, and the selected alert and advisory context, but it must not query the upstream repository at runtime or carry upstream credentials. GitHub advisory temporary private forks do not support CI or secrets, and forks of public repositories must remain public. Source, context, and workflows must therefore reach a repository whose visibility, fork status, and Actions state have been verified first, and the operator wants one command that ends with the investigation running.

### Decision

We will add `gh aw private-clone`, which creates a new standalone repository with visibility exactly `private` (not a fork; `internal` is rejected) on a single GitHub host. The clone is also the side repository: it hosts the operator-supplied workflow bundle, private remediation PRs, and run evidence.

Setup is local first. The command rejects sources that use Git submodules or Git LFS, removes inherited workflows, quarantines inherited agent instruction files and compiler caches, installs the bundle, downloads offline alert and advisory context, and compiles strictly. Bundles may not use remote or Markdown body imports, `aw.json`, symlinks, precompiled locks, or native Actions YAML. GitHub tools are pinned to the clone and its `GITHUB_TOKEN` even when `tools.github` is absent, and `lockdown` is rejected. The selected tree is committed as an independent root commit with the bootstrap commit on top, so no upstream history is published.

Publication happens in a fixed order: create the repository, disable Actions, verify that it is private, not a fork, and Actions-disabled, then push only `main` and the `private-clone/bootstrap` tag. By default the command then sets operator `--secret` values from the environment, enables Actions, and dispatches the `workflow_dispatch` starter, whose required inputs must have defaults. Invoking the command is the operator's authorization to run their own locally compiled bundle. `--no-start` keeps Actions disabled for a separate review.

### Alternatives Considered

#### Alternative 1: Repository security advisory temporary private fork

Advisory forks are the built-in path for private security work and are already linked to the advisory. They were rejected because they do not support CI or secrets, so agentic workflows cannot run there.

#### Alternative 2: Regular fork of the upstream repository

A fork keeps upstream history and normal CI. It was rejected because forks of public repositories must stay public, and a fork inherits the upstream fork relationship, workflows, and configuration rather than providing a clean disposable boundary.

#### Alternative 3: Separate side repository plus a private clone

A dedicated automation repository could host workflows and act on a separate private clone. It was rejected by the owner to limit the number of repositories per remediation; the clone hosts its own automation instead.

#### Alternative 4: Leave Actions disabled until a separate activation command

Setup could stop with Actions disabled and require a second, separately authorized activation. The owner directed that the flow must activate Actions and start the investigation. This remains available as `--no-start` for cases where someone other than the invoking operator must review the repository first.

#### Alternative 5: Enclave-backed isolated execution

Enclaves isolate bounded computations and disclosure but do not provide durable repositories or PR lifecycles, and their current agent GitHub tool surface supports only `list_issues` and `issue_read`. This is deferred, not permanently rejected: it would need an offline source and context staging contract and a validated patch-export path.

### Consequences

#### Positive
- Confidential remediation runs in a private, standalone repository with CI, clone-scoped secrets, and PRs, which advisory forks cannot provide.
- Agents reason from an offline snapshot of the selected alerts and advisories, with GitHub tools confined to the clone.
- The verify-before-push order means source and context are never pushed to a repository that is not private, is a fork, or has Actions enabled.
- The `private-clone/bootstrap` commit, built on an independent root snapshot, is a clear export baseline and publishes no upstream history.
- One command takes the operator from selected findings to a running investigation.

#### Negative
- The command does not block network access; public upstream source can still be fetched anonymously.
- Actions starts by default. Ambient organization secrets, installed Apps, shared runner credentials, and private dependency credentials remain reachable unless the operator reviews them before invoking the command or uses `--no-start`.
- Offline snapshots may not match the selected source revision; missing evidence requires a new operator-prepared snapshot.
- Sources that rely on submodules or LFS are not supported.
- A failed publication can leave an empty or partially configured private repository; the command does not delete it.
- Tests mock GitHub and Git calls; no live private repository was created during validation.

#### Neutral
- The command supplies no fix-generation workflow; the operator authors the bundle and starter.
- Exports must be applied upstream as patches because the bootstrap shares no history with upstream.
- Upstream publication and repository deletion stay separate, operator-authorized actions, documented in `docs/src/content/docs/reference/private-clone.md`.
