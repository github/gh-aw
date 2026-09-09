package cli

import (
	"context"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var mcpToolsManagementLog = logger.New("cli:mcp_tools_management")

// addArgs holds the input parameters for the add tool.
type addArgs struct {
	Workflows []string `json:"workflows" jsonschema:"Workflows to add (e.g., 'owner/repo/workflow-name' or 'owner/repo/workflow-name@version')"`
	Number    int      `json:"number,omitempty" jsonschema:"Create multiple numbered copies (corresponds to -c flag, default: 1)"`
	Name      string   `json:"name,omitempty" jsonschema:"Specify name for the added workflow - without .md extension (corresponds to -n flag)"`
}

// registerAddTool registers the add tool with the MCP server.
func registerAddTool(server *mcp.Server, execCmd execCmdFunc) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "add",
		Annotations: &mcp.ToolAnnotations{
			OpenWorldHint: boolPtr(true),
		},
		Description: "Add workflows from remote repositories to .github/workflows",
		Icons:       mcpToolIcons("➕"),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args addArgs) (*mcp.CallToolResult, any, error) {
		// Check for cancellation before starting
		select {
		case <-ctx.Done():
			return nil, nil, newMCPError(jsonrpc.CodeInternalError, "request cancelled", ctx.Err().Error())
		default:
		}

		// Validate required arguments
		if len(args.Workflows) == 0 {
			return nil, nil, newMCPError(jsonrpc.CodeInvalidParams, "missing required parameter: at least one workflow specification is required", nil)
		}
		for _, workflowSpec := range args.Workflows {
			if isRepositoryOnlyWorkflowSpec(workflowSpec) {
				return nil, nil, newMCPError(jsonrpc.CodeInvalidParams, "failed to add workflows", map[string]any{
					"error": "workflow specification must be in format 'owner/repo/workflow-name[@version]'",
					"spec":  workflowSpec,
				})
			}
		}

		mcpToolsManagementLog.Printf("add tool invoked: workflows=%d, number=%d, name=%q", len(args.Workflows), args.Number, args.Name)

		// Build command arguments
		cmdArgs := []string{"add"}

		// Add workflows
		cmdArgs = append(cmdArgs, args.Workflows...)

		// Add optional flags
		if args.Number > 0 {
			cmdArgs = append(cmdArgs, "-c", strconv.Itoa(args.Number))
		}
		if args.Name != "" {
			cmdArgs = append(cmdArgs, "-n", args.Name)
		}

		// Execute the CLI command
		output, err := runMCPExecCombinedOutput(ctx, execCmd, cmdArgs...)

		if err != nil {
			return nil, nil, newMCPError(jsonrpc.CodeInternalError, "failed to add workflows", map[string]any{"error": err.Error(), "output": string(output)})
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(output)},
			},
		}, nil, nil
	})
}

func isRepositoryOnlyWorkflowSpec(spec string) bool {
	if strings.HasPrefix(spec, "http://") || strings.HasPrefix(spec, "https://") || isLocalWorkflowPath(spec) {
		return false
	}

	// Strip optional @version suffix so owner/repo@v1 is treated as repository-only.
	specWithoutVersion := strings.SplitN(spec, "@", 2)[0]
	slashParts := strings.Split(specWithoutVersion, "/")
	return len(slashParts) == 2 && slashParts[0] != "" && slashParts[1] != ""
}

// updateArgs holds the input parameters for the update tool.
type updateArgs struct {
	Workflows []string `json:"workflows,omitempty" jsonschema:"Workflow IDs to update (empty for all workflows)"`
	Major     bool     `json:"major,omitempty" jsonschema:"Allow major version updates when updating tagged releases"`
	Force     bool     `json:"force,omitempty" jsonschema:"Force update even if no changes detected"`
}

