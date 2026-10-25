package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/stringutil"
)

const (
	maxMCPCompileDiagnosticBytes = 4 << 10
	maxMCPCompileFallbackBytes   = 48 << 10
	compileDiagnosticTruncation  = "\n... (diagnostic truncated)"
	compileDebugNamespace        = `[[:alnum:]_]+(?:[:./-][[:alnum:]_]+)*`
)

// Match the logger's namespace/message/elapsed format, not a fixed namespace set.
var compileDebugLinePattern = regexp.MustCompile(`^` + compileDebugNamespace + ` .* \+[0-9]+(?:\.[0-9]+)?(?:ns|[µμ]s|ms|s|m|h)$`)
var compileDebugStartPattern = regexp.MustCompile(`^` + compileDebugNamespace + ` `)
var compileDebugEndPattern = regexp.MustCompile(`(?:^| )\+[0-9]+(?:\.[0-9]+)?(?:ns|[µμ]s|ms|s|m|h)$`)

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
	line         compileDiagnosticText
	message      string
	inBlock      bool
	errorSeen    bool
	omitted      bool
	warning      string
	warnings     strings.Builder
	inWarning    bool
	shellcheck   strings.Builder
	inFinding    bool
	debugLines   []string
	debugBytes   int
	progressDir  string
	progressFile string
	timeoutNote  string
}

func (diagnostics *mcpCompileDiagnostics) partialResults() []ValidationResult {
	if diagnostics == nil || diagnostics.progressFile == "" {
		return nil
	}
	output, err := os.ReadFile(diagnostics.progressFile)
	if err != nil {
		return nil
	}
	var results []ValidationResult
	if json.Unmarshal(output, &results) != nil {
		return nil
	}
	return results
}

func (diagnostics *mcpCompileDiagnostics) cleanup() {
	if diagnostics != nil && diagnostics.progressDir != "" {
		_ = os.RemoveAll(diagnostics.progressDir)
	}
}

func validateMCPCompileProgressPath(path string) error {
	if path == "" {
		return nil
	}
	cleanPath := filepath.Clean(path)
	dir := filepath.Dir(cleanPath)
	if filepath.Base(cleanPath) != "results.json" ||
		filepath.Dir(dir) != filepath.Clean(os.TempDir()) ||
		!strings.HasPrefix(filepath.Base(dir), "gh-aw-mcp-compile-") {
		return errors.New("MCP compile progress file must be inside a private temporary directory")
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return errors.New("MCP compile progress directory must be a private temporary directory")
	}
	return nil
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
		diagnostics.consumeLine()
		data = data[index+1:]
	}
	return size, nil
}

func (diagnostics *mcpCompileDiagnostics) finishLine() {
	if diagnostics.line.total > 0 {
		diagnostics.consumeLine()
	}
	diagnostics.replayDebugLines()
}

func (diagnostics *mcpCompileDiagnostics) consumeLine() {
	line := diagnostics.line.String()
	if diagnostics.line.total > maxMCPCompileDiagnosticBytes {
		diagnostics.omitted = true
	}
	diagnostics.line = compileDiagnosticText{}
	if strings.ContainsRune(line, '\x1b') {
		line = stringutil.StripANSI(line)
	}
	debugStart := compileDebugStartPattern.MatchString(line)
	unindented := line == strings.TrimLeft(line, " \t")
	line = strings.TrimSpace(line)
	if unindented && (isCompileErrorLine(line) || isCompileWarningLine(line) || strings.Contains(line, "shellcheck findings in ")) {
		diagnostics.replayDebugLines()
		diagnostics.recordDiagnosticLine(line)
		return
	}
	if isCompileDebugLine(line) && (len(diagnostics.debugLines) == 0 || debugStart) {
		// A new complete record cannot confirm an earlier incomplete record.
		diagnostics.replayDebugLines()
		diagnostics.omitted = true
		return
	}
	// Namespace-like diagnostics are ambiguous until an elapsed suffix confirms
	// a multiline logger record. Replay unconfirmed text at EOF or the byte limit.
	if debugStart && !compileDebugEndPattern.MatchString(line) {
		diagnostics.replayDebugLines()
	}
	if len(diagnostics.debugLines) > 0 {
		if diagnostics.debugBytes+len(line)+1 > maxMCPCompileDiagnosticBytes {
			diagnostics.replayDebugLines()
		} else {
			diagnostics.debugLines = append(diagnostics.debugLines, line)
			diagnostics.debugBytes += len(line) + 1
			if compileDebugEndPattern.MatchString(line) {
				diagnostics.debugLines = nil
				diagnostics.debugBytes = 0
				diagnostics.omitted = true
			}
			return
		}
	} else if debugStart && len(line)+1 <= maxMCPCompileDiagnosticBytes {
		diagnostics.debugLines = []string{line}
		diagnostics.debugBytes = len(line) + 1
		return
	}
	diagnostics.recordDiagnosticLine(line)
}

func (diagnostics *mcpCompileDiagnostics) replayDebugLines() {
	for _, line := range diagnostics.debugLines {
		diagnostics.recordDiagnosticLine(line)
	}
	diagnostics.debugLines = nil
	diagnostics.debugBytes = 0
}

