package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/spf13/cobra"
)

var mcpListToolsLog = logger.New("cli:mcp_list_tools")

const (
	// maxDescriptionLength is the maximum length for tool descriptions before truncation
	maxDescriptionLength = 60
)

// ListToolsForMCP lists available tools for a specific MCP server
func ListToolsForMCP(workflowFile string, mcpServerName string, verbose bool) error {
	mcpListToolsLog.Printf("Listing tools for MCP server: %s, workflow: %s", mcpServerName, workflowFile)
	workflowsDir := getWorkflowsDir()

	// If no workflow file specified, search for workflows containing the MCP server
	if workflowFile == "" {
		mcpListToolsLog.Printf("No workflow file specified, searching in: %s", workflowsDir)
		return findWorkflowsWithMCPServer(workflowsDir, mcpServerName, verbose)
	}

	// Resolve the workflow file path
	workflowPath, err := ResolveWorkflowPath(workflowFile)
	if err != nil {
		return err
	}

	// Convert to absolute path if needed
	if !filepath.IsAbs(workflowPath) {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
		workflowPath = filepath.Join(cwd, workflowPath)
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Looking for MCP server '%s' in: %s", mcpServerName, workflowPath)))
	}

	// Parse the workflow file and extract MCP configurations
	_, mcpConfigs, err := loadWorkflowMCPConfigs(workflowPath, mcpServerName)
	if err != nil {
		return err
	}

	// Find the specific MCP server
	var targetConfig *parser.MCPServerConfig
	for _, config := range mcpConfigs {
		if strings.EqualFold(config.Name, mcpServerName) {
			targetConfig = &config
			break
		}
	}

	if targetConfig == nil {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("MCP server '%s' not found in workflow '%s'", mcpServerName, filepath.Base(workflowPath))))

		// Show available servers
		if len(mcpConfigs) > 0 {
			fmt.Fprintf(os.Stderr, "Available MCP servers: ")
			serverNames := make([]string, len(mcpConfigs))
			for i, config := range mcpConfigs {
				serverNames[i] = config.Name
			}
			fmt.Fprintf(os.Stderr, "%s\n", strings.Join(serverNames, ", "))
		}
		return nil
	}

	// Connect to the MCP server and get its tools
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("📡 Connecting to MCP server: %s (%s)",
		targetConfig.Name,
		targetConfig.Type)))

	info, err := connectToMCPServer(*targetConfig, verbose)
	if err != nil {
		return fmt.Errorf("failed to connect to MCP server '%s': %w", mcpServerName, err)
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Successfully connected to MCP server"))
	}

	// Display the tools
	displayToolsList(info, verbose)

	return nil
}

// findWorkflowsWithMCPServer searches for workflows containing a specific MCP server
func findWorkflowsWithMCPServer(workflowsDir string, mcpServerName string, verbose bool) error {
	// Scan workflows for MCP configurations, filtering by server name
	results, err := ScanWorkflowsForMCP(workflowsDir, mcpServerName, verbose)
	if err != nil {
		return err
	}

	var matchingWorkflows []string

	for _, result := range results {
		// Check if this workflow contains the target MCP server
		for _, config := range result.MCPConfigs {
			if strings.EqualFold(config.Name, mcpServerName) {
				matchingWorkflows = append(matchingWorkflows, result.BaseName)
				break
			}
		}
	}

	if len(matchingWorkflows) == 0 {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("MCP server '%s' not found in any workflow", mcpServerName)))
		return nil
	}

	// Display matching workflows and suggest using one
	fmt.Fprintf(os.Stderr, "Found MCP server '%s' in %d workflow(s): %s\n",
		mcpServerName, len(matchingWorkflows), strings.Join(matchingWorkflows, ", "))
	fmt.Fprintf(os.Stderr, "\nRun 'gh aw mcp list-tools %s <workflow-name>' to list tools for a specific workflow\n", mcpServerName)

	return nil
}

// displayToolsList shows the tools available from the MCP server in a formatted table
func displayToolsList(info *parser.MCPServerInfo, verbose bool) {
	if len(info.Tools) == 0 {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("No tools available from this MCP server"))
		return
	}

	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("\n🛠️  Available Tools (%d total)", len(info.Tools))))

	// Configure options based on verbose flag
	opts := MCPToolTableOptions{
		ShowSummary: true,
	}

	if verbose {
		// In verbose mode, show full descriptions without truncation
		opts.TruncateLength = 0
		opts.ShowVerboseHint = false
	} else {
		// In non-verbose mode, truncate descriptions to keep tools on single lines
		opts.TruncateLength = maxDescriptionLength
		opts.ShowVerboseHint = true
	}

	// Render the table using the shared helper
	table := renderMCPToolTable(info, opts)
	fmt.Print(table)
}

// NewMCPListToolsSubcommand creates the mcp list-tools subcommand
func NewMCPListToolsSubcommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list-tools <server> [workflow]",
		Short: "List available tools for a specific MCP server",
		Long: `List available tools for a specific MCP server.

This command connects to the specified MCP server and displays all available tools.
It reuses the same infrastructure as 'mcp inspect' to establish connections and
query server capabilities.

The workflow-id-or-file can be:
- A workflow ID (basename without .md extension, e.g., "weekly-research")
- A file path (e.g., "weekly-research.md" or ".github/workflows/weekly-research.md")

Examples:
  gh aw mcp list-tools github                    # Find workflows with 'github' MCP server
  gh aw mcp list-tools github weekly-research    # List tools for 'github' server in weekly-research.md
  gh aw mcp list-tools safe-outputs issue-triage # List tools for 'safe-outputs' server in issue-triage.md
  gh aw mcp list-tools playwright test-workflow -v  # Verbose output with tool descriptions

The command will:
- Parse the workflow to find the specified MCP server configuration
- Connect to the MCP server using the same logic as 'mcp inspect'
- Display available tools with their descriptions and allowance status`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mcpServerName := args[0]
			var workflowFile string
			if len(args) > 1 {
				workflowFile = args[1]
			}

			verbose, _ := cmd.Flags().GetBool("verbose")

			return ListToolsForMCP(workflowFile, mcpServerName, verbose)
		},
		ValidArgsFunction: completeMCPListToolsArgs,
	}

	return cmd
}

// commonMCPServerNames contains commonly used MCP server names for shell completion
var commonMCPServerNames = []string{"github", "playwright", "serena", "tavily", "safe-outputs"}

// completeMCPListToolsArgs provides completion for mcp list-tools command arguments
func completeMCPListToolsArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	// First argument: MCP server names are not easily discoverable without a workflow
	// For now, provide no file completion but suggest common server names
	if len(args) == 0 {
		var filtered []string
		for _, s := range commonMCPServerNames {
			if toComplete == "" || strings.HasPrefix(s, toComplete) {
				filtered = append(filtered, s)
			}
		}
		return filtered, cobra.ShellCompDirectiveNoFileComp
	}
	// Second argument: complete workflow names
	return CompleteWorkflowNames(cmd, args, toComplete)
}
