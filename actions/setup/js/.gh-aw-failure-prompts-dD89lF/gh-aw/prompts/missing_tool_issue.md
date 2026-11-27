## Missing Tools Reported

The following tools were reported as missing during workflow execution. These tools are needed for the agent to complete its tasks effectively.

### Missing Tools

{missing_tools_list}

## Action Required

Please investigate why these tools are missing and either:
1. Add the missing tools to the agent's configuration
2. Update the workflow to use available alternatives
3. Document why these tools are intentionally unavailable

## Agent Instructions

**Agent:** `agentic-workflows`
**Task:** Debug and resolve missing tool issue

**Steps:**

1. Invoke skill: `agentic-workflows`
2. Command: "Debug this missing tool issue"
3. Analyze which tools are missing and why they're needed
4. Determine the appropriate solution:
   - Add missing tools to the workflow configuration
   - Update the workflow to use available alternatives
   - Install required MCP servers or dependencies
   - Document why certain tools are intentionally unavailable
5. Implement the fix and validate tools are now accessible

## References

- **Workflow:** [{workflow_name}]({workflow_source_url})
- **Failed Run:** {run_url}
- **Source:** {workflow_source}
