# ADR-72938: Exclude Safe-Output Credentials from Agent-Side Configuration

**Date**: 2026-07-21
**Status**: Draft
**Deciders**: pelikhan, Copilot (PR #72938) [TODO: verify]

---

### Context

The compiler generates one safe-outputs configuration (`GH_AW_SAFE_OUTPUTS_CONFIG`). The agent job writes it to `${RUNNER_TEMP}/gh-aw/safeoutputs/config.json`, and the `safe_outputs` job also uses it. Per-output `github-token`, `head-github-token`, and nested `repository-dispatch` tool `github-token` values were copied into the agent-side copy. As a result, the agent job had to declare any token-minting step referenced by those expressions. The agent job also received the matching `GH_AW_SECRET_*` env bindings, and live credentials were written to a file mounted into the agent sandbox. The "Redact secrets in logs" step treats that config as control data that must not be rewritten. When it found a token there, it deleted the file and failed. Artifact uploads were then skipped, and `safe_outputs` applied nothing (issue #72648). The global `safe-outputs.github-token` cannot carry a runtime-minted token because the `conclusion` job, which also uses it, cannot run user steps. Per-output tokens minted in `jobs.safe_outputs.pre-steps` are therefore the only supported route for that use case.

### Decision

We will remove all credential fields (`github-token`, `head-github-token`, and nested tool `github-token`) from each handler's entry in the agent-side safe-outputs configuration in `addStandardHandlerConfigs`. Only the `safe_outputs` job's trusted handler configuration will keep these credentials. Step-output tokens used by safe-output handlers only have to be minted in `jobs.safe_outputs.pre-steps`. Compile-time validation still fails when the consuming job has no step with the referenced id. The main driver is credential isolation: the agent only needs to know which outputs it may emit and their constraints, not how they will be authenticated.

### Alternatives Considered

#### Alternative 1: Keep tokens in agent config and allow-list the config file in secret redaction

Change redaction so it skips or masks tokens in `config.json` without deleting the file. This would unblock artifact uploads with a smaller compiler change. We did not choose it because live credentials would still reach the agent sandbox and the agent job would still need the minting steps. It removes the symptom, not the exposure.

#### Alternative 2: Require a global `safe-outputs.github-token` for runtime-minted tokens

Guide users back to a single global token, as worked in v0.86.2. We did not choose it because the global token is now also consumed by the `conclusion` job, which cannot run user-provided steps. A step-output global token would fail to compile.

#### Alternative 3: Emit separate agent and safe_outputs config generators

Build two distinct configuration schemas so the agent's copy is credential-free by construction. This is a cleaner long-term boundary, but it requires a much larger refactor. The PR instead deletes the fields at a single choke point. [TODO: verify whether this was considered]

### Consequences

#### Positive
- Live credentials (secrets and minted step-output tokens) are no longer written into the agent sandbox's safe-outputs config, which narrows the agent's credential surface.
- Secret redaction no longer deletes the config and fails, so agent artifacts upload and safe outputs are applied again for workflows that use per-output tokens.
- Unused `GH_AW_SECRET_*` / `SECRET_*` env bindings disappear from agent jobs in regenerated lock files (8 workflows in this PR).
- Token-minting steps can stay job-local in `jobs.safe_outputs.pre-steps`.

#### Negative
- The safe-outputs config is no longer identical across jobs. Readers must know that the agent copy is a filtered projection, and future handler code in the agent/MCP server cannot rely on token fields being present.
- The fix is a deny-list (`github-token`, `head-github-token`, `tools.*.github-token`). Any new credential-bearing field must be added explicitly or it will leak again.
- Every affected workflow's `.lock.yml` must be regenerated, which adds churn to the lock files.

#### Neutral
- Compile-time step-token reference validation stays in place. It now passes when the minting step exists only in the consuming `safe_outputs` job.
- New tests cover token exclusion from the agent config and job-local minting in `safe_outputs` pre-steps. They check that the token does not appear in the `agent` or `conclusion` jobs.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
