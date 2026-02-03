package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var mcpLogsGuardrailLog = logger.New("cli:mcp_logs_guardrail")

const (
	// DefaultMaxMCPLogsOutputTokens is the default maximum number of tokens for MCP logs output
	// before triggering the guardrail (12000 tokens)
	DefaultMaxMCPLogsOutputTokens = 12000

	// CharsPerToken is the approximate number of characters per token
	// Using OpenAI's rule of thumb: ~4 characters per token
	CharsPerToken = 4
)

// MCPLogsGuardrailResponse represents the response when output is too large
type MCPLogsGuardrailResponse struct {
	Message          string             `json:"message"`
	OutputTokens     int                `json:"output_tokens"`
	OutputSizeLimit  int                `json:"output_size_limit"`
	Schema           LogsDataSchema     `json:"schema"`
	SuggestedQueries []SuggestedJqQuery `json:"suggested_queries"`
}

// LogsDataSchema describes the structure of the full logs output
type LogsDataSchema struct {
	Description string                 `json:"description"`
	Type        string                 `json:"type"`
	Fields      map[string]SchemaField `json:"fields"`
}

// SchemaField describes a field in the schema
type SchemaField struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

// SuggestedJqQuery represents a suggested jq filter
type SuggestedJqQuery struct {
	Description string `json:"description"`
	Query       string `json:"query"`
	Example     string `json:"example,omitempty"`
}

// estimateTokens estimates the number of tokens in a string
// Using the approximation: ~4 characters per token
func estimateTokens(text string) int {
	return len(text) / CharsPerToken
}

// checkLogsOutputSize checks if the logs output exceeds the token limit
// and returns a guardrail response if it does
func checkLogsOutputSize(outputStr string, maxTokens int) (string, bool) {
	if maxTokens == 0 {
		maxTokens = DefaultMaxMCPLogsOutputTokens
	}

	outputTokens := estimateTokens(outputStr)
	mcpLogsGuardrailLog.Printf("Checking logs output size: tokens=%d, limit=%d", outputTokens, maxTokens)

	if outputTokens <= maxTokens {
		mcpLogsGuardrailLog.Print("Output size within limits")
		return outputStr, false
	}

	mcpLogsGuardrailLog.Printf("Output exceeds limit, generating guardrail response")

	// Generate guardrail response
	guardrail := MCPLogsGuardrailResponse{
		Message: fmt.Sprintf(
			"⚠️  Output size (%d tokens) exceeds the limit (%d tokens). "+
				"To reduce output size, use the 'jq' parameter with one of the suggested queries below.",
			outputTokens,
			maxTokens,
		),
		OutputTokens:     outputTokens,
		OutputSizeLimit:  maxTokens,
		Schema:           getLogsDataSchema(),
		SuggestedQueries: getSuggestedJqQueries(),
	}

	// Marshal to JSON
	guardrailJSON, err := json.MarshalIndent(guardrail, "", "  ")
	if err != nil {
		mcpLogsGuardrailLog.Printf("Failed to marshal guardrail response: %v", err)
		// Fallback to simple text message if JSON marshaling fails
		return fmt.Sprintf(
			"Output size (%d tokens) exceeds the limit (%d tokens). "+
				"Please use the 'jq' parameter to filter the output.",
			outputTokens,
			maxTokens,
		), true
	}

	mcpLogsGuardrailLog.Printf("Generated guardrail response with %d suggested queries", len(guardrail.SuggestedQueries))
	return string(guardrailJSON), true
}

