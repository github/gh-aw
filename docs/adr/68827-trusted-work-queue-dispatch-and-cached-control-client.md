# ADR-68827: Route Trusted Work-Queue Dispatch Through the Controller and Build the Control Queue Client From the Publisher Token

> [!NOTE]
> Superseded by work queue protocol v4 ([specification](../src/content/docs/specs/work-queue-specification.md)): v4 removed revision pinning and principal proofs; dispatchers launch workers at the configured target ref and actors come from the job's own context.

**Date**: 2026-10-14
**Status**: Proposed (implemented in PR #68827; maintainer acceptance pending)
**Decision owner**: @pelikhan, the PR author. Formal architecture approval is not implied by this record.

---

### Context

Operators need a supported CLI entry point to start a *dispatcher* without selecting a Work, assigning workers directly, or rebinding immutable Claims. Dispatcher activation must use the same revision whose source and compiled controls were validated, even if the default branch moves between validation and launch.

Separately, the control step for trusted queue operations used a queue client without registered static authentication, so repeated queue loads within one invocation re-verified the full checkpoint history. On a full-step fake-transport fixture (32 ancestors, 3 deployment updates, 15 Work items, 100 retained requests), the baseline required 4,946 API calls and 8,181 ms locally, dominated by repeated Git commit/tree/blob reads. A related defect affected same-epoch compacted-Policy recovery for accepted control requests without a compacted parameters digest.

### Decision

Add a first-class `gh aw work-queue --repo OWNER/REPO dispatch DISPATCHER` command that resolves a dispatcher route at the repository's verified default revision (`pkg/workqueue/dispatcher.go`, `pkg/cli/work_command_dispatch.go`) and activates the dispatcher whose trusted controls select and launch assignments. Validate the dispatcher source and its compiled `.lock.yml` and correlate the native run rather than assigning workers or rebinding immutable Claims.

Construct the control-step queue client from the protected publisher token before the first queue load (`actions/setup/js/work_queue_control_adapter.cjs` reading `GH_AW_WORK_QUEUE_TOKEN`). The compiler binds this environment variable to the same publisher credential used by the control step, using the existing shared publisher-token resolution. Registered static-auth clients become eligible for checkpoint-history proof reuse within the invocation. Registration is scoped to the exact client object and unchanged authentication; injected or dynamic-auth clients remain uncached unless explicitly registered at a known static-token constructor. Worker-launch credentials remain a separate credential path, and control-step preview constructs no credential client.

The dispatcher validates both compiled control allowlists against the source worker-name set, then creates or reuses the SHA-derived lightweight tag `refs/tags/gh-aw/work-queue/<verified-sha>`. Dispatch targets that verified tag, not the moving default branch; mismatched tags and ambiguous creation failures stop before activation. Tags are never overwritten or deleted by the command. Repository writers remain trusted not to mutate these tags while in use; returned native-run identity checks detect mismatches but cannot undo an activation. Preview performs only read-only route validation.

### Alternatives Considered

The following alternatives describe the tradeoffs assessed for this record, not a claim that each was previously implemented or formally reviewed.

#### Alternative 1: Use worker-assignment / Claim operations for dispatch

Expose dispatcher activation through worker-assignment or Claim-manipulation operations. This conflates launching the scheduler with selecting or assigning work. A dedicated dispatcher command preserves the trusted controller's authority over assignment and leaves immutable Claims unchanged.

#### Alternative 2: Keep injecting an arbitrary queue client and add a dedicated history cache

Instead of constructing the client from the publisher token, add a separate memoization layer for ancestry reads to an arbitrary injected or dynamic-auth client. This would duplicate the existing static-auth cache mechanism and require additional safeguards against reusing verification proofs after authorization changes. Using a known static-token constructor enables the existing exact-client-scoped mechanism without broadening cache eligibility.

#### Alternative 3: Reduce history depth (shallower checkpoint verification)

Verifying fewer ancestors could reduce cold-read cost, but changes checkpoint verification semantics. Preserve full verification, fresh authorization/ref probes, and compare-and-swap behavior; optimize repeated reads instead.

#### Alternative 4: Dispatch the moving default branch or a bare commit SHA

Dispatching the default branch after validation can activate different code if it moves. A SHA-derived tag supplies a named Actions dispatch ref tied to the verified commit without relying on acceptance of a bare SHA. Persistent tags and the repository-writer trust assumption are the cost of this choice.

### Consequences

#### Positive
- Dispatch becomes a supported, validated operation (name pattern, frontmatter, compiled lock, verified default revision) rather than an ad-hoc workaround.
- The full-step fake-transport fixture drops from 4,946 to 305 API calls, 2,080 to 65 ancestry commit reads, and 8,181 ms to 705 ms local elapsed time. Offline replay of a captured live ledger preserves the 413-request cold read and reduces repeated reads from 413 requests to 2.
- Regression coverage exercises source/compiled allowlist parity, snapshot activation, native-run identity, credential separation, retries, and control-step performance.
- Same-epoch compacted-Policy recovery now works for accepted control requests without a compacted parameters digest, while retaining actor and request-fingerprint checks.

#### Negative
- A new credential binding (`GH_AW_WORK_QUEUE_TOKEN`) is threaded into compiled workflows, requiring affected lock files to be regenerated and reviewed for publisher-token correctness. It reuses the existing publisher credential rather than granting additional token authority.
- The dispatcher CLI adds a new trusted entry point that must be kept in lockstep with queue protocol changes and validated on every protocol upgrade.
- Snapshot tags persist and require `contents: write` when created, in addition to workflow-dispatch permission. Privileged tag mutation remains outside the ordinary branch-movement guarantee.
- Repeated-read performance depends on static-auth registration of the exact client: unregistered or dynamic-auth clients retain the uncached path, even if they currently resolve to the same token.

#### Neutral
- Cache eligibility rules, checkpoint verification, fresh authorization/ref probes, and compare-and-swap semantics are unchanged; proof reuse is scoped to one invocation of a registered, unchanged static-auth client.
- CLI preview validates the route through read-only repository API calls but does not create a snapshot tag or activate a workflow. Control-step preview does not construct publisher or launch credential clients and cannot validate launch credentials.
- Cold history traversal is unchanged. Reported elapsed timings use a fake local transport; no production elapsed-time improvement has been verified.

---

Implementation: [PR #68827](https://github.com/github/gh-aw/pull/68827).
