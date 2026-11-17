package cli

import (
	"fmt"

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
  - list       - List MCP servers defined in agentic workflows
  - list-tools - List available tools for a specific MCP server, or find workflows using it
  - inspect    - Inspect MCP servers and list available tools, resources, and roots
  - add        - Add an MCP server to an agentic workflow`,
		Example: `  gh aw mcp list                              # List all workflows with MCP servers
  gh aw mcp inspect weekly-research           # Inspect MCP servers in a workflow
  gh aw mcp add my-workflow tavily            # Add Tavily MCP server to workflow
  gh aw mcp inspect weekly-research --server github --tool create_issue  # Inspect specific tool`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("unknown command %q for %q", args[0], cliCommandPath(cmd))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	// Add subcommands
	cmd.AddCommand(newLegacyGHGuardSubcommand())
	cmd.AddCommand(NewMCPAddSubcommand())
	cmd.AddCommand(NewMCPListSubcommand())
	cmd.AddCommand(NewMCPListToolsSubcommand())
	cmd.AddCommand(NewMCPInspectSubcommand())

	return cmd
}
