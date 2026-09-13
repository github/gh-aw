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
