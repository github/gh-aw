---
private: true
emoji: "🧪"
description: Smoke test workflow that validates Claude engine functionality by reviewing recent PRs twice daily
on:
  schedule: every 2 days
  slash_command:
    name: smoke-claude
    strategy: centralized
    events: [issues, issue_comment, pull_request, pull_request_comment]
  workflow_dispatch:
  pull_request:
    types: [labeled]
    names: ["smoke"]
  reaction: "heart"
  status-comment: true
permissions:
  contents: read
  issues: read
  pull-requests: read
  discussions: read
  actions: read
  
name: Smoke Claude
models:
  blocked: ["*opus*"]
max-turns: 100
model: claude-sonnet-4-6
engine:
  id: claude
  bare: true
inlined-imports: true
imports:
  - shared/gh.md
  - shared/mcp/tavily.md
  - shared/go-make.md
  - shared/github-mcp-app.md
  - shared/otlp.md
  - shared/token-telemetry-check.md
  - shared/smoke-test-brevity.md
  - shared/playwright-title-test.md
  - shared/reporting.md
network:
  allowed:
    - defaults
    - github
    - playwright
    - content-autofill.googleapis.com
    - www.google.com
    - accounts.google.com
    - android.clients.google.com
    - www.gstatic.com
sandbox:
  agent:
    config:
      filesystem:
        allowWrite:
          - /tmp/gh-aw/agent
tools:
  agentic-workflows:
  cli-proxy: true
  cache-memory: true
  github:
    mode: gh-proxy
    toolsets: [repos, pull_requests]
  playwright:
  bash:
    - "*"
runtimes:
  go:
    version: "1.26"
checkout:
  fetch: ["*"]
  fetch-depth: 0
  force-clean-git-credentials: true
safe-outputs:
    allowed-domains: [default-safe-outputs]
    add-comment:
      hide-older-comments: true
      max: 2
    create-issue:
      expires: 2h
      group: true
      close-older-issues: true
      close-older-key: "smoke-claude"
      labels: [automation, testing]
    add-labels:
      allowed: [smoke-claude]
    create-code-scanning-alert:
      driver: "Smoke Claude"
    create-check-run:
      name: "Smoke Claude: Agent Status"
      max: 1
    update-pull-request:
      title: true
      body: true
      max: 1
      target: "*"
    close-pull-request:
      staged: true
      max: 1
    create-pull-request-review-comment:
      max: 5
      side: "RIGHT"
      target: "*"
    submit-pull-request-review:
      max: 1
      footer: true
    resolve-pull-request-review-thread:
      max: 5
    push-to-pull-request-branch:
      staged: true
      target: "*"
      required-labels: [smoke-claude]
      if-no-changes: "warn"
      allowed-files:
        - "smoke-test-files/smoke-claude-push-test.md"
    add-reviewer:
      max: 2
      target: "*"
    messages:
      footer: "> 💥 *[THE END] — Illustrated by [{workflow_name}]({run_url})*{ai_credits_suffix}{history_link}"
      run-started: "💥 **WHOOSH!** [{workflow_name}]({run_url}) springs into action on this {event_type}! *[Panel 1 begins...]*"
      run-success: "🎬 **THE END** — [{workflow_name}]({run_url}) **MISSION: ACCOMPLISHED!** The hero saves the day! ✨"
      run-failure: "💫 **TO BE CONTINUED...** [{workflow_name}]({run_url}) {status}! Our hero faces unexpected challenges..."
    scripts:
      post-slack-message:
        description: Post a message to a fictitious Slack channel (smoke test only — no real Slack integration)
        inputs:
          channel:
            description: Slack channel name to post to
            required: false
            default: "#general"
            type: string
          message:
            description: Message text to post
            required: false
            default: ""
            type: string
        script: |
          const targetChannel = item.channel || "#general";
          const text = item.message || "(no message)";
          core.info(`[FICTITIOUS SLACK] → ${targetChannel}: ${text}`);
          return { success: true, channel: targetChannel, message: text };
