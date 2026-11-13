---
description: Prepare reviewed branches and run explicitly authorized, bounded live debugging tests.
disable-model-invocation: true
---

# Live Workflow Testing

Load this guide only for permitted live testing, not ordinary diagnosis,
compile-only checks or existing-run health. First follow the
[shared strategy](debug-agentic-workflow.md) for requested scope, host
restrictions and review requirements. This procedure grants no authorization.

## Run an Unmodified Default-Branch Workflow

When the user explicitly requests an existing workflow from the repository's
default branch, **dry-run is not required** if the workflow is unmodified.
Do not recompile, replace its published lock with a diagnostic lock, or require
a feature branch solely to dispatch that workflow.

Resolve the repository's actual default branch and record its current commit.
Review the published source, generated lock, imports and pins at that commit;
apply the authorization, effect-scope, run/time/spend bounds, monitoring and
stop conditions in [Live Debug Loop](#live-debug-loop). Dispatch with
`gh aw run WORKFLOW --ref DEFAULT_BRANCH`, rechecking the remote commit before
dispatch and verifying the resulting run SHA.

This exception does not override stricter host or repository restrictions,
including Copilot cloud's dry-run-only dispatch rule. Any workflow change
returns to the branch preparation and review gates below; authorization for an
unmodified default-branch run does not cover a changed revision.

## Test Workflow Changes from a Branch

Notify the user before live-test authorization: a published feature branch
containing the reviewed Markdown and generated lock is required to live test
workflow changes. Name the branch and exact commit; reuse the current suitable
branch rather than creating another. A draft PR is recommended, not required,
and a merge is not required. If publication is still needed, state that the
agent will prepare and publish the branch before dispatch.

When updated workflow files are unpublished, recommend a feature branch and
draft PR instead of requiring a merge to the default branch or ending with a
generic refusal. Reuse an existing suitable branch or PR. The agent prepares
the patch, compiles and reviews the source and generated lock, then publishes
them together when authorized. Use the PR for review; creating it is not live
execution approval.

After the live gates below are satisfied, dispatch the exact reviewed branch
with `gh aw run WORKFLOW --ref REVIEWED_BRANCH`. Verify its remote commit before
dispatch and the resulting run SHA afterward. Review imports, runtime/action
pins and immutable worker profiles, since selecting a branch does not
automatically update dependencies or worker revisions pinned elsewhere.

A branch is not an isolated test environment: repository secrets, queue
branches, memory, issues, PRs and other outputs can still be shared with
production. Retain the approved effect scope and required protections; use a
reviewed dry-run lock when testing startup without compiler-managed mutations.
Dry-run suppresses queue writes and worker launches, so it cannot verify live
dispatch. Under the [work-queue protocol](work-queue.md), the first accepted
producer submit bootstraps Policy and Work automatically — do not demand
administrator seeding or repository-rule inventories for queue use.

If another gate blocks execution, state the specific blocker and the smallest
authorized resolution. Branch/PR preparation is still useful progress, but
does not override no-dispatch contexts, credential denials, missing required
protections or unresolved review findings. Do not switch refs, credentials or
execution contexts to evade a failed gate.

## Live Debug Loop

Include the dispatch ref and exact reviewed commit in the user-visible
authorization packet. For workflow changes, publish the reviewed test branch
first; a PR alone does not establish which revision will run. Unmodified
default-branch runs use the published revision above without a test branch.
Branch publication is not a substitute for live-effect authorization.

Before each upload/dispatch, require a completed [agentic security review](debug-security-review.md) for the current source/lock hashes and valid human authorization: source/lock hashes,
triggers/imports/pins, permissions/OIDC, declared credential sources and flows, destinations,
external writes and changed approval protections. The agent gathers and reviews
this evidence; the human validates the agent-prepared packet and authorizes
effects, rather than performing the technical review. Specify run-count, time/spend
caps and monitoring deadline/poll interval; default to one run.
An explicit [session authorization](debug-security-review.md#session-authorization)
may cover subsequent agent-reviewed iterations within the recorded scope and
bounds without repeated human approval. Any compiler security warning revokes it;
a later clean compile does not reinstate it.
Record host/repo, workflow, inputs, reviewed commit and remote ref. Recheck the
ref before `gh aw run WORKFLOW --ref REVIEWED_REF`; verify the resulting run SHA.

| Outcome | Required action |
| --- | --- |
| Dispatch 403 or audit/log collection denied | Stop live iteration; preserve redacted denial/patch; diagnose/patch. |
| Audit nonzero exit, run pending/in progress | Confirm same-run status; poll within approved bounds. Deadline expiry leaves verification incomplete. |
| Other audit/log collection error | Evidence unavailable; diagnose/patch, not another run. |
| Dispatch timeout/no run ID | May have started. Reconcile by approved identity/time; no match remains unknown. Stop without redispatch. |
| Moved ref or mismatched SHA | Invalidate review/test claims; retain actual identity/evidence as unverified; stop. |

No automatic dispatch retries or credential/API/session swaps, push-trigger or
manual-dispatch workarounds. Stopping iteration does not cancel started runs;
cancel only when authorized. Audit matching completed runs, add regressions,
and revalidate every revision before another approved iteration.
