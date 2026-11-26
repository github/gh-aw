---
# Bundle for daily/scheduled code quality workflows that create GitHub issues.
# Bundles: activation-app + reporting guidelines + standardized create-issue safe-outputs.
#
# Usage:
#   imports:
#     - uses: shared/daily-issue-base.md
#       with:
#         title-prefix: "[my-workflow] "
#         expires: "2d"      # optional, default: 2d
#         labels: [automation, cookie]
#         assignees: [copilot]  # optional, default: []
# New issues start in the native Backlog planning status.

import-schema:
  title-prefix:
    type: string
    required: true
    description: "Title prefix for created issues, e.g. '[my-workflow] '"
  expires:
    type: string
    default: "2d"
    description: "How long to keep issues before expiry"
  labels:
    type: array
    default: [automated-analysis, cookie]
    description: "Labels to apply to created issues"
  assignees:
    type: array
    default: []
    description: "Assignees for created issues"

imports:
  - shared/activation-app.md
  - shared/reporting.md

safe-outputs:
  create-issue:
    expires: ${{ github.aw.import-inputs.expires }}
    title-prefix: "${{ github.aw.import-inputs.title-prefix }}"
    labels: ${{ github.aw.import-inputs.labels }}
    assignees: ${{ github.aw.import-inputs.assignees }}
    planning-status: Backlog
    allowed-statuses: [Backlog]
    max: 1
  noop:
---
