---
title: Work queue priority and fairness
description: A fair work-queue DAG with cross-repository Issue/PR dependencies, batched assignments, and independent Claim finalization.
---

# Priority and fairness for the gh-aw work queue

**Research date:** 2026-10-05

**Repository examined:** `github/gh-aw`, commit `81891f23dfb58b88bd90c9736887880234bffbe5`

**Status:** research and proposed specification; not an implemented feature

**Scope:** the durable `tools.work-queue` protocol, including the Git and Issues backends, with particular attention to HPC workflow dispatch and scheduling literature.

**Required storage constraint:** all authoritative scheduling decisions and their policy, charge, reservation, launch-binding, and release transactions remain in the **same canonical `work-queue.jsonl` transaction log** as Work and Claim transactions. No separate scheduling log, authoritative outbox, database, cursor file, manifest, or state sidecar is introduced. In-memory projections and activation snapshots are disposable derivatives of that one log.

**Required scheduling constraint:** every queue MUST enforce scheduling. There is no opt-out, legacy queue mode, advisory-only operating mode, or backward-compatible transaction reader. The proposed protocol replaces the existing behavior; unsupported old records or backends fail explicitly rather than falling back to an unscheduled queue.

**Required default behavior:** without explicit priority or fairness grouping, the queue MUST feel like a normal FIFO work queue: one priority, one accounting bucket, and oldest-available Work selected next. This is the mandatory scheduler's default behavior, not a legacy mode or a scheduling opt-out.

**Required worker batching:** one worker dispatch MAY carry several scheduler-selected Claims. The inbound assignment is always a bounded array, even for one Claim. Each Claim has independent finish intent, terminal state, and effect authorization; completing one must not require completing the rest.

**Required DAG support:** Work is a schedulable DAG node. Immutable typed predecessor edges and trusted result/Issue/PR observations determine the ready frontier before priority/fairness selection. Independent Work has an empty dependency list and retains queue-like defaults.

## 1. Recommendation

Introduce a **mandatory, trusted, hierarchical grant scheduler for every queue**, rather than making priority and fairness additional agent-controlled sort keys:

```text
dependency/resource/capacity eligibility
    -> priority-class policy
    -> weighted fairness among trusted accounting keys
    -> oldest ready DAG node within the selected key
    -> one committed Claim that grants, charges, and reserves capacity
    -> dispatch
    -> existing worker admission and final safe-output authorization
```

Out of the box, resolve every submission to **priority 3 and one default accounting bucket**, then select the oldest available Work. The mandatory hierarchical scheduler therefore behaves like an ordinary FIFO queue without requiring configuration.

When priority levels or accounting keys are explicitly requested, use **weighted priority classes with positive shares** by default, weighted tenant fairness within each class, and bounded outstanding reservations. Offer **strict priority** as a separately named scheduling policy whose documented consequence is possible starvation of lower classes. FIFO remains the ordering within every selected bucket, including the single default bucket.

Use a deterministic, stride-inspired virtual-pass algorithm with exact integer ticks for the first implementation. Account in **durable Claims**, not worker launches or completed jobs. Each Claim grants and reserves one logical Work slot; compatible Claims share one worker-dispatch reservation. Both projections come from the same Claim records, with no separate reservation file.

Keep dispatchers agentic through **staged submission and `dispatch_next` intents**: agents decide what useful work exists, prepare its immutable plan, and choose when to request a bounded batch. The protocol selects the next Work; the selected worker then executes that Work's plan. The agent never needs to implement the fairness algorithm or guess that its snapshot still names the winner.

The most important HPC lesson is that **workflow readiness, cross-user entitlement, and resource placement are different decisions**. DAGMan and Pegasus manage workflow structure; HTCondor and Slurm manage competing users and available resources. Within-workflow critical-path priority is not a substitute for cross-workflow fairness. [H4], [H5], [H6], [H7], [H8], [L3], [L4]

The proposed minimum guarantee is therefore:

> Within one configured scheduling pool, durable grants follow the configured priority policy and weighted accounting-key policy, using fresh authoritative state. Fairness is over grants while keys remain eligible, not over execution order, completion order, runner-seconds, or external effects.

This is stronger than the current protocol's advisory ordering and requires a protocol/ADR extension. It must not be presented as something a comparator change can deliver.

### 1.1 Simplicity, debuggability, and agentic-dispatch review

The earlier specification had the right trust boundary, but too much protocol machinery and too little explanation of how an agentic dispatcher could use it. The revised design replaces that machinery rather than layering another protocol on top.

| Review finding | Decision |
|---|---|
| A scheduler chain plus source-view digests/deltas duplicated history and complicated replay | Use one causal `QueueCommit` envelope for **all** mutations; its predecessor defines the complete pre-decision state |
| Separate grant, Claim, and reservation facts created companion-validation and partial-state cases | A Claim grants/charges a logical slot; its dispatch group shares a native slot, both derived from the same operations |
| Wall-clock enqueue keys obscured what “FIFO” meant during concurrent publication | Order Work by its first committed `(commit ordinal, operation index)`; timestamps measure age, not queue position |
| Rational wire state, arbitrary hierarchy, and live weight rebasing expanded the first release | Use exact integer ticks, two scheduling levels, and policy changes only after the queue drains |
| An agent could prepare inputs for a snapshot winner that changes before publication | Store Work-specific plans at submission; stage `dispatch_next(pool, max_claims, max_dispatches)` and bind selected Work late |
| Multiple Claims in one worker could be confused with one fairness unit or one terminal result | Charge every Claim, reserve one native dispatch slot, and finalize/effect-gate each assigned handle independently |
| “Read,” “staged,” “granted,” and “launched” could look equally successful | Give each an explicit status; expose partial batches, blocking reasons, and uncertain launches |
| Tracing risked becoming a second authority or a new observability subsystem | Reuse existing OTLP/context plumbing; correlate by request/commit/Claim IDs and derive receipts from the canonical log |

The first release should have one codec/replayer, one pure `planNext` selector, one checked publication path, and one trusted worker binder. It should not include adaptive scheduling, a third fairness level, arbitrary selection filters, live weight edits, or a new external coordinator.

## 2. Evidence and current implementation

### 2.1 Repository findings

All repository links below are pinned to the examined commit. “Current” in this report means this checkout, not whatever subsequently lands on `main`.

| Evidence | What it establishes |
|---|---|
| [Work-queue guidance, lines 7-15][R1] | MCP reads an immutable activation snapshot. `sort` changes a recommendation, not trusted Claim selection or authority. Dispatch takes an explicit Work identity. |
| [Runtime replay, lines 37-55 and 122-161][R2] | Transaction shapes are closed. Work permits enqueue metadata, but no priority, accounting key, weight, or scheduling state. Duplicate conflicting Work facts are invalid. |
| [Runtime replay, lines 201-279][R3] | Ownership is determined by stable Claim identity; available Work is sorted by immutable enqueue time and UTF-8 Work identity. Ownership arbitration and queue ordering are separate. |
| [Runtime selection helpers, lines 382-415][R4] | Oldest-available selection uses the caller's local view. A fixed Claim is revalidated for safety, not FIFO. |
| [MCP server, lines 48-132][R5] | Read sort keys are `id`, `enqueued`, and `id_length`; `next_work` is not an acquired Claim. |
| [Dispatch handler, lines 253-263 and 340-370][R6] | Trusted dispatch checks that a selected Work is available, publishes a Claim, checks effectiveness, and supplies the trusted worker assignment. It does not compare the selection against a scheduling policy. |
| [Git store, lines 150-215][R7] | Writes refresh the log and retry fast-forward-only ref updates. The retry loop reapplies the same supplied intents; it is not a general “choose the next fair Work” callback. |
| [Issues store, lines 81-139 and 167-226][R8] | Issues/comments are separately published signed records. Refreshes span multiple API reads. There is no queue-wide atomic compare-and-swap matching the Git branch's publication point. |
| [Operator selection, lines 20-55][R9] and [operator claim, lines 163-203][R10] | The operator CLI has oldest-available selection, but keeps the initially selected Work across retries. Its wire format and branch differ from the runtime's. |
| [TypeSpec contracts, lines 8-128][R11] | Operator `Transaction` and workflow `WorkQueueTransaction` are separate contracts. Workflow records currently use protocol version 2. |
| [ADR-64955, protocol commitments][R12] | The intended current guarantee is best-effort oldest-available ordering, not global FIFO, a global sequence allocator, or a completion barrier. |
| [Protocol-upgrade ADR][R13] | Migrations are deterministic; historical Work gets no fabricated enqueue timestamp. Transaction version and snapshot-envelope version are independent. |
| [TLA+ model, lines 133-176][R14] | The model constrains local staging to oldest-known Work, but retries reapply serialized intents. Existing safety modeling does not establish a weighted scheduling liveness guarantee. |

