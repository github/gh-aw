---
name: operational-value-designer
description: "Design recorded-session outcome metrics using built-ins, isolated JavaScript, or separate-job operational-value Bash. Usage: /operational-value-designer OWNER/REPO WORKFLOW-NAME."
argument-hint: "OWNER/REPO WORKFLOW-NAME"
metadata:
  version: "2.1.0"
---

# Operational Value Designer

## Session grading contract

Built-in and JavaScript graders are pure computation over the redacted in-memory unified session.
Ten fast built-ins are enabled by default and run in conclusion after session
collection. Configure a built-in when it measures the required property;
do not substitute activity metrics for operational outcomes.

Custom metrics use `graders.<distinct-id>.script`: a synchronous JavaScript
body receiving `session`, `config`, and `helpers`. The compiler runs it only
in an isolated downstream `graders` job. Never inject user steps into
`jobs.conclusion` or customize `jobs.graders`. Repository-owned Bash compatibility
instead uses `graders.operational-value.run` in that same separate job, with the
session-file contract below. Built-in implementation overrides remain unsupported.

JavaScript inputs are immutable snapshots. JavaScript graders cannot read files, invoke processes,
use credentials, fetch evidence, call another model, or use clocks/randomness.
Reuse `helpers.createSessionLookups(session)` for statistics and lookups.
Return `null` when required observations are absent; the runtime records
`unavailable`. Never return the string `"unavailable"` or invent a zero.

Preserve historical evaluator files and prior results unless explicitly
retiring their workflow configuration. Freeze the workflow's intent and its
grader acceptance contract together; corrections apply only prospectively.

## Design methodology

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

### Custom safe-output evidence

For custom safe jobs, declare `outputs.status` and `outputs.url` mapped to
execution-step outputs. Use `success`, `failure`, `cancelled`, or `skipped`;
publish `skipped` for staged previews and intentional no-ops. The URL must be
the canonical absolute resource URI returned by the external service, not an
agent-suggested target. Safe scripts instead return `{ success: true, url }`
after the service confirms the operation.

Conclusion records these observations as `safe_output.result` with the normalized
tool name in `data.type`, the execution status in `data.status`, and an optional
URI in `data.url`. Failed, cancelled, and skipped jobs do not publish a created
resource URL. A successful job without a URI proves only successful execution,
not resource creation; missing identity is unavailable evidence when identity is
required. Filter for successful outcomes before counting resources. Historical
events without status remain legacy observations, not newly verified outcomes.
Prepared work-queue adapters still require their independent effect verification.

## Deliverables

Embed a compact pure JavaScript body in the workflow under a descriptive custom
ID. This example checks target binding of recorded comment results, not comment
quality, delivery completeness, or the workflow's ultimate outcome:

```yaml
graders:
  comment-target-match:
    name: Recorded comment target match
    description: Whether recorded comments target the configured issue
    unit: boolean
    direction: higher_is_better
    threshold: 1
    config:
      repository: OWNER/REPO
      issueNumber: 123
    script: |
      if (typeof config.repository !== "string" || !config.repository.trim() ||
          !Number.isInteger(config.issueNumber) || config.issueNumber < 1) return null;
      const { eventsOfType } = helpers.createSessionLookups(session);
      const comments = eventsOfType("safe_output.result").filter(({ data }) => data?.type === "add_comment");
      if (!comments.length || comments.some(({ data }) =>
          typeof data.repo !== "string" || !data.repo.trim() ||
          !Number.isInteger(data.number) || data.number < 1 ||
          typeof data.url !== "string" || !data.url)) return null;
      return comments.every(({ data }) =>
        data.repo === config.repository && data.number === config.issueNumber) ? 1 : 0;
```

Set a concise `name`, `description`, `unit`, and `direction`; a custom `threshold`
requires `direction`. Use data-only `config` for declared targets and policy.
Each script is limited to **256 KiB (262,144 UTF-8 bytes, inclusive)**.
Simplify oversized logic; do not wrap JavaScript in filesystem/process calls.

