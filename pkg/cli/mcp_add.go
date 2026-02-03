package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/spf13/cobra"
)

var mcpAddLog = logger.New("cli:mcp_add")

// AddMCPTool adds an MCP tool to an agentic workflow
func AddMCPTool(workflowFile string, mcpServerID string, registryURL string, transportType string, customToolID string, verbose bool) error {
	mcpAddLog.Printf("Adding MCP tool: serverID=%s, registryURL=%s, transport=%s", mcpServerID, registryURL, transportType)

	// Resolve the workflow file path
	workflowPath, err := ResolveWorkflowPath(workflowFile)
	if err != nil {
		mcpAddLog.Printf("Failed to resolve workflow path: %v", err)
		return err
	}
	mcpAddLog.Printf("Resolved workflow path: %s", workflowPath)

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Adding MCP tool '%s' to workflow: %s", mcpServerID, console.ToRelativePath(workflowPath))))
	}

	// Create registry client
	registryClient := NewMCPRegistryClient(registryURL)

	// Search for the MCP server in the registry
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Searching for MCP server '%s' in registry: %s", mcpServerID, registryClient.registryURL)))
	}

	mcpAddLog.Printf("Searching MCP registry for server: %s", mcpServerID)
	servers, err := registryClient.SearchServers(mcpServerID)
	if err != nil {
		mcpAddLog.Printf("MCP registry search failed: %v", err)
		return fmt.Errorf("failed to search MCP registry: %w", err)
	}
	mcpAddLog.Printf("Found %d matching servers in registry", len(servers))

	if len(servers) == 0 {
		return fmt.Errorf("no MCP servers found matching '%s'", mcpServerID)
	}

	// Find exact match by name first, then by partial match
	var selectedServer *MCPRegistryServerForProcessing
	for i, server := range servers {
		// Prioritize name matches over ID matches
		if server.Name == mcpServerID {
			selectedServer = &servers[i]
			break
		}
	}

	// If no name match, try partial match
	if selectedServer == nil {
		for i, server := range servers {
			if strings.Contains(strings.ToLower(server.Name), strings.ToLower(mcpServerID)) {
				selectedServer = &servers[i]
				break
			}
		}
	}

	// If still no exact match, use the first result if it looks like a partial match
	if selectedServer == nil && len(servers) > 0 {
		selectedServer = &servers[0]
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("No exact match for '%s', using closest match: %s", mcpServerID, selectedServer.Name)))
		}
	}

	if selectedServer == nil {
		return fmt.Errorf("no MCP servers found matching '%s'", mcpServerID)
	}

	// Determine tool ID (use custom if provided, otherwise use cleaned server name)
	toolID := stringutil.SanitizeToolID(selectedServer.Name)
	if customToolID != "" {
		toolID = customToolID
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Selected server: %s (Transport: %s)", selectedServer.Name, selectedServer.Transport)))
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Will add as tool ID: %s", toolID)))
	}

	// Read the workflow file
	content, err := os.ReadFile(workflowPath)
	if err != nil {
		return fmt.Errorf("failed to read workflow file: %w", err)
	}

	// Parse the workflow file
	workflowData, err := parser.ExtractFrontmatterFromContent(string(content))
	if err != nil {
		return fmt.Errorf("failed to parse workflow file: %w", err)
	}

	// Check if tool already exists
	if workflowData.Frontmatter["tools"] != nil {
		if tools, ok := workflowData.Frontmatter["tools"].(map[string]any); ok {
			if _, exists := tools[toolID]; exists {
				return fmt.Errorf("tool '%s' already exists in workflow", toolID)
			}
		}
	}

	// Create MCP tool configuration based on server info and preferences
	mcpConfig, err := createMCPToolConfig(selectedServer, transportType, registryClient.registryURL, verbose)
	if err != nil {
		return fmt.Errorf("failed to create MCP tool configuration: %w", err)
	}

	// Add the tool to the workflow
	if err := addToolToWorkflow(workflowPath, toolID, mcpConfig, verbose); err != nil {
		return fmt.Errorf("failed to add tool to workflow: %w", err)
	}

	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Added MCP tool '%s' to workflow %s", toolID, console.ToRelativePath(workflowPath))))

	// Check for required secrets and provide CLI commands if missing
	if err := checkAndSuggestSecrets(mcpConfig, verbose); err != nil {
		// Don't fail the command if secret checking fails, just log a warning
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Could not check repository secrets: %v", err)))
		}
	}

	// Compile the workflow
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Compiling workflow..."))
	}

	mcpAddLog.Print("Compiling workflow after adding MCP tool")
	compiler := workflow.NewCompiler(
		workflow.WithVerbose(verbose),
	)

	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		// Security fix for CWE-312, CWE-315, CWE-359: Avoid logging detailed error messages
		// that could contain sensitive information from secret references
		mcpAddLog.Print("Workflow compilation failed")
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Workflow compilation failed. Please check your workflow configuration."))
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("You can fix the issues and run 'gh aw compile' manually"))
	} else {
		mcpAddLog.Print("Workflow compiled successfully")
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Workflow compiled successfully"))
	}

	return nil
}

