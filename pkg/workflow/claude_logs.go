package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/typeutil"
)

var claudeLogsLog = logger.New("workflow:claude_logs")

// ParseLogMetrics implements engine-specific log parsing for Claude
func (e *ClaudeEngine) ParseLogMetrics(logContent string, verbose bool) LogMetrics {
	claudeLogsLog.Printf("Parsing Claude log metrics: %d bytes", len(logContent))
	entries := e.parseClaudeLogEntries(logContent, verbose)
	if hasClaudeResultEntry(entries) {
		return e.extractClaudeMetricsFromEntries(entries, verbose)
	}
	metrics := e.extractClaudeMetricsFromEntries(entries, verbose)
	var maxTokenUsage int

	// Process line by line for error counting and fallback parsing
	lines := strings.SplitSeq(logContent, "\n")

	for line := range lines {
		// Skip empty lines
		if strings.TrimSpace(line) == "" {
			continue
		}

		// If we haven't found cost data yet from JSON parsing, try streaming JSON
		if metrics.TokenUsage == 0 || metrics.EstimatedCost == 0 || metrics.Turns == 0 {
			jsonMetrics := ExtractJSONMetrics(line, verbose)
			if jsonMetrics.TokenUsage > 0 || jsonMetrics.EstimatedCost > 0 {
				// Check if this is a Claude result payload with aggregated costs
				if e.isClaudeResultPayload(line) {
					// For Claude result payloads, use the aggregated values directly
					if resultMetrics := e.extractClaudeResultMetrics(line); resultMetrics.TokenUsage > 0 || resultMetrics.EstimatedCost > 0 || resultMetrics.Turns > 0 {
						metrics.TokenUsage = resultMetrics.TokenUsage
						metrics.EstimatedCost = resultMetrics.EstimatedCost
						metrics.Turns = resultMetrics.Turns
					}
				} else {
					// For streaming JSON, keep the maximum token usage found
					if jsonMetrics.TokenUsage > maxTokenUsage {
						maxTokenUsage = jsonMetrics.TokenUsage
					}
					if metrics.EstimatedCost == 0 && jsonMetrics.EstimatedCost > 0 {
						metrics.EstimatedCost += jsonMetrics.EstimatedCost
					}
				}
				continue
			}
		}
	}

	// If no result payload was found, use the maximum from streaming JSON
	if metrics.TokenUsage == 0 {
		metrics.TokenUsage = maxTokenUsage
	}

	claudeLogsLog.Printf("Parsed log metrics: tokens=%d, cost=$%.4f, turns=%d", metrics.TokenUsage, metrics.EstimatedCost, metrics.Turns)
	return metrics
}

// isClaudeResultPayload checks if the JSON line is a Claude result payload with type: "result"
func (e *ClaudeEngine) isClaudeResultPayload(line string) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		return false
	}

	var jsonData map[string]any
	if err := json.Unmarshal([]byte(trimmed), &jsonData); err != nil {
		return false
	}

	typeField, exists := jsonData["type"]
	if !exists {
		return false
	}

	typeStr, ok := typeField.(string)
	return ok && typeStr == "result"
}

// extractClaudeResultMetrics extracts metrics from Claude result payload
func (e *ClaudeEngine) extractClaudeResultMetrics(line string) LogMetrics {
	claudeLogsLog.Print("Extracting metrics from Claude result payload")
	var metrics LogMetrics

	trimmed := strings.TrimSpace(line)
	var jsonData map[string]any
	if err := json.Unmarshal([]byte(trimmed), &jsonData); err != nil {
		return metrics
	}

	// Extract total_cost_usd directly
	if totalCost, exists := jsonData["total_cost_usd"]; exists {
		if cost := typeutil.ConvertToFloat(totalCost); cost > 0 {
			metrics.EstimatedCost = cost
		}
	}

	// Extract usage information with all token types
	if usage, exists := jsonData["usage"]; exists {
		if usageMap, ok := usage.(map[string]any); ok {
			inputTokens := typeutil.ConvertToInt(usageMap["input_tokens"])
			outputTokens := typeutil.ConvertToInt(usageMap["output_tokens"])
			cacheCreationTokens := typeutil.ConvertToInt(usageMap["cache_creation_input_tokens"])
			cacheReadTokens := typeutil.ConvertToInt(usageMap["cache_read_input_tokens"])

			totalTokens := inputTokens + outputTokens + cacheCreationTokens + cacheReadTokens
			if totalTokens > 0 {
				metrics.TokenUsage = totalTokens
			}
		}
	}

	// Extract number of turns
	if numTurns, exists := jsonData["num_turns"]; exists {
		if turns := typeutil.ConvertToInt(numTurns); turns > 0 {
			metrics.Turns = turns
		}
	}

	// Note: Duration extraction is handled in the main parsing logic where we have access to tool calls
	// This is because we need to distribute duration among tool calls

	claudeLogsLog.Printf("Extracted Claude result metrics: tokens=%d, cost=$%.4f, turns=%d", metrics.TokenUsage, metrics.EstimatedCost, metrics.Turns)
	return metrics
}

