package cli

import (
	"context"
	"os"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

var mcpLog = logger.New("mcp:server")

// NewMCPServerCommand creates the mcp-server command
func NewMCPServerCommand() *cobra.Command { //nolint:largefunc // Existing command setup remains centralized.
	var port int
	var cmdPath string
	var validateActor bool
	var allowWriteTools bool

	cmd := &cobra.Command{
		Use:   "mcp-server",
		Short: "Run an MCP server that exposes gh aw CLI commands as MCP tools",
		Long: `Run an MCP server that exposes gh aw CLI commands as MCP tools.

This command starts an MCP server that wraps the gh aw CLI. It spawns a subprocess for each tool invocation to ensure that GitHub tokens and other
secrets are not shared with the MCP server process itself.

The server provides the following tools:
  - status      - Show status of agentic workflow files
  - health      - Read workflow health metrics, success rates, and execution trends
  - compile     - Compile Markdown workflows to GitHub Actions YAML
  - logs        - Download and analyze workflow logs (requires write access or higher)
  - audit       - Investigate a workflow run, job, or step and generate a report (requires write access or higher)
  - checks      - Classify CI check state for a pull request
  - work-queue  - Read Git-backed work queue state or inspect Work/Claim metadata (read-only)
  - mcp-inspect - List MCP server configurations without starting or connecting to servers
  - add         - Add workflows from remote repositories to .github/workflows
  - update      - Update workflows from their source repositories
  - fix         - Apply automatic codemod-style fixes to workflow files

Access Control:
  Compile uses --no-emit by default and does not modify files. Fixes, dry-run
  compilation, and external scanners require --allow-write-tools.
  Workflow-writing tools (add, update, fix) are disabled by default.
  The operator must explicitly enable writes with --allow-write-tools. Enable this
  only for trusted MCP clients and workflow sources: these tools can modify
  GitHub Actions workflows and invoke compiler scanners.

  mcp-inspect only lists configuration; live MCP discovery remains CLI-only.
  Run gh aw mcp inspect <workflow> directly only after reviewing and trusting
  the workflow's commands, packages, container images, and endpoints.

  The GITHUB_ACTOR environment variable specifies the GitHub username for role-based
  access control. The actor's repository role (admin, maintain, write, etc.) determines
  which tools are available. Tools requiring elevated permissions (logs, audit) are always
  mounted but will return permission denied errors if the actor lacks write access or higher.

  Use the --validate-actor flag to enforce actor validation for logs, audit, audit-diff,
  and enabled workflow-writing tools. When enabled, these tools return permission
  denied errors if GITHUB_ACTOR is not set. Disabling actor validation does not
  enable workflow-writing tools.

By default, the server uses stdio transport. Use the --port flag to run
an HTTP server with SSE (Server-Sent Events) transport instead.`,
		Example: `  gh aw mcp-server                                     # Run with stdio transport (default for MCP clients)
  gh aw mcp-server --validate-actor                    # Run with actor validation enforced
  gh aw mcp-server --allow-write-tools                 # Enable workflow modifications for trusted clients
  gh aw mcp-server --port 8080                         # Run HTTP server on port 8080 with SSE transport (for web-based clients)
  gh aw mcp-server --cmd ./gh-aw                       # Use custom gh-aw binary path
  GITHUB_ACTOR=octocat gh aw mcp-server                # Set actor via environment variable for access control
  DEBUG=mcp:* GITHUB_ACTOR=octocat gh aw mcp-server    # Run with verbose debug logging and actor set via environment variable`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMCPServer(cmd.Context(), port, cmdPath, validateActor, allowWriteTools)
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 0, "Port to run HTTP server on (uses stdio if not specified)")
	cmd.Flags().StringVar(&cmdPath, "cmd", "", "Path to gh aw command to use (defaults to 'gh aw')")
	cmd.Flags().BoolVar(&validateActor, "validate-actor", false, "Enforce actor validation for logs/audit and enabled workflow-writing tools")
	cmd.Flags().BoolVar(&allowWriteTools, "allow-write-tools", false, "Allow add/update/fix/compile to modify workflows (trusted clients and sources only)")

	return cmd
}

// runMCPServer starts the MCP server on stdio or HTTP transport
func runMCPServer(ctx context.Context, port int, cmdPath string, validateActor bool, allowWriteTools bool) error { //nolint:largefunc // Existing server startup remains centralized.
	mcpServerEnv := withNonInteractiveCIEnv(nil)

	// Get actor from environment variable
	actor := os.Getenv("GITHUB_ACTOR") //nolint:osgetenvlibrary

	if validateActor {
		mcpLog.Printf("Actor validation enabled (--validate-actor flag)")
	}

	if actor != "" {
		mcpLog.Printf("Using actor: %s", actor)
	} else {
		mcpLog.Print("No actor specified (GITHUB_ACTOR environment variable)")
	}

	if port > 0 {
		mcpLog.Printf("Starting MCP server on HTTP port %d", port)
	} else {
		mcpLog.Print("Starting MCP server with stdio transport")
	}

	// Determine, log, and validate the binary path only if --cmd flag is not provided
	// When --cmd is provided, the user explicitly specified the binary path to use
	if cmdPath == "" {
		// Attempt to detect the binary path and assign it to cmdPath
		// This ensures createMCPServer receives the actual binary path instead of falling back to "gh aw"
		detectedPath, err := logAndValidateBinaryPath()
		if err == nil && detectedPath != "" {
			cmdPath = detectedPath
			mcpLog.Printf("Using detected binary path: %s", cmdPath)
		}
	}

	// Log current working directory
	if cwd, err := os.Getwd(); err == nil {
		mcpLog.Printf("Current working directory: %s", cwd)
	} else {
		mcpLog.Printf("WARNING: Failed to get current working directory: %v", err)
	}

	// Validate that the CLI and secrets are properly configured
	// Note: Validation failures are logged as warnings but don't prevent server startup
	// This allows the server to start in test environments or non-repository directories
	if err := validateMCPServerConfiguration(ctx, cmdPath, mcpServerEnv); err != nil {
		mcpLog.Printf("Configuration validation warning: %v", err)
	}

	// Pre-cache lock-file manifests at startup, before any agent can modify the working tree.
	// The cache is serialised to a temp file so that each compile subprocess invocation
	// can reference it via --prior-manifest-file.
	manifestCacheFile := ""
	manifestCache := CollectLockFileManifests("")
	if len(manifestCache) > 0 {
		cacheFile, err := WritePriorManifestFile(manifestCache)
		if err != nil {
			mcpLog.Printf("Failed to write manifest cache file: %v (safe update will fall back to git HEAD / filesystem)", err)
		} else {
			manifestCacheFile = cacheFile
			mcpLog.Printf("Manifest cache written to %s (%d entries)", cacheFile, len(manifestCache))
			// Clean up the temp file when the server exits
			defer func() {
				if removeErr := os.Remove(cacheFile); removeErr != nil && !os.IsNotExist(removeErr) {
					mcpLog.Printf("Failed to remove manifest cache file %s: %v", cacheFile, removeErr)
				}
			}()
		}
	}

	// Create the server configuration
	logsCache := &mcpLogsReportCache{}
	defer func() {
		if err := logsCache.close(); err != nil {
			mcpLog.Printf("Failed to clean up private logs cache: %v", err)
		}
	}()
	server := createMCPServer(cmdPath, actor, validateActor, allowWriteTools, manifestCacheFile, mcpServerEnv, logsCache)

	if port > 0 {
		// Run HTTP server with SSE transport
		return runHTTPServer(ctx, server, port)
	}

	// Run stdio transport
	mcpLog.Print("MCP server ready on stdio")
	return server.Run(ctx, &mcp.StdioTransport{})
}
