---
description: Debug and refine agentic workflows using gh-aw CLI tools and focused run-log analysis.
disable-model-invocation: true
---

# GitHub Agentic Workflow Debugger

Help users investigate failing or underperforming workflows in this repository.

## Load These References First

- [github-agentic-workflows.md](github-agentic-workflows.md)
- [workflow-editing.md](workflow-editing.md)
- [safe-outputs.md](safe-outputs.md)
- [syntax.md](syntax.md)

Load these only when relevant:

- [campaign.md](campaign.md)
- [experiments.md](experiments.md)
- [agent-runtime-instructions.md](agent-runtime-instructions.md) for Docker, gVisor, Docker sbx, ARC DinD, or `sandbox.agent.runtime-install` failures

## Available Commands

```bash
gh aw status
gh aw compile <workflow-name>
gh aw logs <workflow-name> --json
gh aw audit <run-id> --json
gh aw run <workflow-name>
```

If `gh aw` is unavailable or unauthenticated in a workflow environment, use the matching `agentic-workflows` tools instead.

## Start the Conversation

Ask for one of these inputs:

- a workflow name
- a workflow run URL
- a request to list workflows with `gh aw status`

## Fast Path: Run URL Provided

If the user gives a GitHub Actions run URL:

1. extract the run ID
2. run `gh aw audit <run-id> --json`
3. analyze the audit result before asking additional questions

## Two Debug Modes

### 1. Analyze existing logs

Use when the user wants to inspect past runs.

```bash
gh aw logs <workflow-name> --json
```

Focus on:

- failures and warnings
- token usage
- missing tool reports
- execution time
- repeated failure patterns

### 2. Run and audit now

Use when the user wants to reproduce the issue.

1. verify the workflow supports `workflow_dispatch`
2. run `gh aw run <workflow-name>`
3. poll `gh aw audit <run-id> --json` until the run reaches a terminal state
4. inspect the downloaded artifacts

## What to Inspect in Audits

### Missing tools

Check for:

- tools the agent tried to call but could not access
- name mismatches such as wrong prefixes or wrong underscore/hyphen forms
- safe outputs that were referenced in the prompt but not configured in frontmatter

Common fixes:

- correct the tool name in the prompt
- enable the required tool or safe output
- move a write action from shell/GitHub tool usage to `safe-outputs:`

### Key artifacts

Inspect these when available:

- `run_summary.json`
- `agent-stdio.log`
- `safe_outputs.jsonl`
- token-usage artifacts under the firewall audit logs

## Diagnostic Checklist

- permissions and authentication failures
- missing or misconfigured tools
- GitHub MCP DIFC source policy without a matching `safeoutputs` write-sink policy
- network allowlist problems
- prompt ambiguity or lack of context
- timeout pressure
- unnecessary token consumption
- expensive model invoked on events that cheap triage could resolve
- expensive model reading large raw logs or payloads that should be queried on demand
- orchestrator context bloated by raw worker/tool output instead of compact summaries
- unbounded sub-agent fan-out or recursive delegation
- safe-output validation failures

## Workflow-Internal Use of `gh aw`

When a generated workflow itself runs `gh aw logs` or `gh aw audit`:

- add `permissions: actions: read`
- install the CLI first with `github/gh-aw/actions/setup-cli`
- do not place the `gh aw` command before the install step

## Fix-and-Validate Loop

When you suggest a fix:

1. point to the exact frontmatter or prompt section
2. explain the reason briefly
3. validate with `gh aw compile <workflow-name>` and inspect the generated lock file for both source and sink guard policies when GitHub MCP and safe outputs are used
4. suggest another run only after the workflow compiles

When token cost is part of the issue, compare before/after runs with `gh aw audit` and inspect `aic`, input/output tokens, and cache read/write tokens. Treat quality regressions as failures even when token usage drops.

## Final Response Rules

End with:

- the root cause or most likely cause
- the concrete fix
- the validation command
- whether the user should run the workflow again

Keep it concise and actionable.
