# ADR-77588: Let a Model Expression Choose the Routing Level at Run Time

**Date**: 2026-11-13
**Status**: Proposed
**Deciders**: rabeckett; pending maintainer review (pelikhan)

---

### Context

A workflow picks its model with `model:` or `engine.model`. A literal router value (`router`, `router/economy`, `router/balanced`, `router/robust`) turns on model routing, but the value is fixed in the workflow file, so each workflow runs at one level. Workflows whose right level depends on the dispatched item, the changed files, run history or an experiment must be duplicated once per level. Before this change, an expression that resolved to a router value at run time stopped the run before the agent started, and a model chosen in a pre-activation step was empty in the agent job because each job re-evaluated the `model:` expression. The routing decision must stay identical across the activation, agent, threat-detection and `aw_info` jobs, router values must not reach the shared model variables used by threat detection and evals, and the change must not add new frontmatter. This decision was proposed in issue #77587.

### Decision

We will accept a router value from a main-agent `model:` or `engine.model` that is a single Actions expression, on Copilot, Claude and Codex workflows that are eligible for default routing. The activation job resolves the expression once and publishes the routing decision (`model-routing`, `model-routing-mode`, `model-selector`), and later jobs read those outputs instead of re-evaluating the expression. The primary driver is simplicity: the level is chosen inside the existing `model:` field, with no second routing setting. Expressions that read step outputs, `github.job` or `hashFiles()`, and workflows that are not eligible, keep today's behavior, and malformed router values stop the run in activation.

### Alternatives Considered

#### Alternative 1: Keep literal router values and one workflow copy per level

Status quo. Each level needs its own workflow file, or the author accepts one fixed level. This was the only way to vary the level, and it adds no runtime behavior, no second compiled path and no new validation. It was not chosen because the level cannot depend on dispatch inputs, changed files or run history without duplicating workflows, and the duplication cost grows with each use case.

#### Alternative 2: New frontmatter for deciders and floors

Add a dedicated routing-selector or floor setting beside `model:` and `engine.model-routing`, so the level comes from a declared decider. This would make intent explicit and keep `model:` a plain model name. It was not chosen because it creates a second routing surface next to `model: router/*` and `engine.model-routing`, and the decider and floor rules would need their own schema, documentation and tests. This was a close call, since explicitness was attractive; the issue defers it as out of scope.

#### Alternative 3: Accept router values from the engine model variables

Allow `GH_AW_MODEL_AGENT_<ENGINE>` and `GH_AW_DEFAULT_MODEL_<ENGINE>` to hold router values, so an organization can set a default level without editing workflows. This is attractive for central control. It was not chosen for this change because threat detection and evals read the same variables, so a router value there would reach components that must stay unrouted. A router value in those variables still stops the run, and this remains a follow-up that needs its own guards.

### Consequences

#### Positive

- A workflow can vary its routing level per run from dispatch inputs, pre-activation steps, experiments or `github.event` without duplicating workflow files.
- Activation resolves the model expression once. This also fixes the `needs-consistency` case where a pre-activation model was empty in the agent job, for concrete models as well as router values.
- The agent, threat detection and `aw_info` read one routing decision, so the record matches what actually ran.

#### Negative

- A router value that is malformed at run time (for example `router/fast`, `Router/Economy` or `router/economy?effort=high`) is caught only in activation, so the run stops after dispatch instead of failing at compile time.
- Eligible workflows compile both a routed and a non-routed path, which enlarges the generated YAML and the test matrix.
- Expressions are not restricted to safe contexts. Input that reaches the expression can raise the level, and so the spend, up to what `models.allowed` and the model policy permit. Authors should prefer comparisons to passing input through.
- On a routed run, threat detection uses its own default model rather than the main agent's model, so detection does not run on the same model as the agent.

#### Neutral

- An empty value falls back to the model variables and then the engine default, without routing. Authors who want a default must write it into the expression.
- Pi is not supported yet, and router values in the model variables remain rejected, so the change is partial by design.
- The model alias specification moves to 1.4.0, and the model-routing reference gains a "Choosing the level at run time" section.
- The routed AWF config reads its mode from `GH_AW_MODEL_ROUTING_MODE`, and the dynamic-routing check gates the threat-detection default change in `resolveThreatDetectionModel`.

---

*ADR drafted by [adr-writer agent] and reviewed by the PR author. Change the status to Accepted once a maintainer approves.*
