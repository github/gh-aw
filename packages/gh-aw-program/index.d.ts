// Type declarations for gh-aw deterministic programs.
//
// A workflow with `engine: none` and `program: <file>.ts` runs <file>.ts instead of an
// agent. The module default-exports an async function receiving `Program`:
//
//   import type { Program } from "@github/gh-aw-program";
//   export default async function (aw: Program) {
//     await aw.workQueue?.dispatchNext({ pool: "default", max_claims: 3, max_dispatches: 3 });
//   }
//
// Programs run with Node.js type stripping: use erasable TypeScript only (no enums,
// namespaces or parameter properties) and `import type` for type-only imports.
// The runtime is shipped by the gh-aw setup action; this package only provides types.

import type * as Core from "@actions/core";
import type * as Exec from "@actions/exec";
import type * as Io from "@actions/io";
import type { context as GitHubContext, getOctokit as GetOctokit } from "@actions/github";
import type { GitHub } from "@actions/github/lib/utils";

/** Authenticated Octokit client of the program step (`github` in actions/github-script). */
export type GitHubClient = InstanceType<typeof GitHub>;

/** The object a program receives. */
export interface Program {
  /** @actions/core: logging, outputs, step summary (`core.summary`). */
  readonly core: typeof Core;
  /** Octokit client authenticated with the step token. */
  readonly github: GitHubClient;
  /** Workflow run context (event payload, repo, run ID, workflow_dispatch inputs). */
  readonly context: typeof GitHubContext;
  /** @actions/exec: run subprocesses. */
  readonly exec: typeof Exec;
  /** @actions/io: file-system helpers. */
  readonly io: typeof Io;
  /** Create additional Octokit clients for other tokens. */
  readonly getOctokit: typeof GetOctokit;
  /** Work-queue API; null unless the workflow declares tools.work-queue. */
  readonly workQueue: WorkQueue | null;
  /** Safe-output API; worker and non-queue programs only. */
  readonly safeOutputs: SafeOutputs;
}

/** Program default export. */
export type ProgramMain = (aw: Program) => Promise<void> | void;

export type WorkQueueRole = "dispatcher" | "worker";

/**
 * Typed wrappers of the work-queue MCP tools with identical arguments and validation.
 *
 * Programs with `tools.work-queue: true` hold the dispatcher role: `submit` and
 * `dispatchNext` commit immediately through the trusted controls and resolve to the
 * control receipt; a blocked receipt rejects. Worker programs (`worker: true`) stage
 * `submit` and `claimFinish` like an agent; the safe_outputs job applies them.
 */
export interface WorkQueue {
  readonly role: WorkQueueRole;
  /** The worker's assigned Claim, or null outside a worker run. */
  readonly assignment: WorkQueueAssignment | null;
  /** `work_queue_read`: a bounded view of the activation snapshot of one pool. */
  read(args?: WorkQueueReadArgs): Promise<WorkQueueState>;
  /** `work_queue_submit`: submit 1–256 immutable Work nodes. */
  submit(args: WorkQueueSubmitArgs): Promise<WorkQueueControlReceipt | WorkQueueStagedIntent>;
  /** `work_queue_dispatch_next`: claim the pool's next eligible Work and launch workers (dispatcher only). */
  dispatchNext(args: WorkQueueDispatchNextArgs): Promise<WorkQueueControlReceipt>;
  /** `work_queue_claim_finish`: stage the assigned Claim's outcome (worker only). Missing means failed. */
  claimFinish(args?: WorkQueueClaimFinishArgs): Promise<WorkQueueStagedFinish>;
}

export interface WorkQueueReadArgs {
  work?: string;
  pool?: string;
  /** 1–128, default 32. */
  limit?: number;
  offset?: number;
  /** Presentation only; never changes the scheduler's priority-then-FIFO order. */
  sort?: Array<{ key: "id" | "position" | "priority"; direction: "asc" | "desc" }>;
}

