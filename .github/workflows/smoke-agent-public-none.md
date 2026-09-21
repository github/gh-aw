---
private: true
emoji: "🧪"
description: "Guard policy smoke test: repos=public, min-integrity=none"
on:
  schedule: every 2 days
  slash_command:
    name: smoke-agent-public-none
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

sandbox:
  agent:
    id: awf
name: "Smoke Agent: public/none"
engine: claude
strict: true
tools:
  cli-proxy: false
  bash: false
  github:
    mode: local
    allowed-repos: "public"
    min-integrity: none
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
    run-started: "🔍 [{workflow_name}]({run_url}) testing guard policy: `repos=public, min-integrity=none`..."
    run-success: "✅ [{workflow_name}]({run_url}) completed guard policy test."
    run-failure: "❌ [{workflow_name}]({run_url}) {status} testing guard policy: `repos=public, min-integrity=none`. Check the logs for details."
timeout-minutes: 10
imports:
  - shared/otlp.md
  - shared/reporting.md
features:
  gh-aw-detection: false
---

# Guard Policy Smoke Test: public/none

This workflow tests the guard policy with `repos: public` and `min-integrity: none`.
Only public repositories should be accessible, but with no integrity restrictions on content.

## Instructions

Test GitHub MCP tool access under this guard policy by performing these operations and reporting results.

### Step 1: Search public repositories (should succeed)

Use `search_repositories` with `perPage: 3` to search for `topic:actions stars:>1000` and return the top 3 results. These are public repos. Record:
- Repository full name (owner/repo)
- Star count
- Whether access was allowed or denied

### Step 2: List issues from a popular public repository (should succeed)

Use `list_issues` on `actions/checkout` with `state: open` and `perPage: 3`. This is a public repository. Record:
- Issue number and title
- Whether access was allowed or denied

### Step 3: Search repositories in github org (may include private repos)

Use `search_repositories` with `perPage: 3` to search for `org:github gh-aw`. This may return private/internal repos which should be filtered by the `public` scope. Record:
- Repository full name
- Visibility (if available)
- Whether access was allowed or denied

### Step 4: List issues from this repository

Use `list_issues` on `${{ github.repository }}` with `state: open` and `perPage: 3`. Note whether this private/internal repo is accessible under `public` scope. Record:
- Issue number and title
- Whether access was allowed or denied

### Step 5: Search code in public repos (should succeed)

Use `search_code` to search for `actions/checkout language:yaml` and return the top 3 results. Record:
- File paths and repositories
- Whether access was allowed or denied

### Step 6: Report results

Use the `add_comment` safe-output tool to post a summary to the current PR:

```json
{
  "type": "add_comment",
  "body": "## Guard Policy Test Results: `public/none`\n\n### Policy\n- repos: `public`\n- min-integrity: `none`\n\n### Results\n<results from each step>\n\n### Expected\n- Steps 1, 2, 5: ALLOWED (public repos, no integrity restriction)\n- Steps 3, 4: May be BLOCKED if repos are private/internal"
}
```

If there is no PR context, use the `noop` tool to report the results summary.