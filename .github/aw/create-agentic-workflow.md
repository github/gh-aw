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

- [maintainer.md](maintainer.md) for recurring repository maintenance, backlog triage, owned-PR upkeep, or long-term code health
- [campaign.md](campaign.md) for campaign, KPI, pacing, cadence, or `stop-after`
- [experiments.md](experiments.md) for experiments, A/B tests, variants, or prompt comparisons
- [visual-regression.md](visual-regression.md) for screenshot comparison workflows
- [deployment-status.md](deployment-status.md) for external deployment monitoring
- [charts.md](charts.md) for chart-generation workflows
- [report.md](report.md) for reporting output structure and recurring report lifecycle
- [release-workflow.md](release-workflow.md) for release workflows that build, test, publish a GitHub release, and generate release highlights
- [linter-workflows.md](linter-workflows.md) for mining, refining, or applying custom linter rules
- [agent-runtime-instructions.md](agent-runtime-instructions.md) when choosing or debugging Docker, gVisor, Docker sbx, ARC DinD, self-hosted runners, or `sandbox.agent.runtime-install`
- [skills.md](skills.md) when the user asks for specific skills or agent plugins

## Skills and Plugins

When the user requests specific skills or agent plugins, declare them in the built-in top-level `skills:` and `plugins:` frontmatter fields — gh-aw installs them before the agent runs. Never generate on-the-fly installation (`steps:` running `gh skill install`, `copilot plugin install`, `npx`, `curl`, or `git clone`) and never instruct the agent to install a skill or plugin from the prompt body. See [skills.md](skills.md).

## Modes

### Interactive mode

When the user has not already stated an automation goal, start with exactly:

> What do you want to automate today?

When the request already states a goal, infer its intent and ask only for information that is still needed. Then follow a progressive interview — ask one question at a time, advance only when the current phase is clear:

1. **Goal and intent** — confirm the workflow name, description, and a concise outcome-oriented `intent:`. Derive activation, required-effect, no-op, success, and uncertainty conditions before choosing implementation; see [intent.md](intent.md).
2. **Repository survey and intent mining** — only for maintenance or underspecified automation requests, inspect bounded repository evidence using [maintainer.md](maintainer.md). Summarize observed signals, propose evidence-backed candidate intents, then select and augment one before choosing a portfolio or cadence.
3. **Architecture and trigger** — compare feasible architectures against the augmented intent's coverage, timeliness, attention cost, safety, boundedness, determinism, state, complexity, and evidence. Then ask "When should this run?" and map the selected architecture to an `on:` block. For scheduled workflows that create issues or pull requests, also choose how previous results are handled using [Choose the previous-result strategy](#choose-the-previous-result-strategy).
4. **Scope** — ask what it reads and what it creates or updates; map to `permissions:`, `tools:`, and `safe-outputs:`.
5. **Data strategy** — ask whether GitHub data should be pre-fetched with `gh` + `jq` (DataOps default); map to `steps:`.
6. **Guardrails** — ask whether it should block, advise, or silently log; guide toward `noop` and safe-output behavior.
7. **Context & network** — ask about external APIs, MCP servers, and required secrets; map to `network.allowed` and `env:`.
8. **Engine** — preserve explicit engine hints. With no engine preference or
   engine-specific requirement, omit `engine:` and let the configured default
   apply. If an explicit model requirement forces engine selection, try Copilot
   first.
9. **Confirmation** — present a structured summary before generating:

   ```text
   Proposed workflow:
   - Name: <workflow-id>
   - Trigger: <event + key options>
   - Engine: <explicit engine or default (omitted)>
   - Tools: <tool summary>
   - Safe outputs: <list or none>
   - Network: <allowed summary>
   - Integrations/Auth: <service/mcp + required secrets/env vars>
   - Repository signals: <maintenance workflows only>
   - Initial maintenance portfolio: <maintenance workflows only>
   - Intent: <one-sentence task>
   ```

   Ask: **"Ready to generate, or want to adjust anything?"**

Skip phases when the answer is already clear from earlier statements. Apply progressive disclosure: at most 5 questions before presenting the confirmation summary; then ask "anything else?" if needed. Detect done signals (`that's it`, `looks good`, `generate it`) and proceed to generation.

For detailed trigger/safe-output/network/tool decision heuristics and integration auth setup patterns, load [designer-mappings.md](designer-mappings.md). For token-optimization defaults, load [designer.md](designer.md).

### Issue-form mode

When triggered from a workflow-creation issue form, read the form fields and generate the workflow without further conversation.

## Conversation Rules

- Keep the conversation short and iterative.
- Translate user intent into workflow structure.
- When the user asks for exploration, evaluation, or scenario design rather than file creation, stay in ad hoc evaluation mode.
- In ad hoc evaluation mode, do not create `.github/workflows/*.md`.
- Do not overwhelm the user with long option dumps unless they ask.
- If the request exceeds the single-job model, explain the constraint and recommend traditional GitHub Actions.

## Ad Hoc Evaluation Mode

Use this mode for exploratory testing, persona walkthroughs, and "what workflow would you create for this scenario?" requests.

- Do not create or edit workflow files.
- Return a compact recommendation covering trigger, any scoped `paths:` filters for file-event triggers, read tools, safe outputs, permissions, and explicit `noop` criteria.
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
| Information Worker — weekly digest | `schedule` + `workflow_dispatch` | 7-day window, grouped by assignee | `github` (`gh-proxy`, default toolset) | `create-issue` with `close-older-issues: true` | `contents: read`, `issues: write` | window has no assigned issues/PRs |
| Product Manager — backlog triage | `schedule` + `workflow_dispatch` | recurring window, grouped by staleness bucket | `github` (`gh-proxy`, default toolset) | `create-issue` with `close-older-issues: true` | `contents: read`, `issues: write` | no items cross the staleness threshold |
| Backend Engineer — API contract review | `pull_request` with `paths:` scoped to API/schema files | per-PR, no window | `github` (`gh-proxy`, default toolset) | `add-comment` on the PR | `contents: read`, `pull-requests: write` | no API contract files changed in the PR |

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

For every daily or scheduled workflow that creates issues or pull requests, choose the strategy that best matches the workflow's goal:

- **Wait for the previous result** when only one active result should exist. Configure `on.skip-if-match` to skip the entire agent execution while the issue or pull request created by an earlier run remains open. The workflow resumes after that item is closed or merged.
- **Replace previous results** when the newest result supersedes older reports. For issues, configure `safe-outputs.create-issue.close-older-issues: true` and use `close-older-key` when an explicit matching key is needed.
- **Keep previous results** when each run should produce a distinct item or preserve a history of work. Instruct the agent to search for and review existing issues or pull requests before acting, then select a materially different scope so it does not repeat previous work. Treat those existing items as the workflow's memory.

Do not default every scheduled workflow to the same strategy. Base the choice on whether the workflow needs a single active item, a latest-only result, or a continuing series of distinct results, and include the selected behavior in the generated workflow.

### 3. Keep permissions read-only

See [workflow-constraints.md](workflow-constraints.md) for the read-only security posture. Specific to workflow creation:

- Do not grant `issues: write`, `pull-requests: write`, or `contents: write` to the agent job.
- When targeting the Copilot coding agent, recommend `permissions: { copilot-requests: write }` so Copilot can authenticate with `${{ github.token }}`.
- If the user asks for direct writes, explain why the safe-output pattern is required.

### 4. Select tools

- `bash` and `edit` are enabled by default in sandboxed workflows; do not add them unless you are restricting them.
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