The original [work-coordinator issue #64852][R15] also requires deterministic replay of the same accepted fact set, trusted Claim authority, terminal-state protection, a single canonical authoritative file, and compaction preserving future semantics. The implementation documentation explicitly describes the MCP as a snapshot reader, rather than the live mutable MCP surface originally envisioned in the issue.

**Conclusion:** there is presently no built-in priority/fairness contract in this queue. Agent prompts or read-time sorting can express preferences, but cannot establish enforcement across independent dispatchers.

The existing upgrade, ordering, and compatibility behavior above is evidence about the examined implementation, not a compatibility requirement for the proposed protocol. The new design intentionally removes unscheduled operation and old-record support.

### 2.2 Two storage and command distinctions matter

The examined workflow runtime uses `work-queue` and versioned workflow facts. The examined operator CLI defaults to `gh-aw-work-queue` and preserves payload/run-provenance fields in a different format. Those current formats are not interchangeable. The replacement MUST use one current `QueueCommit` codec and replayer for both surfaces, rather than maintain two scheduling implementations. Separately configured queue branches remain separate authorities; sharing a format does not merge their entitlement, permissions, or worker-effect authorization. Neither surface retains an unscheduled mode. [R11], [R12]

Git and Issues expose similar tools, but do not offer identical consistency primitives. HMAC signatures authenticate Issues records; they do not turn a sequence of issue/comment reads and writes into an atomic queue-wide scheduling transaction. A design requiring a single global scheduling decision must acknowledge that difference. [R7], [R8]

## 3. Specify the meaning of fairness before choosing an algorithm

“Fair” can describe several incompatible objectives.

| Objective | Unit being distributed | Appropriate mechanism | What it does not imply |
|---|---|---|---|
| FIFO | Position among eligible Work | First-committed enqueue position | Tenant isolation; equal resource consumption |
| Dispatch fairness | Grants or starts | Weighted round robin, stride, virtual-time scheduling | Equal running mix or completion times |
| Concurrent allocation fairness | Outstanding slots/resources | Fair-share allocation and capacity admission | Equal historical consumption |
| Historical resource fairness | CPU-seconds, runner-seconds, billed resource units | Usage accounting, often with decay | A hard near-term share for every tenant |
| Multi-resource fairness | CPU, memory, GPUs, licenses jointly | DRF or resource-aware allocation | Placement feasibility or workflow deadlines |
| Workflow outcome fairness | Turnaround or slowdown | Workflow-aware multi-DAG scheduling | A simple fixed dispatch ratio |
| Deadline priority | Urgency/slack | EDF, deadline admission, reservations | Fairness or feasibility under overload |

Temporal's fairness is primarily task-dispatch fairness. Slurm fair-share is a usage-based influence on job priority. HTCondor user priority influences resource allocation, separately from a user's ordering of their own jobs. Multi-DAG literature also considers fairness of workflow slowdown. These are related, but not synonymous. [T1], [H1], [H5], [L3]

For example, equal numbers of grants to one tenant whose jobs take 60 minutes and another whose jobs take one minute produce about **98.36% versus 1.64% of total runner-time**, assuming equal runner allocation per job and complete execution. Even equal concurrent slots need not give equal completions per hour.

Likewise, priority at dispatch is **not preemption**. If every runner is occupied, selecting an urgent job first does not free a runner. Meaningful urgent-start guarantees require bounded runtimes, reserved capacity, or an independently specified preemption/checkpoint mechanism.

## 4. What HPC systems and literature contribute

### 4.1 The architectural separation: workflow engine versus resource scheduler

In a scientific DAG, a task becomes ready only when its required predecessors have succeeded. A workflow engine identifies ready tasks, handles retries and data movement, and submits work to a resource scheduler. A resource scheduler decides which competing accounts receive capacity and where jobs fit.

Pegasus represents scientific workflows as DAGs, maps abstract work to execution environments, and can reorder, group, and prioritize tasks. Its optimization documentation discusses clustering short tasks to amortize submission and execution overhead. These transformations improve efficiency, but do not independently guarantee fairness among different users. [H7], [H8]

DAGMan's node priorities affect submission among simultaneously ready nodes and set HTCondor `JobPrio`. They cannot override dependencies or guarantee execution order. After submission, `JobPrio` orders jobs of the same user; user allocation is handled separately. This is a particularly useful model for gh-aw: **a dispatcher's preference among its own tasks should not increase its account's entitlement relative to other dispatchers**. [H4], [H5]

DAGMan also propagates accounting-group metadata into DAG jobs and sub-DAGs. That supports stable charging across fan-out, instead of letting each new child become a fresh resource claimant. [H6]

**Translation to gh-aw:** separate:

1. **Eligibility:** Work exists, is available, is executable by the configured worker pool, and satisfies limits.
2. **Entitlement:** an authorized account/project receives a share of grants.
3. **Local ordering:** select an eligible Work within that entitlement.
4. **Execution placement:** GitHub Actions allocates actual runners and applies its own concurrency rules.

The examined runtime is not yet a DAG dependency engine. The replacement makes
that layer first-class: immutable predecessor edges, verified results, and typed
cross-repository Issue/PR gates define readiness. Do not infer readiness from
timestamps, issue links, or an agent's statement that a task is ready.

### 4.2 Slurm: multifactor priority and hierarchical historical fair-share

Slurm's multifactor plugin combines enabled contributions such as age, fair-share, job size, partition, QoS, association, site policy, and trackable resources, with a nice adjustment. Larger computed job-priority values are better. Scheduler consideration additionally accounts for preemption, advanced reservations, and partition priority tiers. Consequently, “highest priority” is not just one universal scalar. [H1]

Its fair-share contribution compares assigned shares with historical resource consumption. Usage can decay with a configurable half-life, and billing can include multiple resource types. It is not a fixed quota that stops a user once consumed: over-served users may use otherwise idle resources. [H1]

Fair Tree makes that entitlement hierarchical. Sibling accounts are ranked using normalized shares and usage; an account's ranking controls its descendants' fair-share factors. The relation `Level Fairshare = normalized shares / normalized usage` captures under- versus over-service at each level. This is valuable when a project contains many users or workflows: child proliferation must not create more project entitlement. [H2]

However, Fair Tree ranks a fair-share **factor**, not an unconditional final job order or exact percentage of future starts. Other configured factors and resource feasibility still matter. Age also saturates, and dependent/ineligible jobs do not accrue age in the same way as eligible pending jobs. A bounded age term alone is not a proof that every low-priority job eventually runs. [H1], [H2]

**Adopt:** stable accounting identities, hierarchy, auditable policy factors, usage accounting as a future independent layer, and borrowing unused capacity.

**Do not copy blindly:** the full multifactor tuning surface, CPU accounting when gh-aw cannot measure it reliably, or a claim that a weighted priority score creates an enforceable minimum service share.

### 4.3 Backfilling: resource feasibility without unnecessary head-of-line blocking

Lifka's ANL/IBM SP scheduling work is the classic EASY backfilling reference. The later Mu'alem/Feitelson work examines utilization, predictability, workloads, and runtime estimates. [L1], [L2]

The conventional distinction is:

| Policy | Protected reservation | Trade-off |
|---|---|---|
| EASY-style backfilling | The head job's reservation | More aggressive use of gaps; weaker protection for other pending jobs |
| Conservative backfilling | Reservations for all considered pending jobs | Stronger predictability; potentially less flexibility and more planning |

Slurm's documented backfill scheduler can start lower-priority jobs when doing so does not delay the expected start of higher-priority jobs. It considers resource requests and runtime limits, and reserves future resources. Accurate enough time limits and an adequate planning horizon are essential. [H3]

This is not equivalent to arbitrarily skipping a blocked urgent job. A scheduler claiming safe backfill must be able to demonstrate that the borrowed resources will be available by the protected start.

**Translation to gh-aw:** initially skip unavailable, incompatible, retry-delayed, and capped keys so they do not block independent eligible work. Do **not** describe this as EASY backfilling: the current queue does not own runner placement or a reservation calendar.

Reserve runtime-aware backfilling for a future scheduler that knows capacities, execution bounds, and reservation times. Separate resource pools are preferable to pretending that a GPU-blocked job and a CPU-ready job compete for the same interchangeable resource.

Also, HPC systems are not universally nonpreemptive: Slurm supports configured cancellation, requeue, suspension, and gang-scheduling modes. The relevant limitation is that **this gh-aw proposal does not add safe preemption**, not that Slurm cannot preempt. [H9]

### 4.4 DAGMan throttles and Slurm arrays: bound workflow fan-out

DAGMan provides per-DAG submitted-node and idle-job limits, plus category-specific `MAXJOBS`. These limits prevent a single workflow from overwhelming staging capacity or the scheduler. They are not, by themselves, cross-user fair-share policies, and a submitted cluster may contain multiple jobs. [H10]

Slurm arrays offer an analogous practical control: an array expression such as `0-15%4` limits simultaneously running array tasks to four. Array tasks remain subject to ordinary job limits. [H11]

**Translation to gh-aw:** a fair dispatch order can still produce poor responsiveness if one workflow preloads hundreds of accepted or pending Actions runs. Limit **outstanding reservations**, not just calls per dispatcher invocation. Count reserved, launching, queued, and running work until trusted reconciliation releases the reservation.

Distinguish:

- **Share:** a relative entitlement while multiple keys are eligible.
- **Outstanding cap:** an absolute admission limit, potentially leaving capacity unused.
- **Rate limit:** a maximum number of grants per time interval.
- **Queue/admission bound:** a maximum amount of submitted backlog.

They address different failure modes. A per-run batch limit cannot establish a global cap when many dispatchers run concurrently.

### 4.5 HEFT: valuable local priority, not inter-workflow fairness

HEFT ranks tasks using an upward-rank measure and chooses processor placement by earliest finish time. It is a foundational heuristic for minimizing the schedule length of a DAG on heterogeneous resources. That objective is different from distributing resources fairly among independently arriving workflows. [L5], [L6]

A dispatcher that globally prioritizes whichever task lies on the longest critical path may optimize makespan while allowing a large workflow to dominate the ready pool. Conversely, shortest-workflow-first can postpone a long workflow when shorter work keeps arriving.

**Translation to gh-aw:** select oldest-ready Work within an accounting key.
Dependencies are first-class, but critical-path ranking remains a separate
future local-order policy. Do not let an agent invent a globally superior rank
or use a graph's width to increase its accounting entitlement.

### 4.6 Actual multi-workflow fairness literature

Zhao and Sakellariou's *Scheduling Multiple DAGs onto Heterogeneous Systems* explicitly considers both overall makespan and fairness measured through the slowdown each DAG experiences due to competition. This is a direct antecedent for evaluating independent gh-aw workflows, rather than only counting grants. [L3]

Arabnejad, Barbosa, and Suter's *Fair Resource Sharing for Dynamic Scheduling of Workflows on Heterogeneous Systems* discusses online arrivals and distinguishes average completion performance from each workflow's experienced QoS. Its FDWS discussion takes one highest-local-priority ready task from each workflow into the inter-workflow candidate pool, then applies a separate global ranking. This avoids treating a wide DAG's every ready node as a separate chance to win. The chapter's online-scheduling and FDWS sections are particularly relevant. [L4, sections 1.2.2 and 1.2.2.3][L4]

The Hilman/Rodriguez/Buyya survey maps these methods and discusses fairness, priority, workflow structure, deadlines, budgets, and multi-tenant provisioning. It is a useful literature map, but individual heuristics' simulation improvements should not be translated into universal starvation bounds. Its discussion of FDWS also makes the one-ready-representative-per-workflow pattern explicit. [L6, sections 4 and 5.3][L6]

**Adopt:** choose an accounting group before choosing its next task; measure workflow turnaround and slowdown as well as grant shares.

**Important identity caveat:** fairness per workflow *instance* is vulnerable to splitting one user's work into many instances. Entitlement should belong to an authenticated account/project or configured workflow family. Optional instance-level fairness should be nested beneath that entitlement, not increase it.

### 4.7 Two-level scheduling and pilots

Falkon separates resource provisioning from lightweight task dispatch, an important many-task-computing technique. A workflow/task dispatcher can fill an acquired worker allocation efficiently without asking the batch scheduler to place every small task separately. [L7]

The corresponding fairness boundary is also important: a lower-level scheduler may be fair **within its allocation** without being fair across the whole cluster. Long-lived or oversized allocations can delay competitors even if their internal task scheduler is excellent.

For gh-aw, scheduling grants before Actions dispatch is one layer; Actions' runner and concurrency allocation is another. Never infer end-to-end fairness merely because the first layer generated a fair sequence. Keep pre-dispatch buffering bounded.

### 4.8 DRF for genuinely heterogeneous resources

Dominant Resource Fairness equalizes users' dominant shares: the largest fraction of any resource allocated to each user. It generalizes single-resource max-min fairness and analyzes properties including sharing incentive, envy-freeness, strategy-proofness, and Pareto efficiency under its allocation model. The paper implements and evaluates the approach in Mesos. [L8, sections 1-4][L8]

This is relevant if jobs differ materially in CPU, memory, GPU, or license demands. Counting each as one slot can be unfair and inefficient.

It is not the right minimum implementation for today's work queue. gh-aw does not presently own a trustworthy multidimensional resource allocator. Integral nonpreemptive jobs, placement constraints, changing demand, and unavailable runners also prevent simply borrowing the paper's theoretical guarantees.

**Recommendation:** first implement grant fairness plus caps. Introduce resource-vector accounting only when the system can enforce and measure the resource allocation it claims to distribute.

## 5. Temporal: what to adopt, and where not to overclaim

The requested Temporal documentation gives a useful public vocabulary and hierarchy. The observations below are based on the live documentation retrieved on the research date. [T1]

| Temporal behavior | Implication for gh-aw |
|---|---|
| Priority keys are integers 1-5; lower is more urgent; default is 3 | Reuse this convention to avoid unnecessary cognitive differences. |
| Priority is strict across levels | Document that perpetual high-priority backlog can starve lower levels. |
| Fairness keys identify virtual queues within a priority level | Use a stable accounting key; select the key before selecting its Work. |
| Weights determine relative dispatch frequency | Explain the resource unit; a dispatch weight is not a CPU or completion guarantee. |
| Same priority and fairness key use FIFO | Retain immutable enqueue order within each key in the initial proposal. |
| Unkeyed tasks share the empty-string bucket, weight 1 | Resolve unspecified accounting keys to one default bucket when submitting new Work, rather than granting every unkeyed item a separate identity. |
| Workflow/Activity/Child Workflow fields inherit independently | Child work should retain its accounting identity unless trusted policy authorizes a different one. |
| Multiple weights attached to one key have unspecified behavior | Avoid this ambiguity: resolve one administrator-controlled weight per key and policy epoch. |
| Ordering and fairness apply within a task-queue partition | Publish the actual fairness domain. Multiple gh-aw pools/queues do not constitute one global share. |
| Fairness uses backlog, weights, and approximate dispatch tracking, including a count-min sketch | Do not represent Temporal's explanatory round-robin model as exact deterministic cyclic scheduling. |
| Dispatched executions are not considered by fairness | Add independent outstanding limits if running/queued domination matters. |
| Schedule-time weights generally do not reorder old backlog; mode changes drain the old-mode backlog first | Specify policy-update behavior for current-protocol backlog explicitly; do not introduce old-record compatibility. |
| Restart, key cardinality, worker-version changes, and partition imbalance can affect fairness | Prefer exact bounded state for the much smaller Git-backed queue, and test restart and unsupported-version rejection. |

Temporal's documentation describes fairness both with round-robin intuition and probabilistic weighted sequencing. It also documents preservation of fairness state for the top 100 keys during server restarts, accuracy degradation with more keys, weight overrides, and rate-limit limitations. The safe interpretation is **weighted dispatch with documented approximation boundaries**, not an exact global sequence or short-window share guarantee. [T1, “Task Queue Fairness” and “Limitations of Fairness”][T1]

A further subtlety is that fairness within a priority class does not prevent starvation **between** classes. Once different priorities are explicitly assigned, this report uses weighted classes by default; it is a deviation from Temporal's strict class semantics, not a claim of compatibility with that behavior. Without assigned priorities or accounting keys, the same scheduler has only one active class and bucket and reduces to oldest-available ordering.

## 6. Algorithm choices

| Candidate | Strength | Limitation in this queue | Decision |
|---|---|---|---|
| FIFO | Simple and transparent | A large producer dominates the backlog if no grouping is requested | Out-of-box behavior through one scheduled bucket; also the order within configured keys, not an unscheduled mode. |
| Strict priority + FIFO | Urgent backlog overtakes batch work | Lower classes can starve; no isolation among keys | Evaluation baseline only; the supported strict mode also enforces key fairness. |
| Strict priority + weighted keys | Temporal-like hierarchy | Fair only within a class | Useful when strict urgency is intentional. |
| Fixed weighted round robin | Easy to explain | Bursty service; changing key sets need specified cursor state | Reasonable equal-cost alternative. |
| Stride/virtual-pass scheduling | Deterministic weighted service; explicit persistent state | Dynamic entry, weight changes, and hierarchy require careful semantics | Recommended for unit-grant accounting. |
| Deficit round robin | Handles variable-sized, indivisible service with low scheduler overhead | Requires a defined cost/quantum; fairness is in charged units | Prefer if trusted variable-cost charging is introduced. |
| WFQ/SFQ family | Strong virtual-time foundation | Packet/service assumptions do not imply workflow completion bounds | Use as theory, not an imported runtime SLA. |
| Slurm-like decayed usage score | Reflects historical resource consumption | Needs trustworthy resource measurements; tuning interactions | Future independent resource-fairness layer. |
| DRF | Addresses multiple resource types | Requires enforceable resource vectors and placement | Future resource-allocation layer. |
| EDF or critical-path-first | Helps specific deadline/makespan objectives | Needs trusted estimates/admission; may harm fairness | Optional local/future policy, not entitlement. |
| Random weighted lottery/sketch | Scalable approximate service | Variance, replay/audit complexity, unnecessary approximation at small scale | Not recommended for the first Git-backed implementation. |

DRR's original work specifically notes applicability where service cannot be broken into smaller units, making it relevant to nonpreemptive jobs. Its throughput fairness is not a deadline guarantee. [L9]

Stride scheduling provides deterministic proportional-share allocation, dynamic-client mechanisms, and hierarchical variants. Its paper also distinguishes pairwise error from aggregate absolute error. The exact bounds of its original CPU/time-slice algorithms must not be asserted for a modified hierarchical job-grant scheduler with eligibility filters and caps without a new proof. [L10, sections 2-4][L10]

Demers/Keshav/Shenker fair queueing and Goyal/Vin/Cheng start-time fair queueing supply the virtual-service foundation. Packet length and link-service assumptions are different from unknown-duration, parallel workflow runs. [L11], [L12]

## 7. Proposed normative specification

The words **MUST**, **SHOULD**, and **MAY** below describe the proposed feature, not current behavior. Field names and configuration examples are illustrative API design, not accepted gh-aw syntax.

### 7.1 Mandatory scheduling and policy modes

Every queue MUST have an installed scheduling policy, and every new Claim MUST be created through the trusted scheduler. Scheduling cannot be disabled through configuration, an omitted setting, an API argument, or an operator command.

When policy configuration is omitted for a new queue, trusted initialization MUST install the default `weighted-priority` policy as a transaction in the same canonical log before accepting Work or Claims. Explicit disable values and unrecognized modes MUST be rejected. An existing ledger without a valid current-protocol policy MUST fail; it MUST NOT be silently initialized over old Work or Claims.

One scheduling **pool** is one durable decision domain with a fixed worker-capability class, worker-routing policy, capacity model, and authoritative policy epoch. All dispatchers in that domain MUST use the same policy. Arbitrary agent-chosen filters MUST NOT redefine the competition set.

The first implementation can support the **Git backend** only. If an equivalent queue-wide serialization mechanism is not implemented for Issues, `storage: issues` MUST be rejected for queue operation altogether. A supported backend MUST enforce the full scheduler contract; advisory display, unscheduled FIFO operation, or weakened consistency is not a permitted fallback.

Support exactly these scheduling modes:

| Mode | Selection |
|---|---|
| `strict-priority` | Lowest eligible priority number first, weighted fairness among keys in that class |
| `weighted-priority` (default) | Positive weighted service among eligible classes, then weighted fairness among keys in the selected class |

For `weighted-priority`, a reasonable starting class-weight vector for priorities 1-5 is **8:4:2:1:1**. When all classes are continuously eligible, that targets 50%, 25%, 12.5%, 6.25%, and 6.25% of grants. Empty/ineligible classes lend their opportunities to eligible classes.

These numbers are a suggested policy, not a literature-derived optimum or an SLA. They deliberately exchange absolute urgent precedence for progress opportunities in every class.

#### Queue-like defaults

With no priority/fairness options, the effective policy MUST be:

| Setting | Default behavior |
|---|---|
| Submission priority | `3` for root submissions; children inherit their trusted parent's priority |
| Accounting key | `""` for root submissions; children inherit their trusted parent's key |
| Accounting weight | `1` for the default key |
| Competition | One active priority class and one accounting bucket |
| Next Work | First-committed enqueue position among currently eligible Work |
| Producer grouping | No automatic per-user, per-workflow, per-run, or per-instance buckets |
| Outstanding capacity | The default bucket can use the pool's available reservation capacity; no smaller per-key cap unless explicitly configured |
| Durable authority | Scheduled Claims grant/charge logical slots and identify shared native dispatch reservations in the same log |

For Work A, B, and C accepted in that order, successive grants MUST select A, B, and C while all remain eligible. Other workflows submitting to the same unconfigured queue share that order; the scheduler MUST NOT secretly interleave producers or prioritize frequent/rare producers.

This is FIFO among currently eligible Work by its first committed `(commit ordinal, operation index)` position, derived from the causal log in section 7.6. Claimed, terminal, retry-delayed, or incompatible Work does not block independent eligible Work. Multiple worker slots MAY execute concurrently, so physical starts and completions need not be FIFO. Concurrent or delayed publication can differ from real-world submission time, but clock skew cannot reorder accepted Work.

The virtual-pass mechanism does not introduce reordering in this case: choosing the sole class and sole key is trivial, and the Work comparator determines the grant. Explicit priority or accounting metadata changes the competition set; scheduling itself is never disabled.

Fairness within a class is not automatically fairness for a tenant's total workload across classes. A tenant admitted to several classes can receive service from each. Priority entitlements and a tenant-wide outstanding cap MUST be checked across classes; if a global tenant grant share is required instead, use a different hierarchy, such as tenant -> priority -> Work, and document the changed urgent-precedence semantics.

### 7.2 Immutable Work metadata and trusted policy

Define one new current `QueueCommit` transaction protocol with closed, typed operations for both runtime and operator queues. At the examined revision, version 3 would be the next workflow version, but the final version number MUST be allocated against the implementation branch. Readers MUST accept only the current supported envelope and operation shapes; earlier, unversioned, standalone old facts, and unknown records MUST be rejected without codemods, automatic upgrades, or compatibility aliases.

| Field | Proposed semantics |
|---|---|
| `priority` | Required integer 1-5; a new submission omitting it resolves to 3 before persistence |
| `fairness_key` | Required trusted accounting identity; a new submission omitting it resolves to `""` before persistence |
| `pool` | Required configured scheduling/worker-capability domain |
| `worker_profile` | Approved worker/recipe profile; omitted submissions use the pool's explicit default |
| `payload` | Immutable validated task description, inputs, and optional agent-generated plan |
| `graph_id`, `node_key` | Trusted graph namespace and stable local node identity; neither grants a separate fairness share |
| `depends_on` | Required array of typed Work, Issue, or PR predecessor references; new independent submissions resolve it to `[]` |
| `subject` | Optional typed Issue/PR that this Work operates on, distinct from its dependency conditions |
| `enqueued` | Required immutable nonnegative safe-integer Unix-millisecond timestamp for age diagnostics, not selection order |

When explicit fairness grouping is configured, the accounting key SHOULD correspond to a project, authenticated tenant, or administrator-approved workflow family. It MUST NOT be generated per Work or per retry. Without that configuration or explicit trusted submission metadata, use the single default key; do not infer a group from a producer's identity.

The initial scheduler has exactly two levels: class and accounting key. Workflow-family/instance identifiers MAY be diagnostic payload metadata but MUST NOT add a third entitlement level in this release. `worker_profile` is immutable routing, validated against the pool's compiler-approved targets, not a per-dispatch filter. Work's first committed position is derived by replay, not supplied by an agent or a client clock.

The trusted producer/policy resolves the metadata once. A child's default priority and accounting key MUST inherit from its parent's trusted assignment. A child MUST NOT gain a better priority or a separate entitlement solely because the agent asks for it.

Weights belong in authoritative queue policy, not in each Work's user-supplied metadata. There MUST be one positive integer weight per accounting key within an epoch, shared consistently across its appearances in classes. A simple initial range is 1-1000. Reject zero, negative, fractional, nonfinite, and out-of-range values.

Key length, character set, and registered-key cardinality MUST be bounded and validated. A reasonable initial profile is a maximum of 128 UTF-8 bytes per key and 1024 registered keys per pool/epoch, with explicit administrator-approved increases. These are proposed engineering bounds. Never hash many unrelated tenants into one bucket or evict fairness debt silently.

Idempotent resubmission MUST NOT update priority, key, enqueue age, or pool. Preserve the existing identity-based submission behavior; an explicit metadata conflict SHOULD be reported to the producer rather than represented as a successful update. A future reprioritization operation would require a separate trusted transaction with defined effects on existing reservations.

Defaults apply only at new submission, not while reading an incomplete durable record. Missing required scheduling metadata, a missing policy epoch, or an old transaction version MUST fail validation. There is no legacy Work upgrade, timestamp fabrication, historical tenant inference, routing alias, or automatic migration path. Existing upgrade behavior in [R13] is deliberately not retained.

### 7.3 Eligibility

Before applying shares, the scheduler MUST determine which Work is eligible using fresh authoritative state.

An eligible Work:

- Exists and has state `available`.
- Belongs to the pool and can be executed by a compiler-allowed worker target.
- Satisfies any trusted retry delay and admission restrictions.
- Has an accounting key below its outstanding-Claim limit and an available logical Work slot.
- Can be packed into an approved dispatch group without exceeding profile or worker-dispatch limits.
- Has verified successful results for every Work predecessor.
- Has a fresh trusted satisfying observation for every Issue/PR predecessor.

Within a `(pool, priority, fairness_key)` bucket, choose the eligible Work with the least first-committed `(commit ordinal, operation index)` position. Distinct Work positions are unique. Idempotent submissions preserve the original position, and a cancelled Claim does not reset its Work's position. Claimed and terminal Work do not block another eligible Work.

Eligibility is not authority, and an agent's snapshot cannot establish it at publication time.

The scheduler is **work-conserving relative to these restrictions**: it should not idle while eligible Work and a packable reservation slot exist. Hard caps, incompatible profiles, fixed fair-prefix packing, and closed admission can intentionally leave physical capacity idle. “Borrow unused shares” does not override a hard safety cap or permit skipping the next winner.

### 7.4 Accounting unit

Every newly persisted scheduled Claim has cost **1**, including each member of a multi-Claim assignment. Its Claim identity is the logical reservation handle; its `dispatch_id` links it to a shared native worker reservation. Charge its selected priority class and accounting key once when the containing `QueueCommit` becomes durable. Neither packaging nor partial completion changes that charge.

This unit measures **reserved dispatch opportunities**, including attempts whose launch later fails. It does not measure useful completions. Duplicate intent publication and retries of the same grant identity MUST NOT charge again. A genuinely new attempt/grant is a new unit.

Cancellation, supersession during supported recovery paths, or dispatch failure MUST NOT automatically refund the charge. Refunds would allow repeated cancel/retry cycles to manipulate service history. Definitive nonlaunch or termination can release capacity independently.

Failures SHOULD create trusted retry backoff and an attempt limit so repeatedly failing work does not immediately monopolize its key. Actual launches, completed Work, failures, and consumed resources MUST be reported as separate metrics.

### 7.5 Deterministic hierarchical virtual-pass selection

For each sibling competition set, keep:

```text
V                 monotone parent virtual clock
P[key]            next virtual pass for the key
weight[key]       positive policy weight
active[key]       eligibility state at the preceding committed decision
```

Use an exact integer tick scale for each sibling competition set. Compute `Q` as the least common multiple of the epoch's configured weights, including default weight 1; define `stride[key] = Q / weight[key]`. The bounded integer-weight profile makes the scale finite. Use `BigInt`/Go big integers, rendering diagnostic values as canonical decimal strings. No fractions or duplicate pass-state records appear on the wire.

The scale is computed once per policy epoch and immutable during it. Newly admitted authorized keys with default weight 1 need no scale change. Rational arithmetic remains useful as an independent reference model; the runtime MUST NOT use rounded floating-point reciprocals.

The following is the version-1 rule, including an explicit no-idle-credit policy:

```text
pick(parent, eligible_keys):
    for every key entering eligibility:
        P[key] = max(previous P[key], V + stride[key])
        # For a new key, previous P[key] is absent/zero.

    mark the current eligible set as active
    if the set is empty:
        return none without advancing V

    k = minimum eligible key by (P[key], stable key ordering)
    V = P[k]
    P[k] = P[k] + stride[k]
    return k
```

Passes of keys that remain eligible MUST NOT be recomputed against the new `V` on every selection. Doing so would move an unselected key's goalpost and can starve it. Initialization/reactivation is the clamp point, not ordinary selection.

In weighted-class mode, apply `pick` to eligible priority classes and then to the eligible keys of the selected class. In strict mode, select the least numeric class and apply `pick` only within that class. No additional fairness hierarchy is supported in the first release.

Only the selected path receives a cost increment. Use numeric order for class ties and specified UTF-8 key ordering for accounting-key ties. Preserve the Work tie-breaker independently.

Empty keys do not accumulate an unlimited claim to idle-time service. Reactivation retains any existing future pass but is clamped to the parent's current baseline. Temporary ineligibility caused by a hard cap is treated the same way. This is a deliberate anti-burst rule; it is not the original stride paper's full remaining-pass dynamic-client algorithm.

All eligibility-transition and pass changes MUST be derived while planning the candidate operations. A rejected publication MUST discard those transient changes. Replaying committed Claim operations recreates them exactly; do not serialize a duplicate pass-state record or depend on a dispatcher's remembered active set.

The same exact algorithm and fixtures MUST be used for authoritative runtime scheduling and any scheduler-aware operator implementation. Snapshot prediction uses it without modifying durable state.

### 7.6 One causal transaction log, one atomic envelope

Use **one `QueueCommit` JSON object per line of the existing `work-queue.jsonl`**. Every accepted mutation, not just scheduler decisions, uses this envelope. There is one causal chain and no separate scheduler chain, observation digest, source-fact delta, checkpoint, manifest, or authoritative state file.

| Envelope field | Meaning |
|---|---|
| `version` | Current closed protocol version |
| `id` | Opaque unique identity for this commit |
| `previous` | Predecessor commit identity; `null` only for genesis |
| `request` | Trusted stable request ID, request kind, validated parameters, and semantic fingerprint |
| `actor` | Trusted logical-request origin, including workflow run/originating attempt or authorized operator identity |
| `policy_epoch` | Active policy identity; genesis/policy installation explicitly establishes it |
| `at` | Trusted decision timestamp used for retry eligibility and diagnostics |
| `operations` | Nonempty ordered array of typed operations, accepted atomically |
| `trace` | Optional validated correlation metadata; never authority or a scheduling input |

The request fingerprint binds the actor and the validated logical intent, not the observed branch SHA or tentative selected Work. Reusing a request ID with different meaning MUST fail. A conflict retry may regenerate the tentative operations and predecessor, but must reuse the same logical request. Once committed, its identity and result are immutable.

Keep the logical request's origin fixed across job retries of the same serialized intent. A new publisher attempt belongs in diagnostics/trace metadata, not in a regenerated fingerprint that makes the same request look different. New agent runs/intents receive new handles; recovering an old handle preserves its trusted origin.

Use a small operation union:

| Operation | Durable responsibility |
|---|---|
| `Policy` | Install a policy epoch and bounded approved routing/producer rules |
| `Work` | Admit immutable task metadata/payload; its first commit/operation position is its FIFO position |
| `Claim` | Record selected Work, Claim, `dispatch_id`, and local assignment handle; charge/reserve a logical Work slot and create the group's native reservation on its first Claim |
| `Completion` | Preserve effective-Claim-only terminal Work completion and final authorization prerequisites |
| `Result` | Record a completed node's verified result/availability barrier after its scoped effect processing succeeds |
| `Observation` | Record normalized trusted GitHub Issue/PR condition evidence, including identity and observation time |
| `ClaimCancellation` | Remove this Claim's authority; optionally record trusted retry-not-before metadata |
| `WorkCancellation` | Terminal cancellation; does not pretend the worker has stopped using capacity |
| `Dispatch` | Record launch started, bound run, definitive rejection, or uncertainty for a `dispatch_id` and its immutable Claim array |
| `Release` | Release the group's native reservation once with trusted termination/nonlaunch evidence; logical Claim slots close independently |

Do not repeat the entire Claim array in each dispatch lifecycle operation.
Immutable membership is derived from the admission commit; lifecycle operations
reference `dispatch_id`, its binding/evidence, and stable request identity.

Only a `dispatch_next` request may create new Claims. The replayer verifies that each Claim is the next selection returned by `planNext` for the current prefix, after preceding operations in that same commit. It then applies the implied unit charge and reservation. There is no separately published grant to reconcile with a Claim.

A request can commit several Claim operations, packed into one or more bounded worker assignments. Charge each operation in fair-selection order. The largest packable prefix is bounded by `max_claims`, `max_dispatches`, profile capacity, and separate logical/native outstanding limits. Never skip the next fair winner just to fill a preferred worker group. Claim admission is atomic; subsequent per-Claim finalization is intentionally independent, as specified in section 7.14.

Replay follows `previous` links from genesis and applies operation order. The pre-state of every operation is reconstructible from the predecessor prefix and earlier operations in its commit. Commit ordinal is derived from this chain; it is not a timestamp, a raw file-line index, or a separate global sequence allocator. Therefore physical reordering/deduplication of complete commit records does not change the projection.

Reject multiple genesis records, forks, cycles, missing predecessors, conflicting duplicate IDs/requests, unauthorized operations, invalid policy epochs, or a Claim that does not match the recomputed selection. Exact duplicate commit records are idempotent. Canonical serialization writes the unique chain in causal order.

The current tip, Work positions, passes, charge counts, reservation counts, dispatch state, and request-result index are all disposable replay projections. A dispatcher MAY cache them but MUST recover the same next decision after losing the cache. Compiled configuration proposes a policy; the installed `Policy` operation is authoritative.

This is a breaking protocol refinement, not the old fact union plus extra side records. It retains deterministic projection of the same valid accepted commit set, while making the already serialized queue mutations' causal order explicit. The ADR and TLA+ model MUST be updated; the old model's invariants cannot be assumed to prove this new protocol. [R12], [R15]

`Release` and `Dispatch` lifecycle operations must not reopen terminal Work or create another authorization. Define Work's frozen ownership facts separately from these lifecycle records and preserve single-winner, one-shot authorization per Claim.

### 7.7 Trusted publication and concurrent dispatchers

Keep one pure `planNext(state, pool, as_of)` function, a deterministic prefix packer, and one checked publication path. Process `dispatch_next(pool, max_claims, max_dispatches)`:

```text
capture trusted request identity, fingerprint, and run budget
read current branch version and complete validated ledger
replay the QueueCommit chain and check for this committed request
if already committed:
    return its same Claims and reconcile their launch state; create no new Claims
validate installed policy, routing, capacity, and eligibility
derive a bounded sequence of next selections against a working projection
build one QueueCommit whose Claim operations grant, charge, and reserve
validate the candidate by replaying its predecessor and operations
publish against the captured branch version

on branch conflict:
    discard selected Work, tentative Claims, and transient scheduler changes
    refresh and recompute the entire decision

on an uncertain publication response:
    refresh and look for the stable grant/request identity
    if already committed, return that batch without charging/selecting again
    otherwise regenerate or fail closed with diagnostics
```

The initial selection is not a fixed Work identity carried through retry. This differs intentionally from existing `claimOldestAvailableWork`, the operator `claim` closure, and the current store's supplied-intent retry behavior. [R4], [R7], [R10]

Two dispatchers proposing against the same branch version cannot both publish their candidates. The loser recomputes using the winner's durable service charge and reservation. This is where cross-dispatcher policy enforcement happens.

Once a Claim batch has committed, its selected Work and Claims MUST remain immutable; do not rebind an already granted request on later retries. Recover by stable request/Claim identity, not by choosing a new winner.

Returning a committed request is not permission to relaunch completed, cancelled, or released Claims. Reconcile the batch's current lifecycle first and report its current state. Claim creation and external launch have separate idempotency boundaries.

An evaluation with no Claims need not write an empty commit or advance service state. It returns `no_work`, `capacity_blocked`, or `no_eligible_work` with the inspected tip. Such a response grants nothing and does not consume an idempotency key; a later reevaluation may see different availability. At most one committed nonempty result is permitted per logical request.

Every queue MUST refuse unscheduled new Claims and old writers. There is no legacy mode in which the old direct-claim path remains valid. An explicit Work selector cannot bypass the scheduler. Preserve single-winner ownership and terminal safety as invariants of the new protocol, not by retaining an unscheduled compatibility path.

### 7.8 Dispatch, capacity, and crash recovery

The grant is the scheduler's linearization point, not the GitHub Actions launch. Queue publication and an external dispatch API call are not an atomic transaction.

Keep these derived states distinct:

```text
reserved -> launch requested/uncertain -> run bound -> terminal reconciled -> released
```

A group's first Claim creates its native reservation; all its Claims reference one `dispatch_id`. Record `Dispatch(started)` once for that group before the single API call. Record `Dispatch(bound, run_id)` for the actual worker run, binding every immutable assigned Claim. Retries reconcile the group and must not split it or launch one copy per Claim.

A native reservation remains outstanding while launch is uncertain or its run is queued/running, even after some Claims finish. Individual Completion or ClaimCancellation closes that Claim's logical slot, not the worker slot. Native release requires trusted run termination or definitive nonlaunch. A terminal worker cancels only still-open Claims; it must preserve previously completed Claims.

A dispatcher crash after reservation must leave a recoverable reservation. A crash after GitHub accepted launch but before the returned run ID was durably bound is harder: blindly redispatching can create duplicate runs. Use trusted run correlation, returned run details where supported, and a defined reconciliation procedure. If nonlaunch cannot be established, keep the reservation and fail closed rather than “recovering” by guessing.

When a timeout is observed and the log remains writable, record `Dispatch(uncertain)` with a sanitized reason code. A crash may leave only `started`: offline inspection must label it unbound/possibly in progress, not invent a precise failure cause. Both states retain the reservation until trusted reconciliation establishes binding or nonlaunch.

Disable blind transport retries for the non-idempotent dispatch POST. A timeout after `Dispatch(started)` is an uncertain launch, not a retryable queue-publication conflict. Reuse bounded backoff for safe reads/compare-and-swap conflicts, fail fast on schema/auth errors, and reconcile uncertain writes before attempting another mutation.

Bind the **actual worker run**, not the dispatcher's run. A terminal dispatcher is not evidence that an unbound launch never happened or its worker terminated. Since a worker may activate before the dispatcher persists the returned run ID, trusted activation can perform a bounded read-only wait for `Dispatch(bound)` matching its native run identity. If that binding never appears or names another run, activation fails closed; ordinary outputs remain blocked. The final effect gate MUST also verify this binding. Reliable binding/correlation is a release prerequisite, not an old-API fallback that skips validation.

Do not expire and reuse a slot merely because a lease timed out while its run might still execute. A hard expiration design needs cancellation/fencing or positive terminal evidence.

The hard invariant is about **outstanding scheduler reservations**. It is not automatically an exact bound on physical GitHub jobs when dispatch duplication or multiple jobs per workflow are possible. Runner concurrency remains a separate infrastructure constraint.

Replace the scalar assignment with one fixed inbound Claim array. Preserve admission and final authorization **per Claim** and one native-run binding for the group. A worker may publish multiple Completions, at most one per assigned Claim, with one authorization pass per Claim. The Completion-to-effects crash window remains for each Claim; batching does not establish exactly-once effect delivery.

### 7.9 Proposed API and configuration surfaces

Do not overload read-only sorting with mutation authority. Use staged `work_queue_submit` and `work_queue_dispatch_next` intents, processed in trusted safe outputs. Store task-specific preparation on Work and select Work/target at publication, rather than requiring an agent to supply the winning Work identity. The agentic lifecycle is specified in section 7.12.

Illustrative policy:

```yaml
work-queue-policy:
  mode: weighted-priority
  class-weights: [8, 4, 2, 1, 1]
  accounting-weights:
    project-a: 2
    project-b: 1
  outstanding:
    claims: 8
    per-account-claims: 2
    dispatches: 4
  worker-profiles:
    analysis:
      max-claims-per-dispatch: 3
  dependencies:
    repositories: [example/design, example/library]
    max-observation-age: 60s
```

This is **not currently valid gh-aw frontmatter**. The example explicitly configures
tenant weights, a smaller per-account cap, and allowed foreign dependency reads;
these are not implicit defaults. The 60-second observation age is an example
policy bound, not a measured SLA. Credentials must be bound separately by trusted
compiler configuration. The installed policy epoch in the log is authoritative.
Omitting the block gives queue-like defaults and no foreign-repository grant;
it does not disable scheduling.

Suggested behavior:

| Surface | Required behavior |
|---|---|
| Queue initialization/configuration | Install mandatory default policy when configuration is omitted; resolve to one class/key and oldest-available ordering; reject disable values and unsupported modes |
| Work submission | Stage a validated immutable payload/plan and approved worker profile; trusted processing resolves metadata and preserves identity-based idempotency |
| `work_queue_read` | Display policy, key/class metadata, blocked reasons, and a snapshot prediction; explicitly non-authoritative |
| Existing `sort` | Remain presentation-only; never override policy |
| `work_queue_dispatch_next` | Stage pool, `max_claims`, and `max_dispatches`; commit a fair packable prefix and bind complete Claim arrays to approved workers |
| Explicit Work selection | No direct-claim selector, preferred-Work assertion, arbitrary filter, or target-specific queue bypass in version 1 |
| Administrative priority/weight change | Append an authorized policy/control transaction and follow the same scheduler; no out-of-policy grant override |
| Worker finish | Accept outcome plus an assigned local handle; resolve authority from the immutable array, never accept arbitrary Work/Claim identities |
| Audit/logs | Expose policy epoch, selection reason, blocked reason, grants, launch outcomes, and reservations without leaking submitted identifiers |
| Operator CLI | Use the same QueueCommit codec, selector, and publisher on its configured queue; no direct-claim, legacy-format, or unscheduled bypass |

Pools must define complete target eligibility. Letting each caller restrict the selection to its preferred tenant or worker subset would defeat a queue-wide entitlement policy. If workers have incompatible capabilities, either define separate fairness domains or design resource-aware arbitration across them explicitly.

### 7.10 Policy changes and breaking deployment

For version 1, policy is immutable while the queue contains any nonterminal Work or outstanding reservation. To change it, stop new admission, drain/cancel and reconcile existing work, then append a `Policy` operation establishing a new epoch. Old terminal history remains unchanged.

Initialize the new epoch's derived clocks/passes at zero and compute its integer tick scales from its weights. Epoch transitions are explicit prospective reset boundaries, not hidden resets during competition. Reject a policy change on a non-drained queue with `policy_not_quiescent`.

This deliberately removes live weight rebasing, mid-backlog mode changes, and hierarchy edits from the first release. If such features are later required, they need separately specified service-debt and in-flight semantics; do not grow them into the initial scheduler.

Initial deployment is a breaking replacement, not a compatibility rollout:

1. Stop old writers and terminate or reconcile old in-flight workers before deployment; do not mix protocols on one authority.
2. Deploy current-only readers, closed schemas, mandatory scheduling, reservation recovery, and backend validation together.
3. Initialize new current-protocol queues with a policy transaction in their canonical logs; do not convert or silently adopt old ledgers.
4. Recompile and deploy every dispatcher, operator client, and worker sharing each pool against the current protocol.
5. Permit queue operation only when its backend, policy epoch, and transaction versions satisfy the complete contract.

There is no runtime migration or backward-compatible reader for old queues. Encountering an old ledger MUST produce an explicit unsupported-protocol error and leave it unchanged; it must not erase, reset, reinterpret, or silently copy existing work. Do not rewrite committed current-protocol grants or retrospective fairness measurements. Missing policy, unsupported transaction/policy versions, and mismatched compiled policies MUST fail closed.

Compaction must preserve the complete causal commit chain and all operations, including ownership, charge history, request idempotency, dispatch bindings, and releases. Removing unique old commits can change future selections or lose an uncertain launch. Version 1 compaction only removes exact duplicate commit records and writes causal order; checkpoints/retention are outside scope.

### 7.11 Guarantees and non-guarantees

**Required guarantees**

- Deterministic replay of authority and scheduling state.
- Mandatory scheduling and installed policy for every operational queue; no configuration or command bypass.
- One charge/logical reservation per unique Claim; one native reservation per dispatch group.
- Fresh policy-derived selection at every successful grant/Claim publication.
- No scheduling fork or unscheduled Claim bypass.
- Positive-weight service opportunities for continuously eligible keys under the assumptions below.
- Work-conserving selection relative to eligibility and configured hard limits.
- Ownership, terminal-state, and external-effect safety unchanged.

**Assumptions for eventual service**

There are finitely many competing eligible keys, fixed positive weights during the interval, recurring dispatch opportunities, successful publication eventually, and eventual release of capacity. The key stays eligible, and the relevant class has positive share or eventually wins under strict priority. FIFO uses committed position, so producer backdating cannot overtake accepted Work.

Without those assumptions, “no starvation” is not an unconditional liveness theorem. In strict mode, continuous higher-class demand is a direct starvation counterexample. An individual Work can also wait behind other same-key Work; a key's share alone is not a per-item latency bound.

**Explicit non-guarantees**

- Global fairness across independent repositories, queues, or worker pools.
- FIFO by real-world arrival time despite delayed publication or clock skew.
- Equal CPU, token cost, walltime, completions, or useful outputs.
- Immediate priority inversion of already running work.
- Completion ordering or deadline satisfaction.
- Exactly-once agent execution, workflow launch, or external-effect delivery.
- Hard real-time waiting bounds without capacity and execution-time bounds.

### 7.12 Agentic dispatchers on a deterministic fair protocol

The dispatcher remains an agent, not a scripted fairness loop. Its job is to identify useful tasks, gather context, prepare plans, choose an approved worker recipe, decide when a bounded batch is useful, and react to observed failures/backpressure. The trusted protocol owns admission, accounting identity validation, selection, Claim creation, launch binding, and release.

| Agent may decide | Protocol must decide |
|---|---|
| What useful Work to propose and how to deduplicate it | Whether the submission is valid, authorized, and new |
| Task description, evidence, and an immutable execution plan | Payload/input validation and approved worker-profile routing |
| A permitted priority/accounting request | Resolved priority/key under the trusted producer policy |
| When to stage a batch and its desired bounded size | Actual batch size, global/run budget, capacity, and which Work wins |
| What to do next after a blocked/partial/failed batch | Whether a prior request already committed or a launch is uncertain |
| How an assigned worker solves its task | Claim ownership, completion, and external-effect authorization |

#### Minimal MCP intent surface

These proposed tool names are not currently implemented:

| Tool | Semantics |
|---|---|
| `work_queue_read` | Read the activation snapshot with bounded Work lists, counts, provenance, and prediction |
| `work_queue_explain` | Explain a candidate/Claim using that same snapshot; never claim fresh durable state |
| `work_queue_submit` | Stage payload, approved worker profile, and optional permitted metadata |
| `work_queue_dispatch_next` | Stage `{pool, max_claims, max_dispatches}`; accept no Work selector, filter, authority, or trace override |
| `work_queue_claim_finish` | Stage `{claim_handle, outcome}` for one member of the trusted inbound array |

The agent-side MCP server continues to read its immutable activation artifact and write only staged-intent files. It receives no queue Git credentials and never publishes a `QueueCommit`. Trusted safe-output processing binds staged intent handles to stable request IDs/fingerprints; retries reuse those handles, while different logical requests have different IDs.

Staging returns `{intent_id, status: "staged"}`, not a Claim or an effective assignment. A read/pending preview MUST distinguish `activation_snapshot` from `staged_preview`; pending submissions and dispatch requests never confer authority. A queue prediction can be stale without being an error.

An agent can request another batch, but cannot evade limits by emitting many small requests. Apply the existing compiler's dispatch budget across the run, and enforce outstanding pool/account limits and request idempotency during trusted publication. Reuse the existing allowed-worker and per-run dispatch-count concepts rather than create a parallel privilege system. [R21]

#### Late-bound execution, not stale winner-specific inputs

Store task-specific inputs and plans on the immutable Work record. After the scheduler selects Work, the trusted binder constructs the compiler-managed assignment from **that selected Work's payload and approved profile**.

Never attach a preparation for snapshot Work A to `dispatch_next` and then reuse it when fresh selection chooses Work B. Common execution behavior belongs to the approved worker profile; task-specific behavior belongs to Work. An approved profile may itself be agentic and use the stored plan as guidance, not as a mandate to perform unauthorized writes.

Producer/accounting scope is validated at submission, with priority 3/default key when nothing explicit is requested. Agents cannot invent new entitlement by submitting per-run keys or choosing a different pool on each dispatch. Worker capabilities define fixed pools; selecting a profile at Work admission is validated routing, not permission to filter competitors at grant time.

#### Normal workflow

The following example assumes the compiler permits a three-Claim batch:

```text
activation:
    capture queue snapshot and trace/caller context

dispatcher agent:
    inspect snapshot and explain predicted next Work
    identify new useful tasks
    stage submissions with their own plans and approved profiles
    stage dispatch_next(pool="default", max_claims=3, max_dispatches=1)

trusted safe outputs:
    admit validated submissions in explicit intent order
    evaluate dispatch_next against the current durable prefix
    atomically commit up to 3 scheduled Claims under limits
    bind each selected Work's own payload
    pack compatible Claims into one approved worker assignment
    record its launch marker, dispatch once, and bind the returned run identity
    produce a request/Claim receipt

workers:
    accept a bounded trusted Claim array with local handles
    solve assigned tasks and stage finish/effects for each handle independently
    finalize each Claim independently; unfinished handles do not authorize effects

next dispatcher activation:
    inspect durable progress and prior receipts
    adapt task generation, plans, or bounded demand
```

There is no promise that the dispatcher can observe a durable grant during the same agent turn: publication happens later in safe outputs. Persist the derived receipt as a workflow artifact/step summary and expose it to subsequent activations. A same-turn durable acquire-and-respond API would require a different trusted orchestration boundary and is outside this design.

Queue workers MUST reject unassigned execution; an ordinary dispatch cannot bypass the Claim requirement. Dispatcher workflows need no inbound Claim merely to propose queue intents, but cannot use that role to execute a queued Work's effects outside its assigned worker.

### 7.13 Explainability, receipts, and tracing

#### One explanation algorithm

`planNext` SHOULD return a bounded explanation alongside its candidate: source tip, policy epoch, eligibility counts, selected class/key, integer pass/stride comparison, first-committed FIFO position, and capacity before/after. The explanation and authoritative selection MUST come from the same function. Do not create an independent “approximately why” ranking for the UI.

Diagnostic pass values, candidate matrices, and reason prose are derived, not extra authoritative log records. `QueueCommit.previous`, its operations, its request, and its decision time are sufficient to reproduce the explanation offline. Operation index disambiguates several Claims in one commit.

Use stable reason/status codes:

| Code | Meaning |
|---|---|
| `fifo_default` | Sole default class/key; first eligible committed Work |
| `priority_class` | Strict policy selected the most urgent eligible class |
| `weighted_class` / `weighted_key` | Integer virtual-pass comparison selected the class/key |
| `no_work` | No nonterminal unclaimed backlog to consider |
| `no_eligible_work` | Backlog exists but is delayed, incompatible, or otherwise ineligible |
| `dependency_wait` / `dependency_failed` | A Work predecessor has no successful Result, or an ancestor was terminally cancelled |
| `dependency_result_unavailable` | Completion exists but verified outputs/Result do not |
| `external_wait` / `external_unavailable` | An Issue/PR condition is unsatisfied or cannot be read/validated freshly |
| `capacity_blocked` | Outstanding limits prevent a grant |
| `partial_batch` | Fewer Claims than requested committed; include the limiting reason |
| `request_replayed` | Return an already committed request; no new charge or selection |
| `publish_conflict` | Tentative proposal lost; selection must be regenerated |
| `launch_uncertain` | Launch marker exists without trustworthy binding/nonlaunch evidence; retain capacity |
| `policy_not_quiescent` | Policy edit attempted before the queue drained |

Administrative/schema/auth errors, malformed history, exhausted publication retries, and uncertain launch are not success-shaped empty queues. Return their explicit error/status and preserved state. Backpressure is a legitimate evaluation result, but must not be labeled `no_work`.

#### Minimal durable-to-human receipt

Each processed dispatch intent produces a bounded derived receipt:

```json
{
  "request_id": "r_example",
  "source_commit": "q_before",
  "commit_id": "q_granted",
  "policy_epoch": "p_example",
  "requested_claims": 3,
  "granted_claims": 2,
  "requested_dispatches": 2,
  "reserved_dispatches": 2,
  "status": "launch_uncertain",
  "grant_status": "partial_batch",
  "limiting_reason": "capacity_blocked",
  "claims": [
    {"claim_ref": "c_1", "work_ref": "w_1", "dispatch_id": "d1", "finish": "open"},
    {"claim_ref": "c_2", "work_ref": "w_2", "dispatch_id": "d2", "finish": "open"}
  ],
  "dispatches": [
    {"dispatch_id": "d1", "claim_refs": ["c_1"], "status": "bound", "run_id": "101"},
    {"dispatch_id": "d2", "claim_refs": ["c_2"], "status": "launch_uncertain", "run_id": null}
  ]
}
```

IDs above are illustrative. A real receipt includes the authoritative claim-commit references and observed launch-state tip, because later recovery may update the launch result. Distinguish requested, granted, API-accepted/bound, completed, and released counts. A Claim may be granted without a launched worker.

Batch grant status and overall execution status are separate. A partial grant with an uncertain launch MUST expose `launch_uncertain` as the overall outcome; it is not a successful partial batch that merely hides the error in an item.

Normal output should be a few lines: batch outcome, limiting reason, committed request/Claim references, and worker-run links. Detailed comparisons and per-attempt retry output belong in structured JSON/collapsible diagnostics. Always escape submitted identifiers/text; reuse the existing work-queue rendering discipline. [R20]

Proposed operator commands, all read-only:

```text
gh aw work-queue explain --request-id REQUEST --json
gh aw work-queue explain --before-claim CLAIM --json
gh aw work-queue trace --claim-id CLAIM --json
```

`--before-claim` replays the predecessor and earlier operations within its commit. Operator live reads and agent snapshot reads use the same explanation engine but MUST label their different provenance. Every page/result reports its commit tip and evaluation time; paginate candidate detail rather than dump the whole log into an agent context.

#### Reuse existing trace context

The repository already has `otlp.logSpan` for custom spans, a sanitized local OTLP JSONL mirror, and nonfatal export handling. `buildAwContext` already propagates `otel_trace_id`/`otel_parent_span_id` into dispatched workflows. Reuse those facilities instead of introducing a queue-specific tracing service or requiring an OTLP backend to debug a grant. [R17], [R18], [R19]

Use phase spans such as `work-queue.schedule.run`, `work-queue.publish.run`, `work-queue.launch.run`, and `work-queue.reconcile.run`, consistent with the existing helper's naming. Propagate the current trusted workflow context into the worker; include request/commit/Claim correlation in its compiler-managed assignment.

The current `logSpan` helper returns no span context. Version 1 therefore should not promise that the worker is parented to the exact grant span. Reuse the existing setup-span parent and correlate phase/worker spans through queue IDs. A batch worker carries all assigned Claim references, so one run trace can contain separately attributable Claim finalizations/effects. Optional cross-trace span links can connect enqueue and later dispatch episodes when supported, but are not necessary for offline explainability. OpenTelemetry distinguishes parent relationships, events, and links for asynchronous causality. [O1]

| Correlation field | Use |
|---|---|
| `request_id` | Stable logical intent across publication retries |
| `commit_id`, `previous`, operation index | Exact accepted decision and its pre-state |
| `policy_epoch`, pool, priority | Applied policy and domain |
| `claim_ref`, `work_ref` | Opaque/redacted diagnostic references, not arbitrary submitted strings |
| `dispatch_id`, local assignment handle | Shared worker launch versus individual Claim completion |
| Graph/node/edge and observation/result references | The exact prerequisite evidence or blocking path used by the decision |
| Dispatcher/worker run ID and attempt | Trusted provenance and native GitHub run navigation |
| Existing trace ID, episode/hop context | Cross-job correlation when telemetry is enabled |
| Attempt number, observed Git SHA, result code | Tentative conflicts/errors, separate from accepted decisions |

A branch SHA identifies the publication snapshot; a queue commit ID identifies a durable protocol decision. Keep both, and do not treat span IDs as ownership or idempotency keys. An agent must not set any of these authority/provenance fields.

Trace export and local diagnostic files are derivative. Missing tracing must not reset passes, lose request idempotency, release capacity, or change authorization. Reports SHOULD mark trace availability explicitly; all accepted decisions remain explainable from the queue log alone.

Keep metrics low-cardinality: counts/latencies by configured pool, priority, mode, and result code, not per Work/Claim/request/tenant label. Identifiers belong in bounded receipts/traces, with redaction, and sensitive payloads, plans, or secrets must not be exported. Go debug logging uses `pkg/logger`; JavaScript should reuse existing work-queue diagnostics and OTLP helpers.

### 7.14 Multiple Claims per worker, independent completion

Batching is part of the current protocol, not a scalar-assignment compatibility layer. The compiler-managed input is `work_queue_assignment`: a bounded Claim array, shared `dispatch_id`, trusted run binding, and request/commit provenance. Reject scalar assignments and agent-provided replacements.

```json
{
  "dispatch_id": "d_example",
  "claims": [
    {"handle": "h1", "claim_id": "c1", "work_id": "w1", "work": {"plan": "task one"}},
    {"handle": "h2", "claim_id": "c2", "work_id": "w2", "work": {"plan": "task two"}},
    {"handle": "h3", "claim_id": "c3", "work_id": "w3", "work": {"plan": "task three"}}
  ]
}
```

The sketch omits other required provenance fields for readability. Handles are generated by trusted processing and scoped to this immutable assignment. `work_queue_claim_finish({"claim_handle":"h1","outcome":"completed"})` identifies one assigned Claim, not the entire dispatch. Repeating the same outcome is idempotent; conflicting outcomes fail explicitly. Unknown handles, arbitrary Claim IDs, or handles from another assignment cannot authorize a mutation.

#### Fair admission before packing

First select each Claim with `planNext`, then pack it into the earliest compatible open group, or open a group if the dispatch budget permits. Compatibility includes approved profile, ref, execution/security scope, and maximum Claims per dispatch. Group membership and local handles become immutable in the Claim commit.

If the next fair winner cannot fit and no new dispatch is permitted, stop the prefix with `dispatch_budget_blocked`; never skip it to fill a preferred profile. For example, profile order X/Y/X with a one-dispatch budget admits the first X only, not both X tasks. A compatible A/B/A fairness sequence can share one worker and is still charged three Claims, not one launch. Workers process assignment order by default, but may finish independently or in parallel.

Track distinct limits and metrics: outstanding Claims per pool/account, outstanding worker dispatches per pool, per-request Claim count, per-run launch count, and per-profile assignment size. Closing a Claim frees its logical slot; finishing one Claim cannot release the shared native slot or erase the others.

#### Individual finalization and effects

Every worker-generated external mutation MUST carry an explicit `claim_handle`. A transient global “current Claim” switch is not sufficient: concurrent preparation could associate an output with the wrong Claim. Read-only diagnostics may be unscoped; queued Work effects may not. The trusted processor resolves the handle against the inbound array and validates the actual worker run and effective ownership for that Claim.

Finish calls stage individual intents during the agent phase. Trusted safe-output processing publishes each Claim's Completion or ClaimCancellation in its **own checked commit**, then independently gates its scoped effects. There is no “all Claims finished” commit barrier. Staging is not durable completion: progress survives failure once its Completion is published, not merely when the agent calls the tool. Immediate durable acknowledgements during the agent turn would require a separate trusted execution boundary and are not claimed here.

| Per-Claim state | Trusted action |
|---|---|
| Effective + completed intent | Persist Completion, verify it, and authorize only this handle's effects once |
| Effective + cancelled intent | Cancel this Claim, close its logical slot, and execute none of its effects |
| Effective + no finish intent at wrap-up | Cancel this Claim only; no implicit completion or effects |
| Losing/terminal/invalid handle | Reject or stage that handle's effects; never borrow another Claim's authorization |
| Reconciliation uncertain | Fail closed for unverified effects; preserve already committed Completions |

Admission can mark individual members ineligible without discarding unrelated effective members, provided the shared native binding is valid. A globally invalid assignment/binding fails the entire worker. After some Claims finish, a crash or terminal-run recovery cancels only still-open members and releases the native slot with terminal evidence. It cannot reopen or roll back completed Work.

For assignment `[c1,c2,c3]`, completing c1, cancelling c2, and leaving c3 unfinished produces one terminal Work completion, two Claim cancellations, and effects only for c1. If failure occurs after c1's Completion but before processing c2/c3, c1 remains completed and recovery settles only c2/c3. The existing Completion-to-effects crash gap applies independently to c1.

The formal invariant is now **at most one Completion and one authorization pass per Claim**, not at most one Completion per worker run. Effect records are bound to `(actual worker run, Claim)`; completing c1 must never authorize c2's writes. Trace and receipt views expose both group-level launch/binding and per-Claim finish/effect state.

### 7.15 Native Work DAGs and verified result barriers

The queue is a DAG scheduler, not an agent-maintained checklist. It tracks Work
ownership separately from readiness:

```text
admitted node -> waiting for predecessors -> ready frontier
             -> fair Claim -> worker -> Completion
             -> scoped effects verified -> Result -> release successors
```

The existing ownership states remain useful: a waiting node can be `available`
but not `ready`. Reads MUST expose both state and readiness, unresolved edges,
and a bounded blocking-path explanation. Waiting nodes consume neither a Claim
charge nor a worker slot. Their age/position remains unchanged when they become
ready; already claimed nodes do not block independent ready nodes.

#### Atomic graph admission

`work_queue_submit` MAY stage one node or a bounded array of graph nodes. Trusted
processing resolves local node handles to deterministic Work IDs scoped by
`(graph_id, node_key)`, validates all references, and commits the graph extension
atomically. Resolve forward references within the same submission before checking
the DAG; do not require clients to find a fragile submission order.

All Work edges MUST reference an existing node in that graph or one included in
the same admission commit. Reject missing nodes, self-edges, duplicate node keys,
inconsistent idempotent resubmissions, and cycles with a concrete cycle path.
Persist resolved immutable edges on each Work. Adding new nodes is allowed;
rewiring an existing node, silently replacing its dependencies, or accepting a
promise that a missing predecessor will be submitted later is not.

Idempotency is scoped to the graph/node identity and its immutable definition,
not a global payload-only hash: identical task payloads in distinct nodes may
have different predecessors. Repeating the same node preserves its admission
position and must not reset priority, dependencies, result, or accounting debt.

One graph stays in one queue/pool in the first release. Different profiles can
execute its ready nodes, but they share the graph's authorized accounting
identity unless policy explicitly authorizes otherwise. Graph IDs, node counts,
and fan-out do not create new entitlement. Queue-to-queue Work dependencies and
an additional fairness level are outside scope; external GitHub references are
supported as described below.

#### Fork, join, and data flow

For `prepare -> {analyze-a, analyze-b} -> summarize`, the root is initially ready,
the two analyses become ready after `prepare` publishes its Result, and the join
waits for **both** analysis Results. The analyses may share one worker assignment
when profiles/limits permit. A parent and its not-yet-ready child cannot be
speculatively claimed together merely to fill a batch.

An agent can construct the DAG, choose plans/profiles within policy, and add
new downstream nodes after examining results. It cannot waive predecessors,
grant readiness, or substitute a snapshot's predicted node for the fresh winner.
The trusted binder injects only the selected node's declared predecessor result
references, alongside its immutable payload and assignment handle.

Results use bounded, validated data or immutable artifact references with native
run provenance and digests where available. Do not put secrets or large result
blobs in the transaction log. Missing/deleted/unreadable artifacts are explicit
input-resolution errors, not an empty successful result. A result descriptor is
immutable; conflicting re-publication fails.

#### Completion is not yet dependency success

Completion protects ownership **before** ordinary safe-output effects execute.
Therefore it cannot alone release a dependent. Trusted processing appends
`Result` only after that Claim's scoped effects pass succeeds and its declared
outputs are available. A successful no-write task still passes the verified
result step; it may publish an empty result descriptor.

If a worker crashes after Completion but before effects/result verification,
the node remains completed but its successors report
`dependency_result_unavailable`. They do not run on nonexistent outputs. If
verified outputs were delivered but only the Result publication failed, trusted
recovery MAY append the same Result after independently re-verifying its evidence;
it must not infer delivery from Completion or replay external writes blindly.

Within a multi-Claim worker, publish successful members' Results independently.
One failed/unfinished member blocks only successors requiring that member, not
an unrelated completed branch. The native worker slot remains reserved until
its run terminates, even when its Results already unblock later dispatches.

WorkCancellation causes dependent readiness to report `dependency_failed`,
including the ancestor path; it does not pretend success or silently cancel all
descendants. Retryable ClaimCancellation leaves the parent Work available.
Permanent attempt exhaustion must use an explicit trusted WorkCancellation.
Blocked descendants remain inspectable until explicitly cancelled or new
replacement nodes are admitted; terminal histories/edges are never rewritten.

### 7.16 First-class cross-repository Issue and PR dependencies

The DAG has two vertex types: schedulable Work nodes and externally observed
GitHub resource gates. An Issue/PR can also be a Work node's `subject`, but linking
that subject does not mean “wait until it is closed.” Dependencies carry explicit
conditions; subjects identify what the task operates on.

If queued Work creates a new Issue/PR, its verified Result supplies the typed
resource identity. An agentic dispatcher can then admit downstream nodes using
that identity. Version 1 does not guess future numbers or accept an unresolved
resource placeholder as an already-ready dependency.

Typed example:

```json
{
  "graph_id": "release-check",
  "node_key": "publish",
  "subject": {
    "kind": "pull_request",
    "host": "github.com",
    "repository": "example/app",
    "number": 42
  },
  "depends_on": [
    {"kind": "work", "node_key": "build"},
    {
      "kind": "issue",
      "host": "github.com",
      "repository": "example/design",
      "number": 7,
      "condition": "completed"
    },
    {
      "kind": "pull_request",
      "host": "github.com",
      "repository": "example/library",
      "number": 81,
      "condition": "merged"
    }
  ]
}
```

These repositories/numbers are illustrative. Work edges resolve inside the
graph; Issue/PR edges can target other explicitly allowed repositories. Normal
UI views render fully qualified clickable resource links and preserve the
Issue-versus-PR type even though GitHub shares their number namespace.

#### Explicit condition semantics

Version 1 supports a deliberately small condition set:

| Vertex / condition | Satisfying trusted evidence |
|---|---|
| Work predecessor | Its effective Claim completed and its verified Result was published |
| Issue / `completed` (default) | Exact Issue resource is closed with `state_reason: completed` |
| Issue / `closed` (explicit) | Exact Issue resource is closed, regardless of closure reason |
| PR / `merged` (default) | Exact Pull Request API resource reports `merged: true` with merge provenance |

An Issue closed as not planned/duplicate does not satisfy `completed`; a PR
closed without merging does not satisfy `merged`. Missing/null fields needed
for the chosen predicate are `unknown`, not successful compatibility defaults.
Comments, task-list checkmarks, labels, a commit message mentioning the number,
or an agent saying “this is done” do not establish these predicates. GitHub's
Issues endpoints can return PRs, so the resolver MUST validate resource type and
use the Pull Request endpoint for merge status. [G2], [G3]

External vertices are gates, not synthetic Claims: they incur no service charge
or runner reservation. They are first-class in read/explain/trace views and have
their own readiness, observation identity, and failure reason.

Issue/PR dependency support is distinct from `storage: issues`. It does not move
queue authority into resource bodies/comments: scheduling, edges, results, and
observations still use the same canonical Git transaction log.

#### Stable identity and permissions

At admission, a trusted resolver validates the configured host/repository,
positive number, resource type, immutable repository/resource IDs, and access
scope. Store display coordinates alongside the resolved IDs. A rename/redirect
may be followed only within approved host/repository scope and must preserve
the expected identity. A transfer, mismatched type/ID, or newly unauthorized
destination produces an explicit error; do not silently retarget the edge.

Use opaque node IDs or lossless canonical strings for persistent identities,
not JavaScript floating-point coercion of arbitrary API integer IDs. The gate's
identity includes its predicate: `issue/completed` and `issue/closed` are distinct
conditions even when one authenticated resource read can service both.

The compiler's dependency-read allowlist and credential mapping govern cross-repo
access. Prefer read-only, repository-scoped GitHub App installation credentials
for foreign private repositories. The queue repository's `GITHUB_TOKEN` must not
be assumed to grant access to other repositories. Keep Git queue publication
credentials separate from foreign metadata-read credentials. Never give either
to the agent's snapshot MCP process. [G4]

A cross-repo reference grants neither foreign mutation permissions nor
cross-repo worker dispatch rights. Existing approved-worker and safe-output
permission boundaries remain in force. Unknown/deleted/inaccessible resources,
401/403/404, rate limits, network failures, and host identity errors never imply
“closed” or “merged.”

#### Observations in the same transaction log

Trusted readers normalize GitHub metadata into an `Observation` operation:
resolved resource identity, condition, `ready`/`waiting`/`failed`/`unknown`,
observation timestamp, source update time/version where available, and sanitized
read-status/evidence fields. Store only fields needed for the predicate, not full
bodies, comments, tokens, or an agent's reasoning.

Readers deduplicate shared resource checks across nodes. Events may wake a
dispatcher, but webhook/agent payloads are hints: re-read the exact authorized
resource before recording authoritative readiness. Every accepted observation
is in `work-queue.jsonl`; no label, webhook cache, or external poller's memory
becomes a second authority.

Before admitting Claims with external edges, trusted processing refreshes their
conditions and binds the accepted observation IDs into the decision. It MAY
reuse a trusted observation within the installed bounded
`max-observation-age` policy only if there is no known invalidation; validate
identity/condition/age on every use. On a failed required refresh, record
`unknown`/a sanitized failure and block that node rather than reuse a success
indefinitely. A CAS conflict regenerates the decision and rechecks the freshness
budget; observations and their selected Claims can share one atomic commit.

Offline replay recomputes readiness from those logged observations and the
decision timestamp; it does not call GitHub. Explain views show exactly which
Issue/PR observation each Claim used and which edge currently blocks a node.
If dependencies are unreadable, unrelated ready nodes may still proceed, but
receipts must surface `external_unavailable`, not claim the whole queue is empty.

#### Mutable resources and guarantee boundary

Issues can reopen and previously failed PR gates can later change. New trusted
observations supersede earlier gate readiness for **future** admission; they do
not rewrite past decisions or retroactively revoke an assigned Claim. A gate
reported `failed` means its observed condition was not met, not that every
dependent Work was automatically terminally cancelled.

This is admission against bounded-age authoritative observations, not an atomic
transaction spanning GitHub repositories and the queue branch. The resource
can change immediately after it is read. Work that requires a stronger
execution-time condition must revalidate that domain condition before its
effects; queue assignment alone does not promise a remote Issue will remain
closed. Trace the observation time and policy age rather than imply global
real-time consistency.

Cycle validation covers immutable Work edges. The queue cannot statically prove
that an agent's future external effect will not create an indirect wait cycle,
such as Work waiting for an Issue closure that only that same Work would cause.
Use dependency subjects/blocking-path diagnostics to make such deadlocks visible;
do not execute a blocked node to “resolve” its own prerequisite.

## 8. Validation and evaluation plan

### 8.1 Small design experiments performed for this report

Disposable Python models exercise the unit-grant rule, first with exact `Fraction` arithmetic and then with the equivalent integer tick scales in section 7.5. These are checks of the proposed selection mechanism, **not tests of the repository runtime or proof of distributed correctness**.

| Scenario | Observed result |
|---|---|
| Three continuously eligible keys, weights 5:3:2, 10,000 grants | 5,000 / 3,000 / 2,000 grants |
| Five continuously eligible classes, weights 8:4:2:1:1, 16,000 grants | 8,000 / 4,000 / 2,000 / 1,000 / 1,000 grants |
| Class 3's 2,000 grants split among 5:3:2 keys | 1,000 / 600 / 400 grants |
| Equal-weight new key joins after 1,000 single-key grants | Next eight grants alternate; no 1,000-grant historical catch-up |
| Equal-weight idle key returns after 1,000 other-key grants | Next eight grants alternate; no accumulated idle credit |
| Restore clock, passes, and active set after 17 grants | Next 100 selections identical to uninterrupted model |
| Equal grant counts, job durations 60:1 | Resource-time split 98.36% / 1.64% |
| Causal replay with client timestamps deliberately out of order | First-committed A still precedes B; timestamp backdating does not overtake |
| Snapshot predicts A, another request claims A, then this request grants B | B carries B's stored plan, not A's prepared inputs |
| Reorder physical commit records and add an exact duplicate | Same causal projection, request result, and unit charges |
| Inject a Claim for a Work other than the prefix's selected winner | Rejected by the disposable replay model |
| Release an unbound launch using the dispatcher's terminal run | Rejected; binding the actual worker run permits the modeled release |

The causal/late-binding checks use a small disposable model, not a complete implementation of actor authorization, all operation variants, or external APIs. These results support the example ratios and basic prefix semantics. They do not establish a short-window error bound under changing eligibility or solve real publication/dispatch races. Live weight changes are deliberately excluded from version 1.

### 8.2 Required automated acceptance cases

The successor model is [`FairWorkQueue.tla`](FairWorkQueue.tla), integrated into
[`check.sh`](check.sh). Before the DAG extension, commit `f50550e8dc`'s four positive
configurations exhausted 9,612 distinct states in total on 2026-10-05; five
deliberate negative controls and two
guarded witnesses returned exactly their expected results. In particular, one
worker can hold several Claims and reach a state where one is completed while
another is open, without authorizing that open Claim's effects. The updated suite
adds ready-frontier, verified Result, cycle, and Issue/PR observation checks:
eight positive configurations exhaust 157,130 distinct states; ten negative
controls and three guarded reachability witnesses have named expected outcomes.
The diamond witness uses a successful-action subset rather than exhaustive
diamond failure interleavings. See the
[verification scope and reproduction instructions](README.md#successor-mandatory-fair-scheduling-and-batched-workers).

**The complete combined suite is not fully verified.** The additional
`FairDAGGitHub` composition search and unchanged original `QueueOrdering`
search were stopped before exhaustion after 588,795 and 109,949,148 distinct
states respectively. Neither reported a violation, but that is not a pass.
Their original bounds, configurations, logs, and checkpoints remain available.
The exhausted cases and explicit negative/witness outcomes are the verified
scope of this change.

| Area | Acceptance criterion |
|---|---|
| Mandatory initialization | Omitted new-queue configuration installs the default policy; existing policy-less ledgers are rejected unchanged |
| Default FIFO behavior | Without priority/key configuration, Work committed A/B/C is granted A/B/C; client timestamps and producer identity do not change ordering |
| Default concurrency | Claimed/blocked Work is skipped, the sole bucket may use pool capacity, and out-of-order completion does not alter the next eligible grant |
| No opt-out | Disable values, unscheduled FIFO modes, advisory-only modes, and direct-claim bypasses fail explicitly |
| Current-only protocol | Old, unversioned, unknown, and incomplete durable records fail; no automatic upgrades or metadata fabrication |
| Submission defaults | New Work resolves priority/key defaults before persistence; persisted records contain all required scheduling fields |
| Fixed-set proportionality | Exact reference-model sequences and counts match for unit costs; include 1:1, 5:3:2, and highly skewed weights |
| Strict priority | No lower-class grant while a higher class is eligible; include the explicit starvation trace |
| Weighted priority | Positive-share classes receive their reference-model service with all classes backlogged |
| FIFO within a bucket | First accepted eligible Work wins by causal commit/operation position; duplicate submission preserves position; timestamps cannot reorder it |
| Dynamics | Join, empty/refill, cap/release, resource incompatibility, and retry-delay transitions preserve specified debt rules |
| Restart | Serialize/replay at every decision prefix; future selections are identical |
| Cross-language parity | JavaScript and Go compare exact canonical passes/keys identically, including non-ASCII ordering where allowed |
| Competing dispatchers | Only one same-version proposal commits; loser refreshes and changes selection as required |
| Uncertain push result | A committed request identity is found and returned once; no second charge |
| Writer enforcement | Old writer, explicit unscheduled Claim, and administrative grant-bypass paths fail for every queue |
| Capacity | Logical open-Claim and native dispatch reservations independently remain within their limits; partial finish does not free a live worker slot |
| Recovery | Definitive no-launch/terminal evidence releases exactly once; uncertain launch and still-running lease expiry do not |
| Launch fencing/binding | Persist start marker before POST, do not blindly retry uncertain POSTs, bind actual worker run, and reject effects without a matching trusted binding |
| Trust | An agent cannot create keys, improve inherited priority, reset debt, alter the policy, or forge releases |
| Worker safety | Losing/admission-failed workers cannot authorize ordinary safe outputs |
| Commit replay | Replaying each causal predecessor/operation prefix reconstructs the selection and explanation; forks/missing parents/invalid Claim choices fail |
| Compaction | Permuting/deduplicating complete commit records preserves projection, request results, and future selections |
| Backend gating | Any backend lacking scheduler serialization is rejected for queue operation; no advisory/unscheduled-FIFO fallback |
| Policy epochs | Reject changes while nonterminal Work/reservations exist; drained epoch transition resets derived ticks explicitly and preserves old history |
| Late binding | If snapshot predicts A but trusted processing selects B, launch B with B's stored plan/profile; never carry A's inputs forward |
| Agentic intent status | Staging creates no Claim or launch; snapshot/staged/durable/granted/bound states are distinguishable |
| Batch idempotency | Replaying one committed batch returns the same Claims and handles launches conservatively; different request parameters with the same ID fail |
| Shared worker assignment | Several fairly selected Claims share one API dispatch/native binding and retain distinct handles and charges |
| Independent finish | Complete c1, cancel c2, omit c3; only c1 completes/authorizes effects, and published c1 survives later failure |
| Effect isolation | Missing/unknown/wrong handles and c2 effects cannot use c1's Completion authorization |
| Stable packing | Incompatible next profile stops a bounded prefix rather than being skipped; compatible Claims share a bounded group |
| No-grant semantics | No-op evaluations neither charge nor write empty commits; backlog/capacity/delay reasons are distinct |
| Explain/trace parity | The stored prefix reproduces the receipt's winner and reason with or without OTLP; multi-Claim operation positions are unambiguous |
| Trace trust/privacy | Agent trace/provenance overrides are rejected; payloads do not leak and IDs are not metric labels |
| DAG admission | Atomic forward-reference resolution; missing/self/cyclic/conflicting edges reject with an actionable path |
| Ready frontier | Only nodes whose complete predecessor set has verified Results receive Claims; blocked roots do not consume service |
| Fork/join | A fork permits parallel sibling Claims; a join waits for every sibling, including after partial worker completion |
| Result barrier | Completion without verified effect/output delivery never releases a dependent; verified recovery can publish the same result without rerunning effects |
| Typed foreign resources | Issue versus PR/host/repository/immutable ID checks fail closed; reference alone gives no foreign write/dispatch privilege |
| GitHub conditions | Completed Issue/merged PR can unblock; unplanned Issue, closed-unmerged PR, unknown/error/stale response cannot satisfy the default conditions |
| Mutable observations | Reopening/invalidation blocks future admission without rewriting prior Claims; trace the exact observation IDs/times |

The successor TLA+ model covers bounded causal admission, ready-frontier Work
dependencies, verified-delivery/Result barriers, normalized Issue/PR gates,
logical/native capacity, per-handle closure/effects, and conflict regeneration.
Further refinement must cover dynamic graph admission, full request fingerprints,
multi-profile packing, observation freshness and actual GitHub credentials/APIs,
policy changes, and parser/transport behavior. Model-check safety separately from
liveness; fairness of scheduling cannot make a failed prerequisite succeed.

Bounded checks do not imply an unbounded proof or runtime refinement. The current model documentation already makes that distinction. [R14], [R16]

### 8.3 Workload evaluation grounded in HPC

Evaluate with wide DAG/fan-out patterns, narrow critical paths, many short workflows against one long workflow, staggered arrivals, heavy-tailed durations, heterogeneous worker requirements, restart bursts, dispatch failures, and multiple competing dispatchers.

Compare the default single-bucket FIFO behavior with explicitly configured strict-plus-key-fairness and weighted-class policies, including outstanding-cap effects. An unscheduled FIFO or strict-without-fairness simulator MAY be used as a research baseline, but neither is a supported bypass mode. Hold actual runner capacity and dispatcher cadence constant when comparing policies.

Report these outcomes separately:

- Eligible wait-to-grant and actual wait-to-run by class/account.
- Outstanding and actually running workload mix.
- Grant-share deviation during intervals of continuous competing eligibility.
- Runner-seconds or billed resource units, if trustworthy.
- Per-workflow turnaround, completion/failure rates, and p50/p95/p99 tails.
- Workflow slowdown against a stated isolated baseline.
- Idle capacity while eligible work exists, blocked reasons, publication conflicts, and orphan reservations.

For slowdown, define the numerator and denominator explicitly, for example `shared turnaround / isolated execution baseline`; a value greater than one indicates delay relative to that baseline. Do not mix this with inverse “speedup” ratios used elsewhere in the literature. DAG size, arrivals, and resource topology can make an isolated baseline difficult to estimate. [L3], [L4], [L6]

Jain's index or a similar summary can hide low-volume starvation. If used, normalize service by intended weight and report oldest eligible age and per-key zero-service intervals alongside it. Measure fairness during actual contention, not by comparing tenants that were idle or capped.

For the initial deterministic unit-grant model, exact fixed-set sequence fixtures are preferable to statistical confidence bands. For production SLOs, set thresholds only after workload traces establish realistic dispatcher cadence, run-duration bounds, capacity, and error margins.

## 9. Implementation map

This research intentionally changes no repository implementation. A follow-up implementation must wire all relevant surfaces, not just replay sorting.

| Surface | Expected work |
|---|---|
| `specs/work-queue/transactions.tsp` and emitted schemas | One current-only QueueCommit contract with Work edges, typed subjects, Result/Observation evidence, and stable request semantics |
| `work_queue_codemods.cjs` and protocol-upgrade documentation | Remove old-record loading/automatic upgrades from operational paths; document explicit unsupported-protocol failures |
| `work_queue_replay.cjs` | Causal-prefix replay, cycle/reference checks, ready-frontier/result/observation predicates, one selector/explanation engine, exact ticks |
| `work_queue_store.cjs` | One checked QueueCommit publication path, regenerated atomic batches, stable request recovery |
| `dispatch_workflow.cjs` or new trusted queue-dispatch handler | Policy-selected Work/target, reserved dispatch, binding/reconciliation |
| `work_queue_issues_store.cjs` | Reject queue operation until the backend can enforce equivalent mandatory scheduling serialization |
| `work_queue_mcp_server.cjs` and snapshots | Bounded read/explain plus staged submit/dispatch-next intents; explicit snapshot/staged provenance |
| Queue policy initialization and submission defaults | Mandatory policy with one default class/key, no implicit producer grouping, and oldest-available default grants |
| Worker finish/reconciliation and compiler integration | Bounded assignment arrays, explicit Claim handles on intents/effects, independent commits/gates, shared native reservation |
| `pkg/workqueue/` and `pkg/cli/work_command.go` | Shared envelope/selection fixtures, current-only records, explain/trace views, no direct-claim bypass; preserve configured authority boundaries |
| `pkg/cli/logs_work_queue*.go` and summaries | Bounded request/Claim receipts, stable statuses, current-protocol validation, escaped diagnostics |
| `otlp.cjs`, `aw_context.cjs`, and trusted worker binder | Reuse phase spans/context; correlate request/commit/Claim/run without promising unsupported span-parent behavior |
| Trusted dependency resolver and credential mapping | Deduplicated allowlisted cross-repo metadata reads, normalized observations, bounded freshness, explicit failures |
| ADR, TLA+, fixtures, tests, `.github/aw/work-queue.md` | Guarantee boundaries, negative controls, restart/concurrency/compaction coverage |

The release gate includes mandatory DAG scheduling, current-only validation,
ready-frontier/result/observation correctness, batched closure/effect isolation,
restart/concurrency correctness, recovery, and capacity limits. There is no
advisory-only or unscheduled intermediate queue. Historical usage accounting,
critical-path optimization, DRF, runtime-aware backfilling, and preemption remain
separate later capabilities.

## 10. Research limits and unresolved engineering questions

The recommendation is concrete, but several items require implementation-specific decisions and proof:

1. The wire schema/runtime are proposals. The successor checks abstract causal-prefix, DAG/readiness, normalized external gates, and batched closure; dynamic graph admission, JSON validation, actual cross-repo identity/permissions/freshness, packing, transport, and compaction refinement remain obligations.
2. The current Git backend's freshness/ref semantics must be validated under actual concurrent publication, including ambiguous responses.
3. Run correlation and recovery after “launch accepted, binding not persisted” need a supported GitHub Actions mechanism. Older dispatch APIs may not return run details.
4. The proposed dynamic-key, integer-tick hierarchy, and eligibility rules need conformance checks and are not covered wholesale by the original stride algorithm's theorems; live weight rebasing is outside version 1.
5. Resource fairness cannot be promised until trusted resource measurements and enforceable capacities exist.
6. There is no evidence here that the suggested class weights or outstanding defaults are optimal for gh-aw workloads.
7. Staged intents cannot return durable grants to the agent in the same turn under the current job topology; a live acquire-response design would be a different execution boundary.
8. Existing OTLP plumbing supplies correlation but not the exact grant-span context; causal span links may need a small separate helper extension if later required.
9. Cross-repository dependency reads are not atomic with queue publication; the stated freshness/admission semantics must not be marketed as global live consistency.

Primary manuals and open author/institutional papers were preferred. Publisher metadata was checked for key bibliographic identities. Several initial search results contained incorrect DOI/arXiv associations; these were corrected before inclusion. Some publisher full text was inaccessible. HEFT, EASY, and the Mu'alem/Feitelson article are used through verified bibliographic identities and corroborating system/survey material; the report does not claim to have rerun their published experiments.

The live Temporal and Slurm manuals may change. HTCondor links use the verified 23.0 manual because the corresponding `latest` URLs did not resolve during retrieval. This versioned manual is evidence for the documented mechanisms, not a claim about every subsequent HTCondor release.

No GitHub Actions workflow was triggered and no scheduler/evaluator results were rewritten. Runtime/compiler implementation remains unchanged; the successor TLA+ model and formal runner are updated with this specification.

## 11. Sources

### Repository evidence

All file sources use commit `81891f23dfb58b88bd90c9736887880234bffbe5`.

| ID | Source |
|---|---|
| R1 | [`.github/aw/work-queue.md`, lines 7-15][R1] |
| R2 | [`actions/setup/js/work_queue_replay.cjs`, lines 37-161][R2] |
| R3 | [`actions/setup/js/work_queue_replay.cjs`, lines 201-279][R3] |
| R4 | [`actions/setup/js/work_queue_replay.cjs`, lines 382-415][R4] |
| R5 | [`actions/setup/js/work_queue_mcp_server.cjs`, lines 48-132][R5] |
| R6 | [`actions/setup/js/dispatch_workflow.cjs`, lines 253-370][R6] |
| R7 | [`actions/setup/js/work_queue_store.cjs`, lines 150-215][R7] |
| R8 | [`actions/setup/js/work_queue_issues_store.cjs`, lines 81-226][R8] |
| R9 | [`pkg/workqueue/selection.go`, lines 20-55][R9] |
| R10 | [`pkg/cli/work_command.go`, lines 163-203][R10] |
| R11 | [`specs/work-queue/transactions.tsp`, lines 8-128][R11] |
| R12 | [ADR-64955: Git-Backed Work Queue Coordination][R12] |
| R13 | [Versioned Work Queue Messages ADR][R13] |
| R14 | [`specs/work-queue/WorkQueue.tla`, lines 133-176][R14] |
| R15 | [Issue #64852: Dispatch work coordinator][R15] |
| R16 | [`specs/work-queue/README.md`, modeling and ordering guarantees][R16] |
| R17 | [`actions/setup/js/otlp.cjs`, lines 74-173][R17]. Custom span API, sanitization/mirroring, export behavior, and no returned span context. |
| R18 | [`actions/setup/js/aw_context.cjs`, lines 284-289][R18]. Existing trace-context propagation contract. |
| R19 | [`actions/setup/js/aw_context.cjs`, lines 391-402][R19]. Propagated environment trace ID and setup-span parent. |
| R20 | [`pkg/cli/logs_work_queue_render.go`, lines 15-48][R20]. Current bounded-text rendering and escaping discipline. |
| R21 | [`actions/setup/js/dispatch_workflow.cjs`, lines 28-41][R21]. Existing approved targets, per-run dispatch bound, and trusted queue publication token. |

### Systems documentation

| ID | Source and relevant section |
|---|---|
| T1 | [Temporal: Task Queue Priority and Fairness][T1]. Priority, combined selection hierarchy, inheritance, mode transitions, rate limits, weight overrides, and fairness limitations. Retrieved 2026-10-05. |
| H1 | [Slurm: Multifactor Priority Plugin][H1]. Scheduler consideration order, factors, age, historical fair-share, billing, and decay. |
| H2 | [Slurm: Fair Tree Fairshare Algorithm][H2]. Account hierarchy, Level Fairshare calculation, and ranking. |
| H3 | [Slurm: Scheduling Configuration Guide][H3]. Event/periodic scheduling and backfill reservations/runtime limits. |
| H4 | [HTCondor 23.0: DAGMan Node Priorities][H4]. Readiness, submission priority, `JobPrio`, dependencies, and effective sub-DAG priority. |
| H5 | [HTCondor 23.0: Priorities and Preemption][H5]. Separate user/job priorities, historical user usage, and optional preemption. |
| H6 | [HTCondor 23.0: DAGMan and Accounting Groups][H6]. Accounting identity propagation to jobs and sub-DAGs. |
| H7 | [Pegasus: Introduction][H7]. DAG execution, workflow mapping, portability, data/retry management. |
| H8 | [Pegasus: Optimizing Workflows for Efficiency and Scalability][H8]. Job clustering and granularity/overhead trade-offs. |
| H9 | [Slurm: Preemption][H9]. Configured cancellation, requeue, suspension, and gang scheduling. |
| H10 | [HTCondor 23.0: DAGMan Throttling][H10]. Submitted nodes, idle jobs, and category `MAXJOBS`. |
| H11 | [Slurm: Job Array Support][H11]. Per-array simultaneous-task limits and ordinary job limits. |
| G1 | [GitHub Actions: Control workflow and job concurrency][G1]. Concurrency groups and pending queues are an additional execution-layer mechanism, not this queue's accounting policy. |
| O1 | [OpenTelemetry: Traces][O1]. Context propagation, parent spans, attributes, events, and asynchronous span links; retrieved 2026-10-05. |
| G2 | [GitHub REST Issues][G2]. Issue `state_reason`, resource identity, and PR records returned by Issues endpoints. |
| G3 | [GitHub REST Pull Requests][G3]. Exact PR identity/merge state; do not infer merge from issue closure. |
| G4 | [GitHub Actions token authentication][G4]. Least-privilege tokens and GitHub App credentials for permissions unavailable to the workflow token. |

### Research literature

| ID | Citation |
|---|---|
| L1 | David A. Lifka. **The ANL/IBM SP Scheduling System.** JSSPP/LNCS 949, 1995. [DOI: 10.1007/3-540-60153-8_35][L1]. Classic EASY scheduling reference. |
| L2 | Ahuva W. Mu'alem and Dror G. Feitelson. **Utilization, Predictability, Workloads, and User Runtime Estimates in Scheduling the IBM SP2 with Backfilling.** IEEE TPDS, 12(6), 2001. [DOI: 10.1109/71.932708][L2]. Correct journal DOI; distinct from the 1998 conference antecedent. |
| L3 | Henan Zhao and Rizos Sakellariou. **Scheduling Multiple DAGs onto Heterogeneous Systems.** IPDPS, 2006. [DOI: 10.1109/IPDPS.2006.1639387][L3]. [Author institution record and abstract][L3a]. |
| L4 | Hamid Arabnejad, Jorge G. Barbosa, and Frederic Suter. **Fair Resource Sharing for Dynamic Scheduling of Workflows on Heterogeneous Systems.** In *High-Performance Computing on Complex Environments*, Wiley, 2014. [HAL author manuscript][L4]; [open PDF][L4a]. Especially online scheduling/FDWS sections, PDF pages 10-15. |
| L5 | Haluk Topcuoglu, Salim Hariri, and Min-You Wu. **Performance-Effective and Low-Complexity Task Scheduling for Heterogeneous Computing.** IEEE TPDS, 13(3), 260-274, 2002. [DOI: 10.1109/71.993206][L5]. HEFT/CPOP reference. |
| L6 | Muhammad H. Hilman, Maria A. Rodriguez, and Rajkumar Buyya. **Multiple Workflows Scheduling in Multi-tenant Distributed Systems: A Taxonomy and Future Directions.** ACM Computing Surveys, 2020; preprint first posted 2018. [DOI: 10.1145/3368036][L6]; [arXiv:1809.05574][L6a]; [HTML manuscript][L6b]. Survey, not an original proof of all reviewed algorithms. |
| L7 | Ioan Raicu, Yong Zhao, Catalin Dumitrescu, Ian Foster, and Mike Wilde. **Falkon: A Fast and Light-weight tasK executiON Framework.** SC, 2007. [DOI: 10.1145/1362622.1362680][L7]. Resource provisioning/task dispatch separation. |
| L8 | Ali Ghodsi, Matei Zaharia, Benjamin Hindman, Andy Konwinski, Scott Shenker, and Ion Stoica. **Dominant Resource Fairness: Fair Allocation of Multiple Resource Types.** NSDI, 2011. [USENIX record][L8]; [open paper][L8a]. |
| L9 | M. Shreedhar and George Varghese. **Efficient Fair Queuing Using Deficit Round-Robin.** IEEE/ACM ToN, 4(3), 375-385, 1996; earlier SIGCOMM/technical-report versions. [DOI: 10.1109/90.502236][L9]; [institutional technical report and abstract][L9a]. |
| L10 | Carl A. Waldspurger and William E. Weihl. **Stride Scheduling: Deterministic Proportional-Share Resource Management.** MIT/LCS/TM-528, June 22, 1995. [Author-hosted paper][L10]. Sections 2.1-2.4, 4, and 7 distinguish static, dynamic, nonuniform, and hierarchical allocation. |
| L11 | Alan Demers, Srinivasan Keshav, and Scott Shenker. **Analysis and Simulation of a Fair Queueing Algorithm.** SIGCOMM, 1989. [DOI: 10.1145/75246.75248][L11]. |
| L12 | Pawan Goyal, Harrick M. Vin, and Haichen Cheng. **Start-Time Fair Queueing: A Scheduling Algorithm for Integrated Services Packet Switching Networks.** IEEE/ACM ToN, 1997. [DOI: 10.1109/90.649569][L12]. |

[R1]: https://github.com/github/gh-aw/blob/81891f23dfb58b88bd90c9736887880234bffbe5/.github/aw/work-queue.md#L7-L15
[R2]: https://github.com/github/gh-aw/blob/81891f23dfb58b88bd90c9736887880234bffbe5/actions/setup/js/work_queue_replay.cjs#L37-L161
[R3]: https://github.com/github/gh-aw/blob/81891f23dfb58b88bd90c9736887880234bffbe5/actions/setup/js/work_queue_replay.cjs#L201-L279
[R4]: https://github.com/github/gh-aw/blob/81891f23dfb58b88bd90c9736887880234bffbe5/actions/setup/js/work_queue_replay.cjs#L382-L415
[R5]: https://github.com/github/gh-aw/blob/81891f23dfb58b88bd90c9736887880234bffbe5/actions/setup/js/work_queue_mcp_server.cjs#L48-L132
[R6]: https://github.com/github/gh-aw/blob/81891f23dfb58b88bd90c9736887880234bffbe5/actions/setup/js/dispatch_workflow.cjs#L253-L370
[R7]: https://github.com/github/gh-aw/blob/81891f23dfb58b88bd90c9736887880234bffbe5/actions/setup/js/work_queue_store.cjs#L150-L215
[R8]: https://github.com/github/gh-aw/blob/81891f23dfb58b88bd90c9736887880234bffbe5/actions/setup/js/work_queue_issues_store.cjs#L81-L226
[R9]: https://github.com/github/gh-aw/blob/81891f23dfb58b88bd90c9736887880234bffbe5/pkg/workqueue/selection.go#L20-L55
[R10]: https://github.com/github/gh-aw/blob/81891f23dfb58b88bd90c9736887880234bffbe5/pkg/cli/work_command.go#L163-L203
[R11]: https://github.com/github/gh-aw/blob/81891f23dfb58b88bd90c9736887880234bffbe5/specs/work-queue/transactions.tsp#L8-L128
[R12]: https://github.com/github/gh-aw/blob/81891f23dfb58b88bd90c9736887880234bffbe5/docs/adr/64955-git-backed-work-queue-coordination.md
[R13]: https://github.com/github/gh-aw/blob/81891f23dfb58b88bd90c9736887880234bffbe5/docs/adr/work-queue-protocol-upgrades.md
[R14]: https://github.com/github/gh-aw/blob/81891f23dfb58b88bd90c9736887880234bffbe5/specs/work-queue/WorkQueue.tla#L133-L176
[R15]: https://github.com/github/gh-aw/issues/64852
[R16]: https://github.com/github/gh-aw/blob/81891f23dfb58b88bd90c9736887880234bffbe5/specs/work-queue/README.md
[R17]: https://github.com/github/gh-aw/blob/81891f23dfb58b88bd90c9736887880234bffbe5/actions/setup/js/otlp.cjs#L74-L173
[R18]: https://github.com/github/gh-aw/blob/81891f23dfb58b88bd90c9736887880234bffbe5/actions/setup/js/aw_context.cjs#L284-L289
[R19]: https://github.com/github/gh-aw/blob/81891f23dfb58b88bd90c9736887880234bffbe5/actions/setup/js/aw_context.cjs#L391-L402
[R20]: https://github.com/github/gh-aw/blob/81891f23dfb58b88bd90c9736887880234bffbe5/pkg/cli/logs_work_queue_render.go#L15-L48
[R21]: https://github.com/github/gh-aw/blob/81891f23dfb58b88bd90c9736887880234bffbe5/actions/setup/js/dispatch_workflow.cjs#L28-L41
[T1]: https://docs.temporal.io/develop/task-queue-priority-fairness
[H1]: https://slurm.schedmd.com/priority_multifactor.html
[H2]: https://slurm.schedmd.com/fair_tree.html
[H3]: https://slurm.schedmd.com/sched_config.html
[H4]: https://htcondor.readthedocs.io/en/23.0/automated-workflows/dagman-priorities.html
[H5]: https://htcondor.readthedocs.io/en/23.0/users-manual/priorities-and-preemption.html
[H6]: https://htcondor.readthedocs.io/en/23.0/automated-workflows/dagman-accounting.html
[H7]: https://pegasus.isi.edu/documentation/user-guide/introduction.html
[H8]: https://pegasus.isi.edu/documentation/user-guide/optimization.html
[H9]: https://slurm.schedmd.com/preempt.html
[H10]: https://htcondor.readthedocs.io/en/23.0/automated-workflows/dagman-throttling.html
[H11]: https://slurm.schedmd.com/job_array.html
[G1]: https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/control-workflow-concurrency
[O1]: https://opentelemetry.io/docs/concepts/signals/traces/
[G2]: https://docs.github.com/en/rest/issues/issues#get-an-issue
[G3]: https://docs.github.com/en/rest/pulls/pulls#get-a-pull-request
[G4]: https://docs.github.com/en/actions/tutorials/authenticate-with-github_token
[L1]: https://doi.org/10.1007/3-540-60153-8_35
[L2]: https://doi.org/10.1109/71.932708
[L3]: https://doi.org/10.1109/IPDPS.2006.1639387
[L3a]: https://research.manchester.ac.uk/en/publications/scheduling-multiple-dags-onto-heterogeneous-systems/
[L4]: https://inria.hal.science/hal-00926460
[L4a]: https://inria.hal.science/file/index/docid/926460/filename/book.pdf
[L5]: https://doi.org/10.1109/71.993206
[L6]: https://doi.org/10.1145/3368036
[L6a]: https://arxiv.org/abs/1809.05574
[L6b]: https://ar5iv.labs.arxiv.org/html/1809.05574
[L7]: https://doi.org/10.1145/1362622.1362680
[L8]: https://www.usenix.org/conference/nsdi11/dominant-resource-fairness-fair-allocation-multiple-resource-types
[L8a]: https://www.usenix.org/legacy/events/nsdi11/tech/full_papers/Ghodsi.pdf
[L9]: https://doi.org/10.1109/90.502236
[L9a]: https://repository.library.washu.edu/cse_research/339/
[L10]: https://www.waldspurger.org/carl/papers/stride-mit-tm528.pdf
[L11]: https://doi.org/10.1145/75246.75248
[L12]: https://doi.org/10.1109/90.649569
