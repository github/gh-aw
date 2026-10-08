package cli

import (
	"context"
	"os/exec"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// execCmdFunc is the type for the command execution function passed to tool registrations.
type execCmdFunc func(ctx context.Context, args ...string) *exec.Cmd

// MCP tool handlers in this package return (*mcp.CallToolResult, any, error).
// The second return value is a reserved SDK extension slot and must be nil for now.

// createMCPServer creates and configures the MCP server with all tools
func createMCPServer(cmdPath string, actor string, validateActor bool, manifestCacheFile string, env []string) *mcp.Server {
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
	}, &mcp.ServerOptions{
		Capabilities: &mcp.ServerCapabilities{
			Tools: &mcp.ToolCapabilities{
				ListChanged: false, // Tools are static, no notifications needed
			},
		},
		Logger: logger.NewSlogLoggerWithHandler(mcpLog),
	})

	// Register read-only tools
	registerStatusTool(server)

	if err := registerCompileTool(server, execCmd, manifestCacheFile); err != nil {
		return server
	}

	// Register privileged tools (require write+ access)
	if err := registerLogsTool(server, execCmd, actor, validateActor); err != nil {
		return server
	}

	if err := registerAuditTool(server, execCmd, actor, validateActor); err != nil {
		return server
	}

	if err := registerAuditDiffTool(server, execCmd, actor, validateActor); err != nil {
		return server
	}

	// Register remaining read-only tools
	registerChecksTool(server)
	registerMCPInspectTool(server, execCmd)
	if err := registerWorkQueueTool(server, execCmd); err != nil {
		return server
	}

	// Register workflow management tools
	registerAddTool(server, execCmd)
	registerUpdateTool(server, execCmd)
	registerFixTool(server, execCmd)

	// Add receiving middleware to transform raw JSON-schema "additional properties"
	// validation errors into helpful messages with "Did you mean?" suggestions.
	server.AddReceivingMiddleware(argumentValidationMiddleware(mcpToolParams()))

	return server
}
