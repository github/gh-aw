---
name: operational-value-designer
description: "Design and verify a deterministic operational-value grader for any GitHub Agentic Workflow. Use when choosing outcome metrics, defining repository evidence, or creating an operational-value evaluator. Usage: /operational-value-designer OWNER/REPO WORKFLOW-NAME."
argument-hint: "OWNER/REPO WORKFLOW-NAME"
metadata:
  version: "2.0.0"
---

# Operational Value Designer

Design the smallest deterministic grader that measures whether one workflow run produced its intended operational outcome. Put domain knowledge in the evaluator, not in generic runtime infrastructure.

Operational value is demonstrated progress toward the workflow's intended real-world or repository outcome. It is not agent activity, token usage, output volume, tool usage, or an agent's claim that it succeeded.

The core task is semantic translation:

```text
workflow title + description + intent + effective instructions
  -> real goal and applicable subject
  -> observable evidence
  -> deterministic per-run metric function
```

The evaluator belongs to one workflow and runs whenever that workflow is graded. It must measure that workflow's goal for the current run. It is not a generic safe-output checker: safe outputs are only one possible evidence source and may be irrelevant, insufficient, or merely an intermediate request.

## Deliverables

Create one executable evaluator at:

```text
.github/graders/WORKFLOW-NAME-operational-value.sh
```

Configure the workflow:

```yaml
graders:
  operational-value:
    run: .github/graders/WORKFLOW-NAME-operational-value.sh
```

Before implementation, summarize the design in a compact table containing the intent sentence, primary metric and formula, applicability, success evidence, zero condition, null condition, noop interpretation, and required API calls. Surface unresolved ambiguity instead of hiding it in code. Consult [metric patterns](./references/metric-patterns.md) for calibrated examples across maintenance, routing, triage, reporting, releases, research, and expert review.

## Design Procedure

### 1. Resolve workflow intent

Validate `OWNER/REPO` and resolve `.github/workflows/WORKFLOW-NAME.md`. Do not infer the target repository or workflow from the current checkout, remotes, generated lock files, or similarly named files.

Read the workflow title or `name`, `description`, canonical top-level `intent:`, effective Markdown body, and prompt imports together. Prefer an explicit `intent:` when these sources conflict. Recover:

- the subject the workflow acts on;
- the repository or operational change it is meant to produce;
- explicit success conditions;
- conditions where doing nothing is correct.

Use `evals`, deterministic steps, custom jobs, and safe outputs only as corroborating evidence. Triggers, tools, permissions, and output types describe mechanics; they do not define value by themselves. Never equate “requested a safe output” with “achieved the workflow's goal” unless the Markdown makes that request itself the intended outcome and its required content can be verified.

Resolve referenced prompt or policy files that materially define the goal. If an import is unavailable, report the missing authority instead of guessing. Treat generated files, caches, prior reports, and model output as evidence, not as normative truth, unless the workflow explicitly designates them as authoritative.

Write one sentence before choosing a metric:

> For each applicable run, the workflow creates value when ...

If that sentence cannot be completed from authoritative workflow content, stop and report the ambiguity instead of inventing a metric.

Translate the sentence into a function before writing shell code:

```text
f(run, event, config, observable evidence) -> [{id, value}, ...]
```

For every input, the function must define whether the run was applicable and whether the result is attained, missed, correctly restrained, or unavailable. The implementation should be a direct encoding of this function.

### 2. Identify the valuable effect

First define the unit being evaluated: one event, one issue or pull request, one repository scan, one batch of eligible items, or another subject named by the workflow. Do not default to “one emitted output.” A scheduled monitoring run can be applicable even when it finds no unhealthy items because the repository scan itself is the subject; an item-processing run with no eligible items is usually not applicable.

If the workflow intentionally samples, caps, or rotates through a larger population, the selected sample is the unit. Name and interpret the metric at that scope; do not extrapolate sample performance to the whole repository.

For workflows driven by user input, bind the unit to that exact target. An otherwise valid result for a different issue, URL, repository, ref, theme, or requested mode scores `0`.

Choose the closest effect that is observable when this run is graded:

1. **Durable outcome already established**: completed release, repository mutation, validated state transition, or another lasting change completed before grading.
2. **Verifiable requested action**: a review finding, issue, report, recommendation, patch, or noop request whose content and choice satisfy explicit workflow criteria.
3. **Correct restraint**: an explicit noop when an eligible subject exists and evidence proves no action is appropriate.

Prefer established outcomes over requested actions, and requested actions over execution traces. Never reward output merely for existing. A requested issue is valuable only if requesting the right issue is the workflow's intended per-run effect or the closest observable precursor to it, and its required content can be checked.

Do not confuse the condition being observed with the workflow's value. A security audit, health report, incident monitor, or grader audit can be fully valuable while reporting severe failures. Score whether the workflow correctly detected, represented, and acted on the condition, not whether the condition was healthy.

