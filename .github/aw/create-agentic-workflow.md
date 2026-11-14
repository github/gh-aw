---
description: Design and compile gh-aw workflows with explicit intent, scope and safety.
disable-model-invocation: true
---

# GitHub Agentic Workflow Designer & Creator

Create one `.github/workflows/<workflow-id>.md` using `gh aw`. For evaluation,
recommend only; edit files only on an explicit implementation request.

## Load These References First

Read [intent](intent.md), [workflow rules](github-agentic-workflows.md),
[editing/compilation](workflow-editing.md), [constraints](workflow-constraints.md),
[patterns](workflow-patterns.md), [safe outputs](safe-outputs.md),
[schema index](syntax.md), [MCP CLIs](mcp-clis.md), and the optional repository
overlay `.github/aw/instructions.md`.
Before generation, follow the [design checklist](create-agentic-workflow-design.md)
and [output/validation contract](create-agentic-workflow-validation.md). Load matching
topic guides only: maintenance/docs ([maintainer](maintainer.md),
[documentation](documentation-workflows.md)); campaign/experiments
([campaign](campaign.md), [experiments](experiments.md)); screenshots/deployments
([visual regression](visual-regression.md), [deployment status](deployment-status.md));
charts/reports ([charts](charts.md), [report](report.md)); release/linter
([release](release-workflow.md), [linter](linter-workflows.md)); Docker/ARC/
self-hosted ([runtime](agent-runtime-instructions.md)); skills/plugins or graders
([skills](skills.md), [grader skill](../skills/operational-value-designer/SKILL.md));
instruction/skill generators ([packing contract](../skills/prompt-token-efficiency/SKILL.md)).
Persistent knowledge reuse applies even without an explicit memory or dreaming request:
[memory decision](memory.md#scheduled-dreaming); load
[dreaming.md](dreaming.md) only after the [memory decision and input-scope checks](memory.md#scheduled-dreaming)
justify a separate dreamer.

## Modes

### Interactive mode

If no automation goal is stated, start with exactly:

> What do you want to automate today?

With a goal, reuse supplied facts and skip the opener. Ask only for missing
decisions; follow the [interview](designer.md#interview-framework) and generate
after confirmation.

For scheduled issue/PR creation, choose the [previous-result strategy](create-agentic-workflow-design.md#choose-the-previous-result-strategy).
For maintenance, include `Repository signals` and `Initial maintenance portfolio`
in [confirmation](designer.md#confirmation-format). Use [designer mappings](designer-mappings.md)
for trigger/tool/auth choices.

### Issue-form mode

Read the supplied name, description and context; derive and persist `intent:`
before architecture. Choose a unique kebab-case ID; never overwrite. Create one
primary Markdown file, compile it, and include its generated `.lock.yml` in the PR.

## Mandatory design and safety rules

1. Derive architecture from intent; define activation, effects, inverse/`noop`,
   evidence window and scope. Verify config files named in the request; if missing,
   require `noop` and report, never guess. Put choices (e.g., what “adds” covers,
   lockfile scope, zero-item behavior) in the proposed prompt, not only notes.
   Apply the [memory reuse checkpoint](designer.md#memory-reuse-checkpoint) before
   tool/persistence selection.
2. Keep the agent job read-only: never grant `issues: write`, `pull-requests: write`
   or `contents: write`. Recommend `permissions: { copilot-requests: write }` for
   Copilot inference; see [scope/caveats](syntax-core.md).
3. Use safe outputs for mutations, never raw `gh`, API/write tools or `post-steps:`.
   Require `update-release` for release notes; restrict
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
6. Prefer `pull_request`; use `pull_request_target` only for base-repository
   context. Default `checkout: false`; never check out or execute PR-head code.
   Default to team-only; recommend `roles: all` for community entrypoints.
7. Reuse built-in graders. Custom JavaScript and repo-owned Bash run only in the
   compiler's isolated downstream `graders` job, never conclusion. Follow the
   grader evidence/I/O contract; Bash is trusted, not microVM-isolated. Do not
   fetch evidence or write files while grading.
8. Use deterministic triage before frontier reasoning; bound sub-agent fan-out
   and load context on demand. Do not invent multi-job orchestration or external
   workflow waits; explain the limit; recommend traditional Actions.

## Ad Hoc Evaluation Mode

Use for exploration, persona walkthroughs, and scenario recommendations.

- Do not create/edit workflow files unless implementation is explicitly requested.
- Use only relevant guidance sections: start with the
  [trigger decision matrix](triggers.md#decision-matrix); for review/comment-only
  work use the [least-privilege checklist](workflow-patterns.md#least-privilege-for-review-only-and-comment-only-workflows).
- When requirements conflict with a trigger, recommend the trigger that satisfies the requirement first;
  explain the conflicting alternative's consequence.
- For each material undefined term, propose and clearly label a reasonable `Default:`.
  Ask 1-2 questions only if answers change the recommendation; otherwise state assumptions.
- Resolve each responsible owner from CODEOWNERS or maintainer metadata. If unresolved,
  say so and identify who must confirm. For later edits, state whether to update the
  same target or noop; update changed content and noop only when no change is needed.
  If required input is missing, call `report_incomplete`; never silently noop.
- For non-technical personas, lead with one plain-language line naming the
  operator and output reader, before pre-step details.
- Return trigger, scoped paths/time window, read tools, safe outputs, read permissions,
  network access, and `noop` criteria. For recurring reports, state the window,
  grouping, and stable dedup key.
- Use one comparison table for multiple scenarios; name shared triggers/write paths.
- Offer file generation only as the next explicit implementation step.

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
