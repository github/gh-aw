---
"gh-aw": patch
---

Allow `safe-outputs.failure-issue-repo` to accept a GitHub Actions expression (e.g. `${{ inputs.failure-issue-repo }}`), in addition to a literal `owner/repo` string, so reusable workflows can let callers configure the failure-tracking repository via a `workflow_call` input. Documented that `report-failure-as-issue`, `report-failed-jobs`, and `failure-issue-repo` already support this pattern for dynamic per-caller configuration in reusable workflows.
