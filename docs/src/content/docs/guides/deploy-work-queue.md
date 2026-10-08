---
title: How to deploy a work queue
description: Configure trusted producers, dispatchers, workers and an administrator-installed Git-backed queue policy.
---

A Git-backed queue requires an installed Policy and protected queue branch.
Frontmatter does not install Policy or provision writer restrictions. Use this
guide after preparing a trusted producer identity and an approved worker.

## Publish the worker and dispatcher

Publish and compile both workflows on the repository's default branch. Configure
the worker with a required assignment:

```yaml title="Worker frontmatter"
tools:
  work-queue:
    storage: git
    require-assignment: true
    worker: true
```

Allow only the worker names the dispatcher needs, with a bounded dispatch budget:

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

This allowlist is compiler approval, not dispatch authority. Queue dispatch uses
the installed worker profile's immutable SHA and authenticated principal, not
the dispatcher's moving `target-ref`. Do not give the agent queue-branch write
credentials.

## Define the Policy

Create a complete `QueuePolicy` JSON file using this template. Replace the
repository, both actor IDs and the worker revision. Actor IDs must be verified
positive decimal GitHub principal IDs; the revision must be the actual 40- or
64-character commit containing the worker workflow.

Add one producer entry per trusted submission identity. Administrator status
does not grant producer entitlement. The producer ID must match the principal
authenticated for its submission path. The worker principal must match the
identity proven by the dispatch credential and worker-run authentication, not
a display name or `github.actor`.

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

The template uses the default empty accounting key and maximum supported native
limits. Lower limits when needed; do not increase them beyond those bounds.
The 64 MiB ordinary ledger budget and 16 MiB recovery reserve are separate:
80 MiB is not an admissible ordinary ledger limit. Additional accounting keys
must retain `"": 1` and explicitly grant producer entitlements.

## Protect the queue branch and install Policy

Independently provision branch protections before installation: only the trusted
operator/host may write the queue branch, force updates and deletion must be
prevented, and workflow-agent credentials must not bypass those rules.

> [!WARNING]
> Automated verification and provisioning of writer restrictions are deferred.
> Policy installation does not establish this deployment boundary. See the
> [implementation coverage table](../../specs/work-queue-specification/#91-implementation-coverage-and-remaining-requirements).

With the worker route active, run the following as an explicitly authenticated
administrator authorized to update the protected queue branch:

```bash
gh aw work-queue --repo github/gh-aw policy \
  --file queue-policy.json --epoch eslint-queue-v1
```

The default queue branch is `work-queue`. To use a separately protected branch,
put `--branch QUEUE_BRANCH` before `policy`. Later Policy changes require a
quiescent queue; frontmatter never installs or amends Policy.

## Submit and inspect work

The trusted producer may stage `work_queue_submit` requests only within its
installed pools, priorities and accounting keys. The dispatcher requests a
bounded pool prefix with `work_queue_dispatch_next`; the scheduler selects the
eligible Work and approved worker profile. Workers process only their
version-3 assignment's `claims` array and scope every effect to its original
Claim handle.

Inspect state using `gh aw work-queue --repo github/gh-aw state`, or use `tui`
for keyboard navigation. Use `replay --json` for the full causal projection.
See the [queue reference](../../reference/work-queue/) for operator commands and
the [daily report portfolio](../../patterns/daily-report-portfolio/) for a
dedicated-queue deployment example.
