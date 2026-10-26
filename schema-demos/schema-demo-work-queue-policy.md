---
description: Demonstrates the `work-queue-policy` schema field
on:
  workflow_dispatch:
permissions:
  contents: read
engine: codex
tools:
  work-queue: true
work-queue-policy:
  mode: weighted-priority
timeout-minutes: 5
---

# Schema Demo: `work-queue-policy`

This workflow demonstrates the deprecated `work-queue-policy` frontmatter field
for schema feature coverage.

## What `work-queue-policy` Does

Deprecated legacy version-3 Policy proposal. Use scheduling-only work_queue in .github/workflows/aw.json and AW-approved dispatch targets. Cannot be combined with repository work_queue settings. Installed Policy remains authoritative and historical ledgers remain readable.

The required `tools.work-queue: true` enables the observer role. This demo only
reads queue state; it does not submit work, dispatch workers, or install a Policy.
Compile this legacy example in a repository without `work_queue` settings in
`.github/workflows/aw.json`; this repository already uses those modern settings.

## Task

Call `noop` — this is a coverage-only demo workflow.

**Important**: Always call the `noop` safe-output tool.

```json
{"noop": {"message": "Coverage demo for `work-queue-policy` — no action needed."}}
```
