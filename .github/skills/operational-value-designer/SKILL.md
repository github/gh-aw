---
name: operational-value-designer
description: "Design and verify a deterministic operational-value grader for any GitHub Agentic Workflow. Use when choosing outcome metrics or creating an operational-value evaluator. Usage: /operational-value-designer OWNER/REPO WORKFLOW-NAME."
argument-hint: "OWNER/REPO WORKFLOW-NAME"
allowed-tools: bash jq gh
metadata:
  version: "2.0.0"
---

# Operational Value Grader

Create one small deterministic evaluator that measures whether a workflow produced its intended operational outcome. Put domain knowledge in the evaluator: gh-aw only supplies run context, executes it, and validates its metrics.

Operational value measures repository outcomes, not agent activity, output volume, or subjective quality. Do not count tool calls, tokens, comments, commits, or pull requests unless that activity is itself the workflow's intended outcome.

## Design

1. Resolve `OWNER/REPO` and `.github/workflows/WORKFLOW-NAME.md` from the arguments, then derive the intended repository outcome from the effective workflow. Use top-level `intent:` as canonical when present; otherwise read the Markdown body and its prompt imports for the mission, required effects, success conditions, and required `noop` cases. Use `description`, `evals`, deterministic `steps`/`pre-agent-steps`/`post-steps`, custom jobs, and `safe-outputs` as corroborating evidence for precomputed inputs and observable effects. Do not infer intent from triggers, tools, permissions, or an allowed output type alone; those describe execution mechanics and constraints.
2. If the outcome has no obvious direct metric, optionally do bounded web research for how authoritative sources and comparable systems measure the same outcome. Use it to discover candidate definitions, denominators, and failure cases, not to replace the workflow's intent or import an industry benchmark blindly. Reject measures that are not attributable to one run, cannot be observed from available evidence, or reward activity instead of the intended result. Record links or reasoning in the implementation or PR when external research materially influences the metric.
3. Choose one direct, domain-named primary metric grounded in the workflow's intent and available evidence. Higher values must mean more of the intended outcome, normalized to `[0,1]`.
4. Add diagnostic metrics only when they explain the primary result. Keep every metric independently useful and domain-named.
5. Decide what repository evidence each metric needs. Prefer event data and the local checkout. Use GitHub APIs only when the outcome cannot be determined locally, and request only the workflow permissions needed for those calls.
6. Define when evidence is unavailable. Return `null`; do not turn missing data into zero.
7. Implement and verify the evaluator.

Web research is a design-time aid, never evaluator input. Do not make runtime web requests to obtain generic benchmarks or definitions; runtime network calls are only for evidence about the specific repository outcome being graded.

Avoid baselines, maturity windows, opportunity keys, historical replay, and aggregation unless the workflow itself explicitly needs them. They are not part of the gh-aw evaluator protocol.

## Files

Create one executable Bash evaluator:

```text
.github/graders/WORKFLOW-NAME-operational-value.sh
```

Configure it in the workflow:

```yaml
graders:
  operational-value:
    run: .github/graders/WORKFLOW-NAME-operational-value.sh
```

The evaluator must be compatible with Bash 3.2. It may use `jq`, the local checkout, and `GH_TOKEN` for APIs allowed by the workflow's declared permissions.

## Input

gh-aw invokes the evaluator once with no arguments and writes this JSON to stdin:

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
    "eventName": "schedule"
  },
  "event": {},
  "config": {}
}
```

`event` is the triggering event payload when available. `config` is the grader's frontmatter configuration.

## Output

Write one non-empty ordered JSON array to stdout:

```json
[
  {"id": "issue-resolution", "value": 0.75},
  {"id": "repository-health", "value": 0.9}
]
```

Rules:

- Each item contains `id` and `value`.
- `id` is a stable, non-empty, unique domain metric name.
- `value` is a finite number in `[0,1]` or `null` when evidence is unavailable.
- The first item is the primary operational-value metric.
- Later items are optional diagnostics and never alter the primary value.
- Write diagnostics and progress to stderr, never stdout.

gh-aw preserves the array in `grader_results.json` as `metrics` and exposes the first metric's value through the grader's top-level `value` for thresholds and experiments.

## Verify

Run:

```bash
.github/skills/operational-value-designer/scripts/verify-operational-value-evaluator.sh \
  .github/graders/WORKFLOW-NAME-operational-value.sh
gh aw compile .github/workflows/WORKFLOW-NAME.md
```

Before finishing, inspect the compiled grader step and confirm the workflow grants only permissions the evaluator actually uses.