No opportunity and correct restraint are different:

- If no eligible subject or decision existed, the primary metric is `null`.
- If an eligible subject existed and evidence proves that no action was correct, restraint may score `1`.
- Silence, empty output, or an expected historical work rate never proves correct restraint.

### 3. Define applicable runs and evidence

State:

- which runs present a real opportunity for value;
- which evidence proves success;
- which evidence proves a miss;
- when evidence is unavailable and must produce `null`;
- how explicit noop behavior is distinguished from silent failure.

Use only evidence attributable to the run or its subject. Avoid repository-wide changes that could have been caused by unrelated work. Do not add historical replay, maturity periods, baselines, provenance schemas, caches, or opportunity identifiers unless the workflow's own metric genuinely requires them.

Prefer evidence in this order:

1. the event payload and run subject;
2. already materialized workflow inputs, outputs, and safe-output requests;
3. repository state at the run SHA;
4. narrowly scoped GitHub API reads needed to fill a specific gap.

Do not re-fetch data already captured with sufficient fidelity. For batch workflows, define the eligible set and denominator from one consistent snapshot. Do not use historical expectations, another model's findings, the evaluator's own output, or the workflow's confidence as ground truth.

Whenever the metric judges a workflow decision, derive the expected decision independently from source evidence and compare it with the observed workflow request. Do not accept the workflow's explanation as proof that its decision was correct. Existing `evals` are evidence only for the exact predicate they evaluate; an eval that checks whether output exists does not prove that output is accurate.

When evidence sources conflict, apply an explicit precedence justified by the workflow or return `null`; never choose whichever source produces a better score. Validate current-run caches and precomputed files for their expected completion marker, count, or schema before using them. If an expected batch snapshot is missing, stale, truncated, capped, or only partially parsed, return `null` rather than silently shrinking the denominator. Apply intentional eligibility filters before fixing the denominator, then count every eligible item whether processed or missed.

A declared processing or output cap bounds the selected set; items outside that set are not misses. Within the selected set, compare the complete expected action set with the complete observed request set. This is mandatory for destructive actions such as closing, deleting, relabeling, or superseding items: an unjustified extra mutation is a miss, not partial credit.

Treat thresholds, tolerances, and policy cutoffs as authoritative only when the workflow or a referenced policy declares them. Do not infer a regression threshold from noisy measurements, tune it against the current result, or invent a historical baseline. If a declared benchmark cannot be reproduced under its required environment and inputs, return `null`.

Treat retries and repeated schedules as independent runs unless deduplication or idempotence is part of the workflow's stated goal. When it is, independently verify that the repeat should act or noop from current evidence; do not add a generic cross-run identity system.

For experiment variants, apply the same acceptance function to the declared subject and variant. Grade the current run's outcome, not whether its variant beat another run, unless the workflow supplies a complete fixed comparison dataset and deterministic decision rule at the grading boundary.

If intended value depends on future events or human judgment unavailable during the run, do not invent a maturation window or silently substitute engagement. Measure the closest independently checkable outcome available now and name it honestly. Return `null` when no meaningful deterministic per-run outcome can be observed.

Dependency failure is `null` when it prevents evidence collection for some other goal. It is `0` when the dependency or permission is itself the capability under test, such as an authentication smoke test.

### 4. Respect the grading boundary

The evaluator runs once for the current workflow run. It does not wait for future acceptance, replay history, or revise an observation later. Safe-output requests may be graded before the requested GitHub mutation is applied.

Therefore:

- never claim that an issue, pull request, comment, label, or release exists merely because the agent requested it;
- grade the requested action and its content when application has not yet occurred;
- for chained dispatches or downstream workflows, grade only the current run's verifiable dispatch request unless a completed downstream effect is already part of current-run evidence;
- use a durable repository effect only when evidence proves it already occurred;
- describe proposed code changes as requested patches until merge or application is already proven;
- do not query future commits, later incidents, subsequent human reactions, or historical trend windows;
- keep delayed adoption, long-term quality, and causal impact outside the per-run metric.

When the long-term goal cannot be observed yet, name the immediate metric precisely, such as `actionable-refactor-request` rather than `file-decomposed`.

### 5. Research domain conventions only when needed

If the workflow does not make a direct metric clear, inspect at most three targeted external sources for established definitions, denominators, and known measurement failures in that domain. Prefer primary standards, official documentation, and peer-reviewed or widely accepted technical references. Stop when one authoritative definition and its main failure mode are understood; broad literature review is not part of this task.

External research may refine what to measure; it must not:

- override the workflow's stated intent;
- import an industry benchmark without checking that it fits this workflow;
- turn correlation into attribution;
- make external web data a runtime dependency when repository or GitHub evidence is sufficient;
- justify an activity proxy because it is easy to count.

Adopt an external definition only when all operands and ground truth are observable at the per-run grading boundary. For example, a standard may identify precision and recall as useful dimensions, but neither is a valid per-run metric without independently known true and false cases.

