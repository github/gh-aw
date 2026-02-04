---
on:
  workflow_dispatch:
permissions:
  contents: read
  pull-requests: read
engine: claude
---

# Test Claude Push to PR Branch

This is a test workflow to verify that Claude can push changes to an existing pull request branch.

Please:
1. Find the latest open pull request
2. Create a small change (like adding a comment to a file)
3. Push the change to the PR branch
4. Add a comment to the PR explaining what was changed