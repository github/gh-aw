---
description: Compact index for safe-output operations and runtime configuration in GitHub Agentic Workflows.
---

# Safe Outputs Index

Safe outputs are the write path for agentic workflows. Keep the main agent job read-only and use the focused reference files below.

| Topic | File |
|---|---|
| Issues, discussions, comments, pull requests, and review operations | [safe-outputs-content.md](safe-outputs-content.md) |
| Updates, labels, milestones, projects, releases, uploads, and delivery operations | [safe-outputs-management.md](safe-outputs-management.md) |
| Workflow dispatch, automation, code scanning, checks, agent sessions, and assignment flows | [safe-outputs-automation.md](safe-outputs-automation.md) |
| Runtime defaults, custom jobs, scripts, actions, global config, and output variables | [safe-outputs-runtime.md](safe-outputs-runtime.md) |

## Shared Rules

- Prefer the most specific built-in safe output before creating a custom job.
- Always scope mutating operations as tightly as possible.
- For pull-request or branch mutation, always restrict `allowed-files`.
- Use `noop` when no visible change is required after successful execution.
- `target:` is enforced, not merely validated: when set to `"triggering"` or a fixed number, any issue/PR number the agent supplies in its JSON output (including temporary-id references) is ignored in favor of the configured target. Only `target: "*"` lets the agent's own output select which issue/PR is affected.
- Each recorded call returns an `output_id` (`so_` + 12 URL-safe chars) that acknowledges recording, not success; it is distinct from `temporary_id` (`aw_…`), which references created resources. Pass one ID or a nonempty array of distinct IDs as `depends-on` to run an output only after every prerequisite succeeds. Failed, skipped, cancelled, warning, staged, or delegated prerequisites fail dependents (transitively) without executing; missing or deferred prerequisites remain waiting and are retried, allowing dependents to run only if they later succeed. Missing, ambiguous, or cyclic IDs fail closed. Scope is one processing pass (same Claim for queues), with no rollback. `submit_pull_request_review` and `create_pull_request_review_comment` can be prerequisites: their buffered review is submitted at the end of message processing, and dependents run only after it is actually published. `upload_code_coverage`, `dispatch_workflow`, `dispatch_repository`, and custom action outputs delegate their effects, so they never satisfy a prerequisite. Custom safe jobs, `call_workflow`, `push_repo_memory`, `upload_asset`, `noop`, and `report_*` diagnostics reject `depends-on`. No frontmatter is required.
