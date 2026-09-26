# ADR-63499: Pin pull_request activation checkout to base SHA

**Date**: 2026-09-25
**Status**: Accepted
**Deciders**: gh-aw maintainers

---

### Context

For pull-request-related workflows, the activation job checks out repository content that is then used to build runtime imports and install skills before later review steps run. The PR description states that this activation checkout could previously read PR-head content before base-branch restoration, which let same-repo PR authors influence the instructions used to review their own PR. The diff shows compiler-driven workflow output changes that pin activation sparse checkouts to `github.event.pull_request.base.sha` on `pull_request`, `pull_request_review`, and `pull_request_review_comment` events while preserving existing fallback refs for mixed-trigger workflows and `workflow_call`. The decision is how gh-aw should select the activation checkout ref when a workflow can execute against untrusted pull request content.

### Decision

We will pin activation-job sparse checkouts to the pull request base SHA when the active event is `pull_request`, `pull_request_review`, or `pull_request_review_comment`. For other trigger types, including mixed-trigger workflows outside an active pull request and `workflow_call`, the compiler will preserve the existing fallback refs instead of forcing the base SHA behavior universally. Reusable workflows will also retain the same-repository checkout guard whenever activation authentication can fall back to the repository-scoped `GITHUB_TOKEN`. We chose this because activation-time instructions and imported content are security-sensitive, and the PR evidence shows that using PR-head content at that stage can let contributors influence the system that evaluates their own changes.

### Alternatives Considered

#### Alternative 1: Keep using the existing checkout ref behavior for pull_request events

This was a realistic option because it avoids changing activation checkout semantics and preserves current behavior across all workflows. It was not chosen because the PR description identifies a concrete trust-boundary problem: PR-head content could shape runtime imports and installed skills before base-branch restoration, which weakens review integrity for same-repo pull requests.

#### Alternative 2: Always pin activation checkout to the base SHA for every trigger type

This was considered because it would maximize consistency and reduce branching logic in generated workflows. It was not chosen because the PR evidence explicitly preserves existing fallback refs for mixed-trigger workflows and `workflow_call`, indicating those paths still need their current target ref behavior and should not be constrained by pull-request-specific hardening.

### Consequences

#### Positive
- Activation-time imports and skills for pull-request-related workflows are derived from trusted base-branch content rather than mutable PR-head content.
- Same-repo pull request authors lose a path to influence the instructions used to review their own PR during activation.
- Mixed-trigger workflows and `workflow_call` retain their current ref-selection behavior, limiting compatibility risk outside the vulnerable case.
- Cross-repository reusable workflows continue to skip activation checkout when optional GitHub App credentials are unavailable and authentication falls back to `GITHUB_TOKEN`.

#### Negative
- Activation behavior now depends on event-specific ref selection, which increases compiler and generated-workflow complexity.
- Pull request workflows can no longer rely on activation-time changes from the PR head, which may surprise authors expecting activation setup to reflect in-flight branch edits.
- The security fix requires broad lock-file regeneration, increasing maintenance churn in generated workflow outputs.

#### Neutral
- Regression coverage needs to explicitly distinguish `pull_request`, mixed-trigger, `workflow_call + pull_request`, and `pull_request_target` scenarios.
- Generated workflows now encode checkout ref policy directly in expressions instead of inheriting it implicitly from the default checkout ref.

---

*ADR created by [adr-writer agent]. Status: Accepted.*