// getLogsDataSchema returns the schema for LogsData
func getLogsDataSchema() LogsDataSchema {
	return LogsDataSchema{
		Description: "Complete structured data for workflow logs",
		Type:        "object",
		Fields: map[string]SchemaField{
			"summary": {
				Type:        "object",
				Description: "Aggregate metrics across all runs (total_runs, total_duration, total_tokens, total_cost, total_turns, total_errors, total_warnings, total_missing_tools)",
			},
			"runs": {
				Type:        "array",
				Description: "Array of workflow run data (database_id, workflow_name, agent, status, conclusion, duration, token_usage, estimated_cost, turns, error_count, warning_count, missing_tool_count, created_at, url, logs_path, event, branch)",
			},
			"tool_usage": {
				Type:        "array",
				Description: "Tool usage statistics (name, total_calls, runs, max_output_size, max_duration)",
			},
			"errors_and_warnings": {
				Type:        "array",
				Description: "Error and warning summaries (type, message, count, engine, run_id, run_url, workflow_name, pattern_id)",
			},
			"missing_tools": {
				Type:        "array",
				Description: "Missing tool reports (tool, count, workflows, first_reason, run_ids)",
			},
			"mcp_failures": {
				Type:        "array",
				Description: "MCP server failure summaries (server_name, count, workflows, run_ids)",
			},
			"access_log": {
				Type:        "object",
				Description: "Access log analysis (total_requests, allowed_count, blocked_count, allowed_domains, blocked_domains, by_workflow)",
			},
			"firewall_log": {
				Type:        "object",
				Description: "Firewall log analysis (total_requests, allowed_requests, blocked_requests, allowed_domains, blocked_domains, requests_by_domain, by_workflow)",
			},
			"continuation": {
				Type:        "object",
				Description: "Parameters to continue querying when timeout is reached (message, workflow_name, count, start_date, end_date, engine, branch, after_run_id, before_run_id, timeout)",
			},
			"logs_location": {
				Type:        "string",
				Description: "File system path where logs were downloaded",
			},
		},
	}
}

// getSuggestedJqQueries returns a list of useful jq queries to reduce output
func getSuggestedJqQueries() []SuggestedJqQuery {
	return []SuggestedJqQuery{
		{
			Description: "Get only the summary statistics",
			Query:       ".summary",
			Example:     "Use jq parameter: \".summary\"",
		},
		{
			Description: "Get list of run IDs and workflow names",
			Query:       ".runs | map({database_id, workflow_name, status})",
			Example:     "Use jq parameter: \".runs | map({database_id, workflow_name, status})\"",
		},
		{
			Description: "Get only failed runs",
			Query:       ".runs | map(select(.conclusion == \"failure\"))",
			Example:     "Use jq parameter: \".runs | map(select(.conclusion == \\\"failure\\\"))\"",
		},
		{
			Description: "Get summary with first 5 runs",
			Query:       "{summary, runs: .runs[:5]}",
			Example:     "Use jq parameter: \"{summary, runs: .runs[:5]}\"",
		},
		{
			Description: "Get only error and warning summaries",
			Query:       "{errors_and_warnings, missing_tools, mcp_failures}",
			Example:     "Use jq parameter: \"{errors_and_warnings, missing_tools, mcp_failures}\"",
		},
		{
			Description: "Get tool usage statistics",
			Query:       ".tool_usage",
			Example:     "Use jq parameter: \".tool_usage\"",
		},
		{
			Description: "Get runs with high token usage (>10k tokens)",
			Query:       ".runs | map(select(.token_usage > 10000))",
			Example:     "Use jq parameter: \".runs | map(select(.token_usage > 10000))\"",
		},
		{
			Description: "Get runs from specific workflow",
			Query:       ".runs | map(select(.workflow_name == \"YOUR_WORKFLOW_NAME\"))",
			Example:     "Use jq parameter: \".runs | map(select(.workflow_name == \\\"YOUR_WORKFLOW_NAME\\\"))\"",
		},
	}
}

// formatGuardrailMessage creates a user-friendly text message from the guardrail response
func formatGuardrailMessage(guardrail MCPLogsGuardrailResponse) string {
	var builder strings.Builder

	builder.WriteString(guardrail.Message)
	builder.WriteString("\n\n")

	builder.WriteString("📋 Output Schema:\n")
	fmt.Fprintf(&builder, "  Type: %s\n", guardrail.Schema.Type)
	fmt.Fprintf(&builder, "  Description: %s\n\n", guardrail.Schema.Description)

	builder.WriteString("Available fields:\n")
	for field, schema := range guardrail.Schema.Fields {
		fmt.Fprintf(&builder, "  - %s (%s): %s\n", field, schema.Type, schema.Description)
	}

	builder.WriteString("\n💡 Suggested jq Queries:\n")
	for i, query := range guardrail.SuggestedQueries {
		fmt.Fprintf(&builder, "  %d. %s\n", i+1, query.Description)
		fmt.Fprintf(&builder, "     Query: %s\n", query.Query)
		if query.Example != "" {
			fmt.Fprintf(&builder, "     %s\n", query.Example)
		}
		builder.WriteString("\n")
	}

	return builder.String()
}
