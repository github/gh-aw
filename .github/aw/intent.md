---
description: Design-time guidance for deriving an outcome-oriented workflow intent and using it to select, implement, and evaluate an agentic workflow.
---

# Intent-Driven Workflow Design

Start every workflow design by extracting a concise, implementation-independent outcome. Persist that canonical outcome in the top-level `intent:` frontmatter field; use the richer analysis below only while designing the workflow.

```yaml
intent: Reduce maintainer effort spent identifying recurring actionable CI regressions without generating duplicate work.
```

An **intent** is the repository outcome that a workflow should attain for a defined actor and subject. It describes why the workflow exists, not its trigger, tool, schedule, output volume, or write action. It must remain valid if the implementation changes from an immediate issue to a weekly digest.

## Derive an IntentSpec

Before selecting implementation details, derive this transient model:

```text
IntentSpec
- intent: concise canonical outcome
- actors and subject: who benefits and what is affected
- activation conditions: facts that make the intent relevant
- required context: evidence needed to make a decision
- required effects: observable results that satisfy the outcome
- noop conditions: inverse cases that must not create attention or writes
- success conditions: how the design avoids attention cost while producing value
- uncertainties: policy or evidence gaps that need a conservative default or clarification
```

Do not serialize this structure in workflow frontmatter. The `intent:` value is the only persisted part. Do not duplicate executable configuration such as `on:`, `tools:`, `permissions:`, `safe-outputs:`, or schedules in it.

For an explicit, narrow request, infer the obvious intent and keep this pass lightweight. For an underspecified request or one asking what to automate, collect bounded repository evidence first, then propose evidence-backed candidate intents with their evidence, feasibility, expected value, risk, and uncertainties. Do not perform a broad survey when it cannot materially change a clear request.

## Design from the IntentSpec

Use the model to derive implementation rather than mapping the request directly to a trigger:

1. Compare plausible architectures against intent coverage, timeliness, attention cost, safety, boundedness, determinism, state requirements, implementation complexity, and available evidence.
2. Select the trigger, schedule, data collection, tools, permissions, safe outputs, and deduplication strategy that satisfy the selected architecture.
3. Put the activation conditions, required effects, evidence threshold, and no-op conditions in the prompt body. Require `noop` when a counter-case applies or evidence is insufficient.
4. Make duplicate detection, filters, output caps, and previous-result strategy enforce the same conditions where configuration can do so.

For example, an intent to surface actionable CI regressions can require completed relevant CI, actionable and novel evidence, and sufficient diagnostics. Known flakes, infrastructure failures, already-tracked regressions, closed pull requests, and insufficient evidence are counter-cases. An immediate incident, PR comment, daily digest, and weekly trend report are alternative architectures; choose the one that best meets the intent without unnecessary attention cost.

## Apply PromptPex to Derive Evals

PromptPex treats the prompt as a behavioral specification and expands each intent condition into a concrete scenario. Use the IntentSpec to generate both sides of the behavior:

1. For each activation condition and required effect, create a positive fixture in which the workflow should produce an observable result. Derive an eval that returns `YES` only when the output demonstrates that result.
2. Invert each activation condition, required effect, and evidence threshold to find counter-intent cases such as irrelevant, duplicate, benign, stale, or insufficiently evidenced input.
3. Create an inverse fixture for each meaningful counter-case. Derive an inverse eval that returns `YES` only when the output demonstrates the intended no-op, bounded investigation, or other safe behavior.
4. Check that the positive and inverse fixtures jointly cover the intent without prescribing the implementation.

Create representative positive and adversarial scenario fixtures from required effects and no-op conditions. A BinEval run evaluates one provided scenario and one `agent_output.json`; do not combine mutually exclusive scenarios into one unconditional question list. For each fixture, use a separate scenario-specific eval question about the observable agent output:

- a novel, sufficiently evidenced actionable case produces the intended visible result;
- a duplicate or known benign case produces no visible write;
- an uncertain case investigates when appropriate but does not write.

Keep eval questions binary and output-observable. If a shared eval suite must accept different scenarios, make applicability explicit and treat a scenario that was not provided as `UNKNOWN`, not as a failure. Do not ask a judge whether the intent itself is good or whether the agent made sufficient effort.

See [evals.md](evals.md) for BinEval syntax and question constraints.

## Infer Operational Value from Intent

Infer one per-run attainment metric from IntentSpec, not activity, output volume,
or the agent's assessment:

1. Bind actors, subject, and activation to one applicable run opportunity.
2. Map required effects to accepted recorded evidence and a native-scale metric.
3. Map observed misses to zero and absent/uncertain evidence to `null`.
4. Declare noop, maturity, repository, and target-matching rules.

Evals test representative output behavior; operational value measures the
strongest attributable recorded effect for a real run. Follow the
[operational-value-designer skill](../skills/operational-value-designer/SKILL.md).
`graders.operational-value.script` receives immutable session inputs downstream
and returns ordered `{id,value}` metrics; the first is primary. Never fetch
missing evidence. Removed `graders.operational-value.run` declarations must fail
with migration guidance and must not be translated automatically.

gh-aw owns recorded facts, run-scoped grading, and shared delivery/adoption/
reversal/survival rules and provider refresh. GitHub owns resource disposition.
Inspect lifecycle evidence with `logs --delivery`, `logs --adoption`, or single-run
`audit --adoption`; no CAO or campaign is required.
Attributed value additionally requires a declared measurement, attribution evidence,
and complete post-adoption-window survival. CAO is an optional scheduler/consumer.
`graders.operational-value.script` is optional recorded evidence, not attributed
value by itself.

For execution-quality diagnostics, follow [Infer Trace Graders](#infer-trace-graders-from-intent).

<!-- instruction-prefix-end -->

## Infer Trace Graders from Intent

Ten fast built-ins are enabled by default. Configure their thresholds or disable irrelevant metrics; enable additional analyses only for concrete risks to achieving the intent. Use `tool-success-rate` and `tool-failure-count` when reliable collection is required; `retries`, `loops`, `execution-step-count`, and `execution-duration` when boundedness or timely escalation matters; `working-set-rebuild-factor` and `context-growth` when repeated context is an attention or cost risk; `trajectory-efficiency` when unnecessary tool churn is a concern; and `artifact-production` only when producing the intended artifacts is itself useful diagnostic evidence.

Then inspect the implemented fragments in [`shared/graders/`](../workflows/shared/graders/README.md). Use `policy-near-miss` for explicit guard or no-op requirements, and `skill-constraint-coverage` when a harness/skill declares behavioral requirements that should be exercised. Use `exploration-error` when failure may stem from insufficient search, and `exploitation-error` when the agent had enough information but failed to use it. For intents that require efficient investigation rather than repeated exploration, consider `state-revisit-probability-rep`, `recurrence-rate`, `recurrence-determinism`, `recurrence-laminarity`, or `recurrence-trapping-time`. For intents where varied, non-repetitive investigation is relevant, consider `event-entropy-rate` or `lempel-ziv-trajectory-complexity`. Use `tool-output-consumption-rate` when tool outputs going unused by later actions is a risk. Import only the applicable implemented fragments and ensure their documented trace prerequisites are available. These trace graders diagnose execution behavior; they do not replace scenario evals or the operational-value attainment metric.

## Preserve Intent on Updates

Read the existing `intent:` before changing an existing workflow. Preserve it for an implementation-only change, including a trigger or output-channel redesign. Reconsider and update it only when the request materially expands, contracts, or otherwise changes the outcome. When it changes, re-derive conditions, architecture, prompt behavior, and evals.
