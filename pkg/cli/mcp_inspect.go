package cli

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2/tree"
	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/styles"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/spf13/cobra"
)

var mcpInspectLog = logger.New("cli:mcp_inspect")

// mcpScriptsServerShutdownDelay gives the embedded mcp-scripts server a brief window to stop gracefully.
const mcpScriptsServerShutdownDelay = 500 * time.Millisecond

// InspectWorkflowMCP inspects MCP servers used by a workflow and lists available tools, resources, and roots
func InspectWorkflowMCP(ctx context.Context, workflowFile string, serverFilter string, toolFilter string, verbose bool, useActionsSecrets bool) error { //nolint:largefunc // Existing inspection flow remains centralized.
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
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Inspecting MCP servers in: "+workflowPath))
	}

	// Use the compiler to parse the workflow file
	// This automatically handles imports, merging, and validation
	compiler := workflow.NewCompiler(
		workflow.WithVerbose(verbose),
	)
	workflowData, err := compiler.ParseWorkflowFile(workflowPath)
	if err != nil {
		// Handle shared workflow error separately (not a fatal error for inspection)
		if errors.As(err, new(*workflow.SharedWorkflowError)) {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Cannot inspect shared/imported workflows directly - they must be imported by a main workflow"))
			return nil
		}

		// Handle redirect-only workflow error separately (not a fatal error for inspection)
		if errors.As(err, new(*workflow.RedirectOnlyWorkflowError)) {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Cannot inspect redirect-only workflows directly - run 'gh aw update' to follow the redirect and get the full workflow"))
			return nil
		}

		errMsg := fmt.Sprintf("failed to parse workflow file: %v", err)
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(errMsg))
		return fmt.Errorf("failed to parse workflow file: %w", err)
	}

	mcpInspectLog.Printf("Workflow parsed: name=%s, has_mcp_scripts=%t",
		workflowData.Name, workflowData.MCPScripts != nil)

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

	mcpInspectLog.Printf("Extracted %d MCP configs (server_filter=%q)", len(mcpConfigs), serverFilter)

	// Filter out safe-outputs MCP servers for inspection
	mcpConfigs = filterOutSafeOutputs(mcpConfigs)
	mcpInspectLog.Printf("After filtering safe-outputs: %d MCP configs remain", len(mcpConfigs))

	// Start mcp-scripts server if present
	var mcpScriptsServerCmd *exec.Cmd
	var mcpScriptsTmpDir string
	if workflowData != nil && workflowData.MCPScripts != nil && len(workflowData.MCPScripts.Tools) > 0 {
		// Start mcp-scripts server and add it to the list of MCP configs
		config, serverCmd, tmpDir, err := startMCPScriptsServer(ctx, workflowData.MCPScripts, verbose)
		if err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to start mcp-scripts server: %v", err)))
			}
			mcpInspectLog.Printf("Failed to start mcp-scripts server: %v", err)
		} else {
			mcpScriptsServerCmd = serverCmd
			mcpScriptsTmpDir = tmpDir
			// Add mcp-scripts config to the list of MCP servers to inspect
			mcpConfigs = append(mcpConfigs, *config)
			mcpInspectLog.Print("MCP Scripts server started successfully")
		}
	}

	// Cleanup mcp-scripts server when done
	if mcpScriptsServerCmd != nil {
		defer func() {
			if mcpScriptsServerCmd.Process != nil {
				// Try graceful shutdown first
				if err := mcpScriptsServerCmd.Process.Signal(os.Interrupt); err != nil && verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to send interrupt signal: %v", err)))
				}
				// Wait a moment for graceful shutdown (respects context cancellation)
				select {
				case <-time.After(mcpScriptsServerShutdownDelay):
				case <-ctx.Done():
				}
				// Attempt force kill (may fail if process already exited gracefully, which is fine)
				_ = mcpScriptsServerCmd.Process.Kill()
			}
			// Cleanup temporary directory
			if mcpScriptsTmpDir != "" {
				if err := os.RemoveAll(mcpScriptsTmpDir); err != nil && verbose {
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
	if toolFilter == "" {
		if hierarchy := renderMCPInspectionTree(workflowPath, workflowData, mcpConfigs); hierarchy != "" {
			fmt.Fprintln(os.Stderr)
			fmt.Fprintln(os.Stderr, console.FormatSectionHeader("MCP Server Hierarchy"))
			fmt.Fprintln(os.Stderr, hierarchy)
		}
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

func renderMCPInspectionTree(workflowPath string, workflowData *workflow.WorkflowData, mcpConfigs []parser.RegistryMCPServerConfig) string {
	if len(mcpConfigs) == 0 {
		return ""
	}

	workflowLabel := workflowPath
	if workflowData != nil && workflowData.WorkflowID != "" {
		workflowLabel = workflowData.WorkflowID
	} else if workflowPath != "" {
		workflowLabel = filepath.Base(workflowPath)
	}

	serversTree := tree.Root("MCP Servers")
	sortedConfigs := append([]parser.RegistryMCPServerConfig(nil), mcpConfigs...)
	slices.SortFunc(sortedConfigs, func(a, b parser.RegistryMCPServerConfig) int {
		if nameCmp := strings.Compare(a.Name, b.Name); nameCmp != 0 {
			return nameCmp
		}
		return strings.Compare(a.Type, b.Type)
	})

	for _, config := range sortedConfigs {
		serversTree.Child(fmt.Sprintf("%s (%s)", config.Name, config.Type))
	}

	engineID := workflow.ResolveEngineID(workflowData)
	if engineID == "" {
		engineID = "unknown"
	}

	return tree.
		Root("Workflow: " + workflowLabel).
		Child("Engine: " + engineID).
		Child(serversTree).
		Enumerator(tree.RoundedEnumerator).
		EnumeratorStyle(styles.TreeEnumerator).
		ItemStyle(styles.TreeNode).
		String()
}

// NewMCPInspectSubcommand creates the mcp inspect subcommand
// This is the former mcp inspect command now nested under mcp
func NewMCPInspectSubcommand() *cobra.Command { //nolint:largefunc // Existing command setup remains centralized.
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

MCP Scripts servers are automatically detected and inspected when present in the workflow.

The workflow-id-or-file can be:
- A workflow ID (basename without .md extension, e.g., "weekly-research")
- A file path (e.g., "weekly-research.md" or ".github/workflows/weekly-research.md")

When no workflow is provided, this command lists workflows that have MCP server configurations
(equivalent to 'gh aw mcp list'). To inspect tools/resources/roots, pass a specific workflow.

The command will:
- Parse the workflow file to extract MCP server configurations
- Start each MCP server (stdio, docker, http)
- Automatically start and inspect mcp-scripts server if present
- Query available tools, resources, and roots
- Validate required secrets are available
- Display results in formatted tables with error details`,
		Example: `  gh aw mcp inspect                    # List workflows with MCP servers
  gh aw mcp inspect weekly-research    # Inspect MCP servers in weekly-research.md
  gh aw mcp inspect daily-news --server tavily  # Inspect only the tavily server
  gh aw mcp inspect weekly-research --server github --tool create_issue  # Show details for a specific tool
  gh aw mcp inspect weekly-research -v # Verbose output with detailed connection info
  gh aw mcp inspect weekly-research --inspector  # Launch @modelcontextprotocol/inspector
  gh aw mcp inspect weekly-research --check-secrets  # Check GitHub Actions secrets`,
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
				return errors.New("--tool flag requires --server flag to be specified")
			}

			// Handle spawn inspector flag
			if spawnInspector {
				return spawnMCPInspector(cmd.Context(), workflowFile, serverFilter, verbose)
			}

			return InspectWorkflowMCP(cmd.Context(), workflowFile, serverFilter, toolFilter, verbose, checkSecrets)
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

// buildFrontmatterFromWorkflowData constructs a fully resolved frontmatter map from WorkflowData.
// It uses RawFrontmatter as the base (for safe-outputs, mcp-scripts, on, etc.) and overlays the
// fully merged tools and mcp-servers so that configurations from imported workflows are included.
// This is critical for `mcp inspect` to display all MCP servers from the main workflow and imports.
func buildFrontmatterFromWorkflowData(workflowData *workflow.WorkflowData) map[string]any {
	frontmatter := make(map[string]any)

	// Start from RawFrontmatter to preserve top-level keys like safe-outputs, mcp-scripts, on, etc.
	// Guard against nil (maps.Copy is safe with a nil source, but explicit is clearer).
	if workflowData.RawFrontmatter != nil {
		maps.Copy(frontmatter, workflowData.RawFrontmatter)
	}

	// Override tools with the fully merged result (includes built-in tools from imports such as
	// GitHub and removed built-ins). ExtractMCPConfigurations only picks up GitHub
	// from the tools key, so extra user-defined entries here are harmless.
	if len(workflowData.Tools) > 0 {
		frontmatter["tools"] = workflowData.Tools
	}

	// Override mcp-servers with the fully merged result (includes user-defined servers from all
	// imports). ResolvedMCPServers is set to allMCPServers in processToolsAndMarkdown, which merges
	// the main workflow's mcp-servers with every imported workflow's mcp-servers.
	if len(workflowData.ResolvedMCPServers) > 0 {
		frontmatter["mcp-servers"] = workflowData.ResolvedMCPServers
	} else {
		// No mcp-servers defined in main workflow or any import; remove any stale key so
		// ExtractMCPConfigurations falls through to check tools for built-in MCP tools.
		delete(frontmatter, "mcp-servers")
	}

	return frontmatter
}