// createMCPToolConfig creates the MCP tool configuration based on registry server info
func createMCPToolConfig(server *MCPRegistryServerForProcessing, preferredTransport string, registryURL string, verbose bool) (map[string]any, error) {
	config := make(map[string]any)

	// Determine transport type (use preference if provided and supported)
	transport := server.Transport
	if preferredTransport != "" {
		switch preferredTransport {
		case "stdio", "http", "docker":
			transport = preferredTransport
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Using preferred transport: %s", transport)))
			}
		default:
			return nil, fmt.Errorf("unsupported transport type: %s (supported: stdio, http, docker)", preferredTransport)
		}
	}

	// Create MCP configuration based on transport type
	mcpSection := map[string]any{
		"type":     transport,
		"registry": fmt.Sprintf("%s/servers/%s", registryURL, server.Name),
	}

	switch transport {
	case "stdio":
		// Handle container field (simplified Docker run)
		if server.Config != nil {
			if container, hasContainer := server.Config["container"]; hasContainer {
				if containerStr, ok := container.(string); ok {
					mcpSection["container"] = containerStr

					// Add environment variables for Docker container
					if env, hasEnv := server.Config["env"]; hasEnv {
						mcpSection["env"] = convertToGitHubActionsEnv(env, server.EnvironmentVariables)
					}
				}
			} else {
				// Handle regular command and args
				// Use runtime_hint for command if available, otherwise fall back to Command
				if server.RuntimeHint != "" {
					mcpSection["command"] = server.RuntimeHint
				} else if server.Command != "" {
					mcpSection["command"] = server.Command
				}

				// Combine runtime_arguments and package arguments for args
				var allArgs []string
				allArgs = append(allArgs, server.RuntimeArguments...)
				allArgs = append(allArgs, server.Args...)
				if len(allArgs) > 0 {
					mcpSection["args"] = allArgs
				}

				// Add environment variables if present
				if env, hasEnv := server.Config["env"]; hasEnv {
					mcpSection["env"] = convertToGitHubActionsEnv(env, server.EnvironmentVariables)
				}
			}
		} else {
			// Handle command and args when no config
			// Use runtime_hint for command if available, otherwise fall back to Command
			if server.RuntimeHint != "" {
				mcpSection["command"] = server.RuntimeHint
			} else if server.Command != "" {
				mcpSection["command"] = server.Command
			}

			// Combine runtime_arguments and package arguments for args
			var allArgs []string
			allArgs = append(allArgs, server.RuntimeArguments...)
			allArgs = append(allArgs, server.Args...)
			if len(allArgs) > 0 {
				mcpSection["args"] = allArgs
			}
		}

	case "http":
		// For HTTP transport, we need a URL
		if server.Config != nil {
			if url, hasURL := server.Config["url"]; hasURL {
				mcpSection["url"] = url
			} else {
				return nil, fmt.Errorf("HTTP transport requires URL configuration")
			}

			// Add headers if present
			if headers, hasHeaders := server.Config["headers"]; hasHeaders {
				mcpSection["headers"] = headers
			}
		} else {
			return nil, fmt.Errorf("HTTP transport requires configuration")
		}

	case "docker":
		// For Docker transport, use container configuration
		if server.Config != nil {
			if container, hasContainer := server.Config["container"]; hasContainer {
				mcpSection["container"] = container
			} else {
				return nil, fmt.Errorf("docker transport requires container configuration")
			}

			// Add environment variables if present
			if env, hasEnv := server.Config["env"]; hasEnv {
				mcpSection["env"] = convertToGitHubActionsEnv(env, server.EnvironmentVariables)
			}
		} else {
			return nil, fmt.Errorf("docker transport requires configuration")
		}

	default:
		return nil, fmt.Errorf("unsupported transport type: %s", transport)
	}

	config["mcp"] = mcpSection

	return config, nil
}

