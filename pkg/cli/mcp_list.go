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

var mcpListLog = logger.New("cli:mcp_list")

// ListWorkflowMCP lists MCP servers defined in a workflow
func ListWorkflowMCP(workflowFile string, verbose bool) error {
	mcpListLog.Printf("Listing MCP servers: workflow=%s, verbose=%t", workflowFile, verbose)
	// Determine the workflow directory and file
	workflowsDir := ".github/workflows"
	var workflowPath string

	if workflowFile != "" {
		// Resolve the workflow file path
		var err error
		workflowPath, err = ResolveWorkflowPath(workflowFile)
		if err != nil {
			mcpListLog.Printf("Failed to resolve workflow path: %v", err)
			return err
		}
		mcpListLog.Printf("Resolved workflow path: %s", workflowPath)
	} else {
		// No specific workflow file provided, list all workflows with MCP servers
		mcpListLog.Print("No workflow file specified, listing all workflows with MCP servers")
		return listWorkflowsWithMCPServers(workflowsDir, verbose)
	}

	// Parse the specific workflow file and extract MCP configurations
	frontmatter, mcpConfigs, err := loadWorkflowMCPConfigs(workflowPath, "")
	if err != nil {
		mcpListLog.Printf("Failed to load MCP configs from workflow: %v", err)
		return err
	}

	mcpListLog.Printf("Found %d MCP servers in workflow", len(mcpConfigs))
	if len(mcpConfigs) == 0 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No MCP servers found in workflow"))
		return nil
	}

	// Check if workflow has network access configured
	hasNetworkAccess := checkNetworkAccess(frontmatter.Frontmatter)

	// Display the MCP servers
	if verbose {
		// Create detailed table for verbose mode
		headers := []string{"Server Name", "Type", "Status", "Tools Count", "Network Access", "Command/URL"}
		rows := make([][]string, 0, len(mcpConfigs))

		for _, config := range mcpConfigs {
			status := determineConfigStatus(config)
			toolsCount := formatToolsCount(config.Allowed)
			networkAccess := formatNetworkAccess(hasNetworkAccess)

			commandOrURL := ""
			if config.Command != "" {
				commandOrURL = config.Command
			} else if config.URL != "" {
				commandOrURL = config.URL
			} else if config.Container != "" {
				commandOrURL = config.Container
			}
			// Truncate if too long
			if len(commandOrURL) > 40 {
				commandOrURL = commandOrURL[:37] + "..."
			}

			rows = append(rows, []string{
				config.Name,
				config.Type,
				status,
				toolsCount,
				networkAccess,
				commandOrURL,
			})
		}

		tableConfig := console.TableConfig{
			Title:   fmt.Sprintf("MCP servers in %s", filepath.Base(workflowPath)),
			Headers: headers,
			Rows:    rows,
		}
		fmt.Fprint(os.Stderr, console.RenderTable(tableConfig))
	} else {
		// Simple table for basic mode
		headers := []string{"Server Name", "Status", "Tools Count", "Network Access"}
		rows := make([][]string, 0, len(mcpConfigs))

		for _, config := range mcpConfigs {
			status := determineConfigStatus(config)
			toolsCount := formatToolsCount(config.Allowed)
			networkAccess := formatNetworkAccess(hasNetworkAccess)

			rows = append(rows, []string{
				config.Name,
				status,
				toolsCount,
				networkAccess,
			})
		}

		tableConfig := console.TableConfig{
			Title:   fmt.Sprintf("MCP servers in %s", filepath.Base(workflowPath)),
			Headers: headers,
			Rows:    rows,
		}
		fmt.Fprint(os.Stderr, console.RenderTable(tableConfig))
	}

	if !verbose {
		fmt.Fprintf(os.Stderr, "\nRun 'gh aw mcp list %s --verbose' for detailed information\n", workflowFile)
	}

	return nil
}

