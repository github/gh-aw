---
name: Test Assign to Agent with Model
description: Test workflow for assign_to_agent safe output with model parameter
on:
  issues:
    types: [labeled]
  workflow_dispatch:
    inputs:
      issue_number:
        description: 'Issue number to test with'
        required: true
        type: string
      model:
        description: 'AI model to use'
        required: false
        type: string
        default: 'claude-sonnet-5'

permissions:
  actions: read
  contents: read
  issues: read
  pull-requests: read

# NOTE: Assigning Copilot agents requires:
# 1. A Personal Access Token (PAT) or GitHub App token with repo scope
#    - The standard GITHUB_TOKEN does NOT have permission to assign bot agents
#    - Create a PAT at: https://github.com/settings/tokens
#    - Add it as a repository secret named GH_AW_AGENT_TOKEN
#    - Required scopes: repo (full control) or fine-grained: actions, contents, issues, pull-requests (write)
# 
# 2. All four workflow permissions declared above (for the safe output job)
#
# 3. Repository Settings > Actions > General > Workflow permissions:
#    Must be set to "Read and write permissions"

engine: copilot
timeout-minutes: 5

safe-outputs:
  assign-to-agent:
    max: 5
    name: copilot
    model: claude-sonnet-5  # Default model to use when not specified per-item
    reasoning-effort: high  # Forwarded independently of the selected model
    target: "triggering"     # Auto-resolves from workflow context (default)
    allowed: [copilot]       # Only allow copilot agent
strict: false
---

# Assign to Agent with Model Test Workflow

This workflow tests the `assign_to_agent` safe output feature with the new `model` parameter.

## Task

Assign the Copilot agent to the issue with the specified AI model. The workflow demonstrates:

1. **Default model configuration**: Set via `safe-outputs.assign-to-agent.model` in frontmatter (applies to all assignments in the workflow)

**For issues event:**
Assign the Copilot agent using the default model (Claude Sonnet 5) to the triggering issue.

**For workflow_dispatch:**
Assign the Copilot agent to issue #${{ github.event.inputs.issue_number }} using the default model configured in the frontmatter.

Use the `assign_to_agent` tool from the `safeoutputs` MCP server:

```
assign_to_agent(issue_number=<issue_number>, agent="copilot")
```

The model is configured at the workflow level in the frontmatter and applies to all assignments.

Available models include:
- `auto` - Auto-select model (default, currently Claude Sonnet 4.5)
- `claude-sonnet-4.5` - Claude Sonnet 4.5
- `claude-opus-4.5` - Claude Opus 4.5
- `claude-opus-4.6` - Claude Opus 4.6
- `gpt-5.1-codex-max` - GPT-5.1 Codex Max
- `gpt-5.2-codex` - GPT-5.2 Codex
