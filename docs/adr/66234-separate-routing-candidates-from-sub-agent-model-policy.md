# ADR-66234: Separate Router Candidate Models from the API-Proxy Request Allowlist

**Date**: 2026-10-06
**Status**: Draft
**Deciders**: gh-aw maintainers (pending review; PR author: app/copilot-swe-agent, requested by pelikhan)

---

### Context

When a workflow enables `engine.model-routing`, the gh-aw compiler emitted a single list — the router's `allowed-models` candidate set — as `apiProxy.allowedModels` in the generated AWF config. The API proxy uses `allowedModels` as the *request* policy, so any model not in that list is rejected with a 403. Inline sub-agents (`## agent: \`name\`` blocks) and imported agent files can declare their own `model:` in frontmatter; because those models were never added to `allowedModels`, every such sub-agent request failed. The obvious workaround — adding the sub-agent's model to `allowed-models` — also makes that model a candidate for the router's main-task selection, which changes routing behavior for the whole workflow. AWF v0.28.33 introduced `apiProxy.routing.candidateModels`, which lets the two concerns be expressed independently, but older AWF versions do not understand the field.

### Decision

We will emit the two lists separately when the workflow pins AWF v0.28.33 or newer (`constants.AWFRoutingCandidateModelsMinVersion`): the router's policy-filtered candidate set goes to `apiProxy.routing.candidateModels`, while `apiProxy.allowedModels` is extended with the models declared by inline and imported sub-agents, after resolving aliases through the workflow's `models` alias map and re-applying `models.allowed` / `models.blocked`. On older AWF versions the previous single-list behavior is preserved unchanged, and the compiler emits warnings when a declared sub-agent model cannot be admitted by policy or when the default pinned API-proxy image predates `candidateModels` support. The primary driver is correctness: a sub-agent's model declaration must be honored as a request permission without silently widening the router's main-task search space.

### Alternatives Considered

#### Alternative 1: Keep the single list and just union sub-agent models into `allowed-models`

The simplest fix would be to append every declared sub-agent model to the existing `allowedModels` list, with no `candidateModels` split. This was considered because it requires no AWF version gate, no new schema field, and no compatibility warnings. It was rejected because `allowedModels` is also what the router uses to pick the model for the main task, so a sub-agent that declares a cheap model (e.g. `claude-haiku-4.5`) would make that model eligible for the primary objective and silently change routing outcomes — the exact coupling this PR exists to break.

#### Alternative 2: Require authors to declare sub-agent models explicitly in `engine.model-routing`

We could have left the compiler untouched and documented a new frontmatter knob (for example a separate `request-models` list) that authors must fill in by hand. This was a genuine option because it is fully explicit and avoids inferring intent from sub-agent frontmatter. It was rejected because the information is already present and unambiguous in the sub-agent's own `model:` field; duplicating it invites drift between the agent definition and the proxy policy, and it does not fix existing workflows without an author edit.

#### Alternative 3: Always emit `candidateModels` regardless of pinned AWF version

Emitting the new field unconditionally would remove the version branch and the two warning paths. It was rejected because the AWF config schema is validated by the proxy at runtime and older images would either reject the unknown field or ignore it, downgrading the router to an unconstrained candidate set. Preserving the legacy shape below v0.28.33 keeps pinned workflows byte-compatible.

### Consequences

#### Positive
- A sub-agent declaring `model:` now receives a working API-proxy permission instead of a 403, for both inline `## agent:` blocks and imported agent files.
- Router candidate selection is no longer perturbed as a side effect of granting a sub-agent request permission; the two lists are independently auditable in the generated config.
- `models.allowed` / `models.blocked` and the alias map remain the single policy authority: an unadmittable sub-agent model is dropped and surfaced as a compile-time warning rather than failing opaquely at runtime.

#### Negative
- Behavior now forks on the pinned AWF version, so the compiler carries a version gate plus two compatibility warning paths that must be maintained until the floor rises above v0.28.33.
- Sub-agent model extraction is duplicated across three parse paths (main markdown in `compiler_orchestrator_tools.go`, inline imports in `import_field_extractor.go`, agent files in `import_bfs.go`), increasing the chance that a future import mechanism forgets to populate `SubAgentModels`.
- `subAgentRequestModels` implements bespoke wildcard/alias matching against the policy lists, which is non-trivial logic that partially overlaps `intersectModelRoutingPolicy` and `matchesModelPolicy`.

#### Neutral
- `WorkflowData` gains a `SubAgentModels []parser.SubAgentModel` field and `ImportsResult` gains the matching accumulator plumbing; `pkg/workflow` now depends on a new exported parser type.
- `pkg/workflow/schemas/awf-config.schema.json` adds an optional `candidateModels` array (`minItems: 1`) under `apiProxy.routing`.
- Workflows that pin the default routed API-proxy image will see a new advisory warning until they pin a current AWF image in `sandbox.agent.images`.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
