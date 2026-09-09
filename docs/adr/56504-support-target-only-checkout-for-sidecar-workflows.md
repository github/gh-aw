# ADR-56504: Support Target-Only Checkout for Sidecar Workflows

**Date**: 2026-08-28
**Status**: Draft
**Deciders**: pelikhan, copilot-swe-agent

---

### Context

Before this change, workflow checkout behavior was effectively all-or-nothing: `checkout: false` disabled all checkout activity, while any configured `checkout:` entries still caused the workflow repository itself to be checked out. Sidecar and MultiRepoOps workflows that only needed to operate on a target repository had no way to skip checking out their own hosting repository while preserving explicit target-repository checkouts. This PR adds parser, workflow-data, compiler-step, test, and documentation changes centered on that gap, and the PR description explicitly distinguishes the new behavior from `checkout: false`.

### Decision

We will treat `permissions.contents: none` as the signal to skip only the automatic workflow-repository checkout and the generated "Checkout PR branch" step, while still honoring explicitly configured `checkout:` entries for target repositories. We will carry this decision through frontmatter parsing into workflow data via `CheckoutSkipDefault`, gate checkout step generation on that flag, and document the pattern as target-only checkout for sidecar workflows. This preserves existing `checkout: false` semantics for full checkout disablement while introducing a narrower, more composable opt-out for the default checkout.

### Alternatives Considered

#### Alternative 1: Keep Using `checkout: false` for Sidecar Workflows

Continue to use the existing `checkout: false` setting whenever a workflow should avoid checking out its own repository.

This was considered because it already exists and requires no new parsing or workflow state. It was not chosen because the PR evidence shows `checkout: false` disables all checkout behavior, including additional configured repositories, which does not satisfy the sidecar use case where the target repository must still be checked out.

#### Alternative 2: Add a Dedicated Checkout-Specific Frontmatter Flag

Introduce a new frontmatter field specifically for suppressing only the default checkout, separate from permissions.

This was considered because it would make the behavior explicit within checkout configuration itself. It was not chosen because the implementation and docs in this PR instead align the behavior with the existing permissions model: when `contents` access is explicitly `none`, the workflow signals it does not need its own repository contents. Reusing that signal avoids a new top-level concept and keeps the behavior tied to the permission constraint that motivates it.

### Consequences

#### Positive
- Sidecar and MultiRepoOps workflows can check out only their target repositories without also checking out the repository that hosts the workflow.
- Existing explicit target-repository checkout entries remain intact, so the new behavior composes with current multi-repo patterns instead of replacing them.
- The distinction between full checkout disablement (`checkout: false`) and default-checkout suppression (`permissions.contents: none`) becomes documented and test-covered.

#### Negative
- Checkout behavior now depends on an additional cross-cutting signal (`permissions.contents: none`), which increases coupling between permission parsing and workflow generation.
- The compiler and frontmatter model gain extra state (`CheckoutSkipDefault`) and branching, which adds maintenance overhead and more cases to test.
- Users may initially confuse `permissions.contents: none` with `checkout: false`, requiring clearer docs and examples to prevent misconfiguration.

#### Neutral
- The change introduces a new helper (`ContentsIsNone`) and threads its result through both frontmatter parsing and workflow building as a backwards-compatible internal extension.
- Test coverage expands to assert that default checkout is omitted while target-repository checkout remains present for this configuration.
- Reference and pattern documentation now describe target-only checkout as the recommended sidecar workflow pattern.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
