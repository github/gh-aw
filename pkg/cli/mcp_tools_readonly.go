package cli

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// statusArgs holds the input parameters for the status tool.
type statusArgs struct {
	Pattern string `json:"pattern,omitempty" jsonschema:"Optional pattern to filter workflows by name"`
}

// registerStatusTool registers the status tool with the MCP server.
// The status tool is read-only and idempotent.
func registerStatusTool(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "status",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:   true,
			IdempotentHint: true,
			OpenWorldHint:  boolPtr(false),
		},
		Description: `Show status of agentic workflow files and workflows.

Returns a JSON array where each element has the following structure:
- workflow: Name of the workflow file
- agent: AI engine used (e.g., "copilot", "claude", "codex")
- compiled: Whether the workflow is compiled ("Yes", "No", or "N/A")
- status: GitHub workflow status ("active", "disabled", "Unknown")
- time_remaining: Time remaining until workflow deadline (if applicable)`,
		Icons: mcpToolIcons("📊"),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args statusArgs) (*mcp.CallToolResult, any, error) {
		// Check for cancellation before starting
		select {
		case <-ctx.Done():
			return nil, nil, newMCPError(jsonrpc.CodeInternalError, "request cancelled", ctx.Err().Error())
		default:
		}

		mcpLog.Printf("Executing status tool: pattern=%s", args.Pattern)

		// Call GetWorkflowStatuses directly instead of spawning subprocess
		statuses, err := GetWorkflowStatuses(ctx, args.Pattern, "", "", "")
		if err != nil {
			return nil, nil, newMCPError(jsonrpc.CodeInternalError, "failed to get workflow statuses", map[string]any{"error": err.Error()})
		}

		// Marshal to JSON
		jsonBytes, err := json.Marshal(statuses)
		if err != nil {
			return nil, nil, newMCPError(jsonrpc.CodeInternalError, "failed to marshal workflow statuses", map[string]any{"error": err.Error()})
		}

		outputStr := string(jsonBytes)

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: outputStr},
			},
		}, nil, nil
	})
}

// compileArgs holds the input parameters for the compile tool.
type compileArgs struct {
	Workflows      []string `json:"workflows,omitempty" jsonschema:"Workflow files to compile as an array (e.g., [\"workflow.md\"]) (empty for all)"`
	DryRun         bool     `json:"dry_run,omitempty" jsonschema:"Disable compiler-managed GitHub mutations, stage safe outputs, record dry_run in aw_info.json, and enable strict validation, shellcheck, and model checks; Docker-based scanners are opt-in; JSON includes a batch dry-run gate and scanner invocation coverage; model inventory refresh warnings fail the gate; custom scripts and external MCP effects remain unverified; explicitly disabling a required check is rejected; does not authorize execution, upload, or dispatch workflows"`
	Environment    string   `json:"environment,omitempty" jsonschema:"Override the environment on every generated job; reusable-workflow caller jobs are rejected"`
	Strict         bool     `json:"strict,omitempty" jsonschema:"Override frontmatter to enforce strict mode validation for all workflows. Note: Workflows default to strict mode unless frontmatter sets strict: false"`
	ValidateImages bool     `json:"validate_images,omitempty" jsonschema:"Require Docker for container image validation; fail instead of skipping when Docker is unavailable"`
	Zizmor         bool     `json:"zizmor,omitempty" jsonschema:"Run zizmor security scanner on generated .lock.yml files"`
	Poutine        bool     `json:"poutine,omitempty" jsonschema:"Run poutine security scanner on generated .lock.yml files"`
	Actionlint     bool     `json:"actionlint,omitempty" jsonschema:"Run actionlint linter on generated .lock.yml files"`
	RunnerGuard    bool     `json:"runner-guard,omitempty" jsonschema:"Run runner-guard taint analysis scanner on generated .lock.yml files"`
	Syft           bool     `json:"syft,omitempty" jsonschema:"Run syft SBOM scanner on container images referenced in compiled .lock.yml files"`
	Grype          bool     `json:"grype,omitempty" jsonschema:"Run grype vulnerability scanner on container images referenced in compiled .lock.yml files"`
	Grant          bool     `json:"grant,omitempty" jsonschema:"Run grant license scanner on container images referenced in compiled .lock.yml files"`
	Yamllint       bool     `json:"yamllint,omitempty" jsonschema:"Run yamllint YAML linter on generated .lock.yml files"`
	Fix            bool     `json:"fix,omitempty" jsonschema:"Apply automatic codemod fixes to workflows before compiling"`
	MaxTokens      int      `json:"max_tokens,omitempty" jsonschema:"Deprecated: accepted for backward compatibility but ignored."`
}

