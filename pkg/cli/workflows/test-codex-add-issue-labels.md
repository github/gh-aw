---
on:
  workflow_dispatch:
permissions:
  issues: write
strict: false
features:
  dangerous-permissions-write: true
engine: codex
---

# Test Codex Add Issue Labels

This is a test workflow to verify that Codex can add labels to GitHub issues.

Please add the label "test" to issue #1.