---
description: Structured interview playbook for turning user goals into complete, runnable agentic workflow specifications.
---

# Workflow Designer

Use this skill to run a structured interview with users who know their goal but not the workflow syntax yet, then generate one complete workflow `.md` file.

## When to Use This Skill

- Use `.github/aw/designer.md` to discover and confirm requirements.
- Use `.github/aw/create-agentic-workflow.md` once requirements are clear and ready for implementation.
- Use `.github/aw/agentic-chat.md` when the user wants a specification/pseudo-code instead of a runnable workflow file.
- Load `.github/aw/maintainer.md` when the goal is recurring repository maintenance, backlog reduction, owned-PR upkeep, or long-term code health.

## Interview Framework

Ask one question at a time. Move to the next phase only after the current phase is clear.

### Phase 1: Goal

Ask: **"What do you want to automate?"**

Capture:
- Workflow name (kebab-case candidate)
- Brief description
- Optional emoji

### Phase 1a: Intent

Before selecting a trigger or implementation, load [intent.md](intent.md) and derive the concise canonical outcome and transient IntentSpec. Use it to derive PromptPex eval and inverse-eval scenarios and, when needed, operational value. Confirm the outcome when it is ambiguous and persist it later as `intent:`. For explicit, narrow requests, keep this step lightweight.

### Phase 1b: Repository Survey and Intent Mining

