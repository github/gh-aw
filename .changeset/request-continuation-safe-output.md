---
"gh-aw": minor
---

Add the experimental `safe-outputs.request-continuation` safe output (optional `max`, default `5`) that replaces `engine.durable`. The agent must call `request_continuation` with self-contained hints to dispatch the next run, and may emit other safe outputs in the same run; the next run restores the native session checkpoint and receives the hints, but must reconstruct its working context. When the agent does not request continuation, no checkpoint artifact is uploaded and the chain stops. `engine.durable` is deprecated (treated as `request-continuation` with a warning); run `gh aw fix` to migrate it with the new `engine-durable-to-request-continuation` codemod.
