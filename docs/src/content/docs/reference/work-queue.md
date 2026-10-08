---
title: Work queues
description: Git-backed work queue roles, Claim-scoped effects, operator commands and checked keyboard actions.
---

The Git-backed queue provides mandatory fair scheduling, immutable Work DAGs and
Claim-scoped worker effects. The causal `work-queue.jsonl` log is its only
authority. Defaults behave like FIFO: one priority, one accounting key and
oldest eligible Work first. Configured weights share durable Claim opportunities,
not CPU time or successful completions.

Issues and pull requests can be dependency nodes, not queue storage. For
lightweight issue checklists, sub-issues, Discussions or cache-memory backlogs,
see [WorkQueueOps](../../patterns/workqueue-ops/); those progress markers do not
provide this protocol's authority or fairness guarantees.

See [deployment](../../guides/deploy-work-queue/) for Policy installation, the
[specification](../../specs/work-queue-specification/) for normative behavior and
implementation coverage, and the
[formal verification reference](https://github.com/github/gh-aw/blob/main/specs/work-queue/README.md)
for executable models, contracts and reproduction instructions.

## Workflow roles and intent tools

`tools.work-queue` enables an immutable activation snapshot. Trusted
compiler/runtime context establishes the role; snapshot metadata and
agent-created files cannot grant authority.

| Role | Behavior |
| --- | --- |
| Observer | Read/explain tools only; ordinary configured report or `noop` authorization; cannot submit, dispatch or finish Work |
| Producer/dispatcher | Stages entitled task plans and bounded pool/budget requests; trusted processing refreshes the ledger, selects by Policy and atomically commits a fair prefix |
| Worker | Required version-3 Claim array; trusted activation authenticates the actual run/workflow/revision before scoped effects |

Missing assignment input cannot downgrade a declared worker to an observer.
A genuinely absent queue can be reported as uninitialized; an existing empty,
malformed or unsupported ledger is an explicit failure, not an empty backlog.
Unassigned dispatcher queue-control authority does not authorize arbitrary
resource-writing outputs.

| MCP tool | Purpose |
| --- | --- |
| `work_queue_read`, `work_queue_explain` | Inspect the immutable snapshot; predictions may be stale and sorting affects only presentation |
| `work_queue_submit` | Stage bounded immutable task/graph plans within installed producer entitlements |
| `work_queue_dispatch_next` | Request a bounded pool prefix, without selecting winning Work or a target |
| `work_queue_claim_finish` | Stage an original Claim's `completed` or `cancelled` outcome |

If `<mcp-clis>` advertises the runtime wrapper, invoke it as
`work-queue work_queue_read '{}'`. These subcommands are MCP tools, not
`gh aw work-queue` operator commands.

### Claim-scoped effects and delivery

The compiler supplies reserved `work_queue_assignment` and caller context.
Assignment input alone does not authorize effects, and a rerun cannot inherit
attempt 1's authority.

Every safe output and finish intent belongs to one original Claim. Only an
originally single-Claim assignment can omit the selector. Multi-Claim assignments
require `claim_handle` on every message even after other members close.
Malformed, null, foreign or conflicting selectors fail.

Each member finishes independently with `outcome: "completed"` or `"cancelled"`.
Missing finish does not authorize effects. Batching defaults to one Claim and is
enabled only for compatible Work with substantial reusable setup. Staged finish
is an intent: trusted processing publishes Completion before scoped effects and
Result only after independently verified delivery.

An Issue predecessor needs fresh completed-state evidence; a PR predecessor
needs actual merge evidence. These nodes consume no Claims. Work successors wait
for their own predecessor Results, not a batch-wide native conclusion.

Lost launch responses, dispatcher cancellation and elapsed deadlines do not
prove nonlaunch. Reservations remain until exact termination/nonlaunch evidence
exists. Completion with uncertain delivery does not rerun effects; bounded
verification yields Result or DeliveryFailure.

### Diagnostic artifacts

Claim-scoped audit exports contain executed operations, temporary-ID references
and structured errors, not raw handler output or proof of Result. They use private
directories/files, decoded-field secret redaction and fail-closed final uploads
with one-day retention by default. Protected in-memory verification evidence
remains separate; the unused disk delivery receipt is removed.

Only queue workers receive these upload paths and the final redaction step.
Repository references are not anonymized, and opaque masks registered only in
the safe-output job cannot be recovered from agent logs. See
[Claim diagnostic artifacts](../../patterns/daily-report-portfolio/#claim-diagnostic-artifacts)
for file purposes and redaction limits.

## Operator commands

`gh aw work-queue` defaults to branch `work-queue` and supports Git storage only.
Select the repository with `--repo owner/repo`; an explicit `--branch` chooses a
separate authority, not a migration. The CLI and workflow runtime share the
closed QueueCommit contract and scheduling rules, checked against independent
fixtures. Access failures, malformed logs and unsupported versions are explicit
errors; commands do not silently initialize Policy over an invalid ledger.

| Command | Purpose |
| --- | --- |
| `replay`, `stats` | Inspect the causal projection, typed graph nodes, independent Claims and native reservations |
| `state [--graph GRAPH] [--pool POOL] [--state STATE] [--search TEXT]` | Metadata-only ASCII graph/Work/Claim forest; `--offset` and `--limit` paginate at most 256 rows/64 KiB |
| `inspect --work-id ID` or `inspect --claim-id ID` | Full copyable IDs, dependency cross-references, current/historical ownership, original assignment membership and independent delivery/native barriers |
| `tui` (alias `interactive`) | Keyboard master-detail browser; search, multi-select, live cursor details, checked cancellation and prospective priority changes |
| `explain --pool POOL [--work-id ID]` | Evaluate the native selector or inspect a dependency path without changing passes |
| `explain --request-id ID` or `explain --claim-id ID` | Reconstruct an exact historical grant using the authoritative prefix |
| `trace --request-id ID` or `trace --claim-id ID` | Read bounded causal events without exposing payloads or receipt contents; use `--offset` and `--limit` for pagination |
| `compact` | Canonicalize and deduplicate complete commits without dropping history, resetting debt or moving FIFO positions |
| `policy --file policy.json --epoch EPOCH` | Install an authorized prospective Policy only when the queue is drained |
| `submit-work --file work.json` | Admit an immutable payload with default priority 3 and shared accounting key `""`, subject to installed entitlements |
| `submit-graph` | Atomically admit a bounded normalized graph, including Issue/PR dependency nodes |
| `dispatch-next --pool POOL --max-claims N --max-dispatches N` | Commit a deterministic fair prefix and its reservations; does not itself send a workflow-dispatch POST |
| `control`, `cancel-work` | Apply authorized pause/cutover/cancellation decisions without refunding service or force-releasing a possible native run |
| `cancel-claim --claim-id ID[,ID...] --reason CODE` | Atomically fence exact current Claims and terminally cancel their Work; not a retry, worker impersonation or native-run stop |
| `reprioritize --work-id ID[,ID...] --priority 1..5 --reason CODE` | Administrator-only, compare-and-set override for future grants of available Work; preserves admitted definitions, FIFO positions, retry boundaries and accumulated debt |
| `evidence`, `reconcile --dispatch-id ID` | Inspect authenticated run evidence and reconcile exact termination; uncertainty retains capacity |

Use `--json` for structured output and `--request-id` to retain a logical
publication handle across retries. Consult each command's `--help` for validated
inputs. Reservation, native launch, verified binding, Completion, Result and
Release are distinct states; successful `dispatch-next` is not proof of launch.

Worker finish is the scoped MCP intent, not an operator impersonation command.
Direct `claim --work-id`, scalar assignments, Issues storage, old records,
automatic upgrades and arbitrary run-ID adoption are unsupported. Quiesce old
writers/workflows and preserve evidence before explicit current-protocol
deployment. Mutation permissions and trusted run evidence must be independently
established; an operator actor string is not worker authorization.

### Keyboard browser and checked actions

```bash
gh aw work-queue --repo owner/repo tui
gh aw work-queue --repo owner/repo state --json --limit 80
gh aw work-queue --repo owner/repo cancel-claim \
  --claim-id CLAIM_A,CLAIM_B --reason operator_cancelled --request-id incident-cancel
gh aw work-queue --repo owner/repo reprioritize \
  --work-id WORK_A,WORK_B --priority 1 --reason operator_reprioritized --request-id incident-priority
```

The TUI requires interactive stdin/stdout and at least 45 columns by 16 rows.
At 100 columns it shows the forest beside cursor-synchronized details; narrower
terminals switch panes with `Tab` or `Enter`. Full IDs remain in scrollable
details, while list labels are abbreviated. Selection and focus also use text
markers, not color alone.

| Key | Action |
| --- | --- |
| Arrows or `j`/`k`, `PgUp`/`PgDn`, `g`/`G` | Navigate Work and Claim attempts |
| Left/right or `h`/`l` | Collapse/expand Claim history |
| `Tab`/`Enter` | Focus details; arrows/page keys scroll that pane |
| `/`, then `Enter` | Apply a case-insensitive ID/metadata search |
| `Space` | Toggle multi-selection, including selections hidden by search |
| `c`, `p` | Review cancellation or select priority 1..5; `y` publishes, `n`/`Esc` dismisses |
| `r`, `?`, `q` | Refresh, keyboard help, quit |
| `Esc` outside a dialog | Clear search and selection |

Reads refresh every 10 seconds while idle; search and action reviews freeze
automatic reads. Every action shows its authority and exact targets, and cannot
publish if the targets exceed the confirmation viewport. Each TUI action
generates a stable request ID (`--json` and `--request-id` are rejected);
uncertain acknowledgments remain visible for `explain --request-id` inspection.
Read failures retain the previous view with an explicit stale/error status.

Forest edges group graph membership and Claim attempts, not dependency
parenthood. Shared Work dependencies and typed Issue/PR gates are explicit
cross-references in details. These views never display payloads or receipt
bodies; `replay --json` remains the full authoritative projection.

Cancellation and reprioritization accept repeated selector flags or
comma-separated IDs, require an administrator, and publish one all-or-nothing
checked transaction. A Claim selector includes a durable ownership fence, so
CAS refresh cannot cancel a replacement owner. Completed ownership cannot be
cancelled; historical Claims cannot cancel a new attempt. Cancellation keeps
dispatch membership, charges and native reservations; reconcile exact evidence
separately.

Priority overrides apply only to available Work, including retry-waiting Work.
They do not edit active assignments, admitted metadata, child admission defaults,
fairness weights or prior charges. Configured weights determine class shares;
defaults favor lower numbers without strict-preemption semantics. Competing
grants or priority changes reject stale actions rather than redirecting them.
Go/JavaScript protocol tests cover these administrator extensions; the existing
fixed-priority TLA+ models do not model arbitrary operator reprioritization.
