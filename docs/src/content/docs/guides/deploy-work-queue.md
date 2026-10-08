---
title: How to deploy a work queue
description: Configure trusted producers, dispatchers, workers and an administrator-installed Git-backed queue policy.
---

Use this guide to deploy a native Git-backed queue with `tools.work-queue`.
Work queues can also use lightweight
[issue-backed queue patterns](/gh-aw/patterns/workqueue-ops/). Those patterns
use GitHub read tools and safe outputs, but do not provide native fair
scheduling, Claim authority, or verified dependency graphs. Native version-3
`tools.work-queue` accepts only `storage: git`.

Before you begin, prepare a trusted producer identity and an approved worker.
The producer submits tasks; the dispatcher requests assignments; the worker
processes them. You also need a Policy (the queue's scheduling and authorization
rules) and a protected queue branch. Workflow frontmatter does not install
Policy or configure restrictions on who can write to that branch.

## Publish the worker and dispatcher

Publish and compile the worker and dispatcher workflows on the repository's
default branch. Require an assignment for the worker so it cannot run without
the Claims that authorize its work:

```yaml title="Worker frontmatter"
tools:
  work-queue:
    storage: git
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
authorize a dispatch. Queue dispatch uses the fixed commit SHA and
authenticated principal (the GitHub identity) in the installed worker profile,
not the dispatcher's moving `target-ref`. Do not give the agent credentials
that can write to the queue branch.

## Define the Policy

Create a complete `QueuePolicy` JSON file from the following template. Replace
the repository, both actor IDs, and the worker revision. Verify that each actor
ID is a positive decimal GitHub principal ID. Set the revision to the actual
40- or 64-character commit SHA containing the worker workflow.

Add one producer entry for each trusted submission identity. Each entry grants
that identity permission to submit to specific pools, priorities, and accounting
keys. Pools group tasks with shared worker routes and capacity limits.
Administrator status alone does not grant these producer entitlements.
The producer ID must match the identity authenticated when it submits work.

For the worker principal, use the identity verified by the dispatch credential
and worker-run authentication. A display name or `github.actor` is not proof
of that identity.

```json title="queue-policy.json"
{
  "mode": "weighted-priority",
  "class_weights": [8, 4, 2, 1, 1],
  "accounting_weights": { "": 1 },
  "producers": {
    "REPLACE_WITH_PRODUCER_ACTOR_ID": {
      "pools": ["default"],
      "priorities": [1, 2, 3, 4, 5],
      "fairness_keys": [""]
    }
  },
  "pools": {
    "default": {
      "default_profile": "eslint-refiner",
      "profiles": {
        "eslint-refiner": {
          "workflow": ".github/workflows/eslint-refiner.lock.yml",
          "ref": "REPLACE_WITH_40_OR_64_HEX_COMMIT_SHA",
          "principal": "REPLACE_WITH_WORKER_CREDENTIAL_ACTOR_ID",
          "trust_domain": "eslint-refiner",
          "credential_scope": "repository",
          "effect_scope": "github/gh-aw",
          "max_claims": 1,
          "share_keys": false
        }
      },
      "logical_limit": 16,
      "native_limit": 16,
      "allowed_repositories": ["github/gh-aw"],
      "max_observation_age_ms": 60000,
      "retry": { "max_attempts": 3, "backoff_ms": 1000 },
      "reconciliation": { "max_attempts": 5, "deadline_ms": 300000 }
    }
  },
  "limits": {
    "ledger_bytes": 67108864,
    "recovery_bytes": 16777216,
    "payload_bytes": 16384,
    "graph_nodes": 4096,
    "predecessors": 64,
    "pending_nodes": 4096,
    "operations": 256,
    "assignment_bytes": 49152,
    "result_bytes": 4096,
    "evidence_bytes": 1024,
    "observation_writes": 4096
  }
}
```

The template uses the default empty accounting key (`""`) and the maximum
supported native limits. Accounting keys group tasks for fairness accounting.
Lower the limits if needed, but do not exceed the values in the template.

The ledger is the queue's transaction log. Its 64 MiB ordinary budget and
16 MiB recovery reserve are separate: you cannot set the ordinary ledger limit
to 80 MiB. If you add accounting keys, retain `"": 1` and explicitly grant
producers permission to use each additional key.

## Protect the queue branch and install Policy

Before installing Policy, configure branch protections separately. Allow only
the trusted operator or runtime host to write to the queue branch. Prevent
force updates and branch deletion, and ensure that workflow-agent credentials
cannot bypass these rules.

> [!WARNING]
> Automated verification and provisioning of writer restrictions are still
> deferred. Installing Policy does not configure or verify these protections.
> You must establish them independently. See the
> [implementation coverage table](/gh-aw/specs/work-queue-specification/#91-implementation-coverage-and-remaining-requirements).

Make sure the configured worker route is active. Then run the following command
as an explicitly authenticated administrator with permission to update the
protected queue branch:

```bash
gh aw work-queue --repo github/gh-aw policy \
  --file queue-policy.json --epoch eslint-queue-v1
```

The default queue branch is `work-queue`. To use another protected branch, put
`--branch QUEUE_BRANCH` before `policy`.

Before changing Policy later, make the queue quiescent: settle all nonterminal
(unfinished) Work, outstanding reservations, and unresolved delivery of
completed Work. Cancellation alone does not stop a worker or release its
reservation; reconcile exact termination or nonlaunch evidence separately.
Frontmatter never installs or updates Policy.

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
