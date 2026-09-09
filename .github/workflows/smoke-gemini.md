---
private: true
emoji: "🧪"
description: Smoke test workflow that validates Gemini engine functionality twice daily
on:
  schedule: every 2 days
  slash_command:
    name: smoke-gemini
    strategy: centralized
    events: [issues, issue_comment, pull_request, pull_request_comment]
  workflow_dispatch:
  pull_request:
    types: [labeled]
    names: ["smoke"]
  reaction: "rocket"
  status-comment: true
permissions:
  contents: read
  issues: read
  pull-requests: read
name: Smoke Gemini
experiments:
  sub_agent_strategy:
    variants: [single_agent, sub_agents]
    description: "Test whether decomposing smoke test tasks to sub-agents reduces cost without losing reliability"
    hypothesis: "H0: no change in effective_tokens. H1: sub_agents reduces tokens by >=20%"
    metric: effective_tokens
    secondary_metrics: [run_duration_seconds, success_rate]
    guardrail_metrics:
      - name: success_rate
        threshold: ">=0.95"
    min_samples: 30
    weight: [50, 50]
    start_date: "2026-05-20"
    analysis_type: mann_whitney
    tags: [cost_optimization, smoke_tests]
engine:
  id: gemini
strict: true
imports:
  - shared/reporting-otlp.md
  - shared/otlp.md
  - shared/token-telemetry-check.md
  - shared/smoke-test-brevity.md
  - shared/playwright-title-test.md
network:
  allowed:
    - defaults
    - github
tools:
  cache-memory: true
  github:
    toolsets: [repos, pull_requests]
  edit:
  bash:
    - "*"
  web-fetch:
safe-outputs:
    allowed-domains: [default-safe-outputs]
    add-comment:
      hide-older-comments: true
      max: 2
    create-issue:
      expires: 2h
      close-older-issues: true
      close-older-key: "smoke-gemini"
      labels: [automation, testing]
    add-labels:
      allowed: [smoke-gemini]
    messages:
      footer: "> ✨ *[{workflow_name}]({run_url}) — Powered by Gemini*{ai_credits_suffix}{history_link}"
      run-started: "✨ Gemini awakens... [{workflow_name}]({run_url}) begins its journey on this {event_type}..."
      run-success: "🚀 [{workflow_name}]({run_url}) **MISSION COMPLETE!** Gemini has spoken. ✨"
      run-failure: "⚠️ [{workflow_name}]({run_url}) {status}. Gemini encountered unexpected challenges..."
timeout-minutes: 10
evals:
  - id: sub_agent_strategy_goal_met
    question: Does the agent output show that the objective for experiment sub_agent_strategy was successfully completed?

sandbox:
  agent:
    id: awf
    runtime: cloud-hypervisor
---

# Smoke Test: Gemini Engine Validation

{{#if experiments.sub_agent_strategy == 'single_agent'}}
## Test Requirements (Single Agent — Baseline)

Execute all 5 tests sequentially in this agent:

1. **GitHub MCP Testing**: Use GitHub MCP tools to fetch details of exactly 2 merged pull requests from ${{ github.repository }} (title and number only)
2. **Web Fetch Testing**: Use the web-fetch MCP tool to fetch https://github.com and verify the response contains "GitHub" (do NOT use bash or playwright)
3. **File Writing Testing**: Create a test file `/tmp/gh-aw/agent/smoke-test-gemini-${{ github.run_id }}.txt` with content "Smoke test passed for Gemini at $(date)"
4. **Bash Tool Testing**: Execute bash commands to verify file creation was successful (use `cat` to read the file back)
5. **Build gh-aw**: Run `GOCACHE=/tmp/gh-aw/agent/go-cache GOMODCACHE=/tmp/gh-aw/agent/go-mod make build` to verify the agent can successfully build the gh-aw project. If the command fails, mark this test as ❌ and report the failure.

After completing all tests, proceed to the Output section below.
{{/if}}

{{#if experiments.sub_agent_strategy == 'sub_agents'}}
## Test Requirements (Sub-Agent Strategy)

Launch 5 parallel `task` sub-agents (agent_type: `task`) to execute tests independently. Each sub-agent should run one test requirement and return a simple ✅ or ❌ result.

Use the `task` tool with `mode: "background"` to launch all 5 agents in parallel:

1. **Agent: github-mcp-test** — Use GitHub MCP tools to fetch details of exactly 2 merged pull requests from ${{ github.repository }} (title and number only). Return ✅ if successful.
2. **Agent: web-fetch-test** — Use the web-fetch MCP tool to fetch https://github.com and verify the response contains "GitHub". Return ✅ if successful.
3. **Agent: file-write-test** — Create a test file `/tmp/gh-aw/agent/smoke-test-gemini-${{ github.run_id }}.txt` with content "Smoke test passed for Gemini at $(date)". Return ✅ if successful.
4. **Agent: bash-test** — Execute bash commands to verify file creation was successful (use `cat` to read the file back). Return ✅ if successful.
5. **Agent: build-test** — Run `GOCACHE=/tmp/gh-aw/agent/go-cache GOMODCACHE=/tmp/gh-aw/agent/go-mod make build` to verify the agent can successfully build the gh-aw project. Return ✅ if successful, ❌ if failed.

After launching all agents, wait for completion notifications and collect results using `read_agent`. Then proceed to the Output section below.
{{/if}}

## Output

**ALWAYS create an issue** with a summary of the smoke test run:
- Title: "Smoke Test: Gemini - ${{ github.run_id }}"
- Body should include:
  - Test results (✅ or ❌ for each test)
  - Overall status: PASS or FAIL
  - Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}
  - Timestamp

**Only if this workflow was triggered by a pull_request event**: Use the `add_comment` tool to add a **very brief** comment (max 5-10 lines) to the triggering pull request (omit the `item_number` parameter to auto-target the triggering PR) with:
- ✅ or ❌ for each test result
- Overall status: PASS or FAIL

If all tests pass and this workflow was triggered by a pull_request event, use the `add_labels` safe-output tool to add the label `smoke-gemini` to the pull request (omit the `item_number` parameter to auto-target the triggering PR).