Check authored scripts with `gh aw lint --rules grader-file-io --javascript-module
/path/to/gh-aw/pkg/workflow/taint/javascript /path/to/repository/.github/workflows/WORKFLOW.md`.
The pinned native parser must already be installed. Treat missing-parser warnings
as unchecked coverage, not approval. Module loading and filesystem/host capabilities
are rejected; collect any required evidence before grading and use session helpers.

Return a finite number, `null`, or an object with `value` and optional `message`,
`details`, and `diagnostics`. Each grader ID produces one measurement, not an
ordered array of metric objects. Use another distinct grader ID only for an
independently useful numeric metric.

Keep exact fixtures for attained, missed, unavailable, and malformed evidence,
plus noop and inapplicable cases when relevant. Each fixture provides an event
array, configuration, and expected numeric/null result. Execute the actual
script through the restricted custom-grader runtime, not an unrestricted
`eval` or shell runner, and repeat identical inputs to check determinism.

Before implementation, summarize the design in a compact table containing the ultimate goal, outcome ladder, selected measurable effect, why stronger downstream effects are unavailable, primary metric and formula, applicability, success evidence, zero condition, null condition, noop interpretation, adoption point, and required recorded observations. Surface unresolved ambiguity instead of hiding it in code.

### Existing Bash evaluators

Retain a repository-owned Bash evaluator when migrating its input is simpler
than rewriting its domain logic:

```yaml
graders:
  operational-value:
    run: .github/graders/WORKFLOW-NAME-operational-value.sh
    unit: boolean
    direction: higher_is_better
    threshold: 1
    config:
      repository: OWNER/REPO
      issueNumber: 123
```

`run` is a confined `.sh` path, not an inline command. Root-relative paths resolve
from the repository; `./` resolves from the declaring workflow. Source must be
UTF-8, start with a Bash shebang, and fit within **64 KiB (65,536 UTF-8 bytes,
inclusive)**. Compilation freezes the bytes into the separate graders job,
never conclusion; evaluating the frozen script needs no source checkout.

Refactor legacy stdin-request reads to session JSONL in `$1` /
`GH_AW_SESSION_FILE`, and parse JSON configuration from `GH_AW_GRADER_CONFIG`.
Stdin is empty. Replace legacy `.run`, `.event`, and `.outputs` wrapper
assumptions with recorded unified events. For example, the same narrow target
binding predicate above can retain its Bash metric-array output:

```bash
#!/usr/bin/env bash
set -euo pipefail
session_file=$1
jq -s --argjson config "$GH_AW_GRADER_CONFIG" '
  def repository: type == "string" and test("\\S");
  def issue: type == "number" and . >= 1 and floor == .;
  [.[] | select(.type == "safe_output.result" and .data.type == "add_comment") | .data] as $comments
  | if (($config.repository | repository) and ($config.issueNumber | issue) | not)
       or ($comments | length == 0)
       or any($comments[];
         ((.repo | repository) and (.number | issue) and (.url | type == "string" and length > 0) | not))
    then null
    elif all($comments[]; .repo == $config.repository and .number == $config.issueNumber)
    then 1 else 0 end
  | [{id: "comment-target-match", value: .}]
' "$session_file"
```

Keep a nonempty ordered array of unique `{id,value}` metrics. The first value
supplies the canonical `operational-value` result; remaining metrics become
diagnostics, and the complete `metrics` array is retained. Values are finite
numbers or `null`; IDs and order remain stable across fixtures. Other custom
IDs accept only JavaScript `script`, not `run`.

Remove temporary-file writes, live API calls, checkout scans, and sourced
helpers. Read only the session and compute in memory, then print metrics.
Compiler validation and the default `grader-shell-file-write` rule parse Bash
ASTs and reject detected writes and unchecked dynamic execution, even in disabled
definitions. `gh aw lint --rules grader-shell-file-write WORKFLOW.md` requires no
optional JavaScript parser.

Bash starts without profiles or inherited credentials, in a temporary working
directory, with a 120-second deadline and 1 MiB output cap. It is **trusted
repository code**, not a filesystem/network sandbox. MicroVM isolation is
future work. Static lint is not proof of arbitrary shell safety. Never execute
session text as code or turn absent evidence into a successful score.

