---
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: write
engine: copilot
tools:
  work-queue:
    worker: true
safe-outputs:
  jobs:
    custom:
      runs-on: ubuntu-latest
      steps:
        - run: echo prepared
  claim-adapters:
    custom:
      mode: prepared
      effect-type: update_issue
      target-repo: github/gh-aw
      field-map:
        body: content
      expected:
        item_number: 1
---

# Test Copilot Claim Adapters

Process the assigned work-queue claim with the custom claim adapter and emit one JSONL result for `custom`.
