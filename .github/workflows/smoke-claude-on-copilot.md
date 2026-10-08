---
private: true
emoji: "🧪"
description: Smoke test for Claude engine on GitHub Inference that posts a concise PR summary comment
on:
  schedule: every 2 days
  slash_command:
    name: smoke-claude-on-copilot
    strategy: centralized
    events: [pull_request, pull_request_comment]
  status-comment: true
permissions:
  contents: read
  pull-requests: read
name: Smoke Claude on Copilot
model: claude-haiku-4.5
engine:
  id: claude
  model-provider: github
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

# Smoke Test: Claude on GitHub Inference PR Summary

Goal: validate that Claude with `model-provider: github` can reach GitHub inference through AWF and, for pull requests, post one concise summary comment.

1. For a pull request, read its details and produce a short summary with:
   - PR title
   - author
   - file count
   - a 2-3 sentence high-level summary of what changed
   Post exactly one `add_comment` safe output to that PR with the summary.
2. For a scheduled run, make a concise, model-generated observation about this Claude-on-Copilot smoke check and its AWF provider gateway, then submit that observation through the `noop` safe output. Do not claim the check succeeded unless the run itself provides evidence.
3. For any other context without a pull request, call `noop`.

Keep the comment compact (max 8 lines).