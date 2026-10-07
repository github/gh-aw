# ADR-66649: Explicit Maintainer-Declared Trusted Checkout Policy for `pull_request_target`

**Date**: 2026-10-07
**Status**: Draft
**Deciders**: pelikhan [TODO: verify full decider list]

---

### Context

In strict mode, `pull_request_target` workflows are validated by `pkg/workflow/pull_request_target_validation.go` with two unconditional behaviors: a hard error for any checkout that is not omitted, `false`, or pinned to the base repository/base ref, and a "very dangerous trigger" warning emitted even when `checkout: false` is set. This blocks a legitimate class of workflows (#66564): a workflow that reacts to *merged* PRs (`types: [closed]` + `if: github.event.pull_request.merged == true`), checks out a fixed external documentation repository at a fixed ref, and reads the source PR's changes as data rather than executing them. The only escape hatches were `strict: false` or reorganizing to `pull_request`, both of which are unacceptable: the first disables unrelated security diagnostics for the entire workflow, and the second does not receive repository secrets for fork PRs. The constraint is a deliberate policy, not a bug, so relaxing it needs a scoped, auditable opt-in rather than a loosened default.

### Decision

We will add two opt-in keys under `on.pull_request_target` in the main workflow's frontmatter: `allowed-checkouts`, a list of exact, case-sensitive literal `repository`/`ref` pairs that are additionally trusted for checkout validation, and `acknowledge-risk`, a boolean that suppresses *only* the strict-mode dangerous-trigger warning. Checkout validation still runs when risk is acknowledged, and every configured checkout must independently satisfy either the existing base-repository rules or an allowlist entry. The validator fails closed: GitHub expressions, wildcards, omitted refs, `refs/pull/...` refs, and wiki checkouts are rejected even if their literal text appears in the policy (`pullRequestTargetLiteralRepositoryPattern` / `pullRequestTargetLiteralRefPattern`). The primary driver is to keep strict mode as the default security posture while allowing maintainers to record an explicit, reviewable trust statement at the exact repository/ref granularity. The change also preserves configured checkout refs as strings in generated YAML so literals such as `0x10` are not reinterpreted as numbers.

### Alternatives Considered

#### Alternative 1: Require `strict: false` for this scenario

The existing mechanism already permits the configuration by lowering the effective strict mode. It was rejected because `strict: false` is workflow-wide: it disables every other strict diagnostic, destroys the zero-warning validation gate the requesting team relies on, and leaves no machine-readable record of *which* external checkout was trusted. Reviewers would see a blanket opt-out instead of a specific, greppable allowlist entry.

#### Alternative 2: Automatically allow any literal (non-expression) external repository/ref checkout

Trust could have been inferred: if neither repository nor ref derives from the PR payload, the checkout cannot pull untrusted head code, so the validator could accept it without new frontmatter. This was a close call and is the smallest change, but it silently weakens the default for every existing workflow and removes the explicit maintainer acknowledgment. A moved branch or tag in a trusted-by-inference repository would then execute with elevated permissions and secret access with no prior review signal, so an explicit declaration was preferred.

#### Alternative 3: A single global escape key (e.g. `pull-request-target: unsafe: true`)

One boolean covering both the warning and the checkout error would have been simpler to implement and document. It was rejected because it conflates two independent risks — acknowledging elevated permissions versus trusting a specific external code source — and gives no per-repository granularity, which is precisely the audit information a security reviewer needs.

### Consequences

#### Positive
- Workflows that legitimately need a fixed external checkout under `pull_request_target` can compile with strict mode fully enabled, preserving all other diagnostics and zero-warning gates.
- The trust decision is explicit, declarative, and diffable: reviewers can see the exact `repository`/`ref` pairs a maintainer vouched for, and the JSON schema constrains them to literals.
- Validation fails closed independently of schema checks, so expressions, wildcards, PR refs, and wiki checkouts cannot slip through the allowlist path.
- Exact ref preservation prevents a class of silent misconfiguration where YAML scalar coercion changes the checked-out ref.

#### Negative
- The security surface of `pull_request_target` now has a documented bypass; an over-broad or careless allowlist entry (especially a mutable branch or tag) grants privileged execution to code the repository does not control.
- `acknowledge-risk: true` removes the most visible warning about the most dangerous trigger, which may reduce the chance that a reviewer notices a later, riskier change to the same workflow.
- Two more frontmatter keys, schema entries, and validation branches increase the compiler surface that must stay in sync with the reference documentation and regression tests.

#### Neutral
- Defaults are unchanged: with the policy omitted, existing workflows compile and warn exactly as before, so no migration is required.
- The policy is scoped to the main workflow's frontmatter and is not inherited from imports, matching how other trust-bearing settings are handled.
- The policy covers compile-time checkout authorization only; checkout authentication (`github-app` / `github-token`) and safe-output publication remain configured and validated separately.
- Enforcement lives in `pull_request_target_validation.go` with behavior pinned by `pull_request_target_validation_test.go`; future changes to trusted-checkout semantics should be made there and reflected in `docs/src/content/docs/reference/checkout.md`.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
