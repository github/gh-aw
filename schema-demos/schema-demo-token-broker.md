---
description: Demonstrates the `token-broker` schema field
on:
  workflow_dispatch:
permissions:
  contents: read
engine: codex
token-broker:
  copilot-task: true
timeout-minutes: 5
---

# Schema Demo: `token-broker`

This workflow demonstrates usage of the `token-broker` field in the gh-aw
frontmatter schema. It exists solely to achieve 100% schema feature coverage.

## What `token-broker` Does

Experimental. Select independent Copilot capabilities to authenticate through the
OIDC token broker. Using this field emits a compile-time warning. Configuration
and behavior may change without deprecation.

This example selects brokered authentication for Copilot task creation and issue
assignment. It does not configure a task consumer, so no broker exchange occurs.
Using this capability with a task consumer requires an externally configured
OIDC token broker and the `GH_AW_TOKEN_BROKER_URL` Actions variable or secret.

## Task

Call `noop` — this is a coverage-only demo workflow.

**Important**: Always call the `noop` safe-output tool.

```json
{"noop": {"message": "Coverage demo for `token-broker` — no action needed."}}
```
