package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type workQueueArgs struct {
	Operation   string `json:"operation" jsonschema:"Read-only operation: state, inspect, replay, stats, explain, trace, or evidence"`
	Repo        string `json:"repo" jsonschema:"GitHub repository owner/repo"`
	Branch      string `json:"branch,omitempty" jsonschema:"Queue branch (defaults to work-queue; an explicit branch is a separate authority)"`
	WorkID      string `json:"work_id,omitempty" jsonschema:"Exact Work ID for inspect or live explain"`
	ClaimID     string `json:"claim_id,omitempty" jsonschema:"Exact Claim ID for inspect or trace"`
	RequestID   string `json:"request_id,omitempty" jsonschema:"Committed request ID for historical explain or trace"`
	BeforeClaim string `json:"before_claim,omitempty" jsonschema:"Committed Claim ID for historical explain"`
	DispatchID  string `json:"dispatch_id,omitempty" jsonschema:"Exact native reservation ID for evidence"`
	Pool        string `json:"pool,omitempty" jsonschema:"Scheduling pool for state or live explain (explain defaults to default)"`
	Graph       string `json:"graph,omitempty" jsonschema:"Exact graph namespace for state"`
	State       string `json:"state,omitempty" jsonschema:"Work state filter for state: available, claimed, completed, or cancelled"`
	Search      string `json:"search,omitempty" jsonschema:"Case-insensitive metadata or ID substring for state"`
	Offset      *int   `json:"offset,omitempty" jsonschema:"Matching row or event offset for state or trace (default 0)"`
	Limit       *int   `json:"limit,omitempty" jsonschema:"Page size 1..256 for state or trace (defaults: state 80, trace 100)"`
}

func registerWorkQueueTool(server *mcp.Server, execCmd execCmdFunc) error {
	schema, err := GenerateSchema[workQueueArgs]()
	if err != nil {
		mcpLog.Printf("Failed to generate work-queue tool schema: %v", err)
		return err
	}
	schema.Properties["operation"].Enum = []any{"state", "inspect", "replay", "stats", "explain", "trace", "evidence"}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "work-queue",
		InputSchema: schema,
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:   true,
			IdempotentHint: true,
			OpenWorldHint:  boolPtr(true),
		},
		Icons: mcpToolIcons("📋"),
		Description: `Read the Git-backed work queue without publishing, claiming, dispatching, or changing queue state.

state: bounded metadata-only Work/Claim forest with filters and pagination (recommended overview).
inspect: metadata and dependencies for exactly one work_id or claim_id.
replay: full authoritative queue projection; may include payloads and be large.
stats: ownership, graph node, and native reservation counts.
explain: live eligibility or snapshot prediction, or historical provenance using exactly one work_id, before_claim, or request_id. Historical selectors cannot be combined with pool. Predictions are not grants.
trace: paginated committed provenance for exactly one request_id or claim_id, without payloads or receipts.
evidence: delivery evidence for a required dispatch_id; does not reconcile or release reservations.

Returns the selected CLI operation's JSON output. Git-only storage uses the server's existing credentials; private repositories require read access. Options are accepted only by their corresponding operation.`,
	}, func(ctx context.Context, req *mcp.CallToolRequest, args workQueueArgs) (*mcp.CallToolResult, any, error) {
		if err := ctx.Err(); err != nil {
			return nil, nil, newMCPError(jsonrpc.CodeInternalError, "request cancelled", err.Error())
		}
		cmdArgs, err := workQueueMCPCommand(args, req.Params.Arguments)
		if err != nil {
			return nil, nil, newMCPError(jsonrpc.CodeInvalidParams, "invalid work-queue arguments: "+err.Error(), nil)
		}
		mcpLog.Printf("Executing work-queue tool: operation=%s", args.Operation)
		stdout, stderr, err := runMCPExecOutputWithStderr(ctx, execCmd, cmdArgs...)
		if err != nil {
			detail := strings.TrimSpace(string(stderr))
			if detail == "" {
				detail = err.Error()
			}
			return nil, nil, newMCPError(jsonrpc.CodeInternalError, "work-queue command failed: "+detail, nil)
		}
		if !json.Valid(stdout) {
			return nil, nil, newMCPError(jsonrpc.CodeInternalError, "work-queue command returned invalid JSON", nil)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(stdout)}}}, nil, nil
	})
	return nil
}

func workQueueMCPCommand(args workQueueArgs, raw json.RawMessage) ([]string, error) {
	var options []string
	switch args.Operation {
	case "replay", "stats":
	case "state":
		options = []string{"graph", "pool", "state", "search", "offset", "limit"}
	case "inspect":
		options = []string{"work_id", "claim_id"}
	case "explain":
		options = []string{"work_id", "before_claim", "request_id", "pool"}
	case "trace":
		options = []string{"request_id", "claim_id", "offset", "limit"}
	case "evidence":
		options = []string{"dispatch_id"}
	default:
		return nil, fmt.Errorf("unsupported read-only operation %q", args.Operation)
	}
	if args.Offset != nil && *args.Offset < 0 || args.Limit != nil && (*args.Limit < 1 || *args.Limit > 256) {
		return nil, errors.New("offset must be >= 0 and limit must be 1..256")
	}
	var supplied map[string]json.RawMessage
	if err := json.Unmarshal(raw, &supplied); err != nil {
		return nil, err
	}
	cmdArgs := []string{"work-queue", args.Operation, "--json", "--storage=git"}
	// Preserve explicit empty values and CLI flag-presence semantics. Each value
	// stays attached to its allowlisted flag, so it cannot become another flag.
	for _, name := range jsonFieldNames(workQueueArgs{}) {
		value, ok := supplied[name]
		if !ok || name == "operation" {
			continue
		}
		if name != "repo" && name != "branch" && !slices.Contains(options, name) {
			return nil, fmt.Errorf("%s is not supported by %s", name, args.Operation)
		}
		var text string
		switch name {
		case "offset", "limit":
			var number *int
			if err := json.Unmarshal(value, &number); err != nil || number == nil {
				return nil, fmt.Errorf("%s must be an integer", name)
			}
			text = strconv.Itoa(*number)
		default:
			if err := json.Unmarshal(value, &text); err != nil || string(value) == "null" {
				return nil, fmt.Errorf("%s must be a string", name)
			}
		}
		cmdArgs = append(cmdArgs, "--"+strings.ReplaceAll(name, "_", "-")+"="+text)
	}
	return cmdArgs, nil
}
