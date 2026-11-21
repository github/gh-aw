---
title: Work Queue Specification
description: Normative protocol version 4 for the gh-aw Git-backed work queue — ledger format, roles, operations, scheduling, worker lifecycle, maintenance, and conversion from version 3.
sidebar:
  order: 1380
---

# Work Queue Specification

This page is the normative contract of work queue protocol version 4. For configuration and operator commands, see [Work queues](/gh-aw/reference/work-queue/). The bounded formal model and the shared conformance fixtures live in [`specs/work-queue/`](https://github.com/github/gh-aw/tree/main/specs/work-queue).

The key words MUST, MUST NOT, SHOULD, and MAY are used as described in RFC 2119. The reference implementations are `actions/setup/js/work_queue_replay.cjs` (JavaScript) and `pkg/workqueue` (Go); both MUST accept and reject the same ledgers with the same error codes.

## 1. Design principles

1. **One append-only ledger.** Queue state is the replay of `work-queue.jsonl`. No other store is authoritative.
2. **No clocks.** No transition, scheduling decision, or recovery rule reads time. The commit `at` field is informational and replay MUST NOT read it. There is no expiry, backoff, observation age, lease, or deadline.
3. **No installed configuration.** Pool limits and worker routes come from compiled configuration on every invocation. The ledger records the limits a dispatcher used, but replay never enforces them.
4. **Branch write access is the authority.** Publishers identify themselves from their own job context. Replay checks role tables and run binding, not API-verified identity.
5. **Run state drives recovery.** A Claim is released when its launch fails, its worker run ends, or its run is missing, as observed through GitHub Actions run state.

## 2. Storage and publication

The ledger is the file `work-queue.jsonl` at the root of branch `work-queue` (operators MAY select another branch). Each line is one canonical JSON QueueCommit terminated by `\n`. The file MUST be non-empty, newline terminated, contain no empty lines, and be at most `LEDGER_BYTES` long. An absent branch is an empty queue.

A publisher MUST:

1. Read the branch head, replay the ledger, and stop with the recorded commit if the ledger already contains its `request.id`.
2. Compute the operations from the replayed state. An empty operation list commits nothing.
3. Append the commit and write a Git commit whose parent is the observed head, then update the ref without force (compare-and-swap).
4. On a ref conflict or ambiguous write, repeat from step 1, at most 8 attempts in total, without sleeping. After the last attempt, if the re-read ledger contains the request, report it committed; otherwise fail with `cas_exhausted`.

Because operations are recomputed from each re-read state, a publisher that loses the race re-plans against the winner's commit.

### 2.1 Canonical JSON

All hashed and stored JSON uses the canonical encoding:

- Object keys sorted by UTF-8 byte order; no insignificant whitespace; HTML characters and `/` are not escaped.
- Numbers MUST be safe integers (`|n| ≤ 2^53 − 1`, not `-0`), else `noncanonical_number`. Encode other exact quantities as strings.
- Strings MUST be valid Unicode scalar values, else `invalid_unicode`. Duplicate object keys are rejected on parse.
- Nesting depth ≤ 64 and arrays ≤ 16,384 members, else `resource_limit`.

Identity strings (IDs, pool and profile names) are non-empty, at most 256 UTF-8 bytes, and contain no control characters. Run IDs and resource IDs are positive decimal strings.

## 3. Commit line

```json
{"version":4,"id":"q_<sha256>","previous":"q_<sha256>"|null,
 "request":{"id":"request:<sha256>","kind":"<kind>","parameters":{}},
 "actor":{"role":"<role>","repository":"owner/repo","workflow":".github/workflows/x.lock.yml","run_id":"123","run_attempt":1},
 "at":1795099690785,"operations":[]}
```

| Field | Rule |
| --- | --- |
| `version` | MUST be `4`; any other value fails with `unsupported_protocol`. |
| `request.id` | `"request:" + sha256(canonical({actor, kind, parameters}))`. A request ID already in the ledger fails with `request_reused`. |
| `id` | `"q_" + sha256(previous + "\n" + request.id)`, or `"q_" + sha256(request.id)` when `previous` is null. |
| `previous` | The `id` of the preceding line, or `null` on line 1. Otherwise `ledger_invalid`. |
| `actor` | Closed: `role`, `repository`, optional `workflow`, optional `run_id` with `run_attempt` (1–4,096). A worker actor MUST carry `workflow` and its run. |
| `at` | Safe integer milliseconds; informational only. |
| `operations` | 1–256 operations permitted for the request kind. |

Unknown or missing fields anywhere in the line fail with `request_invalid` (or `ledger_invalid` for the commit envelope).

## 4. Roles and actors

Roles are `administrator`, `producer`, `dispatcher`, `worker`, `reconciler`, `projector`, and `observer`. Observers never publish. A publisher builds its actor from its own job: `repository` from the job's repository, `workflow` from the path in `GITHUB_WORKFLOW_REF`, and `run_id`/`run_attempt` from the run. No API call authenticates the actor.

The first commit MAY be published by any non-worker role and sets `state.repository = actor.repository`. Every later commit MUST have the same `actor.repository`, else `actor_unauthorized`. A worker cannot publish the first commit (`actor_unauthorized`).

## 5. Request kinds

| Kind | Parameters (closed) | Permitted operations |
| --- | --- | --- |
| `submit` | `work_ids` (1–256 unique) | `Work` |
| `dispatch_next` | `pool`, `max_claims`, `max_dispatches`, `concurrency`, `per_account_limit`, `profiles` | `Observation`, `Claim` |
| `launch` | `dispatch_id` | `Dispatch`, `Release`, `WorkCancellation` |
| `bind` | `dispatch_id` | `Bound` |
| `finish` | `dispatch_id` | `Completion`, `ClaimCancellation`, `WorkCancellation` |
| `release` | `dispatch_id`, `reason` | `Release`, `WorkCancellation` |
| `cancel_work` | `work_ids`, `reason` | `WorkCancellation` |
| `checkpoint` | `prior_tip` | `Checkpoint` |
| `issue_link` | `operations` (MUST equal the commit's operations) | `IssueLink`, `IssueComment` |
| `migrate` | `work_ids` | `Work` |

An operation outside its request kind fails with `request_invalid`. `dispatch_next` parameters: `max_claims` and `max_dispatches` 1–256, `concurrency` 1–4,096, `per_account_limit` 0–4,096, `profiles` an object of 1–256 entries `{"<profile>": {"workflow": ".github/workflows/<name>.lock.yml"}}`. Replay uses `profiles` only to record each Dispatch's workflow; it does not enforce the limits. `migrate` commits MUST be published by an `administrator` and MUST form a prefix of the ledger.

## 6. Operations

### 6.1 Fields

Every operation is a closed object. Unknown or missing fields fail with `request_invalid`.

| Kind | Required fields | Optional fields |
| --- | --- | --- |
| `Work` | `kind`, `work_id`, `graph_id`, `node_key`, `pool`, `priority`, `fairness_key`, `worker_profile`, `payload`, `depends_on` | `subject`, `backing_issue`, `replacement_of` |
| `Observation` | `kind`, `observation_id`, `resource`, `condition`, `state` | `state_reason`, `merged`, `merge_commit` |
| `Claim` | `kind`, `work_id`, `claim_id`, `dispatch_id`, `handle` | — |
| `Dispatch` | `kind`, `dispatch_id`, `state` | — |
| `Bound` | `kind`, `dispatch_id`, `run_id`, `run_attempt` | — |
| `Completion` | `kind`, `work_id`, `claim_id`, `dispatch_id`, `claim_handle`, `effects` | — |
| `ClaimCancellation` | `kind`, `work_id`, `claim_id`, `reason` | — |
| `WorkCancellation` | `kind`, `work_id`, `reason` | `claim_id` |
| `Release` | `kind`, `dispatch_id`, `reason` | — |
| `Checkpoint` | `kind`, `prior_tip`, `state` | — |
| `IssueLink` | `kind`, `work_id`, `resource` | `claim_id` |
| `IssueComment` | `kind`, `work_id`, `comment_id` | `claim_id` |

Field rules:

- `Work.work_id` MUST equal `sha256(canonical({graph_id, node_key}))`, else `work_identity_invalid`. `priority` is 1–5 (1 is dispatched first). `fairness_key` is at most 128 bytes and MAY be empty. `payload` is a JSON object of at most `PAYLOAD_BYTES` canonical bytes (`work_invalid`, `resource_limit`). `replacement_of` is `{"work_id": …}` naming other Work.
- `depends_on` holds at most `PREDECESSORS` unique edges: `{"kind":"work","work_id":…}`, `{"kind":"issue","resource":…,"condition":"completed"|"closed"}`, or `{"kind":"pull_request","resource":…,"condition":"merged"}` (`dependency_invalid`). The payload plus `EFFECTS_BYTES` per Work edge MUST fit `ASSIGNMENT_BYTES` (`assignment_limit`).
- A resource is `{"kind","host","repository","repository_id","resource_id","number"}` with decimal-string IDs. `host` MUST equal the configured GitHub server (`resource_unauthorized`). `backing_issue` and `IssueLink.resource` MUST be Issues.
- `Observation.state` is `ready`, `waiting`, `failed`, or `unknown`; `condition` matches the resource kind; `state_reason` is `completed`, `not_planned`, or `reopened`. A `ready` pull request observation requires `merged: true` and `merge_commit`; a `ready` `completed` Issue observation requires `state_reason: "completed"` (`observation_invalid`).
- `Dispatch.state` MUST be `"started"`. `Bound.run_id` is a decimal string and `run_attempt` 1–4,096.
- `Completion.effects` is an array of objects of at most `EFFECTS_BYTES` canonical bytes.

### 6.2 Reasons

Reasons are closed enums; any other value fails with `reason_invalid`.

| Operation | Reasons |
| --- | --- |
| `ClaimCancellation` | `worker_failed`, `worker_terminated`, `launch_failed`, `run_terminal`, `run_missing`, `operator` |
| `WorkCancellation` | `attempts_exhausted`, `operator`, `related_issue_closed`, `worker_contract_obsolete` |
| `Release` | `launch_failed`, `worker_terminated`, `run_terminal`, `run_missing`, `operator` |

### 6.3 Roles

| Operation | Roles |
| --- | --- |
| `Work` | `producer`, `dispatcher`, `administrator`, `worker` |
| `Observation`, `Claim`, `Dispatch` | `dispatcher`, `administrator` |
| `Bound`, `Completion`, `ClaimCancellation` | `worker` |
| `WorkCancellation` | `producer`, `administrator`, `reconciler`; `worker` only in `finish`; `dispatcher` only in `launch` |
| `Release` | `reconciler`, `administrator`; `dispatcher` only with reason `launch_failed` |
| `Checkpoint` | `administrator`, `reconciler` |
| `IssueLink`, `IssueComment` | `projector` |

A role outside this table fails with `unauthorized_operation`.

## 7. Projection

Replay produces:

| Field | Content |
| --- | --- |
| `repository`, `tip`, `commits` | Queue repository, last commit ID, and commit ordinal (including history folded into a checkpoint) |
| `works` | Work definition plus `state`, `position {commit, operation}`, `attempts`, and when applicable `claim_id`, `completion_id`, `effects`, `cancellation_reason`, `cancellation_commit_id`, `backing_issue`, `issue_comment` |
| `claims` | `claim_id`, `work_id`, `dispatch_id`, `handle`, `state` (`open`, `completed`, `cancelled`), `commit_id`, `request_id`, and when applicable `terminal_commit_id`, `cancellation_reason`, `effects`, `issue_comment` |
| `dispatches` | `dispatch_id`, `request_id`, `commit_id`, `pool`, `worker_profile`, `workflow`, `state` (`reserved`, `started`, `bound`, `closed`), `run` (`{run_id, run_attempt}` or null), `claims` |
| `observations` | Latest Observation per gate key `canonical({host, repository_id, resource_id, kind, condition})` |
| `requests`, `work_creators` | Committed request IDs and the actor that created each Work |

Work states are `available`, `claimed`, `completed`, and `cancelled`. `position` is the FIFO key: the commit ordinal and operation index where the Work was admitted.

## 8. Transitions

Operations apply in order within a commit; a commit applies atomically or fails as a whole.

| Operation | Precondition (error code) | Effect |
| --- | --- | --- |
| `Work` | If `work_id` exists: no-op. Otherwise graph admission: Work predecessors exist in the same graph and pool or the same commit (`dependency_invalid`), no cycle (`dependency_cycle`), one pool per graph and unique node keys per commit (`graph_invalid`), one Work per backing Issue (`issue_binding_conflict`), at most `GRAPH_NODES` Work and gate vertices per graph and `MAX_PENDING_WORK` pending Work per pool (`resource_limit`). | Work `available`, `attempts` 0. |
| `Observation` | — | Replace the observation of its gate key. |
| `Claim` | Work exists (`work_missing`), is `available` and unowned (`ownership_conflict`); `claim_id` unused (`claim_conflict`); Work profile in request `profiles` (`request_invalid`); an existing `dispatch_id` belongs to the same commit, pool, and profile (`dispatch_state_conflict`) with a unique handle (`claim_conflict`). Replay does not re-plan the selection. | Claim `open`; Work `claimed` with `claim_id`; Dispatch created `reserved` with `workflow = profiles[worker_profile].workflow`, or extended. |
| `Dispatch` | Dispatch exists (`dispatch_missing`) and is `reserved` (`dispatch_state_conflict`). | Dispatch `started`. |
| `Bound` | Dispatch is `reserved`, `started`, or `bound` with an open Claim (`dispatch_state_conflict`); `actor.workflow = dispatch.workflow`, `actor.run_id = run_id`, `actor.run_attempt = run_attempt` (`run_binding_conflict`). If already bound: the same run and attempt is a no-op, the same run with a higher attempt rebinds, anything else fails (`run_binding_conflict`). | Dispatch `bound`, `run` set. |
| `Completion` | Claim exists (`claim_missing`) and is `open` (`claim_ineffective`); `work_id`, `dispatch_id`, `claim_handle` match the Claim and the Dispatch is bound to the actor's workflow, run, and attempt (`claim_scope_invalid`). | Claim and Work `completed`; `effects` stored on both; `work.completion_id` = commit ID; Dispatch `closed` when no Claim is open. |
| `ClaimCancellation` | Claim names the Work (`claim_scope_invalid`). Already `cancelled`: no-op. Otherwise `open` (`claim_ineffective`) and the Work's current Claim (`ownership_conflict`); a worker actor must be the bound run (`claim_scope_invalid`). | Claim `cancelled` with reason; Work `available`, `attempts + 1`, `claim_id` removed; Dispatch `closed` when no Claim is open. |
| `WorkCancellation` | Work exists (`work_missing`) and is not `completed` (`work_terminal`). Already `cancelled`: no-op with the same reason, else `work_terminal`. `claimed` Work fails (`ownership_conflict`). A `claim_id` MUST name a cancelled Claim of this Work (`claim_scope_invalid`). | Work `cancelled`, `cancellation_reason`, `cancellation_commit_id`. |
| `Release` | Dispatch exists and is not `closed` (`dispatch_state_conflict`). | Every open Claim of the Dispatch is cancelled with the Release reason (as `ClaimCancellation`); Dispatch `closed`. |
| `Checkpoint` | Only as the sole operation of line 1 (`checkpoint_invalid`). | See [section 14](#14-checkpoint). |
| `IssueLink` | Work exists; optional Claim belongs to it (`claim_scope_invalid`). The Work has no other backing Issue and the Issue backs no other Work (`issue_binding_conflict`); the same binding is a no-op. | `work.backing_issue` set. |
| `IssueComment` | Work (and optional Claim) exists; an existing different `issue_comment` fails (`issue_binding_conflict`); the same value is a no-op. | `issue_comment` set on the Claim, or on the Work without `claim_id`. |

### 8.1 Attempts rule

Replay never cancels Work on its own. Whenever a publisher cancels a Claim (`finish` with a failed outcome, `release`, or a failed `launch`), it MUST append `WorkCancellation{reason: "attempts_exhausted", claim_id}` to the same commit, after the cancelling operation, for each affected Work whose `attempts + 1 >= max_attempts`. `max_attempts` comes from the pool configuration at publish time (default 3). Every cancelled Claim therefore consumes one attempt, and Work settles after at most `max_attempts` Claims.

## 9. Configuration

The compiler emits `GH_AW_WORK_QUEUE_POLICY` (chunked when large) to every queue job:

```json
{"version":4,"pending_limit":30,"issues":{"label":"work","title_prefix":"[work queue]"},
 "pools":{"default":{"concurrency":3,"per_account_limit":0,"max_attempts":3,"default_profile":"worker",
   "profiles":{"worker":{"workflow":".github/workflows/worker.lock.yml","effect_scope":"owner/repo"}}}}}
```

The object is closed. `pending_limit` 1–4,096; per pool `concurrency` 1–4,096, `per_account_limit` 0–4,096 (0 is unlimited), `max_attempts` 1–16, 1–256 profiles, and a `default_profile` naming one of them. Each profile `workflow` matches `.github/workflows/<name>.lock.yml` and `effect_scope` is `owner/repo`. `issues` is `false` or `{label, title_prefix, orphaned_after_days?}`. Invalid environments fail with `policy_invalid`. The environment is derived from `.github/workflows/aw.json` `work_queue` and the workflows declaring `tools.work-queue.worker: true`; see [repository scheduling settings](/gh-aw/reference/work-queue/#repository-scheduling-settings).

## 10. Submission

A producer, dispatcher, administrator, or worker publishes `submit` with `work_ids` and the new `Work` operations. Existing Work IDs are omitted from the operations; when every ID exists, nothing is committed. Before publishing, the publisher MUST check that the pool's `available` plus `claimed` Work, plus the new Work, does not exceed `pending_limit` (`work_queue_pending_limit`).

Staged agent submissions default `graph_id` to the canonical payload digest, `node_key` to `root`, `pool` to `default`, `priority` to 3, `fairness_key` to `""`, and `worker_profile` to the pool's `default_profile`; a profile not configured for the pool fails with `work_queue_profile_not_approved`. A worker MAY submit child Work only while its bound Claim is open; omitted graph, pool, priority, fairness key, and same-pool profile are inherited from the Claim's Work. Issue and pull request references without numeric IDs are resolved before publication (`external_admission_unverified` on failure).

## 11. Scheduling and launch

### 11.1 Eligibility and order

For `dispatch_next` with parameters `P`, Work is eligible when it is in pool `P.pool`, `available`, routed to a profile in `P.profiles`, and every dependency is satisfied: a Work predecessor is `completed`, and an Issue or pull request edge has an observation of its gate key with state `ready`. Cancelled or missing predecessors never satisfy an edge.

Eligible Work is ordered by `priority` ascending, then `position` (commit ordinal, then operation index) ascending. The planner walks that order and, for each Work:

1. stops with `capacity_blocked` when the pool's non-closed Dispatches reach `P.concurrency`;
2. stops with `claim_budget_reached` when `min(P.max_claims, P.max_dispatches)` Claims are planned;
3. skips the Work (`account_capacity_blocked`) when `P.per_account_limit > 0` and the pool's open Claims with the same `fairness_key` reach the limit;
4. otherwise plans one Claim in its own Dispatch.

Planning is pure. Identities are derived from `seed = sha256(request.id + "\n" + work_id)`: `claim_id = "c_" + seed + "_1"`, `dispatch_id = "d_" + seed + "_1"`, `handle = "h1"`. Plan reasons are `no_work`, `no_eligible_work`, `capacity_blocked`, `account_capacity_blocked`, `claim_budget_reached`, and `prefix_complete`. The shared fixture `specs/work-queue/fixtures/selection.json` fixes the expected decisions.

### 11.2 Dispatcher

The dispatcher copies `concurrency`, `per_account_limit`, and each profile's `workflow` from the configuration into the `dispatch_next` parameters. Before planning it MAY refresh observations of unresolved external edges of the pool's available Work whose Work predecessors completed, at most `min(128, 256 − max_claims)` edges; read failures leave edges unsatisfied. The commit carries the changed `Observation` operations followed by the planned `Claim` operations, re-planned on every compare-and-swap retry.

### 11.3 Launch

For each new `reserved` Dispatch the dispatcher sends exactly one `workflow_dispatch` request for `dispatch.workflow` at its configured `target-ref` with input:

```text
work_queue_assignment = {"version":4,"dispatch_id":"d_…"}
```

It then publishes a `launch` commit: `[Dispatch{state:"started"}]` after a successful request, or `[Release{reason:"launch_failed"}]` plus the attempts rule after any failure. If the Dispatch is no longer `reserved` when the launch commit is planned, nothing is committed. The dispatcher MUST NOT discover runs, poll, or resend. If the launch commit cannot be published, the Dispatch stays `reserved` and maintenance resolves it.

### 11.4 Control receipt

Trusted controls set the step output `work_queue_requests`:

```json
{"version":4,"success":true,"status":"committed",
 "receipts":[{"intent_id":"…","request_id":"request:…","commit_id":"q_…","status":"committed","dispatches":1,
   "launches":[{"dispatch_id":"d_…","state":"started"}]}],
 "remaining_dispatches":0,"summary":"…"}
```

A receipt is `committed` or `blocked` (with `reason`). A launch is `started` or `launch_failed` (with `reason`). The controls step MUST fail when any receipt is `blocked` or a dispatcher staged no intents, and MUST NOT fail for `launch_failed`, because that Work is back in the pool.

## 12. Worker lifecycle

### 12.1 Bind and snapshot

The worker's activation step reads the ledger as `worker` (a version 3 head fails with `unsupported_protocol`) and finds `dispatch_id` (`work_queue_assignment_unknown` if absent). If the Dispatch has an open Claim, it publishes `bind` with `[Bound{dispatch_id, run_id, run_attempt}]` for its own run; when the Dispatch is already bound to this attempt it publishes nothing. A re-run attempt rebinds the Dispatch.

If no Claim is open, the step sets output `work_queue_assignment_settled=true`; the agent and safe-output jobs MUST then be skipped.

The snapshot `/tmp/gh-aw/work-queue.snapshot.json` is:

```json
{"version":4,"sha":"<git sha>","transactionLog":"<jsonl>","origin":{"role":"worker"},"role":"worker",
 "worker":{"dispatch_id":"d_…","claims":[{"handle":"h1","claim_id":"c_…","work_id":"…","work":{},
   "result_refs":[{"work_id":"…","result_commit_id":"q_…","descriptor":[]}]}]}}
```

`work` is the Work payload. `result_refs` list the Work predecessors, with the commit that completed each and its recorded `effects` as `descriptor`. `worker` is `null` for other roles and for a settled worker.

### 12.2 Finish intents

The agent stages one line per Claim in `work-queue.finish.jsonl`:

```json
{"version":4,"claim_handle":"h1","outcome":"completed"}
```

`outcome` is `completed` or `failed`. A missing intent means `failed`; conflicting outcomes for one handle are invalid, and an invalid file fails every Claim.

### 12.3 Effects

A Claim-scoped safe output MAY be applied only when its Claim is `open`, its Dispatch is `bound` to the current run and attempt, the Claim's finish intent is `completed`, and every target repository equals the profile's `effect_scope`. Otherwise it MUST be rejected (`E004`, `work_queue_effect_scope_denied`) or suppressed. Each applied effect appends `{"claim_handle","kind","resource"}` to `/tmp/gh-aw/work-queue.effects.jsonl`.

### 12.4 Finish

After safe outputs are processed, the worker publishes one `finish` commit for its Dispatch. For each open Claim:

- `completed`: `Completion` with that handle's effects, or `[]` with a warning when they exceed `EFFECTS_BYTES`;
- otherwise: `ClaimCancellation{reason:"worker_failed"}` plus the attempts rule.

The step output `work_queue_outcome` is `completed` when every Claim of the Dispatch is completed, else `failed`. Effects are applied before the commit; if the job stops in between, the Claim is later released and the next attempt MAY repeat the effects (at-least-once).

### 12.5 Termination

The worker's conclusion job reads the ledger as `reconciler`. If the run's Dispatch is not `closed`, has an open Claim, and is unbound or bound to this run, it publishes `release` with `[Release{reason:"worker_terminated"}]` plus the attempts rule.

## 13. Maintenance

Scheduled maintenance (role `reconciler`) MUST:

1. Convert a version 3 head ([section 15](#15-conversion-from-version-3)).
2. For each non-closed Dispatch with an open Claim:
   - `bound`: read the bound run. `status: "completed"` → `Release{run_terminal}`; HTTP 404 → `Release{run_missing}`; otherwise leave it.
   - `reserved` or `started`: list the worker workflow's `workflow_dispatch` runs with status `queued`, `in_progress`, `waiting`, `pending`, and `requested` (at most two pages of 100 each). If any run's display title contains the `dispatch_id`, leave it; otherwise `Release{run_missing}`. The title is a correlation hint only and never authorizes anything.
   Each release is one `release` commit with the attempts rule. A read error skips that Dispatch with a warning.
3. Write a checkpoint when the ledger has more than `CHECKPOINT_AFTER_LINES` lines.
4. Run the Issue projection.

Only conversion and checkpoint failures fail maintenance. A run released while still queued later finds no open Claim at bind and exits as settled.

## 14. Checkpoint

A checkpoint replaces the file content with one root line (`previous: null`) whose request is `checkpoint {prior_tip}` and whose only operation is `Checkpoint{prior_tip, state}`. The Git commit's parent is the previous head, so the folded history stays reachable. `state` is the canonical projection: `repository`, `tip` (equal to `prior_tip`), `commits`, `works`, `claims`, `dispatches`, `observations`, `requests` (sorted), and `work_creators`.

Restoring a checkpoint MUST validate every record and cross-reference (`checkpoint_invalid`): open Claims and `claimed` Work agree, completed Work carries its completion and effects, cancelled Work carries its reason, only bound or closed Dispatches carry a run, non-closed Dispatches have an open Claim, and every Work has a creator. Replay continues with the following lines; the checkpoint's own request ID joins `requests` and `commits` increases by one.

## 15. Conversion from version 3

A job with role `dispatcher`, `producer`, `administrator`, or `reconciler` that reads a version 3 head MUST convert it before any other publication; other roles fail with `unsupported_protocol`. Only the converter reads version 3 fields.

1. If line 1 is a version 3 `Checkpoint`, start from its Work map; otherwise warn `v3 ledger without checkpoint; pending Work not carried` and produce an empty log.
2. Apply later version 3 lines in order: `Work` adds Work, `Claim` marks it claimed, `ClaimCancellation` returns claimed Work to available, `Completion` completes it (or returns it when the version 3 Completion kept the Work open), and `WorkCancellation` cancels it.
3. Keep `available` Work, sorted by version 3 position. Skip, with a warning each, Work whose pool or worker profile is not configured, Work with a Work predecessor that is not carried, and Work that is invalid as version 4 `Work`.
4. Map each to a version 4 `Work` operation with `work_id`, `graph_id`, `node_key`, `pool`, `priority` (the version 3 effective priority when present), `fairness_key`, `worker_profile`, `payload`, `depends_on`, and optional `subject`, `backing_issue`, and `replacement_of`.
5. Write `migrate` commits by an `administrator` actor, at most `OPERATIONS_PER_COMMIT` operations each and the first with `previous: null`, as the new file content. The Git parent is the version 3 head. Retry on compare-and-swap conflicts like any publication.

Claims, Dispatches, completed and cancelled Work, and request history of version 3 are not carried.

## 16. Issue projection

When `issues` is configured, a `projector` publishes best-effort `issue_link` commits, each carrying the operations of one Issue mutation. Per run it handles at most 50 Work of the configured pools in FIFO order:

- `available` or `claimed` Work without `backing_issue`: create an Issue titled with `title_prefix` and labelled `label`, or adopt an open labelled Issue carrying the Work's marker, then publish `IssueLink`.
- A claimed Work whose open Claim has no `issue_comment` and whose Dispatch is `bound`: comment and publish `IssueComment{claim_id}`.
- `completed` or `cancelled` Work without `issue_comment`: comment, close the Issue if open, and publish `IssueComment`.
- An open labelled Issue whose marked Work is absent from the ledger: comment and close it.

Comments carry `<!-- gh-aw-work-queue:<work_id>:<state> -->` markers and are not repeated when the marker is among recent comments. Projection failures MUST NOT fail the calling job. Closing a backing Issue of `available` Work publishes `cancel_work` with `[WorkCancellation{reason:"related_issue_closed"}]`; other Work states are unchanged.

## 17. Limits

| Constant | Value | Bound |
| --- | --- | --- |
| `PAYLOAD_BYTES` | 16384 | Canonical bytes of one Work payload |
| `OPERATIONS_PER_COMMIT` | 256 | Operations per commit |
| `GRAPH_NODES` | 4096 | Work and external gate vertices per graph |
| `PREDECESSORS` | 64 | Dependencies per Work |
| `ASSIGNMENT_BYTES` | 49152 | Payload plus predecessor effects per Claim |
| `EFFECTS_BYTES` | 4096 | Canonical bytes of one Completion's effects |
| `LEDGER_BYTES` | 67108864 | Ledger file size |
| `CHECKPOINT_AFTER_LINES` | 512 | Lines before maintenance checkpoints |
| `MAX_PENDING_WORK` | 4096 | Pending Work per pool in replay; upper bound of `pending_limit` |

These are protocol constants, not configuration. Publication makes at most 8 compare-and-swap attempts.

## 18. Error codes

| Code | Meaning |
| --- | --- |
| `unsupported_protocol` | A line or head is not version 4 |
| `ledger_invalid` | Malformed envelope, chain, identity, or log framing |
| `request_invalid` | Unknown field, kind, or operation outside its request kind |
| `request_reused` | Request ID already committed |
| `actor_unauthorized` | Worker genesis or foreign repository |
| `unauthorized_operation` | Role not permitted for the operation |
| `reason_invalid` | Reason outside its enum |
| `work_identity_invalid`, `work_invalid`, `dependency_invalid`, `dependency_cycle`, `graph_invalid` | Work admission failures |
| `resource_invalid`, `resource_unauthorized`, `observation_invalid` | Resource or observation shape |
| `resource_limit`, `assignment_limit` | Size bounds |
| `work_missing`, `claim_missing`, `dispatch_missing` | Unknown referenced record |
| `ownership_conflict`, `claim_conflict` | Work owned by another Claim or duplicate Claim identity |
| `dispatch_state_conflict` | Dispatch in the wrong state |
| `run_binding_conflict` | Bind from another workflow, run, or older attempt |
| `claim_ineffective`, `claim_scope_invalid` | Settling a non-open Claim or a Claim outside the bound run |
| `work_terminal` | Cancelling completed Work or recancelling with another reason |
| `issue_binding_conflict` | Changing an immutable Issue binding |
| `checkpoint_invalid` | Misplaced or inconsistent checkpoint |
| `noncanonical_number`, `invalid_unicode`, `codec_invalid` | Canonical JSON violations |
| `cas_exhausted` | Head changed on every publication attempt |
| `policy_invalid` | Invalid compiled configuration |

## 19. Conformance

The JavaScript and Go implementations MUST produce identical results for the shared fixtures in [`specs/work-queue/fixtures`](https://github.com/github/gh-aw/tree/main/specs/work-queue/fixtures):

| Fixture | Contract |
| --- | --- |
| `canonical.json`, `canonical-typed.json` | Canonical JSON encoding and typed-number rejection |
| `identity-validation.json` | Identity byte and decimal-string bounds |
| `reason-validation.json` | Closed reason enums per operation |
| `selection.json` | Priority-then-FIFO scheduling decisions |
| `canonical-prefix.json` | Every ledger prefix: projection summary, rejection code, and next plan |

`actions/setup/js/work_queue_conformance_fixtures.json` adds codec and replay cases consumed by both test suites.

## 20. Formal model

[`WorkQueueV4.tla`](https://github.com/github/gh-aw/blob/main/specs/work-queue/WorkQueueV4.tla) models submission, planning, launch, bind, completion, failure, termination, maintenance release, and re-runs without clocks. TLC checks, within the bounds in its [README](https://github.com/github/gh-aw/blob/main/specs/work-queue/README.md), that each Work has at most one open Claim, a completed Claim was completed by the run attempt its Dispatch is bound to, non-closed Dispatches never exceed `concurrency`, attempts never decrease or exceed `max_attempts`, terminal Work never changes, and every submitted Work eventually completes or is cancelled under fair scheduling, maintenance, and run progress.

## 21. Guarantees and non-guarantees

The protocol guarantees that replay is deterministic, that a Work has at most one open Claim, that only the run bound to a Dispatch can complete its Claims, that pool concurrency is never exceeded by planned Dispatches, and that every Work settles within `max_attempts` Claims once scheduling and maintenance run.

It does not guarantee exactly-once effects (effects are at-least-once across attempts), that a launched run starts, that observations reflect the current state of an Issue or pull request, or that repository writers do not rewrite the queue branch. Fairness is limited to priority order, FIFO order within a priority, and `per_account_limit`.
