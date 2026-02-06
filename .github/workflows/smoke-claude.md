---
description: Smoke test workflow that validates Claude engine functionality by reviewing recent PRs twice daily
on: 
  schedule: every 12h
  workflow_dispatch:
  pull_request:
    types: [labeled]
    names: ["smoke"]
  reaction: "heart"
permissions:
  contents: read
  issues: read
  pull-requests: read
  discussions: read
  actions: read
  
name: Smoke Claude
engine:
  id: claude
  max-turns: 15
strict: true
imports:
  - shared/mcp-pagination.md
  - shared/gh.md
  - shared/mcp/tavily.md
  - shared/reporting.md
  - shared/github-queries-safe-input.md
  - shared/go-make.md
  - shared/github-mcp-app.md
network:
  allowed:
    - defaults
    - github
    - playwright
sandbox:
  mcp:
    container: "ghcr.io/github/gh-aw-mcpg"
tools:
  agentic-workflows:
  cache-memory: true
  github:
    toolsets: [repos, pull_requests]
  playwright:
    allowed_domains:
      - github.com
  edit:
  bash:
    - "*"
  serena:
    languages:
      go: {}
runtimes:
  go:
    version: "1.25"
safe-outputs:
    add-comment:
      hide-older-comments: true
      max: 2
    create-issue:
      expires: 2h
      group: true
      close-older-issues: true
    add-labels:
      allowed: [smoke-claude]
    messages:
      footer: "> 💥 *[THE END] — Illustrated by [{workflow_name}]({run_url})*"
      run-started: "💥 **WHOOSH!** [{workflow_name}]({run_url}) springs into action on this {event_type}! *[Panel 1 begins...]*"
      run-success: "🎬 **THE END** — [{workflow_name}]({run_url}) **MISSION: ACCOMPLISHED!** The hero saves the day! ✨"
      run-failure: "💫 **TO BE CONTINUED...** [{workflow_name}]({run_url}) {status}! Our hero faces unexpected challenges..."
timeout-minutes: 10
---

# Smoke Test: Claude Engine Validation.

**IMPORTANT: Keep all outputs extremely short and concise. Use single-line responses where possible. No verbose explanations.**

## Test Requirements

1. **GitHub MCP Testing**: Review the last 2 merged pull requests in ${{ github.repository }}
2. **Safe Inputs GH CLI Testing**: Use the `safeinputs-gh` tool to query 2 pull requests from ${{ github.repository }} (use args: "pr list --repo ${{ github.repository }} --limit 2 --json number,title,author")
3. **Serena MCP Testing**: 
   - Use the Serena MCP server tool `activate_project` to initialize the workspace at `${{ github.workspace }}` and verify it succeeds (do NOT use bash to run go commands - use Serena's MCP tools or the safeinputs-go/safeinputs-make tools from the go-make shared workflow)
   - After initialization, use the `find_symbol` tool to search for symbols (find which tool to call) and verify that at least 3 symbols are found in the results
4. **Make Build Testing**: Use the `safeinputs-make` tool to build the project (use args: "build") and verify it succeeds
5. **Playwright Testing**: Use the playwright tools to navigate to https://github.com and verify the page title contains "GitHub" (do NOT try to install playwright - use the provided MCP tools)
6. **Tavily Web Search Testing**: Use the Tavily MCP server to perform a web search for "GitHub Agentic Workflows" and verify that results are returned with at least one item
7. **File Writing Testing**: Create a test file `/tmp/gh-aw/agent/smoke-test-claude-${{ github.run_id }}.txt` with content "Smoke test passed for Claude at $(date)" (create the directory if it doesn't exist)
8. **Bash Tool Testing**: Execute bash commands to verify file creation was successful (use `cat` to read the file back)
9. **Discussion Interaction Testing**: 
   - Use the `github-discussion-query` safe-input tool with params: `limit=1, jq=".[0]"` to get the latest discussion from ${{ github.repository }}
   - Extract the discussion number from the result (e.g., if the result is `{"number": 123, "title": "...", ...}`, extract 123)
   - Use the `add_comment` tool with `discussion_number: <extracted_number>` to add a fun, comic-book style comment stating that the smoke test agent was here
10. **Agentic Workflows MCP Testing**: 
   - Call the `agentic-workflows` MCP tool using the `status` method with workflow name `smoke-claude` to query workflow status
   - If the tool returns an error or no results, mark this test as ❌ and note "Tool unavailable or workflow not found" but continue to the Output section
   - If the tool succeeds, extract key information from the response: total runs, success/failure counts, last run timestamp
   - Write a summary of the results to `/tmp/gh-aw/agent/smoke-claude-status-${{ github.run_id }}.txt` (create directory if needed)
   - Use bash to verify the file was created and display its contents

## Output

**CRITICAL: You MUST create an issue regardless of test results - this is a required safe output.**

1. **ALWAYS create an issue** with a summary of the smoke test run:
   - Title: "Smoke Test: Claude - ${{ github.run_id }}"
   - Body should include:
     - Test results (✅ or ❌ for each test)
     - Overall status: PASS or FAIL
     - Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}
     - Timestamp
   - If ANY test fails, include error details in the issue body
   - This issue MUST be created before any other safe output operations

2. **Only if this workflow was triggered by a pull_request event**: Use the `add_comment` tool to add a **very brief** comment (max 5-10 lines) to the triggering pull request (omit the `item_number` parameter to auto-target the triggering PR) with:
   - PR titles only (no descriptions)
   - ✅ or ❌ for each test result
   - Overall status: PASS or FAIL

3. Use the `add_comment` tool with `item_number` set to the discussion number you extracted in step 9 to add a **fun comic-book style comment** to that discussion - be playful and use comic-book language like "💥 WHOOSH!"
   - If step 9 failed to extract a discussion number, skip this step

If all tests pass, use the `add_labels` tool to add the label `smoke-claude` to the pull request (omit the `item_number` parameter to auto-target the triggering PR if this workflow was triggered by a pull_request event).
