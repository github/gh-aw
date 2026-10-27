# ADR-72412: Guided pull_request_target Remediation in `gh aw fix` with Conservative Role Restriction

**Date**: 2026-05-06
**Status**: Draft
**Deciders**: pelikhan, Copilot (PR #72412) [TODO: verify]

---

### Context

Strict-mode compilation rejects risky `pull_request_target` workflows: ones where checkout is enabled without a trusted ref, or where `on.roles` admits `read` or `all`. These are "pwn request" attack vectors that expose repository secrets to untrusted contributors. The daily cross-repo compatibility audit (#72369) found real repositories (`vaadin/flow`, `ohcnetwork/care_fe`) stuck on these errors. `gh aw fix --write` offered no remediation, so users had to choose a fix by hand and were tempted to use blanket `strict: false` opt-outs. Any fix tooling must not weaken security on its own. In particular, it must never add `acknowledge-risk: true` automatically, because doing so would silently approve a security trade-off on the user's behalf.

### Decision

We will add two codemods to `gh aw fix`. The first, `pull-request-target-restrict-roles`, is an automatic early codemod. It removes `read`/`all` from `on.roles`, keeps any other explicitly listed valid roles, and falls back to `[admin, maintainer, write]` when no roles remain. The second, `pull-request-target-guided-error`, is a late codemod that does not modify the file (it is marked `Guided: true`). It runs the same lint rules the compiler enforces (`PullRequestTargetCheckoutRule`, `PullRequestTargetRolesRule`) after all automatic migrations. If problems remain, it returns an actionable error that lists the manual options. This error blocks every write to that workflow, so `fix` never saves a partly remediated file. Automatic changes are limited to edits that only tighten security. Any change that requires judgment is left to a human, and compiler enforcement is unchanged.

### Alternatives Considered

#### Alternative 1: Guided error only (no automatic changes)

Emit only a manual-fix message and change nothing. This is the cheapest option and was listed first in #72369. It was not chosen because restricting roles is a security-tightening change with a clear safe default. Making users perform it by hand adds friction for no safety benefit.

#### Alternative 2: Fully automatic remediation, including risk acknowledgment or trigger switching

Have `fix` pick a fix on its own, for example by adding `on.pull_request_target.acknowledge-risk: true`, switching to `pull_request`, or rewriting checkouts. This was rejected because acknowledgment silently weakens security. A trigger switch changes permission and secret semantics, which the tool cannot evaluate. Checkout choices such as trusted refs or `allowed-checkouts` require a human to review the repository and ref.

#### Alternative 3: Implement separate detection logic inside the codemod

Write standalone YAML inspection in the codemod instead of reusing `pkg/workflow/lint` rules. This was rejected because two implementations could drift and give different answers. Reusing the compiler's lint rules keeps `fix` guidance consistent with what compilation rejects.

### Consequences

#### Positive
- Users get one-step remediation for unsafe roles, plus explicit guidance on the remaining choices, instead of a dead-end compile error.
- No automatic action weakens security: acknowledgment is never added, and compiler enforcement is unchanged.
- Detection reuses the compiler's lint rules, so `fix` and `compile` agree on what is unsafe.
- Blocking all writes when a manual issue remains keeps per-workflow edits atomic, so no partially fixed file is saved.

#### Negative
- The fallback roles `[admin, maintainer, write]` may be stricter than some workflows intend, and the authors of those workflows must widen the list again by hand.
- When a guided error fires, other unrelated codemod changes to the same workflow are also held back until the manual blocker is resolved.
- The checks look at authored configuration only. Issues brought in through imports still surface only at `gh aw compile --strict`.
- Codemods with invalid or non-string role entries return errors instead of fixing them, which adds another failure mode users must handle.

#### Neutral
- Order matters: the role codemod runs early, and the guided check runs after all automatic migrations (including the existing `pull-request-target-checkout-false` codemod).
- Both codemods skip workflows that declare `strict: false` or have no `pull_request_target` trigger.
- The CLI reference docs now state that `acknowledge-risk` only suppresses the trigger warning and does not bypass checkout or role restrictions.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
