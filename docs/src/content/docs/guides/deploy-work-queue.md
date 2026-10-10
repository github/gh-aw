---
title: How to deploy a work queue
description: Configure trusted producers, dispatchers, workers and first-submit bootstrap for a Git-backed queue.
---

Use this guide to deploy a native Git-backed queue with `tools.work-queue`.
Work queues can also use lightweight
[issue-backed queue patterns](/gh-aw/patterns/workqueue-ops/). Those patterns
use GitHub read tools and safe outputs, but do not provide native fair
scheduling, Claim authority, or verified dependency graphs. Native version-3
`tools.work-queue` always uses Git; no storage selector is available.

Publish approved AW workers and enable work-queue. AW handles authorization,
credentials and approved dispatch targets; work-queue handles scheduling and
reliable delivery. No queue enrollment, producer allowlist, principal, trust
domain, credential scope, branch-protection setup or administrator seed is
required. The first accepted submission installs Policy and Work together.
Compilation alone does not install Policy.

## Publish the worker and dispatcher

Publish and compile the worker and dispatcher workflows on the repository's
default branch. Require an assignment for the worker so it cannot run without
the Claims that authorize its work:

```yaml title="Worker frontmatter"
tools:
  work-queue:
    require-assignment: true
    worker: true
```

Allow only the worker names the dispatcher needs, and set a dispatch limit:

```yaml title="Dispatcher frontmatter"
tools:
  work-queue: true
safe-outputs:
  dispatch-workflow:
    workflows: [eslint-refiner]
    target-ref: ${{ github.event.repository.default_branch }}
    max: 3
  noop:
```

The allowlist tells the compiler which workers are approved. It does not
authorize arbitrary dispatch. Queue dispatch uses the compatible approved
execution revision (or explicit Work pin) and AW's selected credential, then binds the actual native run to its
Claim, not the dispatcher's moving `target-ref`. Do not give the agent credentials
that can write to the queue branch.

## Optionally override scheduling

Without `aw.json`, without `work_queue`, or with `"work_queue": {}`, bootstrap
uses weighted-priority scheduling, one pool named `default`, concurrency 16,
pending limit 4,096, singleton assignments, and three attempts with a
30-second backoff. Override only the values needed:

```json title=".github/workflows/aw.json"
{
  "work_queue": {
    "concurrency": 3,
    "pending_limit": 30,
    "retry": {"max_attempts": 3, "backoff_seconds": 30}
  }
}
```

