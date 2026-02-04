---
# Shared: Orchestration / Delegation patterns
#
# Import this file when your workflow delegates work to:
# - a coding agent (`assign-to-agent`), and/or
# - a specialized worker workflow (`dispatch-workflow`).
---

## Orchestration / Delegation

Use these patterns when an orchestrator workflow needs to delegate work:

- **Assign to an AI coding agent** (`assign-to-agent`) when you already have an issue/PR that describes a concrete unit of work.
- **Dispatch a specialized worker workflow** (`dispatch-workflow`) when you want a repeatable, scoped worker with its own prompt/tools/permissions.

You can combine them (dispatch a worker that then assigns an agent).

### Correlation IDs (recommended)

Always include at least one stable correlation identifier in delegated work:

- `tracker_issue_number`
- `bundle_key` (for example `npm :: package-lock.json`)
- `run_id` (`${{ github.run_id }}`) or a custom string `run-YYYY-MM-DD-###`

### Assign-to-agent

Frontmatter:

```yaml wrap
safe-outputs:
  assign-to-agent:
    name: "copilot"          # default agent (optional)
    allowed: [copilot]       # optional allowlist
    target: "*"             # "triggering" (default), "*", or number
    max: 10
```

Agent output:

```text
assign_to_agent(issue_number=123, agent="copilot")

# Works with temporary IDs too (same run)
assign_to_agent(issue_number="aw_abc123def456", agent="copilot")
```

Notes:
- Requires `GH_AW_AGENT_TOKEN` for automated assignment.
- Temporary IDs (`aw_...`) are supported for `issue_number`.

### Dispatch-workflow

Frontmatter:

```yaml wrap
safe-outputs:
  dispatch-workflow:
    workflows: [worker-a, worker-b]
    max: 10
```

Notes:
- Each worker must exist and support `workflow_dispatch`.
- Define explicit `workflow_dispatch.inputs` on workers so dispatch tools get the correct schema.

Example calls (preferred): call the generated tool for the worker.

If your workflow allowlists `worker-a`, the generated tool name will be `worker_a` (hyphens become underscores):

```javascript
worker_a({
  tracker_issue: 123,
  work_item_id: "item-001",
  dry_run: false
})
```

Equivalent agent output format:

```json
{
  "type": "dispatch_workflow",
  "workflow_name": "worker-a",
  "inputs": {
    "tracker_issue": "123",
    "work_item_id": "item-001",
    "dry_run": "false"
  }
}
```
