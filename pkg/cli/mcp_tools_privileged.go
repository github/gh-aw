package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultMCPLogsToolCount            = 100
	defaultMCPLogsTimeoutMinutes       = 1
	mcpLogsRunsPerDefaultTimeoutMinute = 40
	mcpLogsGatewayDeadlineMargin       = 5 * time.Second
	// defaultMCPLogsMinTimeoutMinutesAllWorkflows is the minimum timeout (in minutes)
	// used when no workflow_name filter is provided.  Querying all workflow runs at once
	// requires a single GitHub API call rather than a workflow-scoped call, but for
	// large repositories the unfiltered endpoint can be significantly slower.  A higher
	// floor gives the tool enough headroom in those cases.
	defaultMCPLogsMinTimeoutMinutesAllWorkflows = 5
	// maxMCPLogsSubprocessTimeoutMinutes caps the user-supplied timeout to prevent
	// a runaway subprocess from holding a guardrail slot for an unbounded duration.
	// With up to 4 concurrent subprocess slots (maxActiveMCPChildProcesses), a
	// single long-running request could otherwise block all callers for an arbitrarily
	// long time.
	maxMCPLogsSubprocessTimeoutMinutes = 60

	// defaultMCPAuditTimeoutMinutes is the default subprocess timeout for the audit
	// tool.  Auditing a single run typically takes 5–30 s, but large runs with many
	// artifact sets can take longer.  5 minutes gives ample headroom while still
	// bounding the subprocess lifetime.
	defaultMCPAuditTimeoutMinutes = 5
	// defaultMCPAuditDiffTimeoutMinutes is the default subprocess timeout for the
	// audit-diff tool.  5 minutes gives ample headroom for the artifact-download
	// and diff steps while still bounding the subprocess lifetime.
	defaultMCPAuditDiffTimeoutMinutes = 5
)

// appendRepoFlagFromEnv appends "--repo <owner/repo>" to args when GITHUB_REPOSITORY
// is set in the environment. This allows gh CLI subcommands to identify the repository
// without falling back to git-based detection, which fails in sandboxed environments
// (e.g., MCP server containers where git is not installed).
// GITHUB_REPOSITORY is forwarded to the agentic-workflows MCP server container via
// env_vars in the MCP configuration and inherited by spawned subprocesses.
func appendRepoFlagFromEnv(args []string) []string {
	if repo := os.Getenv("GITHUB_REPOSITORY"); repo != "" { //nolint:osgetenvlibrary
		return append(args, "--repo", repo)
	}
	return args
}

// newMCPSubprocessContext creates a subprocess context that is detached from the
// MCP gateway's per-request deadline.  The gateway imposes a short RPC deadline
// (typically 60 s) on the request context; passing that context directly to
// exec.CommandContext would kill long-running subprocesses prematurely.
//
// The returned context is rooted at context.Background() (values preserved,
// gateway deadline stripped) and carries only the caller-specified timeout.
// A goroutine is started to forward explicit client cancellations
// (context.Canceled) to the subprocess while ignoring DeadlineExceeded from
// the gateway.
//
// toolName is used solely in the panic-recovery log message.
func newMCPSubprocessContext(ctx context.Context, timeout time.Duration, toolName string) (context.Context, context.CancelFunc) {
	subCtx, subCancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		timeout,
	)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				mcpLog.Printf("Panic in MCP %s context-watcher goroutine (recovered): %v", toolName, r)
			}
		}()
		// Only forward explicit cancellations (context.Canceled); do NOT propagate
		// context.DeadlineExceeded from the MCP gateway — that would kill the subprocess
		// at the gateway's 60 s RPC deadline and defeat the purpose of this fix.
		select {
		case <-ctx.Done():
			if ctx.Err() == context.Canceled {
				subCancel() // propagate client disconnect to subprocess
			}
		case <-subCtx.Done(): // subprocess timed out or caller already cancelled
		}
	}()
	return subCtx, subCancel
}

