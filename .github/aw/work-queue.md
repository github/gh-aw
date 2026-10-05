---
description: Work queue guidance for agentic workflow dispatchers, workers, inspection, and recurring queue patterns.
---

# Work Queue

Use the Git-backed work queue when independently running dispatchers and workers need durable, replayable work and claim state. Configure `tools.work-queue: {storage: issues}` to use GitHub Issues instead; both backends expose the same agent tools and trusted worker reconciliation. For a lightweight checklist, sub-issue, Discussion, or cache-memory backlog, use the [WorkQueueOps pattern](../../docs/src/content/docs/patterns/workqueue-ops.md) instead; those backends are not the `tools.work-queue` protocol.

## Dispatcher workflows

- Enable `tools.work-queue: true` to read the queue snapshot captured at activation. Call `work_queue_read` without arguments to see available Work (oldest first), `next_work`, and claim states; pass `work` to inspect one Work identity. Optionally pass one to four ordered `sort` keys, for example `{"sort":[{"key":"enqueued","direction":"desc"}]}`, to sort available Work and recommend the newest item instead. Keys are `id`, `enqueued`, and `id_length`; later keys break ties, then the default oldest-first order does. `next_work` is a recommendation from a possibly stale snapshot, **not** an acquired Claim. Sorting does not change trusted Claim selection or dispatch authority.
- Select work with stable identities and make processing idempotent. Claimed Work does not block newer available Work; oldest-first selection is best effort, not strict FIFO.
- To dispatch a queue worker, configure `safe-outputs.dispatch-workflow` with an allowed same-repository `workflow_dispatch` worker that enables `tools.work-queue: true`. The compiler automatically adds the reserved `work_queue_claim` input to queue-enabled workers; never declare it or `aw_context` yourself. Pass `work_queue: {work_id: "<id from work_queue_read>"}` to that worker's safe-output tool. This selector is **not** a worker input: trusted safe-output processing refreshes the queue, checks availability, publishes a Claim, and injects the assignment through `work_queue_claim`, while `aw_context` carries caller metadata. The worker must look up any additional task details by that identity. Dispatch failure attempts to cancel the Claim; a concurrent dispatch can supersede it, so the worker must still pass activation and completion checks.
- `safe-outputs.dispatch-workflow` without `work_queue` remains a normal dispatch and does not create a Claim. Cross-repository queue dispatch, workers lacking either required opt-in, and staged dispatches cannot create a Claim. `safe-outputs.call-workflow` does not create a Claim. Do not pass agent-chosen Claim identifiers as though they establish ownership.
- If the safe-output log warns that Claim cancellation failed, use its work and claim IDs to investigate and reconcile the runtime `work-queue` branch. The `gh aw work-queue` operator CLI manages a separate branch and transaction format and cannot cancel runtime Claims.
- Do not write queue transactions from an agent or assume the snapshot is current. Queue mutations and version-checked publication belong to trusted processing; if dispatch-workflow is not configured, use the operator CLI for manual queue management.

## Worker workflows

- Enable `tools.work-queue: true`. A queue worker receives exactly one trusted `work_queue_claim` assignment containing `work_id`, `claim_id`, and a `work` payload. The compiler adds this reserved input automatically; never construct or override it from a prompt, event input, or MCP tool argument. `aw_context` is also compiler-managed and reserved.
- Set `tools.work-queue: {storage: git, require-assignment: true}` for workflows that must never process safe outputs without a trusted inbound assignment. With this option, safe-output reconciliation fails closed when the activation snapshot has no worker claim.
- Read the assigned work, perform the bounded task, and stage any external writes through safe outputs. Call `work_queue_claim_finish` once with `outcome: "completed"` when done, or `"cancelled"` if unable to finish. The tool accepts only `outcome`; it records intent, not authority.
- Trusted safe-output reconciliation refreshes the durable queue, checks the effective Claim, persists Completion, and authorizes ordinary outputs only for the winning worker. Activation-time admission and the MCP snapshot are not final authorization. If there is no trusted inbound assignment, a finish intent does not claim work or authorize writes.
- Make worker effects idempotent. Retries and competing Claims can repeat agent execution; missing or losing finish intent must not produce external effects.

When the runtime advertises the `work-queue` CLI wrapper under `<mcp-clis>`, invoke `work-queue work_queue_read '{}'` or `work-queue work_queue_claim_finish '{"outcome":"completed"}'`. These are MCP tool subcommands, **not** `gh aw work-queue` operator commands.

## Inspect and manage the queue with the CLI

The issue backend stores one issue per Work identity, marked `aw:work-queue`. The issue body records the Work transaction; claim, cancellation, and completion transactions are append-only comments from the authenticated publisher. Public comments are not queue transactions. State labels `aw:work-queue:available`, `:claimed`, `:completed`, and `:cancelled` are synchronized for visibility; replayed transactions, not labels, determine authority. Configure `tools.work-queue: {storage: issues}` in every dispatcher and worker sharing the queue. Issue storage requires `issues: read` in activation and conclusion and `issues: write` in safe outputs. Do not mix Git and issue backends for the same queue; migration is not automatic. Queue issue bodies and comments are protocol records and must only be edited by trusted writers. Issue storage reads all queue issues and comments on every refresh; prefer Git storage for large queues.

Use `gh aw work-queue --repo owner/repo replay --json` to inspect projected Work and Claims, and `gh aw work-queue --repo owner/repo stats` for state counts. All subcommands accept `--repo`, `--branch`, and `--json`. The default **operator** branch is `gh-aw-work-queue`; `--branch` selects another operator branch. The workflow runtime uses a separate `work-queue` branch and a different transaction format: do not point the operator CLI at the runtime branch or treat its replay as the runtime snapshot.

For authorized operators, `submit-work --file work.json` (or `--file -` for stdin) submits a JSON object with an idempotent canonical identity; `claim --run-id RUN` selects the oldest available Work, with `--work-id ID` as an override. `finish --claim-id ID --attempt-id ATTEMPT`, `cancel-work --work-id ID`, and `cancel-claim --claim-id ID` update operator state. `compact` removes identical duplicates and orders records; it does not discard unique history. Mutations require repository contents write permission. Establish run and attempt provenance independently: operator `finish` is **not** worker safe-output authorization.

## Useful patterns

- **Bounded consumption:** process a small batch per run, track remaining Work, and emit `noop` when no useful item remains.
- **Retry and recovery:** inspect stale or failed Claims before retrying; cancel only with trusted evidence. Re-read after publication conflicts rather than publishing a stale decision.
- **Human oversight:** inspect `replay --json` and `stats --json` before operator changes; keep Work payloads small, stable, and free of secrets.
- **Alternative backends:** use issue checklists or sub-issues for human-visible queues, cache-memory for disposable branch-local state, and Discussions for community submissions. Do not mix their progress markers with Git-backed Claim authority.

See the [work queue protocol and CLI reference](../../specs/work-queue/README.md) for exact command flags, storage formats, and current implementation boundaries.
