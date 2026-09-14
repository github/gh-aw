---
name: operational-value-designer
description: "Design and verify a deterministic operational-value grader for any GitHub Agentic Workflow. Use when reasoning from workflow goals to measurable downstream outcomes, defining repository evidence, choosing outcome metrics, or creating an operational-value evaluator. Usage: /operational-value-designer OWNER/REPO WORKFLOW-NAME."
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

Choose one evaluator form. For a compact evaluator, embed the complete Bash program in the workflow so it travels with the Markdown:

```yaml
graders:
  operational-value:
    name: Domain Outcome Conformance
    description: Whether the current run produced the required domain outcome
    unit: ratio
    direction: higher_is_better
    script: |
      #!/usr/bin/env bash
      set -euo pipefail
      request=$(cat)
      # Compute and print the ordered metric array.
```

For a larger evaluator, create one executable file at:

```text
.github/graders/WORKFLOW-NAME-operational-value.sh
```

and configure the workflow:

```yaml
graders:
  operational-value:
    name: Domain Outcome Conformance
    description: Whether the current run produced the required domain outcome
    unit: ratio
    direction: higher_is_better
    run: .github/graders/WORKFLOW-NAME-operational-value.sh
```

Always set a concise `name`, `description`, `unit`, and `direction` for the primary metric. At the top of the evaluator, comment the workflow intent and the meaning of every emitted metric, including its `1`, `0`, and `null` interpretations. These comments are frozen and archived with the evaluator bytes, preserving review context without adding a definition mode or a second metadata schema.

Specify exactly one of `script` or `run`. Use this decision rule:

- Choose `script` for compact, workflow-specific Bash that remains easy to review inside the Markdown and can be covered by the workflow's tests.
- Choose `run` when the Bash is large enough to obscure the workflow, is maintained or reused independently, or benefits from dedicated semantic fixtures and shell tooling.

The operational-value evaluator limit is **65,536 UTF-8 bytes, inclusive**, for both forms. `run` does not permit a larger evaluator. This differs from ordinary custom inline JavaScript graders, whose limit is **4,096 Unicode characters, inclusive**.

Both forms are frozen into the compiled workflow, archived with the run, and identified by the same SHA-256 digest. They produce the same GitHub Actions execution payload, so `run` does not reduce the generated workflow size; Base64 transport adds roughly 33% to the evaluator bytes in either case. Choose by readability and testability, not Actions payload size. If an evaluator exceeds 65,536 bytes, simplify its evidence logic rather than switching forms to bypass the limit.

For a file-backed evaluator, create exact semantic fixtures at:

```text
.github/graders/WORKFLOW-NAME-operational-value.fixtures.json
```

Each fixture contains exactly `name`, `request`, and `expected`. Include `attained`, `missed`, `unavailable`, and `malformed`; also include `noop` and `inapplicable` when those states exist. Expected metrics must be exact, ordered, and deterministic. For inline Bash, encode equivalent cases in the workflow's tests and compile the workflow before adoption.

Before implementation, summarize the design in a compact table containing the ultimate goal, outcome ladder, selected measurable effect, why stronger downstream effects are unavailable, primary metric and formula, applicability, success evidence, zero condition, null condition, noop interpretation, adoption point, and required API calls. Surface unresolved ambiguity instead of hiding it in code.

## Design Procedure

### 1. Resolve workflow intent

Validate `OWNER/REPO` and resolve `.github/workflows/WORKFLOW-NAME.md`. Do not infer the target repository or workflow from the current checkout, remotes, generated lock files, or similarly named files.

Read the workflow title or `name`, `description`, canonical top-level `intent:`, effective Markdown body, and prompt imports together. Prefer an explicit `intent:` when these sources conflict. Recover:

- the subject the workflow acts on;
- the ultimate repository or operational condition it is meant to improve;
- the causal path from the workflow's immediate action to that downstream condition;
- explicit success conditions;
- conditions where doing nothing is correct.

Use `evals`, deterministic steps, custom jobs, and safe outputs only as corroborating evidence. Triggers, tools, permissions, and output types describe mechanics; they do not define value by themselves. Never equate “requested a safe output” with “achieved the workflow's goal” unless the Markdown makes that request itself the intended outcome and its required content can be verified.

Resolve referenced prompt or policy files that materially define the goal. If an import is unavailable, report the missing authority instead of guessing. Treat generated files, caches, prior reports, and model output as evidence, not as normative truth, unless the workflow explicitly designates them as authoritative.

