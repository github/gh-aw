---
on:
  workflow_dispatch:
engine: copilot
permissions:
  contents: read
safe-outputs:
  create-issue:
    title-prefix: "[test-footer] "
    footer: false
---

# Test Footer Disabled in Create Issue

Create a test issue with `footer: false` to demonstrate that:
1. The visible AI-generated footer is omitted
2. XML markers (workflow-id, tracker-id) are still included
3. The issue is searchable via workflow-id

Create an issue with:
- **Title**: "Test issue without footer"
- **Body**: "This issue should not have a visible AI-generated footer, but should still have XML markers for searchability."