Verify shell fixtures through
`scripts/verify-operational-value-evaluator.sh EVALUATOR.sh FIXTURES.json`.
Fixtures use `{name, session, config, expected}`: `session` is a unified-event
array, `config` is data-only, and `expected` is the exact metric array. Include
attained, missed, unavailable, and malformed-evidence cases. The helper invokes
the same bounded Bash runtime as workflow execution and repeats each fixture.
Use malformed recorded evidence inside valid unified events, not an invalid
session envelope. Compile/lint separately to validate source paths and writes.

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

Translate the sentence into a function before writing either implementation:

```text
f(session, config, helpers) -> finite number | null | {value, ...}
```

Bash encodes the same predicate over session JSONL and returns a metric array;
the shell interface does not authorize additional evidence sources.

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

Use only evidence already recorded in the unified session: triggering facts and
the run subject, workflow observations, `safe_output.request` and
`safe_output.result` events, and captured repository or authoritative API
responses. Missing repository state or API evidence cannot be fetched during
grading. A producer must capture it before the conclusion session is finalized,
or the metric must return `null`.

Do not re-fetch data already captured with sufficient fidelity. For batch workflows, define the eligible set and denominator from one consistent recorded snapshot. Do not use historical expectations, another model's findings, the evaluator's own output, or the workflow's confidence as ground truth.

Whenever the metric judges a workflow decision, derive the expected decision independently from source evidence and compare it with the observed workflow request. Do not accept the workflow's explanation as proof that its decision was correct. Existing `evals` are evidence only for the exact predicate they evaluate; an eval that checks whether output exists does not prove that output is accurate.

When evidence sources conflict, apply an explicit precedence justified by the workflow or return `null`; never choose whichever source produces a better score. Validate captured observations for their expected completion marker, count, or schema before using them. If an expected batch snapshot is missing, stale, truncated, capped, or only partially parsed, return `null` rather than silently shrinking the denominator. Apply intentional eligibility filters before fixing the denominator, then count every eligible item whether processed or missed.

A declared sampling rule bounds the selected set; items outside that intentional sample are not misses. An execution safety cap does not shrink the eligible denominator: score the complete eligible set when evidence supports it, or return `null` when the cap prevents complete evaluation. Within the selected set, compare the complete expected action set with the complete observed request set. This is mandatory for destructive actions such as closing, deleting, relabeling, or superseding items: an unjustified extra mutation is a miss, not partial credit.

For time-based eligibility, use one declared UTC reference instant and define every boundary as inclusive or exclusive. Do not round to dates, use the evaluator's wall clock, or tolerate clock skew unless the workflow explicitly declares that behavior.

Treat thresholds, tolerances, and policy cutoffs as authoritative only when the workflow or a referenced policy declares them. Do not infer a regression threshold from noisy measurements, tune it against the current result, or invent a historical baseline. If a declared benchmark cannot be reproduced under its required environment and inputs, return `null`.

Treat retries and repeated schedules as independent runs unless deduplication or idempotence is part of the workflow's stated goal. When it is, independently verify that the repeat should act or noop from current evidence; do not add a generic cross-run identity system.

For experiment variants, apply the same acceptance function to the declared subject and variant. Grade the current run's outcome, not whether its variant beat another run, unless the workflow supplies a complete fixed comparison dataset and deterministic decision rule at the grading boundary.

If intended value depends on future events or human judgment unavailable during the run, do not invent a maturation window or silently substitute engagement. Preserve the downstream goal in the design, state the missing evidence, then measure the strongest independently checkable precursor available now and name it honestly. Return `null` when no meaningful deterministic per-run outcome can be observed.

Dependency failure is `null` when it prevents evidence collection for some other goal. It is `0` when the dependency or permission is itself the capability under test, such as an authentication smoke test.

### 4. Respect the grading boundary

The evaluator runs once over the current run's published conclusion session.
It does not wait for future acceptance, replay history, or revise an observation
later. Conclusion collects safe-output results when available, but a
`safe_output.request` is still not proof that a GitHub mutation was applied.

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