func (e *ClaudeEngine) parseClaudeLogEntries(logContent string, verbose bool) []map[string]any {
	var logEntries []map[string]any
	if err := json.Unmarshal([]byte(logContent), &logEntries); err == nil {
		return logEntries
	} else {
		claudeLogsLog.Print("JSON array parse failed, trying JSONL format")
		if verbose {
			fmt.Fprintf(os.Stderr, "Failed to parse Claude log as JSON array, trying JSONL format: %v\n", err)
		}
	}
	logEntries = e.parseClaudeMixedLogEntries(logContent, verbose)
	if len(logEntries) == 0 {
		if verbose {
			fmt.Fprintf(os.Stderr, "No valid JSON entries found in Claude log\n")
		}
		return nil
	}
	if verbose {
		fmt.Fprintf(os.Stderr, "Extracted %d JSON entries from mixed format Claude log\n", len(logEntries))
	}
	return logEntries
}

func (e *ClaudeEngine) parseClaudeMixedLogEntries(logContent string, verbose bool) []map[string]any {
	var logEntries []map[string]any
	lines := strings.Split(logContent, "\n")
	for i := 0; i < len(lines); i++ {
		if i < 0 || i >= len(lines) {
			break
		}
		trimmedLine := strings.TrimSpace(lines[i])
		if trimmedLine == "" {
			continue
		}
		if strings.HasPrefix(trimmedLine, "[") {
			entries, next := extractClaudeJSONArrayEntries(lines, i)
			i = next // always advance past consumed lines regardless of parse success
			if len(entries) > 0 {
				logEntries = append(logEntries, entries...)
				continue
			}
		}
		if entry, ok := parseClaudeJSONEntry(trimmedLine, verbose); ok {
			logEntries = append(logEntries, entry)
		}
	}
	return logEntries
}

func extractClaudeJSONArrayEntries(lines []string, start int) ([]map[string]any, int) {
	if start < 0 || start >= len(lines) {
		return nil, start
	}
	buf := strings.TrimSpace(lines[start])
	end := start
	if !strings.Contains(buf, "]") {
		var sb strings.Builder
		for offset, line := range lines[start+1:] {
			j := start + 1 + offset
			sb.WriteString("\n" + line)
			end = j
			if strings.Contains(line, "]") {
				break
			}
		}
		buf += sb.String()
	}
	return parseClaudeJSONArrayBuffer(buf), end
}

func parseClaudeJSONArrayBuffer(buf string) []map[string]any {
	var entries []map[string]any
	if err := json.Unmarshal([]byte(buf), &entries); err == nil {
		return entries
	}
	openIdx := strings.Index(buf, "[")
	closeIdx := strings.LastIndex(buf, "]")
	if openIdx == -1 || closeIdx == -1 || closeIdx <= openIdx {
		return nil
	}
	var subEntries []map[string]any
	if err := json.Unmarshal([]byte(buf[openIdx:closeIdx+1]), &subEntries); err == nil {
		return subEntries
	}
	return nil
}

func parseClaudeJSONEntry(trimmedLine string, verbose bool) (map[string]any, bool) {
	if !strings.HasPrefix(trimmedLine, "{") {
		return nil, false
	}
	var jsonEntry map[string]any
	if err := json.Unmarshal([]byte(trimmedLine), &jsonEntry); err != nil {
		if verbose {
			fmt.Fprintf(os.Stderr, "Skipping invalid JSON line: %s\n", trimmedLine)
		}
		return nil, false
	}
	return jsonEntry, true
}