// listWorkflowsWithMCPServers shows available workflow files that contain MCP configurations
// with optional interactive selection
func listWorkflowsWithMCPServers(workflowsDir string, verbose bool) error {
	// Scan workflows for MCP configurations
	results, err := ScanWorkflowsForMCP(workflowsDir, "", verbose)
	if err != nil {
		return err
	}

	var workflowData []struct {
		name        string
		serverCount int
		serverNames []string
	}
	var totalMCPCount int

	for _, result := range results {
		serverNames := make([]string, len(result.MCPConfigs))
		for i, config := range result.MCPConfigs {
			serverNames[i] = config.Name
		}

		workflowData = append(workflowData, struct {
			name        string
			serverCount int
			serverNames []string
		}{
			name:        result.BaseName,
			serverCount: len(result.MCPConfigs),
			serverNames: serverNames,
		})
		totalMCPCount += len(result.MCPConfigs)
	}

	if len(workflowData) == 0 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No workflows with MCP servers found"))
		return nil
	}

	// Try interactive selection first in TTY mode
	selectedWorkflow, err := showInteractiveMCPWorkflowSelection(workflowData, verbose)
	if err == nil && selectedWorkflow != "" {
		// User selected a workflow, show its details
		mcpListLog.Printf("User selected workflow: %s", selectedWorkflow)
		return ListWorkflowMCP(selectedWorkflow, verbose)
	}

	// If interactive selection failed or was cancelled, fall back to table display
	mcpListLog.Printf("Interactive selection failed or cancelled, showing table: %v", err)

	// Display results in table format
	if verbose {
		// Detailed table with server names
		headers := []string{"Workflow", "Server Count", "MCP Servers"}
		rows := make([][]string, 0, len(workflowData))

		for _, workflow := range workflowData {
			serverList := strings.Join(workflow.serverNames, ", ")
			// Truncate if too long
			if len(serverList) > 50 {
				serverList = serverList[:47] + "..."
			}

			rows = append(rows, []string{
				workflow.name,
				fmt.Sprintf("%d", workflow.serverCount),
				serverList,
			})
		}

		tableConfig := console.TableConfig{
			Headers: headers,
			Rows:    rows,
		}
		fmt.Fprint(os.Stderr, console.RenderTable(tableConfig))
	} else {
		// Simple table with just workflow names and counts
		headers := []string{"Workflow", "Server Count"}
		rows := make([][]string, 0, len(workflowData))

		for _, workflow := range workflowData {
			rows = append(rows, []string{
				workflow.name,
				fmt.Sprintf("%d", workflow.serverCount),
			})
		}

		tableConfig := console.TableConfig{
			Headers: headers,
			Rows:    rows,
		}
		fmt.Fprint(os.Stderr, console.RenderTable(tableConfig))
	}

	if !verbose {
		fmt.Fprintf(os.Stderr, "\nRun 'gh aw mcp list --verbose' for detailed information\n")
	}
	fmt.Fprintf(os.Stderr, "Run 'gh aw mcp list <workflow-name>' to list MCP servers in a specific workflow\n")

	return nil
}

// showInteractiveMCPWorkflowSelection displays an interactive list of workflows with MCP servers
func showInteractiveMCPWorkflowSelection(workflows []struct {
	name        string
	serverCount int
	serverNames []string
}, verbose bool) (string, error) {
	mcpListLog.Printf("Showing interactive MCP workflow selection: workflows=%d", len(workflows))

	// Convert workflow data to ListItems
	items := make([]console.ListItem, len(workflows))
	for i, wf := range workflows {
		description := fmt.Sprintf("%d server(s): %s", wf.serverCount, strings.Join(wf.serverNames, ", "))
		// Truncate description if too long
		if len(description) > 80 {
			description = description[:77] + "..."
		}
		items[i] = console.NewListItem(wf.name, description, wf.name)
	}

	// Show interactive list
	title := "Select a workflow to view its MCP servers:"
	selectedWorkflow, err := console.ShowInteractiveList(title, items)
	if err != nil {
		return "", err
	}

	mcpListLog.Printf("Selected workflow: %s", selectedWorkflow)
	return selectedWorkflow, nil
}

