# MCP Server Logs Guardrail

This document describes the output size guardrail implemented for the MCP server's `logs` command.

## Problem

When using the MCP server to fetch workflow logs, the output can exceed GitHub's API limits, especially when:
- Fetching logs for many workflow runs
- Runs contain extensive tool usage data
- Multiple workflows are being analyzed

Large outputs can:
- Exceed token limits in AI models
- Cause performance issues in MCP clients
- Make it difficult to process and understand the data

## Solution

The MCP server `logs` command now includes an automatic guardrail that:

1. **Checks output size** before returning results
2. **Triggers at 12000 tokens** (default, configurable)
3. **Returns helpful guidance** instead of large payloads

## How It Works

### Normal Operation (Output ≤ Token Limit)

When the output is within the token limit, the command returns the full JSON data as usual:

```json
{
  "summary": {
    "total_runs": 5,
    "total_duration": "2h30m",
    "total_tokens": 45000,
    "total_cost": 0.23
  },
  "runs": [...],
  "tool_usage": [...],
  ...
}
```text

### Guardrail Triggered (Output > Token Limit)

When the output exceeds the token limit (default: 12000 tokens), the command returns a structured response with:

```json
{
  "message": "⚠️  Output size (15000 tokens) exceeds the limit (12000 tokens). To reduce output size, use the 'jq' parameter with one of the suggested queries below.",
  "output_tokens": 15000,
  "output_size_limit": 12000,
  "schema": {
    "description": "Complete structured data for workflow logs",
    "type": "object",
    "fields": {
      "summary": {
        "type": "object",
        "description": "Aggregate metrics across all runs (total_runs, total_duration, total_tokens, total_cost, total_turns, total_errors, total_warnings, total_missing_tools)"
      },
      "runs": {
        "type": "array",
        "description": "Array of workflow run data (database_id, workflow_name, agent, status, conclusion, duration, token_usage, estimated_cost, turns, error_count, warning_count, missing_tool_count, created_at, url, logs_path, event, branch)"
      },
      ...
    }
  },
  "suggested_queries": [
    {
      "description": "Get only the summary statistics",
      "query": ".summary",
      "example": "Use jq parameter: \".summary\""
    },
    {
      "description": "Get list of run IDs and workflow names",
      "query": ".runs | map({database_id, workflow_name, status})",
      "example": "Use jq parameter: \".runs | map({database_id, workflow_name, status})\""
    },
    ...
  ]
}
```text

## Configuring the Token Limit

The guardrail uses a token-based limit instead of byte-based. By default, the limit is 12000 tokens (approximately 48KB of text).

You can customize the limit using the `max_tokens` parameter:

```json
{
  "name": "logs",
  "arguments": {
    "count": 100,
    "max_tokens": 20000
  }
}
```text

**Token Estimation**: The system uses approximately 4 characters per token as an estimation (OpenAI's rule of thumb).

## Using the jq Parameter

The `jq` parameter allows you to filter the output using jq syntax. Here are the suggested queries:

### 1. Get Only Summary Statistics

```json
{
  "jq": ".summary"
}
```text

Returns just the aggregate metrics without individual run data.

### 2. Get Run IDs and Basic Info

```json
{
  "jq": ".runs | map({database_id, workflow_name, status})"
}
```text

Returns a simplified list of runs with just the essential fields.

### 3. Get Only Failed Runs

```json
{
  "jq": ".runs | map(select(.conclusion == \"failure\"))"
}
```text

Filters to show only runs that failed.

### 4. Get Summary with First N Runs

```json
{
  "jq": "{summary, runs: .runs[:5]}"
}
```text

Returns summary plus the first 5 runs only.

### 5. Get Error and Warning Summaries

```json
{
  "jq": "{errors_and_warnings, missing_tools, mcp_failures}"
}
```text

Returns only the diagnostic information.

### 6. Get Tool Usage Statistics

```json
{
  "jq": ".tool_usage"
}
```text

Returns aggregated tool usage data.

### 7. Get High Token Usage Runs

```json
{
  "jq": ".runs | map(select(.token_usage > 10000))"
}
```text

Filters to show only runs with high token usage.

### 8. Get Runs from Specific Workflow

```json
{
  "jq": ".runs | map(select(.workflow_name == \"YOUR_WORKFLOW_NAME\"))"
}
```text

Filters to show runs from a specific workflow.

## Implementation Details

### Constants

- `DefaultMaxMCPLogsOutputTokens`: 12000 tokens (default limit)
- `CharsPerToken`: 4 characters per token (estimation factor)

### Files

- `pkg/cli/mcp_logs_guardrail.go` - Core guardrail implementation
- `pkg/cli/mcp_logs_guardrail_test.go` - Unit tests
- `pkg/cli/mcp_logs_guardrail_integration_test.go` - Integration tests
- `pkg/cli/mcp_server.go` - Integration with MCP server

### Functions

- `estimateTokens(text string) int` - Estimates token count from text
- `checkLogsOutputSize(outputStr string, maxTokens int) (string, bool)` - Main guardrail function
- `getLogsDataSchema() LogsDataSchema` - Returns schema description
- `getSuggestedJqQueries() []SuggestedJqQuery` - Returns suggested jq filters

### Testing

Run the tests with:

```bash
# Unit tests
go test -v ./pkg/cli/mcp_logs_guardrail_test.go ./pkg/cli/mcp_logs_guardrail.go ./pkg/cli/jq.go

# Integration tests
go test -v -tags integration -run "TestMCPServer_LogsGuardrail" ./pkg/cli/

# All tests
make test-unit
```text

## Benefits

1. **Prevents overwhelming responses** - Keeps output manageable for AI models
2. **Provides guidance** - Suggests specific filters to get the data you need
3. **Self-documenting** - Returns the schema so you know what fields are available
4. **Preserves functionality** - jq filtering works the same as before
5. **Transparent** - Clear messaging about why guardrail triggered

## Future Enhancements

Potential improvements:

- Make the size limit configurable via parameter
- Add more sophisticated query suggestions based on output content
- Provide automatic chunking for very large datasets
- Add content-aware compression (e.g., gzip) for JSON responses to reduce transfer size while maintaining full data access
