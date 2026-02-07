---
title: Manual Workflows
description: On-demand workflows triggered manually via workflow_dispatch - research, analysis, and tasks you run when needed
sidebar:
  order: 4
---

Manual workflows run only when explicitly triggered via the GitHub Actions UI or CLI. They're perfect for on-demand tasks like research, analysis, or operations that need human judgment about timing.

## When to Use Manual Workflows

- **On-demand research**: Search and analyze topics as needed
- **Manual operations**: Tasks requiring human judgment on timing
- **Testing and debugging**: Run workflows with custom inputs
- **One-time tasks**: Operations that don't fit a schedule

## Example Manual Triggers

```yaml
on:
  workflow_dispatch:
    inputs:
      topic:
        description: 'Research topic'
        required: true
        type: string
```

```yaml
on:
  workflow_dispatch:
    inputs:
      severity:
        description: 'Issue severity level'
        required: false
        type: choice
        options:
          - low
          - medium
          - high
```

## Accessing Inputs in Workflows

Use `${{ github.event.inputs.INPUT_NAME }}` to access input values in your workflow markdown:

```aw wrap
---
on:
  workflow_dispatch:
    inputs:
      topic:
        description: 'Research topic'
        required: true
        type: string
      depth:
        description: 'Analysis depth'
        type: choice
        options:
          - brief
          - detailed
        default: brief
permissions:
  contents: read
safe-outputs:
  create-discussion:
---

# Research Assistant

Research the topic: "${{ github.event.inputs.topic }}"

Analysis depth: ${{ github.event.inputs.depth }}

Provide findings based on the requested depth level.
```

## Running Manual Workflows

Via CLI:

```bash
gh aw run workflow
```

Via GitHub Actions UI:

1. Go to Actions tab
2. Select workflow
3. Click "Run workflow"
4. Fill in inputs (if any)
5. Click "Run workflow" button

## Quick Start

Add a manual workflow to your repository:

```bash
gh aw add-wizard githubnext/agentics/weekly-research
```

Then run it:

```bash
gh aw run weekly-research
```
