package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

var mcpLog = logger.New("mcp:server")

// mcpErrorData marshals data to JSON for use in jsonrpc.Error.Data field.
// Returns nil if marshaling fails to avoid errors in error handling.
func mcpErrorData(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		// Log the error but return nil to avoid breaking error handling
		mcpLog.Printf("Failed to marshal error data: %v", err)
		return nil
	}
	return data
}

// NewMCPServerCommand creates the mcp-server command
func NewMCPServerCommand() *cobra.Command {
	var port int
	var cmdPath string

	cmd := &cobra.Command{
		Use:   "mcp-server",
		Short: "Run an MCP (Model Context Protocol) server exposing gh aw commands as tools",
		Long: `Run an MCP server that exposes gh aw CLI commands as MCP tools.

This command starts an MCP server that wraps the gh aw CLI, spawning subprocess
calls for each tool invocation. This design ensures that GitHub tokens and other
secrets are not shared with the MCP server process itself.

The server provides the following tools:
  - status      - Show status of agentic workflow files
  - compile     - Compile Markdown workflows to GitHub Actions YAML
  - logs        - Download and analyze workflow logs
  - audit       - Investigate a workflow run, job, or step and generate a report
  - mcp-inspect - Inspect MCP servers in workflows and list available tools
  - add         - Add workflows from remote repositories to .github/workflows
  - update      - Update workflows from their source repositories
  - fix         - Apply automatic codemod-style fixes to workflow files

By default, the server uses stdio transport. Use the --port flag to run
an HTTP server with SSE (Server-Sent Events) transport instead.

Examples:
  gh aw mcp-server                    # Run with stdio transport (default for MCP clients)
  gh aw mcp-server --port 8080        # Run HTTP server on port 8080 (for web-based clients)
  gh aw mcp-server --cmd ./gh-aw      # Use custom gh-aw binary path
  DEBUG=mcp:* gh aw mcp-server        # Run with verbose logging for debugging`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMCPServer(port, cmdPath)
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 0, "Port to run HTTP server on (uses stdio if not specified)")
	cmd.Flags().StringVar(&cmdPath, "cmd", "", "Path to gh aw command to use (defaults to 'gh aw')")

	return cmd
}

// checkAndLogGHVersion checks if gh CLI is available and logs its version
func checkAndLogGHVersion() {
	cmd := workflow.ExecGH("version")
	output, err := cmd.CombinedOutput()

	if err != nil {
		mcpLog.Print("WARNING: gh CLI not found in PATH")
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("gh CLI not found in PATH - some MCP server operations may fail"))
		return
	}

	// Parse and log the version
	versionOutput := strings.TrimSpace(string(output))
	mcpLog.Printf("gh CLI version: %s", versionOutput)

	// Extract just the first line for cleaner logging to stderr
	firstLine := strings.Split(versionOutput, "\n")[0]
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("gh CLI: %s", firstLine)))
}

// runMCPServer starts the MCP server on stdio or HTTP transport
func runMCPServer(port int, cmdPath string) error {
	if port > 0 {
		mcpLog.Printf("Starting MCP server on HTTP port %d", port)
	} else {
		mcpLog.Print("Starting MCP server with stdio transport")
	}

	// Log current working directory
	if cwd, err := os.Getwd(); err == nil {
		mcpLog.Printf("Current working directory: %s", cwd)
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Current working directory: %s", cwd)))
	} else {
		mcpLog.Printf("WARNING: Failed to get current working directory: %v", err)
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to get current working directory: %v", err)))
	}

	// Check and log gh CLI version
	checkAndLogGHVersion()

	// Validate that the CLI and secrets are properly configured
	// Note: Validation failures are logged as warnings but don't prevent server startup
	// This allows the server to start in test environments or non-repository directories
	if err := validateMCPServerConfiguration(cmdPath); err != nil {
		mcpLog.Printf("Configuration validation warning: %v", err)
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Configuration validation warning: %v", err)))
	}

	// Create the server configuration
	server := createMCPServer(cmdPath)

	if port > 0 {
		// Run HTTP server with SSE transport
		return runHTTPServer(server, port)
	}

	// Run stdio transport
	mcpLog.Print("MCP server ready on stdio")
	return server.Run(context.Background(), &mcp.StdioTransport{})
}

