# ADR-68348: Dispatch queue launches through verified SHA-derived lightweight tags

> [!NOTE]
> Superseded by work queue protocol v4 ([specification](../src/content/docs/specs/work-queue-specification.md)): v4 removed immutable worker revisions and SHA-derived tags; workers launch at the configured target ref.

**Date**: 2026-10-13
**Status**: Draft
**Deciders**: Unknown (draft generated from PR #68348)

---

### Context

The work queue dispatcher reserves Claims for approved, immutable worker revisions and then launches the worker through GitHub's `workflow_dispatch` API. GitHub's dispatch endpoint requires a *named* ref (branch or tag); it rejects a raw commit SHA with HTTP 422. The queue, however, binds a Claim to an exact approved commit SHA, so passing `destination.ref` verbatim caused every worker launch to be rejected by the provider while the dispatcher still reported the released reservation as a successful launch. Operators therefore saw green dispatcher runs with no worker runs and no recovery signal. The queue's safety constraints are non-negotiable: at most one POST per reservation (no retry of an uncertain dispatch), no reuse of a ref whose commit is not the approved SHA, and no logging of assignments, credentials or raw provider error bodies. Separately, the Go operator failed validation of historical checkpoints affected by a known deployment-aliasing defect that the JavaScript runtime already recovers from, so the two implementations disagreed on the same Git history.

### Decision

We will dispatch queue launches against a lightweight tag derived from the approved commit, `refs/tags/gh-aw/work-queue/<approved-sha>`, created on demand and then read back and verified (exact ref name, `commit` object type, exact SHA) before any POST; an existing tag is never moved or deleted, and a concurrent-creation 422 is tolerated only when the subsequent read-back proves the tag points at the approved SHA. Both the queue launcher (`work_queue_native.cjs`) and the `dispatch_workflow` safe output now share one transport module (`dispatch_workflow_transport.cjs`), with the legacy `return_run_details` fallback enabled only for the safe output and disabled for queue launches so a reservation still yields exactly one POST. A provider rejection is reported as `state: "rejected"`, `dispatched: false` — the reservation is released but the dispatcher step fails and the run summary counts rejected launches — because release is not execution. Finally, Go checkpoint recovery is aligned with the JavaScript runtime, including recovery while validating ancestor checkpoints, accepting only deployment aliasing proven against an unchanged checkpoint in the immediate Git parent, with reads remaining read-only.

### Alternatives Considered

#### Alternative 1: Dispatch against the worker's branch ref (e.g. `refs/heads/main`)

A branch is already a named ref and requires no new write permission on the dispatch credential. It was rejected because a branch is mutable: between reservation and launch the branch tip can move, so the launched worker would not necessarily be the approved immutable revision the Claim authorizes. That breaks the queue's core guarantee that an assignment binds an exact commit SHA, and no later run-identity check could restore it.

#### Alternative 2: Keep the existing release-on-rejection behaviour and only fix the ref

This was a close call, since releasing a reservation after a definitive provider rejection is itself correct and already idempotent. It was rejected because reporting that release as `success: true` hides a systematic launch failure: the exact defect that let the broken ref ship unnoticed. Surfacing rejections as a failing controls step with a counted summary line is what makes a future regression observable without changing the one-POST fencing.

#### Alternative 3: Duplicate the dispatch POST logic in the queue instead of sharing a transport

Keeping two independent call sites avoids coupling the safe output to the queue's stricter fencing rules. It was rejected because the two paths had already diverged in error handling and diagnostics; a single transport with an explicit `allowLegacyFallback` flag makes the difference in retry policy a declared parameter rather than an accident of copy-paste.

### Consequences

#### Positive
- Queue launches now actually reach the provider at the approved immutable SHA, and the tag read-back makes "we dispatched the right commit" a verified property rather than an assumption.
- Rejected launches fail the dispatcher and appear in the step summary (`Rejected launches: N`), so silent systematic launch failure is no longer possible.
- One shared dispatch transport removes divergent error handling between the safe output and the queue, and centralises privacy-preserving failure diagnostics.
- Go and JavaScript now agree on recovered checkpoint histories, backed by explicit equivalence tests and ten corruption-rejection cases.

#### Negative
- The trusted dispatch credential now needs `contents: write` in addition to `actions: write`, widening that credential's scope (the agent still receives neither).
- Each distinct approved worker SHA leaves a permanent `gh-aw/work-queue/*` tag in the repository; no garbage collection is defined, so tag count grows with dispatch history.
- A launch now costs up to three extra API calls (get-ref, optional create-ref, verifying get-ref) before the POST, adding latency and rate-limit pressure to every dispatch.
- Failure diagnostics are promoted to always-visible output, so the dispatcher log is noisier even when `DEBUG` is unset.

#### Neutral
- Diagnostics are deliberately restricted to static event labels, counts, booleans, elapsed time, HTTP status and a fixed set of 422 validation categories; debugging an unanticipated provider error still requires re-running with richer, manually gathered evidence.
- Recovery remains conservative: only aliasing explained by an unchanged immediate-parent checkpoint is accepted, so other corruption still fails validation and requires operator intervention.
- This PR does not establish completed worker delivery; the dispatcher still hit an observed job time limit, leaving some reservations unlaunched. That operational limit is out of scope for this decision.