func (e *ClaudeEngine) extractClaudeMetricsFromEntries(logEntries []map[string]any, verbose bool) LogMetrics {
	var metrics LogMetrics
	toolCallMap := make(map[string]*ToolCallInfo) // Track tool calls across entries
	toolIDs := make(map[string]string)
	sessions := make(map[string]*LogMetrics)
	var currentSequence []string // Track tool sequence within current context

	for _, entry := range logEntries {
		sessionID, _ := typeutil.LookupString(entry, "session_id")
		parentID, _ := typeutil.LookupString(entry, "parent_tool_use_id")
		context := sessionID + "\x00" + parentID + "\x00"
		switch e.claudeEntryType(entry) {
		case "result":
			if parentID != "" {
				continue
			}
			if sessions[sessionID] == nil {
				sessions[sessionID] = &LogMetrics{}
			}
			e.applyClaudeResultEntry(sessions[sessionID], entry, verbose)
		case "assistant":
			currentSequence = append(currentSequence, e.claudeAssistantSequence(entry, toolCallMap, toolIDs, context)...)
		case "user":
			e.claudeUserToolResults(entry, toolCallMap, toolIDs, context)
		}
	}
	for _, snapshot := range sessions {
		metrics.TokenUsage += snapshot.TokenUsage
		metrics.EstimatedCost += snapshot.EstimatedCost
		metrics.Turns += snapshot.Turns
	}

	FinalizeToolCallsAndSequence(&metrics, toolCallMap, currentSequence)
	claudeLogsLog.Printf("Parsed %d log entries: tokens=%d, cost=$%.4f, turns=%d, tool_types=%d",
		len(logEntries), metrics.TokenUsage, metrics.EstimatedCost, metrics.Turns, len(metrics.ToolCalls))
	if verbose && len(metrics.ToolSequences) > 0 {
		totalTools := 0
		for _, seq := range metrics.ToolSequences {
			totalTools += len(seq)
		}
		fmt.Fprintf(os.Stderr, "Claude parser extracted %d tool sequences with %d total tool calls\n",
			len(metrics.ToolSequences), totalTools)
	}
	return metrics
}

func (e *ClaudeEngine) claudeEntryType(entry map[string]any) string {
	typeStr, _ := typeutil.LookupString(entry, "type")
	return typeStr
}

func hasClaudeResultEntry(entries []map[string]any) bool {
	for _, entry := range entries {
		if entry["type"] == "result" {
			return true
		}
	}
	return false
}

func (e *ClaudeEngine) applyClaudeResultEntry(metrics *LogMetrics, entry map[string]any, verbose bool) {
	if totalCost, exists := entry["total_cost_usd"]; exists {
		if cost := typeutil.ConvertToFloat(totalCost); cost > 0 {
			metrics.EstimatedCost = cost
		}
	}
	if usage, exists := entry["usage"]; exists {
		if usageMap, ok := usage.(map[string]any); ok {
			inputTokens := typeutil.ConvertToInt(usageMap["input_tokens"])
			outputTokens := typeutil.ConvertToInt(usageMap["output_tokens"])
			cacheCreationTokens := typeutil.ConvertToInt(usageMap["cache_creation_input_tokens"])
			cacheReadTokens := typeutil.ConvertToInt(usageMap["cache_read_input_tokens"])
			if totalTokens := inputTokens + outputTokens + cacheCreationTokens + cacheReadTokens; totalTokens > 0 {
				metrics.TokenUsage = totalTokens
			}
		}
	}
	if numTurns, exists := entry["num_turns"]; exists {
		if turns := typeutil.ConvertToInt(numTurns); turns > 0 {
			metrics.Turns = turns
		}
	}
	if verbose {
		fmt.Fprintf(os.Stderr, "Extracted from Claude result payload: tokens=%d, cost=%.4f, turns=%d\n",
			metrics.TokenUsage, metrics.EstimatedCost, metrics.Turns)
	}
}

