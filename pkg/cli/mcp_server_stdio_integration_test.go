//go:build integration

package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestMCPServer_StdioDiagnosticsGoToStderr(t *testing.T) {
	binaryPath := testutil.RequireGhAwBinary(t)

	tmpDir := testutil.TempDir(t, "mcp-stdio-*")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	workflowPath := filepath.Join(workflowsDir, "test.md")
	workflowContent := `---
on: push
engine: copilot
---
# Test Workflow
`
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	if err := initTestGitRepo(tmpDir); err != nil {
		t.Fatalf("Failed to initialize git repository: %v", err)
	}

	absBinaryPath := binaryPath
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, absBinaryPath, "mcp-server", "--cmd", absBinaryPath)
	cmd.Dir = tmpDir
	cmd.Stdin = strings.NewReader("")

	env := make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "GITHUB_ACTOR=") {
			continue
		}
		env = append(env, entry)
	}
	cmd.Env = env

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("Failed to run MCP server process: %v", err)
		}
	}

	stdoutText := strings.TrimSpace(stdout.String())
	if stdoutText != "" {
		t.Fatalf("Expected stdout to remain clean for JSON-RPC, got: %q", stdoutText)
	}

}

func TestMCPServer_CompileAllWorkflows_StdoutOnlyJSONRPC(t *testing.T) {
	binaryPath := testutil.RequireGhAwBinary(t)

	tmpDir := testutil.TempDir(t, "mcp-stdio-rpc-*")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	workflowPath := filepath.Join(workflowsDir, "test.md")
	workflowContent := `---
on: push
engine: copilot
---
# Test Workflow
`
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	if err := initTestGitRepo(tmpDir); err != nil {
		t.Fatalf("Failed to initialize git repository: %v", err)
	}

	absBinaryPath := binaryPath
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)

	cmd := exec.CommandContext(ctx, absBinaryPath, "mcp-server", "--cmd", absBinaryPath)
	cmd.Dir = tmpDir
	cmd.Env = os.Environ()

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("Failed to open stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("Failed to open stdout pipe: %v", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start MCP server: %v", err)
	}
	defer func() {
		cancel()
		_ = stdin.Close()
		_ = cmd.Wait()
	}()

	reader := bufio.NewScanner(stdout)
	reader.Buffer(make([]byte, 1024), 1024*1024)

	sendJSONRPCMessage(t, stdin, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "stdio-test-client",
				"version": "1.0.0",
			},
		},
	})

	_, _ = waitForJSONRPCResponse(t, reader, 1)

	sendJSONRPCMessage(t, stdin, map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
		"params":  map[string]any{},
	})

	sendJSONRPCMessage(t, stdin, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "compile",
			"arguments": map[string]any{},
		},
	})

	response, raw := waitForJSONRPCResponse(t, reader, 2)
	if !strings.Contains(raw, `"jsonrpc":"2.0"`) {
		t.Fatalf("Expected JSON-RPC response on stdout, got: %s", raw)
	}

	resultData, ok := response["result"].(map[string]any)
	if !ok {
		t.Fatalf("Expected result object in compile response, got: %#v", response["result"])
	}
	contentList, ok := resultData["content"].([]any)
	if !ok || len(contentList) == 0 {
		t.Fatalf("Expected non-empty content list in compile result, got: %#v", resultData["content"])
	}
	firstContent, ok := contentList[0].(map[string]any)
	if !ok {
		t.Fatalf("Expected first content item to be object, got: %#v", contentList[0])
	}
	text, _ := firstContent["text"].(string)
	if text == "" {
		t.Fatal("Expected compile result text content to be non-empty")
	}

	var compileResult []map[string]any
	if err := json.Unmarshal([]byte(text), &compileResult); err != nil {
		t.Fatalf("Expected compile response text to be pure JSON without log pollution, got parse error: %v; text=%q", err, text)
	}

	sendJSONRPCMessage(t, stdin, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "shutdown",
		"params":  map[string]any{},
	})
	_, _ = waitForJSONRPCResponse(t, reader, 3)
}

func sendJSONRPCMessage(t *testing.T, w io.Writer, msg map[string]any) {
	t.Helper()
	payload, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Failed to marshal JSON-RPC message: %v", err)
	}
	if _, err := fmt.Fprintf(w, "%s\n", payload); err != nil {
		t.Fatalf("Failed to write JSON-RPC message: %v", err)
	}
}

// waitForJSONRPCResponse reads JSON-RPC lines from stdout until it finds the
// response with the expected ID. It returns the decoded response object and the
// raw JSON line as read from stdout.
func waitForJSONRPCResponse(t *testing.T, scanner *bufio.Scanner, expectedID int) (response map[string]any, rawLine string) {
	t.Helper()

	for scanner.Scan() {
		line := scanner.Text()
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatalf("Non-JSON output detected on MCP stdout (stdout must be JSON-RPC only): %q (err=%v)", line, err)
		}

		if version, ok := msg["jsonrpc"].(string); !ok || version != "2.0" {
			t.Fatalf("Non-JSON-RPC output detected on MCP stdout: %q", line)
		}

		if rawID, ok := msg["id"]; ok {
			if messageID, ok := rawID.(float64); ok && int(messageID) == expectedID {
				return msg, line
			}
		}
	}

	if err := scanner.Err(); err != nil {
		t.Fatalf("Failed while reading MCP stdout: %v", err)
	}
	t.Fatalf("Did not receive JSON-RPC response for id=%d", expectedID)
	return nil, ""
}
