---
on:
  workflow_dispatch:
permissions:
  pull-requests: read
  contents: read
engine: claude
---

# Test Claude Create Pull Request

This is a test workflow to verify that Claude can create new pull requests.

Please create a new pull request that:
1. Creates a new branch called "test-branch"
2. Adds a simple README.md change
3. Creates a PR with the title "Test PR from Claude"
4. Includes a proper description explaining this is a test PR