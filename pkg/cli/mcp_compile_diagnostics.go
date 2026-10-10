package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/stringutil"
)

const (
	maxMCPCompileDiagnosticBytes = 4 << 10
	maxMCPCompileFallbackBytes   = 48 << 10
	compileDiagnosticTruncation  = "\n... (diagnostic truncated)"
)

// compileDiagnosticText retains both ends of a line without buffering debug floods.
type compileDiagnosticText struct {
	head  []byte
	tail  []byte
	total int
}

func (text *compileDiagnosticText) Write(data []byte) {
	text.total += len(data)
	half := maxMCPCompileDiagnosticBytes / 2
	if remaining := half - len(text.head); remaining > 0 {
		n := min(remaining, len(data))
		text.head = append(text.head, data[:n]...)
		data = data[n:]
	}
	if len(data) >= half {
		text.tail = append(text.tail[:0], data[len(data)-half:]...)
	} else {
		if excess := len(text.tail) + len(data) - half; excess > 0 {
			text.tail = text.tail[excess:]
		}
		text.tail = append(text.tail, data...)
	}
}

func (text *compileDiagnosticText) String() string {
	if text.total <= maxMCPCompileDiagnosticBytes {
		return strings.ToValidUTF8(string(text.head)+string(text.tail), "")
	}
	head := strings.ToValidUTF8(string(text.head), "")
	tail := strings.ToValidUTF8(string(text.tail), "")
	return head + " ... (diagnostic truncated) ... " + tail
}

// Shellcheck findings are retained separately so bounding execution failures does
// not discard scanner diagnostics accompanying structured compiler stdout.
type mcpCompileDiagnostics struct {
	line       compileDiagnosticText
	message    string
	inBlock    bool
	errorSeen  bool
	omitted    bool
	warning    string
	shellcheck strings.Builder
	inFinding  bool
}

func (diagnostics *mcpCompileDiagnostics) Write(data []byte) (int, error) {
	size := len(data)
	for len(data) > 0 {
		index := bytes.IndexByte(data, '\n')
		if index == -1 {
			diagnostics.line.Write(data)
			break
		}
		diagnostics.line.Write(data[:index])
		diagnostics.finishLine()
		data = data[index+1:]
	}
	return size, nil
}

func (diagnostics *mcpCompileDiagnostics) finishLine() {
	line := diagnostics.line.String()
	if diagnostics.line.total > maxMCPCompileDiagnosticBytes {
		diagnostics.omitted = true
	}
	diagnostics.line = compileDiagnosticText{}
	if strings.ContainsRune(line, '\x1b') {
		line = stringutil.StripANSI(line)
	}
	line = strings.TrimSpace(line)
	if isCompileDebugLine(line) {
		diagnostics.omitted = true
		diagnostics.inBlock = false
		return
	}
	switch {
	case strings.Contains(line, "shellcheck findings in "):
		diagnostics.shellcheck.WriteString("\n\n")
		diagnostics.shellcheck.WriteString(line)
		diagnostics.inFinding = true
	case diagnostics.inFinding && (strings.Contains(line, "script:") || strings.HasPrefix(line, "script ")):
		diagnostics.shellcheck.WriteString("\n")
		diagnostics.shellcheck.WriteString(line)
	case line == "":
		diagnostics.inFinding = false
	}
	if line == "" || isCompileProgressLine(line) {
		if strings.HasPrefix(line, "⚠") {
			diagnostics.warning = boundMCPCompileDiagnostic(line)
		}
		diagnostics.inBlock = false
		return
	}
	isError := strings.HasPrefix(line, "✗") || strings.HasPrefix(line, "Error:") ||
		strings.HasPrefix(line, "error:") || strings.HasPrefix(line, "fatal:") ||
		strings.Contains(line, ": error:")
	if isError || !diagnostics.inBlock && !diagnostics.errorSeen {
		diagnostics.message = line
		diagnostics.errorSeen = diagnostics.errorSeen || isError
		diagnostics.inBlock = true
	} else if diagnostics.inBlock {
		diagnostics.message += "\n" + line
	}
	if len(diagnostics.message) > maxMCPCompileDiagnosticBytes {
		diagnostics.message = boundMCPCompileDiagnostic(diagnostics.message)
		diagnostics.omitted = true
	}
}

func isCompileDebugLine(line string) bool {
	namespace, _, found := strings.Cut(line, " ")
	if !found {
		return false
	}
	for _, prefix := range []string{"cli:", "workflow:", "parser:", "mcp:", "agentdrain:", "repoutil:", "logger:", "stringutil:"} {
		if strings.HasPrefix(namespace, prefix) {
			return true
		}
	}
	return false
}

func isCompileProgressLine(line string) bool {
	for _, prefix := range []string{"✓", "ℹ", "⚠", "⚡", "🔨", "❓", "🔍", "Progress:"} {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func (diagnostics *mcpCompileDiagnostics) failureMessage(err error) string {
	message := diagnostics.message
	if message == "" {
		message = diagnostics.warning
	}
	if message == "" {
		message = err.Error()
	}
	if diagnostics.omitted {
		message += "\n... (debug or oversized diagnostics omitted)"
	}
	return boundMCPCompileDiagnostic(message)
}

func boundMCPCompileDiagnostic(message string) string {
	message = strings.ToValidUTF8(message, "")
	if len(message) <= maxMCPCompileDiagnosticBytes {
		return message
	}
	budget := maxMCPCompileDiagnosticBytes - len(compileDiagnosticTruncation) - 1
	head := strings.ToValidUTF8(message[:budget/2], "")
	tail := strings.ToValidUTF8(message[len(message)-(budget-budget/2):], "")
	return head + compileDiagnosticTruncation + "\n" + tail
}

func runMCPCompileOutput(ctx context.Context, execCmd execCmdFunc, args ...string) ([]byte, *mcpCompileDiagnostics, error) {
	diagnostics := &mcpCompileDiagnostics{}
	if err := defaultMCPSubprocessGuardrail.acquire(ctx); err != nil {
		return nil, diagnostics, err
	}
	defer defaultMCPSubprocessGuardrail.release()
	cmd := execCmd(ctx, args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = diagnostics
	err := cmd.Run()
	diagnostics.finishLine()
	return stdout.Bytes(), diagnostics, err
}

func marshalMCPCompileErrorResults(workflows []string, message string) ([]byte, error) {
	message = boundMCPCompileDiagnostic(message)
	results := buildCompileErrorResults(workflows, message)
	output, err := json.Marshal(results)
	if err != nil || len(output) <= maxMCPCompileFallbackBytes {
		return output, err
	}
	message = boundMCPCompileDiagnostic(fmt.Sprintf("%s\nBatch compilation failed for %d workflows; per-workflow entries omitted to keep the response within %d bytes.", message, len(results), maxMCPCompileFallbackBytes))
	return json.Marshal([]ValidationResult{{
		Scope:    "batch",
		Workflow: "compile",
		Valid:    false,
		Errors:   []ValidationIssue{{Type: "config_error", Message: message}},
		Warnings: []ValidationIssue{},
	}})
}
