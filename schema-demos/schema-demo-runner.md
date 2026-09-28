---
description: Demonstrates the `runner` schema field
on:
  workflow_dispatch:
permissions:
  contents: read
engine: codex
runner:
  topology: arc-dind
timeout-minutes: 5
---

# Schema Demo: `runner`

This workflow was auto-generated to demonstrate usage of the `runner` field in the
gh-aw frontmatter schema. It exists solely to achieve 100% schema feature coverage.

## What `runner` Does

Runner topology configuration for environment-specific behavior activation.

## Task

Call `noop` — this is a coverage-only demo workflow.

**Important**: Always call the `noop` safe-output tool.

```json
{"noop": {"message": "Coverage demo for `runner` — no action needed."}}
```
