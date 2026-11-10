package cli

import (
	"encoding/json"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/stringutil"
)

var ghaLogTimestampPrefixPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T`)

// extractPreAgentStepErrors scans workflow step log files for actionable failure content.
// It always prefers GitHub Actions ##[error] annotations from step logs. When no
// annotations are found and the agent did not execute, it falls back to the final
// step log content. When the agent did execute, it falls back to a short excerpt from
// agent-stdio.log so failed runs still surface concrete diagnostics.
//
// Step log files are stored in workflow-logs/{job}/{step_num}_{step_name}.txt after
// downloading via downloadWorkflowRunLogs. The function first scans all step logs for
// ##[error] annotations (GitHub Actions error annotations), which are the most precise
// failure indicators. If none are found, it falls back to the content of the last step
// (highest step number) as a general failure indicator.
func extractPreAgentStepErrors(logsPath string) []ValidationIssue {
	agentStdioPath := filepath.Join(logsPath, "agent-stdio.log")
	agentRan := fileutil.FileExists(agentStdioPath)

	const maxMessageLen = 1500

	workflowLogsDir := filepath.Join(logsPath, "workflow-logs")
	if _, err := os.Stat(workflowLogsDir); err != nil {
		auditReportLog.Printf("workflow-logs directory not found, skipping step log extraction")
		if agentError := extractAgentFailureError(agentRan, agentStdioPath, maxMessageLen); len(agentError) > 0 {
			return agentError
		}
		return nil
	}

	errorAnnotations, lastStep, err := scanWorkflowStepLogs(workflowLogsDir, maxMessageLen)
	if err != nil {
		return nil
	}
	if len(errorAnnotations) > 0 {
		return errorAnnotations
	}

	if agentError := extractAgentFailureError(agentRan, agentStdioPath, maxMessageLen); len(agentError) > 0 {
		return agentError
	}

	return extractLastStepFallbackError(lastStep, workflowLogsDir, maxMessageLen)
}

type stepLog struct {
	path    string
	num     int
	stepKey string
}

func scanWorkflowStepLogs(workflowLogsDir string, maxMessageLen int) ([]ValidationIssue, *stepLog, error) {
	jobDirs, err := os.ReadDir(workflowLogsDir)
	if err != nil {
		return nil, nil, err
	}

	var lastStep *stepLog
	var errorAnnotations []ValidationIssue

	for _, jobEntry := range jobDirs {
		if !jobEntry.IsDir() {
			lastStep, errorAnnotations = scanFlatStepLog(
				workflowLogsDir,
				jobEntry.Name(),
				lastStep,
				errorAnnotations,
				maxMessageLen,
			)
			continue
		}
		lastStep, errorAnnotations = scanNestedStepLogs(
			workflowLogsDir,
			jobEntry.Name(),
			lastStep,
			errorAnnotations,
			maxMessageLen,
		)
	}

	return errorAnnotations, lastStep, nil
}

func scanFlatStepLog(
	workflowLogsDir, filename string,
	lastStep *stepLog,
	errorAnnotations []ValidationIssue,
	maxMessageLen int,
) (*stepLog, []ValidationIssue) {
	if !strings.HasSuffix(filename, ".txt") {
		return lastStep, errorAnnotations
	}
	num, jobName := parseStepFilename(filename)
	if num <= 0 {
		return lastStep, errorAnnotations
	}

	flatFilePath := filepath.Join(workflowLogsDir, filename)
	lastStep = updateLastStep(lastStep, flatFilePath, num, jobName)
	errorAnnotations = appendErrorAnnotation(errorAnnotations, flatFilePath, jobName, num, maxMessageLen, "flat job log")
	return lastStep, errorAnnotations
}

func scanNestedStepLogs(
	workflowLogsDir, jobName string,
	lastStep *stepLog,
	errorAnnotations []ValidationIssue,
	maxMessageLen int,
) (*stepLog, []ValidationIssue) {
	jobDir := filepath.Join(workflowLogsDir, jobName)
	stepFiles, err := os.ReadDir(jobDir)
	if err != nil {
		return lastStep, errorAnnotations
	}

	for _, stepFile := range stepFiles {
		if stepFile.IsDir() || !strings.HasSuffix(stepFile.Name(), ".txt") {
			continue
		}
		num, stepName := parseStepFilename(stepFile.Name())
		if num <= 0 {
			continue
		}

		stepFilePath := filepath.Join(jobDir, stepFile.Name())
		stepKey := path.Join(jobName, stepName)
		lastStep = updateLastStep(lastStep, stepFilePath, num, stepKey)
		errorAnnotations = appendErrorAnnotation(errorAnnotations, stepFilePath, stepKey, num, maxMessageLen, "step")
	}

	return lastStep, errorAnnotations
}

func updateLastStep(lastStep *stepLog, path string, num int, stepKey string) *stepLog {
	if lastStep == nil || num > lastStep.num {
		return &stepLog{
			path:    path,
			num:     num,
			stepKey: stepKey,
		}
	}
	return lastStep
}

func appendErrorAnnotation(
	errorAnnotations []ValidationIssue,
	filePath, stepKey string,
	num int,
	maxMessageLen int,
	logLabel string,
) []ValidationIssue {
	errorLines := extractGHErrorLines(filePath)
	if len(errorLines) == 0 {
		return errorAnnotations
	}

	message := stringutil.Truncate(strings.Join(errorLines, "\n"), maxMessageLen)
	auditReportLog.Printf("Extracted ##[error] annotations from %s %s (%d)", logLabel, stepKey, num)

	return append(errorAnnotations, ValidationIssue{
		Type:    "step_failure",
		File:    stepKey,
		Message: message,
	})
}

func extractGHErrorLines(filePath string) []string {
	content, err := os.ReadFile(filePath)
	if err != nil {
		auditReportLog.Printf("Failed to read step log %s: %v", filePath, err)
		return nil
	}

	var errorLines []string
	for line := range strings.SplitSeq(string(content), "\n") {
		if strings.Contains(line, "##[error]") {
			stripped := stripGHALogTimestamps(line)
			if stripped != "" && !isAgentToolResultAnnotation(stripped) {
				errorLines = append(errorLines, stripped)
			}
		}
	}

	return errorLines
}

func isAgentToolResultAnnotation(line string) bool {
	_, payload, found := strings.Cut(line, "##[error]")
	if !found {
		return false
	}

	var event struct {
		Type    string `json:"type"`
		Message struct {
			Content []struct {
				Type string `json:"type"`
			} `json:"content"`
		} `json:"message"`
	}
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(payload)))
	if err := decoder.Decode(&event); err != nil || event.Type != "user" {
		return false
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return false
	}
	hasToolResult := false
	for _, content := range event.Message.Content {
		if content.Type != "tool_result" {
			return false
		}
		hasToolResult = true
	}
	return hasToolResult
}

func extractAgentFailureError(agentRan bool, agentStdioPath string, maxMessageLen int) []ValidationIssue {
	if !agentRan {
		return nil
	}
	if agentExcerpt := extractAgentStdioFailureExcerpt(agentStdioPath, maxMessageLen); agentExcerpt != "" {
		return []ValidationIssue{{
			Type:    "agent_failure",
			File:    "agent-stdio.log",
			Message: agentExcerpt,
		}}
	}
	return nil
}

func extractLastStepFallbackError(lastStep *stepLog, workflowLogsDir string, maxMessageLen int) []ValidationIssue {
	if lastStep == nil {
		auditReportLog.Printf("No step log files found in %s", workflowLogsDir)
		return nil
	}

	content, err := os.ReadFile(lastStep.path)
	if err != nil {
		auditReportLog.Printf("Failed to read step log %s: %v", lastStep.path, err)
		return nil
	}

	message := stripGHALogTimestamps(strings.TrimSpace(string(content)))
	if message == "" {
		return nil
	}

	message = stringutil.Truncate(message, maxMessageLen)

	auditReportLog.Printf("Extracted pre-agent step error from %s (step %d) as fallback", lastStep.stepKey, lastStep.num)
	return []ValidationIssue{{
		Type:    "step_failure",
		File:    lastStep.stepKey,
		Message: message,
	}}
}

func extractAgentStdioFailureExcerpt(agentStdioPath string, maxMessageLen int) string {
	f, err := os.Open(agentStdioPath)
	if err != nil {
		auditReportLog.Printf("Failed to read %s: %v", agentStdioPath, err)
		return ""
	}
	defer func() {
		if err := f.Close(); err != nil {
			auditReportLog.Printf("Failed to close agent startup log: %v", err)
		}
	}()

	// Read only the tail of the file to bound memory use for large logs.
	const maxReadBytes int64 = 64 * 1024 // 64 KB tail window

	fi, err := f.Stat()
	if err != nil {
		auditReportLog.Printf("Failed to stat %s: %v", agentStdioPath, err)
		return ""
	}
	if fi.Size() > maxReadBytes {
		if _, err := f.Seek(-maxReadBytes, io.SeekEnd); err != nil {
			auditReportLog.Printf("Failed to seek in %s: %v", agentStdioPath, err)
			return ""
		}
	}

	tail, err := io.ReadAll(f)
	if err != nil {
		auditReportLog.Printf("Failed to read %s: %v", agentStdioPath, err)
		return ""
	}

	var lines []string
	for line := range strings.Lines(stripGHALogTimestamps(string(tail))) {
		lines = append(lines, strings.TrimRight(line, "\r\n"))
	}
	nonEmpty, errorLike := classifyAgentStdioLines(lines)

	if len(errorLike) > 0 {
		const maxErrorLines = 5
		start := max(0, len(errorLike)-maxErrorLines)
		return stringutil.Truncate(strings.Join(errorLike[start:], "\n"), maxMessageLen)
	}

	if len(nonEmpty) == 0 {
		return ""
	}

	const maxTailLines = 10
	start := max(0, len(nonEmpty)-maxTailLines)
	return stringutil.Truncate(strings.Join(nonEmpty[start:], "\n"), maxMessageLen)
}

func classifyAgentStdioLines(lines []string) ([]string, []string) {
	nonEmpty := make([]string, 0, len(lines))
	errorLike := make([]string, 0, len(lines))

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		nonEmpty = append(nonEmpty, trimmed)

		lower := strings.ToLower(trimmed)
		if strings.Contains(lower, "##[error]") ||
			strings.HasPrefix(lower, "error:") ||
			strings.HasPrefix(lower, "fatal:") ||
			strings.HasPrefix(lower, "panic:") {
			errorLike = append(errorLike, trimmed)
		}
	}

	return nonEmpty, errorLike
}

// parseStepFilename extracts the step number and name from a GitHub Actions step log
// filename in the format "{step_num}_{step_name}.txt" (e.g. "12_Validate lockdown mode.txt").
// Returns (0, filename) if the filename does not match the expected format.
func parseStepFilename(filename string) (int, string) {
	base := strings.TrimSuffix(filename, ".txt")
	idx := strings.IndexByte(base, '_')
	if idx <= 0 {
		return 0, base
	}
	num, err := strconv.Atoi(base[:idx])
	if err != nil {
		return 0, base
	}
	return num, base[idx+1:]
}

// stripGHALogTimestamps removes GitHub Actions timestamp prefixes from each line of a log.
// GitHub Actions step log files prefix each line with an RFC3339 timestamp followed by a space,
// e.g. "2024-01-01T10:00:00.1234567Z message here". This function strips those prefixes so the
// returned string contains only the actual log content.
func stripGHALogTimestamps(content string) string {
	var stripped []string
	for raw := range strings.Lines(content) {
		line := strings.TrimSuffix(strings.TrimSuffix(raw, "\n"), "\r")
		// GHA timestamp format: YYYY-MM-DDTHH:MM:SS[.sss...]Z<space>
		// The 'T' separator is always at position 10. Search for the terminating 'Z' after 'T'
		// in a generous window (positions 11-35) to handle any fractional seconds length.
		if len(line) > 19 && ghaLogTimestampPrefixPattern.MatchString(line) {
			// Find the Z that ends the timestamp within a reasonable range
			searchBound := min(35, len(line))
			if zIdx := strings.IndexByte(line[11:searchBound], 'Z'); zIdx >= 0 {
				zPos := 11 + zIdx
				if zPos+1 <= len(line) {
					line = line[zPos+1:]
					// Skip leading space after the timestamp
					line = strings.TrimPrefix(line, " ")
				}
			}
		}
		stripped = append(stripped, line)
	}
	result := strings.Join(stripped, "\n")
	if strings.HasSuffix(content, "\n") {
		result += "\n"
	}
	return result
}
