package workflow

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var codexLogsLog = logger.New("workflow:codex_logs")

var (
	codexLegacyTokensPattern = regexp.MustCompile(`(?i)^tokens\s+used[:\s]+([\d,]+)\s*$`)
	codexLegacyTotalPattern  = regexp.MustCompile(`\btotal_tokens:\s*([\d,]+)`)
	codexLegacyResultPattern = regexp.MustCompile(`^(?:(.*?)\s+)?(success|succeeded|failure|failed)\s+in\s+(\d+(?:\.\d+)?)(ms|s):?\s*$`)
	codexLegacyFramePattern  = regexp.MustCompile(`^\[[^\]]+\]\s+`)
	codexLegacyLogPattern    = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\S+\s+(?:DEBUG|INFO|WARN|ERROR)\s+\S+:\s*`)
	codexGroupedCountPattern = regexp.MustCompile(`^\d+(?:,\d{3})*$`)
	codexLegacyToolPattern   = regexp.MustCompile(`^ToolCall:\s+([\w-]+)__([\w-]+)\s+`)
)

// ParseLogMetrics implements engine-specific log parsing for Codex
func (e *CodexEngine) ParseLogMetrics(logContent string, verbose bool) LogMetrics {
	codexLogsLog.Printf("Parsing Codex log metrics: log_size=%d bytes, lines=%d", len(logContent), strings.Count(logContent, "\n")+1)
	legacy := e.parseCodexLegacyMetrics(logContent)
	structured := e.parseCodexJSONLMetrics(logContent)
	legacy.mergeStructured(structured)
	FinalizeToolMetrics(FinalizeToolMetricsOptions{
		Metrics:         &legacy.metrics,
		ToolCallMap:     legacy.tools,
		CurrentSequence: legacy.sequence,
		Turns:           legacy.turns,
		TokenUsage:      legacy.tokens,
	})
	codexLogsLog.Printf("Parsed Codex metrics: turns=%d, token_usage=%d, tool_calls=%d",
		legacy.metrics.Turns, legacy.metrics.TokenUsage, len(legacy.metrics.ToolCalls))
	return legacy.metrics
}

// parseCodexToolCallsWithSequence extracts tool call information from Codex log lines and returns tool name
func (e *CodexEngine) parseCodexToolCallsWithSequence(line string, toolCallMap map[string]*ToolCallInfo) string {
	name := normalizeCodexToolName(codexLegacyToolName(line))
	if name == "" {
		if command := codexLegacyExecCommand(line); command != "" {
			name = "bash_" + ShortenCommand(command)
		}
	}
	if name == "" {
		return ""
	}
	if toolCallMap[name] == nil {
		toolCallMap[name] = &ToolCallInfo{Name: name}
	}
	toolCallMap[name].CallCount++
	return name
}

func codexLegacyToolName(line string) string {
	for _, pattern := range []*regexp.Regexp{codexToolCallOldFormat, codexToolCallNewFormat} {
		if name := strings.TrimSpace(codexRegexpGroup(pattern.FindStringSubmatch(strings.TrimSpace(line)), 1)); name != "" {
			return name
		}
	}
	match := codexLegacyToolPattern.FindStringSubmatch(codexLegacyPayload(line))
	if len(match) == 0 {
		return ""
	}
	return codexRegexpGroup(match, 1) + "." + codexRegexpGroup(match, 2)
}

func codexRegexpGroup(matches []string, group int) string {
	for index, match := range matches {
		if index == group {
			return match
		}
	}
	return ""
}

func normalizeCodexToolName(name string) string {
	return strings.ReplaceAll(PrettifyToolName(strings.TrimSpace(name)), ".", "_")
}

func codexLegacyPayload(line string) string {
	payload := codexLegacyFramePattern.ReplaceAllString(strings.TrimSpace(line), "")
	return codexLegacyLogPattern.ReplaceAllString(payload, "")
}

func codexLegacyExecCommand(line string) string {
	command, found := strings.CutPrefix(codexLegacyPayload(line), "exec ")
	if !found {
		return ""
	}
	if index := strings.LastIndex(command, " in /"); index >= 0 {
		command = command[:index]
	}
	return strings.TrimSpace(command)
}

func codexLegacyCommandOutputSize(lines []string, index int) int {
	var output []string
	for _, line := range lines[index+1:] {
		payload := codexLegacyPayload(line)
		if strings.HasPrefix(line, "[") || strings.HasPrefix(payload, "tool ") || strings.HasPrefix(payload, "exec ") ||
			strings.HasPrefix(payload, "ToolCall:") || strings.HasPrefix(payload, "tokens used") ||
			payload == "thinking" || payload == "codex" || codexLegacyResultPattern.MatchString(payload) {
			break
		}
		if _, found := extractCodexLegacyTokenCount(line); found {
			break
		}
		output = append(output, line)
	}
	return len(strings.TrimRight(strings.Join(output, "\n"), "\n"))
}

// extractOutputSizeFromResult extracts output size from success/failure result lines
// Returns the character count of the output content if found, 0 otherwise
func (e *CodexEngine) extractOutputSizeFromResult(line string, lines []string, currentIndex int) int {
	// Check if this is a success or failure line
	if codexLegacyResultPattern.FindStringSubmatch(codexLegacyPayload(line)) == nil {
		return 0
	}

	// Parse JSON block following the result line
	// The format is typically:
	// [timestamp] tool.method(...) success in Xms:
	// {
	//   "content": [...],
	//   "isError": false
	// }

	if result, _, found := codexLegacyResultJSON(lines, currentIndex); found {
		return e.extractOutputSizeFromJSON(string(result))
	}
	return 0
}

func codexLegacyResultJSON(lines []string, currentIndex int) (json.RawMessage, int, bool) {
	content := strings.Join(lines[currentIndex+1:], "\n")
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return nil, currentIndex, false
	}
	var result json.RawMessage
	decoder := json.NewDecoder(strings.NewReader(content))
	if decoder.Decode(&result) != nil {
		return nil, currentIndex, false
	}
	end := currentIndex + 1 + strings.Count(content[:decoder.InputOffset()], "\n")
	return result, end, true
}

// extractOutputSizeFromJSON extracts the output size from a Codex result JSON block
func (e *CodexEngine) extractOutputSizeFromJSON(jsonStr string) int {
	// Try to parse as proper JSON first
	var result map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		// If JSON parsing fails, fallback to simple string extraction
		codexLogsLog.Printf("Failed to parse JSON result, using fallback: %v", err)
		return e.extractOutputSizeFromJSONFallback(jsonStr)
	}

	// Extract content array
	contentInterface, exists := result["content"]
	if !exists {
		return 0
	}

	contentArray, ok := contentInterface.([]any)
	if !ok {
		return 0
	}

	// Sum up text content from all content items
	totalSize := 0
	for _, item := range contentArray {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}

		// Look for text field
		if text, exists := itemMap["text"]; exists {
			if textStr, ok := text.(string); ok {
				totalSize += len(textStr)
			}
		}
	}

	return totalSize
}

// extractOutputSizeFromJSONFallback is a fallback method for extracting output size
// when proper JSON parsing fails
func (e *CodexEngine) extractOutputSizeFromJSONFallback(jsonStr string) int {
	// For simple extraction without full JSON parsing, look for "text" fields in content array
	// Format: {"content": [{"text": "...", "type": "text"}], "isError": false}

	// Find all text content - use a simple approach counting characters in quoted strings
	// after "text": markers
	totalSize := 0

	// Split by "text": to find text content
	for index, part := range strings.Split(jsonStr, "\"text\":") {
		if index == 0 {
			continue
		}
		totalSize += codexQuotedStringLength(strings.TrimSpace(part))
	}
	return totalSize
}

func codexQuotedStringLength(part string) int {
	if !strings.HasPrefix(part, `"`) {
		return 0
	}
	inEscape := false
	for index, character := range part {
		if index == 0 {
			continue
		}
		if inEscape {
			inEscape = false
			continue
		}
		switch character {
		case '\\':
			inEscape = true
		case '"':
			return index - 1
		}
	}
	return 0
}

