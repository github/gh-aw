# ADR-77371: Require Scoped Base Branches for merge-pull-request

**Date**: 2026-11-13
**Status**: Draft
**Deciders**: Unknown; PR author pelikhan. Pending maintainer review of [PR #77371](https://github.com/github/gh-aw/pull/77371).

---

### Context

The experimental `merge-pull-request` safe output lets an AI agent merge pull requests under configured gates. Before this change, the destination (base) branch was constrained only by refusing the default and protected branches and requiring an open upstream pull request for the base. The `allowed-branches` filter constrains source branches, not destinations, and the agent must not be able to widen either through tool-call arguments. Repository owners therefore need to declare which non-consequential integration branches an autonomous merge may target, and that declaration must hold at evaluation, at final revalidation, and in staged and cross-repository runs.

### Decision

We will require `safe-outputs.merge-pull-request.allowed-base-branches` as a non-empty list of owner-declared destination patterns, and reject wildcard-only entries such as `"*"` and `"**"`. The live base branch must match at least one pattern during evaluation and final revalidation, including staged and cross-repository operations. `allowed-branches` keeps filtering source branches only and never grants destination scope, and tool-call arguments cannot change the policy. The primary driver is containment: the workflow owner, not the agent, defines where an autonomous merge may land.

### Alternatives Considered

#### Alternative 1: Infer destination scope from `allowed-branches`

Reusing the source-branch filter would need no new configuration, and existing workflows that already restrict source branches would gain a destination restriction automatically. It was rejected because the two dimensions are independent: a branch that is safe to merge *from* is not necessarily safe to merge *into*. The cost is a required configuration change for existing workflows. The PR documents this exclusion explicitly.

#### Alternative 2: Rely on default/protected-branch refusal and repository rulesets

Keeping the destination optional would avoid a breaking change and rely on protected-branch refusal, the open-upstream-PR requirement, and repository rulesets. It was rejected because these checks do not encode the workflow owner's intent: any unprotected branch with an open pull request remains a valid destination. Rulesets also live outside the workflow configuration. [TODO: verify whether repository rulesets were evaluated; the PR body does not discuss them.]

#### Alternative 3: Permit wildcard or optional destinations with a compile-time warning

A wildcard default such as `"*"`, paired with a warning, would preserve existing workflows while surfacing the exposure. It was rejected because a warning does not change runtime behavior: an unreviewed workflow would still merge into any eligible branch. The implementation rejects these entries at schema, compiler, and runtime levels. [TODO: verify the author's rationale; it is not stated in the PR body.]

### Consequences

#### Positive

- Destination scope is an explicit, owner-authored declaration that the agent cannot widen through tool arguments.
- Source and destination filters are checked independently, so an allowed source cannot land on an undeclared base. This holds for staged previews, cross-repository targets, and final revalidation.
- Invalid configuration fails before any GitHub read or merge call, and the schema, compiler, and runtime reject the same inputs.

#### Negative

- This is a breaking change (major changeset). Workflows that use `merge-pull-request: null` or omit the list fail to compile until they add a scoped `allowed-base-branches` and recompile.
- The declaration is only as strong as its protection from agent edits. Owners must keep it outside the agent's writable scope, and a matching pattern does not prove that downstream effects are harmless.
- The mutable-policy race remains. GitHub atomically enforces only the evaluated head SHA, so a policy edited between evaluation and merge is not covered by that precondition.

#### Neutral

- The feature remains experimental. `allowed-base-branches` is a required field within it, not a new safe-output type.
- Schema, type declarations, editor autocomplete data, frontmatter and safe-outputs reference documentation, the specification, and test fixtures change together.
- A new failure code, `base_branch_not_allowed`, joins the merge failure set, so destination mismatches appear as merge-gate failures in decision evidence and outcome manifests.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
