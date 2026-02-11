---
name: Test Rate Limiting
engine: copilot
on:
  workflow_dispatch:
  issue_comment:
    types: [created]
rate-limit:
  max: 3
  window: 30
  events: [workflow_dispatch, issue_comment]
---

Test workflow to demonstrate rate limiting functionality.

This workflow limits each user to 3 runs within a 30-minute window for workflow_dispatch and issue_comment events.
