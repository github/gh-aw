package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type healthArgs struct {
	WorkflowName string   `json:"workflow_name,omitempty" jsonschema:"Workflow name or path (omit for the default-branch fleet report)"`
	Days         *int     `json:"days,omitempty" jsonschema:"Maximum evidence horizon: 7, 30, or 90 days (default 7); mutually exclusive with start_date"`
	StartDate    string   `json:"start_date,omitempty" jsonschema:"Include runs created at or after this UTC date, RFC3339 timestamp, or relative time such as -24h"`
	EndDate      string   `json:"end_date,omitempty" jsonschema:"Include runs created at or before this UTC date, RFC3339 timestamp, or relative time (default now)"`
	Threshold    *float64 `json:"threshold,omitempty" jsonschema:"Success rate warning threshold in percent (default 80)"`
	Repo         string   `json:"repo,omitempty" jsonschema:"GitHub repository owner/repo (defaults to the current repository)"`
	LogsDir      string   `json:"logs_dir,omitempty" jsonschema:"Existing logs cache for failure clustering and sub-agent failures; does not download artifacts"`
	Timeout      string   `json:"timeout,omitempty" jsonschema:"Maximum collection duration such as 30s or 5m (default 2m); 0 disables the deadline. Expiry returns explicitly partial results"`
	Full         bool     `json:"full,omitempty" jsonschema:"Include full per-workflow evidence; omit workflow_name"`
	Resume       string   `json:"resume,omitempty" jsonschema:"Opaque checkpoint from this server's fleet response; omit date flags on resume"`
}

func registerHealthTool(server *mcp.Server, execCmd execCmdFunc) {
	var checkpoints sync.Map
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

Omit workflow_name for a compact structured default-branch fleet report. If next_action.type is resume, pass its checkpoint as resume in the same session within 30 minutes. Fleet collection may save a private temporary checkpoint.

Supply workflow_name for a detailed JSON report for one workflow.
Reports use a bounded current-revision sample, not exhaustive history. Failure clusters reuse typed Jobs API observations and compatible cached evidence; sub-agent failures require current-version cached usage. Cache compatibility and job-evidence completeness are explicit; no artifacts are downloaded.
Continuation requests are metadata-only intent, not successful continuations. Per-run cached request counts and requested runs are separate from unavailable evidence (not zero); Actions status is unchanged. Uses the server's existing GitHub credentials and does not modify workflows or cached logs.`,
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
		if args.WorkflowName != "" && (args.Resume != "" || args.Full) {
			return nil, nil, newMCPError(jsonrpc.CodeInvalidParams, "resume and full require omitting workflow_name", nil)
		}

		cmd := healthMCPCommand(args)
		var checkpoint *healthMCPCheckpoint
		var token string
		if args.WorkflowName == "" {
			var err error
			checkpoint, token, err = openHealthMCPCheckpoint(&checkpoints, req.Session, args.Resume)
			if err != nil {
				return nil, nil, newMCPError(jsonrpc.CodeInvalidParams, "could not open fleet checkpoint", err.Error())
			}
			defer checkpoint.mu.Unlock()
			keep := args.Resume != ""
			defer func() {
				if !keep {
					checkpoint.expiry.Stop()
					removeHealthMCPCheckpoint(&checkpoints, token, checkpoint)
				}
			}()
			cmd = append(cmd, "--checkpoint="+filepath.Join(checkpoint.dir, "health.json"))
			return runHealthMCPFleet(ctx, req, execCmd, cmd, token, &keep)
		}
		return runNamedHealthMCP(ctx, req, execCmd, cmd)
	})
}

