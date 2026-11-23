package cli

import (
	"context"
	"os/exec"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const mcpDiscoveryCacheTTLMs = 60_000

func mcpServerOptions() *mcp.ServerOptions {
	return &mcp.ServerOptions{
		SupportedProtocolVersions: []string{
			"2026-07-28",
			"2025-11-25",
			"2025-06-18",
			"2025-03-26",
			"2024-11-05",
		},
		Capabilities: &mcp.ServerCapabilities{
			Tools: &mcp.ToolCapabilities{
				ListChanged: false, // Tools are static, no notifications needed
			},
		},
		Logger: logger.NewSlogLoggerWithHandler(mcpLog),
		SetCacheable: func(_ context.Context, req mcp.Request, c *mcp.Cacheable) {
			switch req.(type) {
			case *mcp.ListToolsRequest, *mcp.ServerRequest[*mcp.DiscoverParams]:
				c.TTLMs = mcpDiscoveryCacheTTLMs
				c.CacheScope = "private"
			}
		},
	}
}

// execCmdFunc is the type for the command execution function passed to tool registrations.
type execCmdFunc func(ctx context.Context, args ...string) *exec.Cmd

// MCP tool handlers in this package return (*mcp.CallToolResult, any, error).
// The second return value is a reserved SDK extension slot and must be nil for now.

type mcpServerCaches struct {
	logs              *mcpLogsReportCache
	healthCheckpoints *healthMCPCheckpoints
}

// createMCPServer creates and configures the MCP server with all tools
func createMCPServer(cmdPath string, actor string, validateActor bool, allowWriteTools bool, manifestCacheFile string, env []string, caches ...mcpServerCaches) *mcp.Server {
	// Helper function to execute command with proper path
	execCmd := func(ctx context.Context, args ...string) *exec.Cmd {
		var cmd *exec.Cmd
		if cmdPath != "" {
			// Use custom command path
			cmd = exec.CommandContext(ctx, cmdPath, args...)
		} else {
			// Use default gh aw command with proper token handling
			cmd = workflow.ExecGHContext(ctx, append([]string{"aw"}, args...)...)
		}
		if env != nil {
			cmd.Env = append([]string(nil), env...)
		}
		return cmd
	}

	// Log actor and validation settings
	if validateActor {
		if actor != "" {
			mcpLog.Printf("Actor validation enabled: actor=%s (logs/audit tools will check permissions)", actor)
		} else {
			mcpLog.Print("Actor validation enabled: no actor specified (logs/audit tools will deny access)")
		}
	} else {
		if actor != "" {
			mcpLog.Printf("Actor validation disabled: actor=%s (logs/audit tools will allow access)", actor)
		} else {
			mcpLog.Print("Actor validation disabled: no actor specified (logs/audit tools will allow access)")
		}
	}

	// Create MCP server with capabilities and logging
	// Note: Schema caching is automatic in go-sdk v1.3.0+ (eliminates repeated reflection overhead)
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "gh-aw",
		Version: GetVersion(),
	}, mcpServerOptions())

	server.AddReceivingMiddleware(mcpWriteToolsMiddleware(allowWriteTools, actor, validateActor))

	if err := registerMCPTools(server, execCmd, actor, validateActor, manifestCacheFile, allowWriteTools, caches...); err != nil {
		return server
	}

	// Add receiving middleware to transform raw JSON-schema "additional properties"
	// validation errors into helpful messages with "Did you mean?" suggestions.
	server.AddReceivingMiddleware(argumentValidationMiddleware(mcpToolParams()))

	return server
}

func registerMCPTools(server *mcp.Server, execCmd execCmdFunc, actor string, validateActor bool, manifestCacheFile string, allowWriteTools bool, caches ...mcpServerCaches) error {
	var cache mcpServerCaches
	if len(caches) > 0 {
		cache = caches[0]
	}
	// Register read-only tools
	registerStatusTool(server)
	registerHealthTool(server, execCmd, cache.healthCheckpoints)

	if err := registerCompileTool(server, execCmd, manifestCacheFile, allowWriteTools); err != nil {
		return err
	}

	// Register privileged tools (require write+ access)
	var logsCache []*mcpLogsReportCache
	if cache.logs != nil {
		logsCache = append(logsCache, cache.logs)
	}
	if err := registerLogsTool(server, execCmd, actor, validateActor, logsCache...); err != nil {
		return err
	}

	if err := registerAuditTool(server, execCmd, actor, validateActor); err != nil {
		return err
	}

	if err := registerAuditDiffTool(server, execCmd, actor, validateActor); err != nil {
		return err
	}

	// Register remaining read-only tools
	registerChecksTool(server)
	registerMCPInspectTool(server, execCmd)
	if err := registerWorkQueueTool(server, execCmd); err != nil {
		return err
	}

	// Register workflow management tools
	registerAddTool(server, execCmd)
	registerUpdateTool(server, execCmd)
	registerFixTool(server, execCmd)

	return nil
}
