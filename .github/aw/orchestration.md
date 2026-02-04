---
description: Orchestration and delegation patterns for agentic workflows
---

# Orchestration / Delegation Patterns

When designing workflows that coordinate multiple agents or other workflows, use these orchestration patterns.

## When to Use Orchestration

Use orchestration patterns when designing workflows that:

- Coordinate multiple agents working on related tasks
- Dispatch work to specialized worker workflows
- Break down complex tasks into manageable units
- Need to track and correlate related work items

## Core Delegation Patterns

### 1. Assign to AI Coding Agent

Use `assign-to-agent` when you have an issue or PR that describes a concrete unit of work and want to delegate it to an AI coding agent.

**Best for**: Code changes, bug fixes, feature implementation, refactoring

### 2. Dispatch Specialized Worker Workflow

Use `dispatch-workflow` when you want a repeatable, scoped worker with its own dedicated prompt, tools, and permissions.

**Best for**: Repeated analysis tasks, scheduled reports, multi-step workflows with specific requirements

### 3. Combined Approach

You can combine these patterns: dispatch a worker workflow that then assigns work to agents based on analysis results.

## Configuration Best Practices

### Correlation IDs (Strongly Recommended)

Always include at least one stable correlation identifier in delegated work to track related tasks and workflow executions:

- **`tracker_issue_number`**: Link work items to a tracking issue
- **`bundle_key`**: Group related items (e.g., `"npm :: package-lock.json"`)
- **`run_id`**: Use `${{ github.run_id }}` or a custom identifier (e.g., `"run-2026-02-04-001"`)

Correlation IDs enable:
- Progress tracking across multiple workflow runs
- Debugging and troubleshooting complex orchestrations
- Analytics and reporting on workflow performance
- Avoiding duplicate work

## Assign-to-Agent Configuration

**Frontmatter setup:**

```yaml wrap
safe-outputs:
  assign-to-agent:
    name: "copilot"          # default agent (optional)
    allowed: [copilot]       # optional allowlist
    target: "*"             # "triggering" (default), "*", or number
    max: 10
```

**Agent output format:**

```text
assign_to_agent(issue_number=123, agent="copilot")

# Works with temporary IDs too (same run)
assign_to_agent(issue_number="aw_abc123def456", agent="copilot")
```

**Notes:**
- Requires `GH_AW_AGENT_TOKEN` environment variable for automated agent assignment
- Temporary IDs (`aw_...`) are supported for `issue_number` (within the same workflow run)
- Use `target: "*"` to assign any issue, or `"triggering"` (default) to only assign the triggering issue

## Dispatch-Workflow Configuration

**Frontmatter setup:**

```yaml wrap
safe-outputs:
  dispatch-workflow:
    workflows: [worker-a, worker-b]
    max: 10
```

**Notes:**
- Each worker workflow must exist in `.github/workflows/` and support `workflow_dispatch` trigger
- Define explicit `workflow_dispatch.inputs` on worker workflows so dispatch tools get the correct schema
- Worker workflows receive inputs as strings (convert booleans/numbers as needed)

**Example: Calling dispatched workflows**

Preferred approach - use the generated tool for the worker (hyphens become underscores):

```javascript
// If your workflow allowlists "worker-a", the tool name is "worker_a"
worker_a({
  tracker_issue: 123,
  work_item_id: "item-001",
  dry_run: false
})
```

Alternative: Equivalent JSON format for agent output:

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

## Design Guidelines for Orchestrator Workflows

When creating orchestrator workflows, follow these best practices:

1. **Clear Separation of Concerns**: Orchestrators should coordinate, not do heavy processing themselves
2. **Explicit Dependencies**: Document which workers/agents are expected to be available
3. **Error Handling**: Plan for worker failures and implement retry or fallback strategies
4. **Progress Tracking**: Use correlation IDs and status updates to track orchestration progress
5. **Idempotency**: Design workflows to be safely re-runnable without causing duplicate work
6. **Observability**: Log orchestration decisions and delegation events for debugging

## Common Orchestration Patterns

### Pattern: Issue Triage and Assignment
1. Orchestrator analyzes incoming issues
2. Classifies and labels them
3. Assigns to appropriate agent based on issue type

### Pattern: Bulk Processing with Workers
1. Orchestrator queries for work items
2. Dispatches specialized workers for each item
3. Collects results and generates summary report

### Pattern: Sequential Delegation
1. First agent performs analysis and creates issue
2. Orchestrator assigns issue to second agent for implementation
3. Third agent reviews and merges changes

## References

- For full configuration options, see: [github-agentic-workflows.md](https://github.com/github/gh-aw/blob/main/.github/aw/github-agentic-workflows.md)
- For safe-outputs documentation, see the `safe-outputs:` section in the main configuration guide
