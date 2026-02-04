---
on:
  slash_command:
    name: test-noop
  reaction: eyes
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
engine: copilot
safe-outputs:
  noop:
    max: 5
timeout-minutes: 5
---

# Test No-Op Safe Output

Test the noop safe output functionality.

Create noop outputs with transparency messages:
- "Analysis complete - no issues found"
- "Code review passed - all checks successful"
- "No changes needed - everything looks good"

Output as JSONL format.