// logsArgs holds the input parameters for the logs tool.
type logsArgs struct {
	WorkflowName          string   `json:"workflow_name,omitempty" jsonschema:"Name of the workflow to download logs for (empty for all)"`
	Count                 int      `json:"count,omitempty" jsonschema:"Number of workflow runs to download (default: 100)"`
	StartDate             string   `json:"start_date,omitempty" jsonschema:"Filter runs created after this date (YYYY-MM-DD or delta like -1d, -1w, -1mo)"`
	EndDate               string   `json:"end_date,omitempty" jsonschema:"Filter runs created before this date (YYYY-MM-DD or delta like -1d, -1w, -1mo)"`
	Engine                string   `json:"engine,omitempty" jsonschema:"Filter logs by agentic engine type (claude, codex, copilot)"`
	Runtime               string   `json:"runtime,omitempty" jsonschema:"Filter logs by sandbox agent runtime (gvisor, docker-sbx)"`
	Firewall              bool     `json:"firewall,omitempty" jsonschema:"Filter to only runs with firewall enabled"`
	NoFirewall            bool     `json:"no_firewall,omitempty" jsonschema:"Filter to only runs without firewall enabled"`
	FilteredIntegrity     bool     `json:"filtered_integrity,omitempty" jsonschema:"Filter to only runs that contain DIFC integrity-filtered events in gateway logs"`
	Graders               bool     `json:"graders,omitempty" jsonschema:"Filter to only runs with deterministic grader results"`
	Branch                string   `json:"branch,omitempty" jsonschema:"Filter runs by branch name"`
	AfterRunID            int64    `json:"after_run_id,omitempty" jsonschema:"Filter runs with database ID after this value (exclusive)"`
	BeforeRunID           int64    `json:"before_run_id,omitempty" jsonschema:"Filter runs with database ID before this value (exclusive)"`
	Timeout               int      `json:"timeout,omitempty" jsonschema:"Maximum time in minutes to spend downloading logs (default: auto-scales with count in the MCP server, rounded up in 40-run increments; e.g. 1 minute up to 40, 2 minutes for 41-80, 3 minutes for 81-120, and so on)"`
	MaxGitHubAPIRateLimit int      `json:"max_github_api_rate_limit,omitempty" jsonschema:"Maximum used GitHub core API requests before waiting for reset. Positive values are absolute; negative values reserve requests from the API-reported limit (for example, 12000 or -2000)."`
	MaxStorageMB          int      `json:"max_storage,omitempty" jsonschema:"Maximum logs storage in MB after pruning non-essential cache data (0 means unlimited)."`
	PruneOlderRuns        bool     `json:"prune_older_runs,omitempty" jsonschema:"Remove oldest completed runs when non-essential cache pruning cannot satisfy max_storage."`
	MaxTokens             int      `json:"max_tokens,omitempty" jsonschema:"Deprecated: accepted for backward compatibility but ignored. Output is always written to a file."`
	Artifacts             []string `json:"artifacts,omitempty" jsonschema:"Artifact sets to download (default: usage). Valid sets: all, activation, agent, detection, evals, experiment, firewall, github-api, graders, mcp, usage"`
}

func defaultMCPLogsToolTimeoutMinutesForCount(count int) int {
	count = effectiveMCPLogsToolCount(count)

	// Round up in 40-run increments so requests slightly above the current
	// 60-second threshold automatically get an extra minute of budget.
	return max(defaultMCPLogsTimeoutMinutes, (count+mcpLogsRunsPerDefaultTimeoutMinute-1)/mcpLogsRunsPerDefaultTimeoutMinute)
}

func effectiveMCPLogsToolCount(count int) int {
	if count > 0 {
		return count
	}
	return defaultMCPLogsToolCount
}

func effectiveMCPLogsToolTimeoutMinutes(requestedTimeout, count int, workflowName, engine string) int {
	if requestedTimeout > 0 {
		return min(requestedTimeout, maxMCPLogsSubprocessTimeoutMinutes)
	}

	base := defaultMCPLogsToolTimeoutMinutesForCount(count)
	if workflowName == "" || engine != "" {
		// Without a workflow filter, or when filtering by engine, the CLI scans runs
		// across workflows and reads their artifacts. Apply a higher minimum so the
		// tool is less likely to exhaust the MCP gateway's per-tool timeout.
		return max(defaultMCPLogsMinTimeoutMinutesAllWorkflows, base)
	}
	return base
}

func effectiveMCPLogsToolSoftTimeoutSeconds(ctx context.Context, timeoutMinutes int) (int, bool) {
	deadline, ok := ctx.Deadline()
	if !ok || timeoutMinutes <= 0 {
		return 0, false
	}
	softTimeout := time.Until(deadline) - mcpLogsGatewayDeadlineMargin
	if softTimeout <= 0 {
		return 0, false
	}
	if softTimeout >= time.Duration(timeoutMinutes)*time.Minute {
		return 0, false
	}
	return max(1, int(softTimeout.Seconds())), true
}

// The logs tool requires write+ access and checks actor permissions.
// Returns an error if schema generation fails.
func registerLogsTool(server *mcp.Server, execCmd execCmdFunc, actor string, validateActor bool) error {
	// Generate schema with elicitation defaults
	logsSchema, err := generateSchemaWithDefaults[logsArgs](map[string]any{
		"count":      defaultMCPLogsToolCount,
		"max_tokens": 12000,
		"artifacts":  []string{"usage"},
	})
	if err != nil {
		mcpLog.Printf("Failed to generate logs tool schema: %v", err)
		return err
	}
	// No schema default for timeout: the runtime auto-computes it from the effective
	// count and workflow_name so that no-workflow queries (which scan across all runs)
	// receive a higher floor than single-workflow queries.  Setting a static default
	// here would cause the go-sdk to fill it in before the handler sees the arguments,
	// bypassing the per-request computation.

	mcp.AddTool(server, &mcp.Tool{
		Name: "logs",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:   true,
			IdempotentHint: true,
			OpenWorldHint:  boolPtr(true),
		},
		Description: logsToolDescription,
		InputSchema: logsSchema,
		Icons:       mcpToolIcons("📝"),
	}, newLogsToolHandler(execCmd, actor, validateActor))

	return nil
}

