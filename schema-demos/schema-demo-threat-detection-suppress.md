---
description: Demonstrates the `threat-detection-suppress` schema field
on:
  workflow_dispatch:
permissions:
  contents: read
engine: codex
threat-detection-suppress:
  - rule: CTR-001
    reason: Demonstration-only suppression annotation for schema coverage.
timeout-minutes: 5
---

# Schema Demo: `threat-detection-suppress`

This workflow was auto-generated to demonstrate usage of the `threat-detection-suppress` field in the
gh-aw frontmatter schema. It exists solely to achieve 100% schema feature coverage.

## What `threat-detection-suppress` Does

Auditable false-positive suppression annotations for compiler threat-detection rules.

## Task

Call `noop` — this is a coverage-only demo workflow.

**Important**: Always call the `noop` safe-output tool.

```json
{"noop": {"message": "Coverage demo for `threat-detection-suppress` — no action needed."}}
```
