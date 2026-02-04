---
on:
  workflow_dispatch:
permissions:
  issues: read
engine: claude
---

# Test Claude Update Issue

This is a test workflow to verify that Claude can update existing GitHub issues.

Please update issue #1 by:
1. Changing the title to "Updated Test Issue"
2. Adding additional content to the body
3. Adding the label "updated" if it doesn't already exist