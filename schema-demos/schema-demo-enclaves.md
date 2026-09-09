---
description: Demonstrates the `enclaves` schema field
on:
  workflow_dispatch:
permissions:
  contents: read
engine: codex
network: defaults
sandbox:
  agent:
    id: awf
  mcp:
    version: v0.4.15
enclaves:
  - script:
    repos:
      - repo: octo-org/private-service
        sensitivity: trusted
  - agent:
      model: gpt-5
      github:
        cli: issues-read-v1
    repos:
      - repo: octo-org/private-service
        sensitivity: trusted
timeout-minutes: 5
---

# Schema Demo: `enclaves`

This workflow was auto-generated to demonstrate usage of the `enclaves` field in the
gh-aw frontmatter schema. It exists solely to achieve 100% schema feature coverage.

## What `enclaves` Does

AWF-owned private-repository executors exposed only through the compiler-launched MCP
gateway. Omit this field to disable enclaves.

## Task

Call `noop` -- this is a coverage-only demo workflow.

**Important**: Always call the `noop` safe-output tool.

```json
{"noop": {"message": "Coverage demo for `enclaves` -- no action needed."}}
```
