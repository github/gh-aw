---
on:
  workflow_dispatch:
  schedule:
    - cron: '0 9 * * *'  # Daily at 9 AM UTC
name: Dev
description: Daily status report for gh-aw project
timeout-minutes: 30
strict: false
engine: copilot-sdk

permissions:
  contents: read
  issues: read
  pull-requests: read

safe-outputs:
  create-issue:
    expires: 7d
    title-prefix: "[Daily Report] "
imports:
  - shared/mood.md
---

# Daily Status Report

Generate a daily status report for the gh-aw project.

**Requirements:**
1. Analyze the current state of the repository
2. Check for recent commits, pull requests, and issues
3. Identify any potential issues or areas needing attention
4. Create a comprehensive daily status report
5. Post the report as an issue with the date in the title

Keep the report informative but concise.
