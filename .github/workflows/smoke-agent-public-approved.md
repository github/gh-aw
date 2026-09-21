---
private: true
emoji: "🧪"
description: Smoke test that validates assign-to-agent with the agentic-workflows custom agent
on:
  schedule: every 2 days
  slash_command:
    name: smoke-agent-public-approved
    strategy: centralized
    events: [issues, issue_comment, pull_request, pull_request_comment]
  workflow_dispatch:
  pull_request:
    types: [labeled]
    names: ["metal"]
  status-comment: true
permissions:
  contents: read
  issues: read
  pull-requests: read

sandbox:
  agent:
    id: awf
name: "Smoke Agent: public/approved"
engine: claude
strict: true
imports:
  - shared/github-guard-policy.md
  - shared/otlp.md
  - shared/reporting.md
tools:
  cli-proxy: true
  github:
    mode: local
    allowed-repos: "public"
    min-integrity: approved
network:
  allowed:
    - defaults
    - github
safe-outputs:
  allowed-domains: [default-safe-outputs]
  assign-to-agent:
    target: "*"
    max: 1
    allowed: [copilot]
    custom-agent: agentic-workflows
  add-comment:
    hide-older-comments: true
    max: 2
  messages:
    footer: "> 🤖 *Smoke test by [{workflow_name}]({run_url})*{ai_credits_suffix}{history_link}"
    run-started: "🤖 [{workflow_name}]({run_url}) is looking for a Smoke issue to assign..."
    run-success: "✅ [{workflow_name}]({run_url}) completed. Issue assigned to the agentic-workflows agent."
    run-failure: "❌ [{workflow_name}]({run_url}) {status}. Check the logs for details."
timeout-minutes: 10
features:
  gh-aw-detection: false
---

# Smoke Agent: assign-to-agent with agentic-workflows

This workflow validates that `assign-to-agent` works correctly with the `agentic-workflows` custom agent.

## Instructions

1. **Find a Smoke issue**: Use the GitHub MCP tools to search for an open issue in ${{ github.repository }} whose title starts with "Smoke". Use the search query: `is:issue is:open in:title Smoke repo:${{ github.repository }}`. Pick the first result.

2. **Assign the issue**: Use the `assign_to_agent` safe-output tool to assign the issue to copilot using the `agentic-workflows` custom agent:

   ```json
   {
     "type": "assign_to_agent",
     "issue_number": <issue_number>,
     "agent": "copilot"
   }
   ```

3. **Report**: Add a brief comment to the current pull request confirming the issue number that was assigned and which agent was used.

If no Smoke* issue is found, use the `noop` tool to report that no matching issue was found.