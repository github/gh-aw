---
name: Test Create PR Error Handling
description: Test workflow to verify create_pull_request error handling
on:
  workflow_dispatch:

permissions:
  contents: read
  issues: read
  pull-requests: read

engine: claude
strict: true
timeout-minutes: 5

safe-outputs:
  create-pull-request:
    expires: 2d
    labels: [test]

tools:
  cache-memory: true

imports:
  - shared/mood.md
---

# Test Create PR Error Handling

This workflow tests the error handling for the `create_pull_request` safe-output tool.

## Task

Try to create a pull request WITHOUT making any commits. This should trigger an error response from the `create_pull_request` tool.

Expected behavior:
- The tool should return an error response with a clear message
- The error message should explain that no commits were found
- The agent should NOT report this as a "missing_tool"

## Steps

1. Check the current git status to confirm no changes are staged
2. Try to call the `create_pull_request` tool
3. Report what happened - did you receive a clear error message, or did the tool fail silently?

Please call the `create_pull_request` tool with:
- title: "Test PR"
- body: "This is a test PR that should fail due to no commits"

Then report the exact error message you received.
