package cli

import (
	"fmt"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
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

// DefaultMCPToolTableOptions returns the default options for rendering MCP tool tables
func DefaultMCPToolTableOptions() MCPToolTableOptions {
	return MCPToolTableOptions{
		TruncateLength:  60,
		ShowSummary:     true,
		SummaryFormat:   "\n📊 Summary: %d allowed, %d not allowed out of %d total tools\n",
		ShowVerboseHint: false,
	}
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
	allowedMap := make(map[string]bool)

	// Check for wildcard "*" which means all tools are allowed
	hasWildcard := false
	for _, allowed := range info.Config.Allowed {
		if allowed == "*" {
			hasWildcard = true
		}
		allowedMap[allowed] = true
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
		} else if allowedMap[tool.Name] {
			status = "✅"
		}

		rows = append(rows, []string{tool.Name, status, description})
	}

	// Render the table
	table := console.RenderTable(console.TableConfig{
		Headers: headers,
		Rows:    rows,
	})

	result := table

	// Add summary if requested
	if opts.ShowSummary {
		allowedCount := 0
		for _, tool := range info.Tools {
			if len(info.Config.Allowed) == 0 || hasWildcard || allowedMap[tool.Name] {
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

// renderMCPHierarchyTree renders all MCP servers and their tools as a tree structure
// This provides a hierarchical view of the MCP configuration
func renderMCPHierarchyTree(configs []parser.MCPServerConfig, serverInfos map[string]*parser.MCPServerInfo) string {
	mcpToolTableLog.Printf("Rendering MCP hierarchy tree: server_count=%d", len(configs))

	if len(configs) == 0 {
		mcpToolTableLog.Print("No MCP servers to render")
		return ""
	}

	// Build tree structure
	root := console.TreeNode{
		Value:    "MCP Servers",
		Children: make([]console.TreeNode, 0, len(configs)),
	}

	for _, config := range configs {
		serverNode := console.TreeNode{
			Value:    fmt.Sprintf("📦 %s (%s)", config.Name, config.Type),
			Children: []console.TreeNode{},
		}

		// Add server info if available
		if info, ok := serverInfos[config.Name]; ok && info != nil {
			// Create a map for quick lookup of allowed tools
			allowedMap := make(map[string]bool)
			hasWildcard := false
			for _, allowed := range config.Allowed {
				if allowed == "*" {
					hasWildcard = true
				}
				allowedMap[allowed] = true
			}

			// Add tools section
			if len(info.Tools) > 0 {
				toolsNode := console.TreeNode{
					Value:    fmt.Sprintf("🛠️  Tools (%d)", len(info.Tools)),
					Children: make([]console.TreeNode, 0, len(info.Tools)),
				}

				for _, tool := range info.Tools {
					// Determine if tool is allowed
					isAllowed := len(config.Allowed) == 0 || hasWildcard || allowedMap[tool.Name]
					allowIcon := "🚫"
					if isAllowed {
						allowIcon = "✅"
					}

					// Create tool node with truncated description
					toolDesc := tool.Description
					if len(toolDesc) > 50 {
						toolDesc = toolDesc[:47] + "..."
					}

					toolValue := fmt.Sprintf("%s %s - %s", allowIcon, tool.Name, toolDesc)
					toolsNode.Children = append(toolsNode.Children, console.TreeNode{
						Value:    toolValue,
						Children: []console.TreeNode{},
					})
				}

				serverNode.Children = append(serverNode.Children, toolsNode)
			}

			// Add resources section
			if len(info.Resources) > 0 {
				resourcesNode := console.TreeNode{
					Value:    fmt.Sprintf("📚 Resources (%d)", len(info.Resources)),
					Children: make([]console.TreeNode, 0, len(info.Resources)),
				}

				for _, resource := range info.Resources {
					resourceValue := fmt.Sprintf("%s - %s", resource.Name, resource.URI)
					resourcesNode.Children = append(resourcesNode.Children, console.TreeNode{
						Value:    resourceValue,
						Children: []console.TreeNode{},
					})
				}

				serverNode.Children = append(serverNode.Children, resourcesNode)
			}

			// Add roots section
			if len(info.Roots) > 0 {
				rootsNode := console.TreeNode{
					Value:    fmt.Sprintf("🌳 Roots (%d)", len(info.Roots)),
					Children: make([]console.TreeNode, 0, len(info.Roots)),
				}

				for _, root := range info.Roots {
					rootValue := fmt.Sprintf("%s - %s", root.Name, root.URI)
					rootsNode.Children = append(rootsNode.Children, console.TreeNode{
						Value:    rootValue,
						Children: []console.TreeNode{},
					})
				}

				serverNode.Children = append(serverNode.Children, rootsNode)
			}
		} else {
			// Server info not available (error or not yet queried)
			serverNode.Children = append(serverNode.Children, console.TreeNode{
				Value:    "⚠️  Server info not available",
				Children: []console.TreeNode{},
			})
		}

		root.Children = append(root.Children, serverNode)
	}

	// Render the tree
	return console.RenderTree(root)
}