const logsToolDescription = `Download and analyze workflow logs.

In the normal case, returns a file path to a JSON file with workflow run data and metrics.
The data is written to a file to avoid large inline payloads. Use the returned file_path
to read the full data. In rare error cases (e.g., invalid workflow name), a JSON error
response is returned inline instead.

If the command times out before fetching all available logs, a "continuation" field will be present
in the JSON data with updated parameters to continue fetching more data.
Check for the presence of the continuation field to determine if there are more logs available.

When results are incomplete, the tool response also sets "partial": true and repeats the
"continuation" cursor inline, so partial results can be detected without reading the file.

The continuation field includes all necessary parameters (before_run_id, etc.) to resume fetching
from where the previous request stopped due to timeout.`

// newLogsToolHandler builds the handler for the logs tool.
func newLogsToolHandler(execCmd execCmdFunc, actor string, validateActor bool) func(context.Context, *mcp.CallToolRequest, logsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args logsArgs) (*mcp.CallToolResult, any, error) {
		// Check actor permissions first
		if err := checkActorPermission(ctx, actor, validateActor, "logs"); err != nil {
			return nil, nil, err
		}

		// Check for cancellation before starting
		select {
		case <-ctx.Done():
			return nil, nil, newMCPError(jsonrpc.CodeInternalError, "request cancelled", ctx.Err().Error())
		default:
		}

		// Validate firewall parameters
		if args.Firewall && args.NoFirewall {
			return nil, nil, newMCPError(jsonrpc.CodeInvalidParams, "conflicting parameters: cannot specify both 'firewall' and 'no_firewall'", nil)
		}

		// Validate workflow name before executing command
		if err := validateMCPWorkflowName(args.WorkflowName); err != nil {
			mcpLog.Printf("Workflow name validation failed, returning empty result: %v", err)
			return buildLogsEmptyResult(err)
		}

		cmdArgs, effectiveCount, timeoutValue := buildLogsCommandArgs(ctx, args)

		// Log the command being executed for debugging
		mcpLog.Printf("Executing logs tool: workflow=%s, requested_count=%d, effective_count=%d, firewall=%v, no_firewall=%v, filtered_integrity=%v, graders=%v, timeout=%d, command_args=%v",
			args.WorkflowName, args.Count, effectiveCount, args.Firewall, args.NoFirewall, args.FilteredIntegrity, args.Graders, timeoutValue, cmdArgs)

		notifyProgress(ctx, req, 0, 100, "Downloading workflow logs...")

		// Detach from the gateway's per-tool RPC deadline; see newMCPSubprocessContext.
		subCtx, subCancel := newMCPSubprocessContext(ctx, time.Duration(timeoutValue)*time.Minute, "logs")
		defer subCancel()

		// Execute the CLI command
		// Use separate stdout/stderr capture instead of CombinedOutput because:
		// - Stdout contains JSON output (--json flag)
		// - Stderr contains console messages and error details
		stdout, err := runMCPExecOutput(subCtx, execCmd, cmdArgs...)

		// The logs command outputs JSON to stdout when --json flag is used.
		// If the command fails, we need to provide detailed error information.
		outputStr := string(stdout)
		if err != nil {
			return nil, nil, buildLogsCommandError(err, outputStr, cmdArgs, timeoutValue, args.WorkflowName)
		}

		// Always write output to a file and return schema + file path
		finalOutput := buildLogsFileResponse(outputStr)
		notifyProgress(ctx, req, 100, 100, "Workflow logs downloaded")

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: finalOutput},
			},
		}, nil, nil
	}
}

// buildLogsEmptyResult returns an empty structured result instead of an MCP
// protocol error so callers can always expect consistent JSON from this tool.
// Explicit empty slices are used so JSON marshaling produces "runs":[], etc.,
// rather than null (nil slices), and TotalDuration matches the normal
// zero-duration formatting.
func buildLogsEmptyResult(cause error) (*mcp.CallToolResult, any, error) {
	emptyData := LogsData{
		Runs:     []RunData{},
		Episodes: []EpisodeData{},
		Edges:    []EpisodeEdge{},
		Message:  cause.Error(),
	}
	emptyData.Summary.TotalDuration = "0ns"
	jsonBytes, jsonErr := json.Marshal(emptyData)
	if jsonErr != nil {
		return nil, nil, newMCPError(jsonrpc.CodeInvalidParams, cause.Error(), nil)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(jsonBytes)}},
	}, nil, nil
}

