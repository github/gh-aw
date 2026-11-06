---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
safe-outputs:
  create-issue:
jobs:
  eligibility:
    runs-on: ubuntu-latest
    outputs:
      pr_number: ${{ steps.metadata.outputs.pr_number }}
      head_sha: ${{ steps.metadata.outputs.head_sha }}
    steps:
      - id: metadata
        run: |
          echo 'pr_number=123' >> "$GITHUB_OUTPUT"
          echo 'head_sha=0123456789012345678901234567890123456789' >> "$GITHUB_OUTPUT"
  flag_failed_review:
    runs-on: ubuntu-latest
    needs: [eligibility, agent, detection, safe_outputs]
    detection-gate: status-only
    if: always()
    steps:
      - env:
          PR_NUMBER: ${{ needs.eligibility.outputs.pr_number }}
          HEAD_SHA: ${{ needs.eligibility.outputs.head_sha }}
          FAILED: ${{ needs.agent.result != 'success' || needs.safe_outputs.result != 'success' || needs.safe_outputs.outputs.process_safe_outputs_items_failed != '0' }}
        run: printf '%s %s %s\n' "$PR_NUMBER" "$HEAD_SHA" "$FAILED"
---
Report the workflow result.
