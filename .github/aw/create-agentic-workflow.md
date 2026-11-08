---
description: Design and create new agentic workflows using GitHub Agentic Workflows (gh-aw) — unified interview-first experience with concise guidance on triggers, tools, and security.
disable-model-invocation: true
---

# GitHub Agentic Workflow Designer & Creator

Design and create new workflow files under `.github/workflows/` using the installed `gh aw` CLI.

## Load These References First

- [designer.md](designer.md)
- [intent.md](intent.md) for the outcome definition, PromptPex eval derivation, and operational-value inference
- [github-agentic-workflows.md](github-agentic-workflows.md)
- [workflow-editing.md](workflow-editing.md)
- [workflow-constraints.md](workflow-constraints.md)
- [workflow-patterns.md](workflow-patterns.md)
- [safe-outputs.md](safe-outputs.md)
- [syntax.md](syntax.md)
- [mcp-clis.md](mcp-clis.md)

Load these topic files only when relevant:

- [memory.md](memory.md#scheduled-dreaming) when later runs need knowledge reuse or reconciliation — even without an explicit memory or dreaming request; owns the deterministic-first decision, cost gate, and input scope
- [dreaming.md](dreaming.md) only after the [memory decision and input-scope checks](memory.md#scheduled-dreaming) justify a separate dreamer; configuration, publication, and consumer-recall implementation
- [maintainer.md](maintainer.md) for recurring repository maintenance, backlog triage, owned-PR upkeep, or long-term code health
- [documentation-workflows.md](documentation-workflows.md) for documentation agents, required docs checks, and post-merge page/index upkeep
- [campaign.md](campaign.md) for campaign, KPI, pacing, cadence, or `stop-after`
- [experiments.md](experiments.md) for experiments, A/B tests, variants, or prompt comparisons
- [visual-regression.md](visual-regression.md) for screenshot comparison workflows
- [deployment-status.md](deployment-status.md) for external deployment monitoring
- [charts.md](charts.md) for chart-generation workflows
- [report.md](report.md) for reporting output structure and recurring report lifecycle
- [release-workflow.md](release-workflow.md) whenever a workflow creates or updates release notes, including workflows that also build, test, or publish a GitHub release
- [linter-workflows.md](linter-workflows.md) for mining, refining, or applying custom linter rules
- [agent-runtime-instructions.md](agent-runtime-instructions.md) when choosing or debugging Docker, Cloud Hypervisor, ARC DinD, or self-hosted runners
- [skills.md](skills.md) when the user asks for specific skills or agent plugins
- [operational-value-designer](../skills/operational-value-designer/SKILL.md) when deriving or configuring graders over the in-memory unified session

## Graders

Ten fast built-ins are enabled by default; configure thresholds or disable
irrelevant defaults instead of recreating their implementations. Custom metrics
use `graders.<distinct-id>.script`: synchronous JavaScript over `session`,
`config`, and `helpers.createSessionLookups(session)`, returning a finite number,
`null`, or an object with `value`. The compiler generates an isolated downstream
`graders` job. Repository-owned Bash may instead use `graders.operational-value.run`,
reading unified-session JSONL from `$1` / `GH_AW_SESSION_FILE` and config from
`GH_AW_GRADER_CONFIG`, preserving metric-array output. Detected file writes are
rejected; shell remains trusted code until future microVM isolation. Never add
user code or steps in conclusion. See the linked skill for
the full syntax, evidence rules, and fixture checks.

## Skills and Plugins

When the user requests specific skills or agent plugins, declare them in the built-in top-level `skills:` and `plugins:` frontmatter fields — gh-aw installs them before the agent runs. Never generate on-the-fly installation (`steps:` running `gh skill install`, `copilot plugin install`, `npx`, `curl`, or `git clone`) and never instruct the agent to install a skill or plugin from the prompt body. See [skills.md](skills.md).

## Release Notes

Any agent-generated release notes or release-description changes must use the `update-release` safe output. Keep the agent job read-only; never grant it write permissions or direct it to mutate a release with `gh`, the GitHub API, or a GitHub write tool.

## Modes

### Interactive mode

When the user has not already stated an automation goal, start with exactly:

> What do you want to automate today?

When the request already states a goal, skip the opening question and continue with only the design decisions it leaves unanswered.

Run the structured interview in [designer.md#interview-framework](designer.md#interview-framework) — it covers goal/intent, repository survey, architecture/trigger, scope, data strategy, guardrails, context/network, engine, and confirmation, plus the progressive-disclosure rules and the confirmation summary format. This file covers generation, compile, and PR mechanics once the design is confirmed.

Two additions used only here:

- For scheduled workflows that create issues or pull requests, choose how previous results are handled using [Choose the previous-result strategy](#choose-the-previous-result-strategy) during the architecture/trigger phase.
- For maintenance workflows, add `Repository signals` and `Initial maintenance portfolio` fields to the confirmation summary, in addition to the fields in [designer.md#confirmation-format](designer.md#confirmation-format).

For detailed trigger/safe-output/network/tool decision heuristics and integration auth setup patterns, load [designer-mappings.md](designer-mappings.md).

### Issue-form mode

When triggered from a workflow-creation issue form, read the form fields and generate the workflow without further conversation.

## Conversation Rules

- Keep the conversation short and iterative.
- When the request already states a goal, proceed with the relevant mode and ask only about missing details that would change the workflow design.
- Translate user intent into workflow structure.
- When the user asks for exploration, evaluation, or scenario design rather than file creation, stay in ad hoc evaluation mode.
- In ad hoc evaluation mode, do not create `.github/workflows/*.md`.
- Do not overwhelm the user with long option dumps unless they ask.
- If the request exceeds the single-job model, explain the constraint and recommend traditional GitHub Actions.

## Ad Hoc Evaluation Mode

Use this mode for exploratory testing, persona walkthroughs, and "what workflow would you create for this scenario?" requests.

- Do not create or edit workflow files.
- Return a compact recommendation covering trigger, any scoped `paths:` filters for file-event triggers, read tools, safe outputs, permissions, and explicit `noop` criteria.
- Before finalizing trigger or safe-output recommendations, check whether terms that affect scope or matching are ambiguous (for example, whether "completed" means closed issues or merged PRs, or where a coverage threshold comes from). Ask 1-2 targeted clarifying questions when the answer would change the recommendation. If the request needs an immediate recommendation, state the assumption and the relevant alternative rather than silently choosing.
- For recurring reports or digests, always include the report window, grouping dimensions, and deduplication key. See [triggers.md](triggers.md) for key-format examples.
- Exit ad hoc evaluation mode only when the user explicitly asks to create, implement, or write the workflow file.
- End by offering to turn the recommendation into `.github/workflows/<workflow-id>.md` if the user wants to proceed.

### Invocation Surface

Ad hoc evaluation is reached by addressing the `agentic-workflows` custom agent directly in conversation (chat prompt, issue comment, or PR comment) — it is **not** a CLI flag or MCP tool parameter. The `gh aw` CLI and MCP tools (`compile`, `audit`, `status`, `update`, etc.) only manage existing workflow files and do not accept a `prompt`/`scenario`/`query` parameter; passing one will fail with an "Unknown parameter" error. Use the example prompt below instead of trying to script evaluation through a tool call.

### Single-Scenario Evaluation Example

> agentic-workflows evaluate this scenario without creating files: Information Worker — weekly summary of stale documentation files not updated in the last 90 days

Return a single recommendation table using the same fields as the multi-scenario example below (trigger, scope, read tools, safe outputs, permissions, noop condition). Only create `.github/workflows/<workflow-id>.md` if the user then explicitly asks to proceed.

### Multi-Scenario Evaluation Example

To compare multiple persona or task slices in a single request, use the following prompt format:

> agentic-workflows evaluate these scenarios without creating files:
> 1. Information Worker — weekly digest of open issues and PRs assigned to me
> 2. Product Manager — recurring backlog triage report sorted by staleness
> 3. Backend Engineer — API contract diff review on every pull request

Expected comparison output: return one combined table with one row per scenario (not a separate table per scenario), so the trigger/tool/safe-output choices can be compared side by side. Use the scenario's persona/task label as the row key and cover these columns:

| Scenario | Trigger | Scope | Read tools | Safe outputs | Permissions | Noop condition |
|---|---|---|---|---|---|---|
| Information Worker — weekly digest | `schedule` + `workflow_dispatch` | 7-day window, grouped by assignee | `github` (`gh-proxy`, default toolset) | `create-issue` with `close-older-issues: true` | `contents: read`, `issues: read` | window has no assigned issues/PRs |
| Product Manager — backlog triage | `schedule` + `workflow_dispatch` | recurring window, grouped by staleness bucket | `github` (`gh-proxy`, default toolset) | `create-issue` with `close-older-issues: true` | `contents: read`, `issues: read` | no items cross the staleness threshold |
| Backend Engineer — API contract review | `pull_request` with `paths:` scoped to API/schema files | per-PR, no window | `github` (`gh-proxy`, default toolset) | `add-comment` on the PR | `contents: read`, `pull-requests: read` | no API contract files changed in the PR |

This is the same invocation surface as [Single-Scenario Evaluation Example](#single-scenario-evaluation-example) above — reached only by addressing the `agentic-workflows` custom agent directly in conversation, never via a CLI/MCP tool parameter. After the comparison table, call out any scenario that shares a trigger or write path with another (for example two digests that could share a schedule) before offering to generate files.

### Failure Classification

When evaluating scenarios, classify any failure before stopping:

| Failure type | Symptom | Action |
|---|---|---|
| Transient issue | Network error, timeout, or quota exceeded | Retry once; if it persists, record `invocation_unavailable` and continue with partial results |
| Unsupported command | Unknown subcommand or unrecognized option | Record `command_not_supported`, document the gap, and fall back to providing the recommendation directly from local gh-aw guidance |
| Product gap | Invocation succeeds but returns no workflow-design guidance | Record `response_unavailable`, note the scenario, and surface it as a missing capability rather than treating it as an error |

## Design Checklist

### 1. Pick the workflow ID

- Derive kebab-case from the workflow name.
- Before creating the file, check whether `.github/workflows/<workflow-id>.md` already exists.
- If it exists, choose a more specific ID instead of overwriting.

### 2. Derive architecture, then choose the trigger

Use the smallest trigger that satisfies the augmented intent. Treat the mappings below as implementation options, not direct substitutions for intent reasoning. See the [Decision Matrix](triggers.md#decision-matrix) in triggers.md for the base trigger-to-use-case mapping.

Apply the [memory reuse checkpoint](designer.md#memory-reuse-checkpoint) before
<<<<<<< HEAD
selecting tools or persistence, and follow its routes to the decision and
implementation guidance.
=======
selecting tools or persistence, following the decision and scope policy in
[memory.md](memory.md#scheduled-dreaming).
>>>>>>> origin/pelikhan/agent-memory-differential

| Scenario | Trigger and default output | Details |
|---|---|---|
| Recurring reports and stakeholder digests | `schedule` (+ `workflow_dispatch` for reruns), usually `create-issue` | [Reporting/digest guidance](create-agentic-workflow-trigger-details.md#reporting-and-digest-guidance) |
| Persona-oriented requests (PM, design governance, compliance policy) | `pull_request` with scoped `paths:` when the request is framed around changed files (`tokens/**`, `**/*tokens*.json`, `**/theme/**`, `policy/**`, `compliance/**`, `controls/**`, `docs/policies/**`); `schedule` (+ `workflow_dispatch`) for recurring audits | [Persona scenario map](create-agentic-workflow-trigger-details.md#persona-oriented-scenario-map) |
| Backend schema/API review | `pull_request` with backend contract `paths:` and `add-comment` | [Backend review guidance](create-agentic-workflow-trigger-details.md#backend-review-guidance) |
| PR analyzers deciding comment vs issue vs noop | `pull_request` + escalation logic | [PR analyzer escalation](create-agentic-workflow-trigger-details.md#pr-analyzer-escalation-guidance) |
| Incident workflows | `workflow_run` / `deployment_status` with `create-issue` dedup | [Incident dedup-key templates](create-agentic-workflow-trigger-details.md#incident-dedup-key-templates-workflow_run-and-deployment_status) |
| CI regressions tied to a pull request | `workflow_run` with PR-comment escalation; repository-wide or unowned failures use deduplicated `create-issue` | Keep the visible output attached to the affected PR when one is known; use an issue when no single owner can be identified |
| Dependency-license and policy compliance | `pull_request` with manifest `paths:` | [Compliance review guidance](create-agentic-workflow-trigger-details.md#compliance-review-guidance) |
| Coverage analysis | `pull_request` or CI-linked triggers with explicit fallback | [Coverage-analysis guidance](create-agentic-workflow-trigger-details.md#coverage-analysis-guidance) |

Use [triggers.md](triggers.md), [workflow-patterns.md](workflow-patterns.md), and [create-agentic-workflow-trigger-details.md](create-agentic-workflow-trigger-details.md) for detailed trigger-selection patterns.

#### Choose the previous-result strategy

For recurring or PR-reactive workflows that create visible outputs, choose the strategy that best matches the workflow's goal:

- **Wait for the previous result** when a recurring workflow should have only one active issue or pull request. Configure `on.skip-if-match` to skip the entire agent execution while the item created by an earlier run remains open. The workflow resumes after that item is closed or merged.
- **Replace previous results** when the newest result supersedes older reports. For issues, configure `safe-outputs.create-issue.close-older-issues: true` and use `close-older-key` when an explicit matching key is needed. For PR comments, configure `safe-outputs.add-comment.hide-older-comments: true` for rolling updates.
- **Keep previous results** when each run should produce a distinct issue, pull request, or comment, or when an intentional history is needed. Instruct the agent to review prior outputs and ensure each new result has a distinct purpose.

Do not default every recurring or PR-reactive workflow to the same strategy. Base the choice on whether the workflow needs a single active item, a latest-only result, or a continuing series of distinct results, and include the selected behavior in the generated workflow.

### 3. Keep permissions read-only

See [workflow-constraints.md](workflow-constraints.md) for the read-only security posture. Specific to workflow creation:

- Do not grant `issues: write`, `pull-requests: write`, or `contents: write` to the agent job.
- For Copilot inference, recommend `permissions: { copilot-requests: write }` (scope and caveats: [syntax-core.md](syntax-core.md)).
- If the user asks for direct writes, explain why the safe-output pattern is required.

### 4. Select tools

- `bash` and `edit` are enabled by default in sandboxed workflows; do not add them unless you are restricting them.
- When selecting Codex, do not configure a non-empty per-command `tools.bash` allowlist: Codex cannot enforce it. Set `tools.bash: false` to disable shell execution or select an engine that supports command allowlists.
- For GitHub reads, prefer `tools.github.mode: gh-proxy` and instruct the agent to use `gh` commands.
- For non-GitHub MCP servers, prefer `tools.cli-proxy: true` and instruct the agent to use the mounted `mcp-clis` commands.
- Combined configuration example for GitHub reads plus non-GitHub MCP CLI access:

  ```yaml
  tools:
    github:
      mode: gh-proxy
      toolsets: [default]
    cli-proxy: true
  ```

  Omit `cli-proxy: true` when the workflow only needs GitHub reads.

- Suggest `playwright` for browser automation.
- Suggest dedicated topic files rather than embedding long tutorials in the prompt.

### 5. Infer network access from repository files

Do not ask for the ecosystem if it can be inferred from the repository. See [network.md#inferring-ecosystem-from-repository-files](network.md#inferring-ecosystem-from-repository-files) for the manifest-to-ecosystem mapping. Never use `network: defaults` alone for workflows that build, test, or install packages.

For policy/compliance audits that only inspect repository files and/or read GitHub via `gh-proxy`, enforce a "never probe externally" requirement structurally with `network.allowed: []`, rather than relying on prompt wording. Omit external web and MCP tools; for Claude or Codex, an explicit `network` setting also disables provider-hosted web tools unless enabled separately (see [network.md#hosted-web-policy-networkhosted-web](network.md#hosted-web-policy-networkhosted-web)).

```yaml
network:
  allowed: []
```

### 6. Configure safe outputs

Map write behavior to `safe-outputs:`.

Common mappings:

- create issues → `create-issue`
- add comments → `add-comment`
- create PRs → `create-pull-request`
- add labels → `add-labels`
- attach downloadable files → `upload-artifact`
- publish embeddable assets → `upload-asset`

Rules:

- always restrict `create-pull-request.allowed-files`
- prefer the dedicated safe output instead of shelling out to `gh` for the same mutation
- include `noop` guidance in the prompt so successful no-op runs are explicit
- when using `create-issue`, instruct the agent to provide a meaningful body (20-65000 characters; avoid placeholder-only text)

### 7. Decide who can trigger the workflow

- Default behavior is team-only triggering.
- For community-facing issue triage or other public entrypoints, recommend `roles: all`.
- Prefer `pull_request` over `pull_request_target`. Use `pull_request_target` only when base-repository context is necessary; it may have access to secrets and write permissions. Set `checkout: false` unless a trusted checkout is needed, and never check out or execute the pull request's head code. See the [`pull_request_target` checkout guidance](https://github.github.com/gh-aw/reference/checkout/#pull_request_target-checkout).

### 8. Add cost-aware triage and context flow

- For high-volume inputs, apply the [High-Volume Triage and Escalation Pattern](workflow-patterns.md#high-volume-triage-and-escalation-pattern): cheap triage first, `noop`/safe output for known/duplicate/stale/low-value cases, frontier reasoning reserved for ambiguous/high-value cases, and context pulled on demand.
- Use deterministic `steps:` plus compact files under `/tmp/gh-aw/` when large data must be preprocessed.

See also: [subagents.md](subagents.md) and [token-optimization.md](token-optimization.md).

### 9. Omit unnecessary defaults

Avoid adding fields just to restate defaults.

Usually omit:

- `engine: copilot`
- unrestricted `bash`
- `edit`
- `timeout-minutes:` unless a custom timeout is needed

## Prompt Requirements

The markdown body should:

- state the canonical intent clearly
- determine applicability using its activation conditions and required evidence
- produce the required effects only when the evidence supports them
- reference the triggering context explicitly
- name the allowed safe outputs when write actions are expected
- instruct the agent to call `noop` with a short reason when an inverse/no-op condition applies, including duplicates or insufficient evidence
- stay concise and task-focused

When `evals:` are appropriate, derive separate positive and adversarial scenario fixtures from required effects and inverse/no-op conditions as described in [intent.md](intent.md). Each BinEval run receives one fixture; phrase questions for that fixture, or explicitly return `UNKNOWN` when a shared question's scenario is not provided rather than mixing mutually exclusive assertions.

When the workflow generates reports or markdown output, follow [report.md#report-style-and-structure](report.md#report-style-and-structure) and [report.md#workflow-run-references](report.md#workflow-run-references).

## Issue-Form Mode Procedure

When processing a workflow-creation issue form:

1. extract the workflow name, description, and additional context
2. derive and persist a canonical intent, then augment it before implementation choices
3. derive a unique workflow ID and select an architecture, trigger, tools, network access, and safe outputs from the augmented intent
4. create exactly one workflow markdown file
5. compile it with `gh aw compile <workflow-id>`
6. include the generated `.lock.yml` in the PR

## Recommended Workflow Skeleton

```markdown
---
emoji: 🏷️
description: <brief description>
intent: <concise outcome, not an implementation>
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
  cli-proxy: true
safe-outputs:
  add-comment:
---

# <Workflow Name>

## Task

<clear instructions>

## Safe Outputs

- Use the configured safe outputs for visible actions.
- Use `noop` with a short explanation when no action is required.
```

## PR-Report Checklist

Before finalizing any `pull_request`-triggered reporting workflow, verify:

- [ ] **Permissions**: `contents: read` + `pull-requests: read` in the agent job; no write permissions
- [ ] **Safe outputs**: `add-comment` for inline findings; `create-issue` for incidents needing follow-up
- [ ] **Network**: infer ecosystem from repository lock files; never use `defaults` alone when packages are installed
- [ ] **`noop` required**: prompt instructs the agent to call `noop` with a brief explanation when no issues are found

## Generated Workflow Quality Checklist

Before finalizing any newly generated workflow, verify:

- [ ] **Trigger fit**: trigger matches user intent and event granularity (for example `pull_request`, `workflow_run`, `deployment_status`, `schedule`, `slash_command`)
- [ ] **Maintenance baseline**: recurring maintenance strategies are derived from a bounded repository survey, with observed signals separated from recommendations
- [ ] **Tool fit**: enabled tools are the minimal set needed for reads/analysis (prefer `gh-proxy`; add `playwright`/`cache-memory` only when required)
- [ ] **Safe outputs**: all visible writes route through `safe-outputs:` and include `noop` for explicit no-op outcomes
- [ ] **Recurring audit/monitor dedup**: define a stable signature from the scope and finding (for example workflow, job, and normalized error signature), check for an existing open issue before creating one, and choose `close-older-issues`/`close-older-key` when newer findings supersede older ones
- [ ] **Permissions**: agent job remains read-only; no direct write scopes granted
- [ ] **Network**: access is inferred from repository ecosystem and avoids `network: defaults` alone for install/build/test workflows
- [ ] **Prompt clarity**: prompt is concise, context-aware, and clearly states expected outputs and stop/no-op behavior

## Generated Workflow Scoping Checklist

Before finalizing any newly generated workflow, verify:

- [ ] **Paths scope**: include `paths:`/`paths-ignore:` when the automation should ignore unrelated files (for backend reviews, include migration/schema/API contract globs; for design governance, include design-token/theme globs like `tokens/**` and `**/theme/**`; for compliance policy reviews, include policy/control docs like `policy/**`, `compliance/**`, `controls/**`, `docs/policies/**`)
- [ ] **Labels scope**: define required labels (for example `label_command` names or PR/issue label filters) when label-based routing is expected
- [ ] **Workflow-name scope**: for `workflow_run`, explicitly set `workflows:` to named targets and gate conclusions via `if:` on `${{ github.event.workflow_run.conclusion }}` (for incident triage, prefer failure-only outcomes)
- [ ] **Date-window scope**: for reporting/triage, state the exact window (for example `last 24h`, `since previous run`, `current week`)
- [ ] **Safe-output write contract**: name which safe output is used for each outcome and when `noop` is required instead of a write

## Multi-Repository Requests

For cross-repository workflows, first determine whether the question is **finite and bounded**:

- If the answer requires arbitrary source-code extraction, full file contents, or other unbounded access:
  - enable the GitHub toolsets needed to read external repositories
  - configure cross-repo authentication in `safe-outputs:`
  - tell the agent to set `target-repo`
  - explain that the workflow still cannot wait for external workflows or create multi-job orchestration

Use [workflow-patterns.md](workflow-patterns.md) for the compact cross-repo pattern.

## Final Steps

1. create `.github/workflows/<workflow-id>.md`
2. compile with `gh aw compile <workflow-id>`
3. fix all compile errors
4. create a PR with the workflow file and `.lock.yml`

## Guidelines

- create exactly one workflow `.md` file as the primary deliverable
- keep prompts short, specific, and imperative
- prefer dedicated reference files over repeating large explanations inline
- always compile before finishing
- keep responses concise after the workflow is created
