# ADR-64976: Require Explicit GitHub App Token Scopes

**Date**: 2026-10-02
**Status**: Draft
**Deciders**: gh-aw maintainers (review pending)

---

### Context

Several gh-aw workflows mint GitHub App installation tokens via `actions/create-github-app-token`. Without explicit `permission-*` inputs, those tokens inherit the GitHub App installation's full scope even when a job only needs read-only repository access. This pull request adds compile-time validation, updates the shared Squad bootstrap workflow, regenerates affected lock files, and documents the requirement so the compiler can enforce least-privilege token creation across authored, imported, and generated workflow steps.

### Decision

We will require every compiled `actions/create-github-app-token` step to declare at least one explicit `permission-*` input, and we will fail compilation in strict mode when those inputs are missing. In non-strict mode, the compiler will emit a warning instead of silently allowing a fully scoped installation token. We will also scope the shared Squad bootstrap token to `permission-contents: read` and document that job-level `permissions:` does not constrain GitHub App installation tokens.

### Alternatives Considered

#### Alternative 1: Continue Relying on Job-Level `permissions:` and Reviewer Discipline

This keeps the compiler unchanged and relies on authors and reviewers to remember that `actions/create-github-app-token` is independent of the workflow job's `permissions:` block. It was considered because it avoids new validation logic and keeps authored workflows more flexible. It was not chosen because the PR evidence shows that unscoped app-token steps already existed in shared workflow code, making manual review insufficient for preventing over-privileged tokens.

#### Alternative 2: Auto-Inject Default Permissions During Compilation

The compiler could silently add a default scope such as `permission-contents: read` whenever an app-token step omits explicit scopes. It was considered because it would remediate many cases without blocking authors. It was not chosen because the correct scope depends on each workflow's actual needs, and automatic defaults would hide the architectural decision rather than forcing workflow authors to declare the minimum required permissions explicitly.

### Consequences

#### Positive
- GitHub App installation tokens minted by compiled workflows must declare explicit scope, reducing the chance of unintentionally granting full installation permissions.
- Strict-mode compilation catches unsafe workflow definitions before lock files are produced or merged.
- Shared Squad workflows and documentation now model the least-privilege pattern for future workflow authors.

#### Negative
- Existing workflows that relied on implicit full-scope tokens will now fail or warn until authors update them with explicit `permission-*` inputs.
- The compiler now performs an additional validation pass over compiled YAML when app-token steps are present, adding some maintenance and test surface area.
- Authors must understand and choose the correct GitHub App permission set, which may require extra design and review effort.

#### Neutral
- Regenerated lock files reflect the scoped token inputs and related dependency churn, but they do not change the decision itself beyond compiled output alignment.
- Non-strict workflows remain buildable with warnings, so adoption can be incremental while repositories move toward strict enforcement.

---

*Draft decision record for [pull request #64976](https://github.com/github/gh-aw/pull/64976). Review before changing status to Accepted.*
