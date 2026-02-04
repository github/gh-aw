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

# Test Codex Add Issue Comment

This is a test workflow to verify that Codex can add comments to GitHub issues.

Please add a comment to issue #1 saying "This is a test comment from the Codex workflow."