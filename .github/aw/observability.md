---
description: Observability and debugging best practices for agentic workflows
---

# Observability Best Practices for Agentic Workflows

When designing workflows, build in observability from the start to enable effective debugging and monitoring.

## Why Observability Matters

Agentic workflows involve complex interactions between:
- AI models making decisions
- Tool calls to external services
- GitHub API operations
- MCP server communications
- Network requests and data processing

Without proper observability, debugging failures becomes extremely difficult. Good observability enables:

1. **Quick Failure Diagnosis**: Understand what went wrong and why
2. **Performance Optimization**: Identify bottlenecks and slow operations
3. **Security Monitoring**: Detect unauthorized access attempts or data leaks
4. **Usage Analytics**: Track tool usage patterns and workflow effectiveness

## Core Observability Components

### 1. Structured Logging

**Use structured output formats** that can be easily parsed and analyzed:

```yaml
# Good: Structured safe-output logs
safe-outputs:
  create-discussion:
    category: "reports"
    title-prefix: "[Analysis] "
```

**Log key events** in your workflow instructions:
- Tool invocations and their results
- Decision points and rationale
- Error conditions and recovery attempts
- Performance metrics (timing, counts)

### 2. Status Reporting

**Report progress** at meaningful milestones:

```markdown
## Your Task

1. Analyze the issue
   - Log: "Starting analysis of issue #123"
2. Generate recommendations
   - Log: "Generated 5 recommendations"
3. Create summary
   - Log: "Creating discussion with findings"
```

### 3. Error Context

**When errors occur**, provide rich context:

```markdown
If an operation fails:
1. Log the specific error message
2. Include the operation being attempted
3. Capture relevant input data
4. Note any retry attempts
5. Explain the impact on workflow success
```

## Observability Features in gh-aw

### Safe Outputs Logs

Every safe-output call is automatically logged to `safe_outputs.jsonl`:

```yaml
safe-outputs:
  create-issue:
    max: 10
  create-discussion:
    max: 5
  noop:
    max: 1  # Always include noop for "nothing to do" cases
```

**Best practice**: Always include a `noop` safe-output to explicitly report when no action was needed.

### Audit Logs

Workflow audit logs capture:
- Tool availability and usage
- Missing tool calls (agent tried but tool not configured)
- Network access patterns
- Permission usage
- Execution timing

Access via: `gh aw audit <run-id>` or the `agentic-workflows` MCP server

### MCP Gateway Logs

When using MCP servers, the gateway logs:
- Server lifecycle (start, stop, errors)
- Tool invocations and responses
- Authentication and authorization
- Performance metrics

### AWF Firewall Logs

Network requests are logged by the firewall:
- Allowed and denied requests
- Domain access patterns
- Request timing and sizes
- Security violations

## Designing Observable Workflows

### 1. Explicit Success and Failure Paths

```markdown
## Expected Outcomes

**Success**: Create a discussion with findings
**Partial Success**: Create discussion noting incomplete data
**Failure**: Report error via noop safe-output
**Nothing to Do**: Call noop("No issues found in analysis period")
```

### 2. Correlation IDs

Use correlation identifiers to track related workflow executions:

```yaml
# In frontmatter
tracker-id: daily-report

# In workflow body
Include run ID: ${{ github.run_id }}
Link to tracking issue: #<issue-number>
```

### 3. Metrics and Summaries

Include quantitative metrics in outputs:

```markdown
## Report Format

Include these metrics:
- Total items processed: <count>
- Success rate: <percentage>
- Average processing time: <duration>
- Tools used: <list>
- Errors encountered: <count>
```

### 4. Progressive Disclosure

For complex reports, use collapsible sections:

```markdown
<details>
<summary><b>Detailed Analysis</b></summary>

[Long detailed content...]

</details>
```

## Debugging Workflows

### Using gh aw audit

```bash
# Audit a specific run
gh aw audit <run-id>

# Check for missing tools
gh aw audit <run-id> --json | jq '.missing_tools'

# Review safe-outputs
gh aw audit <run-id> --json | jq '.safe_outputs'
```

### Using gh aw logs

```bash
# Download logs for a workflow
gh aw logs <workflow-name>

# Download recent logs
gh aw logs <workflow-name> --count 10

# Parse logs for analysis
gh aw logs <workflow-name> --parse
```

### Common Debugging Patterns

**Missing Tool Calls**: Agent mentions a tool but doesn't call it
- Check: Is tool configured in frontmatter?
- Check: Does agent have permission to use tool?
- Check: Is tool name spelled correctly?

**Network Failures**: Requests to external services fail
- Check: Is domain in `network.allowed` list?
- Check: AWF firewall logs for denials
- Check: MCP server logs for errors

**Permission Errors**: GitHub API calls fail
- Check: Workflow has required permission in frontmatter
- Check: Token has appropriate scope
- Check: Repository/organization settings allow the operation

## Monitoring and Alerting

### Workflow Health Metrics

Track these metrics over time:
- Success rate by workflow
- Average execution time
- Tool usage patterns
- Error frequencies
- Network access patterns

### Alert Conditions

Consider alerting on:
- Success rate drops below threshold
- Execution time exceeds expected range
- Repeated missing tool errors
- Security violations (unexpected network access)
- Resource exhaustion (safe-output limits hit)

## Best Practices Summary

1. **Log Liberally**: Include progress updates and decision rationale
2. **Structure Output**: Use consistent formats for easy parsing
3. **Include Context**: Capture relevant data with errors
4. **Use Correlation IDs**: Track related workflow executions
5. **Report All Outcomes**: Including "nothing to do" cases
6. **Make Reports Scannable**: Use headers, summaries, and progressive disclosure
7. **Test Observability**: Verify logs are useful before failures occur
8. **Document Expected Behavior**: Clarify what success looks like

## Tools for Observability

### agentic-workflows MCP Server

Provides tools for:
- `status`: List workflows and recent run status
- `logs`: Download and parse workflow logs
- `audit`: Detailed analysis of workflow runs
- `compile`: Validate workflow configuration

### GitHub MCP Server

Access workflow run data:
- Workflow run history
- Job and step logs
- Artifact downloads
- Check run status

### repo-memory Tool

Store observability data across runs:
- Historical metrics
- Baseline measurements
- Known issue patterns
- Performance trends

## References

- For tool configuration, see: [github-agentic-workflows.md](https://github.com/github/gh-aw/blob/main/.github/aw/github-agentic-workflows.md)
- For debugging guide, see: [debug-agentic-workflow.md](https://github.com/github/gh-aw/blob/main/.github/aw/debug-agentic-workflow.md)
- For metrics glossary, see: `scratchpad/metrics-glossary.md` in the repository
