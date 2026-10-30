# ADR-72412: Guided pull_request_target Remediation in `gh aw fix` with Conservative Role Restriction

**Date**: 2026-10-27
**Status**: Proposed
**Deciders**: @pelikhan (user and PR author)

@pelikhan approved the scope below during planning. This record documents that decision for [PR #72412](https://github.com/github/gh-aw/pull/72412), which remains under review; it does not claim approval from other maintainers.

---

### Context

Strict-mode compilation rejects `pull_request_target` workflows with unsafe checkouts or `on.roles` admitting `read` or `all`, because privileged execution can expose repository secrets to untrusted contributors. The [cross-repo compatibility audit in #72369](https://github.com/github/gh-aw/issues/72369) identified missing remediation guidance for these failures. The existing checkout auto-fix handles some workflows, but cannot resolve every unsafe configuration or choose a workflow's intended trust boundary. Fix tooling must provide a path forward without adding risk acknowledgments, checkout allowlists, or security-loosening settings on the user's behalf.

### Decision

Add two codemods to `gh aw fix`, combining automatic role restriction with manual guidance as explicitly chosen by @pelikhan. The automatic `pull-request-target-restrict-roles` codemod removes `read` and `all` from `on.roles`, preserving the other explicitly listed supported roles; only when removing unsafe roles leaves none does it use `[admin, maintainer, write]`.

The non-mutating `pull-request-target-guided-error` codemod runs after automatic migrations and reuses the compiler's authored security lint rules for checkout and role restrictions, rather than duplicating detection policy or resolving imports. Remaining errors explain manual choices, including disabling unnecessary checkout, selecting a trusted base checkout, reviewing an exact external repository/ref allowlist, or switching to `pull_request` after reviewing permission and secret requirements.

A guided error prevents all pending codemod edits to that workflow from being written, preserving atomic per-file remediation rather than saving a partially fixed workflow. Both new codemods skip explicit `strict: false` and workflows without `pull_request_target`; the existing checkout auto-fix and compiler enforcement remain unchanged. Neither new codemod adds `acknowledge-risk`, `allowed-checkouts`, or security-loosening settings automatically: acknowledgment suppresses only the trigger-risk warning, not checkout or role restrictions.

### Alternatives Considered

#### Alternative 1: Guided error only (no automatic changes)

Emit only manual guidance for the new remediation cases. This minimizes automatic edits and leaves admission choices entirely to workflow authors. @pelikhan instead chose automatic role restriction as well, because removing untrusted roles tightens admission while preserving explicitly listed supported roles and avoids an unnecessary manual migration.

#### Alternative 2: Fully automatic remediation, including risk acknowledgment or trigger switching

Automatically choose every remaining fix, for example by adding `on.pull_request_target.acknowledge-risk: true`, switching to `pull_request`, or rewriting explicitly configured checkouts. This could eliminate manual intervention, but acknowledgment cannot satisfy checkout or role restrictions and must not silently approve risk. Trigger changes alter permissions and secret availability, and choosing a trusted ref or an `allowed-checkouts` entry requires repository-specific judgment. These choices remain manual without changing the existing conservative checkout auto-fix.

#### Alternative 3: Implement separate detection logic inside the codemod

Detect unsafe checkouts and role admission independently in the guided codemod instead of reusing `pkg/workflow/lint` rules. Although this could tailor detection to the fix command, parallel policy implementations could drift. Reusing the authored lint rules keeps guidance aligned with compiler policy without claiming the fix command performs full compilation.

### Consequences

#### Positive
- Users get one-step remediation for unsafe roles, plus explicit guidance on the remaining choices, instead of a dead-end compile error.
- The new codemods never add risk acknowledgments, checkout allowlists, or security opt-outs, and compiler enforcement is unchanged.
- Reusing authored lint rules reduces policy drift between guidance and compilation.
- Blocking all writes when a manual issue remains keeps per-workflow edits atomic, so no partially fixed file is saved.

#### Negative
- Removing `read` or `all` narrows admission and can exclude previously admitted contributors; authors must review whether the remaining trusted roles or fallback fit their workflow.
- When a guided error fires, other unrelated codemod changes to the same workflow are also held back until the manual blocker is resolved.
- The guided check does not resolve imports or validate the complete effective workflow. Successful remediation is not a substitute for `gh aw compile --strict`.

#### Neutral
- Order matters: the role codemod runs early, and the guided check runs after all automatic migrations (including the existing `pull-request-target-checkout-false` codemod).
- Both codemods skip workflows that declare `strict: false` or have no `pull_request_target` trigger.
