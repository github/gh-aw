---
private: true
emoji: "🧪"
description: Smoke test workflow that validates Codex engine functionality by reviewing recent PRs twice daily
on:
  schedule: every 2 days
  restore-memory: true
  slash_command:
    name: smoke-codex
    strategy: centralized
    events: [issues, issue_comment, pull_request, pull_request_comment]
  workflow_dispatch:
    inputs:
      aw_context:
        description: Agent caller context (used internally by Agentic Workflows).
        required: false
        type: string
        default: ""
  pull_request:
    types: [labeled]
    names: ["smoke"]
  reaction: "hooray"
  status-comment: true
permissions:
  copilot-requests: write
  contents: read
  issues: read
  pull-requests: read
name: Smoke Codex
engine: codex
model: copilot/gpt-5.3-codex
imports:
  - shared/gh.md
  - shared/reporting-otlp.md
  - shared/mcp/serena-go.md
  - shared/trufflehog.md
  - shared/otlp.md
  - shared/token-telemetry-check.md
  - shared/smoke-test-brevity.md
  - shared/playwright-title-test.md
network:
  allowed:
    - defaults
    - github
    - playwright
tools:
  cli-proxy: true
  cache-memory: true
  comment-memory: true
  github:
    mode: gh-proxy
  playwright:
  edit:
  bash:
    - "*"
  web-fetch:
runtimes:
  go:
    version: "1.26"
safe-outputs:
    allowed-domains: [default-safe-outputs]
    add-comment:
      hide-older-comments: true
      max: 2
    create-issue:
      expires: 2h
      close-older-issues: true
      close-older-key: "smoke-codex"
      labels: [automation, testing]
    set-issue-field:
      max: 1
      allowed-fields: ["*"]
    add-labels:
      allowed: [smoke-codex]
    remove-labels:
      allowed: [smoke]
    unassign-from-user:
      allowed: [githubactionagent]
      max: 1
    hide-comment:
    messages:
      footer: "> 🔮 *The oracle has spoken through [{workflow_name}]({run_url})*{ai_credits_suffix}{history_link}"
      run-started: "🔮 The ancient spirits stir... [{workflow_name}]({run_url}) awakens to divine this {event_type}..."
      run-success: "✨ The prophecy is fulfilled... [{workflow_name}]({run_url}) has completed its mystical journey. The stars align. 🌟"
      run-failure: "🌑 The shadows whisper... [{workflow_name}]({run_url}) {status}. The oracle requires further meditation..."
    actions:
      add-smoked-label:
        # zizmor: ignore[github_action_from_unverified_creator_used]
        uses: actions-ecosystem/action-add-labels@v1.1.3
        description: Add the 'smoked' label to the current pull request
        env:
          GITHUB_TOKEN: ${{ github.token }}
timeout-minutes: 15
checkout:
  - fetch-depth: 2
    current: true
features:
  gh-aw-detection: false
sandbox:
  agent:
    id: awf
---

# Smoke Test: Codex Engine Validation

## Test Requirements

1. **GitHub MCP Testing**: Use GitHub MCP tools to fetch details of exactly 2 merged pull requests from ${{ github.repository }} (title and number only, no descriptions)
2. **Serena MCP Testing**: 
   - Use the Serena MCP server tool `activate_project` to initialize the workspace at `${{ github.workspace }}` and verify it succeeds (do NOT use bash to run go commands)
   - After initialization, use the `find_symbol` tool to search for symbols and verify that at least 3 symbols are found in the results
3. **Playwright CLI Testing**: Run `playwright-cli open https://github.com`, then run `playwright-cli snapshot` and verify the page title contains "GitHub".
4. **Web Fetch Testing**: Use the web-fetch MCP tool to fetch https://github.com and verify the response contains "GitHub" (do NOT use bash or playwright for this test - use the web-fetch MCP tool directly)
5. **File Writing Testing**: Create a test file `/tmp/gh-aw/agent/smoke-test-codex-${{ github.run_id }}.txt` with content "Smoke test passed for Codex at $(date)" (create the directory if it doesn't exist)
6. **Bash Tool Testing**: Execute bash commands to verify file creation was successful (use `cat` to read the file back)
7. **Build gh-aw**: Run `GOCACHE=/tmp/gh-aw/agent/go-cache GOMODCACHE=/tmp/gh-aw/agent/go-mod make build` to verify the agent can successfully build the gh-aw project (both caches must be set under `/tmp/gh-aw/agent` because the default cache locations are not writable). If the command fails, mark this test as ❌ and report the failure.
8. **Comment Memory Testing**: Append an original 3-line haiku to the comment-memory markdown file(s) in `/tmp/gh-aw/comment-memory/*.md` without removing existing content.
9. **Cache Memory Testing**:
    - Check if `/tmp/gh-aw/cache-memory/smoke-codex-history.json` exists; if it does, read it and note the previous run's results (run ID, timestamp, status)
    - Write current run results to `/tmp/gh-aw/cache-memory/smoke-codex-history.json` with content: `{"run_id": "${{ github.run_id }}", "timestamp": "<current UTC timestamp in ISO 8601 format: YYYY-MM-DDTHH:MM:SSZ>", "status": "PASS or FAIL", "tests_passed": <count>, "tests_failed": <count>}` (create the parent directory if it doesn't exist)
    - Use bash to verify the file was written successfully (use `cat` to read it back)
10. **Set Issue Field Testing**:
    - After creating the smoke-test issue, use `set_issue_field` exactly once on that new issue
    - Reference the created issue using the `temporary_id` declared on the `create_issue` output: set `issue_number: '#aw_smoke_issue'` in the `set_issue_field` message
    - Discover available issue fields and choose one compatible field/value pair:
      - text field → short text value
      - number field → numeric value
      - date field → `YYYY-MM-DD`
      - single-select field → an existing option name
    - If no editable issue fields are available, report this test as skipped with reason

## Output

**ALWAYS create an issue** with a summary of the smoke test run:
- Title: "Smoke Test: Codex - ${{ github.run_id }}"
- Declare `temporary_id: aw_smoke_issue` on this `create_issue` output so that the `set_issue_field` message (test #10) can reference it via `issue_number: '#aw_smoke_issue'`
- Body should include:
  - Test results (✅ or ❌ for each test, including test #9 Cache Memory and test #10 Set Issue Field)
  - Overall status: PASS or FAIL
  - Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}
  - Timestamp

**Only if this workflow was triggered by a `pull_request` event**: Additionally use the `add_comment` safe-output tool to add a **very brief** comment (max 5-10 lines) to the triggering pull request, specifying `item_number: ${{ github.event.pull_request.number }}` (use this exact number — do NOT search GitHub for a PR):
  - PR titles only (no descriptions)
  - ✅ or ❌ for each test result
  - Overall status: PASS or FAIL

If all tests pass and this workflow was triggered by a `pull_request` event:
- Use the `add_labels` safe-output tool to add the label `smoke-codex` to the pull request (use `item_number: ${{ github.event.pull_request.number }}`)
- Use the `remove_labels` safe-output tool to remove the label `smoke` from the pull request (use `item_number: ${{ github.event.pull_request.number }}`)
- Use the `unassign_from_user` safe-output tool to unassign the user `githubactionagent` from the pull request (this is a fictitious user used for testing; use `item_number: ${{ github.event.pull_request.number }}`)
- Use the `add_smoked_label` safe-output action tool to add the label `smoked` to the pull request (call it with `{"labels": "smoked", "number": "${{ github.event.pull_request.number }}"}`)