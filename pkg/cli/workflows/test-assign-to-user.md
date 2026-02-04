---
name: Test Assign to User
description: Test workflow for assign_to_user safe output feature
on:
  issues:
    types: [labeled]
  workflow_dispatch:
    inputs:
      issue_number:
        description: 'Issue number to test with'
        required: true
        type: string
      assignee:
        description: 'GitHub username to assign'
        required: true
        type: string

permissions:
  actions: write
  contents: read
  issues: read

engine: copilot
timeout-minutes: 5

safe-outputs:
  assign-to-user:
    max: 5
features:
  dangerous-permissions-write: true
---

# Assign to User Test Workflow

This workflow tests the `assign_to_user` safe output feature, which allows AI agents to assign GitHub users to issues.

## Task

**For workflow_dispatch:**
Assign user `${{ github.event.inputs.assignee }}` to issue #${{ github.event.inputs.issue_number }} using the `assign_to_user` tool from the `safeoutputs` MCP server.

Do not use GitHub tools. The assign_to_user tool will handle the actual assignment.