func (e *ClaudeEngine) claudeAssistantSequence(entry map[string]any, toolCallMap map[string]*ToolCallInfo, toolIDs map[string]string, context string) []string {
	messageMap, ok := entry["message"].(map[string]any)
	if !ok {
		return nil
	}
	contentArray, ok := messageMap["content"].([]any)
	if !ok {
		return nil
	}
	return e.parseToolCallsWithSequence(contentArray, toolCallMap, toolIDs, context)
}

func (e *ClaudeEngine) claudeUserToolResults(entry map[string]any, toolCallMap map[string]*ToolCallInfo, toolIDs map[string]string, context string) {
	messageMap, ok := entry["message"].(map[string]any)
	if !ok {
		return
	}
	contentArray, ok := messageMap["content"].([]any)
	if !ok {
		return
	}
	e.parseToolCallsWithSequence(contentArray, toolCallMap, toolIDs, context)
}

// parseToolCallsWithSequence extracts tool call information from Claude log content array and returns sequence
func (e *ClaudeEngine) parseToolCallsWithSequence(contentArray []any, toolCallMap map[string]*ToolCallInfo, toolIDs map[string]string, context string) []string {
	var sequence []string

	for _, contentItem := range contentArray {
		contentMap, ok := contentItem.(map[string]any)
		if !ok {
			continue
		}

		typeStr, ok := typeutil.LookupString(contentMap, "type")
		if !ok {
			continue
		}

		switch typeStr {
		case "tool_use":
			id, _ := typeutil.LookupString(contentMap, "id")
			if id != "" && toolIDs[context+id] != "" {
				continue
			}
			if prettifiedName, ok := e.recordClaudeToolUse(contentMap, toolCallMap); ok {
				sequence = append(sequence, prettifiedName)
				if id != "" {
					toolIDs[context+id] = prettifiedName
				}
			}
		case "tool_result":
			e.recordClaudeToolResult(contentMap, toolCallMap, toolIDs, context)
		}
	}

	return sequence
}

func (e *ClaudeEngine) recordClaudeToolUse(contentMap map[string]any, toolCallMap map[string]*ToolCallInfo) (string, bool) {
	nameStr, ok := typeutil.LookupString(contentMap, "name")
	if !ok {
		return "", false
	}
	prettifiedName := PrettifyToolName(nameStr)
	if nameStr == "Bash" {
		if commandStr, ok := typeutil.LookupStringPath(contentMap, "input", "command"); ok {
			prettifiedName = "bash_" + ShortenCommand(commandStr)
		}
	}
	inputSize := 0
	if input, exists := contentMap["input"]; exists {
		inputSize = e.estimateInputSize(input)
	}
	if toolInfo, exists := toolCallMap[prettifiedName]; exists {
		toolInfo.CallCount++
		if inputSize > toolInfo.MaxInputSize {
			toolInfo.MaxInputSize = inputSize
		}
	} else {
		toolCallMap[prettifiedName] = &ToolCallInfo{
			Name:          prettifiedName,
			CallCount:     1,
			MaxInputSize:  inputSize,
			MaxOutputSize: 0,
			MaxDuration:   0,
		}
	}
	return prettifiedName, true
}

func (e *ClaudeEngine) recordClaudeToolResult(contentMap map[string]any, toolCallMap map[string]*ToolCallInfo, toolIDs map[string]string, context string) {
	id, ok := typeutil.LookupString(contentMap, "tool_use_id")
	if !ok {
		return
	}
	toolInfo := toolCallMap[toolIDs[context+id]]
	if toolInfo == nil {
		return
	}
	outputSize := e.estimateInputSize(contentMap["content"])
	if contentStr, ok := typeutil.LookupString(contentMap, "content"); ok {
		outputSize = len(contentStr) / 4
	}
	if outputSize > toolInfo.MaxOutputSize {
		toolInfo.MaxOutputSize = outputSize
	}
}

// estimateInputSize estimates the input size in tokens from a tool input object
func (e *ClaudeEngine) estimateInputSize(input any) int {
	// Convert input to JSON string to get approximate size
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return 0
	}
	// Estimate token count (rough approximation: 1 token = ~4 characters)
	return len(inputJSON) / 4
}
