---
private: true
emoji: "🧪"
description: "Guard policy smoke test: repos=[github/gh-aw, github/*], min-integrity=approved (scoped patterns)"
on:
  schedule: every 2 days
  slash_command:
    name: smoke-agent-scoped-approved
    strategy: centralized
    events: [issues, issue_comment, pull_request, pull_request_comment]
  workflow_dispatch:
  pull_request:
    types: [labeled]
    names: ["metal"]
  status-comment: true
permissions:
  contents: read
  issues: read
  pull-requests: read
name: "Smoke Agent: scoped/approved"
engine: claude
strict: true
imports:
  - shared/github-guard-policy.md
  - shared/otlp.md
  - shared/reporting.md
  - shared/playwright-title-test.md
tools:
  github:
    mode: local
    allowed-repos:
      - "github/gh-aw"
      - "github/*"
    min-integrity: approved
network:
  allowed:
    - defaults
    - github
safe-outputs:
  allowed-domains: [default-safe-outputs]
  add-comment:
    hide-older-comments: true
    max: 2
  messages:
    footer: "> 🤖 *Guard policy smoke test by [{workflow_name}]({run_url})*{ai_credits_suffix}{history_link}"
    run-started: "🔍 [{workflow_name}]({run_url}) testing guard policy: `repos=[github/gh-aw, github/*], min-integrity=approved`..."
    run-success: "✅ [{workflow_name}]({run_url}) completed guard policy test."
    run-failure: "❌ [{workflow_name}]({run_url}) {status} testing guard policy: `repos=[github/gh-aw, github/*], min-integrity=approved`. Check the logs for details."
timeout-minutes: 10
features:
  gh-aw-detection: false
sandbox:
  agent:
    runtime: cloud-hypervisor
    id: awf
---

# Guard Policy Smoke Test: scoped/approved (scoped patterns)

This workflow tests a scoped guard policy with explicit repository patterns.
- `repos: ["github/gh-aw", "github/*"]` — only github org repos should be accessible
- `min-integrity: approved` — only approved content should be visible

## Instructions

Test GitHub MCP tool access under this guard policy by performing these operations and reporting results.

### Step 1: List issues from this repository (should succeed)

Use `list_issues` on `${{ github.repository }}` with `state: open` and `perPage: 3`. This is `github/gh-aw` which is in the allowed repos list. Record:
- Issue number and title
- Whether access was allowed or denied

### Step 2: Search repositories in the github org (should succeed)

Use `search_repositories` with `perPage: 3` to search for `org:github gh-aw`. These match `github/*` pattern. Record:
- Repository full name
- Whether access was allowed or denied

### Step 3: List issues from a non-github org repository (should be blocked)

Use `list_issues` on `actions/checkout` with `state: open` and `perPage: 3`. This is NOT in the `github/*` scope so access should be denied. Record:
- Whether access was allowed or denied
- Any error message received

### Step 4: Search repositories outside github org (should be blocked)

Use `search_repositories` with `perPage: 3` to search for `topic:actions org:actions` to find repos in the `actions` org. These should NOT be accessible. Record:
- Whether access was allowed or denied
- Any error message received

### Step 5: Search code in scoped repos (should succeed)

Use `search_code` to search for `guard-policies language:go repo:github/gh-aw`. This is within scope. Record:
- File paths found
- Whether access was allowed or denied

### Step 6: Report results

Use the `add_comment` safe-output tool to post a summary to the current PR:

```json
{
  "type": "add_comment",
  "body": "## Guard Policy Test Results: `scoped/approved`\n\n### Policy\n- repos: `[\"github/gh-aw\", \"github/*\"]`\n- min-integrity: `approved`\n\n### Results\n<results from each step>\n\n### Expected\n- Steps 1, 2, 5: ALLOWED (within github/* scope)\n- Steps 3, 4: BLOCKED (outside github/* scope)"
}
```

If there is no PR context, use the `noop` tool to report the results summary.