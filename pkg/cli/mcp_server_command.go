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
func NewMCPServerCommand() *cobra.Command {
	var port int
	var cmdPath string
	var validateActor bool

	cmd := &cobra.Command{
		Use:   "mcp-server",
		Short: "Run an MCP server that exposes gh aw CLI commands as MCP tools",
		Long: `Run an MCP server that exposes gh aw CLI commands as MCP tools.

This command starts an MCP server that wraps the gh aw CLI. It spawns a subprocess for each tool invocation to ensure that GitHub tokens and other
secrets are not shared with the MCP server process itself.

The server provides the following tools:
  - status      - Show status of agentic workflow files
  - compile     - Compile Markdown workflows to GitHub Actions YAML
  - logs        - Download and analyze workflow logs (requires write access or higher)
  - audit       - Investigate a workflow run, job, or step and generate a report (requires write access or higher)
  - checks      - Classify CI check state for a pull request
  - work-queue  - Read Git-backed work queue state or inspect Work/Claim metadata (read-only)
  - mcp-inspect - Inspect MCP servers in workflows and list available tools
  - add         - Add workflows from remote repositories to .github/workflows
  - update      - Update workflows from their source repositories
  - fix         - Apply automatic codemod-style fixes to workflow files

Access Control:
  The GITHUB_ACTOR environment variable specifies the GitHub username for role-based
  access control. The actor's repository role (admin, maintain, write, etc.) determines
  which tools are available. Tools requiring elevated permissions (logs, audit) are always
  mounted but will return permission denied errors if the actor lacks write access or higher.

  Use the --validate-actor flag to enforce actor validation. When enabled, logs and audit
  tools will return permission denied errors if GITHUB_ACTOR is not set. When disabled
  (default), these tools will work without actor validation.

By default, the server uses stdio transport. Use the --port flag to run
an HTTP server with SSE (Server-Sent Events) transport instead.`,
		Example: `  gh aw mcp-server                                     # Run with stdio transport (default for MCP clients)
  gh aw mcp-server --validate-actor                    # Run with actor validation enforced
  gh aw mcp-server --port 8080                         # Run HTTP server on port 8080 with SSE transport (for web-based clients)
  gh aw mcp-server --cmd ./gh-aw                       # Use custom gh-aw binary path
  GITHUB_ACTOR=octocat gh aw mcp-server                # Set actor via environment variable for access control
  DEBUG=mcp:* GITHUB_ACTOR=octocat gh aw mcp-server    # Run with verbose debug logging and actor set via environment variable`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMCPServer(cmd.Context(), port, cmdPath, validateActor)
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 0, "Port to run HTTP server on (uses stdio if not specified)")
	cmd.Flags().StringVar(&cmdPath, "cmd", "", "Path to gh aw command to use (defaults to 'gh aw')")
	cmd.Flags().BoolVar(&validateActor, "validate-actor", false, "Enforce actor validation (logs/audit tools return errors without GITHUB_ACTOR)")

	return cmd
}

// runMCPServer starts the MCP server on stdio or HTTP transport
func runMCPServer(ctx context.Context, port int, cmdPath string, validateActor bool) error {
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
	server := createMCPServer(cmdPath, actor, validateActor, manifestCacheFile, mcpServerEnv)

	if port > 0 {
		// Run HTTP server with SSE transport
		return runHTTPServer(server, port)
	}

	// Run stdio transport
	mcpLog.Print("MCP server ready on stdio")
	return server.Run(ctx, &mcp.StdioTransport{})
}