Write two sentences before choosing a metric:

> Ultimately, this workflow creates value when ...
>
> For this run, the strongest attributable effect observable at grading time is ...

If either sentence cannot be completed from authoritative workflow content and available evidence, stop and report the ambiguity instead of inventing a metric.

Translate the sentence into a function before writing shell code:

```text
f(run, event, config, observable evidence) -> [{id, value}, ...]
```

For every input, the function must define whether the run was applicable and whether the result is attained, missed, correctly restrained, or unavailable. The implementation should be a direct encoding of this function.

### 2. Identify the valuable effect

First define the unit being evaluated: one event, one issue or pull request, one repository scan, one batch of eligible items, or another subject named by the workflow. Do not default to “one emitted output.” A scheduled monitoring run can be applicable even when it finds no unhealthy items because the repository scan itself is the subject; an item-processing run with no eligible items is usually not applicable.

If the workflow intentionally samples, caps, or rotates through a larger population, state whether the limit defines the intended sample or is only an execution safety cap. A declared sampling rule defines the unit and denominator; a safety cap does not make unprocessed eligible items disappear. Name and interpret a sample metric at that scope; do not extrapolate it to the whole repository.

For workflows driven by user input, bind the unit to that exact target. An otherwise valid result for a different issue, URL, repository, ref, theme, or requested mode scores `0`.

Do not begin with the workflow's output type and turn its presence into the metric. First build an outcome ladder from the goal back toward execution:

```text
ultimate operational condition
  <- durable downstream outcome
  <- applied repository or service change
  <- accepted or verifiable requested action
  <- execution activity
```

Adapt the ladder to the domain; not every workflow has every rung. For each rung, ask whether the effect is observable at grading time, attributable to this run or its exact subject, and independently verifiable. Select the furthest downstream rung that satisfies all three. Walk backward only when a stronger rung fails one of those tests, and record the specific evidence gap. Easy-to-count outputs and workflow mechanics must not displace a measurable downstream effect.

Typical measurable effects, strongest first, are:

1. **Durable outcome already established**: completed release, repository mutation, validated state transition, or another lasting change completed before grading.
2. **Applied intermediate effect**: an accepted issue, merged patch, delivered notification, completed dispatch, or other causal step already proven to have occurred and still attributable to the run.
3. **Verifiable requested action**: a review finding, issue, report, recommendation, patch, dispatch, or noop request whose content and choice satisfy explicit workflow criteria.
4. **Correct restraint**: an explicit noop when an eligible subject exists and evidence proves no action is appropriate.

Prefer established outcomes over applied intermediate effects, applied effects over requested actions, and requested actions over execution traces. Never reward output merely for existing. A requested issue is valuable only when no stronger downstream effect is currently measurable and it is an independently checkable precursor on the causal path to the ultimate goal.

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

The function must be actor-independent: identical accepted evidence must receive the same score whether it was produced by this agent, another engine, a person, or deterministic automation. Agent identity, tool choice, and execution trace are not operands unless the workflow explicitly tests that capability.

Prefer evidence in this order:

1. the event payload and run subject;
2. already materialized workflow inputs, outputs, and safe-output requests;
3. repository state at the run SHA;
4. narrowly scoped GitHub API reads needed to fill a specific gap.

Do not re-fetch data already captured with sufficient fidelity. For batch workflows, define the eligible set and denominator from one consistent snapshot. Do not use historical expectations, another model's findings, the evaluator's own output, or the workflow's confidence as ground truth.

Whenever the metric judges a workflow decision, derive the expected decision independently from source evidence and compare it with the observed workflow request. Do not accept the workflow's explanation as proof that its decision was correct. Existing `evals` are evidence only for the exact predicate they evaluate; an eval that checks whether output exists does not prove that output is accurate.

When evidence sources conflict, apply an explicit precedence justified by the workflow or return `null`; never choose whichever source produces a better score. Validate current-run caches and precomputed files for their expected completion marker, count, or schema before using them. If an expected batch snapshot is missing, stale, truncated, capped, or only partially parsed, return `null` rather than silently shrinking the denominator. Apply intentional eligibility filters before fixing the denominator, then count every eligible item whether processed or missed.

A declared sampling rule bounds the selected set; items outside that intentional sample are not misses. An execution safety cap does not shrink the eligible denominator: score the complete eligible set when evidence supports it, or return `null` when the cap prevents complete evaluation. Within the selected set, compare the complete expected action set with the complete observed request set. This is mandatory for destructive actions such as closing, deleting, relabeling, or superseding items: an unjustified extra mutation is a miss, not partial credit.

