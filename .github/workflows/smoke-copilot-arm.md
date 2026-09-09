---
private: true
emoji: "🧪"
description: Smoke Copilot ARM64
on:
  schedule: every 2 days
  slash_command:
    name: smoke-copilot-arm
    strategy: centralized
    events: [issues, issue_comment, pull_request, pull_request_comment]
  workflow_dispatch:
  pull_request:
    types: [labeled]
    names: ["water"]
  reaction: "eyes"
  status-comment: true
permissions:
  contents: read
  pull-requests: read
  issues: read
  discussions: read
  actions: read
name: Smoke Copilot ARM64
engine: copilot
runs-on: ubuntu-24.04-arm
imports:
  - shared/gh.md
  - shared/reporting-otlp.md
  - shared/github-queries-mcp-script.md
  - shared/mcp/serena-go.md
  - shared/otlp.md
  - shared/token-telemetry-check.md
  - shared/smoke-test-brevity.md
network:
  allowed:
    - defaults
    - node
    - github
    - playwright
tools:
  cli-proxy: true
  agentic-workflows:
  cache-memory: true
  edit:
  bash:
    - "*"
  github:
  playwright:
  web-fetch:
runtimes:
  go:
    version: "1.26"
safe-outputs:
    allowed-domains: [default-safe-outputs]
    add-comment:
      allowed-repos: ["github/gh-aw"]
      hide-older-comments: true
      max: 2
    create-issue:
      expires: 2h
      group: true
      close-older-issues: true
      close-older-key: "smoke-copilot-arm"
      labels: [automation, testing]
    create-discussion:
      category: announcements
      labels: [ai-generated]
      expires: 2h
      close-older-discussions: true
      max: 1
    create-pull-request-review-comment:
      max: 5
    submit-pull-request-review:
    add-labels:
      allowed: [smoke-copilot-arm]
      allowed-repos: ["github/gh-aw"]
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
            required: false
            default: ""
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
      footer: "> 📰 *BREAKING: Report filed by [{workflow_name}]({run_url})*{ai_credits_suffix}{history_link}"
      run-started: "[{workflow_name}]({run_url}) ARM64 smoke test started for this {event_type}."
      run-success: "[{workflow_name}]({run_url}) ARM64 smoke test completed successfully."
      run-failure: "[{workflow_name}]({run_url}) ARM64 smoke test {status}. Check the logs for details."
timeout-minutes: 15
features:
  gh-aw-detection: false
sandbox:
  agent:
    id: awf
---

# Smoke Test: Copilot Engine Validation (ARM64)

**PURPOSE**: This smoke test validates that the Copilot engine, AWF firewall, MCP servers, and safe outputs work correctly on Linux ARM64 (ubuntu-24.04-arm) runners. This is critical for ensuring multi-architecture support.

## Test Requirements

1. **Architecture Verification**: Run `uname -m` to confirm you are running on an ARM64 (aarch64) host. Report the architecture.
2. **GitHub MCP Testing**: Review the last 2 merged pull requests in ${{ github.repository }}
3. **MCP Scripts GH CLI Testing**: Use the `mcpscripts-gh` tool to query 2 pull requests from ${{ github.repository }} (use args: "pr list --repo ${{ github.repository }} --limit 2 --json number,title,author")
4. **Serena MCP Testing**:
   - Use the Serena MCP server tool `activate_project` to initialize the workspace at `${{ github.workspace }}` and verify it succeeds (do NOT use bash to run go commands - use Serena's MCP tools)
   - After initialization, use the `find_symbol` tool to search for symbols (find which tool to call) and verify that at least 3 symbols are found in the results
5. **Playwright Testing**: Use playwright-cli to navigate to <https://github.com> and verify the page title contains "GitHub": run `playwright-cli browser_navigate --url https://github.com` then `playwright-cli browser_snapshot` in bash
6. **File Writing Testing**: Create a test file `/tmp/gh-aw/agent/smoke-test-copilot-arm-${{ github.run_id }}.txt` with content "Smoke test passed for Copilot ARM64 at $(date)" (create the directory if it doesn't exist)
7. **Bash Tool Testing**: Execute bash commands to verify file creation was successful (use `cat` to read the file back)
8. **Discussion Interaction Testing**:
   - Use the `github-discussion-query` mcp-script tool with params: `limit=1, jq=".[0]"` to get the latest discussion from ${{ github.repository }}
   - Extract the discussion number from the result (e.g., if the result is `{"number": 123, "title": "...", ...}`, extract 123)
   - Use the `add_comment` tool with `discussion_number: <extracted_number>` to add a fun, playful comment stating that the ARM64 smoke test agent was here
9. **Build gh-aw**: Run `GOCACHE=/tmp/gh-aw/agent/go-cache GOMODCACHE=/tmp/gh-aw/agent/go-mod make build` to verify the agent can successfully build the gh-aw project on ARM64 (both caches must be set under `/tmp/gh-aw/agent` because the default cache locations are not writable). If the command fails, mark this test as ❌ and report the failure.
10. **Discussion Creation Testing**: Use the `create_discussion` safe-output tool to create a discussion in the announcements category titled "copilot-arm64 was here" with the label "ai-generated" and `temporary_id: "aw_discuss"` so you can reference it in the Output section.
11. **Workflow Dispatch Testing**: Use the `dispatch_workflow` safe output tool to trigger the `haiku-printer` workflow with a haiku as the message input. Create an original, creative haiku about ARM64 or multi-architecture computing.
12. **PR Review Testing**: Review the diff of the current pull request. Leave 1-2 inline `create_pull_request_review_comment` comments on specific lines, then call `submit_pull_request_review` with a brief body summarizing your review and event `COMMENT`.

## Output

1. **Create an issue** with a summary of the smoke test run:
   - Title: "Smoke Test: Copilot ARM64 - ${{ github.run_id }}"
   - Body should include:
     - Host architecture (aarch64)
     - Test results (✅ or ❌ for each test)
     - Overall status: PASS or FAIL
     - Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}
     - Timestamp
     - Pull request author and assignees

2. Add a **very brief** comment (max 5-10 lines) to the current pull request with:
   - Architecture confirmation (ARM64/aarch64)
   - PR titles only (no descriptions)
   - ✅ or ❌ for each test result
   - Overall status: PASS or FAIL
   - Mention the pull request author and any assignees

3. Use the `add_comment` tool to add a **fun and creative comment** to the newly created discussion (use `item_number: "aw_discuss"` from step 10) - be playful and entertaining in your comment

4. Use the `send_slack_message` tool to send a brief summary message (e.g., "ARM64 smoke test ${{ github.run_id }}: All tests passed! ✅")

If all tests pass:
- Use the `add_labels` safe-output tool to add the label `smoke-copilot-arm` to the pull request
- Use the `remove_labels` safe-output tool to remove the label `smoke` from the pull request