// buildLogsCommandArgs builds the `gh aw logs` command arguments for the logs
// tool, returning the arguments along with the effective run count and the
// effective timeout (in minutes) applied to the subprocess.
func buildLogsCommandArgs(ctx context.Context, args logsArgs) ([]string, int, int) {
	// Force output directory to /tmp/gh-aw/aw-mcp/logs for MCP server
	cmdArgs := []string{"logs", "-o", constants.TmpAwMcpLogsDir}
	if args.WorkflowName != "" {
		cmdArgs = append(cmdArgs, args.WorkflowName)
	}
	effectiveCount := effectiveMCPLogsToolCount(args.Count)
	cmdArgs = append(cmdArgs, "-c", strconv.Itoa(effectiveCount))
	cmdArgs = appendLogsFilterArgs(cmdArgs, args)
	cmdArgs = appendRepoFlagFromEnv(cmdArgs)

	// Scale the implicit MCP timeout with the requested fetch window so
	// larger fleet-wide requests do not hit the default per-tool timeout.
	timeoutValue := effectiveMCPLogsToolTimeoutMinutes(args.Timeout, effectiveCount, args.WorkflowName, args.Engine)
	cmdArgs = append(cmdArgs, "--timeout", strconv.Itoa(timeoutValue))
	if softTimeoutSeconds, ok := effectiveMCPLogsToolSoftTimeoutSeconds(ctx, timeoutValue); ok {
		cmdArgs = append(cmdArgs, "--timeout-seconds", strconv.Itoa(softTimeoutSeconds))
	}

	// Always use --json mode in MCP server
	cmdArgs = append(cmdArgs, "--json")

	return cmdArgs, effectiveCount, timeoutValue
}

// appendLogsFilterArgs appends the optional filter/pagination flags of the logs
// tool to cmdArgs.
func appendLogsFilterArgs(cmdArgs []string, args logsArgs) []string {
	if args.StartDate != "" {
		cmdArgs = append(cmdArgs, "--start-date", args.StartDate)
	}
	if args.EndDate != "" {
		cmdArgs = append(cmdArgs, "--end-date", args.EndDate)
	}
	if args.Engine != "" {
		cmdArgs = append(cmdArgs, "--engine", args.Engine)
	}
	if args.Runtime != "" {
		cmdArgs = append(cmdArgs, "--runtime", args.Runtime)
	}
	if args.Firewall {
		cmdArgs = append(cmdArgs, "--firewall")
	}
	if args.NoFirewall {
		cmdArgs = append(cmdArgs, "--no-firewall")
	}
	if args.FilteredIntegrity {
		cmdArgs = append(cmdArgs, "--filtered-integrity")
	}
	if args.Graders {
		cmdArgs = append(cmdArgs, "--graders")
	}
	if args.Branch != "" {
		// The MCP parameter is named "branch" for backwards compatibility,
		// but the logs CLI flag is --ref (which accepts branches and tags).
		cmdArgs = append(cmdArgs, "--ref", args.Branch)
	}
	if args.AfterRunID > 0 {
		cmdArgs = append(cmdArgs, "--after-run-id", strconv.FormatInt(args.AfterRunID, 10))
	}
	if args.BeforeRunID > 0 {
		cmdArgs = append(cmdArgs, "--before-run-id", strconv.FormatInt(args.BeforeRunID, 10))
	}
	if args.MaxGitHubAPIRateLimit != 0 {
		cmdArgs = append(cmdArgs, "--max-github-api-rate-limit", strconv.Itoa(args.MaxGitHubAPIRateLimit))
	}
	if args.MaxStorageMB != 0 {
		cmdArgs = append(cmdArgs, "--max-storage", strconv.Itoa(args.MaxStorageMB))
	}
	if args.PruneOlderRuns {
		cmdArgs = append(cmdArgs, "--prune-older-runs")
	}
	if len(args.Artifacts) > 0 {
		cmdArgs = append(cmdArgs, "--artifacts", strings.Join(args.Artifacts, ","))
	}
	return cmdArgs
}

// buildLogsCommandError converts a failed logs subprocess execution into an MCP
// error with detailed diagnostic data.
func buildLogsCommandError(err error, outputStr string, cmdArgs []string, timeoutValue int, workflowName string) error {
	// Try to get stderr and exit code for detailed error reporting
	var stderr string
	var exitCode int
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
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
		"workflow":  workflowName,
	}

	// Extract the user-facing message from stderr, filtering out debug log lines
	// (e.g. "workflow:script_registry Creating new script registry +151ns")
	// to avoid leaking internal diagnostic output in the MCP error response.
	mainMsg := extractLastConsoleMessage(stderr)
	if mainMsg == "" {
		mainMsg = err.Error()
	}

	return newMCPError(jsonrpc.CodeInternalError, "failed to download workflow logs: "+mainMsg, errorData)
}

