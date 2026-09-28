---
description: Demonstrates the `intent` schema field
on:
  workflow_dispatch:
permissions:
  contents: read
engine: codex
intent: Reduce manual issue-triage work while keeping classifications evidence-based.
timeout-minutes: 5
---

# Schema Demo: `intent`

This workflow was auto-generated to demonstrate usage of the `intent` field in the
gh-aw frontmatter schema. It exists solely to achieve 100% schema feature coverage.

## What `intent` Does

Optional statement of the durable outcome the workflow exists to achieve.

## Task

Call `noop` — this is a coverage-only demo workflow.

**Important**: Always call the `noop` safe-output tool.

```json
{"noop": {"message": "Coverage demo for `intent` — no action needed."}}
```
