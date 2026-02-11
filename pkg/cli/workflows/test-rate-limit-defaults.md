---
name: Test Rate Limiting with Default Ignored Roles
engine: copilot
on:
  workflow_dispatch:
  issue_comment:
    types: [created]
rate-limit:
  max: 5
  window: 60
---

Test workflow to demonstrate default ignored roles behavior.

By default, admin, maintain, and write users are exempt from rate limiting.
Only triage and read users will be subject to rate limiting.

Hello! This is a test workflow.
