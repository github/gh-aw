---
"gh-aw": minor
---

Add the experimental `safe-outputs.request-continuation` safe output (optional `max`, default `5`) that replaces `engine.durable`. The agent must call `request_continuation` with self-contained hints, and may emit other safe outputs in the same run.

The new `continuation-policy` option (`type: workflow-dispatch`, optional `approval` environment) controls how the next run starts. The safe-outputs job records the request as an artifact, and the conclusion job dispatches a generic, lazily generated `agentic-continuation-relay.yml` relay. The relay rejects `workflow_dispatch` abuse (non-default-branch relay copies, foreign bots, users without write access, forged `aw_context` lineage), validates the parent run, request and target branch, enforces the optional approval gate (failing closed), and dispatches the next run.

The next run restores the native session checkpoint, reads the hints from the parent's request artifact, and records the relay run in its lineage, but must reconstruct its working context. When the agent does not request continuation, no checkpoint artifact is uploaded and the chain stops. Durable workflows no longer get a compiler-generated concurrency group and may declare their own `concurrency`.

`engine.durable` is deprecated (treated as `request-continuation` with a warning); run `gh aw fix` to migrate it with the new `engine-durable-to-request-continuation` codemod.
