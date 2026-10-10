package cli

import (
	"fmt"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/setutil"
)

var mcpToolTableLog = logger.New("cli:mcp_tool_table")

// MCPToolTableOptions configures how the MCP tool table is rendered
type MCPToolTableOptions struct {
	// TruncateLength is the maximum length for tool descriptions before truncation
	// A value of 0 means no truncation
	TruncateLength int
	// ShowSummary controls whether to display the summary line at the bottom
	ShowSummary bool
	// SummaryFormat is the format string for the summary (default: "📊 Summary: %d allowed, %d not allowed out of %d total tools\n")
	SummaryFormat string
	// ShowVerboseHint controls whether to show the "Run with --verbose" hint in non-verbose mode
	ShowVerboseHint bool
}

// renderMCPToolTable renders an MCP tool table with configurable options
// This is the shared rendering logic used by both mcp list-tools and mcp inspect commands
func renderMCPToolTable(info *parser.MCPServerInfo, opts MCPToolTableOptions) string {
	mcpToolTableLog.Printf("Rendering MCP tool table: server=%s, tool_count=%d, truncate=%d",
		info.Config.Name, len(info.Tools), opts.TruncateLength)

	if len(info.Tools) == 0 {
		mcpToolTableLog.Print("No tools to render")
		return ""
	}

	// Create a map for quick lookup of allowed tools from workflow configuration
	allowedMap := make(map[string]struct {
	})

	// Check for wildcard "*" which means all tools are allowed
	hasWildcard := false
	for _, allowed := range info.Config.Allowed {
		if allowed == "*" {
			hasWildcard = true
		}
		allowedMap[allowed] = struct {
		}{}
	}

	mcpToolTableLog.Printf("Tool permissions: has_wildcard=%v, allowed_count=%d", hasWildcard, len(allowedMap))

	// Build table headers and rows
	headers := []string{"Tool Name", "Allow", "Description"}
	rows := make([][]string, 0, len(info.Tools))

	for _, tool := range info.Tools {
		description := tool.Description

		// Apply truncation if requested
		if opts.TruncateLength > 0 && len(description) > opts.TruncateLength {
			// Leave room for "..."
			truncateAt := opts.TruncateLength - 3
			if truncateAt > 0 {
				description = description[:truncateAt] + "..."
			}
		}

		// Determine status
		status := "🚫"
		if len(info.Config.Allowed) == 0 || hasWildcard {
			// If no allowed list is specified or "*" wildcard is present, assume all tools are allowed
			status = "✅"
		} else if setutil.Contains(allowedMap, tool.Name) {
			status = "✅"
		}

		rows = append(rows, []string{tool.Name, status, description})
	}

	// Render the table
	table := console.RenderTableStdout(console.TableConfig{
		Headers: headers,
		Rows:    rows,
	})

	result := table

	// Add summary if requested
	if opts.ShowSummary {
		allowedCount := 0
		for _, tool := range info.Tools {
			if len(info.Config.Allowed) == 0 || hasWildcard || setutil.Contains(allowedMap, tool.Name) {
				allowedCount++
			}
		}

		summaryFormat := opts.SummaryFormat
		if summaryFormat == "" {
			summaryFormat = "\n📊 Summary: %d allowed, %d not allowed out of %d total tools\n"
		}

		result += fmt.Sprintf(summaryFormat,
			allowedCount, len(info.Tools)-allowedCount, len(info.Tools))
	}

	// Add verbose hint if requested
	if opts.ShowVerboseHint {
		result += "\nRun with --verbose for detailed information\n"
	}

	return result
}
