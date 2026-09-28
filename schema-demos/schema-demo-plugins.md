---
description: Demonstrates the `plugins` schema field
on:
  workflow_dispatch:
permissions:
  contents: read
engine: codex
plugins:
  - github/example-plugin@main
timeout-minutes: 5
---

# Schema Demo: `plugins`

This workflow was auto-generated to demonstrate usage of the `plugins` field in the
gh-aw frontmatter schema. It exists solely to achieve 100% schema feature coverage.

## What `plugins` Does

Experimental agent plugins to install after the agentic engine.

## Task

Call `noop` — this is a coverage-only demo workflow.

**Important**: Always call the `noop` safe-output tool.

```json
{"noop": {"message": "Coverage demo for `plugins` — no action needed."}}
```