For maintenance or broad automation requests, run the bounded survey in [maintainer.md#survey-the-repository-before-choosing-a-strategy](maintainer.md#survey-the-repository-before-choosing-a-strategy). Record examined sources, observed signals, and confidence; if evidence is insufficient, stop and ask the user rather than inventing a portfolio. Separate observed signals from inferred strategy, derive evidence-backed candidate intents, and present competing candidates when none clearly dominates before selecting and augmenting one. Ask only about policy choices that cannot be inferred.

### Phase 2: Trigger

Ask: **"When should this run?"**

Follow up only if needed:
- Which event type(s)?
- Any filters (labels, branches, commands)?
- Scheduled cadence (daily/weekly/hourly)?

Compare candidate architectures against the IntentSpec, then map the selected one to the `on:` block.

### Phase 3: Scope (Read/Write)

Ask:
- **"What should it read?"** (issues, PRs, code, discussions, CI data)
- **"What should it create or update?"** (comments, issues, PRs, labels)

Map to:
- `permissions:` (keep read-only for agent job)
- `tools:`
- `safe-outputs:`

### Phase 4: Data Strategy

Ask:
- **"What data does the agent need to make decisions?"**
- Follow up: **"Can we pre-fetch and aggregate that data with shell commands so the agent only reads compact JSON?"**

Capture:
- Whether `steps:` should pre-fetch GitHub data with `gh` + `jq`
- Output paths under `/tmp/gh-aw/data/`
- Whether batch work should use sub-agents

Map to:
- `steps:`
- Prompt references to pre-computed file paths

### Phase 5: Guardrails

Ask: **"Should it block merging, just advise, or silently log?"**

Capture:
- Visibility expectations (comment, issue, no visible output)
- No-op behavior expectation

Guide toward safe output behavior and explicit `noop` instructions.

### Phase 6: Context & Network

Ask: **"Does it need external APIs, web access, package installs, or MCP servers?"**

Follow up:
- **"Any third-party services or MCP servers to include (for example Slack, Jira, Datadog, custom internal MCP)?"**
- **"Are you deploying on GitHub.com, GHEC with custom endpoints, or GHES?"**
- For each integration, identify required auth from source docs and map it to GitHub Actions secrets + workflow env variables.
- Ask for exact external domains (FQDN/wildcard).

Map to:
- `network.allowed`
- Optional MCP/GitHub tool usage in `tools:`
- `secrets:` / `env:` wiring for integration tokens
- GHES/GHEC settings such as `engine.api-target` and `aw.json` `ghes: true` (when applicable)

### Phase 7: Engine (optional)

Ask **"Any AI engine preference?"** only when the request contains ambiguous
engine-specific hints.

Omit `engine:` and let the configured default apply unless there's an explicit
preference or a requirement the default can't satisfy — then map to `engine:`,
trying Copilot first.

### Phase 7b: Skills, Plugins, LSP & Evals (optional)

Ask only when relevant: **"Does the agent need extra domain knowledge, agent plugins, language-server code intelligence, or automated success checks?"**

Map to:
- `skills:` — pinned external skills (`owner/repo/skill@sha`) or local paths (`.github/skills/<name>`) when the agent needs domain knowledge (see `.github/aw/skills.md`)
- `plugins:` — pinned agent plugins (`owner/repo[/path]@ref`) when the user names specific plugins; experimental and unsupported by `gemini`/`pi` (see `.github/aw/skills.md`)
- `lsp:` — language servers for code intelligence; **experimental** and only valid with `engine: copilot` (see `.github/aw/lsp.md`)
- `evals:` — binary YES/NO questions checking whether the run met its goals; requires `safe-outputs:` so `agent_output.json` exists (see `.github/aw/evals.md`)

gh-aw installs `skills:` and `plugins:` entries before the agent runs. Never emit install steps or prompt instructions that fetch skills or plugins on the fly.

### Phase 8: Confirmation

Present a structured summary and ask for approval before generation.

## Decision Heuristics

Load `.github/aw/designer-mappings.md` for the full trigger, safe-output, network, tool, pattern, integration-auth, and data-strategy mapping tables used to translate interview answers (Phases 2–7) into workflow syntax.

## Token Optimization Defaults

Apply these defaults unless the user explicitly asks otherwise:

1. Use DataOps by default for GitHub reads: pre-fetch/aggregate with `gh` + `jq` in `steps:`, store compact JSON in `/tmp/gh-aw/data/`, and point the prompt to those files (see `.github/aw/token-optimization.md` for details).
2. Keep tool surface minimal: default to `tools.github.mode: gh-proxy`, include only required toolsets, and prefer `bash` + `gh` for simple reads.
3. For batch workloads, split items into compact data and suggest sub-agent processing with `model: small`.
4. Keep prompts compact: concise imperative instructions, explicit file paths, single-line `noop` guidance, and stable instructions before dynamic content.

## Progressive Disclosure Rules

1. Never dump all options at once; ask one targeted question at a time.
2. Skip questions when answers are inferable from prior user statements.
3. Offer smart defaults and request confirmation instead of over-questioning.
4. Ask at most 5 questions before presenting a summary; then ask "anything else?" if needed.
5. Detect done signals (`that's it`, `looks good`, `generate it`) and proceed to generation.

## Confirmation Format

Use this exact structure:

```text
📋 Proposed workflow:
- Name: <workflow-id>
- Trigger: <event + key options>
- Engine: <explicit engine or default (omitted)>
- Tools: <tool summary>
- Safe outputs: <list or none>
- Network: <allowed summary>
- Integrations/Auth: <service/mcp + required secrets/env vars>
- Deployment: <GitHub.com or GHEC/GHES details>
- Intent: <one-sentence task>
```

Then ask: **"Ready to generate, or want to adjust anything?"**

## Generation Template

After confirmation, generate one workflow file using the same skeleton style as `.github/aw/create-agentic-workflow.md`.

```markdown
---
emoji: <emoji>
description: <brief description>
intent: <concise outcome, not an implementation>
on:
  <trigger config>
permissions:
  contents: read
  issues: read
  pull-requests: read
tools:
  github:
    mode: gh-proxy
    toolsets: [default]
steps:
  - name: <optional data prefetch>
    run: |
      mkdir -p /tmp/gh-aw/data
      <gh + jq commands that produce compact JSON>
safe-outputs:
  <safe-output-types-if-needed>
network:
  allowed:
    - defaults
    - <additional entries if needed>
skills:
  - <owner/repo/skill@sha or .github/skills/<name> — only if domain knowledge is needed>
plugins:
  - <owner/repo[/path]@ref — only if the user asked for specific agent plugins>
lsp:
  <language-key>:                 # optional, engine: copilot only (experimental)
    command: <server-executable>
    fileExtensions:
      ".<ext>": <language-id>
evals:
  - id: <eval-id>                 # optional, requires safe-outputs
    question: <binary YES/NO question about the agent output>
---

# <Workflow Name>

## Task

Objective: <canonical intent>

Determine applicability from the activation conditions and required context. Produce the required effects only when the evidence threshold is met. If a no-op condition applies, including insufficient evidence or a duplicate, call `noop` with a short reason and take no visible write action.
If `steps:` includes pre-fetch commands, read the resulting `/tmp/gh-aw/data/*.json` files instead of broad live re-fetches.

## Safe Outputs

- Use configured safe outputs for all visible write actions.
- Call `noop` with a short reason when no action is needed.
```

## Validation Checklist

Before final output, run this internal self-check:

- [ ] Agent job permissions remain read-only (writes only via safe outputs)
- [ ] `safe-outputs:` covers every write action mentioned in prompt/instructions
- [ ] Network access is scoped; avoid blanket wildcard entries
- [ ] Trigger matches the user's intended activation event
- [ ] `intent:` is a concise outcome, and the selected architecture follows the augmented IntentSpec
- [ ] Prompt instructs agent to call `noop` when no action is needed
- [ ] Prompt states applicability, required effects, and inverse/no-op conditions
- [ ] Unnecessary defaults are omitted (for example `engine: copilot`)
- [ ] If reading GitHub data, `steps:` pre-fetches compact JSON (DataOps)
- [ ] `tools.github.mode` is `gh-proxy` unless broader MCP toolsets are explicitly needed
- [ ] Only required toolsets are listed (avoid blanket toolset lists)
- [ ] Prompt references specific pre-computed file paths
- [ ] For batch processing (>5 items), sub-agent pattern is suggested
- [ ] Network entries use valid ecosystem identifiers (no `npm`/`pypi`/`docker`-style invalid shorthands)
- [ ] `skills:` entries are pinned (`owner/repo/skill@sha`) or local paths, and only added when domain knowledge is needed
- [ ] `plugins:` entries are pinned (`owner/repo[/path]@ref`) and only added when the user asked for specific agent plugins
- [ ] Skills and plugins are declared in frontmatter — no on-the-fly install steps or prompt-driven installation
- [ ] `lsp:` is only used with `engine: copilot` (experimental; omit otherwise)
- [ ] `evals:` questions are binary YES/NO and `safe-outputs:` is declared so `agent_output.json` exists
- [ ] Evals, when used, cover both an intent-required effect and a counter-case through separate scenario fixtures or scenario-aware questions; do not require mutually exclusive outcomes from one run
- [ ] For each third-party service/MCP integration, required secrets/env vars are listed
- [ ] Auth guidance includes least-privilege token scope recommendations
- [ ] For GHEC/GHES deployments, `engine.api-target` and GHES compatibility guidance are included when needed

## References (load only when needed)

- `.github/aw/designer-mappings.md` (trigger, safe-output, network, tool, pattern, integration-auth, and data-strategy mapping tables)
- `.github/aw/syntax.md` (index → `.github/aw/syntax-core.md`, `.github/aw/syntax-agentic.md`, `.github/aw/syntax-tools-imports.md`)
- `.github/aw/safe-outputs.md` (index → `.github/aw/safe-outputs-content.md`, `.github/aw/safe-outputs-management.md`, `.github/aw/safe-outputs-automation.md`, `.github/aw/safe-outputs-runtime.md`)
- `.github/aw/network.md`
- `.github/aw/patterns.md`
- `.github/aw/subagents.md`
- `.github/aw/token-optimization.md`
- `.github/aw/triggers.md`
- `.github/aw/create-agentic-workflow.md`
- `.github/aw/skills.md`
- `.github/aw/lsp.md`
- `.github/aw/evals.md`
- `.github/aw/intent.md`

Outside the repo, use `https://github.com/github/gh-aw/blob/main/<path>` for any of the above.