timeout-minutes: 10
features:
  gh-aw-detection: false
---

# Smoke Test: Claude Engine Validation.

## Test Requirements

For tests below, mark a test as passed only if the required tool call succeeds.

1. **GitHub MCP Testing**: Review the last 2 merged pull requests in ${{ github.repository }}
2. **GH CLI Testing (via `gh-proxy`)**: Use `bash` to run `gh pr list --repo ${{ github.repository }} --limit 2 --json number,title,author`
3. **Make Build Testing**: Use the `mcpscripts-make` tool to build the project (use args: "build") and verify it succeeds
4. **Playwright Testing**: Use playwright-cli to navigate to https://github.com and verify the page title contains "GitHub": run `playwright-cli browser_navigate --url https://github.com` then `playwright-cli browser_snapshot` in bash
5. **Tavily Web Search Testing**: Use the Tavily MCP server to perform a web search for "GitHub Agentic Workflows" and verify that results are returned with at least one item
6. **File Writing Testing**: Create a test file `/tmp/gh-aw/agent/smoke-test-claude-${{ github.run_id }}.txt` with content "Smoke test passed for Claude at $(date)" (create the directory if it doesn't exist)
7. **Bash Tool Testing**: Execute bash commands to verify file creation was successful (use `cat` to read the file back)
8. **Discussion Interaction Testing**: 
   - Use `gh api repos/${{ github.repository }}/discussions?per_page=1` to get the latest discussion from ${{ github.repository }}
   - Extract the discussion number from the result (e.g., if the result is `{"number": 123, "title": "...", ...}`, extract 123)
   - Use the `add_comment` tool with `discussion_number: <extracted_number>` to add a fun, comic-book style comment stating that the smoke test agent was here
9. **Agentic Workflows MCP Testing**: 
   - Call the `agentic-workflows` MCP tool using the `status` method with workflow name `smoke-claude` to query workflow status
   - If the tool returns an error or no results, mark this test as ❌ and note "Tool unavailable or workflow not found" but continue to the Output section
   - If the tool succeeds, extract key information from the response: total runs, success/failure counts, last run timestamp
   - Write a summary of the results to `/tmp/gh-aw/agent/smoke-claude-status-${{ github.run_id }}.txt` (create directory if needed)
   - Use bash to verify the file was created and display its contents

10. **Slack Script Safe Output Testing**: Use the `post_slack_message` safe-output tool to post a fictitious Slack message:
   - Use `channel: "#smoke-tests"` and `message: "💥 Smoke test ${{ github.run_id }} passed — Claude engine nominal!"`

11. **Code Scanning Alert Safe Output Testing**: Use the `create_code_scanning_alert` safe-output tool to post a dummy warning code scanning alert:
   - Use `level: "warning"`, `message: "Smoke test dummy warning — Run ${{ github.run_id }}"`, `file: "README.md"`, `line: 1`
   - This tests the SARIF artifact upload/download pipeline

12. **Check Run Safe Output Testing**: Use the `create_check_run` safe-output tool to create a check run on the current commit:
   - Use `conclusion: "success"`, `title: "Smoke Claude - Run ${{ github.run_id }}"`, `summary: "All smoke tests completed."`, and `text: "Detailed results attached."`

## PR Review Safe Outputs Testing

**IMPORTANT**: The following tests require an open pull request. First, use the GitHub MCP tool to find an open PR in ${{ github.repository }} (or use the triggering PR if this is a pull_request event). Store the PR number for use in subsequent tests.

13. **Update PR Testing**: Use the `update_pull_request` tool to update the PR's body by appending a test message: "✨ PR Review Safe Output Test - Run ${{ github.run_id }}"
    - Use `pr_number: <pr_number>` to target the open PR
    - Use `operation: "append"` and `body: "\n\n---\n✨ PR Review Safe Output Test - Run ${{ github.run_id }}"`
    - Verify the tool call succeeds

14. **PR Review Comment Testing**: Use the `create_pull_request_review_comment` tool to add review comments on the PR
    - Find a file in the PR's diff (use GitHub MCP to get PR files)
    - Add at least 2 review comments on different lines with constructive feedback
    - Use `pr_number: <pr_number>`, `path: "<file_path>"`, `line: <line_number>`, and `body: "<comment_text>"`
    - Verify the tool calls succeed

15. **Submit PR Review Testing**: Use the `submit_pull_request_review` tool to submit a consolidated review
    - Use `pr_number: <pr_number>`, `event: "COMMENT"`, and `body: "💥 Automated smoke test review - all systems nominal!"`
    - Verify the review is submitted successfully
    - Note: This will bundle all review comments from test #14
    - After submitting, use the GitHub MCP tool to list review threads on the PR and note the thread IDs from review comments you created in test #14 — these will be used in test #16

16. **Resolve Review Thread Testing**: 
    - Use the GitHub MCP tool to list review threads on the PR and filter for threads that are **not yet resolved** (`isResolved: false`)
    - Prefer resolving a thread created by your own review comments from test #14 in this run
    - Use `thread_id: "<thread_id>"` from one of those unresolved threads
    - **IMPORTANT: Only resolve threads that are currently unresolved — attempting to resolve an already-resolved thread will cause an API error**
    - If no unresolved threads exist, mark this test as ⚠️ (skipped - no unresolved threads to resolve)

17. **Add Reviewer Testing**: Use the `add_reviewer` tool to add a reviewer to the PR
    - Use `pr_number: <pr_number>` and `reviewers: ["copilot"]` (or another valid reviewer)
    - Verify the tool call succeeds
    - Note: May fail if reviewer is already assigned or doesn't have access

18. **Push to PR Branch Testing**: 
    - Create a test file at `smoke-test-files/smoke-claude-push-test.md` in the repository workspace with content "Smoke test push — Run ${{ github.run_id }}"
    - Use the `push_to_pull_request_branch` tool to push this change
    - Use `pr_number: <pr_number>` and `commit_message: "test: Add smoke test file"`
    - Verify the push succeeds
    - Note: This test may be skipped if not on a PR branch or if the PR is from a fork

19. **Close PR Testing** (CONDITIONAL - only if a test PR exists):
    - If you can identify a test/bot PR that can be safely closed, use the `close_pull_request` tool
    - Use `pr_number: <test_pr_number>` and `comment: "Closing as part of smoke test - Run ${{ github.run_id }}"`
    - If no suitable test PR exists, mark this test as ⚠️ (skipped - no safe PR to close)
    - **DO NOT close the triggering PR or any important PRs**

## Output

**CRITICAL: You MUST create an issue regardless of test results - this is a required safe output.**

1. **ALWAYS create an issue** with a summary of the smoke test run:
   - Title: "Smoke Test: Claude - ${{ github.run_id }}"
   - Body should include:
     - Test results (✅ for pass, ❌ for fail, ⚠️ for skipped) for each test (including PR review tests #13-19)
     - Overall status: PASS (all passed), PARTIAL (some skipped), or FAIL (any failed)
     - Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}
     - Timestamp
     - Note which PR was used for PR review testing (if applicable)
   - If ANY test fails, include error details in the issue body
   - This issue MUST be created before any other safe output operations

2. **Only if this workflow was triggered by a pull_request event**: Use the `add_comment` tool to add a **very brief** comment (max 5-10 lines) to the triggering pull request (omit the `item_number` parameter to auto-target the triggering PR) with:
   - Test results for core tests #1-12 (✅ or ❌)
   - Test results for PR review tests #13-19 (✅, ❌, or ⚠️)
   - Overall status: PASS, PARTIAL, or FAIL

3. Use the `add_comment` tool with `item_number` set to the discussion number you extracted in step 8 to add a **fun comic-book style comment** to that discussion - be playful and use comic-book language like "💥 WHOOSH!"
   - If step 8 failed to extract a discussion number, skip this step

If all non-skipped tests pass, use the `add_labels` tool to add the label `smoke-claude` to the pull request (omit the `item_number` parameter to auto-target the triggering PR if this workflow was triggered by a pull_request event).