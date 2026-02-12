---
on:
  workflow_dispatch:
engine: copilot
permissions:
  contents: read
safe-outputs:
  footer: false  # Global: hide footer for all safe outputs
  create-issue:
    title-prefix: "[global-off] "
    # Uses global footer: false
  create-pull-request:
    title-prefix: "[override-on] "
    footer: true  # Local override: show footer for PRs only
---

# Test Global Footer with Override

Demonstrates global footer control with local override:

1. **Global setting**: `safe-outputs.footer: false` hides footers for all outputs
2. **Local override**: `create-pull-request.footer: true` shows footer only for PRs

Create two outputs to demonstrate:
- An issue with title "[global-off] Test issue" (no footer)
- A note that if this were creating a PR, it would have a footer due to the override
