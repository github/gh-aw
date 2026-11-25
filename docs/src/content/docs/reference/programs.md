---
title: Programs
description: Run a deterministic TypeScript or JavaScript program instead of an agent with engine none.
---

A workflow with `engine: none` and a top-level `program:` runs a deterministic program instead of an agent. The compiler emits no engine installation, agent sandbox, MCP gateway or model call; the `agent` job runs the program with [actions/github-script](https://github.com/actions/github-script) and keeps activation, safe outputs and conclusion. Use programs for components that need no model, such as work-queue dispatchers and producers.

```aw wrap title=".github/workflows/queue-dispatcher.md"
---
on: workflow_dispatch
permissions:
  contents: read
engine: none
program: queue-dispatcher.ts
tools:
  work-queue: true
safe-outputs:
  dispatch-workflow:
    workflows: [queue-worker]
    max: 3
---

Dispatch up to three queued tasks.
```

```ts title=".github/workflows/queue-dispatcher.ts"
import type { Program } from "@github/gh-aw-program";

export default async function dispatch(aw: Program): Promise<void> {
  const receipt = await aw.workQueue!.dispatchNext({ pool: "default", max_claims: 3, max_dispatches: 3 });
  aw.core.info(`Started ${receipt.launches?.filter(launch => launch.state === "started").length ?? 0} workers`);
}
```

## Program file

`program` is a path relative to the workflow's directory, at most 256 bytes, with plain segments (no `..` or hidden directories), ending in `.ts`, `.mts`, `.cts`, `.js`, `.mjs` or `.cjs`. The file must exist at compile time and is read from the checked-out repository at run time, so edits take effect without recompiling. The runner refuses a program that resolves outside the workspace, including through symlinks.

The module's default export is an async function receiving the program SDK. A thrown error fails the step and the run; the conclusion job reports it like an agent failure. The markdown body is optional and is not sent anywhere.

TypeScript runs through Node.js type stripping in the github-script runtime (Node.js 24), so only erasable syntax is supported: no `enum`, `namespace` or constructor parameter properties, and type-only imports must use `import type`. Install the type declarations for editor support with `npm install --save-dev @github/gh-aw-program`; the runtime ships with the gh-aw setup action, so programs import nothing from the package at run time.

## Program SDK

| Member | Description |
| --- | --- |
| `core`, `github`, `context`, `exec`, `io`, `getOctokit` | The github-script globals. `github` is authenticated with the step token; `core.summary` writes the step summary. |
| `workQueue` | Work-queue API, or `null` without `tools.work-queue`. |
| `safeOutputs.emit(message)` | Appends one safe-output message (`{ type, ...fields }`) for the `safe_outputs` job and returns its `output_id` for `depends-on`. Throws for types not enabled under `safe-outputs`. |

`workQueue` wraps the [work-queue MCP tools](/gh-aw/reference/work-queue/#workflow-roles-and-tools) with the same arguments and validation. Every method returns a promise.

| Method | MCP tool | Roles |
| --- | --- | --- |
| `read(args?)` | `work_queue_read` | All; reads the activation snapshot. |
| `submit({ nodes, claim_handle? })` | `work_queue_submit` | Dispatcher, worker. |
| `dispatchNext({ pool, max_claims, max_dispatches })` | `work_queue_dispatch_next` | Dispatcher. |
| `claimFinish({ claim_handle?, outcome? })` | `work_queue_claim_finish` | Worker. |

`workQueue.role` is `dispatcher` or `worker`, and `workQueue.assignment` holds a worker's assigned Claim (`claims[0].work` is the Work payload).

## Work-queue roles

A program with `tools.work-queue: true` holds the dispatcher role and publishes directly. `submit` and `dispatchNext` commit through the same trusted controls the [Process trusted work queue controls](/gh-aw/reference/work-queue/#dispatch-and-launch) step uses and resolve to the control receipt; a blocked receipt rejects with `work_queue_control_blocked: <reason>`. Dispatches are bounded by `safe-outputs.dispatch-workflow.max` across the run and launch only the listed workers. Without `dispatch-workflow` targets the program can submit Work but `dispatchNext` fails with `work_queue_worker_not_allowlisted`, which makes it a producer. The compiler emits no `safe_outputs` job for these programs, and they cannot emit safe outputs. The step output `work_queue_requests` and the step summary carry the receipts.

> [!CAUTION]
> A publishing program runs in the job that holds the queue publisher token (`contents: write`) and the worker launch token (`actions: write`). Queue replay authorizes roles, not credentials: code in the program, including any subprocess or dependency it runs, can write the queue branch and launch workflows. Review programs and their dependencies like workflow code with write access.

A program with `tools.work-queue.worker: true` is a worker. It never receives queue credentials: `submit` and `claimFinish` stage intents, and safe outputs it emits are Claim-scoped. The `safe_outputs` job applies them, finishes the Claim and records its effects exactly as for an agent worker. A missing `claimFinish` fails the Claim.

## Restrictions

`engine: none` requires `program:` and the reverse. The string form `engine: none` is the only accepted engine form. Agent configuration is a compile error: `tools` other than `work-queue`, `mcp-servers`, `mcp-scripts`, `network`, `sandbox`, `pre-agent-steps`, `imports`, `plugins`, `skills`, model and routing fields, turn and AI-credit budgets, `graders`, `evals` and `experiments`. Threat detection is disabled because program outputs are not model-generated; an explicit `safe-outputs.threat-detection` is rejected.

`steps:` still run before the program, and `post-steps:` after it, in the same job. The program runs on the runner without the agent firewall, so it can start subprocesses and reach any network destination the runner can.
