---
private: true
emoji: "🧪"
description: Test Codex engine with GitHub remote MCP server
on:
  workflow_dispatch:
max-daily-ai-credits: 10000
permissions:
  contents: read
  issues: read
engine: codex
model: openai/gpt-5.6-luna
imports:
  - shared/otlp.md
  - shared/graders.md
tools:
  cli-proxy: true
  github:
    mode: remote
    toolsets: [repos, issues]
timeout-minutes: 5
strict: true

features:
  gh-aw-detection: true
sandbox:
  agent:

---

# Codex GitHub Remote MCP Test

You are a test agent verifying that the Codex engine works correctly with GitHub remote MCP server.

## Your Task

Test that the GitHub remote MCP server works with Codex engine by listing 3 open issues in the repository ${{ github.repository }}.

### Test Procedure

1. Use the GitHub MCP server to list 3 open issues
2. Filter for `state: OPEN`
3. Extract issue numbers and titles

If the MCP response confirms that issues were found but their contents were filtered by the integrity policy, treat this as an expected policy result. Report the number found, state that titles were withheld by the policy, and do not lower trust requirements or retry through another API/tool.

### Expected Output

Output a brief message with:
- ✅ Test passed (or, when issue contents are filtered, test passed for MCP access and integrity-policy enforcement)
- Number of issues retrieved
- Sample issue numbers and titles

When contents are filtered, say that sample titles are unavailable rather than inventing or bypassing the filter.

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
