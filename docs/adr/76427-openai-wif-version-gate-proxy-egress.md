# ADR-76427: Gate OpenAI WIF on AWF Version and Keep Token Egress in the Proxy

**Date**: 2026-11-10
**Status**: Draft
**Deciders**: Unknown [TODO: verify — maintainers who accept this decision; PR #76427 was requested by pelikhan and implemented by the Copilot coding agent]

---

### Context

`engine.auth: {type: github-oidc, provider: openai}` lets Codex authenticate to OpenAI with a GitHub Actions OIDC token instead of `OPENAI_API_KEY`. The AWF API proxy performs the token exchange with `auth.openai.com`. The feature landed in #75927 and was hardened in #76236, but three gaps remained. The compiler accepted any AWF version, yet the released AWF v0.29.10 predates proxy support for OpenAI WIF, so released workflows could not authenticate. The threat-detection job could inherit OIDC settings from the main engine without receiving `id-token: write`, so a same-engine detector override could hold OIDC auth it could not use. Finally, `auth.openai.com` was added to the agent and detection allowlists, although the proxy reaches the token endpoint through its own egress path. Token exchange must stay inside AWF, as established for Azure in ADR-68457.

### Decision

We will require AWF `v0.29.11` or newer for OpenAI WIF. The compiler rejects the configuration when the effective firewall version is lower, or when a `sandbox.agent.images.api-proxy` override lacks a semantic-version tag of `v0.29.11` or newer. We will stop adding `auth.openai.com` to agent and detection allowlists, and grant `id-token: write` to a detection job whenever its engine environment carries `AWF_AUTH_TYPE=github-oidc`. The primary driver is least-privilege egress and permissions, with failures moved from runtime to compile time.

### Alternatives Considered

#### Alternative 1: Keep `auth.openai.com` in the agent and detection allowlists

This is the status quo from #76236, and it needs no docs or allowlist changes. We rejected it because the proxy performs the exchange through its own egress path (as noted in the AWF review of gh-aw-firewall#11364), so the agent entry adds no function. It only widens agent reach to the host and its subdomains. This was a close call: the cost is a larger egress surface, not a functional break.

#### Alternative 2: Enforce the AWF minimum only at runtime

The Codex startup check already refuses to launch unless fresh AWF reflection reports the configured OpenAI WIF endpoint, so no new compile-time validation would be strictly required. We rejected relying on that alone because an older AWF would fail only after the workflow starts, with a less actionable message. We kept the runtime check as defense in depth.

#### Alternative 3: Strip inherited `AWF_AUTH_*` settings from detector overrides

Removing OIDC settings from `mergeThreatDetectionEngineEnv` would also eliminate the permission mismatch at its source. We did not choose it because the PR keeps same-engine detectors on the same OIDC provider and adds the missing permission. [TODO: verify — no discussion of this option appears in the PR evidence; confirm the rationale with maintainers.]

### Consequences

#### Positive
- Unsupported WIF configurations fail at compile time with the required version in the message, before any run starts.
- Agent and detection allowlists no longer include `auth.openai.com`, which narrows agent egress.
- A same-engine detector that inherits OIDC auth can now request its token, so detection does not run with unusable auth.
- Tests cover an older AWF release, an older and an unversioned API-proxy image, a supported proxy image, and the same-engine detector permission.

#### Negative
- Workflows that configure WIF with an AWF version below `v0.29.11`, or with an API-proxy image that has no semantic-version tag, now fail to compile. Those configurations never authenticated successfully, but they will need a version bump or a re-tagged image.
- The semantic-version check on proxy image tags is a proxy for capability. A correctly built image with a non-semver tag is rejected even if it supports WIF.
- The minimum version is a maintained constant. Each AWF change to WIF support requires a matching update.
- The PR body says `Fixes #76290`, but that issue's "done when" includes an end-to-end token-exchange trial that this PR explicitly excludes. Regressions in the proxy egress path would surface only in a live run.

#### Neutral
- Evals are unchanged. WIF does not apply to the evals job, which calls the Decisions API directly, so `CODEX_API_KEY` or `OPENAI_API_KEY` is still required (documented in `experiments.md`).
- When `engine.auth` is absent, validation reconstructs the auth config from `AWF_AUTH_*` environment values, so the version gate also covers that path.
- This conflicts with ADR-70619 (Proposed), which requires the `codex-vendor` opt-in for Codex authentication at `auth.openai.com`. The PR documents that WIF does not need that opt-in. [TODO: verify — maintainers should either amend ADR-70619 to exempt proxy-owned token exchange, or require `codex-vendor` for WIF.]
- The change extends the proxy-owned token exchange pattern from ADR-68457 (Azure) to OpenAI.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