// registerUpdateTool registers the update tool with the MCP server.
func registerUpdateTool(server *mcp.Server, execCmd execCmdFunc) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "update",
		Annotations: &mcp.ToolAnnotations{
			OpenWorldHint: boolPtr(true),
		},
		Description: `Update workflows from their source repositories and check for gh-aw updates.

The command:
1. Checks if a newer version of gh-aw is available
2. Updates workflows using the 'source' field in the workflow frontmatter
3. Compiles each workflow immediately after update

For workflow updates, it fetches the latest version based on the current ref:
- If the ref is a tag, it updates to the latest release (use major flag for major version updates)
- If the ref is a branch, it fetches the latest commit from that branch
- Otherwise, it fetches the latest commit from the default branch

Returns formatted text output showing:
- Extension update status
- Updated workflows with their new versions
- Compilation status for each updated workflow`,
		Icons: mcpToolIcons("🔄"),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args updateArgs) (*mcp.CallToolResult, any, error) {
		// Check for cancellation before starting
		select {
		case <-ctx.Done():
			return nil, nil, newMCPError(jsonrpc.CodeInternalError, "request cancelled", ctx.Err().Error())
		default:
		}

		mcpToolsManagementLog.Printf("update tool invoked: workflows=%d, major=%v, force=%v", len(args.Workflows), args.Major, args.Force)

		// Build command arguments
		cmdArgs := []string{"update"}

		// Add workflow IDs if specified
		cmdArgs = append(cmdArgs, args.Workflows...)

		// Add optional flags
		if args.Major {
			cmdArgs = append(cmdArgs, "--major")
		}
		if args.Force {
			cmdArgs = append(cmdArgs, "--force")
		}

		// Execute the CLI command
		output, err := runMCPExecCombinedOutput(ctx, execCmd, cmdArgs...)

		if err != nil {
			return nil, nil, newMCPError(jsonrpc.CodeInternalError, "failed to update workflows", map[string]any{"error": err.Error(), "output": string(output)})
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(output)},
			},
		}, nil, nil
	})
}

// fixArgs holds the input parameters for the fix tool.
type fixArgs struct {
	Workflows    []string `json:"workflows,omitempty" jsonschema:"Workflow IDs to fix (empty for all workflows)"`
	Write        bool     `json:"write,omitempty" jsonschema:"Write changes to files (default is dry-run)"`
	ListCodemods bool     `json:"list_codemods,omitempty" jsonschema:"List all available codemods and exit"`
}

// registerFixTool registers the fix tool with the MCP server.
func registerFixTool(server *mcp.Server, execCmd execCmdFunc) { //nolint:largefunc // Existing MCP tool registration remains centralized.
	mcp.AddTool(server, &mcp.Tool{
		Name: "fix",
		Annotations: &mcp.ToolAnnotations{
			IdempotentHint:  true,
			DestructiveHint: boolPtr(false),
			OpenWorldHint:   boolPtr(false),
		},
		Description: `Apply automatic codemod-style fixes to agentic workflow files.

This command applies a registry of codemods that automatically update deprecated fields
and migrate to new syntax. Codemods preserve formatting and comments as much as possible.

Available codemods:
• timeout-minutes-migration: Replaces 'timeout_minutes' with 'timeout-minutes'
• network-firewall-migration: Removes deprecated 'network.firewall' field
• mcp-scripts-mode-removal: Removes deprecated 'mcp-scripts.mode' field

If no workflows are specified, all Markdown files in .github/workflows will be processed.

The command will:
1. Scan workflow files for deprecated fields
2. Apply relevant codemods to fix issues
3. Report what was changed in each file
4. Write updated files back to disk (with write flag)

Returns formatted text output showing:
- List of workflow files processed
- Which codemods were applied to each file
- Summary of fixes applied`,
		Icons: mcpToolIcons("🔧"),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args fixArgs) (*mcp.CallToolResult, any, error) {
		// Check for cancellation before starting
		select {
		case <-ctx.Done():
			return nil, nil, newMCPError(jsonrpc.CodeInternalError, "request cancelled", ctx.Err().Error())
		default:
		}

		mcpToolsManagementLog.Printf("fix tool invoked: workflows=%d, write=%v, list_codemods=%v", len(args.Workflows), args.Write, args.ListCodemods)

		// Build command arguments
		cmdArgs := []string{"fix"}

		// Add workflow IDs if specified
		cmdArgs = append(cmdArgs, args.Workflows...)

		// Add optional flags
		if args.Write {
			cmdArgs = append(cmdArgs, "--write")
		}
		if args.ListCodemods {
			cmdArgs = append(cmdArgs, "--list-codemods")
		}

		// Execute the CLI command
		output, err := runMCPExecCombinedOutput(ctx, execCmd, cmdArgs...)

		if err != nil {
			return nil, nil, newMCPError(jsonrpc.CodeInternalError, "failed to fix workflows", map[string]any{"error": err.Error(), "output": string(output)})
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(output)},
			},
		}, nil, nil
	})
}