For time-based eligibility, use one declared UTC reference instant and define every boundary as inclusive or exclusive. Do not round to dates, use the evaluator's wall clock, or tolerate clock skew unless the workflow explicitly declares that behavior.

Treat thresholds, tolerances, and policy cutoffs as authoritative only when the workflow or a referenced policy declares them. Do not infer a regression threshold from noisy measurements, tune it against the current result, or invent a historical baseline. If a declared benchmark cannot be reproduced under its required environment and inputs, return `null`.

Treat retries and repeated schedules as independent runs unless deduplication or idempotence is part of the workflow's stated goal. When it is, independently verify that the repeat should act or noop from current evidence; do not add a generic cross-run identity system.

For experiment variants, apply the same acceptance function to the declared subject and variant. Grade the current run's outcome, not whether its variant beat another run, unless the workflow supplies a complete fixed comparison dataset and deterministic decision rule at the grading boundary.

If intended value depends on future events or human judgment unavailable during the run, do not invent a maturation window or silently substitute engagement. Preserve the downstream goal in the design, state the missing evidence, then measure the strongest independently checkable precursor available now and name it honestly. Return `null` when no meaningful deterministic per-run outcome can be observed.

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

Higher must always mean more value. Keep a metric ID stable while it continues to describe the same outcome.

Freeze the metric prospectively to prevent hindsight bias:

- choose the formula, thresholds, evidence rules, and fixtures before inspecting any scored outcomes;
- commit the workflow and its inline evaluator, or the workflow and referenced evaluator file, together; that commit is the adoption point for the pair;
- treat the evaluator as read-only while the workflow's intent and acceptance criteria are unchanged;
- when those workflow semantics change, update the workflow and evaluator together at the same path and commit; the new pair applies only to future runs;
- allow an evaluator-only defect correction only prospectively, without changing or regrading prior results;
- retain the metric ID when the measured outcome still means the same thing; choose a new descriptive ID only when the outcome itself changes;
- score each run with the evaluator bytes and configuration frozen into that run. Never move adoption backward or tune a function against observed scores.

The workflow commit and evaluator digest preserve each historical pair, including inline Bash extracted from the committed Markdown. The current evaluator may replace the old one in the same field or path. Do not add versioned filenames, registries, or a second provenance service.

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
  "outputs": [],
  "config": {}
}
```

`outputs` contains the current run's validated safe-output requests from `agent_output.json`. Treat them as requested actions, not proof that the corresponding GitHub mutations were applied.

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
.github/skills/operational-value-designer/scripts/verify-operational-value-contract-change.sh BASE-REF
# File-backed evaluator:
.github/skills/operational-value-designer/scripts/verify-operational-value-evaluator.sh \
  .github/graders/WORKFLOW-NAME-operational-value.sh \
  .github/graders/WORKFLOW-NAME-operational-value.fixtures.json
# Both forms:
gh aw compile .github/workflows/WORKFLOW-NAME.md
```

Review the design against these checks:

- The intent sentence describes an outcome, not activity.
- The outcome ladder starts from the ultimate goal rather than the configured output type.
- The primary metric uses the furthest downstream effect that is currently observable, attributable, and independently verifiable.
- Any fallback to a precursor states exactly why a stronger downstream effect cannot be measured at the grading boundary.
- Missing evidence for the selected rung returns `null`; it never causes runtime fallback to a weaker rung.
- The unit of evaluation is explicit and matches the workflow's actual subject.
- The primary metric directly answers that sentence.
- Applicable, successful, missed, correct-restraint, and unavailable cases are distinguishable.
- Evidence is attributable to the run or its subject.
- The metric uses only evidence available at the grading boundary and does not treat requested safe outputs as applied mutations.
- Identical accepted evidence scores identically regardless of actor or engine.
- Zero means observed non-attainment; `null` means no opportunity or unavailable evidence.
- The denominator cannot silently reward skipped or missing work.
- Diagnostics are independently useful and do not duplicate the primary metric.
- The workflow and evaluator were adopted together, and neither the function nor prior results were changed retroactively.
- External research, if used, changed a definition rather than adding prestige or complexity.
- The evaluator makes the minimum necessary API calls.
- The output is only an ordered array of exact `{id,value}` objects.
- Exact fixtures prove attained scores above missed, unavailable and malformed evidence return the declared result, and repeated evaluation is deterministic.