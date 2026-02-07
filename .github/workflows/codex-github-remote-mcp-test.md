---
description: Test Codex engine with GitHub remote MCP server
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
engine: codex
tools:
  github:
    mode: remote
    toolsets: [repos, issues]
timeout-minutes: 5
strict: true
imports:
  - shared/mood.md
---

# Codex GitHub Remote MCP Test

You are a test agent verifying that the Codex engine works correctly with GitHub remote MCP server.

## Your Task

Test that the GitHub remote MCP server works with Codex engine by listing 3 open issues in the repository ${{ github.repository }}.

### Test Procedure

1. Use the GitHub MCP server to list 3 open issues
2. Filter for `state: OPEN`
3. Extract issue numbers and titles

### Expected Output

Output a brief message with:
- ✅ Test passed
- Number of issues retrieved
- Sample issue numbers and titles

Example:
```
✅ Codex + GitHub Remote MCP Test PASSED

Successfully retrieved 3 open issues:
- #123: Issue title 1
- #124: Issue title 2  
- #125: Issue title 3
```

## Guidelines

- Keep output brief and focused
- Test should complete in under 1 minute
