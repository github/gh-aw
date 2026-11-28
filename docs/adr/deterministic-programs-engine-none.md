# ADR: Deterministic Programs With `engine: none`

**Date**: 2026-11-24
**Status**: Proposed (implemented; maintainer acceptance pending)

---

### Context

Some workflow components need no model. A work-queue dispatcher only asks the scheduler to claim the next eligible Work: the scheduler alone selects Work (priority, then FIFO) and the `work_queue_dispatch_next` tool accepts no Work selector. Authors already move such logic into pre-agent `steps:` that call internal runtime modules (`work_queue_intents.cjs`, `work_queue_mcp_server.cjs`) and leave the agent a summary. Each run still installs the engine CLI, starts AWF and the MCP gateway, makes an LLM call, and then hands staged intents to a separate `safe_outputs` job; observed dispatch latency is about five minutes. The internal modules are not a stable contract: a dispatcher written against protocol v3 stages `version: 3` intents and reads projection fields that v4 removed.

Plain GitHub Actions workflows avoid the agent but cannot take part in the queue: workers bind only Dispatches recorded by compiled dispatcher controls, and roles, policy and allowlists are compiled into gh-aw jobs.

### Decision

Add `engine: none` with a required top-level `program:`, a TypeScript or JavaScript module next to the workflow. The compiler keeps the job graph and the `agent` job ID (downstream `needs.agent.*` wiring is unchanged) but replaces engine installation, MCP setup and agent execution with one `actions/github-script` step that runs `run_program.cjs`. The runner resolves the program inside the workspace, imports it (Node.js 24 type stripping), and calls its default export with a program SDK.

The SDK exposes the github-script globals, `safeOutputs.emit`, and `workQueue`, typed wrappers of the four work-queue MCP tools built from the same tool definitions, so arguments and validation are identical. Types ship as `@github/gh-aw-program` (declarations only); the runtime ships with the setup action and is therefore pinned to the compiler version that wrote the lock file.

Work-queue roles:

- A program with `tools.work-queue: true` holds the dispatcher role and publishes directly ("trusted-direct"). The program step receives the controls step's queue publisher token, dispatch configuration and budget; `submit` and `dispatchNext` call `processParsedWorkQueueIntents` through `configureControls`, the same path as the controls step. The compiler emits no `safe_outputs` job and grants the program job `contents: write`, `actions: write` (and `issues: write` with Issue projection). A GitHub App or token-broker safe-outputs credential is minted in the program job.
- A worker program keeps the agent-equivalent path: no queue credentials, staged child submissions and finish intent, Claim-scoped safe outputs applied by the `safe_outputs` job.

Programs reject agent-only configuration (tools other than `work-queue`, MCP, network, sandbox, model, budgets, imports, graders) and disable threat detection. The markdown body becomes optional. The collector no longer synthesizes `report_incomplete` when a program emits nothing.

### Alternatives Considered

#### Alternative 1: A deterministic `engine.harness` script

Keep an engine and replace its harness with a script that never calls a model. The engine install, AWF, MCP gateway, secrets and job hops remain, and the script runs inside the firewall with read-only mounts. This removes the LLM turn but not most of the latency.

#### Alternative 2: Stage-only programs

Run the program without queue credentials and keep the `safe_outputs` controls step as the only publisher. This preserves the existing boundary that untrusted execution never holds queue-write credentials, at the cost of one more job. It was not chosen: the decision owner accepted trusted-direct publishing for fewer jobs. Worker programs keep stage-only semantics because Claim-scoped effects depend on it.

#### Alternative 3: Declarative dispatcher configuration

Generate a dispatcher from `tools.work-queue.dispatch: {pool, max-claims, ...}` with no code. This covers pure dispatchers but not producers that plan Work from repository data or external tools.

### Consequences

#### Positive
- Dispatchers and producers run without engine installation, AWF, MCP gateway, model call or `safe_outputs` job.
- Programs use a typed, versioned API instead of internal modules; protocol changes ship with the matching runtime.
- Worker programs reuse the existing Claim authorization and finish semantics unchanged.

#### Negative
- Queue replay authorizes roles, not credentials. A publishing program, and any subprocess or dependency it runs, holds the queue publisher and launch tokens and can write any operation to the queue branch or launch any workflow the tokens allow. This is a weaker boundary than agent dispatchers, whose execution never holds those credentials.
- Programs run without the agent firewall and can reach any network destination the runner can.
- A new engine sentinel (`none`) must be excluded from engine listings and handled by engine-aware validators.
- Live end-to-end latency has not been measured.

#### Neutral
- Activation, pre-activation and conclusion jobs are unchanged; activation still renders a prompt artifact that programs do not read.
- The `@github/gh-aw-program` package needs a release process before it is installable from npm.
