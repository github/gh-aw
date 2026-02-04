---
on:
  workflow_dispatch:
permissions:
  contents: read
  pull-requests: read
engine: copilot
safe-outputs:
  max-patch-size: 512  # Limit patches to 512 KB for testing
  create-pull-request:
    title-prefix: "[PATCH-SIZE-TEST] "
    draft: true
    if-no-changes: "warn"
---

# Test Copilot Patch Size Validation

This is a test workflow to verify that the max-patch-size validation works correctly when creating pull requests.

Please:
1. Create a new file called `test-patch-size-demo.txt` with some content (keep it small - under 512 KB)
2. Make a few other small changes to existing files (add comments, update documentation, etc.)
3. Create a pull request with your changes
4. The workflow should succeed since the total patch size will be under the 512 KB limit

This workflow demonstrates the `max-patch-size` configuration under `safe-outputs` which prevents workflows from failing due to excessively large git patches. If the generated patch exceeds 512 KB, the workflow will fail with a clear error message showing the actual patch size vs. the maximum allowed.