// auditArgs holds the input parameters for the audit tool.
type auditArgs struct {
	RunID        any      `json:"run_id,omitempty"          jsonschema:"Alias for run_id_or_url. Accepts run ID or run/job URL (including step anchors). String or number."`
	RunIDOrURL   any      `json:"run_id_or_url,omitempty"   jsonschema:"Deprecated: use run_ids_or_urls instead. Accepts run ID or run/job URL (including step anchors). String or number."`
	RunIDsOrURLs []string `json:"run_ids_or_urls,omitempty" jsonschema:"One or more workflow run IDs or URLs. Single item: detailed audit report. Multiple items: diff mode with first as base (see tool description for accepted formats)."`
	Artifacts    []string `json:"artifacts,omitempty"        jsonschema:"Artifact sets to download (default: all). Valid sets: all, activation, agent, detection, experiment, firewall, github-api, mcp, usage"`
	MaxTokens    int      `json:"max_tokens,omitempty"       jsonschema:"Deprecated: accepted for backward compatibility but ignored."`
	Experiment   string   `json:"experiment,omitempty"       jsonschema:"Filter to runs that include this experiment name. When set, runs whose experiment artifact does not contain an assignment for this experiment name are skipped."`
	Variant      string   `json:"variant,omitempty"          jsonschema:"Filter to runs assigned this specific variant value. Requires experiment to be set."`
	Runtime      string   `json:"runtime,omitempty"          jsonschema:"Filter to runs using a specific sandbox agent runtime (e.g., gvisor, docker-sbx). Runs without a matching runtime are skipped."`
}

// normalizeAuditRunInput converts a single-run audit input (run_id or
// run_id_or_url) from supported MCP argument types into the CLI positional
// argument format. The bool return indicates whether a non-empty value was
// provided.
func normalizeAuditRunInput(input any, fieldName string) (string, bool, error) {
	switch v := input.(type) {
	case nil:
		return "", false, nil
	case string:
		if strings.TrimSpace(v) == "" {
			return "", false, nil
		}
		return v, true, nil
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true, nil
	case int:
		return strconv.Itoa(v), true, nil
	case int64:
		return strconv.FormatInt(v, 10), true, nil
	default:
		return "", false, fmt.Errorf("%s must be a string or number", fieldName)
	}
}

// registerAuditTool registers the audit tool with the MCP server.
// The audit tool requires write+ access and checks actor permissions.
// Returns an error if schema generation fails.
func registerAuditTool(server *mcp.Server, execCmd execCmdFunc, actor string, validateActor bool) error {
	// Generate schema for audit tool
	auditSchema, err := generateSchemaWithDefaults[auditArgs](nil)
	if err != nil {
		mcpLog.Printf("Failed to generate audit tool schema: %v", err)
		return err
	}

	mcp.AddTool(server, &mcp.Tool{
		Name: "audit",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:   true,
			IdempotentHint: true,
			OpenWorldHint:  boolPtr(true),
		},
		Description: auditToolDescription,
		InputSchema: auditSchema,
		Icons:       mcpToolIcons("🔍"),
	}, newAuditToolHandler(execCmd, actor, validateActor))

	return nil
}

const auditToolDescription = `Investigate one or more workflow runs and generate a concise report.

When a single run is provided, generates a detailed audit report.
When two or more runs are provided, the first is the base (reference) run and
the remaining runs are compared against it (diff mode), showing changes in
firewall domains, MCP tool usage, and run metrics.

Each run accepts:
- Numeric run ID: 1234567890
- Run URL: https://github.com/owner/repo/actions/runs/1234567890
- Job URL: https://github.com/owner/repo/actions/runs/1234567890/job/9876543210
- Job URL with step: https://github.com/owner/repo/actions/runs/1234567890/job/9876543210#step:7:1

When a job URL is provided (single-run mode only):
- If a step number is included (#step:7:1), extracts that specific step's output
- If no step number, finds and extracts the first failing step's output
- Saves job logs and step-specific logs to the output directory

Use experiment/variant to filter runs by A/B experiment assignment (skips runs
that do not match). variant requires experiment.

Use runtime to filter runs by sandbox agent runtime (gvisor, docker-sbx); runs
without a matching runtime are skipped.

Single-run returns JSON with:
- overview: Basic run information (run_id, workflow_name, status, conclusion, created_at, started_at, updated_at, duration, event, branch, url, logs_path, experiment)
- metrics: Execution metrics (token_usage, estimated_cost, turns, error_count, warning_count)
- jobs: List of job details (name, status, conclusion, duration)
- downloaded_files: List of artifact files (path, size, size_formatted, description, is_directory)
- missing_tools: Tools that were requested but not available (tool, reason, alternatives, timestamp, workflow_name, run_id, experiment_name, variant)
- mcp_failures: MCP server failures (server_name, status, timestamp, workflow_name, run_id, experiment_name, variant)
- noop_reports: Noop signals from agents (message, timestamp, workflow_name, run_id, experiment_name, variant)
- missing_data: Missing data reports (data_type, reason, context, alternatives, timestamp, workflow_name, run_id, experiment_name, variant)
- errors: Error details (file, line, type, message)
- warnings: Warning details (file, line, type, message)
- tool_usage: Tool usage statistics (name, call_count, max_output_size, max_duration)
- firewall_analysis: Network firewall analysis if available (total_requests, allowed_requests, blocked_requests, allowed_domains, blocked_domains)
- experiments: A/B experiment assignments if present (assignments map, cumulative_counts map)
- graders: Deterministic grader results if present (results with id, name, status, value, unit, passed, direction, threshold, plus aggregate counts: total, passed, failed, error_count, unavailable_count)

Multi-run diff returns JSON describing changes between the base and each comparison run.`

