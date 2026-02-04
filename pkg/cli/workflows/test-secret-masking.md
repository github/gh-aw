---
description: Test workflow for validating secret masking and redaction functionality
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
strict: false
engine: copilot
imports:
  - shared/secret-redaction-test.md
---

# Test Secret Masking Workflow

This workflow tests the secret-masking feature by importing custom secret redaction steps.

The imported steps will search for and replace the pattern "password123" with "REDACTED" in all files under /tmp/gh-aw/.
