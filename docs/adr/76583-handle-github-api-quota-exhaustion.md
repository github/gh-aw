# ADR-76583: Bounded Wait and Explicit Blocked Outcome for GitHub API Quota Exhaustion

**Date**: 2026-11-10
**Status**: Draft
**Deciders**: SivaKesava1 (requester, PR #76583); reviewers [TODO: verify]

---

### Context

Activation and detection setup share the GitHub REST quota with every other workflow in the organization. When the core quota reached zero (incident evidence: `resource=core`, `limit=15000`, `remaining=0`), three setup steps failed with generic errors: daily-AIC restore and accounting and "Generate agentic run info" in activation, and the AWF installer's `gh attestation verify` call in detection. The detection failures happened after agent jobs had already completed, so finished work was lost and the failure looked like an ordinary crash with no reset time. The fix must not bypass AIC budget enforcement, release attestation, or fail-closed threat detection, and any wait must be bounded so jobs do not hold runners indefinitely.

### Decision

We will treat GitHub API primary-quota exhaustion, and explicit server retry restrictions, as a distinct and recoverable outcome. Each affected call waits once until the server's own reset or `Retry-After` deadline when that fits a bounded budget (10 minutes in activation; 300 seconds cumulative for the AWF installer), then retries once. Otherwise the job stops with an explicit "GitHub API quota exhausted" outcome (`quota_blocked` in activation, `github_api_quota_exhausted` in detection) and takes no fallback path. The primary driver is reliability without weakening the security posture.

### Alternatives Considered

#### Alternative 1: Keep failing fast on any quota error (status quo)

The status quo propagates the quota error as a generic failure. It is simple and keeps every failure visible, and it is the behavior seen in the incident. It was rejected because it turns a server-declared, time-bounded reset into a lost run: detection failed after agents had completed successfully, and the error gave no reset time, so operators could not tell whether a rerun would help.

#### Alternative 2: Unbounded wait, or continue without verification on quota failure

Two variants were considered. An uncapped wait until the reset can hold a runner for an unknown duration, which conflicts with job timeouts. Continuing without verification, for example by using the platform AWF binary when bundle attestation cannot be checked, avoids the wait but bypasses signer and checksum policy. The bounded wait was chosen because the failed response carries the exact reset time, so the wait can be capped. The verification fallback was rejected outright: a failed bundle verification never falls back to the platform binary. This was the closer call between the two variants.

#### Alternative 3: Obtain quota from a separate credential or GitHub App

Spreading calls across a separate app installation or credential would raise effective quota without any waiting. It was not chosen for this change. It touches credential attribution and installation ownership, which the PR description places in follow-up work. It remains a viable future option if quota exhaustion recurs.

### Consequences

#### Positive
- Quota exhaustion within the wait budget recovers without a manual rerun.
- Completed agent work is preserved. A deferred detection keeps artifacts and does not publish safe outputs.
- Activation makes zero Contents API calls when the executing revision's lock file is present in the local git object database, because run-info reads that copy first.
- Quota blocks are reported with a summary table (resource, limit, remaining, used, reset). AIC totals never show a false zero.

#### Negative
- Jobs can sit idle for up to 10 minutes in activation, or 300 seconds cumulative in the installer, while waiting for a reset. This consumes runner minutes.
- In `continue-on-error` warn mode the AIC guardrail stays fail-open during quota exhaustion, so AIC budget is not enforced in that window.
- A deferred detection requires a rerun before safe outputs are published.
- The change adds complexity across JavaScript, shell, the Go compiler, and many regenerated `.lock.yml` files.
- When a rate-limit message appears, the attestation check adds one extra `gh api --include` probe, because `gh` does not expose response headers.

#### Neutral
- `quota_blocked` is added to the unified-session type and its regenerated JSON schema. `github_api_quota_exhausted` is recognized by threat-detection warnings, `handle_detection_runs`, the usage-summary allow-list, and the CLI audit parser.
- The conclusion job does not react to activation's `quota_blocked`. That outcome appears in the activation summary, warnings, and outputs.
- The inline (non-feature-flag) detection path receives only the installer behavior. The step-level gating applies to the external-detector path.
- `release_integrity.sh` is SHA-pinned by the TLA+ workflows, so their pins (`shared/tla-plus.md` and `daily-work-queue-formal-verification.md`) are updated in this PR.
- Open items from the PR description: the `quota_blocked` wording in `ai-credits-specification.md`, a CodeQL run, and `make agent-finish`.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
