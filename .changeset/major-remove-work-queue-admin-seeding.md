---
"gh-aw": major
---

Remove standalone administrator seeding from work-queue publishers and activation.

**Breaking change:** `policy` no longer creates an absent queue, and legacy
`initializeWorkQueue` / `initializationContext` calls report an unsupported
protocol error. Workflow and Actions authorization already authorizes the
trusted first submission.

**Migration:** remove pre-submission Policy-seeding commands and initialization
contexts. Submit Work with the workflow's compiler-approved Policy proposal, or
use native `submit-work` / `submit-graph` defaults. The first accepted submission
atomically publishes Policy and Work. Use `policy` only for later quiescent
updates; existing Policy-only genesis history remains readable.
