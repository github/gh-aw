### Workflow Failure

**Workflow:** [{workflow_name}]({workflow_source_url})  
**Branch:** {branch}  
**Run URL:** {run_url}{pull_request_info}

{secret_verification_context}{assignment_errors_context}{create_discussion_errors_context}{missing_safe_outputs_context}

### Action Required

Debug this workflow failure using the `agentic-workflows` agent:

```
/agent agentic-workflows
```

When prompted, instruct the agent to debug this workflow failure.
