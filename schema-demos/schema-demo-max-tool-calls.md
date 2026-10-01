---
description: Demonstrates the `max-tool-calls` schema field
on:
  workflow_dispatch:
permissions:
  contents: read
engine:
  id: copilot
  copilot-sdk: true
max-tool-calls: 50
timeout-minutes: 5
---

# Schema Demo: `max-tool-calls`

This workflow was auto-generated to demonstrate usage of the `max-tool-calls` field in the
gh-aw frontmatter schema. It exists solely to achieve 100% schema feature coverage.

## What `max-tool-calls` Does

Caps the total number of tool invocations the primary agent may dispatch during the run.

## Task

Call `noop` -- this is a coverage-only demo workflow.

**Important**: Always call the `noop` safe-output tool.

```json
{"noop": {"message": "Coverage demo for `max-tool-calls` -- no action needed."}}
```
