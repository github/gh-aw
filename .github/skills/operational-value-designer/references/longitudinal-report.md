# Longitudinal value functions

Author the workflow-specific function consumed by `gh aw value`.
Do not redesign delivery/adoption, infer human approval, or add a second report
pipeline. The CLI reports native lifecycle evidence for any workflow even when
no measurement exists. Missing value is unavailable, not zero.

Read the exact workflow intent, eligible subjects, constraints, and output
semantics before choosing a metric. Inspect representative repository snapshots
and native lifecycle records. If durable value is not observable in repository
history, stop with the missing evidence and leave value unavailable; do not
reward activity, output counts, agent claims, or fabricate an evaluator.

## Define the contract

Write a compact design table in the response: ultimate goal, eligible population,
observable outcome, native unit, direction, required snapshot paths, zero/null
rules, confounders, and adoption boundary. Choose one primary metric.

The shared pipeline owns producer linkage, native delivery/adoption, weekly
snapshots, daily seven-day windows, exact-parent merge comparison, isolation
from neighboring workflow merges, and full-window file survival. The current
attribution adapter supports PR merges only. Other output types still expose
native lifecycle evidence; do not invent a commit or attribute their value.

Only the value function varies by workflow. Create
`.github/aw/value/WORKFLOW.json`:

```json
{
  "id": "documented-apis-v1",
  "name": "Documented public APIs",
  "unit": "APIs",
  "direction": "higher_is_better",
  "paths": ["metrics/api-coverage.json"],
  "script": "const text = snapshot.files['metrics/api-coverage.json']; if (text === undefined) return null; const data = JSON.parse(text); return Number.isFinite(data.covered) ? data.covered : null;"
}
```

This example applies only when the metric file independently measures the
workflow's actual goal. Do not copy its metric without inspecting the workflow.

The synchronous script receives immutable `snapshot.sha`, `snapshot.files`
(repository-relative names to exact UTF-8 contents), and optional `parameters`
from the definition. Paths select literal tracked files or directories, not
globs. Return one finite native-scale number or `null`. Use `0` only for observed
non-attainment; missing, inapplicable, malformed, conflicting, or incomplete
evidence must not become zero. Do not return an array or a grader result object.
No file/module/process/network/environment/credential/clock/random/timer access
or input mutation is available. Do not execute workflow code or shell commands.

Fix the eligible population in `parameters` when denominator churn could mimic
improvement. Do not shrink it to observed successes. Bound inputs to 32 paths,
512 regular files, and 8 MiB per snapshot; script limit is 256 KiB and definition
limit is 512 KiB. Symlinks/submodules are unsupported. Narrow the measurement or
report unavailable evidence rather than relaxing these limits.

For higher-is-better metrics, merge improvement is `after - before`; for
lower-is-better metrics it is `before - after`. Signed regressions remain
negative; never normalize or clamp. The shared gates must pass before any
improvement is attributed. This is event-aligned attribution, not proof of
exclusive causation, correctness, or saved time.

## Validate and adopt

Define attained, missed, unavailable, malformed, changed-population, and
inapplicable fixtures using representative exact historical inputs. Run each
twice through the same restricted runtime and check identical native values,
`null`, or declared errors. Check both improvement directions and
full-window/no-adoption gates.
For local runtime work, use the focused `TestOperationalValue` CLI tests.

Run `gh aw value WORKFLOW --value-function PATH --start DATE --end DATE --json`
against completed history and native records. Verify the pinned commit, exact
file bytes, function definition digest, units, daily samples, and unavailable
reasons. Never dispatch a workflow to validate the function.

Present the definition and fixtures for review before adopting it. Freeze intent,
inputs, parameters, metric semantics, and fixtures together; the report records
the exact definition SHA-256. Corrections are prospective. Label historical
exploration as retrospective and never rewrite old reports, sessions, or grader
results. Keep `graders.operational-value.script` separate: it measures recorded
run evidence and cannot substitute for this longitudinal function.