Invalid settings fail with the property path and supported values. Advanced
`pools`, `class_weights` and `accounting_weights` are optional scheduling
settings; they never contain identities or worker routes. Global backing Issue
projection and labels also belong in `work_queue.issues`, for example
`"issues": {"label": "work"}`. All pools inherit
the declared AW worker routes; a dispatcher's AW allowlist still limits its
launches. See the [configuration reference](/gh-aw/reference/work-queue/#repository-scheduling-settings).

## Automatic first-use bootstrap

Submit Work through the authorized producer or dispatcher. Its first accepted
`work_queue_submit` atomically creates `work-queue` with the compiler-approved
Policy followed by Work in one commit. Normal contents-write permission is
sufficient for publication; no queue-specific administrator seed or repository
rule setup is required.

GitHub enforces repository access rules independently of the queue protocol.
Report an actual Git write denial rather than bypassing it.

> [!WARNING]
> Direct Git writes by other repository writers can alter or delete the
> ledger. The runtime validates existing history and rejects malformed or
> mismatched ledgers, but cannot prevent direct Git writes or reconstruct a
> deleted queue. A missing branch is treated as a fresh queue; deleting it can
> lose prior request identities, reservations and completion evidence.

Make sure the configured worker routes are active and the trusted producer
workflow embeds its complete compiler-approved Policy proposal. Submit before
requesting dispatch. Activation, dispatch-only requests, reads and controls do
not provision the queue.
Activation treats a genuinely absent branch as an uninitialized, empty queue,
even when a compiled Policy proposal is present. It validates the proposal
without installing it; the checked commit publication creates the branch.
An existing ledger with missing or invalid Policy still fails validation.

Native `submit-work` and `submit-graph` also bootstrap on first submission
without an administrator seed. They resolve scheduling and declared AW worker
routes from the verified repository revision, not an unrelated local checkout.
Publish assignment-capable AW workers first. Embedding hosts can provide an
approved `Branch.PolicyProposal`.

```bash
gh aw work-queue --repo OWNER/REPO submit-work \
  --file work.json --request-id initial-work
```

Keep the same request ID and immutable input when retrying an uncertain
publication. Recovery returns the accepted result without another admission.

The default queue branch is `work-queue`. To use another queue branch, put
`--branch QUEUE_BRANCH` before the submission command. Subsequent submissions
use the installed Policy. Editing `aw.json` does not rewrite active queue state.

Remove any setup calls to `initializeWorkQueue`, `initializationContext`, or
pre-submission `policy`: standalone seeding is unsupported. Existing historical
Policy-only genesis records remain readable and do not require migration.

## Evolve workers without draining

Publish and compile a compatible worker update. Its compiler-derived logical
contract preserves declared assignment inputs, permissions, tools and
output/resource capabilities; prompt or engine changes alone do not change
that contract. The trusted deployment transition affects future admissions
and assignments, not existing Work identities, dependencies, Results, fairness
debt or reservations. Explicit `execution_ref` pins retain their approved
revision. Existing assignments complete and reconcile using their frozen
original profile and native run/credential binding.

An incompatible or unavailable worker pauses affected Work, not unrelated
tasks or the entire graph. Do not delete the queue, rewrite completed results
or revoke outstanding assignments to deploy new code. Historical workers
without logical-contract metadata remain exact-ref routes.

Protected producer/dispatcher processing automatically synchronizes its approved
targets before new submit/dispatch requests. To refresh native operator routes:

```bash
gh aw work-queue --repo OWNER/REPO deploy --from-config
```

Use `--pool` or `--worker-profile` to limit this refresh. For reproducible Work,
add `--execution-ref IMMUTABLE_SHA` to native `submit-work` or `execution_ref`
to the submission node. Keep the same pin on retries; changing it requires new
Work rather than rewriting an admitted obligation.

Upgrade readers and runtimes before deploying contract-stamped workers. An
existing historical unmarked Policy does not infer compatibility; migrate it
through the explicit quiescent Policy path once, then use drain-free deployment
updates.

## Update scheduling economics

Before changing Policy later, make the queue quiescent: settle all nonterminal
(unfinished) Work, outstanding reservations, and unresolved delivery of
completed Work. Cancellation alone does not stop a worker or release its
reservation; reconcile exact termination or nonlaunch evidence separately.
Configuration alone does not update Policy; later changes require an explicit,
authorized update.

```bash
gh aw work-queue --repo OWNER/REPO policy \
  --from-config --epoch revised-policy
```

This command resolves scheduling and AW worker routes from repository
configuration and cannot initialize an absent queue. `--file queue-policy.json`
remains available for explicit historical Policy migrations.

## Enable backing Issue projection

Upgrade all queue readers and workflow runtime deployments first. Older
version-3 closed-schema readers cannot read the new projector rules, Issue
links, or comment handles; do not enable them during a mixed-reader rollout.

Set `"issues": true` in `.github/workflows/aw.json` `work_queue` to project
the `work` tracking label and purple `work:<status>` labels (for example,
`work:queued`). Status names are lowercase and hyphenated, without spaces. To
use a different prefix, configure `"issues": {"label": "cookie"}` there. Labels are
provisioned as needed in each target repository; no organization field or
Project is required. See the
[backing Issue reference](/gh-aw/reference/work-queue/#backing-issues).

Historical Policies use an explicit `projectors` array with the actual
authenticated principal and immutable workflow revision for each producer and
worker hook. These legacy rules remain readable; they are not part of
`aw.json.work_queue` or required enrollment for AW-managed queues:

```json
{
  "projectors": [
    {
      "principal": "REPLACE_WITH_NATIVE_PRINCIPAL_ID",
      "workflow": ".github/workflows/eslint-refiner.lock.yml",
      "ref": "REPLACE_WITH_40_OR_64_HEX_COMMIT_SHA",
      "pools": ["default"],
      "repositories": ["github/gh-aw"],
      "completion_policy": "keep-open",
      "backing_issues": [
        {
          "kind": "issue",
          "host": "github.com",
          "repository": "github/gh-aw",
          "repository_id": "REPLACE_WITH_NUMERIC_REPOSITORY_ID",
          "resource_id": "REPLACE_WITH_NUMERIC_ISSUE_ID",
          "number": "REPLACE_WITH_ISSUE_NUMBER"
        }
      ]
    }
  ]
}
```

Merge this property into the complete Policy, not a standalone policy file.
Replace the resource placeholders with the existing Issue's exact decimal
identities, kept as strings. Omit `backing_issues` entirely when only
projector-created Issues are needed.
AW-managed queues also require these explicit grants for pre-existing Issues,
cross-repository projection or closure on Result. Ordinary same-repository
Issue creation needs no projector rules and keeps Issues open.
Install the amended Policy while the queue is quiescent. Scope the
protected hook credential to queue contents writes, Actions reads, and Issue
writes in its allowed backing repositories. If `safe-outputs.github-app` is
configured, each hook mints its own scoped projector token; the runtime checks
the actual token's field capability. Keep this credential out of agent execution.
GitHub repository access rules govern queue and coordination refs; the queue
requires no branch-protection setup. Hooks need permission to create/delete
their own `gh-aw-issue-projection/*` coordination refs.

Submit one independently tracked Work per Issue. Supply `backing_issue` for an
existing Issue only after installing its full resource identity in each
relevant projector rule's `backing_issues` array. A repository allowlist alone
does not authorize a pre-existing Issue. Alternatively, let the authorized
hook create it after durable admission; its verified creation receipt and
checked `IssueLink` establish the binding.
Keep Issues open by default, or install `completion_policy: "close-on-result"`
on the relevant projector rules before admission. This trusted policy, not
agent payload, controls closure after verified non-PR delivery.
Native runs and failed/skipped jobs never substitute for Result. Pending
synchronization leaves Git authority intact and can be retried only by a hook
owning the same Work/Claims; unrelated runs do not repair it.

## Submit and inspect work

Use `work_queue_submit` as the trusted producer to stage tasks within the
pools, priorities, and accounting keys allowed by Policy. Staging a request
does not commit it to the queue.

Use `work_queue_dispatch_next` as the dispatcher to request a limited number of
assignments from a pool. The scheduler—not the agent—selects eligible Work
(immutable tasks) and an approved worker profile. Do not use legacy scalar
dispatch to choose a winner.

Configure workers to process only the `claims` array in their version-3
assignment. A Claim authorizes one attempt at a task. Associate every effect
(such as creating an issue) with its original Claim handle. A multi-Claim
assignment requires `claim_handle` even when only one member remains open;
only an originally single-Claim assignment may omit it. Worker finish is an
intent, not proof of delivery: Completion records task completion, and Result
requires independently verified delivery.

Inspect the queue with `gh aw work-queue --repo github/gh-aw state`, or use `tui`
for keyboard navigation. Use `replay --json` for the full state reconstructed
from the transaction log. See the
[queue reference](/gh-aw/reference/work-queue/) for operator commands and
the [daily report portfolio](/gh-aw/patterns/daily-report-portfolio/) for a
dedicated-queue deployment example.