// registerCompileTool registers the compile tool with the MCP server.
// manifestCacheFile is the path to a temp JSON file containing pre-cached gh-aw-manifests
// collected at server startup; it is passed to each compile subprocess via
// --prior-manifest-file so the compiler uses tamper-proof manifests for safe update
// enforcement.  An empty string disables this feature.
// Returns an error if schema generation fails, which causes the server to stop registering tools.
func registerCompileTool(server *mcp.Server, execCmd execCmdFunc, manifestCacheFile string) error { //nolint:largefunc
	// Generate schema with elicitation defaults
	compileSchema, err := generateSchemaWithDefaults[compileArgs](map[string]any{
		"strict": true,
	})
	if err != nil {
		mcpLog.Printf("Failed to generate compile tool schema: %v", err)
		return err
	}

	mcp.AddTool(server, &mcp.Tool{
		Name: "compile",
		Annotations: &mcp.ToolAnnotations{
			IdempotentHint:  true,
			DestructiveHint: boolPtr(false),
			OpenWorldHint:   boolPtr(false),
		},
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
- scope: "batch" for compiler/scanner diagnostics that cannot be attributed to one workflow (omitted for workflow results); development compilation fails if any result is invalid
- compiled_file: Path to the generated .lock.yml file`,
		InputSchema: compileSchema,
		Icons:       mcpToolIcons("📋"),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args compileArgs) (*mcp.CallToolResult, any, error) { //nolint:largefunc
		// Check for cancellation before starting
		select {
		case <-ctx.Done():
			return nil, nil, newMCPError(jsonrpc.CodeInternalError, "request cancelled", ctx.Err().Error())
		default:
		}

		if args.DryRun {
			var rawArgs map[string]json.RawMessage
			if err := json.Unmarshal(req.Params.Arguments, &rawArgs); err != nil {
				return nil, nil, newMCPError(jsonrpc.CodeInvalidParams, "invalid compile arguments", err.Error())
			}
			explicitFlags := make(map[string]bool)
			for _, name := range DryRunRequiredBoolFlags() {
				if raw, supplied := rawArgs[name]; supplied {
					var enabled bool
					if err := json.Unmarshal(raw, &enabled); err != nil {
						return nil, nil, newMCPError(jsonrpc.CodeInvalidParams, "invalid boolean compile flag", name)
					}
					explicitFlags[name] = enabled
				}
			}
			if err := ValidateDevelopmentCompileFlags(args.DryRun, explicitFlags); err != nil {
				return nil, nil, newMCPError(jsonrpc.CodeInvalidParams, err.Error(), nil)
			}
		}

		// dockerUnavailableWarning is set when Docker is not accessible but the compile
		// should still proceed without the static-analysis tools.  After the compile
		// attempt, the warning is appended to workflow results in the JSON output so
		// the caller knows linting was skipped, while preserving each workflow's
		// valid/invalid status.
		var dockerUnavailableWarning string

		// Check if any static analysis tools are requested that require Docker images
		// Development mode delegates requested checks to the CLI without downgrading them.
		if !args.DryRun && (args.Zizmor || args.Poutine || args.Actionlint || args.RunnerGuard || args.Syft || args.Grype || args.Grant || args.Yamllint) {
			// Check if Docker images are available; if not, start downloading and return retry message
			if err := CheckAndPrepareDockerImages(ctx, DockerImagesOptions{
				Zizmor:      args.Zizmor,
				Poutine:     args.Poutine,
				Actionlint:  args.Actionlint,
				RunnerGuard: args.RunnerGuard,
				Syft:        args.Syft,
				Grype:       args.Grype,
				Grant:       args.Grant,
				Yamllint:    args.Yamllint,
			}); err != nil {
				var dockerUnavailableErr *DockerUnavailableError
				if errors.As(err, &dockerUnavailableErr) {
					// Docker daemon is not running.  Instead of failing every workflow,
					// compile without the Docker-based tools and surface a warning so
					// the caller knows static analysis was skipped.
					dockerUnavailableWarning = err.Error()
					if localScannerPath(ctx, "zizmor") == "" {
						args.Zizmor = false
					}
					if localScannerPath(ctx, "poutine") == "" {
						args.Poutine = false
					}
					args.Actionlint = false
					args.RunnerGuard = false
					args.Syft = false
					args.Grype = false
					args.Grant = false
					args.Yamllint = false
				} else {
					// Images are still downloading — ask the caller to retry.
					// Build per-workflow validation errors instead of throwing an MCP protocol error,
					// so callers always receive consistent JSON regardless of the failure mode.
					jsonBytes, jsonErr := marshalMCPCompileErrorResults(args.Workflows, err.Error())
					if jsonErr != nil {
						return nil, nil, newMCPError(jsonrpc.CodeInternalError, "failed to marshal docker error results", jsonErr.Error())
					}
					return &mcp.CallToolResult{
						Content: []mcp.Content{&mcp.TextContent{Text: string(jsonBytes)}},
					}, nil, nil
				}
			}

			// Check for cancellation after Docker image preparation
			select {
			case <-ctx.Done():
				return nil, nil, newMCPError(jsonrpc.CodeInternalError, "request cancelled", ctx.Err().Error())
			default:
			}
		}

		// Build command arguments
		// Always validate workflows during compilation and use JSON output for MCP.
		cmdArgs := []string{"compile", "--validate", "--json"}

		// Add fix flag if requested
		if args.Fix {
			cmdArgs = append(cmdArgs, "--fix")
		}

		// Add strict flag if requested
		if args.Strict {
			cmdArgs = append(cmdArgs, "--strict")
		}
		if args.DryRun {
			cmdArgs = append(cmdArgs, "--dry-run")
		}
		if args.Environment != "" {
			cmdArgs = append(cmdArgs, "--environment", args.Environment)
		}
		if args.ValidateImages {
			cmdArgs = append(cmdArgs, "--validate-images")
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
		if args.RunnerGuard {
			cmdArgs = append(cmdArgs, "--runner-guard")
		}
		if args.Syft {
			cmdArgs = append(cmdArgs, "--syft")
		}
		if args.Grype {
			cmdArgs = append(cmdArgs, "--grype")
		}
		if args.Grant {
			cmdArgs = append(cmdArgs, "--grant")
		}
		if args.Yamllint {
			cmdArgs = append(cmdArgs, "--yamllint")
		}

		cmdArgs = append(cmdArgs, args.Workflows...)

		// Pass the pre-cached manifest file when available so the compiler uses
		// the tamper-proof manifest baseline captured at server startup.
		if manifestCacheFile != "" {
			cmdArgs = append(cmdArgs, "--prior-manifest-file", manifestCacheFile)
		}

		mcpLog.Printf("Executing compile tool: workflows=%v, strict=%v, fix=%v, zizmor=%v, poutine=%v, actionlint=%v, runner-guard=%v, syft=%v, grype=%v, grant=%v, yamllint=%v",
			args.Workflows, args.Strict, args.Fix, args.Zizmor, args.Poutine, args.Actionlint, args.RunnerGuard, args.Syft, args.Grype, args.Grant, args.Yamllint)

		// Execute the CLI command
		// Use separate stdout/stderr capture instead of CombinedOutput because:
		// - Stdout contains JSON output (--json flag)
		// - Stderr contains console messages that shouldn't be mixed with JSON
		stdout, diagnostics, err := runMCPCompileOutput(ctx, execCmd, cmdArgs...)

		// The compile command always outputs JSON to stdout when --json flag is used, even on error.
		// We should return the JSON output to the LLM so it can see validation errors.
		// Only return an MCP error if we cannot get any output at all.
		outputStr := string(stdout)

		// If the command failed but we have output, it's likely compilation errors
		// which are included in the JSON output. Return the output, not an MCP error.
		if err != nil {
			mcpLog.Printf("Compile command exited with error: %v (output length: %d)", err, len(outputStr))
			// If we have no output, this is a real execution failure
			if strings.TrimSpace(outputStr) == "" {
				jsonBytes, jsonErr := marshalMCPCompileErrorResults(args.Workflows, diagnostics.failureMessage(err))
				if jsonErr != nil {
					return nil, nil, newMCPError(jsonrpc.CodeInternalError, "failed to marshal compile error results", jsonErr.Error())
				}
				return &mcp.CallToolResult{
					Content: []mcp.Content{&mcp.TextContent{Text: string(jsonBytes)}},
				}, nil, nil
			}
			// Otherwise, we have output (likely validation errors in JSON), so continue
			// and return it to the LLM
		}

		// When Docker was unavailable, inject a warning into every workflow result so the
		// caller knows that static analysis was skipped — but does NOT mark valid
		// workflows as invalid.
		if dockerUnavailableWarning != "" {
			outputStr = injectDockerUnavailableWarning(outputStr, dockerUnavailableWarning)
		}
		if args.DryRun {
			outputStr = injectDevelopmentShellcheckDiagnostics(outputStr, diagnostics.shellcheck.String())
		} else {
			outputStr = injectShellcheckDiagnostics(outputStr, diagnostics.shellcheck.String())
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: outputStr},
			},
		}, nil, nil
	})

	return nil
}

// mcpInspectArgs holds the input parameters for the mcp-inspect tool.
type mcpInspectArgs struct {
	WorkflowFile string `json:"workflow_file,omitempty" jsonschema:"Workflow file to inspect MCP servers from (empty to list all workflows with MCP servers)"`
	Server       string `json:"server,omitempty" jsonschema:"Filter to inspect only the specified MCP server"`
	Tool         string `json:"tool,omitempty" jsonschema:"Show detailed information about a specific tool (requires server parameter)"`
}

// registerMCPInspectTool registers the mcp-inspect tool with the MCP server.
func registerMCPInspectTool(server *mcp.Server, execCmd execCmdFunc) { //nolint:largefunc
	mcp.AddTool(server, &mcp.Tool{
		Name: "mcp-inspect",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:   true,
			IdempotentHint: true,
			OpenWorldHint:  boolPtr(true),
		},
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
		Icons: mcpToolIcons("🔬"),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args mcpInspectArgs) (*mcp.CallToolResult, any, error) {
		// Check for cancellation before starting
		select {
		case <-ctx.Done():
			return nil, nil, newMCPError(jsonrpc.CodeInternalError, "request cancelled", ctx.Err().Error())
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
		output, err := runMCPExecCombinedOutput(ctx, execCmd, cmdArgs...)

		if err != nil {
			return nil, nil, newMCPError(jsonrpc.CodeInternalError, "failed to inspect MCP servers", map[string]any{"error": err.Error(), "output": string(output)})
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(output)},
			},
		}, nil, nil
	})
}

// checksArgs holds the input parameters for the checks tool.
type checksArgs struct {
	PRNumber string `json:"pr_number" jsonschema:"Pull request number to classify CI checks for"`
	Repo     string `json:"repo,omitempty" jsonschema:"Repository in owner/repo format (defaults to current repository)"`
}

// registerChecksTool registers the checks tool with the MCP server.
// The checks tool is read-only and idempotent.
func registerChecksTool(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "checks",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:   true,
			IdempotentHint: true,
			OpenWorldHint:  boolPtr(true),
		},
		Description: `Classify CI check state for a pull request and return a normalized result.

Maps PR check rollups to one of the following normalized states:
  success        - all checks passed
  failed         - one or more checks failed
  pending        - checks are still running or queued
  no_checks      - no checks configured or triggered
  policy_blocked - policy or account gates blocked the PR

Returns JSON with two state fields:
  state          - aggregate state across all check runs and commit statuses
  required_state - state derived from check runs and policy commit statuses only;
                   ignores optional third-party commit statuses (e.g., Vercel,
                   Netlify deployments) but still surfaces policy_blocked when
                   branch-protection or account-gate statuses fail

Use required_state as the authoritative CI verdict in repos that have optional
deployment integrations posting commit statuses alongside required CI checks.

Also returns pr_number, head_sha, check_runs, statuses, and total_count.`,
		Icons: mcpToolIcons("✅"),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args checksArgs) (*mcp.CallToolResult, any, error) {
		// Check for cancellation before starting
		select {
		case <-ctx.Done():
			return nil, nil, newMCPError(jsonrpc.CodeInternalError, "request cancelled", ctx.Err().Error())
		default:
		}

		if args.PRNumber == "" {
			return nil, nil, newMCPError(jsonrpc.CodeInvalidParams, "missing required parameter: pr_number", nil)
		}

		mcpLog.Printf("Executing checks tool: pr_number=%s, repo=%s", args.PRNumber, args.Repo)

		result, err := FetchChecksResult(args.Repo, args.PRNumber)
		if err != nil {
			return nil, nil, newMCPError(jsonrpc.CodeInternalError, "failed to fetch checks", map[string]any{"error": err.Error()})
		}

		jsonBytes, err := json.Marshal(result)
		if err != nil {
			return nil, nil, newMCPError(jsonrpc.CodeInternalError, "failed to marshal checks result", map[string]any{"error": err.Error()})
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(jsonBytes)},
			},
		}, nil, nil
	})
}

// buildCompileErrorResults builds a []ValidationResult with a config_error for each target
// workflow. It is used when the compile subprocess cannot return JSON output, so the compile
// tool can still return consistent structured JSON instead of a protocol-level error.
func buildCompileErrorResults(requestedWorkflows []string, errMsg string) []ValidationResult {
	// Determine which workflow names to report
	var workflowNames []string
	if len(requestedWorkflows) > 0 {
		for _, w := range requestedWorkflows {
			// Normalize workflow identifiers so they match the standard compile output.
			// If the caller passed an ID without an extension (e.g. "test1"),
			// treat it as a markdown workflow file ("test1.md") before taking the basename.
			if filepath.Ext(w) == "" {
				w = w + ".md"
			}
			workflowNames = append(workflowNames, filepath.Base(w))
		}
	} else {
		// Discover all workflow files in the default directory.
		// An empty string means "use the default .github/workflows directory".
		if mdFiles, err := getMarkdownWorkflowFiles(""); err == nil {
			for _, f := range mdFiles {
				workflowNames = append(workflowNames, filepath.Base(f))
			}
		}
	}

	// Fallback: if we could not determine workflow names, emit a single generic entry
	if len(workflowNames) == 0 {
		return []ValidationResult{{
			Workflow: "",
			Valid:    false,
			Errors:   []ValidationIssue{{Type: "config_error", Message: errMsg}},
			Warnings: []ValidationIssue{},
		}}
	}

	results := make([]ValidationResult, 0, len(workflowNames))
	for _, name := range workflowNames {
		results = append(results, ValidationResult{
			Workflow: name,
			Valid:    false,
			Errors:   []ValidationIssue{{Type: "config_error", Message: errMsg}},
			Warnings: []ValidationIssue{},
		})
	}
	return results
}

// injectDockerUnavailableWarning parses the JSON compile output and appends a
// "docker_unavailable" warning to every workflow result.  It is used when Docker
// is not running so the caller knows static analysis was skipped, while preserving
// the compile-time valid/invalid status of each workflow.
// If the JSON cannot be parsed the original output is returned unchanged.
func injectDockerUnavailableWarning(outputStr, warningMsg string) string {
	return injectValidationWarning(outputStr, ValidationIssue{
		Type:    "docker_unavailable",
		Message: warningMsg,
	})
}

func injectDevelopmentShellcheckDiagnostics(outputStr, stderrOutput string) string {
	diagnostics := extractShellcheckDiagnostics(stderrOutput)
	if len(diagnostics) == 0 {
		return outputStr
	}
	var results []ValidationResult
	if err := json.Unmarshal([]byte(outputStr), &results); err != nil {
		return outputStr
	}
	index := -1
	for i, result := range results {
		if result.Scope == "batch" && result.Workflow == "shellcheck" {
			index = i
			break
		}
	}
	if index == -1 {
		results = append(results, ValidationResult{Scope: "batch", Workflow: "shellcheck", Valid: false, Warnings: []ValidationIssue{}})
		index = len(results) - 1
	}
	if index >= 0 && index < len(results) {
		for _, diagnostic := range diagnostics {
			results[index].Errors = append(results[index].Errors, ValidationIssue{Type: "shellcheck", Message: diagnostic})
		}
	}
	output, err := formatValidationOutput(results)
	if err != nil {
		return outputStr
	}
	return output
}

func injectShellcheckDiagnostics(outputStr, stderrOutput string) string {
	diagnostics := extractShellcheckDiagnostics(stderrOutput)
	if len(diagnostics) == 0 {
		return outputStr
	}

	warnings := make([]ValidationIssue, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		warnings = append(warnings, ValidationIssue{
			Type:    "shellcheck",
			Message: diagnostic,
		})
	}
	return injectValidationWarnings(outputStr, warnings)
}

func extractShellcheckDiagnostics(stderrOutput string) []string {
	if stderrOutput == "" || !strings.Contains(stderrOutput, "shellcheck findings in ") {
		return nil
	}

	lines := strings.Split(stderrOutput, "\n")
	diagnostics := make([]string, 0)
	var current strings.Builder

	flush := func() {
		if text := strings.TrimSpace(current.String()); text != "" {
			diagnostics = append(diagnostics, text)
		}
		current.Reset()
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.Contains(trimmed, "shellcheck findings in "):
			flush()
			current.WriteString(trimmed)
		case current.Len() > 0 && (strings.Contains(trimmed, "script:") || strings.HasPrefix(trimmed, "script ")):
			current.WriteString("\n")
			current.WriteString(trimmed)
		case current.Len() > 0 && trimmed == "":
			flush()
		}
	}
	flush()

	return diagnostics
}

func injectValidationWarnings(outputStr string, warnings []ValidationIssue) string {
	if len(warnings) == 0 {
		return outputStr
	}

	var results []ValidationResult
	if err := json.Unmarshal([]byte(outputStr), &results); err != nil {
		// Can't parse — return original output so we don't lose information.
		return outputStr
	}

	for i := range results {
		results[i].Warnings = append(results[i].Warnings, warnings...)
	}

	jsonBytes, err := json.Marshal(results)
	if err != nil {
		return outputStr
	}
	return string(jsonBytes)
}

func injectValidationWarning(outputStr string, warning ValidationIssue) string {
	return injectValidationWarnings(outputStr, []ValidationIssue{warning})
}