// determineConfigStatus checks if an MCP server configuration is valid and ready
func determineConfigStatus(config parser.MCPServerConfig) string {
	// Check if the configuration has the minimum required fields
	hasExecutable := config.Command != "" || config.URL != "" || config.Container != ""

	if !hasExecutable {
		return "⚠ Incomplete"
	}

	// Configuration appears valid
	return "✓ Ready"
}

// formatToolsCount returns a human-readable count of allowed tools
func formatToolsCount(allowed []string) string {
	if len(allowed) == 0 {
		return "All tools"
	}

	// Check for wildcard
	for _, tool := range allowed {
		if tool == "*" {
			return "All tools"
		}
	}

	if len(allowed) == 1 {
		return "1 tool"
	}

	return fmt.Sprintf("%d tools", len(allowed))
}

// formatNetworkAccess returns a formatted string indicating network access status
func formatNetworkAccess(hasAccess bool) string {
	if hasAccess {
		return "✓ Enabled"
	}
	return "✗ Disabled"
}

// checkNetworkAccess checks if the workflow has network access configured
func checkNetworkAccess(frontmatter map[string]any) bool {
	if frontmatter == nil {
		return false
	}

	// Check if network field exists and is not empty
	if network, exists := frontmatter["network"]; exists {
		if networkMap, ok := network.(map[string]any); ok {
			// Check if there are any allowed domains or if network is configured
			if allowed, hasAllowed := networkMap["allowed"]; hasAllowed {
				if allowedList, ok := allowed.([]any); ok {
					return len(allowedList) > 0
				}
			}
			// If network map exists with any content, consider it enabled
			return len(networkMap) > 0
		}
	}

	return false
}

// NewMCPListSubcommand creates the mcp list subcommand
func NewMCPListSubcommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list [workflow]",
		Short: "List MCP servers defined in agentic workflows",
		Long: `List MCP servers defined in agentic workflows.

When no workflow ID/file is specified, lists all workflows that contain MCP server configurations.
When a workflow ID/file is specified, lists the MCP servers configured in that specific workflow.

The workflow-id-or-file can be:
- A workflow ID (basename without .md extension, e.g., "weekly-research")
- A file path (e.g., "weekly-research.md" or ".github/workflows/weekly-research.md")

Examples:
  gh aw mcp list                     # List all workflows with MCP servers
  gh aw mcp list weekly-research     # List MCP servers in weekly-research.md
  gh aw mcp list weekly-research -v  # List with detailed information
  gh aw mcp list --verbose           # List all workflows with detailed MCP server info

The command displays:
- Server Name: MCP server identifier
- Status: Configuration status (✓ Ready or ⚠ Incomplete)
- Tools Count: Number of allowed tools or "All tools"
- Network Access: Whether network permissions are configured (✓ Enabled or ✗ Disabled)
- In verbose mode: Also shows Type and Command/URL`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var workflowFile string
			if len(args) > 0 {
				workflowFile = args[0]
			}

			verbose, _ := cmd.Flags().GetBool("verbose")

			// Inherit verbose from parent commands
			if !verbose {
				if cmd.Parent() != nil {
					if parentVerbose, _ := cmd.Parent().PersistentFlags().GetBool("verbose"); parentVerbose {
						verbose = true
					}
					if cmd.Parent().Parent() != nil {
						if rootVerbose, _ := cmd.Parent().Parent().PersistentFlags().GetBool("verbose"); rootVerbose {
							verbose = true
						}
					}
				}
			}

			return ListWorkflowMCP(workflowFile, verbose)
		},
	}

	// Register completions for mcp list command
	cmd.ValidArgsFunction = CompleteWorkflowNames

	return cmd
}
