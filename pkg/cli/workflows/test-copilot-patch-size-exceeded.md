---
on:
  workflow_dispatch:
permissions:
  contents: read
  pull-requests: read
engine: copilot
safe-outputs:
  max-patch-size: 1  # Very small limit (1 KB) to test failure case
  push-to-pull-request-branch:
    if-no-changes: "warn"
---

# Test Copilot Patch Size Limit Exceeded

This is a test workflow to verify that the max-patch-size validation correctly fails when patches exceed the configured limit.

**WARNING**: This workflow has a very small patch size limit (1 KB) and is designed to test the failure case.

Please:
1. Create multiple new files with significant content (each file should be several KB)
2. Make extensive changes to existing files 
3. Try to push these changes to a PR branch
4. The workflow should fail with an error message like: "Patch size (X KB) exceeds maximum allowed size (1 KB)"

This demonstrates how the `max-patch-size: 1` configuration under `safe-outputs` protects against large patches that could cause workflow failures or performance issues. The small limit ensures this test will fail unless only tiny changes are made.

You can compare this with the `test-copilot-max-patch-size.md` workflow which has a more reasonable 512 KB limit.