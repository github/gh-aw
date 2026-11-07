# ADR-74951: Warn (Not Error) on Dynamic Checkout Refs Combined with Host Setup Steps

**Date**: 2026-11-04
**Status**: Draft
**Deciders**: pelikhan and gh-aw maintainers (review pending)

---

### Context

In the generated agent job, repository checkout runs before imported or custom `steps:`, and those steps execute on the runner host, outside the agent sandbox. When `checkout.ref` is an expression derived from `needs.*`, `github.event.*`, or `inputs.*` (for example a planner-selected PR branch), a setup command such as `./scripts/setup.sh` may execute untrusted branch code with the job's credentials (issue #73080). The compiler previously gave no signal about this ordering hazard, and the safe pattern — check out the default branch, run setup, then switch branches in workflow-owned steps — was undocumented. Many legitimate workflows use dynamic refs, so the compiler cannot reliably distinguish trusted from untrusted values at compile time.

### Decision

We will add a compile-time **warning** (`warnCheckoutRefWithSteps`, invoked from `compiler_validators.go`) that fires when any effective checkout — using merged checkout settings and skipping suppressed default checkouts — has a `ref` whose interpolations reference `needs.*`, `github.event.*`, or `inputs.*`, and the workflow has non-empty imported or custom `steps:`. A ref that is exactly the standalone `github.event.repository.default_branch` expression is exempt. We will pair the warning with documentation of the agent-job ordering and the recommended "checkout default branch, then `git checkout -B` in steps" pattern, and state explicitly that the absence of a warning does not prove a ref is trusted.

### Alternatives Considered

#### Alternative 1: Fail compilation (hard error)

Rejecting such workflows would be fail-closed, but dynamic refs with setup steps are common and often safe (e.g., steps that do not execute repository code). A hard error would break existing workflows and force escape hatches without a reliable trust signal at compile time, so a warning was preferred.

#### Alternative 2: Reorder the job so steps run before checking out the dynamic ref

The compiler could automatically check out the default branch, run setup steps, and then switch to the requested ref. This changes established execution semantics for every workflow using `checkout.ref` and `steps:`, and steps that legitimately need the target branch contents would break. Documenting the pattern lets authors opt in explicitly instead.

#### Alternative 3: Documentation only

Documenting the hazard without compiler support is low-cost but relies on authors finding the docs; it gives no signal at authoring time for the specific risky combination.

### Consequences

#### Positive
- Authors get an actionable compile-time signal, with a concrete remediation, for a sandbox-bypass risk.
- No existing workflows break; the change is additive and covered by table-driven regression tests (expression syntaxes, bracket notation, case-insensitivity, merge precedence, warning-free cases).

#### Negative
- Warning-only enforcement can be ignored; risky workflows still compile and run.
- Detection is heuristic (context-prefix matching): it can miss untrusted values from other contexts and can warn on refs that are actually trusted (e.g., `inputs.*` set by maintainers).

#### Neutral
- The standalone default-branch exemption is exact-match; prefixed or computed variants (e.g., `format(...)`) still warn by design.
- The warning increments the compiler warning count, which affects any tooling that treats warnings as failures (e.g., strict CI).

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
