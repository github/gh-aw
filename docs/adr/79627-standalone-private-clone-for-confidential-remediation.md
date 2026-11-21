# ADR-79627: Use a Standalone Private Repository for Confidential Security Remediation

**Date**: 2026-11-21
**Status**: Draft
**Deciders**: pelikhan (PR #79627 author) [TODO: verify other deciders]

---

### Context

Security fixes for code scanning alerts and repository advisories sometimes must be developed privately before disclosure. GitHub advisory forks do not support CI or secrets, and forks of public repositories must remain public, so neither can host confidential agentic remediation work. Remediation workflows need CI, clone-scoped secrets, and the selected alert and advisory context, but they must not query the upstream repository at runtime. Source, context, and workflows must reach the private repository only after its visibility, fork status, and Actions state have been verified.

### Decision

We will add `gh aw private-clone`, which creates a new standalone private repository (not a GitHub fork) and publishes an immutable source snapshot, an operator-supplied workflow bundle, and offline alert and advisory context into it. Publication happens in a fixed order: create the repository, disable Actions, verify that it is private, is not a fork, and has Actions disabled, and only then push `main` and the `private-clone/bootstrap` tag. The primary driver is confidentiality: the remediation workspace stays private while still supporting CI and clone-scoped secrets.

### Alternatives Considered

#### Alternative 1: GitHub repository security advisory temporary private fork

Advisory-linked temporary forks are the platform's built-in path for private security work. They were considered first because they are already tied to the advisory. They were rejected because advisory forks do not support CI or secrets, so the agentic investigation and fix workflows could not run there.

#### Alternative 2: Fork the upstream repository

A fork would keep the upstream history and let workflows run with normal CI. It was rejected because forks of public repositories must stay public, so confidentiality cannot be guaranteed for public upstreams. For private upstreams, a fork still carries the upstream fork relationship and inherited workflows and configuration, so it is not a clean, disposable boundary.

#### Alternative 3: Enclave-backed isolated execution

Enclaves isolate bounded computations and disclosure, so they were a serious candidate. They were not chosen for this change because they do not provide durable GitHub repositories or PR lifecycles, and their current agent GitHub tool surface does not support security-alert reads or publication. This is a deferral rather than a permanent rejection: an enclave-backed option would need an offline source and context staging contract and a validated patch-export path.

### Consequences

#### Positive
- Confidential remediation runs in a private, standalone repository that supports CI and clone-scoped secrets, which advisory forks cannot provide.
- Agents reason from an offline snapshot of the selected alerts and advisories, which reduces accidental disclosure through runtime queries to the upstream repository.
- The verify-before-push order means source and confidential context are not pushed to a repository that is public, a fork, or has Actions enabled.
- The `private-clone/bootstrap` tag gives a reviewable export baseline: only changes made after the tag should be exported.

#### Negative
- The command blocks credential and remote transfer, but it does not block network access. Public upstream source can still be fetched anonymously.
- Actions is enabled immediately after publication and runs trusted, operator-supplied workflows. Ambient organization secrets, installed Apps, shared runner credentials, and private dependency credentials remain reachable unless the operator reviews them first. Creating the repository does not revoke ambient grants.
- Offline snapshots may not match the selected source revision. Missing evidence requires an operator to prepare a new snapshot.
- A failed publication can leave an empty or partially configured private repository, and the command does not clean it up.
- The publication path involves several GitHub API and git operations. Its tests mock external GitHub and Git calls, and no live private repository was created during validation.

#### Neutral
- Root workflows receive `copilot-requests: write`, and GitHub tools are restricted to the clone. The default workflow token stays read-only, while clone-local PR creation is enabled subject to organization policy.
- The command supplies no fix-generation workflow or prompt. The operator must provide the workflow bundle, and the starter workflow must declare `workflow_dispatch`.
- Upstream publication and repository deletion stay separate, operator-authorized actions. The `docs/src/content/docs/reference/private-clone.md` reference and cross-repository docs describe the workflow.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
