---
description: Design, evaluate, create and compile gh-aw workflows with explicit intent, scope and safety.
disable-model-invocation: true
---

# GitHub Agentic Workflow Designer & Creator

Create one `.github/workflows/<workflow-id>.md` using `gh aw`. For evaluation,
return recommendations only; do not create/edit files without an explicit
implementation request.

## Load These References First

Read [intent](intent.md), [workflow rules](github-agentic-workflows.md),
[editing/compilation](workflow-editing.md), [constraints](workflow-constraints.md),
[patterns](workflow-patterns.md), [safe outputs](safe-outputs.md),
[schema index](syntax.md) and [MCP CLIs](mcp-clis.md).
Load the target repository's optional `.github/aw/instructions.md` overlay.

Before generation, follow the [design checklist](create-agentic-workflow-design.md)
and [output/validation contract](create-agentic-workflow-validation.md).
Load only matching topic references:

- Maintenance: [maintainer.md](maintainer.md); docs agents/checks: [documentation-workflows.md](documentation-workflows.md).
- Campaign/KPI: [campaign.md](campaign.md); A/B tests: [experiments.md](experiments.md).
- Screenshots: [visual-regression.md](visual-regression.md); deployments: [deployment-status.md](deployment-status.md).
- Charts: [charts.md](charts.md); reports/digests: [report.md](report.md).
- Release notes: [release-workflow.md](release-workflow.md); linter mining: [linter-workflows.md](linter-workflows.md).
- Docker/Cloud Hypervisor/ARC/self-hosted: [agent-runtime-instructions.md](agent-runtime-instructions.md).
- Skills/plugins: [skills.md](skills.md); graders: [operational-value-designer](../skills/operational-value-designer/SKILL.md).
- Instruction/skill generators: [packing contract](../skills/prompt-token-efficiency/SKILL.md).
- Persistent knowledge reuse, even without an explicit memory or dreaming request:
  [memory decision](memory.md#scheduled-dreaming); load [dreaming.md](dreaming.md)
  only after the [memory decision and input-scope checks](memory.md#scheduled-dreaming)
  justify a separate dreamer.

## Modes

### Interactive mode

If no automation goal is stated, start with exactly:

> What do you want to automate today?

When a goal is stated, reuse supplied facts; skip the opener. Ask only for
missing design decisions; keep it short, iterative and without option dumps.
Follow the [interview](designer.md#interview-framework) for intent, repository survey,
architecture/trigger, scope, data, guardrails, context/network, engine and
confirmation; generate after confirmation.

During trigger selection, choose the [previous-result strategy](create-agentic-workflow-design.md#choose-the-previous-result-strategy)
for scheduled issue/PR creation. For maintenance, add `Repository signals` and
`Initial maintenance portfolio` to the [confirmation](designer.md#confirmation-format).
Use [designer-mappings.md](designer-mappings.md) for detailed trigger/tool/auth choices.

### Issue-form mode

Read the supplied name, description and context; generate without further conversation.
Derive and persist `intent:` before architecture. Choose a unique kebab-case ID;
never overwrite an existing workflow. Create exactly one primary Markdown file,
compile it and include its generated `.lock.yml` in the PR.

## Mandatory design and safety rules

1. Derive architecture from intent; define activation, effects, inverse/`noop`,
   evidence window and scope. Verify config files named in the request; if
   missing, require `noop` and report, never guess. Put each chosen definition
   (e.g., what “adds” covers, lockfile scope, zero-item behavior) in the proposed
   prompt, not only notes. Apply the [memory reuse checkpoint](designer.md#memory-reuse-checkpoint)
   before tool/persistence selection.
2. Keep the agent job read-only: never grant `issues: write`, `pull-requests: write`
   or `contents: write`. For Copilot inference recommend
   `permissions: { copilot-requests: write }`; see [scope/caveats](syntax-core.md).
3. Use configured safe outputs for mutations, never raw `gh`, API, GitHub write
   tools or `post-steps:`. Require `update-release` for release notes; restrict
   `create-pull-request.allowed-files`; require `noop` for duplicates,
   insufficient evidence or no action.
4. Prefer `gh-proxy` and `toolsets:`; use `cli-proxy: true` only for needed
   non-GitHub MCP CLIs. Codex cannot enforce nonempty per-command `tools.bash`
   allowlists; disable bash or choose a compatible engine. Declare `skills:`/
   `plugins:` at top level; never install them via generated steps, shell or
   prompt instructions.
5. Infer network ecosystems from manifests; never use `network: defaults` alone
   for install/build/test. For policy/compliance audits without external probes,
   enforce `network.allowed: []` structurally; omit external tools.
6. Prefer `pull_request`; use `pull_request_target` only when base-repository
   context is needed. Default `checkout: false`; never check out or execute
   PR-head code. Default triggering is team-only; recommend `roles: all` for
   community entrypoints.
7. Reuse built-in graders. Custom JavaScript and repo-owned Bash run only in the
   compiler's isolated downstream `graders` job, never conclusion. Follow the
   grader evidence/I/O contract; Bash is trusted, not microVM-isolated. Do not
   fetch evidence or write files while grading.
8. Use deterministic triage before frontier reasoning; bound sub-agent fan-out
   and load context on demand. Do not invent multi-job orchestration or external
   workflow waits; explain the limit; recommend traditional Actions.

## Ad Hoc Evaluation Mode

Use for exploration, persona walkthroughs and scenario recommendations.

- Do not create or edit workflow files. Exit only on an explicit implementation request.
- Use only relevant guidance sections: start with the
  [trigger decision matrix](triggers.md#decision-matrix); for review/comment-only
  work use the [least-privilege checklist](workflow-patterns.md#least-privilege-for-review-only-and-comment-only-workflows).
- When requirements conflict with a trigger, recommend the trigger that satisfies the requirement first;
  explain the conflicting alternative's consequence.
- For each material undefined term, propose and clearly label a reasonable `Default:`.
  Ask 1-2 targeted questions only if answers change the recommendation; for
  immediate recommendations, state assumptions and alternatives.
- Return trigger, scoped paths/time window, read tools, safe outputs, read
  permissions, network access and `noop` criteria. For recurring reports, state
  window, grouping and a stable dedup key.
- Use one comparison table for multiple scenarios; name shared triggers/write
  paths. Offer file generation only as the next explicit implementation step.

### Invocation Surface

Address the `agentic-workflows` custom agent in conversation. CLI/MCP commands
manage workflow files; none accepts freeform `prompt`, `scenario` or `query`
parameters. For examples and failure recovery, read
[evaluation examples](create-agentic-workflow-evaluation.md).

## Final Steps

Apply the linked design and validation checklists. Create the source, run
`gh aw compile <workflow-id>`, fix every compile error, and create a PR containing
source and generated lock. Report the change and unavailable checks concisely.
Compilation does not execute a workflow or authorize dispatch.