// createMCPServer creates and configures the MCP server with all tools
func createMCPServer(cmdPath string) *mcp.Server {
	// Helper function to execute command with proper path
	execCmd := func(ctx context.Context, args ...string) *exec.Cmd {
		if cmdPath != "" {
			// Use custom command path
			return exec.CommandContext(ctx, cmdPath, args...)
		}
		// Use default gh aw command with proper token handling
		return workflow.ExecGHContext(ctx, append([]string{"aw"}, args...)...)
	}

	// Create MCP server with capabilities and logging
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

	// Add status tool
	type statusArgs struct {
		Pattern  string `json:"pattern,omitempty" jsonschema:"Optional pattern to filter workflows by name"`
		JqFilter string `json:"jq,omitempty" jsonschema:"Optional jq filter to apply to JSON output"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name: "status",
		Description: `Show status of agentic workflow files and workflows.

Returns a JSON array where each element has the following structure:
- workflow: Name of the workflow file
- agent: AI engine used (e.g., "copilot", "claude", "codex")
- compiled: Whether the workflow is compiled ("Yes", "No", or "N/A")
- status: GitHub workflow status ("active", "disabled", "Unknown")
- time_remaining: Time remaining until workflow deadline (if applicable)

Note: Output can be filtered using the jq parameter.`,
		Icons: []mcp.Icon{
			{Source: "📊"},
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args statusArgs) (*mcp.CallToolResult, any, error) {
		// Check for cancellation before starting
		select {
		case <-ctx.Done():
			return nil, nil, &jsonrpc.Error{
				Code:    jsonrpc.CodeInternalError,
				Message: "request cancelled",
				Data:    mcpErrorData(ctx.Err().Error()),
			}
		default:
		}

		mcpLog.Printf("Executing status tool: pattern=%s, jqFilter=%s", args.Pattern, args.JqFilter)

		// Call GetWorkflowStatuses directly instead of spawning subprocess
		statuses, err := GetWorkflowStatuses(args.Pattern, "", "", "")
		if err != nil {
			return nil, nil, &jsonrpc.Error{
				Code:    jsonrpc.CodeInternalError,
				Message: "failed to get workflow statuses",
				Data:    mcpErrorData(map[string]any{"error": err.Error()}),
			}
		}

		// Marshal to JSON
		jsonBytes, err := json.Marshal(statuses)
		if err != nil {
			return nil, nil, &jsonrpc.Error{
				Code:    jsonrpc.CodeInternalError,
				Message: "failed to marshal workflow statuses",
				Data:    mcpErrorData(map[string]any{"error": err.Error()}),
			}
		}

		outputStr := string(jsonBytes)

		// Apply jq filter if provided
		if args.JqFilter != "" {
			filteredOutput, jqErr := ApplyJqFilter(outputStr, args.JqFilter)
			if jqErr != nil {
				return nil, nil, &jsonrpc.Error{
					Code:    jsonrpc.CodeInvalidParams,
					Message: "invalid jq filter expression",
					Data:    mcpErrorData(map[string]any{"error": jqErr.Error(), "filter": args.JqFilter}),
				}
			}
			outputStr = filteredOutput
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: outputStr},
			},
		}, nil, nil
	})

	// Add compile tool
	type compileArgs struct {
		Workflows  []string `json:"workflows,omitempty" jsonschema:"Workflow files to compile (empty for all)"`
		Strict     bool     `json:"strict,omitempty" jsonschema:"Override frontmatter to enforce strict mode validation for all workflows. Note: Workflows default to strict mode unless frontmatter sets strict: false"`
		Zizmor     bool     `json:"zizmor,omitempty" jsonschema:"Run zizmor security scanner on generated .lock.yml files"`
		Poutine    bool     `json:"poutine,omitempty" jsonschema:"Run poutine security scanner on generated .lock.yml files"`
		Actionlint bool     `json:"actionlint,omitempty" jsonschema:"Run actionlint linter on generated .lock.yml files"`
		Fix        bool     `json:"fix,omitempty" jsonschema:"Apply automatic codemod fixes to workflows before compiling"`
		JqFilter   string   `json:"jq,omitempty" jsonschema:"Optional jq filter to apply to JSON output"`
	}

	// Generate schema with elicitation defaults
	compileSchema, err := GenerateOutputSchema[compileArgs]()
	if err != nil {
		mcpLog.Printf("Failed to generate compile tool schema: %v", err)
		return server
	}
	// Add elicitation default: strict defaults to true (most common case)
	if err := AddSchemaDefault(compileSchema, "strict", true); err != nil {
		mcpLog.Printf("Failed to add default for strict: %v", err)
	}

	mcp.AddTool(server, &mcp.Tool{
		Name: "compile",
		Description: `Compile Markdown workflows to GitHub Actions YAML with optional static analysis tools.

⚠️  IMPORTANT: Any change to .github/workflows/*.md files MUST be compiled using this tool.
This tool generates .lock.yml files from .md workflow files. The .lock.yml files are what GitHub Actions
actually executes, so failing to compile after modifying a .md file means your changes won't take effect.

Workflows use strict mode validation by default (unless frontmatter sets strict: false).
Strict mode enforces: action pinning to SHAs, explicit network config, safe-outputs for write operations,
and refuses write permissions and deprecated fields. Use the strict parameter to override frontmatter settings.

Returns JSON array with validation results for each workflow:
- workflow: Name of the workflow file
- valid: Boolean indicating if compilation was successful
- errors: Array of error objects with type, message, and optional line number
- warnings: Array of warning objects
- compiled_file: Path to the generated .lock.yml file

Note: Output can be filtered using the jq parameter.`,
		InputSchema: compileSchema,
		Icons: []mcp.Icon{
			{Source: "🔨"},
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args compileArgs) (*mcp.CallToolResult, any, error) {
		// Check for cancellation before starting
		select {
		case <-ctx.Done():
			return nil, nil, &jsonrpc.Error{
				Code:    jsonrpc.CodeInternalError,
				Message: "request cancelled",
				Data:    mcpErrorData(ctx.Err().Error()),
			}
		default:
		}

		// Check if any static analysis tools are requested that require Docker images
		if args.Zizmor || args.Poutine || args.Actionlint {
			// Check if Docker images are available; if not, start downloading and return retry message
			if err := CheckAndPrepareDockerImages(ctx, args.Zizmor, args.Poutine, args.Actionlint); err != nil {
				return nil, nil, &jsonrpc.Error{
					Code:    jsonrpc.CodeInternalError,
					Message: "docker images not ready",
					Data:    mcpErrorData(err.Error()),
				}
			}

			// Check for cancellation after Docker image preparation
			select {
			case <-ctx.Done():
				return nil, nil, &jsonrpc.Error{
					Code:    jsonrpc.CodeInternalError,
					Message: "request cancelled",
					Data:    mcpErrorData(ctx.Err().Error()),
				}
			default:
			}
		}

		// Build command arguments
		// Always validate workflows during compilation and use JSON output for MCP
		cmdArgs := []string{"compile", "--validate", "--json"}

		// Add fix flag if requested
		if args.Fix {
			cmdArgs = append(cmdArgs, "--fix")
		}

		// Add strict flag if requested
		if args.Strict {
			cmdArgs = append(cmdArgs, "--strict")
		}

		// Add static analysis flags if requested
		if args.Zizmor {
			cmdArgs = append(cmdArgs, "--zizmor")
		}
		if args.Poutine {
			cmdArgs = append(cmdArgs, "--poutine")
		}
		if args.Actionlint {
			cmdArgs = append(cmdArgs, "--actionlint")
		}

		cmdArgs = append(cmdArgs, args.Workflows...)

		mcpLog.Printf("Executing compile tool: workflows=%v, strict=%v, fix=%v, zizmor=%v, poutine=%v, actionlint=%v",
			args.Workflows, args.Strict, args.Fix, args.Zizmor, args.Poutine, args.Actionlint)

		// Execute the CLI command
		// Use separate stdout/stderr capture instead of CombinedOutput because:
		// - Stdout contains JSON output (--json flag)
		// - Stderr contains console messages that shouldn't be mixed with JSON
		cmd := execCmd(ctx, cmdArgs...)
		stdout, err := cmd.Output()

		// The compile command always outputs JSON to stdout when --json flag is used, even on error.
		// We should return the JSON output to the LLM so it can see validation errors.
		// Only return an MCP error if we cannot get any output at all.
		outputStr := string(stdout)

		// If the command failed but we have output, it's likely compilation errors
		// which are included in the JSON output. Return the output, not an MCP error.
		if err != nil {
			mcpLog.Printf("Compile command exited with error: %v (output length: %d)", err, len(outputStr))
			// If we have no output, this is a real execution failure
			if len(outputStr) == 0 {
				// Try to get stderr for error details
				var stderr string
				if exitErr, ok := err.(*exec.ExitError); ok {
					stderr = string(exitErr.Stderr)
				}
				return nil, nil, &jsonrpc.Error{
					Code:    jsonrpc.CodeInternalError,
					Message: "failed to compile workflows",
					Data:    mcpErrorData(map[string]any{"error": err.Error(), "stderr": stderr}),
				}
			}
			// Otherwise, we have output (likely validation errors in JSON), so continue
			// and return it to the LLM
		}

		// Apply jq filter if provided
		if args.JqFilter != "" {
			filteredOutput, jqErr := ApplyJqFilter(outputStr, args.JqFilter)
			if jqErr != nil {
				return nil, nil, &jsonrpc.Error{
					Code:    jsonrpc.CodeInvalidParams,
					Message: "invalid jq filter expression",
					Data:    mcpErrorData(map[string]any{"error": jqErr.Error(), "filter": args.JqFilter}),
				}
			}
			outputStr = filteredOutput
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: outputStr},
			},
		}, nil, nil
	})

	// Add logs tool
	type logsArgs struct {
		WorkflowName string `json:"workflow_name,omitempty" jsonschema:"Name of the workflow to download logs for (empty for all)"`
		Count        int    `json:"count,omitempty" jsonschema:"Number of workflow runs to download (default: 100)"`
		StartDate    string `json:"start_date,omitempty" jsonschema:"Filter runs created after this date (YYYY-MM-DD or delta like -1d, -1w, -1mo)"`
		EndDate      string `json:"end_date,omitempty" jsonschema:"Filter runs created before this date (YYYY-MM-DD or delta like -1d, -1w, -1mo)"`
		Engine       string `json:"engine,omitempty" jsonschema:"Filter logs by agentic engine type (claude, codex, copilot)"`
		Firewall     bool   `json:"firewall,omitempty" jsonschema:"Filter to only runs with firewall enabled"`
		NoFirewall   bool   `json:"no_firewall,omitempty" jsonschema:"Filter to only runs without firewall enabled"`
		Branch       string `json:"branch,omitempty" jsonschema:"Filter runs by branch name"`
		AfterRunID   int64  `json:"after_run_id,omitempty" jsonschema:"Filter runs with database ID after this value (exclusive)"`
		BeforeRunID  int64  `json:"before_run_id,omitempty" jsonschema:"Filter runs with database ID before this value (exclusive)"`
		Timeout      int    `json:"timeout,omitempty" jsonschema:"Maximum time in seconds to spend downloading logs (default: 50 for MCP server)"`
		JqFilter     string `json:"jq,omitempty" jsonschema:"Optional jq filter to apply to JSON output"`
		MaxTokens    int    `json:"max_tokens,omitempty" jsonschema:"Maximum number of tokens in output before triggering guardrail (default: 12000)"`
	}

	// Generate schema with elicitation defaults
	logsSchema, err := GenerateOutputSchema[logsArgs]()
	if err != nil {
		mcpLog.Printf("Failed to generate logs tool schema: %v", err)
		return server
	}
	// Add elicitation defaults for common parameters
	if err := AddSchemaDefault(logsSchema, "count", 100); err != nil {
		mcpLog.Printf("Failed to add default for count: %v", err)
	}
	if err := AddSchemaDefault(logsSchema, "timeout", 50); err != nil {
		mcpLog.Printf("Failed to add default for timeout: %v", err)
	}
	if err := AddSchemaDefault(logsSchema, "max_tokens", 12000); err != nil {
		mcpLog.Printf("Failed to add default for max_tokens: %v", err)
	}

	mcp.AddTool(server, &mcp.Tool{
		Name: "logs",
		Description: `Download and analyze workflow logs.

Returns JSON with workflow run data and metrics. If the command times out before fetching all available logs, 
a "continuation" field will be present in the response with updated parameters to continue fetching more data.
Check for the presence of the continuation field to determine if there are more logs available.

The continuation field includes all necessary parameters (before_run_id, etc.) to resume fetching from where 
the previous request stopped due to timeout.

⚠️  Output Size Guardrail: If the output exceeds the token limit (default: 12000 tokens), the tool will 
return a schema description and suggested jq filters instead of the full output. Use the 'jq' parameter 
to filter the output to a manageable size, or adjust the 'max_tokens' parameter. Common filters include:
  - .summary (get only summary statistics)
  - .runs[:5] (get first 5 runs)
  - .runs | map(select(.conclusion == "failure")) (get only failed runs)`,
		InputSchema: logsSchema,
		Icons: []mcp.Icon{
			{Source: "📜"},
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args logsArgs) (*mcp.CallToolResult, any, error) {
		// Check for cancellation before starting
		select {
		case <-ctx.Done():
			return nil, nil, &jsonrpc.Error{
				Code:    jsonrpc.CodeInternalError,
				Message: "request cancelled",
				Data:    mcpErrorData(ctx.Err().Error()),
			}
		default:
		}

		// Validate firewall parameters
		if args.Firewall && args.NoFirewall {
			return nil, nil, &jsonrpc.Error{
				Code:    jsonrpc.CodeInvalidParams,
				Message: "conflicting parameters: cannot specify both 'firewall' and 'no_firewall'",
				Data:    nil,
			}
		}

		// Build command arguments
		// Force output directory to /tmp/gh-aw/aw-mcp/logs for MCP server
		cmdArgs := []string{"logs", "-o", "/tmp/gh-aw/aw-mcp/logs"}
		if args.WorkflowName != "" {
			cmdArgs = append(cmdArgs, args.WorkflowName)
		}
		if args.Count > 0 {
			cmdArgs = append(cmdArgs, "-c", strconv.Itoa(args.Count))
		}
		if args.StartDate != "" {
			cmdArgs = append(cmdArgs, "--start-date", args.StartDate)
		}
		if args.EndDate != "" {
			cmdArgs = append(cmdArgs, "--end-date", args.EndDate)
		}
		if args.Engine != "" {
			cmdArgs = append(cmdArgs, "--engine", args.Engine)
		}
		if args.Firewall {
			cmdArgs = append(cmdArgs, "--firewall")
		}
		if args.NoFirewall {
			cmdArgs = append(cmdArgs, "--no-firewall")
		}
		if args.Branch != "" {
			cmdArgs = append(cmdArgs, "--branch", args.Branch)
		}
		if args.AfterRunID > 0 {
			cmdArgs = append(cmdArgs, "--after-run-id", strconv.FormatInt(args.AfterRunID, 10))
		}
		if args.BeforeRunID > 0 {
			cmdArgs = append(cmdArgs, "--before-run-id", strconv.FormatInt(args.BeforeRunID, 10))
		}

		// Set timeout to 50 seconds for MCP server if not explicitly specified
		timeoutValue := args.Timeout
		if timeoutValue == 0 {
			timeoutValue = 50
		}
		cmdArgs = append(cmdArgs, "--timeout", strconv.Itoa(timeoutValue))

		// Always use --json mode in MCP server
		cmdArgs = append(cmdArgs, "--json")

		// Log the command being executed for debugging
		mcpLog.Printf("Executing logs tool: workflow=%s, count=%d, firewall=%v, no_firewall=%v, timeout=%d, command_args=%v",
			args.WorkflowName, args.Count, args.Firewall, args.NoFirewall, timeoutValue, cmdArgs)

		// Execute the CLI command
		// Use separate stdout/stderr capture instead of CombinedOutput because:
		// - Stdout contains JSON output (--json flag)
		// - Stderr contains console messages and error details
		cmd := execCmd(ctx, cmdArgs...)
		stdout, err := cmd.Output()

		// The logs command outputs JSON to stdout when --json flag is used.
		// If the command fails, we need to provide detailed error information.
		outputStr := string(stdout)

		if err != nil {
			// Try to get stderr and exit code for detailed error reporting
			var stderr string
			var exitCode int
			if exitErr, ok := err.(*exec.ExitError); ok {
				stderr = string(exitErr.Stderr)
				exitCode = exitErr.ExitCode()
			}

			mcpLog.Printf("Logs command exited with error: %v (stdout length: %d, stderr length: %d, exit_code: %d)",
				err, len(outputStr), len(stderr), exitCode)

			// Build detailed error data
			errorData := map[string]any{
				"error":     err.Error(),
				"command":   strings.Join(cmdArgs, " "),
				"exit_code": exitCode,
				"stdout":    outputStr,
				"stderr":    stderr,
				"timeout":   timeoutValue,
				"workflow":  args.WorkflowName,
			}

			return nil, nil, &jsonrpc.Error{
				Code:    jsonrpc.CodeInternalError,
				Message: fmt.Sprintf("failed to download workflow logs: %s", err.Error()),
				Data:    mcpErrorData(errorData),
			}
		}

		// Apply jq filter if provided
		if args.JqFilter != "" {
			filteredOutput, err := ApplyJqFilter(outputStr, args.JqFilter)
			if err != nil {
				return nil, nil, &jsonrpc.Error{
					Code:    jsonrpc.CodeInvalidParams,
					Message: "invalid jq filter expression",
					Data:    mcpErrorData(map[string]any{"error": err.Error(), "filter": args.JqFilter}),
				}
			}
			outputStr = filteredOutput
		}

		// Check output size and apply guardrail if needed
		finalOutput, _ := checkLogsOutputSize(outputStr, args.MaxTokens)

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: finalOutput},
			},
		}, nil, nil
	})

	// Add audit tool
	type auditArgs struct {
		RunIDOrURL string `json:"run_id_or_url" jsonschema:"GitHub Actions workflow run ID or URL. Accepts: numeric run ID (e.g., 1234567890), run URL (https://github.com/owner/repo/actions/runs/1234567890), job URL (https://github.com/owner/repo/actions/runs/1234567890/job/9876543210), or job URL with step (https://github.com/owner/repo/actions/runs/1234567890/job/9876543210#step:7:1)"`
		JqFilter   string `json:"jq,omitempty" jsonschema:"Optional jq filter to apply to JSON output"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name: "audit",
		Description: `Investigate a workflow run, job, or specific step and generate a concise report.

Accepts multiple input formats:
- Numeric run ID: 1234567890
- Run URL: https://github.com/owner/repo/actions/runs/1234567890
- Job URL: https://github.com/owner/repo/actions/runs/1234567890/job/9876543210
- Job URL with step: https://github.com/owner/repo/actions/runs/1234567890/job/9876543210#step:7:1

When a job URL is provided:
- If a step number is included (#step:7:1), extracts that specific step's output
- If no step number, finds and extracts the first failing step's output
- Saves job logs and step-specific logs to the output directory

Returns JSON with the following structure:
- overview: Basic run information (run_id, workflow_name, status, conclusion, created_at, started_at, updated_at, duration, event, branch, url, logs_path)
- metrics: Execution metrics (token_usage, estimated_cost, turns, error_count, warning_count)
- jobs: List of job details (name, status, conclusion, duration)
- downloaded_files: List of artifact files (path, size, size_formatted, description, is_directory)
- missing_tools: Tools that were requested but not available (tool, reason, alternatives, timestamp, workflow_name, run_id)
- mcp_failures: MCP server failures (server_name, status, timestamp, workflow_name, run_id)
- errors: Error details (file, line, type, message)
- warnings: Warning details (file, line, type, message)
- tool_usage: Tool usage statistics (name, call_count, max_output_size, max_duration)
- firewall_analysis: Network firewall analysis if available (total_requests, allowed_requests, blocked_requests, allowed_domains, blocked_domains)

Note: Output can be filtered using the jq parameter.`,
		Icons: []mcp.Icon{
			{Source: "🔍"},
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args auditArgs) (*mcp.CallToolResult, any, error) {
		// Check for cancellation before starting
		select {
		case <-ctx.Done():
			return nil, nil, &jsonrpc.Error{
				Code:    jsonrpc.CodeInternalError,
				Message: "request cancelled",
				Data:    mcpErrorData(ctx.Err().Error()),
			}
		default:
		}

		// Build command arguments
		// Force output directory to /tmp/gh-aw/aw-mcp/logs for MCP server (same as logs)
		// Use --json flag to output structured JSON for MCP consumption
		// Pass the run ID or URL directly - the audit command will parse it
		cmdArgs := []string{"audit", args.RunIDOrURL, "-o", "/tmp/gh-aw/aw-mcp/logs", "--json"}

		// Execute the CLI command
		cmd := execCmd(ctx, cmdArgs...)
		output, err := cmd.CombinedOutput()

		if err != nil {
			return nil, nil, &jsonrpc.Error{
				Code:    jsonrpc.CodeInternalError,
				Message: "failed to audit workflow run",
				Data:    mcpErrorData(map[string]any{"error": err.Error(), "output": string(output), "run_id_or_url": args.RunIDOrURL}),
			}
		}

		// Apply jq filter if provided
		outputStr := string(output)
		if args.JqFilter != "" {
			filteredOutput, jqErr := ApplyJqFilter(outputStr, args.JqFilter)
			if jqErr != nil {
				return nil, nil, &jsonrpc.Error{
					Code:    jsonrpc.CodeInvalidParams,
					Message: "invalid jq filter expression",
					Data:    mcpErrorData(map[string]any{"error": jqErr.Error(), "filter": args.JqFilter}),
				}
			}
			outputStr = filteredOutput
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: outputStr},
			},
		}, nil, nil
	})

	// Add mcp-inspect tool
	type mcpInspectArgs struct {
		WorkflowFile string `json:"workflow_file,omitempty" jsonschema:"Workflow file to inspect MCP servers from (empty to list all workflows with MCP servers)"`
		Server       string `json:"server,omitempty" jsonschema:"Filter to inspect only the specified MCP server"`
		Tool         string `json:"tool,omitempty" jsonschema:"Show detailed information about a specific tool (requires server parameter)"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name: "mcp-inspect",
		Description: `Inspect MCP servers used by a workflow and list available tools, resources, and roots.

This tool starts each MCP server configured in the workflow, queries its capabilities,
and displays the results. It supports stdio, Docker, and HTTP MCP servers.

Secret checking is enabled by default to validate GitHub Actions secrets availability.
If GitHub token is not available or has no permissions, secret checking is silently skipped.

When called without workflow_file, lists all workflows that contain MCP server configurations.
When called with workflow_file, inspects the MCP servers in that specific workflow.

Use the server parameter to filter to a specific MCP server.
Use the tool parameter (requires server) to show detailed information about a specific tool.

Returns formatted text output showing:
- Available MCP servers in the workflow
- Tools, resources, and roots exposed by each server
- Secret availability status (if GitHub token is available)
- Detailed tool information when tool parameter is specified`,
		Icons: []mcp.Icon{
			{Source: "🔎"},
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args mcpInspectArgs) (*mcp.CallToolResult, any, error) {
		// Check for cancellation before starting
		select {
		case <-ctx.Done():
			return nil, nil, &jsonrpc.Error{
				Code:    jsonrpc.CodeInternalError,
				Message: "request cancelled",
				Data:    mcpErrorData(ctx.Err().Error()),
			}
		default:
		}

		// Build command arguments
		cmdArgs := []string{"mcp", "inspect"}

		if args.WorkflowFile != "" {
			cmdArgs = append(cmdArgs, args.WorkflowFile)
		}

		if args.Server != "" {
			cmdArgs = append(cmdArgs, "--server", args.Server)
		}

		if args.Tool != "" {
			cmdArgs = append(cmdArgs, "--tool", args.Tool)
		}

		// Always enable secret checking (will be silently ignored if GitHub token is not available)
		cmdArgs = append(cmdArgs, "--check-secrets")

		// Execute the CLI command
		cmd := execCmd(ctx, cmdArgs...)
		output, err := cmd.CombinedOutput()

		if err != nil {
			return nil, nil, &jsonrpc.Error{
				Code:    jsonrpc.CodeInternalError,
				Message: "failed to inspect MCP servers",
				Data:    mcpErrorData(map[string]any{"error": err.Error(), "output": string(output)}),
			}
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(output)},
			},
		}, nil, nil
	})

	// Add add tool
	type addArgs struct {
		Workflows []string `json:"workflows" jsonschema:"Workflows to add (e.g., 'owner/repo/workflow-name' or 'owner/repo/workflow-name@version')"`
		Number    int      `json:"number,omitempty" jsonschema:"Create multiple numbered copies (corresponds to -c flag, default: 1)"`
		Name      string   `json:"name,omitempty" jsonschema:"Specify name for the added workflow - without .md extension (corresponds to -n flag)"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "add",
		Description: "Add workflows from remote repositories to .github/workflows",
		Icons: []mcp.Icon{
			{Source: "➕"},
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args addArgs) (*mcp.CallToolResult, any, error) {
		// Check for cancellation before starting
		select {
		case <-ctx.Done():
			return nil, nil, &jsonrpc.Error{
				Code:    jsonrpc.CodeInternalError,
				Message: "request cancelled",
				Data:    mcpErrorData(ctx.Err().Error()),
			}
		default:
		}

		// Validate required arguments
		if len(args.Workflows) == 0 {
			return nil, nil, &jsonrpc.Error{
				Code:    jsonrpc.CodeInvalidParams,
				Message: "missing required parameter: at least one workflow specification is required",
				Data:    nil,
			}
		}

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
		cmd := execCmd(ctx, cmdArgs...)
		output, err := cmd.CombinedOutput()

		if err != nil {
			return nil, nil, &jsonrpc.Error{
				Code:    jsonrpc.CodeInternalError,
				Message: "failed to add workflows",
				Data:    mcpErrorData(map[string]any{"error": err.Error(), "output": string(output)}),
			}
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(output)},
			},
		}, nil, nil
	})

	// Add update tool
	type updateArgs struct {
		Workflows []string `json:"workflows,omitempty" jsonschema:"Workflow IDs to update (empty for all workflows)"`
		Major     bool     `json:"major,omitempty" jsonschema:"Allow major version updates when updating tagged releases"`
		Force     bool     `json:"force,omitempty" jsonschema:"Force update even if no changes detected"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name: "update",
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
		Icons: []mcp.Icon{
			{Source: "🔄"},
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args updateArgs) (*mcp.CallToolResult, any, error) {
		// Check for cancellation before starting
		select {
		case <-ctx.Done():
			return nil, nil, &jsonrpc.Error{
				Code:    jsonrpc.CodeInternalError,
				Message: "request cancelled",
				Data:    mcpErrorData(ctx.Err().Error()),
			}
		default:
		}

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
		cmd := execCmd(ctx, cmdArgs...)
		output, err := cmd.CombinedOutput()

		if err != nil {
			return nil, nil, &jsonrpc.Error{
				Code:    jsonrpc.CodeInternalError,
				Message: "failed to update workflows",
				Data:    mcpErrorData(map[string]any{"error": err.Error(), "output": string(output)}),
			}
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(output)},
			},
		}, nil, nil
	})

	// Add fix tool
	type fixArgs struct {
		Workflows    []string `json:"workflows,omitempty" jsonschema:"Workflow IDs to fix (empty for all workflows)"`
		Write        bool     `json:"write,omitempty" jsonschema:"Write changes to files (default is dry-run)"`
		ListCodemods bool     `json:"list_codemods,omitempty" jsonschema:"List all available codemods and exit"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name: "fix",
		Description: `Apply automatic codemod-style fixes to agentic workflow files.

This command applies a registry of codemods that automatically update deprecated fields
and migrate to new syntax. Codemods preserve formatting and comments as much as possible.

Available codemods:
• timeout-minutes-migration: Replaces 'timeout_minutes' with 'timeout-minutes'
• network-firewall-migration: Removes deprecated 'network.firewall' field
• sandbox-agent-false-removal: Removes 'sandbox.agent: false' (firewall now mandatory)
• safe-inputs-mode-removal: Removes deprecated 'safe-inputs.mode' field

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
		Icons: []mcp.Icon{
			{Source: "🔧"},
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args fixArgs) (*mcp.CallToolResult, any, error) {
		// Check for cancellation before starting
		select {
		case <-ctx.Done():
			return nil, nil, &jsonrpc.Error{
				Code:    jsonrpc.CodeInternalError,
				Message: "request cancelled",
				Data:    mcpErrorData(ctx.Err().Error()),
			}
		default:
		}

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
		cmd := execCmd(ctx, cmdArgs...)
		output, err := cmd.CombinedOutput()

		if err != nil {
			return nil, nil, &jsonrpc.Error{
				Code:    jsonrpc.CodeInternalError,
				Message: "failed to fix workflows",
				Data:    mcpErrorData(map[string]any{"error": err.Error(), "output": string(output)}),
			}
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(output)},
			},
		}, nil, nil
	})

	return server
}

// sanitizeForLog removes newline and carriage return characters from user input
// to prevent log injection attacks where malicious users could forge log entries.
func sanitizeForLog(input string) string {
	// Remove both \n and \r to prevent log injection
	sanitized := strings.ReplaceAll(input, "\n", "")
	sanitized = strings.ReplaceAll(sanitized, "\r", "")
	return sanitized
}

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func loggingHandler(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a response writer wrapper to capture status code.
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		// Sanitize user-controlled input before logging to prevent log injection
		sanitizedPath := sanitizeForLog(r.URL.Path)

		// Log request details.
		log.Printf("[REQUEST] %s | %s | %s %s",
			start.Format(time.RFC3339),
			r.RemoteAddr,
			r.Method,
			sanitizedPath)

		// Call the actual handler.
		handler.ServeHTTP(wrapped, r)

		// Log response details.
		duration := time.Since(start)
		log.Printf("[RESPONSE] %s | %s | %s %s | Status: %d | Duration: %v",
			time.Now().Format(time.RFC3339),
			r.RemoteAddr,
			r.Method,
			sanitizedPath,
			wrapped.statusCode,
			duration)
	})
}

// runHTTPServer runs the MCP server with HTTP/SSE transport
func runHTTPServer(server *mcp.Server, port int) error {
	mcpLog.Printf("Creating HTTP server on port %d", port)

	// Create the streamable HTTP handler.
	handler := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		return server
	}, &mcp.StreamableHTTPOptions{
		SessionTimeout: 2 * time.Hour, // Close idle sessions after 2 hours
		Logger:         logger.NewSlogLoggerWithHandler(mcpLog),
	})

	handlerWithLogging := loggingHandler(handler)

	// Create HTTP server
	addr := fmt.Sprintf(":%d", port)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           handlerWithLogging,
		ReadHeaderTimeout: MCPServerHTTPTimeout,
	}

	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Starting MCP server on http://localhost%s", addr)))
	mcpLog.Printf("HTTP server listening on %s", addr)

	// Run the HTTP server
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		mcpLog.Printf("HTTP server failed: %v", err)
		return fmt.Errorf("HTTP server failed: %w", err)
	}

	return nil
}