When research is used, add a short research note to the design table: source URL, definition considered, what was adopted or rejected, and why. Record a source in evaluator comments only when it materially affects the implemented formula. Runtime grading must remain deterministic from the in-memory session and data-only configuration; it must never browse the web for metric design.

When live external data is itself the workflow's declared subject, such as a model or service inventory, use the authoritative response captured in the session. Pin the endpoint and required fields in the producer's design, validate completeness, and return `null` on unavailable, truncated, or incompatible observations; do not substitute search results or design-time research.

### 6. Choose the smallest useful metric set

Choose one primary metric that answers the intent sentence directly. Use a precise domain name such as `eligible-issues-triaged`, `security-review-policy-conformance`, or `release-request-valid`, not `operational-value` or `success`. Do not name a metric after a stronger claim than its evidence proves.

Add a diagnostic only when it explains a distinct failure mode and can change an operational decision. Do not combine unrelated outcomes into a weighted score merely to produce one number. If the workflow has independent goals, select the one declared as primary or report the ambiguity.

Use the simplest defensible formula:

- a raw count, amount, duration, or other domain quantity in its native unit;
- a fraction or proportion with an explicit numerator and denominator for sets;
- continuous progress in the workflow's declared unit;
- binary `0` or `1` only when the valuable outcome is genuinely atomic and has no meaningful magnitude;
- `null` when applicability or evidence cannot be established.

For proportions, define every numerator and denominator term and prevent missing items from disappearing from the denominator. A quality metric requires independent acceptance criteria or ground truth; the workflow cannot grade its own judgment by counting its findings.

Preserve the metric's native numeric scale. Do not normalize, clamp, rescale, or reduce an operational quantity to pass/fail merely to fit `[0,1]`. Values may be fractional, negative, or greater than one when the declared unit and formula give those values meaning. A ratio is appropriate only when the metric is inherently a ratio. Declare `unit` and `direction` so consumers can interpret and compare the raw value without transforming the stored observation.

Translate qualitative words such as “actionable,” “correct,” “relevant,” “complete,” and “high quality” into deterministic predicates grounded in the workflow Markdown. For example, an actionable incident report might require the triggering environment, a failing step, linked evidence, and a concrete remediation. If semantic correctness cannot be determined without another model or later human judgment, narrow the metric to the strongest deterministic claim available, such as `required-incident-analysis-present`, and state that limitation in the design table.

For creative or aesthetic goals with no objective acceptance criteria, do not manufacture operational value from length, output existence, or model ratings. Measure only explicit structural or target-binding requirements under a narrowly named metric, or report that no meaningful deterministic grader can be designed.

Validation supports only the property it checks. A passing formatter proves formatting, a focused test proves the tested behavior, and a successful build proves buildability; none alone proves semantic improvement or absence of regressions. Name the metric after the verified property and include every workflow-required check in the expected decision.

Set `direction` to `higher_is_better` or `lower_is_better` according to the metric's native meaning. Keep a metric ID stable while it continues to describe the same outcome and unit.

Freeze the metric prospectively to prevent hindsight bias:

- choose the formula, thresholds, evidence rules, and fixtures before inspecting any scored outcomes;
- commit the workflow intent, inline `script` or referenced Bash evaluator, data configuration, and semantic fixtures together; that commit is the adoption point for the contract;
- treat the evaluator as read-only while the workflow's intent and acceptance criteria are unchanged;
- when those workflow semantics change, update the workflow and its grader together at the same workflow path and commit; the new contract applies only to future runs;
- allow an evaluator-only defect correction only prospectively, without changing or regrading prior results;
- retain the metric ID when the measured outcome still means the same thing; choose a new descriptive ID only when the outcome itself changes;
- score each run with the evaluator bytes and configuration frozen into that run. Never move adoption backward or tune a function against observed scores.

The workflow commit and custom implementation's SHA-256 source digest preserve
the adopted contract. Keep the script at the same grader ID while its meaning
is stable. Result artifacts omit executable source. Do not add versioned
filenames, registries, or a second provenance service.