func (diagnostics *mcpCompileDiagnostics) recordDiagnosticLine(line string) {
	switch {
	case strings.Contains(line, "shellcheck findings in "):
		diagnostics.inWarning = false
		diagnostics.shellcheck.WriteString("\n\n")
		diagnostics.shellcheck.WriteString(line)
		diagnostics.inFinding = true
		return
	case diagnostics.inFinding && (strings.Contains(line, "script:") || strings.HasPrefix(line, "script ")):
		diagnostics.shellcheck.WriteString("\n")
		diagnostics.shellcheck.WriteString(line)
		return
	case line == "":
		diagnostics.inFinding = false
	}
	if line == "" {
		diagnostics.inWarning = false
		diagnostics.inBlock = false
		return
	}
	isError := isCompileErrorLine(line)
	if isCompileWarningLine(line) {
		diagnostics.recordWarning(line)
		diagnostics.inWarning = true
		diagnostics.inBlock = false
		return
	}
	if isCompileProgressLine(line) {
		diagnostics.inWarning = false
		diagnostics.inBlock = false
		return
	}
	if isError {
		diagnostics.inWarning = false
	}
	if diagnostics.inWarning && !isError {
		diagnostics.appendWarningContinuation(line)
		return
	}
	diagnostics.recordFailureLine(line, isError)
}

func isCompileErrorLine(line string) bool {
	return strings.HasPrefix(line, "✗") || strings.HasPrefix(line, "Error:") ||
		strings.HasPrefix(line, "error:") || strings.HasPrefix(line, "fatal:") ||
		strings.Contains(line, ": error:")
}

func (diagnostics *mcpCompileDiagnostics) recordFailureLine(line string, isError bool) {
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

func (diagnostics *mcpCompileDiagnostics) recordWarning(line string) {
	diagnostics.warning = boundMCPCompileDiagnostic(line)
	if diagnostics.warnings.Len() == 0 && len(line) > maxMCPCompileDiagnosticBytes/2 {
		line = strings.ToValidUTF8(line[:maxMCPCompileDiagnosticBytes/4], "") +
			compileDiagnosticTruncation + "\n" +
			strings.ToValidUTF8(line[len(line)-maxMCPCompileDiagnosticBytes/4:], "")
		diagnostics.omitted = true
	}
	diagnostics.appendWarningLine(line)
}

func (diagnostics *mcpCompileDiagnostics) appendWarningContinuation(line string) {
	warning := diagnostics.warning + "\n" + line
	if len(warning) > maxMCPCompileDiagnosticBytes {
		diagnostics.omitted = true
	}
	diagnostics.warning = boundMCPCompileDiagnostic(warning)
	diagnostics.appendWarningLine(line)
}

func (diagnostics *mcpCompileDiagnostics) appendWarningLine(line string) {
	if diagnostics.warnings.Len()+len(line)+1 <= maxMCPCompileDiagnosticBytes {
		if diagnostics.warnings.Len() > 0 {
			diagnostics.warnings.WriteByte('\n')
		}
		diagnostics.warnings.WriteString(line)
	} else {
		diagnostics.omitted = true
	}
}

func isCompileDebugLine(line string) bool {
	return compileDebugLinePattern.MatchString(line)
}

func isCompileProgressLine(line string) bool {
	for _, prefix := range []string{"✓", "ℹ", "⚠", "⚡", "🔨", "❓", "🔍", "Progress:"} {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func isCompileWarningLine(line string) bool {
	return strings.HasPrefix(line, "⚠") || strings.HasPrefix(line, "warning:") ||
		strings.HasPrefix(line, "Warning:") || strings.Contains(line, ": warning:")
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
		return nil, diagnostics, &mcpStageError{stage: "subprocess_slot", err: err}
	}
	defer defaultMCPSubprocessGuardrail.release()
	if progressDir, err := os.MkdirTemp("", "gh-aw-mcp-compile-"); err == nil {
		diagnostics.progressDir = progressDir
		diagnostics.progressFile = filepath.Join(progressDir, "results.json")
		progressArgs := []string{"--mcp-progress-file", diagnostics.progressFile}
		inserted := false
		for index, arg := range args {
			if arg == "--" {
				args = append(args[:index], append(progressArgs, args[index:]...)...)
				inserted = true
				break
			}
		}
		if !inserted {
			args = append(args, progressArgs...)
		}
	}
	cmd := execCmd(ctx, args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = diagnostics
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	diagnostics.finishLine()
	if err != nil && ctx.Err() != nil {
		err = &mcpStageError{stage: "compilation", err: ctx.Err()}
	}
	return stdout.Bytes(), diagnostics, err
}

func injectDevelopmentCompilerDiagnostics(outputStr string, diagnostics *mcpCompileDiagnostics) string {
	if diagnostics.warnings.Len() == 0 {
		return outputStr
	}
	var results []ValidationResult
	if err := json.Unmarshal([]byte(outputStr), &results); err != nil {
		return outputStr
	}
	updated := false
	for i := range results {
		if i < 0 || i >= len(results) {
			continue
		}
		result := &results[i]
		if result.Scope != "batch" || result.Workflow != "compiler" || result.Valid {
			continue
		}
		for j := range result.Errors {
			if j < 0 || j >= len(result.Errors) {
				continue
			}
			issue := &result.Errors[j]
			if issue.Type == "compiler_warning" {
				updated = true
				issue.Message = boundMCPCompileDiagnostic("compiler reported warnings:\n" + diagnostics.warnings.String())
				if diagnostics.omitted {
					issue.Message = boundMCPCompileDiagnostic(issue.Message + "\n... (additional diagnostics omitted)")
				}
			}
		}
	}
	if !updated {
		return outputStr
	}
	output, err := formatValidationOutput(results)
	if err != nil {
		return outputStr
	}
	return output
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
