package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultMCPToolResponseTimeout = 55 * time.Second
	mcpToolResponseMargin         = 5 * time.Second
)

// Leave time to return partial results before the default 60s RPC deadline.
func mcpToolResponseDeadline(ctx context.Context) time.Time {
	if deadline, ok := ctx.Deadline(); ok {
		return deadline.Add(-mcpToolResponseMargin)
	}
	return time.Now().Add(defaultMCPToolResponseTimeout)
}

type mcpStageError struct {
	stage string
	err   error
}

func (err *mcpStageError) Error() string {
	return fmt.Sprintf("%s: %v", err.stage, err.err)
}

func (err *mcpStageError) Unwrap() error {
	return err.err
}

func mcpErrorStage(err error, fallback string) string {
	var stageErr *mcpStageError
	if errors.As(err, &stageErr) {
		return stageErr.stage
	}
	return fallback
}

func buildMCPCompileTimeoutResult(err error, stdout []byte, diagnostics *mcpCompileDiagnostics, workflows []string) (*mcp.CallToolResult, any, error) {
	stage := mcpErrorStage(err, "compilation")
	var results []ValidationResult
	if json.Unmarshal(stdout, &results) != nil {
		if diagnostics != nil {
			results = diagnostics.partialResults()
		}
	}
	diagnosticSummary := "Compiler and scanner diagnostics were withheld because subprocess output may contain sensitive or untrusted data."
	if diagnostics != nil && diagnostics.timeoutNote != "" {
		diagnosticSummary = diagnostics.timeoutNote
	}
	envelope := map[string]any{
		"error":           "compile timed out during " + stage,
		"partial":         true,
		"complete":        false,
		"stage":           stage,
		"workflows":       workflows,
		"partial_results": results,
		"diagnostics":     diagnosticSummary,
	}
	output, marshalErr := json.Marshal(envelope)
	if marshalErr != nil {
		return nil, nil, marshalErr
	}
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: string(output)}},
	}, nil, nil
}
