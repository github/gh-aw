---
private: true
emoji: "🧪"
description: Smoke test for Claude engine using GitHub provider that posts a concise PR summary comment
on:
  schedule: every 2 days
  slash_command:
    name: smoke-github-claude
    strategy: centralized
    events: [pull_request, pull_request_comment]
  status-comment: true
permissions:
  contents: read
  pull-requests: read
name: Smoke GitHub Claude
model: claude-haiku-4.5
engine:
  id: claude
  model-provider: github
  fallback-models:
    - copilot/claude-sonnet-5
    - copilot/claude-sonnet-4.6
  bare: true
strict: true
imports:
  - shared/reporting.md
tools:
  github:
    mode: gh-proxy
safe-outputs:
  allowed-domains: [default-safe-outputs]
  add-comment:
    max: 1
    hide-older-comments: true
timeout-minutes: 10
sandbox:
  agent:
    id: awf
---

# Smoke Test: Claude on GitHub Provider PR Summary

Goal: validate that Claude with `model-provider: github` can read the current pull request and post one concise summary comment.

1. If this run is not in PR context, call `noop` and stop.
2. Read the current PR details for `${{ github.event.pull_request.number }}` from `${{ github.repository }}`.
3. Produce a short summary with:
   - PR title
   - author
   - file count
   - a 2-3 sentence high-level summary of what changed
4. Post exactly one `add_comment` safe output to the current PR with this summary.

Keep the comment compact (max 8 lines).