---
on:
  workflow_dispatch:
permissions:
  contents: read
  pull-requests: read
engine: copilot
safe-outputs:
  merge-pull-request:
    allowed-base-branches: ["agent-work/*"]
    max: 1
    required-labels: ["automerge"]
    allowed-branches: ["feature/*", "fix/*"]
timeout-minutes: 5
---

# Test Copilot Merge Pull Request

Test the `merge_pull_request` safe output type with the Copilot engine.

## Task

Merge pull request #1 using the "squash" merge method with:
- commit_title: "Test: Squash merge via automated workflow"
- commit_message: "This pull request was merged automatically by the Copilot test workflow to verify the merge_pull_request safe output type."

Output results in JSONL format using the `merge_pull_request` tool.
