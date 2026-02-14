---
description: Smoke Copilot SDK
on: 
  workflow_dispatch:
  pull_request:
    types: [labeled]
    names: ["smoke"]
  reaction: "eyes"
permissions:
  contents: read
  pull-requests: read
  issues: read
  discussions: read
  actions: read
name: Smoke Copilot SDK
engine: copilot-sdk
imports:
  - shared/gh.md
  - shared/reporting.md
tools:
  agentic-workflows:
  cache-memory: true
  edit:
  bash:
    - "*"
  github:
safe-outputs:
    add-comment:
      allowed-repos: ["github/gh-aw"]
      hide-older-comments: true
      max: 2
    create-issue:
      expires: 2h
      group: true
      close-older-issues: true
    add-labels:
      allowed: [smoke-copilot-sdk]
      allowed-repos: ["github/gh-aw"]
    remove-labels:
      allowed: [smoke]
---

# Smoke Test for Copilot SDK Engine

This is a smoke test workflow for the experimental `copilot-sdk` engine.

## Test Requirements

The copilot-sdk engine should:
1. Successfully start Copilot CLI in headless mode on port 3312
2. Configure the SDK client via the GH_AW_COPILOT_CONFIG environment variable
3. Execute the copilot-client.js wrapper successfully
4. Handle basic tasks (file analysis, issue creation, etc.)
5. Support the GitHub MCP toolset
6. Use host.docker.internal for MCP server connections

## Task

Please perform the following smoke test:

1. Analyze the current repository structure and identify key files
2. Create a test issue with a summary of what you found
3. Add the label "smoke-copilot-sdk" to the issue
4. Remove the "smoke" label from the pull request that triggered this workflow
5. Add a comment to the pull request confirming the smoke test completed

Keep your responses concise and focused on validating that the copilot-sdk engine is working correctly.