export type WorkState = "available" | "claimed" | "completed" | "cancelled";

export interface WorkQueueWorkRow {
  id: string;
  graph_id: string;
  node_key: string;
  state: WorkState;
  pool: string;
  priority: number;
  worker_profile: string;
  attempts: number;
  position: { commit: number; operation: number };
  claim_id: string | null;
  readiness?: string;
  cancellation_reason?: string;
}

export interface WorkQueueState {
  view: "activation_snapshot";
  snapshot_sha: string | null;
  queue_tip: string | null;
  pool: string;
  assignment?: WorkQueueAssignment;
  /** Next eligible Work; may be stale and is never a grant. */
  prediction: { work_id: string | null; authoritative: false };
  counts: Partial<Record<WorkState, number>>;
  claim_counts: Record<string, number>;
  open_dispatches: { total: number; unbound: number };
  work?: { id: string; state: WorkState | "absent" };
  total: number;
  offset: number;
  next_offset: number | null;
  works: WorkQueueWorkRow[];
}

export interface WorkQueueAssignedClaim {
  claim_handle: string;
  claim_id: string;
  work_id: string;
  /** The immutable Work payload. */
  work: Record<string, unknown>;
  /** Completed Work predecessors and their recorded effects. */
  result_refs: Array<{ work_id: string; result_commit_id: string; descriptor: unknown }>;
  /** Untrusted remaining-work context of a continuation successor. */
  continuation?: { route: string; message: string; remaining: number };
}

export interface WorkQueueAssignment {
  dispatch_id: string;
  claims: WorkQueueAssignedClaim[];
}

/** One Work node; omitted graph_id/node_key default to the payload hash and "root". */
export interface WorkNode {
  payload: Record<string, unknown>;
  graph_id?: string;
  node_key?: string;
  work_id?: string;
  pool?: string;
  /** 1 (first) to 5, default 3. */
  priority?: number;
  fairness_key?: string;
  worker_profile?: string;
  /** Work, Issue or pull request dependency edges; see the work-queue specification. */
  depends_on?: Array<Record<string, unknown>>;
  subject?: Record<string, unknown>;
  backing_issue?: Record<string, unknown>;
  replacement_of?: string;
}

export interface WorkQueueSubmitArgs {
  nodes: WorkNode[];
  /** Workers only; must equal the assigned handle when supplied. */
  claim_handle?: string;
}

export interface WorkQueueDispatchNextArgs {
  pool: string;
  /** 1–256. */
  max_claims: number;
  /** 1–256; bounded by safe-outputs.dispatch-workflow.max across the run. */
  max_dispatches: number;
}

export interface WorkQueueClaimFinishArgs {
  claim_handle?: string;
  /** Default "completed". */
  outcome?: "completed" | "failed";
}

export interface WorkQueueLaunchReceipt {
  dispatch_id: string;
  state: "started" | "launch_failed";
  reason?: string;
}

/** Receipt of a committed control (publisher programs). */
export interface WorkQueueControlReceipt {
  intent_id?: string;
  request_id?: string;
  commit_id?: string;
  status: "committed";
  dispatches?: number;
  launches?: WorkQueueLaunchReceipt[];
}

/** A staged intent (worker programs); applied by the safe_outputs job. */
export interface WorkQueueStagedIntent {
  intent_id: string;
  status: "staged";
  claim_handle?: string;
}

export interface WorkQueueStagedFinish {
  claim_handle: string;
  outcome: "completed" | "failed";
  status: "staged";
}

/** A safe-output message; `type` must be enabled under safe-outputs. */
export interface SafeOutputMessage {
  type: string;
  /** output_id values of messages this one depends on. */
  "depends-on"?: string | string[];
  [field: string]: unknown;
}

export interface SafeOutputs {
  /**
   * Append one safe-output message for the safe_outputs job and return its output_id.
   * Throws for publisher programs and for types not enabled in safe-outputs.
   */
  emit(message: SafeOutputMessage): string;
}