// newAuditToolHandler builds the handler for the audit tool.
func newAuditToolHandler(execCmd execCmdFunc, actor string, validateActor bool) func(context.Context, *mcp.CallToolRequest, auditArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args auditArgs) (*mcp.CallToolResult, any, error) {
		// Check actor permissions first
		if err := checkActorPermission(ctx, actor, validateActor, "audit"); err != nil {
			return nil, nil, err
		}

		// Check for cancellation before starting
		select {
		case <-ctx.Done():
			return nil, nil, newMCPError(jsonrpc.CodeInternalError, "request cancelled", ctx.Err().Error())
		default:
		}

		if args.Variant != "" && args.Experiment == "" {
			return nil, nil, newMCPError(jsonrpc.CodeInvalidParams, "--variant requires --experiment", nil)
		}

		runItems, err := resolveAuditRunItems(args)
		if err != nil {
			return nil, nil, err
		}

		cmdArgs := buildAuditCommandArgs(args, runItems)

		notifyProgress(ctx, req, 0, 100, "Downloading audit artifacts...")

		// Detach from the gateway's per-tool RPC deadline; see newMCPSubprocessContext.
		subCtx, subCancel := newMCPSubprocessContext(ctx, time.Duration(defaultMCPAuditTimeoutMinutes)*time.Minute, "audit")
		defer subCancel()

		// Execute the CLI command.
		// Use separate stdout/stderr capture instead of CombinedOutput because:
		// - Stdout contains JSON output (--json flag)
		// - Stderr contains console messages and debug logs that shouldn't be mixed with JSON
		stdout, execErr := runMCPExecOutput(subCtx, execCmd, cmdArgs...)

		// The audit command outputs JSON to stdout when --json flag is used.
		// If the command fails, we need to provide detailed error information.
		outputStr := string(stdout)

		if execErr != nil {
			return buildAuditErrorResult(execErr, outputStr, runItems)
		}

		notifyProgress(ctx, req, 100, 100, "Audit complete")

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: outputStr},
			},
		}, nil, nil
	}
}

// resolveAuditRunItems resolves the list of run IDs/URLs to pass to the audit
// command. run_ids_or_urls takes precedence; it falls back to run_id, then to
// the deprecated run_id_or_url.
func resolveAuditRunItems(args auditArgs) ([]string, error) {
	runItems := args.RunIDsOrURLs
	if len(runItems) == 0 {
		runID, hasRunID, err := normalizeAuditRunInput(args.RunID, "run_id")
		if err != nil {
			return nil, newMCPError(jsonrpc.CodeInvalidParams, err.Error(), nil)
		}
		if hasRunID {
			runItems = []string{runID}
		}
	}
	if len(runItems) == 0 {
		runIDOrURL, hasRunIDOrURL, err := normalizeAuditRunInput(args.RunIDOrURL, "run_id_or_url")
		if err != nil {
			return nil, newMCPError(jsonrpc.CodeInvalidParams, err.Error(), nil)
		}
		if hasRunIDOrURL {
			runItems = []string{runIDOrURL}
		}
	}
	if len(runItems) == 0 {
		return nil, newMCPError(jsonrpc.CodeInvalidParams, "at least one run ID or URL must be provided via run_ids_or_urls, run_id, or run_id_or_url", nil)
	}
	return runItems, nil
}

