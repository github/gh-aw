# ADR-76806: Data-Only Checkout Acknowledgement for Checkout-Ref Warnings

**Date**: 2026-11-11
**Status**: Draft
**Deciders**: pelikhan (PR actor) and gh-aw maintainers; review pending [TODO: verify]

---

### Context

[ADR-74951](74951-warn-on-dynamic-checkout-ref-with-host-steps.md) added a compile-time warning when a `checkout.ref` is a dynamic expression (`needs.*`, `github.event.*`, or `inputs.*`) and host steps run after that checkout. Host steps run outside the agent sandbox and may execute code from the selected ref. Issue #76586 describes a workflow where trusted tooling is checked out from the default branch and a dynamically selected content repository is read only as data; the warning fires even though no code from that checkout runs. Authors need a supported way to record this separation without removing validation or suppressing unrelated diagnostics. The compiler cannot verify data-only use, because a `working-directory` value does not show which checkout a command reads or executes.

### Decision

We will add a literal boolean `data-only` option (default `false`) to each `checkout:` entry. When it is `true`, it suppresses only that checkout's checkout-ref warning. When several requests merge into the same checkout target, the target stays acknowledged only if every request sets `data-only: true`. The option is an author acknowledgement, not enforced isolation: it does not change checkout steps, credentials, permissions, or other diagnostics. The primary driver is keeping the warning active for unacknowledged checkouts while giving authors a narrowly scoped way to record intent.

### Alternatives Considered

#### Alternative 1: Check out the dynamic ref after host steps

ADR-74951 documents a "check out the default branch, run setup, then switch refs" pattern, and the reporter says they would prefer to follow the recommended pattern. This was not sufficient on its own: the example runs tooling against the target checkout in the same job (`./scripts/validate-content ../target`), so the target must exist before the host step runs. Restructuring into separate jobs or artifacts is possible, but it is a larger workflow change than a diagnostic warning justifies. This was a close call.

#### Alternative 2: Disable the checkout-ref warning globally

A single switch that disables the warning would be simple to implement. It would also hide the warning for every checkout, including checkouts that really do run untrusted code on the host. The reporter explicitly asked not to suppress unrelated diagnostics, so this was rejected.

#### Alternative 3: Infer data-only use from `working-directory` or step usage

The compiler could treat a checkout as data-only when no step runs inside it. This is unreliable: the documented example runs a script in `tooling` that reads `../target`, and commands can execute code outside their working directory. Inference would silence warnings for checkouts that may run code, so it was rejected.

### Consequences

#### Positive
- Authors can record tooling/data separation and silence only the warning that does not apply, while other checkouts keep their warnings (covered by merged-checkout tests).
- Generated checkout steps are unchanged, so the option carries no runtime or credential risk (`TestCheckoutDataOnlyPreservesCheckoutSteps`).
- The merge rule fails safe: an unacknowledged duplicate request keeps the warning on.

#### Negative
- The acknowledgement is unverified. An incorrect `data-only: true` silences a warning that may indicate real risk, and the compiler does not check it.
- The option adds a frontmatter key, a schema entry, and a documentation section that must stay consistent with the warning text.
- Whether a warning fires now depends on every merged request for a target, which is harder to predict for imported workflows.

#### Neutral
- Existing workflows compile to the same checkout YAML; the only user-visible change is the warning text, which now mentions trusted tooling refs and `data-only`.
- This refines the scope of ADR-74951's warning rather than replacing its decision. The warning remains the default.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
