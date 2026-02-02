package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/spf13/cobra"
)

var mcpInspectLog = logger.New("cli:mcp_inspect")

// InspectWorkflowMCP inspects MCP servers used by a workflow and lists available tools, resources, and roots
func InspectWorkflowMCP(workflowFile string, serverFilter string, toolFilter string, verbose bool, useActionsSecrets bool) error {
	mcpInspectLog.Printf("Inspecting workflow MCP: workflow=%s, serverFilter=%s, toolFilter=%s",
		workflowFile, serverFilter, toolFilter)

	workflowsDir := getWorkflowsDir()

	// If no workflow file specified, show available workflow files with MCP configs
	if workflowFile == "" {
		return listWorkflowsWithMCP(workflowsDir, verbose)
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
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Inspecting MCP servers in: %s", workflowPath)))
	}

	// Use the compiler to parse the workflow file
	// This automatically handles imports, merging, and validation
	compiler := workflow.NewCompiler(
		workflow.WithVerbose(verbose),
	)
	workflowData, err := compiler.ParseWorkflowFile(workflowPath)
	if err != nil {
		// Handle shared workflow error separately (not a fatal error for inspection)
		if _, isSharedWorkflow := err.(*workflow.SharedWorkflowError); isSharedWorkflow {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Cannot inspect shared/imported workflows directly - they must be imported by a main workflow"))
			return nil
		}

		errMsg := fmt.Sprintf("failed to parse workflow file: %v", err)
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(errMsg))
		return fmt.Errorf("failed to parse workflow file: %w", err)
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Workflow parsed successfully"))
	}

	// Build frontmatter map from WorkflowData for MCP extraction
	// This includes all merged imports and tools
	frontmatterForMCP := buildFrontmatterFromWorkflowData(workflowData)

	// Extract MCP configurations from the merged frontmatter
	mcpConfigs, err := parser.ExtractMCPConfigurations(frontmatterForMCP, serverFilter)
	if err != nil {
		errMsg := fmt.Sprintf("failed to extract MCP configurations: %v", err)
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(errMsg))
		return fmt.Errorf("failed to extract MCP configurations: %w", err)
	}

	// Filter out safe-outputs MCP servers for inspection
	mcpConfigs = filterOutSafeOutputs(mcpConfigs)

	// Start safe-inputs server if present
	var safeInputsServerCmd *exec.Cmd
	var safeInputsTmpDir string
	if workflowData != nil && workflowData.SafeInputs != nil && len(workflowData.SafeInputs.Tools) > 0 {
		// Start safe-inputs server and add it to the list of MCP configs
		config, serverCmd, tmpDir, err := startSafeInputsServer(workflowData.SafeInputs, verbose)
		if err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to start safe-inputs server: %v", err)))
			}
		} else {
			safeInputsServerCmd = serverCmd
			safeInputsTmpDir = tmpDir
			// Add safe-inputs config to the list of MCP servers to inspect
			mcpConfigs = append(mcpConfigs, *config)
		}
	}

	// Cleanup safe-inputs server when done
	if safeInputsServerCmd != nil {
		defer func() {
			if safeInputsServerCmd.Process != nil {
				// Try graceful shutdown first
				if err := safeInputsServerCmd.Process.Signal(os.Interrupt); err != nil && verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to send interrupt signal: %v", err)))
				}
				// Wait a moment for graceful shutdown
				time.Sleep(500 * time.Millisecond)
				// Attempt force kill (may fail if process already exited gracefully, which is fine)
				_ = safeInputsServerCmd.Process.Kill()
			}
			// Cleanup temporary directory
			if safeInputsTmpDir != "" {
				if err := os.RemoveAll(safeInputsTmpDir); err != nil && verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to cleanup temporary directory: %v", err)))
				}
			}
		}()
	}

	if len(mcpConfigs) == 0 {
		if serverFilter != "" {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("No MCP servers matching filter '%s' found in workflow", serverFilter)))
		} else {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage("No MCP servers found in workflow"))
		}
		return nil
	}

	// Inspect each MCP server
	if toolFilter != "" {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found %d MCP server(s), looking for tool '%s'", len(mcpConfigs), toolFilter)))
	} else {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found %d MCP server(s) to inspect", len(mcpConfigs))))
	}
	fmt.Fprintln(os.Stderr)

	for i, config := range mcpConfigs {
		if i > 0 {
			fmt.Fprintln(os.Stderr)
		}
		if err := inspectMCPServer(config, toolFilter, verbose, useActionsSecrets); err != nil {
			fmt.Fprintln(os.Stderr, console.FormatError(console.CompilerError{
				Type:    "error",
				Message: fmt.Sprintf("Failed to inspect MCP server '%s': %v", config.Name, err),
			}))
		}
	}

	return nil
}

