package cli

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type healthArgs struct {
	WorkflowName string   `json:"workflow_name,omitempty" jsonschema:"Workflow name or path (empty for all workflows)"`
	Days         *int     `json:"days,omitempty" jsonschema:"Maximum evidence horizon: 7, 30, or 90 days (default 7); mutually exclusive with start_date"`
	StartDate    string   `json:"start_date,omitempty" jsonschema:"Include runs created at or after this UTC date, RFC3339 timestamp, or relative time such as -24h"`
	EndDate      string   `json:"end_date,omitempty" jsonschema:"Include runs created at or before this UTC date, RFC3339 timestamp, or relative time (default now)"`
	Threshold    *float64 `json:"threshold,omitempty" jsonschema:"Success rate warning threshold in percent (default 80)"`
	Repo         string   `json:"repo,omitempty" jsonschema:"GitHub repository owner/repo (defaults to the current repository)"`
	LogsDir      string   `json:"logs_dir,omitempty" jsonschema:"Existing logs cache for failure clustering and sub-agent failures; does not download artifacts"`
	Timeout      string   `json:"timeout,omitempty" jsonschema:"Maximum collection duration such as 30s or 5m (default 2m); 0 disables the deadline. Expiry returns explicitly partial results"`
}

func registerHealthTool(server *mcp.Server, execCmd execCmdFunc) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "health",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    true,
			IdempotentHint:  true,
			DestructiveHint: boolPtr(false),
			OpenWorldHint:   boolPtr(true),
		},
		Icons: mcpToolIcons("📊"),
		Description: `Read workflow health metrics, success rates, execution trends, and revision-aware recovery evidence.

Returns the health command's JSON summary for all workflows, or a detailed JSON report when workflow_name is supplied.
Reports use a bounded current-revision sample, not exhaustive history. Failure clusters reuse typed Jobs API observations and compatible cached evidence; sub-agent failures require current-version cached usage. Cache compatibility and job-evidence completeness are explicit; no artifacts are downloaded.
Uses the server's existing GitHub credentials and does not modify workflows or cached logs.`,
	}, func(ctx context.Context, req *mcp.CallToolRequest, args healthArgs) (*mcp.CallToolResult, any, error) {
		if err := ctx.Err(); err != nil {
			return nil, nil, newMCPError(jsonrpc.CodeInternalError, "request cancelled", err.Error())
		}
		if err := validateMCPPositionalArgs("workflow_name", args.WorkflowName); err != nil {
			return nil, nil, err
		}
		if args.Days != nil && args.StartDate != "" {
			return nil, nil, newMCPError(jsonrpc.CodeInvalidParams, "days and start_date are mutually exclusive", nil)
		}
		if args.Timeout != "" {
			timeout, err := time.ParseDuration(args.Timeout)
			if err != nil || timeout < 0 {
				return nil, nil, newMCPError(jsonrpc.CodeInvalidParams, "timeout must be a non-negative duration such as 30s or 5m", nil)
			}
		}

		mcpLog.Print("Executing health tool")
		notifyProgress(ctx, req, 0, 100, "Reading workflow health...")
		stdout, stderr, err := runMCPExecOutputWithStderr(ctx, execCmd, healthMCPCommand(args)...)
		if err != nil {
			detail := strings.TrimSpace(string(stderr))
			if detail == "" {
				detail = err.Error()
			}
			return nil, nil, newMCPError(jsonrpc.CodeInternalError, "health command failed: "+detail, nil)
		}
		if !json.Valid(stdout) {
			return nil, nil, newMCPError(jsonrpc.CodeInternalError, "health command returned invalid JSON", nil)
		}
		notifyProgress(ctx, req, 100, 100, "Workflow health read complete")
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(stdout)}}}, nil, nil
	})
}

func healthMCPCommand(args healthArgs) []string {
	cmdArgs := []string{"health", "--json"}
	if args.Days != nil {
		cmdArgs = append(cmdArgs, "--days="+strconv.Itoa(*args.Days))
	}
	if args.Threshold != nil {
		cmdArgs = append(cmdArgs, "--threshold="+strconv.FormatFloat(*args.Threshold, 'f', -1, 64))
	}
	for _, option := range []struct{ flag, value string }{
		{"start-date", args.StartDate},
		{"end-date", args.EndDate},
		{"repo", args.Repo},
		{"logs-dir", args.LogsDir},
		{"timeout", args.Timeout},
	} {
		if option.value != "" {
			cmdArgs = append(cmdArgs, "--"+option.flag+"="+option.value)
		}
	}
	if args.WorkflowName != "" {
		cmdArgs = appendMCPPositionals(cmdArgs, args.WorkflowName)
	}
	return cmdArgs
}
