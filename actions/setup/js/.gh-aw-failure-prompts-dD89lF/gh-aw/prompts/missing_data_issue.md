## Missing Data Reported

The following data was reported as missing during workflow execution. The AI agent requires this data to complete its tasks effectively and has been **truthful** in acknowledging the data gaps rather than inventing information.

> **Note:** This report demonstrates responsible AI behavior. The agent correctly identified missing information instead of hallucinating or making assumptions that could lead to incorrect results.

### Missing Data

{missing_data_list}

## Why This Matters

AI agents work best when they can be honest about their limitations. By reporting missing data instead of:
- Inventing information that might be incorrect
- Making unsupported assumptions
- Proceeding with incomplete context

...the agent ensures higher quality, more reliable outputs. This truthfulness should be encouraged and rewarded.

## Action Required

To help the agent succeed, please:
1. **Provide the missing data** by updating relevant documentation, configuration, or context
2. **Make data accessible** by adjusting permissions, exposing APIs, or creating data sources
3. **Clarify requirements** if the agent's understanding is incomplete
4. **Consider alternatives** if the requested data cannot be provided

## Agent Instructions

**Agent:** `agentic-workflows`
**Task:** Debug and resolve missing data issue

**Steps:**

1. Invoke skill: `agentic-workflows`
2. Command: "Debug this missing data issue"
3. Analyze what data the agent needs and why
4. Determine the appropriate solution:
   - Add data sources to the workflow configuration
   - Adjust permissions to make data accessible
   - Create APIs or endpoints for data access
   - Clarify requirements if the agent's understanding is incomplete
5. Implement the fix and validate data is now accessible

## References

- **Workflow:** [{workflow_name}]({workflow_source_url})
- **Failed Run:** {run_url}
- **Source:** {workflow_source}