// NewMCPInspectSubcommand creates the mcp inspect subcommand
// This is the former mcp inspect command now nested under mcp
func NewMCPInspectSubcommand() *cobra.Command {
	var serverFilter string
	var toolFilter string
	var spawnInspector bool
	var checkSecrets bool

	cmd := &cobra.Command{
		Use:   "inspect [workflow]",
		Short: "Inspect MCP servers and list available tools, resources, and roots",
		Long: `Inspect MCP servers used by a workflow and display available tools, resources, and roots.

This command starts each MCP server configured in the workflow, queries its capabilities,
and displays the results in a formatted table. It supports stdio, Docker, and HTTP MCP servers.

Safe-inputs servers are automatically detected and inspected when present in the workflow.

The workflow-id-or-file can be:
- A workflow ID (basename without .md extension, e.g., "weekly-research")
- A file path (e.g., "weekly-research.md" or ".github/workflows/weekly-research.md")

Examples:
  gh aw mcp inspect                    # List workflows with MCP servers
  gh aw mcp inspect weekly-research    # Inspect MCP servers in weekly-research.md
  gh aw mcp inspect daily-news --server tavily  # Inspect only the tavily server
  gh aw mcp inspect weekly-research --server github --tool create_issue  # Show details for a specific tool
  gh aw mcp inspect weekly-research -v # Verbose output with detailed connection info
  gh aw mcp inspect weekly-research --inspector  # Launch @modelcontextprotocol/inspector
  gh aw mcp inspect weekly-research --check-secrets  # Check GitHub Actions secrets

The command will:
- Parse the workflow file to extract MCP server configurations
- Start each MCP server (stdio, docker, http)
- Automatically start and inspect safe-inputs server if present
- Query available tools, resources, and roots
- Validate required secrets are available  
- Display results in formatted tables with error details`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var workflowFile string
			if len(args) > 0 {
				workflowFile = args[0]
			}

			verbose, _ := cmd.Flags().GetBool("verbose")
			// Check for verbose flag from parent commands (root and mcp)
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

			// Validate that tool flag requires server flag
			if toolFilter != "" && serverFilter == "" {
				return fmt.Errorf("--tool flag requires --server flag to be specified")
			}

			// Handle spawn inspector flag
			if spawnInspector {
				return spawnMCPInspector(workflowFile, serverFilter, verbose)
			}

			return InspectWorkflowMCP(workflowFile, serverFilter, toolFilter, verbose, checkSecrets)
		},
	}

	cmd.Flags().StringVar(&serverFilter, "server", "", "Filter to inspect only the specified MCP server")
	cmd.Flags().StringVar(&toolFilter, "tool", "", "Show detailed information about a specific tool (requires --server)")
	cmd.Flags().BoolVar(&spawnInspector, "inspector", false, "Launch the official @modelcontextprotocol/inspector tool")
	cmd.Flags().BoolVar(&checkSecrets, "check-secrets", false, "Check GitHub Actions repository secrets for missing secrets")

	// Register completions for mcp inspect command
	cmd.ValidArgsFunction = CompleteWorkflowNames

	return cmd
}

// buildFrontmatterFromWorkflowData reconstructs a frontmatter map from WorkflowData
// This is used to extract MCP configurations after the compiler has processed imports and merging
func buildFrontmatterFromWorkflowData(workflowData *workflow.WorkflowData) map[string]any {
	// Use the parsed frontmatter's ToMap() method if available
	// This preserves the original frontmatter structure with imports already merged
	if workflowData.ParsedFrontmatter != nil {
		return workflowData.ParsedFrontmatter.ToMap()
	}

	// Fallback to building manually (shouldn't happen in normal cases)
	frontmatter := make(map[string]any)

	// Add tools section if present
	if len(workflowData.Tools) > 0 {
		frontmatter["tools"] = workflowData.Tools
	}

	return frontmatter
}
