---
private: true
emoji: "🧪"
description: Smoke Copilot - AOAI (apikey)
on:
  schedule: every 2 days
  slash_command:
    name: smoke-copilot-aoai-apikey
    strategy: centralized
    events: [issues, issue_comment, pull_request, pull_request_comment]
  workflow_dispatch:
  label_command:
    name: smoke
    events: [pull_request]
  reaction: "eyes"
  status-comment: true
  github-token: ${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}
permissions:
  contents: read
  pull-requests: read
  issues: read
  discussions: read
  actions: read
name: Smoke Copilot - AOAI (apikey)
model: o4-mini-aw
engine:
  id: copilot
  max-continuations: 2
  bare: true
  env:
    COPILOT_PROVIDER_BASE_URL: ${{ secrets.FOUNDRY_OPENAI_ENDPOINT }}
    COPILOT_PROVIDER_API_KEY: ${{ secrets.FOUNDRY_API_KEY }}
    # o4-mini is a reasoning (o-series) model: the Copilot CLI defaults custom
    # providers to the legacy "completions" wire API, which the Azure o4-mini
    # deployment rejects with HTTP 400. Force the "responses" wire API instead.
    COPILOT_PROVIDER_WIRE_API: responses
imports:
  - shared/github-guard-policy.md
  - shared/gh.md
  - shared/reporting.md
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
  agentic-workflows:
  cache-memory: true
  comment-memory: true
  edit:
  bash:
    - "*"
  github:
    mode: gh-proxy
    min-integrity: approved
    trusted-users:
      - pelikhan
  playwright:
  web-fetch:
  cli-proxy: true
runtimes:
  go:
    version: "1.26"
safe-outputs:
    allowed-domains: [default-safe-outputs]
    upload-artifact:
      max-uploads: 1
      retention-days: 1
      skip-archive: true
    add-comment:
      allowed-repos: ["github/gh-aw"]
      hide-older-comments: true
      max: 2
    create-issue:
      expires: 2h
      group: true
      close-older-issues: true
      close-older-key: "smoke-copilot-aoai-apikey"
      labels: [automation, testing]
    create-discussion:
      category: announcements
      labels: [ai-generated]
      expires: 2h
      close-older-discussions: true
      close-older-key: "smoke-copilot-aoai-apikey"
      max: 1
    create-pull-request-review-comment:
      max: 5
    submit-pull-request-review:
    reply-to-pull-request-review-comment:
      max: 5
    add-labels:
      allowed: [smoke-copilot-aoai-apikey]
      allowed-repos: ["github/gh-aw"]
    remove-labels:
      allowed: [smoke]
    set-issue-type:
    dispatch-workflow:
      workflows:
        - haiku-printer
      max: 1
    create-check-run:
      name: "Smoke Copilot - AOAI (apikey)"
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
      run-started: "📰 BREAKING: [{workflow_name}]({run_url}) is now investigating this {event_type}. Sources say the story is developing..."
      run-success: "📰 VERDICT: [{workflow_name}]({run_url}) has concluded. All systems operational. This is a developing story. 🎤"
      run-failure: "📰 DEVELOPING STORY: [{workflow_name}]({run_url}) reports {status}. Our correspondents are investigating the incident..."
timeout-minutes: 15
# Per-run cost guardrail: healthy runs cost ~27 AI credits, so 60 leaves
# headroom while capping the token burn of a runaway or failing run.
max-ai-credits: 60
experiments:
  caveman: [yes, no]
  subagent_model: [small, large]
features:
  gh-aw-detection: false
evals:
  - id: caveman_goal_met
    question: Does the agent output show that the objective for experiment caveman was successfully completed?
  - id: subagent_model_goal_met
    question: Does the agent output show that the objective for experiment subagent_model was successfully completed?

sandbox:
  agent:
    id: awf
---

# Smoke Test: Copilot Engine Validation (AOAI apikey BYOK)

> **⚡ EXECUTE IMMEDIATELY**: You are the test runner. Begin executing the tests below right now using bash and the available tools. Do NOT analyze the task, do NOT propose creating files, do NOT say "no action needed". Your first action MUST be a real tool call (bash, `github` tool, or safeoutputs). Jump directly to the "Tests to Execute" section and start with test 1.

This variant routes the Copilot engine through Azure OpenAI (AOAI) using BYOK
mode with an API key, via the `FOUNDRY_API_KEY` and `FOUNDRY_OPENAI_ENDPOINT`
secrets wired into `engine.env`.

