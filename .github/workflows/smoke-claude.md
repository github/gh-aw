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
  max-turns: 100
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
      if-no-changes: "warn"
    add-reviewer:
      max: 2
      target: "*"
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

## PR Review Safe Outputs Testing

**IMPORTANT**: The following tests require an open pull request. First, use the GitHub MCP tool to find an open PR in ${{ github.repository }} (or use the triggering PR if this is a pull_request event). Store the PR number for use in subsequent tests.

11. **Update PR Testing**: Use the `update_pull_request` tool to update the PR's body by appending a test message: "✨ PR Review Safe Output Test - Run ${{ github.run_id }}"
    - Use `pr_number: <pr_number>` to target the open PR
    - Use `operation: "append"` and `body: "\n\n---\n✨ PR Review Safe Output Test - Run ${{ github.run_id }}"`
    - Verify the tool call succeeds

12. **PR Review Comment Testing**: Use the `create_pull_request_review_comment` tool to add review comments on the PR
    - Find a file in the PR's diff (use GitHub MCP to get PR files)
    - Add at least 2 review comments on different lines with constructive feedback
    - Use `pr_number: <pr_number>`, `path: "<file_path>"`, `line: <line_number>`, and `body: "<comment_text>"`
    - Verify the tool calls succeed

13. **Submit PR Review Testing**: Use the `submit_pull_request_review` tool to submit a consolidated review
    - Use `pr_number: <pr_number>`, `event: "COMMENT"`, and `body: "💥 Automated smoke test review - all systems nominal!"`
    - Verify the review is submitted successfully
    - Note: This will bundle all review comments from test #12

14. **Resolve Review Thread Testing**: 
    - Use the GitHub MCP tool to list review threads on the PR
    - If any threads exist, use the `resolve_pull_request_review_thread` tool to resolve one thread
    - Use `thread_id: "<thread_id>"` from an existing thread
    - If no threads exist, mark this test as ⚠️ (skipped - no threads to resolve)

15. **Add Reviewer Testing**: Use the `add_reviewer` tool to add a reviewer to the PR
    - Use `pr_number: <pr_number>` and `reviewers: ["copilot"]` (or another valid reviewer)
    - Verify the tool call succeeds
    - Note: May fail if reviewer is already assigned or doesn't have access

16. **Push to PR Branch Testing**: 
    - Create a test file at `/tmp/test-pr-push-${{ github.run_id }}.txt` with content "Test file for PR push"
    - Use git commands to check if we're on the PR branch
    - Use the `push_to_pull_request_branch` tool to push this change
    - Use `pr_number: <pr_number>` and `commit_message: "test: Add smoke test file"`
    - Verify the push succeeds
    - Note: This test may be skipped if not on a PR branch or if the PR is from a fork

17. **Close PR Testing** (CONDITIONAL - only if a test PR exists):
    - If you can identify a test/bot PR that can be safely closed, use the `close_pull_request` tool
    - Use `pr_number: <test_pr_number>` and `comment: "Closing as part of smoke test - Run ${{ github.run_id }}"`
    - If no suitable test PR exists, mark this test as ⚠️ (skipped - no safe PR to close)
    - **DO NOT close the triggering PR or any important PRs**

## Output

**CRITICAL: You MUST create an issue regardless of test results - this is a required safe output.**

1. **ALWAYS create an issue** with a summary of the smoke test run:
   - Title: "Smoke Test: Claude - ${{ github.run_id }}"
   - Body should include:
     - Test results (✅ for pass, ❌ for fail, ⚠️ for skipped) for each test (including PR review tests #11-17)
     - Overall status: PASS (all passed), PARTIAL (some skipped), or FAIL (any failed)
     - Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}
     - Timestamp
     - Note which PR was used for PR review testing (if applicable)
   - If ANY test fails, include error details in the issue body
   - This issue MUST be created before any other safe output operations

2. **Only if this workflow was triggered by a pull_request event**: Use the `add_comment` tool to add a **very brief** comment (max 5-10 lines) to the triggering pull request (omit the `item_number` parameter to auto-target the triggering PR) with:
   - Test results for core tests #1-10 (✅ or ❌)
   - Test results for PR review tests #11-17 (✅, ❌, or ⚠️)
   - Overall status: PASS, PARTIAL, or FAIL

3. Use the `add_comment` tool with `item_number` set to the discussion number you extracted in step 9 to add a **fun comic-book style comment** to that discussion - be playful and use comic-book language like "💥 WHOOSH!"
   - If step 9 failed to extract a discussion number, skip this step

If all non-skipped tests pass, use the `add_labels` tool to add the label `smoke-claude` to the pull request (omit the `item_number` parameter to auto-target the triggering PR if this workflow was triggered by a pull_request event).
