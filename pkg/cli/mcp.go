package cli

import (
	"github.com/github/gh-aw/pkg/logger"
	"github.com/spf13/cobra"
)

var mcpCommandLog = logger.New("cli:mcp")

// NewMCPCommand creates the main mcp command with subcommands
func NewMCPCommand() *cobra.Command {
	mcpCommandLog.Print("Creating MCP command with subcommands")
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Manage MCP (Model Context Protocol) servers",
		Long: `Model Context Protocol (MCP) server management and inspection.

MCP enables AI workflows to connect to external tools and data sources through
standardized servers. This command provides tools for inspecting and managing
MCP server configurations in your agentic workflows.

Available subcommands:
  • list       - List MCP servers defined in agentic workflows
  • list-tools - List available tools for a specific MCP server
  • inspect    - Inspect MCP servers and list available tools, resources, and roots
  • add        - Add an MCP tool to an agentic workflow

Examples:
  gh aw mcp list                              # List all workflows with MCP servers
  gh aw mcp inspect weekly-research           # Inspect MCP servers in workflow
  gh aw mcp add my-workflow tavily            # Add Tavily MCP server to workflow
  gh aw mcp inspect weekly-research --server github --tool create_issue  # Inspect specific tool`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	// Add subcommands
	cmd.AddCommand(NewMCPAddSubcommand())
	cmd.AddCommand(NewMCPListSubcommand())
	cmd.AddCommand(NewMCPListToolsSubcommand())
	cmd.AddCommand(NewMCPInspectSubcommand())

	return cmd
}
