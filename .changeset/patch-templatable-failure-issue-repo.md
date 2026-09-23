---
"gh-aw": patch
---

Allow `safe-outputs.failure-issue-repo` to accept a GitHub Actions expression (e.g. `${{ inputs.failure-issue-repo }}`), in addition to a literal `owner/repo` string, so reusable workflows can let callers configure the failure-tracking repository via a `workflow_call` input. Documented that `report-failure-as-issue`, `report-failed-jobs`, and `failure-issue-repo` already support this pattern for dynamic per-caller configuration in reusable workflows. Expression-derived values are resolved at runtime from caller-supplied data, so they are validated against an allowlist scoped to the current repository owner before any issue API call; literal values keep their compile-time trust.
