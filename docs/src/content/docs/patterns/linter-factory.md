---
title: Linter Factory
description: An example of fair work-queue dispatch, Claim-scoped outputs, and verified delivery using the ESLint factory
sidebar:
  badge: { text: 'Work queue', variant: 'note' }
---

The ESLint factory is an example of the `tools.work-queue` feature. Its
dispatcher requests eligible work for three worker profiles; each worker
processes an authenticated assignment and attributes its outputs to the
original Claim.

Unlike the lightweight [WorkQueueOps](/gh-aw/patterns/workqueue-ops/) patterns,
this example uses one authoritative `work-queue.jsonl` transaction log on the
`work-queue` branch. Scheduling decisions, Claims, run bindings, Completion,
delivery outcomes, and recovery operations share that log. Issues, pull
requests, and refiner memory are output resources, not alternative queue
ledgers.

## Factory roles

| Workflow | Assigned work | Intended outputs |
| --- | --- | --- |
| [Dispatcher](https://github.com/github/gh-aw/blob/main/.github/workflows/eslint-factory-dispatcher.md) | Request a fair prefix within the installed pool policy | Launch approved worker profiles; no eligible work produces `noop` |
| [Miner](https://github.com/github/gh-aw/blob/main/.github/workflows/eslint-miner.md) | Find and implement a useful new ESLint rule | At most one draft rule PR per Claim, or `noop` |
| [Refiner](https://github.com/github/gh-aw/blob/main/.github/workflows/eslint-refiner.md) | Identify false positives, missing edge cases, or weak diagnostics | Up to three issues, a discussion, and one immutable memory snapshot per Claim |
| [Monster](https://github.com/github/gh-aw/blob/main/.github/workflows/eslint-monster.md) | Group actionable diagnostics and arrange remediation | Issue updates, Copilot assignments, and a discussion, or `noop` after a clean scan |

An authenticated operator must first install Policy, producer entitlements,
worker profiles, immutable workflow revisions, and resource scopes. An
authorized producer explicitly admits Work with its output contract. The
supplied workflows do **not** automatically create a miner-to-refiner-to-monster
DAG, and creating a refinement issue does not submit another Work.

The dispatcher requests
`work_queue_dispatch_next({"pool":"default","max_claims":3,"max_dispatches":3})`.
Those are request ceilings, not guaranteed assignments. The scheduler chooses
the fair eligible prefix; it stops at the first winner it cannot pack. Different
worker profiles use separate dispatches. A profile permits singleton
assignments by default; compatible multi-Claim batches require explicit policy.

## Ledger lifecycle

The sequence shows a successful launch and the per-Claim completion alternatives.
Every ledger write is a version-3 `QueueCommit` published with compare-and-swap
(CAS); the labels below name its operations. Agent tool calls stage intents,
not authoritative records.

```mermaid
sequenceDiagram
    participant O as Authenticated operator
    participant P as Authorized producer
    participant D as Trusted dispatcher
    participant L as work-queue.jsonl
    participant W as Factory worker
    participant T as Trusted reconciliation and delivery
    participant G as GitHub resources

    O->>L: Policy with profiles, entitlements, and resource scopes
    P->>L: Work admission with immutable contract
    D->>L: Replay current head and request fair prefix
    D->>L: CAS Claim operations and immutable assignment
    Note over D,L: Each durable Claim is charged once with no refunds
    D->>L: CAS Dispatch started with one sender
    D->>G: Launch approved immutable worker revision
    D->>L: Dispatch bound to verified run and attempt 1
    L-->>W: Read-only activation snapshot and original claims
    W->>T: Stage outputs with each original claim_handle
    W->>T: Stage work_queue_claim_finish per member
    T->>L: Replay latest head and recheck winning Claim
    alt Completed and still authorized
        T->>L: CAS Completion
        T->>L: Verify durable Completion
        T->>G: Deliver only this Claim's authorized effects
        T->>G: Independently read back the whole contract
        alt Contract verified
            T->>L: CAS Result
            Note over L: Only verified Result releases Work successors
        else Readback unavailable or inconclusive
            Note over T,L: Barrier stays pending. API success is not Result
            T->>L: DeliveryFailure after bounded verification exhaustion and terminal evidence
        end
    else Cancelled or terminal run missing finish intent
        T->>L: ClaimCancellation and retry backoff
        Note over T,G: No effects or Result for the cancelled Claim
    end
```

For the refiner, the verified contract can include issues, a discussion, and the
exact memory commit, tree, blobs, and Claim ref. New snapshots live under
`memory/eslint-refiner-runs/claims/<trusted-namespace>`; the legacy
`memory/eslint-refiner` history is preserved. Both are restored read-only and
cannot grant Claim authority.

A `noop` or an empty output batch is not automatically a verified success.
The installed Work contract must permit the no-write outcome; required output
minimums and independent verification still apply.

## Calls and dispatch input

Each object below is the `params` of an individual MCP `tools/call` request;
the arrays only group examples for display. Queue tools belong to the
`work-queue` server; output tools belong to `safeoutputs`. IDs and task payloads
are illustrative. Assume the installed policy names the approved refiner
profile `eslint-refiner`. `WORK_A` and `WORK_B` stand for the generated Work
IDs returned by queue reads, not caller-chosen task names.

### Read, explain, and admit

Reads and explanations use the activation snapshot, not a fresh reservation.
An authorized producer can separately submit the two independent roots:

```json wrap
[
  {"name":"work_queue_read","arguments":{"pool":"default","limit":3}},
  {"name":"work_queue_explain","arguments":{"work":"WORK_A","pool":"default"}},
  {"name":"work_queue_submit","arguments":{"nodes":[
    {"graph_id":"eslint-refinement","node_key":"parser-edge-cases","worker_profile":"eslint-refiner","payload":{"plan":"Check parser edge cases"}},
    {"graph_id":"eslint-refinement","node_key":"diagnostic-wording","worker_profile":"eslint-refiner","payload":{"plan":"Check diagnostic wording"}}
  ]}}
]
```

Submission stages an intent; it does not install policy, bypass entitlements,
or give the producer a Claim.

### Ask for the next fair prefix

```json
{"name":"work_queue_dispatch_next","arguments":{"pool":"default","max_claims":3,"max_dispatches":3}}
```

The immediate MCP text result decodes to a staging acknowledgement, not an
assignment:

```json
{"intent_id":"intent:example","status":"staged"}
```

The trusted publisher subsequently selects and records Claims. If policy
permits a compatible two-Claim batch, the decoded assignment has this shape:

```json wrap
{
  "version": 3,
  "dispatch_id": "d_example_1",
  "request_id": "request:example",
  "commit_id": "commit:example",
  "policy_epoch": "factory-v1",
  "pool": "default",
  "worker_profile": "eslint-refiner",
  "claims": [
    {"handle":"h1","claim_id":"c_example_1","work_id":"WORK_A","work":{"plan":"Check parser edge cases"},"result_refs":[]},
    {"handle":"h2","claim_id":"c_example_2","work_id":"WORK_B","work":{"plan":"Check diagnostic wording"},"result_refs":[]}
  ]
}
```

Each `work` is the exact admitted payload, including any output contract;
`result_refs` carries independently verified predecessor Results when present.
This example is not an authenticated grant and must not be manually replayed.

### Pass the assignment through `workflow_dispatch`

The protected publisher sends **one JSON string**, not a nested input object.
This excerpt shows native request fields, not an agent-accessible dispatch tool:

```javascript
await dispatchClient.rest.actions.createWorkflowDispatch({
  owner: "github",
  repo: "gh-aw",
  workflow_id: profile.workflow,
  ref: profile.ref,
  inputs: { work_queue_assignment: canonical(assignment) },
  headers: { "X-GitHub-Api-Version": "2026-03-10" },
  request: { retries: 0, retryCount: 0, timeout: 30000 }
});
```

`profile.workflow` selects the compiled refiner workflow and `profile.ref` is
its installed immutable commit SHA. The runtime fences the sender in the
ledger before POST and verifies the returned
run before binding it. The compiler reserves `work_queue_assignment` as a
string input; supplied JSON alone cannot authorize worker outputs.

### Attribute outputs and finish each member

For the batch above, the refiner stages an issue and memory for `h1`, completes
that member, and cancels `h2` independently:

```json wrap
[
  {"name":"create_issue","arguments":{"claim_handle":"h1","title":"Handle optional-chain parser edge case","body":"The rule flags a valid optional-chain expression. Add a regression case and preserve the intended diagnostic."}},
  {"name":"persist_eslint_memory","arguments":{"claim_handle":"h1","memory":"{\"work_id\":\"WORK_A\",\"strategy\":\"parser-edge-cases\",\"findings\":[\"Optional-chain false positive\"]}"}},
  {"name":"work_queue_claim_finish","arguments":{"claim_handle":"h1","outcome":"completed"}},
  {"name":"work_queue_claim_finish","arguments":{"claim_handle":"h2","outcome":"cancelled"}}
]
```

The memory argument is itself an encoded JSON object. Copy each original
`handle` into `claim_handle`; do not substitute `claim_id` or `work_id`. These
calls stage intentions, not immediate GitHub writes or Result. Any additional
outputs required by the immutable contract must also be staged before finish.
No MCP call lets the worker assert its own Completion or verified Result.

## Singleton and mixed-Claim outcomes

Batching shares a worker run, not completion or output authority. In this
example, a refiner receives original assignment `[c1,c2]`. Cancelling `c2` does
not shrink that assignment or make `c1` an implicit singleton.

```mermaid
flowchart TD
    A["Immutable refiner assignment: c1, c2"] --> C1["c1: issues, discussion, memory<br/>All intents select c1's original handle"]
    A --> C2["c2: unable to finish<br/>Cancel using c2's original handle"]
    C1 --> Complete["Completion for c1"]
    Complete --> Effects["Deliver and independently verify c1's contract"]
    Effects --> Result["Result for c1"]
    Result --> Ready["Successors depending on c1's Work can become eligible"]
    C2 --> Cancel["ClaimCancellation for c2"]
    Cancel --> Retry{"Attempt budget remaining?"}
    Retry -- Yes --> Backoff["Work can be claimed again after backoff<br/>New Claim incurs a new charge"]
    Retry -- No --> Terminal["WorkCancellation"]
    Cancel -. "Does not authorize" .-> Forbidden["c2 effects or Result"]
    A -. "Membership never shrinks" .-> Selector["Explicit selectors remain required"]
```

The originally singleton case may omit `claim_handle`. An original multi-Claim
assignment always needs an explicit original handle, even when only one member
remains unfinished. Counts are checked per Claim and output family: one
member's unused issue allowance cannot subsidize another member's overflow.

## Contention and interrupted launches

Recovery records its decisions in the same ledger. A stale snapshot is never
authority to publish a previously selected batch or release a reservation.

```mermaid
flowchart TD
    Plan["Replay latest head and derive candidate"] --> CAS{"CAS accepted?"}
    CAS -- "Head moved" --> Refresh["Fetch new head and recompute selection"]
    Refresh --> Plan
    CAS -- Yes --> Sender["Fence one sender with Dispatch started"]
    Sender --> Launch{"Launch and binding evidence?"}
    Launch -- "Verified exact run" --> Bound["Dispatch bound; worker may activate"]
    Launch -- "Definitive nonlaunch" --> Release["ClaimCancellation plus Release"]
    Launch -- "Timeout or ambiguous response" --> Hold["Retain reservations<br/>Do not blindly repeat launch"]
    Hold --> Read["Reconciler reads exact native run evidence"]
    Read -- "Verified run found" --> Bound
    Read -- "Unknown or conflicting" --> Hold
    Bound --> End{"Exact bound run terminated?"}
    End -- No --> Wait["Keep reservation"]
    End -- Yes --> Recover["Cancel only remaining open Claims<br/>Preserve completed members; Release"]
    Recover --> Backoff["Retry eligible Work within policy budgets"]
```

An accepted request replay returns its original outcome; it does not perform
another selection or charge existing Claims again. A crash after Completion
but before effects leaves a delivery barrier to reconcile, not permission to
replay completed effects blindly. This feature does not make external writes
atomic or exactly-once.

## Common pitfalls

| Pitfall | Required distinction |
| --- | --- |
| Treat a rule PR or refinement issue as the next queued task | Output creation is not Work admission. An authorized producer must explicitly install any DAG; Work edges wait for verified Result, and Issue/PR edges need fresh typed observations |
| Treat the activation snapshot, memory, or task text as authorization | Trusted reconciliation replays the latest ledger and validates the original Claim, profile, principal, resource scope, and actual run/attempt |
| Treat finish intent, Completion, job success, or an API response as delivered work | Completion precedes effects; Result requires independent verification of the whole immutable output contract |
| Retry a timed-out launch or release its Claims on elapsed time alone | Keep uncertainty conservative; require definitive nonlaunch or exact bound-run termination evidence |
| Reuse one handle or aggregate all output allowances across a batch | Use each member's original handle and enforce per-Claim counts; original membership never shrinks |
| Treat monster's clean flag as proof of zero diagnostics | ESLint exit zero includes warning-only results. Installation/build/tool failure is not a clean scan |
| Treat prompt quality requirements as ledger guarantees | Rule quality, nonduplicate findings, and the monster's three-total-remediation-assignments instruction are prompt obligations, not a global runtime quota or proof of successful remediation |
| Assume this example installs deployment security | This example does not provision queue-branch writer restrictions; protected credentials and writer enforcement must be installed independently |

See the [queue specification](https://github.com/github/gh-aw/blob/main/specs/work-queue/priority-and-fairness.md#91-implementation-coverage-and-remaining-requirements)
for coverage and remaining deployment/host requirements, and the
[bounded factory model](https://github.com/github/gh-aw/blob/main/specs/eslint-factory/README.md)
for independently checked scenarios and explicit abstraction limits.