### 7. Implement the evaluator

For JavaScript, write a synchronous function body, not a module, CLI program, or
`module.exports`. `session` is an array of unified events with `type`, `data`,
and `provenance`; it is not a filename or the legacy trace/request object.

Use `const lookups = helpers.createSessionLookups(session)`:

| Helper | Meaning |
|---|---|
| `lookups.stats` | Concrete session accounting, including tool calls, usage, retries, and artifact counts |
| `lookups.eventsOfType(type)` | Recorded events of one unified-session type |
| `lookups.toolCalls` | Correlated calls with native/gateway duplicate handling |
| `lookups.toolCallsById` | Read-only scoped tool-call index |
| `lookups.trace` | Session-derived trajectory projection for analyses that need it |

These are immutable snapshots of the supplied session, not a facility to load
other logs or reconstruct evidence absent from the input.

Return the metric value directly or `{ value, message, details, diagnostics }`.
The runtime adds the configured ID, status, and implementation identity.
Promises, strings, booleans, non-finite values, and metric arrays are rejected.
Use `null` for absent or malformed required evidence; include an optional
message explaining the specific missing observation. Do not catch real
implementation errors and convert them into success or unavailable results.

The grader must return the same result for the same inputs, preserve the
metric's native numeric scale, and inspect recorded validation results instead
of repeating builds, scans, services, or browsers. File, module, network,
process, clock, random, and timer APIs are unavailable. Do not add permissions,
secrets, or custom conclusion steps to work around this boundary.

### 8. Verify and review

Compile the workflow, then replay the selected grader with a unified-session
JSONL file or JSON event array. For example:

```bash
gh aw compile .github/workflows/WORKFLOW-NAME.md
cat aw_session.jsonl | gh aw graders run WORKFLOW-NAME comment-target-match
git diff BASE-REF -- .github/workflows/WORKFLOW-NAME.md
```

Exercise exact semantic fixtures through `runCustomGraders` in
`actions/setup/js/custom_graders.cjs`, including repeated identical inputs,
missing evidence, and malformed observations. For this skill's example and
guidance regressions, run `bash .github/skills/operational-value-designer/tests/test.sh`.

Inspect the generated DAG: `graders` must depend on successful conclusion and
read the published `usage/aw_session.jsonl`, while conclusion receives only
custom registration IDs. Built-ins remain in `usage/graders/`; the separate
`graders` artifact contains combined built-in/custom results and data-only
metadata, not executable source. Audit optionally downloads registered custom
results. `gh aw logs --graders` or `--artifacts graders` includes both artifacts.
Custom results do not rewrite conclusion's session.

For Bash, replay with `gh aw graders run WORKFLOW-NAME operational-value`.
`verify-operational-value-evaluator.sh` validates session-file fixtures through
the actual shell runtime. `verify-operational-value-contract-change.sh BASE-REF`
checks evaluator/workflow pairing; use `--correction` only for an explicit
prospective defect fix. These shell helpers do not validate inline JavaScript.

Review the design against these checks:

- The intent sentence describes an outcome, not activity.
- The outcome ladder starts from the ultimate goal rather than the configured output type.
- The primary metric uses the furthest downstream effect that is currently observable, attributable, and independently verifiable.
- Any fallback to a precursor states exactly why a stronger downstream effect cannot be measured at the grading boundary.
- Missing evidence for the selected rung returns `null`; it never causes runtime fallback to a weaker rung.
- The unit of evaluation is explicit and matches the workflow's actual subject.
- The primary value remains in its native unit and scale rather than being normalized or collapsed to pass/fail.
- The declared direction matches whether larger or smaller native values are better.
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
- The evaluator uses only supplied session evidence and configuration; JavaScript performs no I/O, and Bash reads the session file without writes or live evidence fetches.
- JavaScript returns a finite number, `null`, or an object containing `value`; Bash returns the stable legacy metric array.
- Exact fixtures prove attained scores above missed, unavailable and malformed evidence return the declared result, and repeated evaluation is deterministic.