// buildAuditCommandArgs builds the `gh aw audit` command arguments.
// Output is forced to /tmp/gh-aw/aw-mcp/logs for the MCP server (same as logs)
// and --json is used for structured MCP consumption. All run IDs/URLs are passed
// directly - the audit command handles single vs. diff mode.
func buildAuditCommandArgs(args auditArgs, runItems []string) []string {
	cmdArgs := []string{"audit"}
	cmdArgs = append(cmdArgs, runItems...)
	cmdArgs = append(cmdArgs, "-o", constants.TmpAwMcpLogsDir, "--json")
	if len(args.Artifacts) > 0 {
		cmdArgs = append(cmdArgs, "--artifacts", strings.Join(args.Artifacts, ","))
	}
	if args.Experiment != "" {
		cmdArgs = append(cmdArgs, "--experiment", args.Experiment)
	}
	if args.Variant != "" {
		cmdArgs = append(cmdArgs, "--variant", args.Variant)
	}
	if args.Runtime != "" {
		cmdArgs = append(cmdArgs, "--runtime", args.Runtime)
	}
	return appendRepoFlagFromEnv(cmdArgs)
}

// buildAuditErrorResult converts a failed audit subprocess execution into a JSON
// error envelope instead of an MCP protocol error so callers always receive
// consistent JSON and the run IDs are always present. IsError must be false so
// that callers (e.g. mcp_cli_bridge) treat this as a graceful not-found /
// failure response rather than a fatal protocol error.
func buildAuditErrorResult(err error, outputStr string, runItems []string) (*mcp.CallToolResult, any, error) {
	// Try to get stderr for message extraction
	var stderr string
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		stderr = string(exitErr.Stderr)
	}

	mcpLog.Printf("Audit command exited with error: %v (stdout length: %d, stderr length: %d)",
		err, len(outputStr), len(stderr))

	// Extract the user-facing message from stderr, filtering out debug log lines
	// (e.g. "workflow:script_registry Creating new script registry +151ns")
	// to avoid leaking internal diagnostic output in the MCP error response.
	mainMsg := extractLastConsoleMessage(stderr)
	if mainMsg == "" {
		mainMsg = err.Error()
	}

	errorMsg := "failed to audit workflow run: " + mainMsg
	if len(runItems) > 1 {
		errorMsg = "failed to audit workflow runs: " + mainMsg
	}
	errorEnvelope := map[string]any{
		"error":           errorMsg,
		"run_ids_or_urls": runItems,
		"suggestions": []string{
			"Verify the run ID is correct",
			"Use the 'logs' tool to list recent run IDs",
		},
	}
	jsonBytes, jsonErr := json.Marshal(errorEnvelope)
	if jsonErr != nil {
		return nil, nil, newMCPError(jsonrpc.CodeInternalError, errorMsg, nil)
	}
	return &mcp.CallToolResult{
		IsError: false,
		Content: []mcp.Content{&mcp.TextContent{Text: string(jsonBytes)}},
	}, nil, nil
}

// auditDiffArgs holds the input parameters for the audit-diff tool.
type auditDiffArgs struct {
	BaseRunID     string   `json:"base_run_id"     jsonschema:"Numeric ID of the base (reference) workflow run"`
	CompareRunIDs []string `json:"compare_run_ids" jsonschema:"One or more numeric IDs of the comparison runs"`
	Artifacts     []string `json:"artifacts,omitempty" jsonschema:"Artifact sets to download (default: all). Valid sets: all, activation, agent, detection, experiment, firewall, github-api, mcp, usage"`
}

// registerAuditDiffTool registers the audit-diff tool with the MCP server.
// It exposes the `gh aw audit diff` subcommand for comparing two workflow runs.
func registerAuditDiffTool(server *mcp.Server, execCmd execCmdFunc, actor string, validateActor bool) error {
	schema, err := generateSchemaWithDefaults[auditDiffArgs](nil)
	if err != nil {
		mcpLog.Printf("Failed to generate audit-diff tool schema: %v", err)
		return err
	}

	mcp.AddTool(server, &mcp.Tool{
		Name: "audit-diff",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:   true,
			IdempotentHint: true,
			OpenWorldHint:  boolPtr(true),
		},
		Description: `Compare behavior between a base workflow run and one or more comparison runs.

Downloads artifacts for all referenced runs (using locally cached data when available),
then produces a diff showing:
- New or removed domains in firewall logs
- Domain allow/deny status changes
- Anomaly flags (new denied domains, previously-denied now allowed)
- MCP tool invocation changes (new/removed tools, call/error count diffs)
- Run metrics comparison (token usage, duration, turns)

Returns JSON describing the differences between the base run and each comparison run.`,
		InputSchema: schema,
		Icons:       mcpToolIcons("🔎"),
	}, newAuditDiffToolHandler(execCmd, actor, validateActor))

	return nil
}

