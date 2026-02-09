---
description: Smoke Copilot
on: 
  schedule: every 12h
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
name: Smoke Copilot
engine: copilot
imports:
  - shared/gh.md
  - shared/reporting.md
  - shared/github-queries-safe-input.md
network:
  allowed:
    - defaults
    - node
    - github
    - playwright
tools:
  agentic-workflows:
  cache-memory: true
  edit:
  bash:
    - "*"
  github:
  playwright:
    allowed_domains:
      - github.com
  serena:
    languages:
      go: {}
  web-fetch:
runtimes:
  go:
    version: "1.25"
sandbox:
  mcp:
    container: "ghcr.io/github/gh-aw-mcpg"
safe-outputs:
    add-comment:
      hide-older-comments: true
      max: 2
    create-issue:
      expires: 2h
      group: true
      close-older-issues: true
    add-labels:
      allowed: [smoke-copilot]
    remove-labels:
      allowed: [smoke]
    dispatch-workflow:
      workflows:
        - haiku-printer
      max: 1
    jobs:
      send-slack-message:
        description: "Send a message to Slack (stub for testing)"
        runs-on: ubuntu-latest
        output: "Slack message stub executed!"
        inputs:
          message:
            description: "The message to send"
            required: true
            type: string
        permissions:
          contents: read
        steps:
          - name: Stub Slack message
            run: |
              echo "🎭 This is a stub - not sending to Slack"
              if [ -f "$GH_AW_AGENT_OUTPUT" ]; then
                MESSAGE=$(cat "$GH_AW_AGENT_OUTPUT" | jq -r '.items[] | select(.type == "send_slack_message") | .message')
                echo "Would send to Slack: $MESSAGE"
                {
                  echo "### 📨 Slack Message Stub"
                  echo "**Message:** $MESSAGE"
                  echo ""
                  echo "> ℹ️ This is a stub for testing purposes. No actual Slack message is sent."
                } >> "$GITHUB_STEP_SUMMARY"
              else
                echo "No agent output found"
              fi
    messages:
      append-only-comments: true
      footer: "> 📰 *BREAKING: Report filed by [{workflow_name}]({run_url})*"
      run-started: "📰 BREAKING: [{workflow_name}]({run_url}) is now investigating this {event_type}. Sources say the story is developing..."
      run-success: "📰 VERDICT: [{workflow_name}]({run_url}) has concluded. All systems operational. This is a developing story. 🎤"
      run-failure: "📰 DEVELOPING STORY: [{workflow_name}]({run_url}) reports {status}. Our correspondents are investigating the incident..."
timeout-minutes: 15
strict: true
---

# Smoke Test: Copilot Engine Validation

**IMPORTANT: Keep all outputs extremely short and concise. Use single-line responses where possible. No verbose explanations.**

## Test Requirements

1. **GitHub MCP Testing**: Review the last 2 merged pull requests in ${{ github.repository }}
2. **Safe Inputs GH CLI Testing**: Use the `safeinputs-gh` tool to query 2 pull requests from ${{ github.repository }} (use args: "pr list --repo ${{ github.repository }} --limit 2 --json number,title,author")
3. **Serena MCP Testing**: 
   - Use the Serena MCP server tool `activate_project` to initialize the workspace at `${{ github.workspace }}` and verify it succeeds (do NOT use bash to run go commands - use Serena's MCP tools)
   - After initialization, use the `find_symbol` tool to search for symbols (find which tool to call) and verify that at least 3 symbols are found in the results
4. **Playwright Testing**: Use the playwright tools to navigate to <https://github.com> and verify the page title contains "GitHub" (do NOT try to install playwright - use the provided MCP tools)
5. **File Writing Testing**: Create a test file `/tmp/gh-aw/agent/smoke-test-copilot-${{ github.run_id }}.txt` with content "Smoke test passed for Copilot at $(date)" (create the directory if it doesn't exist)
6. **Bash Tool Testing**: Execute bash commands to verify file creation was successful (use `cat` to read the file back)
7. **Discussion Interaction Testing**: 
   - Use the `github-discussion-query` safe-input tool with params: `limit=1, jq=".[0]"` to get the latest discussion from ${{ github.repository }}
   - Extract the discussion number from the result (e.g., if the result is `{"number": 123, "title": "...", ...}`, extract 123)
   - Use the `add_comment` tool with `discussion_number: <extracted_number>` to add a fun, playful comment stating that the smoke test agent was here
8. **Build gh-aw**: Run `GOCACHE=/tmp/go-cache GOMODCACHE=/tmp/go-mod make build` to verify the agent can successfully build the gh-aw project (both caches must be set to /tmp because the default cache locations are not writable). If the command fails, mark this test as ❌ and report the failure.
9. **Workflow Dispatch Testing**: Use the `dispatch_workflow` safe output tool to trigger the `haiku-printer` workflow with a haiku as the message input. Create an original, creative haiku about software testing or automation.

## Output

1. **Create an issue** with a summary of the smoke test run:
   - Title: "Smoke Test: Copilot - ${{ github.run_id }}"
   - Body should include:
     - Test results (✅ or ❌ for each test)
     - Overall status: PASS or FAIL
     - Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}
     - Timestamp
     - Pull request author and assignees

2. Add a **very brief** comment (max 5-10 lines) to the current pull request with:
   - PR titles only (no descriptions)
   - ✅ or ❌ for each test result
   - Overall status: PASS or FAIL
   - Mention the pull request author and any assignees

3. Use the `add_comment` tool to add a **fun and creative comment** to the latest discussion (using the `discussion_number` you extracted in step 7) - be playful and entertaining in your comment

4. Use the `send_slack_message` tool to send a brief summary message (e.g., "Smoke test ${{ github.run_id }}: All tests passed! ✅")

If all tests pass:
- Use the `add_labels` safe-output tool to add the label `smoke-copilot` to the pull request
- Use the `remove_labels` safe-output tool to remove the label `smoke` from the pull request
