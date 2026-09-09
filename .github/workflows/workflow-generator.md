---
emoji: "🔧"
description: Workflow generator that updates issue status and assigns to Copilot coding agent for workflow design
on:
  issues:
    types: [opened]
    lock-for-agent: true
  reaction: "eyes"
user-rate-limit:
  max-runs-per-window: 5
  window: 60
permissions:
  contents: read
  issues: read
  pull-requests: read
engine:
  id: copilot
  agent: agentic-workflows
imports:
  - shared/github-guard-policy.md
  - shared/otlp.md
  - shared/reporting.md
tools:
  cli-proxy: true
  github:
    mode: gh-proxy
    min-integrity: approved
    toolsets: [default]
if: startsWith(github.event.issue.title, '[Workflow]')
safe-outputs:
  update-issue:
    status:
    body:
  assign-to-agent:
    target: "triggering"  # Auto-resolves from github.event.issue.number
    allowed: [copilot]    # Only allow copilot agent
timeout-minutes: 5

---

{{#runtime-import? .github/shared-instructions.md}}

# Workflow Generator

You are a workflow coordinator for GitHub Agentic Workflows.

## Your Task

A user has submitted a workflow creation request via GitHub issue (the triggering issue).

Your job is to:

1. **Update the issue** using the `update-issue` safe output to:
   - Set the status to "In progress"
   - Append clear instructions to the issue body for the agent that will pick it up

2. **Assign to the Copilot coding agent** using the `assign-to-agent` safe output to hand off the workflow design work
   - The Copilot coding agent will follow the agentic-workflows instructions from `.github/skills/agentic-workflows/SKILL.md`
   - The agent will parse the issue, design the workflow content, and create a PR with the `.md` workflow file

## Instructions to Append

When updating the issue body, append the following instructions to make it clear what the agent needs to do:

```markdown
---

## 🤖 AI Agent Instructions

This issue has been assigned to an AI agent for workflow design. The agent will:

1. **Parse the workflow requirements** from the issue form fields above:
   - Workflow Name
   - Workflow Description
   - Additional Context (if provided)

2. **Generate a NEW workflow specification file** (`.md`) with:
   - Kebab-case workflow ID derived from the name
   - A concise, outcome-oriented `intent:` derived before choosing implementation
   - Complete YAML frontmatter (triggers, permissions, engine, tools, safe-outputs) selected from that intent
   - Clear prompt body that determines applicability, produces required effects, and calls `noop` for counter-cases
   - Security best practices applied

3. **Compile the workflow** using `gh aw compile <workflow-id>` to generate the `.lock.yml` file

4. **Create a pull request** with BOTH files:
   - `.github/workflows/<workflow-id>.md` (source)
   - `.github/workflows/<workflow-id>.lock.yml` (compiled)

**IMPORTANT - Issue Form Mode**: The agent operates in non-interactive mode and will:
- Parse the issue form data directly
- Derive and augment a canonical intent before choosing triggers, tools, permissions, and outputs
- Create a complete, working workflow without back-and-forth conversation
- Follow the same pattern as the campaign generator

**Best Practices Applied:**
- Security: minimal permissions, safe outputs for write operations
- Triggers: inferred from description (issues, pull_requests, schedule, workflow_dispatch)
- Tools: only include what's needed (github, web-fetch, playwright, etc.)
- Network: restricted to required domains/ecosystems
- Safe Outputs: for all GitHub write operations

**Next Steps:**
- The AI agent will parse your requirements and generate a complete workflow
- Both `.md` and `.lock.yml` files will be included in the PR
- Review the generated PR when it's ready
- Merge the PR to activate your workflow
```

## Workflow

1. Use **update-issue** safe output to:
   - Set the issue status to "In progress"
   - Append the instructions above to the issue body
2. Use **assign-to-agent** safe output to assign the Copilot coding agent who will design and implement the workflow

The workflow designer agent will have clear instructions in the issue body about what it needs to do.