When research is used, add a short research note to the design table: source URL, definition considered, what was adopted or rejected, and why. Record a source in evaluator comments only when it materially affects the implemented formula. Runtime grading must remain deterministic from the request, repository state, declared GitHub access, and workflow outputs; it must never browse the web for metric design.

When live external data is itself the workflow's declared subject, such as a model or service inventory, use the workflow's captured response or the narrow declared authoritative API as evidence. Pin the endpoint and required fields in the design, validate completeness, and return `null` on unavailable, truncated, or incompatible responses; do not substitute search results or design-time research.

### 6. Choose the smallest useful metric set

Choose one primary metric that answers the intent sentence directly. Use a precise domain name such as `eligible-issues-triaged`, `security-review-policy-conformance`, or `release-request-valid`, not `operational-value` or `success`. Do not name a metric after a stronger claim than its evidence proves.

Add a diagnostic only when it explains a distinct failure mode and can change an operational decision. Do not combine unrelated outcomes into a weighted score merely to produce one number. If the workflow has independent goals, select the one declared as primary or report the ambiguity.

Use the simplest defensible formula:

- binary `0` or `1` for discrete outcomes;
- a proportion with an explicit numerator and denominator for sets;
- bounded progress toward a declared target for continuous outcomes;
- `null` when applicability or evidence cannot be established.

For proportions, define every numerator and denominator term and prevent missing items from disappearing from the denominator. A quality metric requires independent acceptance criteria or ground truth; the workflow cannot grade its own judgment by counting its findings.

Translate qualitative words such as “actionable,” “correct,” “relevant,” “complete,” and “high quality” into deterministic predicates grounded in the workflow Markdown. For example, an actionable incident report might require the triggering environment, a failing step, linked evidence, and a concrete remediation. If semantic correctness cannot be determined without another model or later human judgment, narrow the metric to the strongest deterministic claim available, such as `required-incident-analysis-present`, and state that limitation in the design table.

For creative or aesthetic goals with no objective acceptance criteria, do not manufacture operational value from length, output existence, or model ratings. Measure only explicit structural or target-binding requirements under a narrowly named metric, or report that no meaningful deterministic grader can be designed.

Validation supports only the property it checks. A passing formatter proves formatting, a focused test proves the tested behavior, and a successful build proves buildability; none alone proves semantic improvement or absence of regressions. Name the metric after the verified property and include every workflow-required check in the expected decision.

Higher must always mean more value. Keep metric IDs stable after adoption.

### 7. Implement the evaluator

Use Bash 3.2-compatible Bash and `jq`. The evaluator runs once, accepts no mode arguments, reads one request from stdin, and writes one result to stdout.

Input:

```json
{
  "schemaVersion": 1,
  "run": {
    "id": "12345",
    "attempt": 1,
    "repository": "OWNER/REPO",
    "workflow": "Workflow name",
    "ref": "refs/heads/main",
    "sha": "...",
    "eventName": "issues"
  },
  "event": {},
  "config": {}
}
```

Output:

```json
[
  {"id": "domain-primary-metric", "value": 0.75},
  {"id": "optional-diagnostic", "value": null}
]
```

The output must be one non-empty ordered array. The first item is primary. Later items are optional diagnostics. Every object must contain exactly `id` and `value`; IDs must be non-empty and unique; values must be finite numbers in `[0,1]` or `null`.

The evaluator must:

- consume stdin once and write only the metric array to stdout;
- write human-readable diagnostics to stderr;
- return the same result for the same evidence;
- use `null`, not zero, for missing or malformed required evidence;
- request no more GitHub permissions or API calls than its evidence requires;
- avoid network calls when local event, repository, or workflow-output evidence is sufficient.
- consume existing validation artifacts instead of repeating expensive builds, browsers, services, or scans;
- never invoke another model or agent to grade the workflow's model or agent output.

### 8. Verify and review

Run:

```bash
.github/skills/operational-value-designer/scripts/verify-operational-value-evaluator.sh \
  .github/graders/WORKFLOW-NAME-operational-value.sh
gh aw compile .github/workflows/WORKFLOW-NAME.md
```

Review the design against these checks:

- The intent sentence describes an outcome, not activity.
- The unit of evaluation is explicit and matches the workflow's actual subject.
- The primary metric directly answers that sentence.
- Applicable, successful, missed, correct-restraint, and unavailable cases are distinguishable.
- Evidence is attributable to the run or its subject.
- The metric uses only evidence available at the grading boundary and does not treat requested safe outputs as applied mutations.
- Zero means observed non-attainment; `null` means no opportunity or unavailable evidence.
- The denominator cannot silently reward skipped or missing work.
- Diagnostics are independently useful and do not duplicate the primary metric.
- External research, if used, changed a definition rather than adding prestige or complexity.
- The evaluator makes the minimum necessary API calls.
- The output is only an ordered array of exact `{id,value}` objects.