// newAuditDiffToolHandler builds the handler for the audit-diff tool.
func newAuditDiffToolHandler(execCmd execCmdFunc, actor string, validateActor bool) func(context.Context, *mcp.CallToolRequest, auditDiffArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args auditDiffArgs) (*mcp.CallToolResult, any, error) {
		if err := checkActorPermission(ctx, actor, validateActor, "audit-diff"); err != nil {
			return nil, nil, err
		}

		select {
		case <-ctx.Done():
			return nil, nil, newMCPError(jsonrpc.CodeInternalError, "request cancelled", ctx.Err().Error())
		default:
		}

		if args.BaseRunID == "" {
			return nil, nil, newMCPError(jsonrpc.CodeInvalidParams, "base_run_id is required", nil)
		}
		if len(args.CompareRunIDs) == 0 {
			return nil, nil, newMCPError(jsonrpc.CodeInvalidParams, "compare_run_ids must contain at least one run ID", nil)
		}

		// Build: gh aw audit diff <base> <compare...> -o ... --json [--artifacts ...]
		cmdArgs := []string{"audit", "diff", args.BaseRunID}
		cmdArgs = append(cmdArgs, args.CompareRunIDs...)
		cmdArgs = append(cmdArgs, "-o", constants.TmpAwMcpLogsDir, "--json")
		if len(args.Artifacts) > 0 {
			cmdArgs = append(cmdArgs, "--artifacts", strings.Join(args.Artifacts, ","))
		}

		cmdArgs = appendRepoFlagFromEnv(cmdArgs)

		notifyProgress(ctx, req, 0, 100, "Downloading artifacts for diff...")

		// Detach from the gateway's per-tool RPC deadline; see newMCPSubprocessContext.
		subCtx, subCancel := newMCPSubprocessContext(ctx, time.Duration(defaultMCPAuditDiffTimeoutMinutes)*time.Minute, "audit-diff")
		defer subCancel()

		stdout, err := runMCPExecOutput(subCtx, execCmd, cmdArgs...)
		outputStr := string(stdout)

		if err != nil {
			return buildAuditDiffErrorResult(err, outputStr, args)
		}

		notifyProgress(ctx, req, 100, 100, "Diff complete")

		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: outputStr}},
		}, nil, nil
	}
}

// buildAuditDiffErrorResult converts a failed audit-diff subprocess execution
// into a JSON error envelope (IsError false) so callers always receive
// consistent JSON rather than a fatal protocol error.
func buildAuditDiffErrorResult(err error, outputStr string, args auditDiffArgs) (*mcp.CallToolResult, any, error) {
	var stderr string
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		stderr = string(exitErr.Stderr)
	}
	mcpLog.Printf("Audit-diff command failed: %v (stdout: %d bytes, stderr: %d bytes)", err, len(outputStr), len(stderr))
	mainMsg := extractLastConsoleMessage(stderr)
	if mainMsg == "" {
		mainMsg = err.Error()
	}
	errorEnvelope := map[string]any{
		"error":        "failed to diff workflow runs: " + mainMsg,
		"base_run_id":  args.BaseRunID,
		"compare_runs": args.CompareRunIDs,
		"suggestions": []string{
			"Verify the run IDs are correct",
			"Use the 'logs' tool to list recent run IDs",
		},
	}
	jsonBytes, jsonErr := json.Marshal(errorEnvelope)
	if jsonErr != nil {
		return nil, nil, newMCPError(jsonrpc.CodeInternalError, "failed to diff workflow runs: "+mainMsg, nil)
	}
	return &mcp.CallToolResult{
		IsError: false,
		Content: []mcp.Content{&mcp.TextContent{Text: string(jsonBytes)}},
	}, nil, nil
}

// notifyProgress sends a progress notification to the MCP client if the request
// includes a progress token. The req, req.Params, and req.Session fields are
// checked for nil before use. Errors are silently ignored because progress
// notifications are best-effort; the tool result is not affected. If the client
// has disconnected or the notification fails for any reason, the tool continues
// executing normally.
func notifyProgress(ctx context.Context, req *mcp.CallToolRequest, progress, total float64, message string) {
	if req == nil || req.Session == nil {
		return
	}
	if token := req.Params.GetProgressToken(); token != nil {
		_ = req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
			ProgressToken: token,
			Progress:      progress,
			Total:         total,
			Message:       message,
		})
	}
}

// filtering out debug log lines (e.g. "workflow:script_registry Creating... +151ns").
// Console messages are identified by their prefix symbols (✗, ✓, ℹ, ⚠, etc.).
// Falls back to the last non-empty line if no console message is found.
func extractLastConsoleMessage(stderr string) string {
	// Console message prefixes used by the console package
	consoleSymbols := []string{"✗ ", "✓ ", "ℹ ", "⚠ ", "⚡ ", "🔨 ", "❓ ", "🔍 "}

	var lastConsole string
	var lastLine string

	for line := range strings.SplitSeq(stderr, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lastLine = trimmed
		for _, sym := range consoleSymbols {
			if strings.HasPrefix(trimmed, sym) {
				lastConsole = trimmed
				break
			}
		}
	}

	if lastConsole != "" {
		return lastConsole
	}
	return lastLine
}
