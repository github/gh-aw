package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/sliceutil"
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
		fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr(fmt.Sprintf("Looking for MCP server '%s' in: %s", mcpServerName, workflowPath)))
	}

	// Parse the workflow file and extract MCP configurations
	_, mcpConfigs, err := loadWorkflowMCPConfigs(workflowPath, mcpServerName)
	if err != nil {
		return err
	}

	mcpListToolsLog.Printf("Found %d MCP configs in workflow, searching for server: %s", len(mcpConfigs), mcpServerName)

	// Find the specific MCP server
	var targetConfig *parser.RegistryMCPServerConfig
	for _, config := range mcpConfigs {
		if strings.EqualFold(config.Name, mcpServerName) {
			targetConfig = &config
			break
		}
	}

	if targetConfig == nil {
		mcpListToolsLog.Printf("MCP server %q not found in workflow %q", mcpServerName, filepath.Base(workflowPath))
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("MCP server '%s' not found in workflow '%s'", mcpServerName, filepath.Base(workflowPath))))

		// Show available servers
		if len(mcpConfigs) > 0 {
			serverNames := sliceutil.Map(mcpConfigs, func(config parser.RegistryMCPServerConfig) string { return config.Name })
			fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr("Available MCP servers: "+strings.Join(serverNames, ", ")))
		}
		return nil
	}

	mcpListToolsLog.Printf("Found MCP server: name=%s, type=%s", targetConfig.Name, targetConfig.Type)

	// Connect to the MCP server and get its tools
	fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr(fmt.Sprintf("📡 Connecting to MCP server: %s (%s)",
		targetConfig.Name,
		targetConfig.Type)))

	info, err := connectToMCPServer(*targetConfig, verbose)
	if err != nil {
		return fmt.Errorf("failed to connect to MCP server '%s': %w", mcpServerName, err)
	}

	mcpListToolsLog.Printf("Connected to MCP server: tools=%d", len(info.Tools))

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessageStderr("Successfully connected to MCP server"))
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
	fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr(
		fmt.Sprintf("Found MCP server '%s' in %d workflow(s): %s", mcpServerName, len(matchingWorkflows), strings.Join(matchingWorkflows, ", ")),
	))
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr(
		fmt.Sprintf("Run 'gh aw mcp list-tools <workflow-name> --server %s' to list tools for a specific workflow", mcpServerName),
	))

	return nil
}

// displayToolsList shows the tools available from the MCP server in a formatted table
func displayToolsList(info *parser.MCPServerInfo, verbose bool) {
	if len(info.Tools) == 0 {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("No tools available from this MCP server"))
		return
	}

	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr(fmt.Sprintf("🛠️  Available Tools (%d total)", len(info.Tools))))

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

	// Render the table using the shared helper and print to stdout (structured data)
	fmt.Fprint(os.Stdout, renderMCPToolTable(info, opts))
}

// NewMCPListToolsSubcommand creates the mcp list-tools subcommand
func NewMCPListToolsSubcommand() *cobra.Command {
	var serverFilter string

	cmd := &cobra.Command{
		Use:   "list-tools [workflow]",
		Short: "List available tools for a specific MCP server, or find workflows using it",
		Long: `List available tools for a specific MCP server, or find workflows using it.

When no workflow argument is provided, this command searches workflows in .github/workflows
for references to the specified MCP server and returns matching workflow IDs.
When a workflow is provided, it connects to the specified MCP server and displays all
available tools. It reuses the same infrastructure as 'mcp inspect' to establish
connections and query server capabilities.

The workflow-id-or-file can be:
- A workflow ID (basename without .md extension, e.g., "weekly-research")
- A file path (e.g., "weekly-research.md" or ".github/workflows/weekly-research.md")

The command will:
- Parse the workflow to find the specified MCP server configuration
- Connect to the MCP server using the same logic as 'mcp inspect'
- Display available tools with their descriptions and allowance status`,
		Example: `  gh aw mcp list-tools --server github                    # Search for workflows containing the 'github' MCP server
  gh aw mcp list-tools weekly-research --server github    # List tools for 'github' server in weekly-research.md
  gh aw mcp list-tools issue-triage --server safe-outputs # List tools for 'safe-outputs' server in issue-triage.md
  gh aw mcp list-tools test-workflow --server playwright -v  # Verbose output with tool descriptions
`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if serverFilter == "" {
				return errors.New(console.FormatErrorWithSuggestions(
					"missing required flag: --server",
					[]string{
						"Specify an MCP server name, for example: gh aw mcp list-tools --server github",
						"Optionally provide a workflow: gh aw mcp list-tools <workflow> --server github",
						"Use 'gh aw mcp list <workflow>' to see available MCP servers in a workflow",
					},
				))
			}

			var workflowFile string
			if len(args) > 0 {
				workflowFile = args[0]
			}

			verbose, _ := cmd.Flags().GetBool("verbose")

			return ListToolsForMCP(workflowFile, serverFilter, verbose)
		},
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) > 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return CompleteWorkflowNames(cmd, args, toComplete)
		},
	}

	cmd.Flags().StringVar(&serverFilter, "server", "", "MCP server name to list tools for (required)")

	return cmd
}
