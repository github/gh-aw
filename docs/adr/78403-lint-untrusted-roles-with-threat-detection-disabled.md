# ADR-78403: Lint `on.roles: all` Workflows That Disable Threat Detection (CTR-005)

**Date**: 2026-11-16
**Status**: Draft
**Deciders**: pelikhan [TODO: verify]

---

### Context

Depi Security's Codex issue-labeler attack began with an untrusted user opening an issue and escalated through prompt injection, shell access in the agent sandbox, a host `/tmp` escape and token theft. The gh-aw equivalent of an open-to-everyone trigger is `on.roles: all`. When it is combined with `safe-outputs.threat-detection: false`, nothing inspects agent output between untrusted input and published outputs. The default AWF container sandbox also shares the runner's user and host `/tmp`, so it does not contain this class of escape on its own. gh-aw already has a built-in AW-AST lint registry that maps rules to compiler threat IDs and runs during `gh aw compile`.

### Decision

We will add a built-in AW-AST lint rule, `untrusted-roles-detection` (threat rule CTR-005). It fires when `on.roles` includes `all`, threat detection is disabled (checked on the compiled job graph first, then the authored frontmatter), and at least one trigger can be fired by an untrusted actor. It is an error in strict mode (the default) and a warning otherwise. Trusted-only triggers (`push`, `schedule`, `workflow_dispatch`, `merge_group`) are exempt, and unknown or command-style triggers count as untrusted so the rule fails closed. The diagnostic recommends removing `threat-detection: false` or narrowing `roles`, and recommends the NVX microVM sandbox unless it is already configured. The existing time-limited `threat-detection-suppress` mechanism is the opt-out. We chose a compile-time guard because it fails closed at authoring time without changing runtime behaviour.

### Alternatives Considered

#### Alternative 1: Document the risk only

Add guidance to the triggers and sandbox docs without enforcement. This is the lowest-cost option and breaks no workflows. It was not chosen because the unsafe combination would still compile silently, and the attack chain needs only one workflow that does so.

#### Alternative 2: Hard-reject the combination in the compiler schema or validator, with no suppression

Reject the combination unconditionally, outside the lint registry. This gives the strongest guarantee. It was not chosen because it offers no time-limited acknowledgement path, no warning mode for `strict: false`, and does not reuse the existing rule-to-threat-ID and suppression infrastructure.

#### Alternative 3: Require the NVX sandbox instead of enabling threat detection

Treat NVX microVM isolation as an exemption or as the primary mitigation. It was not chosen because NVX addresses sandbox escape but not unreviewed agent output, so NVX is recommended in the diagnostic but does not exempt the workflow.

### Consequences

#### Positive
- Strict-mode compilation blocks the highest-risk combination of untrusted triggers and no detection layer.
- The rule reuses the lint registry, the CTR threat-ID mapping and the suppression lifecycle, so opt-out is explicit and time-limited.
- Detection status is read from the compiled job graph first, so disabling detection through an import is still caught.
- Unknown triggers fail closed, so new event types are not silently exempted.

#### Negative
- This is a behaviour change: existing strict workflows with this combination now fail to compile (`ai-moderator.md` had to drop `threat-detection: false`).
- The lint package cannot import `pkg/workflow` (import cycle), so the list of `on:` setting keys is duplicated from `gatewayDispatchOnSettings` and must be kept in sync by hand.
- `ai-moderator.md` is sourced from `githubnext/agentics`, so the upstream copy needs the same change or `gh aw update` will reintroduce the failure.

#### Neutral
- The rule only warns, rather than blocks, when `strict: false`.
- The docs (linters, triggers, sandbox pages), the compiler threat-detection spec and the lint README are updated, and a minor changeset is added.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
