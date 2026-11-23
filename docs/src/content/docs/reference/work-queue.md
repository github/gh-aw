---
title: Work queues
description: Native work-queue configuration, Claim-scoped tools, lifecycle rules, and operator commands.
---

`tools.work-queue` provides a native, Git-backed queue with priority-then-FIFO scheduling and task dependencies. The protocol version 4 transaction log, `work-queue.jsonl` on the `work-queue` branch, is authoritative. Issues can mirror tasks but cannot store the queue. There is no `storage` option.

For architecture, a [minimal deployment example](/gh-aw/patterns/workqueue-ops/#deploy-a-native-queue), and lightweight alternatives, see [WorkQueueOps](/gh-aw/patterns/workqueue-ops/). The [protocol specification](/gh-aw/specs/work-queue-specification/) and the [formal model](https://github.com/github/gh-aw/blob/main/specs/work-queue/README.md) cover implementation contracts and verification.

## Queue model

The queue has no installed configuration. Every workflow that uses the queue receives the compiled pool configuration in `GH_AW_WORK_QUEUE_POLICY`, so a change to `aw.json` takes effect on the next run after recompilation. The first commit of any non-worker role creates the queue; an absent branch is an empty queue.

| Record | Meaning |
| --- | --- |
| Work | Immutable task definition with pool, priority, fairness key, worker profile, payload, and dependencies. |
| Claim | One attempt at one Work, identified inside its Dispatch by a `claim_handle`. |
| Dispatch | One worker launch. States: `reserved` → `started` → `bound` → `closed`. |
| Completion | The worker finished its Claim; records the effects the run applied. |
| Release | Closes a Dispatch and returns its open Claims' Work to the pool. |

Work states are `available`, `claimed`, `completed`, and `cancelled`. Every cancelled Claim adds one to the Work's `attempts`; when `attempts` reaches the pool's `max_attempts`, the same commit cancels the Work with reason `attempts_exhausted`. No decision reads a clock: there is no expiry, backoff, observation age, or deadline.

Write access to the queue branch is the only authority. Each job writes as the role compiled into it (producer, dispatcher, worker, reconciler, or projector) and identifies itself with its own repository, workflow path, and run. Replay checks which role may publish each operation and that a worker settles only the Dispatch bound to its own run attempt. Agents receive a read-only snapshot, never queue-write credentials.

> [!CAUTION]
> Repository writers can alter or delete the queue branch. A deleted branch is treated as a fresh queue, losing Work and request deduplication. Checkpoints do not recover a deleted branch.

## Repository scheduling settings

Configure the optional `work_queue` object in `.github/workflows/aw.json`. A missing file, missing section, or `{}` uses defaults. Unknown properties, nulls, invalid types, and out-of-range values are compile errors.

| Field | Default | Accepted values |
| --- | --- | --- |
| `concurrency` | 16 | Integer 1–4,096; maximum non-closed Dispatches per pool |
| `pending_limit` | 4,096 | Integer 1–4,096; maximum `available` plus `claimed` Work per pool |
| `retry.max_attempts` | 3 | Integer 1–16; Claims per Work before `attempts_exhausted` |
| `pools` | One `default` pool | Up to 64 named objects overriding `concurrency`, `retry.max_attempts`, and `per_account_limit` |
| `pools.<name>.per_account_limit` | Unlimited | Integer 1–4,096; open Claims per `fairness_key` in the pool |
| `issues` | Disabled | Boolean or settings object; see [Backing Issues](#backing-issues) |

```json title=".github/workflows/aw.json"
{
  "work_queue": {
    "concurrency": 3,
    "pending_limit": 30,
    "retry": { "max_attempts": 3 }
  }
}
```

A `default` pool always exists. Every pool can route to every workflow declared with `tools.work-queue.worker: true`; the worker profile name is the workflow ID, and the pool's default profile is the first worker ID in sorted order. Each profile's effect scope is the current repository.

## Workflow roles and tools

```yaml title="Producer or dispatcher"
tools:
  work-queue: true
```

```yaml title="Worker"
tools:
  work-queue:
    require-assignment: true
    worker: true
```

Dispatchers also declare the worker workflows in `safe-outputs.dispatch-workflow.workflows` and bound launches per run with `max`. The [deployment example](/gh-aw/patterns/workqueue-ops/#deploy-a-native-queue) combines these declarations. Standalone `tools.repo-memory` persistence is unsupported in native queue workers; memory-backed queues are separate [alternative patterns](/gh-aw/patterns/workqueue-ops/).

| Role | Agent tools |
| --- | --- |
| Observer | `work_queue_read` only. |
| Dispatcher | `work_queue_read`, `work_queue_submit`, `work_queue_dispatch_next`. |
| Worker | `work_queue_read`, `work_queue_submit` (child Work), `work_queue_claim_finish`. |

| MCP tool | Input | Purpose |
| --- | --- | --- |
| `work_queue_read` | `work`, `pool`, `limit`, `offset`, `sort` | Read the activation snapshot: counts, open Dispatches, Work rows with `attempts` and readiness, and a non-authoritative prediction of the next Work. |
| `work_queue_submit` | `nodes` (1–256), `claim_handle` | Stage Work nodes. Omitted `graph_id`/`node_key` default to the payload hash and `root`; a worker's children inherit its Work's graph, pool, priority, fairness key, and profile. |
| `work_queue_dispatch_next` | `pool`, `max_claims`, `max_dispatches` | Stage a request for the scheduler to claim and launch the pool's next eligible Work. It accepts no Work selector. |
| `work_queue_claim_finish` | `claim_handle`, `outcome` | Stage `completed` (default) or `failed` for one Claim. |

When listed in `<mcp-clis>`, invoke tools as `work-queue work_queue_read '{}'`. Staging writes an intent file; trusted steps after the agent publish it. These are agent tools, not `gh aw work-queue` operator subcommands.

### Dispatch and launch

The trusted control step publishes one `dispatch_next` commit per staged request. The scheduler takes eligible Work of the pool in priority order (1 first), then submission order, while the pool has fewer non-closed Dispatches than `concurrency`, the Work's `fairness_key` is under `per_account_limit`, and fewer than `min(max_claims, max_dispatches)` Claims were taken. Each Dispatch carries one Claim. Work is eligible when every Work predecessor is `completed` and every Issue or pull request dependency has a `ready` observation; the dispatcher refreshes those observations in the same commit.

For each new Dispatch the step sends one `workflow_dispatch` of the worker at the dispatcher's `target-ref` with the input `work_queue_assignment: {"version":4,"dispatch_id":"d_…"}`. A successful request is recorded as `Dispatch{started}`. A failed request releases the Dispatch with reason `launch_failed`, returning its Work to the pool; it does not fail the step.

### Claim-scoped effects

- The worker's activation job binds the Dispatch to its run attempt. If the Dispatch has no open Claim (for example, maintenance already released it), the run writes a settled snapshot and skips the agent and safe-output jobs.
- Every safe output belongs to one Claim. A one-Claim assignment may omit `claim_handle`.
- A safe output is applied only when its Claim is open, its Dispatch is bound to this run attempt, the Claim's finish outcome is `completed`, and every target repository equals the profile's effect scope. Otherwise the handler rejects it with `E004`.
- Each applied effect is appended to `/tmp/gh-aw/work-queue.effects.jsonl`. The **Finish work queue claim** step, after **Process Safe Outputs**, publishes one `finish` commit: a Completion with those effects (dropped with a warning above 4,096 bytes) for `completed` Claims, and a `worker_failed` Claim cancellation for `failed` or missing outcomes.
- Effects are applied before the `finish` commit. If the job stops between the two, maintenance releases the Dispatch and the next attempt can repeat the effects. Write idempotent workers.
- A Work predecessor is satisfied by its Completion. Its recorded effects are passed to successors as `result_refs[].descriptor`.

Beyond built-in safe outputs, declare `safe-outputs.claim-adapters` for Claim-scoped effects with custom handlers; adapters use the same authorization.

### Claim-scoped memory

`tools.work-queue.memory` generates a structured output tool and protected Git-tree publication adapter for a worker. It is not standalone `tools.repo-memory`. Configuration fields are `name` (default `persist_work_queue_memory`), `path`, `target-repo`, `base-revision`, `branch-prefix`, `schema`, and `max-bytes` (default and maximum `262144`, including the final newline).

Each call supplies the Claim's `claim_handle` and a `memory` object matching the schema. Preparation validates the selector and any `memory.work_id` without repository credentials.

Schemas are limited to 16 KiB and 16 nested levels. Supported keywords are `type`, `description`, `properties`, `required`, boolean `additionalProperties`, `items`, scalar `enum`, `minimum`, `maximum`, `minLength`, `maxLength`, `minItems`, and `maxItems`. Each nested schema declares one type; references, regexes, combinators, and executable validators are unsupported.

Data is limited to 32 nested levels and 16,384 members per array. Numbers must be safe integers; encode fractional or other exact quantities as strings. See the [Linter Factory example](/gh-aw/gallery/linter-factory/#refiner-memory) for its use.

## Recovery

Recovery is driven by GitHub run state, never by elapsed time:

| Situation | Who acts | Ledger result |
| --- | --- | --- |
| `workflow_dispatch` request fails | Dispatcher | `Release{launch_failed}` |
| Worker run ends with an open Claim | Worker conclusion job **Terminate work queue worker** | `Release{worker_terminated}` |
| Bound or launched run completed or was deleted | Scheduled maintenance | `Release{run_terminal}` or `Release{run_missing}` |
| Launch never recorded | Scheduled maintenance | After 6 hours, on a complete listing of worker runs created since the reservation: `Release{run_missing}` when no run has the `dispatch_id` in its title, `Release{run_terminal}` when that run completed. An incomplete listing leaves the Dispatch open. |
| Operator decision | `gh aw work-queue release` or `cancel-work` | `Release{operator}` or `WorkCancellation{operator}` |

Maintenance runs in `agentics-maintenance.yml`. It also converts a protocol version 3 ledger, writes a checkpoint when the log exceeds 512 lines, and runs the Issue projection. A run that starts after its Dispatch was released finds no open Claim and exits without effects.

### Version 3 ledgers

The first dispatcher, producer, administrator, or maintenance job that reads a version 3 head converts it. The converter keeps `available` Work whose pool and worker profile are still configured, in their original order, and writes it as administrator `migrate` commits into a new log whose Git parent is the version 3 head. Claimed, completed, and cancelled version 3 Work is not carried; the old log stays in Git history. Workers, observers, and projectors fail with `unsupported_protocol` until conversion has run.

## Backing Issues

```json title=".github/workflows/aw.json"
{
  "work_queue": {
    "issues": { "label": "work", "title_prefix": "[work queue]" }
  }
}
```

| Setting | Behavior |
| --- | --- |
| Absent or `false` | Disable projection. |
| `true` or `{}` | Use label `work` and title prefix `[work queue]`. |
| `label` | Nonblank literal, at most 33 UTF-8 bytes. |
| `title_prefix` | Nonblank literal, at most 80 UTF-8 bytes. |
| `orphaned_after_days` | Accepted for compatibility (integer 1–3,650); the projection does not use it. |

The projection is best-effort and never fails a job. It runs after dispatcher controls, worker finish and termination, and maintenance, and handles at most 50 Work per run, oldest first:

- `available` or `claimed` Work without a backing Issue gets a new labelled Issue (or adopts a labelled Issue carrying its marker), bound by an immutable `IssueLink`.
- A Claim whose Dispatch is bound gets one comment on the Work's Issue.
- `completed` or `cancelled` Work gets a final comment and its Issue is closed.
- A labelled open Issue whose marked Work is absent from the ledger is commented on and closed.

Comments carry `<!-- gh-aw-work-queue:… -->` markers so a repeated projection does not duplicate them. Submitted Work can also name an existing Issue in `backing_issue`:

```json
{
  "kind": "issue",
  "host": "github.com",
  "repository": "owner/repo",
  "repository_id": "123",
  "resource_id": "456",
  "number": "7"
}
```

Resource hosts must match `GITHUB_SERVER_URL` (default `github.com`); one Issue backs at most one Work. The compiler generates `agentic-workqueue.yml` for Issue events: closing a backing Issue cancels its Work with reason `related_issue_closed` when the Work is `available`, and leaves claimed or terminal Work unchanged.

## Operator commands

```bash
gh aw work-queue --repo OWNER/REPO [--branch QUEUE_BRANCH] [--json] COMMAND
```

`work` is an alias of `work-queue`. Repository selection is `--repo`, then `GH_REPO`, then local `origin`, then `gh repo view`. Offline `replay --file` needs no repository. `--branch` selects a separate queue. Commands that write publish as the repository `administrator` role and need push access to the queue branch.

| Command | Purpose |
| --- | --- |
| `replay` (alias `read`) | Replay the live ledger and list Work states; JSON output is the full projection. |
| `stats` | Count Work, Claims, and Dispatches per pool. |
| `state [--graph ID] [--pool POOL] [--state STATE] [--search TEXT] [--offset N] [--limit N]` | Metadata-only ASCII Work/Claim forest, at most 256 rows and 64 KiB per page (default 80 rows). |
| `inspect --work-id ID` or `--claim-id ID` | Metadata, dependencies, Dispatch, bound run, and effects of one Work or Claim. |
| `tui` (alias `interactive`) | Keyboard-driven Work/Claim browser with checked operator actions. |
| `submit-work --file PATH --worker-profile WORKER` | Submit one Work item. Optional `--graph-id` (default: payload hash), `--node-key` (default `root`), `--pool` (default `default`), `--priority` 1–5 (default 3), `--fairness-key`. |
| `submit-graph --file PATH` | Atomically submit a JSON array of 1–256 complete Work operations, including Issue and pull request dependencies. |
| `cancel-work --work-id ID[,ID...] [--reason REASON]` | Cancel `available` Work (default reason `operator`). Claimed Work must be released first. |
| `release --dispatch-id ID [--reason REASON]` | Release a non-closed Dispatch (default reason `operator`); its Work returns to the pool or is cancelled with `attempts_exhausted`. |
| `clear [--dry-run]` | Release every open Dispatch and cancel all `available` Work; see below. |
| `compact [--min-lines N]` | Replace the log with one checkpoint commit when it has more than N lines (default 1). |
| `dispatch DISPATCHER [--dry-run] [--wait]` | Run a dispatcher workflow on the default branch; `--wait` waits for the dispatcher only. |

Resubmitting an existing Work ID is a no-op, so retrying a submission is safe. `release` and `cancel-work` do not stop running workflow runs: a late worker finds its Claims closed and cannot complete them.

### Clear unfinished Work

```bash
gh aw work-queue --repo OWNER/REPO clear --dry-run
```

`clear` releases every non-closed Dispatch with reason `operator`, then cancels every `available` Work with reason `operator` in batches of at most 256. Released Work that reaches its pool's `max_attempts` is cancelled with `attempts_exhausted`. Completed Work and ledger history are retained; Work admitted after the preview is not cancelled. Mutations show a preview and require interactive confirmation; non-TTY input and CI environments are refused. `--dry-run` previews without publishing.

### Replay an Actions snapshot

```bash
gh aw work-queue --repo OWNER/REPO replay --run-id RUN_ID \
  --pool default --max-claims 3 --max-dispatches 3 --concurrency 3 --json
```

`--run-id` downloads the activation snapshot of that run; `--file PATH` reads a downloaded `work-queue.snapshot.json` offline. `--pool`, `--max-claims`, `--max-dispatches`, and `--concurrency` (default 16) predict the next dispatch from the captured ledger. Snapshot replay is read-only: it never writes the ledger or launches workers.

## OpenTelemetry queue health

Ledger reads and publications emit `work_queue.read` and `work_queue.publish` spans. Nested reads and publications contribute to the outer span. They reuse the workflow's trace and job parent, [OTLP configuration](/gh-aw/reference/open-telemetry/), and sanitized `otel.jsonl` mirror. Without valid workflow trace context, queue instrumentation is disabled.

Attributes use the `gh-aw.work_queue.` prefix. `queue.id` is a SHA-256 digest of the lowercased repository coordinates and queue branch, allowing correlation across runs without exporting queue identifiers or task content.

| Attribute suffix | Meaning |
| --- | --- |
| `operation`, `request.kind`, `reason`, `outcome` | Operation, last committed request kind, scheduler plan reason, and launch or settlement outcome |
| `snapshot.present` | Whether a replayed ledger was observed |
| `work.total`, `work.available`, `work.claimed`, `work.completed`, `work.cancelled` | Work counts of the observed ledger |
| `claims.open`, `dispatches.reserved`, `dispatches.started`, `dispatches.bound` | Open Claims and non-closed Dispatches by state |
| `publication.attempts`, `publication.conflict`, `publication.ambiguous`, `publication.commits`, `publication.recovered` | Compare-and-swap attempts, ref conflicts, ambiguous writes, new commits, and requests found already committed |
| `committed.<kind>` | Newly committed `Work`, `Claim`, `Dispatch`, `Bound`, `Completion`, `ClaimCancellation`, `WorkCancellation`, and `Release` operations |
| `error.type` | Allowlisted error category, never raw provider messages |

These are span attributes, not sampled metrics. Group by `queue.id` and use the latest span; never sum counts across spans. Queue telemetry excludes payloads, effects, and Work, Claim, Dispatch, and request IDs.

## Diagnostics

| Setting | Scope |
| --- | --- |
| `DEBUG=work-queue:*` | All JavaScript runtime queue events |
| `DEBUG=work-queue:store,work-queue:dispatch` | Selected components (`claims`, `dispatch`, `effects`, `intents`, `mcp`, `memory`, `native`, `reconciler`, `replay`, `run`, `scheduler`, `store`, `workflow-dispatch`) |
| `DEBUG=workqueue:*` | Go CLI ledger operations |
| `ACTIONS_RUNNER_DEBUG=true` or `RUNNER_DEBUG=1` | All runtime queue events |

Events go to stderr with sanitized counts, flags, and error categories, not payloads or credentials.

| Code | Meaning and recovery |
| --- | --- |
| `unsupported_protocol` | The ledger is version 3 and this job cannot convert it, or a line is not version 4. Run a dispatcher or maintenance first. |
| `cas_exhausted` | The branch head changed on all 8 publication attempts. Rerun the job; publication is idempotent by request ID. |
| `work_queue_pending_limit` | A submission would exceed the pool's `pending_limit`. |
| `work_queue_profile_not_approved` | The Work names a worker profile that is not configured for its pool. Declare the worker with `tools.work-queue.worker: true` and recompile. |
| `pool_invalid` | The pool is not in the compiled configuration. Add it to `aw.json` and recompile. |
| `work_queue_assignment_unknown` | The worker's `dispatch_id` is not in the ledger. |
| `claim_scope_invalid`, `run_binding_conflict` | A run tried to bind or settle a Dispatch bound to another run or workflow. |
| `attempts_exhausted` | Terminal Work cancellation after `max_attempts` cancelled Claims. Inspect the Claims' cancellation reasons before resubmitting with a new `node_key`. |

### Diagnostic artifacts

The `safe_outputs` job exposes `work_queue_outcome` (`completed` when every Claim completed, otherwise `failed`). When trusted queue controls run, `safe_outputs.outputs.work_queue_requests` contains their receipt:

```json
{
  "version": 4,
  "success": true,
  "status": "committed",
  "receipts": [
    {
      "intent_id": "intent:…",
      "request_id": "request:…",
      "commit_id": "q_…",
      "status": "committed",
      "dispatches": 1,
      "launches": [{ "dispatch_id": "d_…", "state": "started" }]
    }
  ],
  "remaining_dispatches": 0,
  "summary": "## Work queue controls…"
}
```

A launch with `state: "launch_failed"` carries a `reason` and returned its Work to the pool. The controls step fails only when a receipt is `blocked` or a dispatcher staged no intents.

The activation snapshot `/tmp/gh-aw/work-queue.snapshot.json` contains the ledger at activation and, for a worker, `worker.claims[]` with `handle`, `claim_id`, `work_id`, the Work payload as `work`, and predecessor `result_refs`. Claim audit files live under `/tmp/gh-aw/claims/<identity-hash>/` and contain executed operations and structured errors, not raw handler output. Writers redact configured secrets and built-in credential patterns; redaction does not anonymize repository names, IDs, URLs, or unknown secrets.