func runNamedHealthMCP(ctx context.Context, req *mcp.CallToolRequest, execCmd execCmdFunc, cmd []string) (*mcp.CallToolResult, any, error) {
	notifyProgress(ctx, req, 0, 100, "Reading workflow health...")
	stdout, stderr, err := runMCPExecOutputWithStderr(ctx, execCmd, cmd...)
	if err != nil {
		detail := strings.TrimSpace(string(stderr))
		if detail == "" {
			detail = err.Error()
		}
		return nil, nil, newMCPError(jsonrpc.CodeInternalError, "health command failed: "+detail, nil)
	}
	var report struct {
		Collection struct{ Partial bool }
	}
	if err := json.Unmarshal(stdout, &report); err != nil {
		return nil, nil, newMCPError(jsonrpc.CodeInternalError, "health command returned invalid JSON", nil)
	}
	notifyProgress(ctx, req, 100, 100, healthMCPProgress(report.Collection.Partial))
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(stdout)}}}, nil, nil
}

func runHealthMCPFleet(ctx context.Context, req *mcp.CallToolRequest, execCmd execCmdFunc, cmd []string, token string, keep *bool) (*mcp.CallToolResult, any, error) {
	notifyProgress(ctx, req, 0, 100, "Reading workflow health...")
	stdout, stderr, err := runMCPExecOutputWithStderr(ctx, execCmd, cmd...)
	if err != nil {
		return nil, nil, newMCPError(jsonrpc.CodeInternalError, "health command failed", strings.TrimSpace(string(stderr)))
	}
	var report healthFleetReport
	if err := json.Unmarshal(stdout, &report); err != nil || report.SchemaVersion != 1 {
		return nil, nil, newMCPError(jsonrpc.CodeInternalError, "invalid health fleet report", nil)
	}
	*keep = report.NextAction != nil && report.NextAction.Type == "resume"
	if *keep {
		report.NextAction.Checkpoint = token
	}
	notifyProgress(ctx, req, 100, 100, healthMCPProgress(report.Coverage.Partial))
	return nil, report, nil
}

type healthMCPCheckpoint struct {
	dir     string
	session *mcp.ServerSession
	mu      sync.Mutex
	expiry  *time.Timer
}

func removeHealthMCPCheckpoint(checkpoints *sync.Map, token string, checkpoint *healthMCPCheckpoint) {
	checkpoints.Delete(token)
	for _, path := range []string{filepath.Join(checkpoint.dir, "health.json"), checkpoint.dir} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			mcpLog.Printf("Remove health checkpoint: %v", err)
		}
	}
}

func openHealthMCPCheckpoint(checkpoints *sync.Map, session *mcp.ServerSession, resume string) (*healthMCPCheckpoint, string, error) {
	if resume != "" {
		value, _ := checkpoints.Load(resume)
		checkpoint, ok := value.(*healthMCPCheckpoint)
		if !ok || checkpoint.session != session {
			return nil, "", os.ErrNotExist
		}
		if !checkpoint.mu.TryLock() {
			return nil, "", os.ErrExist
		}
		if _, live := checkpoints.Load(resume); !live {
			checkpoint.mu.Unlock()
			return nil, "", os.ErrNotExist
		}
		return checkpoint, resume, nil
	}
	dir, err := os.MkdirTemp("", "gh-aw-health-*")
	if err != nil {
		return nil, "", err
	}
	token := filepath.Base(dir)
	checkpoint := &healthMCPCheckpoint{dir: dir, session: session}
	checkpoint.mu.Lock()
	checkpoints.Store(token, checkpoint)
	checkpoint.expiry = time.AfterFunc(30*time.Minute, func() {
		checkpoint.mu.Lock()
		defer checkpoint.mu.Unlock()
		removeHealthMCPCheckpoint(checkpoints, token, checkpoint)
	})
	return checkpoint, token, nil
}

func healthMCPProgress(partial bool) string {
	if partial {
		return "Workflow health returned partial evidence"
	}
	return "Workflow health read complete"
}

func healthMCPCommand(args healthArgs) []string {
	cmdArgs := []string{"health", "--json"}
	if args.Full {
		cmdArgs = append(cmdArgs, "--full")
	}
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
