---
description: Output, scope, cross-repository and completion checks for generated workflows.
disable-model-invocation: true
---

# Generated Workflow Validation

Use before finalizing a [new workflow](create-agentic-workflow.md). Apply the
[design checklist](create-agentic-workflow-design.md), inspect the generated
source and compile it. Do not claim runtime success from compilation.

## Prompt requirements

State canonical intent, triggering context, activation/evidence requirements,
required effects, allowed safe outputs and inverse/no-op conditions. Require
`noop` with a brief reason for duplicates, insufficient evidence or no action.
Keep instructions short, specific and imperative.

For `evals:`, derive separate positive and adversarial fixtures from required
effects and inverse conditions using [intent.md](intent.md). Each BinEval run
receives one fixture: ask fixture-specific questions or return `UNKNOWN` when
a shared question's scenario is absent; never mix mutually exclusive assertions.

For reports follow [structure](report.md#report-style-and-structure) and
[run references](report.md#workflow-run-references).
For instruction/skill generation follow the
[7,500-byte execution contract](../skills/prompt-token-efficiency/SKILL.md).

## Quality and scoping checks

- Trigger matches intent and event granularity; maintenance choices follow a
  bounded repository survey, separating observed signals from recommendations.
- Enable only needed read/analysis tools; prefer `gh-proxy`. Add `playwright`
  or `cache-memory` only for a demonstrated need.
- Agent-job permissions remain read-only. PR reporting needs `contents: read`
  and `pull-requests: read`, not write scopes.
- Route visible writes through configured safe outputs. Use `add-comment` for
  PR-local findings, `create-issue` for cross-cutting incidents, `noop` for
  no action. Never use raw writes or `post-steps:` to bypass safe outputs.
- Recurring monitors define a stable scope/finding signature and check existing
  open issues before creation. Use close-older options only when newer results
  supersede older ones; otherwise choose wait/keep deliberately.
- Infer network ecosystems from lock/manifests; never use defaults alone for
  install/build/test.
- Scope paths to relevant files: backend contracts, design tokens/theme, or
  policy/control docs. Add `paths:`/`paths-ignore:` when unrelated files must
  not activate the workflow.
- State required labels or `label_command` names for label-based routing.
- For `workflow_run`, name `workflows:` and gate `${{ github.event.workflow_run.conclusion }}`;
  prefer failure-only conditions for incident triage.
- For reporting/triage, state the exact evidence window: last 24h, since prior
  run, current week, or another explicit interval.
- Name each safe output's outcome and when `noop` replaces it.
- Require concise, context-aware outputs and explicit stop/failure behavior.

## Multi-repository requests

First determine whether the question is finite and bounded.
For arbitrary code extraction, full file contents or other unbounded access:
enable necessary external-repository read toolsets, configure cross-repository
safe-output authentication and instruct the agent to set `target-repo`.
This does not enable waiting for external workflows or multi-job orchestration.
Use the [cross-repo pattern](workflow-patterns.md).

## Minimal source shape

```markdown
---
description: <brief description>
intent: <outcome, not implementation>
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: read
tools:
  github:
    mode: gh-proxy
    toolsets: [default]
safe-outputs:
  add-comment:
---

# <Workflow Name>

<Intent, context, task and evidence requirements>
Use configured safe outputs; call noop with a reason when no action is required.
```

Add non-GitHub `cli-proxy: true` only when needed.

## Final steps

1. Check the unique workflow ID; create exactly one primary Markdown file.
2. Run `gh aw compile <workflow-id>`; fix every compile error.
3. Include source and generated `.lock.yml` in the PR.
4. Report the change and unavailable checks concisely. Compilation neither
   executes a workflow nor authorizes dispatch.
