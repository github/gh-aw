# ADR-77371: Require Scoped Base Branches for merge-pull-request

**Date**: 2026-11-13
**Status**: Proposed; implementation under review; maintainer sign-off not recorded
**Decision source**: User direction for [PR #77371](https://github.com/github/gh-aw/pull/77371)

---

### Context

The experimental `merge-pull-request` safe output lets an AI agent merge pull requests under configured gates. Before this change, the destination (base) branch was constrained by refusing the default and protected branches and requiring an open upstream pull request for the base, without a destination allowlist. The `allowed-branches` filter constrains source branches, not destinations, and the agent must not be able to widen either through tool-call arguments.

The user requires an explicit declaration of owner-preauthorized, non-consequential destinations rather than unrestricted access to otherwise eligible branches. This declaration records the owner's intent; it does not prove that a merge or its downstream effects are harmless. Destination scope must hold at evaluation, at final revalidation, and in staged and cross-repository runs. This record documents that direction, not independent human approval or completed maintainer sign-off.

### Decision

Require `safe-outputs.merge-pull-request.allowed-base-branches` as a non-empty list of static, scoped destination patterns. Reject wildcard/separator-only entries such as `"*"`, `"**"`, and `"**/**"`, expressions, and unsupported glob operators consistently across schema, compiler, and runtime validation. The live base branch must match at least one pattern during evaluation and final revalidation, including staged and cross-repository operations. `allowed-branches` keeps filtering source branches only and never grants destination scope, and tool-call arguments cannot change the policy. The primary driver is containment: the workflow owner, not the agent, defines where an autonomous merge may land.

An allowlist match never overrides default/protected-branch refusal, the open-upstream-PR requirement, caution vetoes, protected-file policy, or any other existing merge gate. Per-merge human approval remains optional unless configured, including through `required-approvers`. Owners remain responsible for independent policy ownership and assessment of downstream consequences.

Use the name `allowed-base-branches` for consistency with `create-pull-request` and to distinguish destination scope from the existing source filter.

### Alternatives Considered

These are design tradeoffs against the confirmed requirements, not claims that each option was historically evaluated or approved.

#### Alternative 1: Infer destination scope from `allowed-branches`

Reusing the source-branch filter would need no new configuration, and existing workflows that already restrict source branches would gain a destination restriction automatically. It was rejected because the two dimensions are independent: a branch that is safe to merge *from* is not necessarily safe to merge *into*. The cost is a required configuration change for existing workflows. The PR documents this exclusion explicitly.

#### Alternative 2: Rely on default/protected-branch refusal and repository rulesets

Keeping the destination optional would avoid a breaking change and rely on protected-branch refusal, the open-upstream-PR requirement, and repository rulesets. These safeguards remain useful but do not replace the required workflow-specific declaration of destination intent. This alternative does not satisfy the user's explicit-allowlist requirement.

#### Alternative 3: Permit wildcard or optional destinations with a compile-time warning

A wildcard default such as `"*"`, paired with a warning, would preserve existing workflows while surfacing the exposure. A warning does not restrict destination scope and therefore does not satisfy the user's requirement that the field be mandatory and not unrestricted.

#### Alternative 4: Use a consequence-oriented or shorter field name

`non-consequential-targets`, `allowed-parents`, and `allowed-bases` were considered. `allowed-base-branches` was chosen for consistency with `create-pull-request` and explicit source/base terminology. A consequence-oriented name could misleadingly imply that the tool certifies downstream effects.

### Consequences

#### Positive

- Destination scope is an explicit, owner-authored declaration that the agent cannot widen through tool arguments.
- Source and destination filters are checked independently, so an allowed source cannot land on an undeclared base. This holds for staged previews, cross-repository targets, and final revalidation.
- Invalid configuration fails before any GitHub read or merge call, and the schema, compiler, and runtime reject the same inputs.

#### Negative

- This is a breaking change (major changeset). Workflows that use `merge-pull-request: null` or omit the list fail to compile until they add a scoped `allowed-base-branches` and recompile.
- The declaration is only as strong as its protection from agent edits. Owners must maintain independent control of policy and assess downstream automation; a matching pattern does not prove that downstream effects are harmless or substitute for human approval.
- The mutable-policy race remains. GitHub atomically enforces only the evaluated head SHA, so a policy edited between evaluation and merge is not covered by that precondition.

#### Neutral

- The feature remains experimental. `allowed-base-branches` is a required field within it, not a new safe-output type.
- Schema, type declarations, editor autocomplete data, frontmatter and safe-outputs reference documentation, the specification, and test fixtures change together.
- A new failure code, `base_branch_not_allowed`, joins the merge failure set, so destination mismatches appear as merge-gate failures in decision evidence and outcome manifests.

---

This ADR does not certify test results, reviewer approval, or live merge validation. The outstanding detection caution requires independent human review and is not cleared by this decision. No merge or workflow dispatch is authorized by this record.
