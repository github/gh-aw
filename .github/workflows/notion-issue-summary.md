---
description: Creates issue summaries and syncs them to Notion for project management and tracking
timeout-minutes: 5
on:
  workflow_dispatch:
    inputs:
      issue-number:
        description: "Issue number to analyze"
        required: true
        type: string
permissions:
  contents: read
  issues: read
  pull-requests: read
tools:
  github:
imports:
  - shared/mood.md
  - shared/mcp/notion.md
strict: true
---

# Issue Summary to Notion

Analyze the issue #${{ github.event.inputs.issue-number }} and create a brief summary, then add it as a comment to the Notion page.

## Instructions

1. Read and analyze the issue content
2. Create a concise summary (2-3 sentences) of the issue
3. Use the `notion_add_comment` safe-job to add your summary as a comment to the Notion page
