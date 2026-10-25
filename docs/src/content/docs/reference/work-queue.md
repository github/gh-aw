---
title: Work queues
description: Native work-queue configuration, Claim-scoped tools, lifecycle rules, and operator commands.
---

`tools.work-queue` provides a native, Git-backed queue with fair scheduling and task dependencies. The version-3 transaction log, `work-queue.jsonl`, is authoritative. The default queue branch is `work-queue`; Issues can mirror tasks but cannot store the queue. There is no `storage` option.

For architecture, a [minimal deployment example](/gh-aw/patterns/workqueue-ops/#deploy-a-native-queue), and lightweight alternatives, see [WorkQueueOps](/gh-aw/patterns/workqueue-ops/). The [protocol specification](/gh-aw/specs/work-queue-specification/) and [executable models](https://github.com/github/gh-aw/blob/main/specs/work-queue/README.md) cover implementation contracts and verification.

## Initialization and authorization

The first accepted producer submission atomically installs Policy and Work. Compilation, activation, reads, and dispatch-only requests do not initialize a queue. On an existing AW-managed queue, trusted submission and dispatch processing install missing configured pools on demand before deployment synchronization. Add the pool to `aw.json` and recompile the producer or dispatcher. Installation publishes a separate `PoolInstall` operation, preserves existing pools and the Policy epoch, and reuses worker authority already granted by installed pools. Workers, observers, dry-run previews, and accepted-request recovery do not install pools. New worker profiles, economic changes, and existing-pool changes still require a quiescent queue and an explicit administrator `policy` update.

Native `submit-work` and `submit-graph` resolve scheduling and approved AW worker routes from the verified repository revision, not an unrelated local checkout. Assignment-capable workers must already be published.

AW supplies authenticated roles, worker routes, and credentials. Agents receive an immutable snapshot, not queue-write credentials. Workers must accept `work_queue_assignment` and be verifiable at an approved immutable revision. Queue Git access uses isolated storage and ephemeral authentication, independently of source-checkout credentials.

| Record | Meaning |
| --- | --- |
| Work | Immutable task definition, optionally part of a dependency graph. |
| Claim | Authorization for one attempt, identified by its original `claim_handle`. |
| Policy | Scheduling limits and approved worker contracts. |
| Deployment | Worker revision and availability for future execution. |
| Reservation | Worker capacity retained until verified termination or nonlaunch. |
| Completion | Durable task completion, recorded before delivering effects. |
| Result | Independently verified delivery; satisfies Work dependencies. |
| Release | Frees a reservation; does not establish Result. |

An existing malformed, policyless, or unsupported ledger fails validation rather than being reset. Git access failures have no REST/GraphQL ledger fallback.

> [!CAUTION]
> Repository writers can alter or delete the queue branch. A deleted branch is treated as a fresh queue, losing active deduplication and settlement evidence. Checkpoints do not recover a deleted branch.

## Repository scheduling settings

Configure the optional `work_queue` object in `.github/workflows/aw.json`. A missing file, missing section, or `{}` uses defaults. Unknown properties, nulls, invalid types, and out-of-range values are errors. Identities, credentials, and worker routes are managed by AW, not this object.

| Field | Default | Accepted values |
| --- | --- | --- |
| `concurrency` | 16 | Integer 1–4,096; capacity per pool |
| `pending_limit` | 4,096 | Integer 1–4,096; queue-wide pending Work |
| `retry.max_attempts` | 3 | Integer 1–16 |
| `retry.backoff_seconds` | 30 | Integer 1–3,600 |
| `mode` | `weighted-priority` | `weighted-priority` or `strict-priority` |
| `class_weights` | `[8,4,2,1,1]` | Five integer weights, each 1–1,000 |
| `accounting_weights` | `{"":1}` | Named integer weights, each 1–1,000; the empty key retains weight 1 |
| `pools` | One `default` pool | Up to 64 named objects overriding `concurrency`, `retry`, and `per_account_limit` (1–4,096); inherit AW worker routes |
| `issues` | Disabled | Boolean or settings object; see [Backing Issues](#backing-issues) |

```json title=".github/workflows/aw.json"
{
  "work_queue": {
    "concurrency": 3,
    "pending_limit": 30,
    "retry": { "max_attempts": 3, "backoff_seconds": 30 }
  }
}
```

Defaults use singleton assignments and FIFO ordering within one priority and accounting key. Weights allocate opportunities to receive durable Claims, not CPU time or successful completions. Lower priority numbers receive greater weight by default, not strict precedence.

Editing configuration does not replace an installed Policy. Apply economic changes with `policy --from-config --epoch EPOCH` only when no unfinished Work, reservations, or unresolved delivery barriers remain.

### Worker evolution

Compatible worker updates affect future execution without draining the queue. Contracts cover declared inputs, permissions, tools, and output/resource capabilities; prompt or engine changes alone do not change the contract. Existing assignments retain their original worker profile and run binding. Unavailable or incompatible workers block only affected Work and its successors.

Protected producer/dispatcher processing synchronizes approved routes from the verified default-branch revision. Operators use `deploy --from-config`, optionally with `--pool` or `--worker-profile`. To pin Work, use `submit-work --execution-ref IMMUTABLE_SHA` or a submission node's `execution_ref`; changing the pin requires new Work.

Worker dispatch uses verified tags named `gh-aw/work-queue/<SHA>`. The trusted dispatch credential needs `actions: write` and `contents: write` for tag creation. Tags are never overwritten or deleted by the queue; repository writers must not mutate tags in use. A launch sends at most one dispatch POST: an uncertain response does not authorize another launch.

## Workflow roles and intent tools

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

Dispatchers also declare approved workers in `safe-outputs.dispatch-workflow.workflows` and bound launches with `max`. The [deployment example](/gh-aw/patterns/workqueue-ops/#deploy-a-native-queue) combines these declarations.

Standalone `tools.repo-memory` persistence is unsupported in native queue workers; memory-backed queues are separate [alternative patterns](/gh-aw/patterns/workqueue-ops/).

| Role | Authority |
| --- | --- |
| Observer | Read/explain the snapshot; no submit, dispatch, or finish authority. |
| Producer/dispatcher | Stage task plans and assignment requests within approved pools, routes, and budgets. The scheduler selects Work. |
| Worker | Process only its authenticated assignment's `claims` array. Missing required assignment input is an error. |

| MCP tool | Purpose |
| --- | --- |
| `work_queue_read`, `work_queue_explain` | Inspect the immutable activation snapshot; predictions can be stale. |
| `work_queue_submit` | Stage an immutable task or dependency graph. |
| `work_queue_dispatch_next` | Request scheduler-selected assignments from a pool within a limit. |
| `work_queue_claim_finish` | Stage a Claim's `completed` or `cancelled` finish intent. |

When listed in `<mcp-clis>`, invoke tools as `work-queue work_queue_read '{}'`. These are agent tools, not `gh aw work-queue` operator subcommands. Staging an intent does not publish it.

### Claim-scoped effects and delivery

- Every safe output and finish intent belongs to an original Claim. Multi-Claim assignments require `claim_handle` on every message, even if only one Claim remains open. Only originally single-Claim assignments may omit it.
- Finish accepts `outcome: "completed"` or `"cancelled"`. `keep_open` defaults to false; `true` is valid only with `"completed"`. Retries must preserve both values.
- Trusted processing records Completion before authorizing effects and Result only after independent delivery verification. Assignment input, a successful Actions run, or an output artifact alone cannot establish authority or Result.
- `keep_open: true` makes Work eligible again only after verified delivery. It preserves fairness debt, resets failure-retry counters, leaves the backing Issue open, and does not satisfy successors.
- Work dependencies require predecessor Results. External Issue dependencies require fresh completion evidence; pull request dependencies require merge evidence. External nodes consume no Claims.
- Cancellation does not stop a native worker or release its reservation. Release requires verified worker quiescence, exact native termination, or definitive nonlaunch.
- Uncertain delivery is not retried by rerunning effects. Bounded verification records Result or `DeliveryFailure`; unavailable evidence leaves the barrier pending.

The immutable Work payload declares its output contract in `effect_contract`. A read-only task can use `{"kind":"none"}`; a missing contract or successful `noop` alone does not prove no-write delivery. Write-capable contracts must bound the permitted outputs and resource scope.

Worker conclusion performs inline reservation cleanup. Scheduled `agentics-maintenance.yml` reconciliation handles stranded reservations and pending delivery before compaction. Maintenance never cancels active runs or manufactures Result; disabling it leaves dispatcher/operator reconciliation as the fallback.

### Claim-scoped memory

`tools.work-queue.memory` generates a structured output tool and protected Git-tree publication adapter for a worker. It is not standalone `tools.repo-memory`. Configuration fields are `name` (default `persist_work_queue_memory`), `path`, `target-repo`, `base-revision`, `branch-prefix`, `schema`, and `max-bytes` (default and maximum `262144`, including the final newline).

Each call supplies the original `claim_handle` and a `memory` object matching the schema. Preparation validates the selector and any `memory.work_id` without repository credentials. Publication requires the immutable Work/profile/ancestor resource scope and independently verifies the resulting commit, tree, blobs, and Claim ref.

Schemas are limited to 16 KiB and 16 nested levels. Supported keywords are `type`, `description`, `properties`, `required`, boolean `additionalProperties`, `items`, scalar `enum`, `minimum`, `maximum`, `minLength`, `maxLength`, `minItems`, and `maxItems`. Each nested schema declares one type; references, regexes, combinators, and executable validators are unsupported.

Data is limited to 32 nested levels and 16384 members per array. Numbers must be safe integers; encode fractional or other exact quantities as strings. See the [Linter Factory example](/gh-aw/gallery/linter-factory/#refiner-memory) for its use.

## Work expiration

`expires_at` is an immutable Unix-millisecond timestamp in `0..9007199254740991`, supplied per submission node or as `submit-work --expires-at`. Omission means no expiration.

At or after the deadline, unclaimed Work becomes cancelled with reason `work_expired`. Running Claims may still complete. Expiration does not release reservations, refund fairness charges, unblock dependencies, or bypass delivery barriers.

Replay uses maximum committed time; live reads also evaluate current time, and activation snapshots use `captured_at`. A wall-clock-only expiration view is not durable until queue activity commits it. There is no background timer or Claim lease.

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
| `true` or `{}` | Use tracking label `work`, status labels `work:<status>`, and title prefix `[work queue]`. |
| `label` | Nonblank literal, at most 33 UTF-8 bytes; whitespace becomes hyphens in status labels. |
| `title_prefix` | Nonblank literal, at most 80 UTF-8 bytes. |

Unknown keys are errors. Labels are created as needed. Titles show the first 12 Work-ID characters; the body retains the full ID. Newly created Issues omit payload titles/descriptions because conclusion lacks the original runtime redaction context.

Projection updates only Work owned by the authenticated admission/Claim hooks, or exact authorized terminal requests. Immutable `IssueLink` records bind one Work to one Issue. Human titles, bodies, discussion, and unrelated labels are preserved.

| Queue outcome | Issue summary |
| --- | --- |
| Completion, delivery pending | Verifying |
| Verified PR delivery | Needs review |
| Verified non-PR delivery | Done |
| Verified `keep_open` completion | Returns to the queue |

Issues stay open by default. Pre-existing `backing_issue` targets, cross-repository projection, and `completion_policy: "close-on-result"` require explicit installed projector grants; ordinary `aw.json` does not grant them. Closure requires verified non-PR delivery and authority installed before admission. `keep_open: true` prevents closure.

`backing_issue` uses string resource identities:

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

Resource hosts must match `GITHUB_SERVER_URL` (default `github.com`). A backing Issue is separate from a Work subject or dependency.

The compiler generates `agentic-workqueue.yml` for Issue events. Closing an exactly bound Issue can cancel unfinished Work after fresh identity and label checks; it cannot revoke completed delivery, stop a worker, or establish Result.

Projection hooks require `contents: write`, `issues: write`, and `actions: read`, with credentials kept outside agent execution. Failed projection does not roll back queue admission or justify rerunning a worker. Ambiguous Issue/comment creation remains pending rather than blindly creating duplicates; automatic recovery of unreceipted creation is not implemented.

### Projector grants

Explicit Policy `projectors` entries are required for pre-existing Issues, cross-repository projection, or closure on Result. Historical non-AW Policies also use them for ordinary projection. Each entry contains:

| Field | Contract |
| --- | --- |
| `principal` | Actual authenticated native principal. |
| `workflow`, `ref` | Workflow path and immutable 40- or 64-hex commit SHA. |
| `pools`, `repositories` | Authorized pool names and backing repositories. |
| `backing_issues` | Exact resource identities for pre-existing Issues, using the shape above; omit for projector-created Issues only. |
| `completion_policy` | `keep-open` or `close-on-result`; installed before admission. |

Entries belong in the complete Policy, not `aw.json.work_queue`. Installation uses `policy --file POLICY.json --epoch EPOCH` on a quiescent queue. Each projecting workflow independently needs the relevant exact-target grant.

Protected hooks need permission to create/delete their `gh-aw-issue-projection/*` coordination refs. With `safe-outputs.github-app`, each hook mints its own scoped projector token. A repository allowlist or Issue marker does not replace a grant or verified creation receipt.

### Synchronization and recovery

```bash
gh aw work-queue --repo OWNER/REPO sync-issues --request-id REQUEST_ID
```

A repository maintainer or administrator can synchronize one committed terminal request identified with `trace`. This updates existing same-repository AW-created mirrors only; it does not create Issues, rebind receipts, close/reopen Issues, repeat effects, or change queue state. Missing or ambiguous receipts remain pending. There is no global Issue scan.

## Operator commands

```bash
gh aw work-queue --repo OWNER/REPO [--branch QUEUE_BRANCH] COMMAND
```

Repository selection is `--repo`, then `GH_REPO`, then local `origin`, then `gh repo view`. Invalid explicit environment selection fails rather than falling back. Local inference selects the fork, regardless of `gh repo set-default`; use an explicit canonical repository after a rename. Offline `replay --file` needs no repository inference.

`--branch` selects a separate queue, not a migration. `--json` is available where supported. Reuse `--request-id` and immutable inputs to recover an uncertain accepted publication without repeating it. Use each subcommand's `--help` for its complete flags.

| Command | Purpose |
| --- | --- |
| `stats` | Metadata-only counts, delivery, reservations, eligibility, and activity. |
| `state [--graph GRAPH] [--pool POOL] [--state STATE] [--search TEXT]` | Metadata-only ASCII tree. |
| `inspect --work-id ID` or `--claim-id ID` | IDs, dependencies, ownership, delivery, and native-run barriers. |
| `replay` | Full reconstructed live state, including payloads in JSON; `--run-id` or `--file` selects a captured snapshot. |
| `explain --pool POOL [--work-id ID]` | Read-only scheduler/dependency evaluation. |
| `explain --request-id ID` or `--claim-id ID` | Exact historical grant decision. |
| `trace --request-id ID` or `--claim-id ID` | Bounded causal events without payloads or receipt contents. |
| `tui` (alias `interactive`) | Keyboard browser with reviewed cancellation and priority actions. |
| `submit-work --file work.json` | Admit Work; default priority 3 and accounting key `""`. |
| `submit-graph` | Atomically admit a graph, including external Issue/PR dependencies. |
| `dispatch-next --pool POOL --max-claims N --max-dispatches N` | Commit assignments and reservations; does not launch workflows. |
| `dispatch DISPATCHER [--wait] [--dry-run]` | Activate a verified default-revision dispatcher, not a worker directly. |
| `deploy --from-config` | Refresh compatible worker routes; optional `--pool`, `--worker-profile`. |
| `policy --from-config --epoch EPOCH` | Update existing, quiescent Policy; `--file` is the mutually exclusive alternative. |
| `control`, `cancel-work` | Apply authorized pause, cutover, or Work cancellation. |
| `cancel-claim --claim-id ID[,ID...] --reason CODE` | Cancel exact current Claims and their Work, not native runs. |
| `reprioritize --work-id ID[,ID...] --priority 1..5 --reason CODE` | Administrator-only priority override for future grants of available Work. |
| `clear --reason CODE [--dry-run] [--hard]` | Interactive local cancellation or settled-queue reset; see below. |
| `evidence`, `reconcile --dispatch-id ID` | Inspect native evidence and reconcile reservations. |
| `sync-issues --request-id ID` | Repair eligible existing mirrors for one terminal request. |
| `compact` | Checkpoint the ledger while preserving replay state and request identities. |

`state` and `trace` accept `--offset` and `--limit`; state output is bounded to 256 rows/64 KiB. `cancel-claim` and `reprioritize` require administrator authority, accept repeated or comma-separated IDs, and publish all-or-nothing checked transactions. Stale ownership or priority changes reject the action. Reprioritization preserves FIFO positions, retries, and fairness charges.

### Structured diagnostics

All timestamps are Unix milliseconds. Inspection reads do not refresh external dependencies, fetch live Actions status, reconcile reservations, or retry effects.

| Output | Fields and interpretation |
| --- | --- |
| `stats --json` | Snapshot identity, `policy_mode`, pause controls, `claims_by_state`, cancellation/failure reasons, `delivery_by_state`, and 24-hour committed `activity`. |
| Dispatch counters | `dispatches_total`/`dispatches_by_state` include history. `native_reservations`/`reservations_by_state` count retained reservations; legacy `dispatches` aliases the retained count. Neither proves live Actions status. |
| `pools` | Per-pool counts, limits, eligibility, oldest admission age, and a one-Claim/one-dispatch prediction. Eligibility excludes native-capacity checks; prediction includes them. |
| `state --json` | Typed tree rows and `next_offset`; pagination counts graph and Claim rows as well as Work. Keep filters unchanged between pages. |
| `inspect --json` | `selection`, `details`, `work`, `scheduling`, `claim`, and `dispatch`; unavailable metadata is null. Use `--claim-id` for historical attempts. |
| `explain --json` | Scheduler `decision` and compact `prediction`. |

Metadata diagnostics exclude payloads, Result descriptors, request parameters, and receipt bodies. Retired pools remain visible with `policy_installed: false`; unknown admission ages are counted rather than inferred from client timestamps.

### Clear unfinished Work

```bash
gh aw work-queue --repo OWNER/REPO clear \
  --reason operator_clear --request-id incident-clear --dry-run --json
```

Without `--hard`, `clear` cancels cancellable unfinished snapshot Work while preserving completed Work, delivery barriers, history, deduplication, and fairness debt. It neither pauses admission/grants nor stops workers/releases reservations. Work with pending retained delivery is excluded.

Both modes require maintain/admin access, interactive confirmation for new mutations, and a reason of 1–128 ASCII letters, digits, `_`, `.`, `:`, or `-`, starting with a letter or digit. They refuse CI environments, including `COPILOT_AGENT_SESSION_ID`, before queue access. There is no `--yes`, force, or CI bypass.

`--dry-run` validates without writing or reserving the request ID. Normal cancellation is atomic, limited by Policy to at most 256 Work; use bounded `cancel-work` batches for larger sets. Concurrent target changes reject the entire operation. Retrying an accepted request with the same reason recovers its original targets, not later admissions.

#### Hard reset

`clear --hard` additionally requires:

- Admission and grants already paused.
- All native reservations released and no open Claims.
- No pending/failed delivery barriers or uncertain effects.
- Confirmation and publication against the exact previewed Git revision.

> [!CAUTION]
> Hard reset forgets active Work, Claim, and request identities, retry history, and fairness debt. Previously settled effects can repeat if old identities are resubmitted. Prefer ordinary `clear` for cancellation or `compact` for ledger size.

The reset retains current Policy, worker routes/availability, credential generation, and paused controls. Previous history remains at `prior_git_sha`; this is a fast-forward publication, not history erasure.

### Replay an Actions snapshot

```bash
gh aw work-queue --repo OWNER/REPO replay --run-id RUN_ID \
  --pool default --max-claims 3 --max-dispatches 3 --json
```

`--run-id` selects the exact attempt's matching activation artifact; `--file PATH` reads a downloaded snapshot offline. Missing, ambiguous, expired-only, or invalid matches fail. Candidate downloads and snapshot reads each have an aggregate 162 MiB limit.

Artifact JSON is metadata-only, unlike live replay JSON. Scheduler predictions use capture time. Replay never launches workers, repairs live state, or repeats effects.

`dispatch --dry-run` previews a verified dispatcher route without creating tags or launching. A real dispatch requires the returned run identity to match the verified snapshot. Unknown outcomes must be inspected before retrying. `--wait` waits only for the dispatcher, not worker Results. Direct Work/Claim/ref selectors and alternate queue branches are unsupported for `dispatch`.

### Keyboard browser and checked actions

`tui` requires interactive input/output and at least 45 columns × 16 rows. At 100 columns it displays tree and details side by side. It rejects `--json` and `--request-id`.

| Key | Action |
| --- | --- |
| Arrows, `j`/`k`, `PgUp`/`PgDn`, `g`/`G` | Navigate |
| Left/right, `h`/`l` | Collapse/expand Claim history |
| `Tab`/`Enter` | Switch tree/details focus |
| `/`, then `Enter` | Search IDs/metadata |
| `Space` | Toggle multi-selection |
| `c`, `p` | Review administrator cancellation/priority changes; `y` publishes, `n`/`Esc` dismisses |
| `r`, `?`, `q` | Refresh, help, quit |
| `Esc` outside a dialog | Clear search and selection |

Idle refresh is every 10 seconds; search and action review pause it. Failed reads retain the prior view with an explicit stale/error status. Uncertain publications retain their request ID for `explain --request-id`.

## Runtime debug logging

| Setting | Scope |
| --- | --- |
| `DEBUG=work-queue:*` | JavaScript runtime queue events |
| `DEBUG=work-queue:store,work-queue:dispatch` | Selected runtime components |
| `DEBUG=work-queue:claims,work-queue:delivery,work-queue:native_delivery` | Runtime delivery verification |
| `DEBUG=workqueue:delivery,workqueue:native_delivery` | Native operator delivery recovery |
| `ACTIONS_RUNNER_DEBUG=true` or `RUNNER_DEBUG=1` | All runtime queue events |

Events go to stderr with elapsed timing and sanitized counts, flags, statuses, and error categories, not payloads, identities, or credentials. Blocked controls and exhausted delivery verification also emit always-visible diagnostics.

Routing diagnostics distinguish installed Policy gaps from compiler approval failures:

| Code | Meaning and recovery |
| --- | --- |
| `work_queue_pool_not_installed` | The requested pool is absent from installed Policy. Configuration edits alone do not install it; recovery requires an explicit administrator Policy update after the queue is quiescent. |
| `work_queue_profile_not_approved` | The worker profile is absent from the installed pool. Check installed Policy and caller-approved routes; adding profiles requires an administrator Policy update on a quiescent queue. |
| `work_queue_worker_not_compiler_approved` | The caller's compiled configuration does not approve the worker route. Check `safe-outputs.dispatch-workflow.workflows` and the published worker's assignment support. |

HTTP failure diagnostics include validated quota counters, `rate_limit_reset_unix` (Unix seconds), `retry_after_seconds`, fixed rate-limit/SSO flags, and the GitHub request ID when available. Explicit rate-limit evidence distinguishes throttled HTTP 403 responses from otherwise ambiguous access denials; quota headroom alone does not prove permission denial. Raw provider messages, arbitrary headers, and authorization URLs are never printed.

Projection coordination failures emit `projection.locks.acquire.failed` and counts of requested versus acknowledged locks. `projection.locks.retained` indicates unacknowledged native writes whose locks remain held. These diagnostics do not authorize retries, worker reruns, receipt recreation, or lock deletion.

GraphQL failures include a validated GitHub request ID even when no HTTP status is exposed. The failure summary retains the error count and allowlisted categories; individual `.graphql_error` records require runtime debug logging and are limited to five per failure. `graphql_error_details_omitted` counts errors without detailed records. Detailed records identify the error index, allowlisted type and operation, and zero-based lock (`l0`–`l49`), unlock (`u0`–`u49`), or Issue projection (`m0`–`m49`) aliases only when the request builder supplies that alias scheme. Unaliased label operations also require explicit request-builder context. Arbitrary Claim-handler aliases, including aliases named after GraphQL operations, remain unclassified. `graphql_result_present` means only that the alias returned non-null data, not that a native receipt was verified. `graphql_reference_already_exists` recognizes the exact reference-conflict message; it does not establish lock ownership or authorize deletion. Unknown paths/types remain unclassified. Collections larger than 50 are marked `graphql_errors_truncated=true` and `graphql_error_types_inspected=false`: summary category inspection is skipped, not a finding that categories are absent. Their debug details are marked `graphql_error_sample_only=true`; sampled types do not affect the summary's queue-control classification or authorize retries. Raw messages, paths, queries, variables, and response bodies are never printed.

### Diagnostic artifacts

Claim audit exports contain operations, temporary-ID references, and structured errors, not raw handler output or proof of Result. Default retention is one day; failed worker `safe-outputs-items` are retained for seven days. Repository policy or `GH_AW_DEFAULT_ARTIFACT_RETENTION_DAYS` can override retention. Uploads fail if redaction fails.

The `safe_outputs` job exposes `work_queue_outcome`. Unverified Claims retain delivery state, effects disposition, and reconciliation reason in scoped `claim-delivery-errors.json`.

When trusted queue controls are configured, `safe_outputs.outputs.work_queue_requests` contains their version-3 JSON receipt. A successful controls job can report `no_grant` with no launches; only a launch with `state: "bound"` identifies a verified native worker run. This receipt proves launch, not worker completion or Result.

Claim files live under `/tmp/gh-aw/claims/<original-identity-hash>/` with directory/file modes `0700`/`0600`:

| File | Contents |
| --- | --- |
| `safe-output-items.jsonl` | Executed operations and original Claim attribution |
| `temporary-id-map.json` | Audit references; runtime resolution uses the in-memory map |
| `safe-output-errors.json` | Structured failures, not raw handler output |

No `delivery-receipt.json` is written or uploaded; protected in-memory evidence and the ledger establish delivery.

Writers redact configured secrets, gateway tokens, and built-in credential patterns from decoded JSON keys/values. After verification, redaction also uses worker-job secrets and recoverable agent-log runtime masks. Redaction does not anonymize repository names, IDs, URLs, personal data, or unknown secrets. Masks registered only in the safe-output job cannot be recovered from agent logs. Limit diagnostic content and artifact access accordingly.

## Compatibility

Upgrade all closed-schema readers and runtimes before using newer version-3 fields or evidence records, including Issue projection, expiration, worker contracts, and inline termination evidence.

Historical Policy-only genesis and unmarked worker profiles remain readable. Unmarked workers retain exact-ref behavior until explicit quiescent migration. Workflow-level `work-queue-policy` and `tools.work-queue.issues` are deprecated and conflict with repository-level equivalents.

Standalone seeding (`initializeWorkQueue`, `initializationContext`, or pre-submission `policy`), scalar assignments, direct agent-selected claiming, and arbitrary run-ID adoption are unsupported. First accepted submission replaces seeding; existing queues need no reset.