// addToolToWorkflow adds a tool configuration to the workflow file
func addToolToWorkflow(workflowPath string, toolID string, toolConfig map[string]any, verbose bool) error {
	// Use frontmatter helper to update the workflow file
	return parser.UpdateWorkflowFrontmatter(workflowPath, func(frontmatter map[string]any) error {
		// Ensure tools section exists
		tools := parser.EnsureToolsSection(frontmatter)

		// Check if tool already exists
		if _, exists := tools[toolID]; exists {
			return fmt.Errorf("tool '%s' already exists in workflow", toolID)
		}

		// Add the new tool
		tools[toolID] = toolConfig
		return nil
	}, verbose)
}

// NewMCPAddSubcommand creates the mcp add subcommand
func NewMCPAddSubcommand() *cobra.Command {
	var registryURL string
	var transportType string
	var customToolID string

	cmd := &cobra.Command{
		Use:   "add [workflow] [server]",
		Short: "Add an MCP tool to an agentic workflow",
		Long: `Add an MCP tool to an agentic workflow by searching the MCP registry.

This command searches the MCP registry for the specified server, adds it to the workflow's tools section,
and automatically compiles the workflow. If the tool already exists, the command will fail.

When called with no arguments, it will show a list of available MCP servers from the registry.

The workflow-id-or-file can be:
- A workflow ID (basename without .md extension, e.g., "weekly-research")
- A file path (e.g., "weekly-research.md" or ".github/workflows/weekly-research.md")

Examples:
  gh aw mcp add                                          # List available MCP servers
  gh aw mcp add weekly-research makenotion/notion-mcp-server  # Add Notion MCP server to weekly-research.md
  gh aw mcp add weekly-research makenotion/notion-mcp-server --transport stdio  # Prefer stdio transport
  gh aw mcp add weekly-research makenotion/notion-mcp-server --registry https://custom.registry.com/v1  # Use custom registry
  gh aw mcp add weekly-research makenotion/notion-mcp-server --tool-id my-notion  # Use custom tool ID

The command will:
- Search the MCP registry for the specified server
- Check that the tool doesn't already exist in the workflow
- Add the MCP tool configuration to the workflow's frontmatter
- Automatically compile the workflow to generate the .lock.yml file

Registry URL defaults to: https://api.mcp.github.com/v0.1`,
		Args: cobra.RangeArgs(0, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			verbose, _ := cmd.Flags().GetBool("verbose")

			// If no arguments provided, show list of available servers
			if len(args) == 0 {
				// Use default registry URL if not provided
				if registryURL == "" {
					registryURL = string(constants.DefaultMCPRegistryURL)
				}
				return listAvailableServers(registryURL, verbose)
			}

			// If only workflow ID/file is provided, show error (need both workflow and server)
			if len(args) == 1 {
				return fmt.Errorf("both workflow ID/file and server name are required to add an MCP tool\nUse 'gh aw mcp add' to list available servers")
			}

			// If both arguments are provided, add the MCP tool
			workflowFile := args[0]
			mcpServerID := args[1]

			return AddMCPTool(workflowFile, mcpServerID, registryURL, transportType, customToolID, verbose)
		},
	}

	cmd.Flags().StringVar(&registryURL, "registry", "", "MCP registry URL (default: https://api.mcp.github.com/v0)")
	cmd.Flags().StringVar(&transportType, "transport", "", "Preferred transport type (stdio, http, docker)")
	cmd.Flags().StringVar(&customToolID, "tool-id", "", "Custom tool ID to use in the workflow (default: uses server ID)")

	return cmd
}