{{#if experiments.caveman }}
Talk like a caveman in all your responses and outputs. Use short, broken sentences. Me test. You run.
{{/if}}

## Hard Limit: `add_comment` Budget

`safe-outputs.add-comment.max` is `2`. Never exceed 2 total `add_comment` calls in this run.

- Call #1 is required for the discussion interaction test (comment on latest discussion).
- Call #2 depends on trigger:
  - `pull_request` event: post the brief PR summary comment, and **skip** the fun discussion follow-up comment.
  - non-`pull_request` event: **skip** the PR summary comment and post the fun discussion follow-up comment.

## Tool Access Overview

This workflow uses `cli-proxy: true`. The following MCP servers are **NOT available as MCP tools** — they are mounted exclusively as **shell CLI commands** (see `<mcp-clis>` section above). You **must** use them via the `bash` tool:

- **`playwright`** — installed as `@playwright/cli`, use `playwright-cli <command>` in bash (e.g. `playwright-cli open https://github.com`, `playwright-cli screenshot`)
- **`serena`** — use `serena <tool> [--param value...]` in bash (e.g. `serena activate_project --path ...`)
- **`agenticworkflows`** — use `agenticworkflows <tool> [--param value...]` in bash
- **`safeoutputs`** — use `safeoutputs <tool> [--param value...]` in bash (e.g. `safeoutputs add_comment --body "..."`)
- **`mcpscripts`** — use `mcpscripts <tool> [--param value...]` in bash (e.g. `mcpscripts mcpscripts-gh --args "..."`)

The `github` MCP server is **NOT** CLI-mounted — it remains available as a normal MCP tool.

Run `<server> --help` to list all available tools for a server, or `<server> <tool> --help` for detailed parameter info.

These are **not** MCP protocol tools — they are bash executables. Call them with the `bash` tool only.

## Tests to Execute

Run each check NOW and mark as ✅/❌. Do NOT create files to automate this — execute directly using bash and tools:

1. `github` tool (configured with `mode: gh-proxy`): review 2 merged PRs in `${{ github.repository }}`.
2. `mcpscripts-gh`: query 2 PRs using `pr list --repo ${{ github.repository }} --limit 2 --json number,title,author`.
3. Serena CLI (bash only): run `serena activate_project --path ${{ github.workspace }}`, then `serena find_symbol --name_path <symbol>` and confirm at least 3 symbols.
4. Playwright CLI (bash only): run `playwright-cli open https://github.com` then `playwright-cli screenshot`; confirm successful GitHub navigation.
5. `web-fetch` tool: fetch `https://github.com` and confirm response contains `GitHub`.
6. File + bash: create `/tmp/gh-aw/agent/smoke-test-copilot-${{ github.run_id }}.txt` with timestamped success text, then `cat` it.
7. Discussion interaction: get latest discussion with `github-discussion-query` (`limit=1`, `jq=".[0]"`), extract number, then `add_comment` to that discussion.
8. Build: run `GOCACHE=/tmp/gh-aw/agent/go-cache GOMODCACHE=/tmp/gh-aw/agent/go-mod make build`.
9. Artifact upload (only if build passes): stage `./gh-aw` at `$RUNNER_TEMP/gh-aw/safeoutputs/upload-artifacts/gh-aw` and call `upload_artifact` with `path: "gh-aw"`.
10. Discussion create: call `create_discussion` in `announcements` with label `ai-generated`, title `copilot was here`, and `temporary_id: "aw_discuss"`.
11. Workflow dispatch: call `dispatch_workflow` for `haiku-printer`, set top-level `ref` to `${{ github.event.repository.default_branch }}`, and include `inputs.message` with an original testing/automation haiku (non-empty string).
12. PR review tools: add 1-2 inline `create_pull_request_review_comment` comments, submit review with event `COMMENT`, then reply to most recent existing review comment ID when available.
13. Comment memory: append an original 3-line haiku to `/tmp/gh-aw/comment-memory/*.md`.
14. Sub-agent: use `file-summarizer` on `README.md`.
15. Check run: call `create_check_run` with `conclusion=success`, title `Smoke Copilot - AOAI (apikey) - Run ${{ github.run_id }}`, summary `All smoke tests completed.`, text `Detailed results attached.`

## Output

1. **Create an issue** with a summary of the smoke test run:
   - Use the temporary ID `aw_smoke1` for the issue so you can reference it later
   - Title: "Smoke Test: Copilot - AOAI (apikey) - ${{ github.run_id }}"
   - Body should include:
     - Test results (✅ or ❌ for each test)
     - Overall status: PASS or FAIL
     - Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}
     - Timestamp
     - Pull request author and assignees

2. **Set Issue Type** (**required**): Use the `set_issue_type` safe-output tool with `issue_number: "aw_smoke1"` (the temporary ID from step 1) and `issue_type: "Bug"` to set the type of the just-created smoke test issue.

3. **Only if this workflow was triggered by a pull_request event**: Use the `add_comment` tool to add a **very brief** comment (max 5-10 lines) to the triggering pull request (omit the `item_number` parameter to auto-target the triggering PR) with:
   - PR titles only (no descriptions)
   - ✅ or ❌ for each test result
   - Overall status: PASS or FAIL
   - Mention the pull request author and any assignees

4. **Only if this workflow was NOT triggered by a pull_request event**: Use the `add_comment` tool to add a **fun and creative comment** to the newly created discussion (use `item_number: "aw_discuss"` from step 11) - be playful and entertaining in your comment

5. Use the `send_slack_message` tool to send a brief summary message (e.g., "Smoke test ${{ github.run_id }}: All tests passed! ✅")

If all tests pass and this workflow was triggered by a pull_request event:
- Use the `add_labels` safe-output tool to add the label `smoke-copilot-aoai-apikey` to the pull request (omit the `item_number` parameter to auto-target the triggering PR)
- Use the `remove_labels` safe-output tool to remove the label `smoke` from the pull request (omit the `item_number` parameter to auto-target the triggering PR)
## agent: `file-summarizer`
---
model: ${{ experiments.subagent_model }}
description: Summarizes the content of a file in a few concise sentences
---
You are a file summarization assistant. When given a file path, read the file and return a brief summary (2–4 sentences) describing its purpose and key contents. Be concise and factual.