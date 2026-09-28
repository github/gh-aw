---
description: Demonstrates the `excluded-env` schema field
on:
  workflow_dispatch:
permissions:
  contents: read
engine: codex
excluded-env:
  - MY_DISPATCH_TOKEN
timeout-minutes: 5
---

# Schema Demo: `excluded-env`

This workflow was auto-generated to demonstrate usage of the `excluded-env` field in the
gh-aw frontmatter schema. It exists solely to achieve 100% schema feature coverage.

## What `excluded-env` Does

Optional list of environment variable names to unconditionally exclude from the AWF agent container via --exclude-env.

## Task

Call `noop` — this is a coverage-only demo workflow.

**Important**: Always call the `noop` safe-output tool.

```json
{"noop": {"message": "Coverage demo for `excluded-env` — no action needed."}}
```