func extractCodexLegacyTokenCount(line string) (int, bool) {
	payload := codexLegacyPayload(line)
	for _, pattern := range []*regexp.Regexp{codexLegacyTokensPattern, codexLegacyTotalPattern} {
		if pattern == codexLegacyTotalPattern && !strings.HasPrefix(payload, "total_tokens:") && !strings.Contains(payload, "TokenCount") {
			continue
		}
		if match := pattern.FindStringSubmatch(payload); len(match) > 1 {
			return parseCodexGroupedCount(codexRegexpGroup(match, 1))
		}
	}
	return 0, false
}

func parseCodexGroupedCount(value string) (int, bool) {
	if !codexGroupedCountPattern.MatchString(value) {
		return 0, false
	}
	count, err := strconv.Atoi(strings.ReplaceAll(value, ",", ""))
	return count, err == nil && float64(count) <= 9007199254740991
}

// GetLogParserScriptId returns the JavaScript script name for parsing Codex logs
func (e *CodexEngine) GetLogParserScriptId() string {
	return "parse_codex_log"
}

// GetErrorDetectionScriptId returns the JavaScript script name for detecting
// post-run agent errors from the host runner (including invalid/unsupported model names).
func (e *CodexEngine) GetErrorDetectionScriptId() string {
	return "detect_agent_errors"
}

// GetInternalLogsDir returns the host-runner path of the directory containing Codex CLI's own
// tracing/diagnostic log files ($CODEX_HOME/logs). Codex CLI writes this output (controlled by
// RUST_LOG) to files rather than to stdout/stderr, so a bare non-zero exit code with no console
// output can still have a diagnosable error recorded there. The error detection step tails the
// most recently modified log file under this directory into the step log on failure.
func (e *CodexEngine) GetInternalLogsDir() string {
	return strings.TrimSuffix(constants.TmpMcpConfigLogsDir, "/")
}
