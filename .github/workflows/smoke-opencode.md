---
description: Smoke test workflow that validates OpenCode custom engine functionality daily
on: 
  schedule: daily
  workflow_dispatch:
  pull_request:
    types: [labeled]
    names: ["smoke"]
  reaction: "rocket"
permissions:
  contents: read
  issues: read
  pull-requests: read
  discussions: read
  
name: Smoke OpenCode
imports:
  - shared/opencode.md
  - shared/gh.md
  - shared/github-queries-safe-input.md
strict: true
sandbox:
  mcp:
    container: "ghcr.io/github/gh-aw-mcpg"
tools:
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
      allowed: [smoke-opencode]
    messages:
      footer: "> 🚀 *[Liftoff Complete] — Powered by [{workflow_name}]({run_url})*"
      run-started: "🚀 **IGNITION!** [{workflow_name}]({run_url}) launching for this {event_type}! *[T-minus counting...]*"
      run-success: "🎯 **MISSION SUCCESS** — [{workflow_name}]({run_url}) **TARGET ACQUIRED!** All systems nominal! ✨"
      run-failure: "⚠️ **MISSION ABORT...** [{workflow_name}]({run_url}) {status}! Houston, we have a problem..."
timeout-minutes: 15
---

# Smoke Test: OpenCode Custom Engine Validation

**IMPORTANT: Keep all outputs extremely short and concise. Use single-line responses where possible. No verbose explanations.**

## Test Requirements

1. **GitHub MCP Testing**: Review the last 2 merged pull requests in ${{ github.repository }}
2. **Safe Inputs GH CLI Testing**: Use the `safeinputs-gh` tool to query 2 pull requests from ${{ github.repository }} (use args: "pr list --repo ${{ github.repository }} --limit 2 --json number,title,author")
3. **Serena MCP Testing**: 
   - Use the Serena MCP server tool `activate_project` to initialize the workspace at `${{ github.workspace }}` and verify it succeeds (do NOT use bash to run go commands - use Serena's MCP tools)
   - After initialization, use the `find_symbol` tool to search for symbols (find which tool to call) and verify that at least 3 symbols are found in the results
4. **Playwright Testing**: Use the playwright tools to navigate to https://github.com and verify the page title contains "GitHub" (do NOT try to install playwright - use the provided MCP tools)
5. **File Writing Testing**: Create a test file `/tmp/gh-aw/agent/smoke-test-opencode-${{ github.run_id }}.txt` with content "Smoke test passed for OpenCode at $(date)" (create the directory if it doesn't exist)
6. **Bash Tool Testing**: Execute bash commands to verify file creation was successful (use `cat` to read the file back)
7. **Discussion Interaction Testing**: 
   - Use the `github-discussion-query` safe-input tool with params: `limit=1, jq=".[0]"` to get the latest discussion from ${{ github.repository }}
   - Extract the discussion number from the result (e.g., if the result is `{"number": 123, "title": "...", ...}`, extract 123)
   - Use the `add_comment` tool with `discussion_number: <extracted_number>` to add a space/rocket-themed comment stating that the smoke test agent was here
8. **Build gh-aw**: Run `GOCACHE=/tmp/go-cache GOMODCACHE=/tmp/go-mod make build` to verify the agent can successfully build the gh-aw project (both caches must be set to /tmp because the default cache locations are not writable). If the command fails, mark this test as ❌ and report the failure.

## Output

1. **Create an issue** with a summary of the smoke test run:
   - Title: "Smoke Test: OpenCode - ${{ github.run_id }}"
   - Body should include:
     - Test results (✅ or ❌ for each test)
     - Overall status: PASS or FAIL
     - Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}
     - Timestamp

2. Add a **very brief** comment (max 5-10 lines) to the current pull request with:
   - PR titles only (no descriptions)
   - ✅ or ❌ for each test result
   - Overall status: PASS or FAIL

3. Use the `add_comment` tool to add a **space/rocket-themed comment** to the latest discussion (using the `discussion_number` you extracted in step 7) - be creative and use space mission language like "🚀 IGNITION!"

If all tests pass, add the label `smoke-opencode` to the pull request.
