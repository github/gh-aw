---
private: true
emoji: "🌸"
description: Smoke Copilot Auto — generates a haiku and posts it as a PR comment
on:
  schedule: every 2 days
  label_command:
    name: smoke
    events: [pull_request]
  workflow_dispatch:
permissions:
  contents: read
  pull-requests: read
  copilot-requests: write
name: Smoke Copilot Auto
model: auto
engine:
  id: copilot
  bare: true
imports:
  - shared/smoke-test-brevity.md
  - shared/reporting.md
  - uses: shared/session-artifact-check.md
    with:
      session-artifact: copilot-events
tools:
  github:
    mode: gh-proxy
    toolsets: [default]
safe-outputs:
  add-comment:
    hide-older-comments: true
    max: 1
timeout-minutes: 5
features:
  gh-aw-detection: false
sandbox:
  agent:
    id: awf
---

# Smoke Test: Auto Haiku

## Task

1. Read the pull request title and description from the GitHub context.
2. Compose an original haiku (3 lines: 5-7-5 syllables) inspired by the pull request.
3. Post the haiku as a comment on the pull request using `add_comment`.

## Output

Post a comment on the pull request with:
- The haiku (3 lines, 5-7-5 syllables)
- A one-sentence caption explaining what inspired the haiku

Use `noop` if no pull request is available in the context.