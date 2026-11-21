# ADR-79701: Dispatch Continuations via a Generated Relay Workflow

**Date**: 2026-11-21
**Status**: Draft
**Deciders**: pelikhan (PR author) [TODO: verify other deciders]

---

### Context

`safe-outputs.request-continuation` lets an agent run schedule the next run of the same workflow, passing a native session checkpoint and self-contained hints. Before this PR, the safe-outputs job dispatched that next run itself. That required `actions: write` in an agent-adjacent job, offered no point to insert a human approval gate, and trusted `aw_context` for lineage, so a crafted context could forge continuation lineage. Any fix must keep continuations working for every workflow that enables the feature, fail closed on invalid hops, and support an optional approval environment.

### Decision

We will record continuation requests as an artifact in the safe-outputs job, and have the conclusion job dispatch a compiler-generated, generic `agentic-continuation-relay.yml` workflow. The relay is the only component that dispatches continuation runs. It validates each hop (default branch, same repository, non-fork, actor write access, parent lineage, depth limit, compiled policy, and approval) before dispatching. The primary driver is security: it removes `actions: write` from the agent-adjacent safe-outputs job and centralizes fail-closed validation in one place.

### Alternatives Considered

#### Alternative 1: Keep self-dispatch in the safe-outputs job

The prior design called the workflow dispatch API from the request handler, with `actions: write` granted to the safe-outputs job and the child trusting `aw_context` for lineage. It was the simplest option and the one in place before this PR. It was rejected because the agent-adjacent job holds workflow-dispatch rights, it cannot wait on a human approval, and forged lineage cannot be detected. [TODO: verify whether other reasons were discussed in review.]

#### Alternative 2: Dispatch directly from the conclusion job

Moving the dispatch call into the conclusion job would take it out of the agent-adjacent job without adding a workflow. The job would still need `actions: write`, and the approval gate and lineage checks would have to live either in that job or in the child, so validation would be split across two places instead of one. [TODO: verify whether this option was evaluated during design.]

### Consequences

#### Positive
- The safe-outputs job no longer needs `actions: write`. The regenerated lock files for the affected workflows drop that grant.
- Every hop is validated in one workflow: default branch, same repository, non-fork, actor write access, parent lineage, depth limit, and policy read from the compiled lock at the parent head SHA.
- An optional approval environment can gate each continuation on a human reviewer, and the relay fails closed when the environment is missing or lacks required reviewers.
- The `gh-aw-durable-*` concurrency group is removed, so a pending-run group can no longer silently replace a dispatched continuation.

#### Negative
- The conclusion job receives `actions: write` when the feature is enabled, so the privilege moves rather than disappears.
- Every continuation adds an extra workflow hop (resolve, optional approve, dispatch), which increases latency.
- Residual risk: another workflow using `GITHUB_TOKEN` could replay a legitimate `relay_run_id` and start a duplicate child run. The relay cannot forge lineage, depth, or hints, but it does not prevent duplicates.
- The compiler now generates and maintains a second workflow (about 150 lines of YAML from a 180-line Go generator).

#### Neutral
- The relay is generated lazily per workflow directory, as `agentic-maintenance.yml` is, and removed when no workflow uses `request-continuation`.
- A new `continuation-policy` option (`workflow-dispatch`, optionally with `approval`) is added to the frontmatter schema, and lock file headers record it as `continuation` metadata.
- Audit and logs schemas gain a `relay_run_id` field.
- Work-queue support and relay distribution through `gh aw add` and `gh aw init` are out of scope for this PR.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
