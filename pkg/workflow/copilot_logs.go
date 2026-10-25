package workflow

import (
	"encoding/json"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

const (
	// outputSampleMaxLines is the maximum number of lines to include in a tool output preview.
	outputSampleMaxLines = 3
	// outputSampleMaxLineLen is the maximum character length of each line in a tool output preview.
	outputSampleMaxLineLen = 120
)

// truncateOutputSample returns the first outputSampleMaxLines lines of output,
// each truncated to outputSampleMaxLineLen characters.
func truncateOutputSample(output string) string {
	lines := strings.SplitN(output, "\n", outputSampleMaxLines+1)
	if len(lines) > outputSampleMaxLines {
		lines = lines[:outputSampleMaxLines]
	}
	for i, line := range lines {
		if len(line) > outputSampleMaxLineLen {
			runes := []rune(line)
			if len(runes) > outputSampleMaxLineLen {
				lines[i] = string(runes[:outputSampleMaxLineLen]) + "…"
			}
		}
	}
	return strings.Join(lines, "\n")
}

// sanitizeJSONBlock extracts a clean JSON object from a string that may contain
// trailing non-JSON content (e.g. [INFO] log lines appended after the closing brace).
// Returns an empty string if no valid JSON object boundary is found.
func sanitizeJSONBlock(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return ""
	}
	open := strings.Index(trimmed, "{")
	if open < 0 {
		return ""
	}

	depth := 0
	inString := false
	escaped := false

	for i := open; i < len(trimmed); i++ {
		ch := trimmed[i]

		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}

		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return trimmed[open : i+1]
			}
			if depth < 0 {
				return ""
			}
		}
	}

	return ""
}

func timestampedLogRemainder(line string) (string, bool) {
	trimmed := strings.TrimLeft(line, " \t")
	ts, rest, ok := strings.Cut(trimmed, " ")
	if !ok {
		return "", false
	}
	if !strings.Contains(ts, "T") || !strings.Contains(ts, ":") {
		return "", false
	}
	return rest, true
}

func isTimestampedDebugOrInfoLine(line string) bool {
	rest, ok := timestampedLogRemainder(line)
	if !ok {
		return false
	}
	return strings.HasPrefix(rest, "[DEBUG]") || strings.HasPrefix(rest, "[INFO]")
}

func isTimestampedDebugLine(line string, marker string) bool {
	rest, ok := timestampedLogRemainder(line)
	if !ok {
		return false
	}
	return strings.HasPrefix(rest, marker)
}

var copilotLogsLog = logger.New("workflow:copilot_logs")

// SessionEntry represents a single entry in a Copilot session JSONL file
type SessionEntry struct {
	Type     string          `json:"type"`
	Subtype  string          `json:"subtype,omitempty"`
	Message  *SessionMessage `json:"message,omitempty"`
	Usage    *SessionUsage   `json:"usage,omitempty"`
	NumTurns int             `json:"num_turns,omitempty"`
	RawData  map[string]any  `json:"-"`
}

// SessionMessage represents the message field in session entries
type SessionMessage struct {
	Content []SessionContent `json:"content"`
}

// SessionContent represents content items in messages
type SessionContent struct {
	Type      string         `json:"type"`
	Text      string         `json:"text,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Content   string         `json:"content,omitempty"`
}

// SessionUsage represents token usage in a session result entry
type SessionUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// parseSessionJSONL attempts to parse the log content as JSONL session format
// Returns true if successful, false if the format is not recognized
func (e *CopilotEngine) parseSessionJSONL(logContent string, verbose bool) (LogMetrics, bool) {
	parser := newCopilotSessionJSONLParser(verbose)
	for line := range strings.SplitSeq(logContent, "\n") {
		parser.processLine(line)
	}
	return parser.finalize()
}

type copilotSessionJSONLParser struct {
	metrics               LogMetrics
	verbose               bool
	totalTokenUsage       int
	toolCallMap           map[string]*ToolCallInfo
	toolUseIDMap          map[string]string // maps tool_use ID → tool name for output-size correlation
	currentSequence       []string
	turns                 int
	assistantMessageCount int
	foundSessionEntry     bool
}

func newCopilotSessionJSONLParser(verbose bool) *copilotSessionJSONLParser {
	return &copilotSessionJSONLParser{
		verbose:      verbose,
		toolCallMap:  make(map[string]*ToolCallInfo),
		toolUseIDMap: make(map[string]string),
	}
}

func (p *copilotSessionJSONLParser) processLine(line string) {
	trimmedLine := strings.TrimSpace(line)
	if trimmedLine == "" || !strings.HasPrefix(trimmedLine, "{") {
		return
	}
	var entry SessionEntry
	if err := json.Unmarshal([]byte(trimmedLine), &entry); err != nil {
		return
	}
	p.foundSessionEntry = true
	switch entry.Type {
	case "system":
		if p.verbose {
			copilotLogsLog.Printf("Found system init entry")
		}
	case "assistant":
		p.handleAssistantEntry(entry)
	case "user":
		p.handleUserEntry(entry)
	case "result":
		p.handleResultEntry(entry)
	}
}

func (p *copilotSessionJSONLParser) handleAssistantEntry(entry SessionEntry) {
	p.assistantMessageCount++
	if entry.Message == nil {
		return
	}
	for _, content := range entry.Message.Content {
		if content.Type != "tool_use" {
			continue
		}
		p.currentSequence = append(p.currentSequence, content.Name)
		if content.ID != "" {
			p.toolUseIDMap[content.ID] = content.Name
		}
		inputSize := copilotSessionInputSize(content.Input)
		if toolInfo, exists := p.toolCallMap[content.Name]; exists {
			toolInfo.CallCount++
			if inputSize > toolInfo.MaxInputSize {
				toolInfo.MaxInputSize = inputSize
			}
		} else {
			p.toolCallMap[content.Name] = &ToolCallInfo{
				Name:          content.Name,
				CallCount:     1,
				MaxInputSize:  inputSize,
				MaxOutputSize: 0,
			}
		}
		if p.verbose {
			copilotLogsLog.Printf("Found tool call: %s with input size %d", content.Name, inputSize)
		}
	}
}

func copilotSessionInputSize(input map[string]any) int {
	if input == nil {
		return 0
	}
	inputJSON, _ := json.Marshal(input) //nolint:jsonmarshalignoredeerror // used only for len() size metric; failure yields len(nil)==0 which is acceptable
	return len(inputJSON)
}

func (p *copilotSessionJSONLParser) handleUserEntry(entry SessionEntry) {
	if entry.Message == nil {
		return
	}
	for _, content := range entry.Message.Content {
		if content.Type != "tool_result" || content.ToolUseID == "" {
			continue
		}
		toolName, ok := p.toolUseIDMap[content.ToolUseID]
		if !ok {
			continue
		}
		if content.Content != "" {
			outputSize := len(content.Content)
			if toolInfo, exists := p.toolCallMap[toolName]; exists {
				if outputSize > toolInfo.MaxOutputSize {
					toolInfo.MaxOutputSize = outputSize
					if p.verbose {
						copilotLogsLog.Printf("Updated %s MaxOutputSize to %d bytes", toolName, outputSize)
					}
				}
			}
		}
	}
}

func (p *copilotSessionJSONLParser) handleResultEntry(entry SessionEntry) {
	if entry.Usage == nil {
		return
	}
	p.totalTokenUsage = entry.Usage.InputTokens + entry.Usage.OutputTokens
	p.turns = entry.NumTurns
	if p.verbose {
		copilotLogsLog.Printf("Found result entry: input_tokens=%d, output_tokens=%d, num_turns=%d",
			entry.Usage.InputTokens, entry.Usage.OutputTokens, p.turns)
	}
}

func (p *copilotSessionJSONLParser) finalize() (LogMetrics, bool) {
	if p.turns == 0 && p.assistantMessageCount > 0 {
		p.turns = p.assistantMessageCount
		copilotLogsLog.Printf("num_turns not available in result entry, using assistant message count as turns: %d", p.turns)
	}
	if !p.foundSessionEntry {
		return p.metrics, false
	}
	copilotLogsLog.Printf("Session JSONL parsing complete: totalTokenUsage=%d, turns=%d, toolCalls=%d",
		p.totalTokenUsage, p.turns, len(p.toolCallMap))
	FinalizeToolMetrics(FinalizeToolMetricsOptions{
		Metrics:         &p.metrics,
		ToolCallMap:     p.toolCallMap,
		CurrentSequence: p.currentSequence,
		Turns:           p.turns,
		TokenUsage:      p.totalTokenUsage,
	})
	return p.metrics, true
}

// ParseLogMetrics implements engine-specific log parsing for Copilot CLI.
//
// Parsing Strategy:
// 1. First attempts to parse as JSONL session format (from ~/.copilot/session-state/*.jsonl)
// 2. Falls back to debug log format if JSONL parsing fails or finds no entries
//
// Token Counting Behavior:
// Copilot CLI makes multiple API calls during a workflow run (one per turn).
// Each API call returns a response with usage statistics including token counts.
// This function accumulates token counts from ALL API responses to get the total
// token usage for the entire workflow run.
//
// Example: If a run has 3 turns with token counts [1000, 1500, 800],
// the total token usage will be 3300 (sum of all turns).
//
// This matches the behavior of the JavaScript parser in parse_copilot_log.cjs.
//
// Wire request block parsing (wireApi=responses format):
// When the Copilot CLI uses the OpenAI Responses API wire format, each turn is
// preceded by a [DEBUG] Wire request: block containing the full conversation
// history, including function_call_output items for completed tool calls.
// These blocks are parsed to extract tool output sizes and a response preview.
func (e *CopilotEngine) ParseLogMetrics(logContent string, verbose bool) LogMetrics {
	// Try parsing as JSONL session format first
	if metrics, success := e.parseSessionJSONL(logContent, verbose); success {
		copilotLogsLog.Printf("Successfully parsed session JSONL format")
		return metrics
	}

	// Fall back to debug log format parsing
	copilotLogsLog.Printf("JSONL parsing failed or no entries found, falling back to debug log format")

	state := newCopilotDebugLogParseState(e, verbose)
	for line := range strings.SplitSeq(logContent, "\n") {
		state.processLine(line)
	}
	return state.finalize()
}

type copilotDebugLogParseState struct {
	engine           *CopilotEngine
	metrics          LogMetrics
	verbose          bool
	totalTokenUsage  int
	toolCallMap      map[string]*ToolCallInfo
	currentSequence  []string
	turns            int
	inDataBlock      bool
	currentJSONLines []string
	inWireBlock      bool
	currentWireLines []string
}

func newCopilotDebugLogParseState(engine *CopilotEngine, verbose bool) *copilotDebugLogParseState {
	return &copilotDebugLogParseState{
		engine:      engine,
		verbose:     verbose,
		toolCallMap: make(map[string]*ToolCallInfo),
	}
}

func (s *copilotDebugLogParseState) processLine(line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	if s.startDataBlock(line) || s.startWireBlock(line) {
		return
	}
	s.collectDataBlockLine(line)
	s.collectWireBlockLine(line)
	s.collectToolCallLine(line)
}

func (s *copilotDebugLogParseState) startDataBlock(line string) bool {
	if !isTimestampedDebugLine(line, "[DEBUG] data:") {
		return false
	}
	if s.inWireBlock {
		s.flushWireBlock()
	}
	s.inDataBlock = true
	s.currentJSONLines = nil
	s.turns++
	if len(s.currentSequence) > 0 {
		s.metrics.ToolSequences = append(s.metrics.ToolSequences, s.currentSequence)
		s.currentSequence = nil
	}
	return true
}

func (s *copilotDebugLogParseState) startWireBlock(line string) bool {
	if !isTimestampedDebugLine(line, "[DEBUG] Wire request:") {
		return false
	}
	if s.inDataBlock {
		s.flushDataBlock()
	}
	if s.inWireBlock {
		s.flushWireBlock()
	}
	s.inWireBlock = true
	s.currentWireLines = nil
	if idx := strings.Index(line, "{"); idx >= 0 {
		s.currentWireLines = append(s.currentWireLines, line[idx:])
	}
	return true
}

func (s *copilotDebugLogParseState) collectDataBlockLine(line string) {
	if !s.inDataBlock {
		return
	}
	if !strings.Contains(line, "[DEBUG]") {
		s.currentJSONLines = append(s.currentJSONLines, line)
		return
	}
	_, after, ok := strings.Cut(line, "[DEBUG]")
	if !ok {
		return
	}
	cleanLine := strings.TrimSpace(after)
	if strings.HasPrefix(cleanLine, "{") || strings.HasPrefix(cleanLine, "}") ||
		strings.HasPrefix(cleanLine, "[") || strings.HasPrefix(cleanLine, "]") ||
		strings.HasPrefix(cleanLine, "\"") {
		s.currentJSONLines = append(s.currentJSONLines, cleanLine)
		return
	}
	s.flushDataBlock()
}

func (s *copilotDebugLogParseState) collectWireBlockLine(line string) {
	if !s.inWireBlock {
		return
	}
	if isTimestampedDebugOrInfoLine(line) {
		s.flushWireBlock()
		return
	}
	s.currentWireLines = append(s.currentWireLines, line)
}

func (s *copilotDebugLogParseState) collectToolCallLine(line string) {
	if toolName := s.engine.parseCopilotToolCallsWithSequence(line, s.toolCallMap); toolName != "" {
		s.currentSequence = append(s.currentSequence, toolName)
	}
}

func (s *copilotDebugLogParseState) flushDataBlock() {
	if len(s.currentJSONLines) == 0 {
		s.inDataBlock = false
		return
	}
	jsonStr := strings.Join(s.currentJSONLines, "\n")
	copilotLogsLog.Printf("Parsing JSON block with %d lines (%d bytes)", len(s.currentJSONLines), len(jsonStr))
	jsonMetrics := ExtractJSONMetrics(jsonStr, s.verbose)
	if jsonMetrics.TokenUsage > 0 {
		copilotLogsLog.Printf("Extracted %d tokens from JSON block", jsonMetrics.TokenUsage)
		s.totalTokenUsage += jsonMetrics.TokenUsage
	} else {
		copilotLogsLog.Printf("No tokens extracted from JSON block (possible format issue)")
	}
	if jsonMetrics.EstimatedCost > 0 {
		s.metrics.EstimatedCost += jsonMetrics.EstimatedCost
	}
	s.engine.extractToolCallSizes(jsonStr, s.toolCallMap, s.verbose)
	s.inDataBlock = false
	s.currentJSONLines = nil
}

func (s *copilotDebugLogParseState) flushWireBlock() {
	if len(s.currentWireLines) > 0 {
		s.engine.extractWireRequestOutputs(strings.Join(s.currentWireLines, "\n"), s.toolCallMap, s.verbose)
	}
	s.inWireBlock = false
	s.currentWireLines = nil
}

func (s *copilotDebugLogParseState) finalize() LogMetrics {
	if s.inDataBlock {
		s.flushDataBlock()
	}
	if s.inWireBlock {
		s.flushWireBlock()
	}
	copilotLogsLog.Printf("Finalized metrics: totalTokenUsage=%d, turns=%d, toolCalls=%d", s.totalTokenUsage, s.turns, len(s.toolCallMap))
	FinalizeToolMetrics(FinalizeToolMetricsOptions{
		Metrics:         &s.metrics,
		ToolCallMap:     s.toolCallMap,
		CurrentSequence: s.currentSequence,
		Turns:           s.turns,
		TokenUsage:      s.totalTokenUsage,
	})
	return s.metrics
}

// extractToolCallSizes extracts tool call input sizes from Copilot JSON responses.
// It sanitizes the JSON block first to handle trailing non-JSON log lines (e.g.
// [INFO] lines that are appended after the closing brace in the wireApi=responses format).
func (e *CopilotEngine) extractToolCallSizes(jsonStr string, toolCallMap map[string]*ToolCallInfo, verbose bool) {
	clean := sanitizeJSONBlock(jsonStr)
	if clean == "" {
		if verbose {
			copilotLogsLog.Printf("No valid JSON object found for tool size extraction")
		}
		return
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(clean), &data); err != nil {
		if verbose {
			copilotLogsLog.Printf("Failed to parse JSON for tool size extraction: %v", err)
		}
		return
	}

	// Look for tool_calls in the choices array (Copilot/OpenAI format)
	if choices, ok := data["choices"].([]any); ok {
		for _, choice := range choices {
			if choiceMap, ok := choice.(map[string]any); ok {
				if message, ok := choiceMap["message"].(map[string]any); ok {
					if toolCalls, ok := message["tool_calls"].([]any); ok {
						e.processToolCalls(toolCalls, toolCallMap, verbose)
					}
				}
			}
		}
	}

	// Also check for tool_calls directly in the message (alternative format)
	if message, ok := data["message"].(map[string]any); ok {
		if toolCalls, ok := message["tool_calls"].([]any); ok {
			e.processToolCalls(toolCalls, toolCallMap, verbose)
		}
	}
}

// processToolCalls processes tool_calls array and updates tool call map with sizes
func (e *CopilotEngine) processToolCalls(toolCalls []any, toolCallMap map[string]*ToolCallInfo, verbose bool) {
	for _, toolCall := range toolCalls {
		if tcMap, ok := toolCall.(map[string]any); ok {
			// Extract function information
			if function, ok := tcMap["function"].(map[string]any); ok {
				if toolName, ok := function["name"].(string); ok {
					// Calculate input size from arguments (if present)
					inputSize := 0
					if arguments, ok := function["arguments"].(string); ok {
						inputSize = len(arguments)
					}

					// Initialize or update tool call info
					if toolInfo, exists := toolCallMap[toolName]; exists {
						// If a stub entry was first created from function_call_output in a
						// Wire request, it already carries evidence of one invocation.
						// Avoid double-counting when the corresponding tool_call arrives later.
						if !isWireOutputStub(toolInfo) {
							toolInfo.CallCount++
						}
						// Update max input size if this call is larger
						if inputSize > toolInfo.MaxInputSize {
							toolInfo.MaxInputSize = inputSize
							if verbose {
								copilotLogsLog.Printf("Updated %s MaxInputSize to %d bytes", toolName, inputSize)
							}
						}
					} else {
						toolCallMap[toolName] = &ToolCallInfo{
							Name:         toolName,
							CallCount:    1,
							MaxInputSize: inputSize,
						}
						if verbose {
							copilotLogsLog.Printf("Created tool info for %s with MaxInputSize=%d bytes", toolName, inputSize)
						}
					}
				}
			}
		}
	}
}

// isWireOutputStub returns true when a ToolCallInfo entry was inferred from a
// function_call_output item before we observed the corresponding tool_call input.
// In this state, CallCount is already seeded to 1 based on output evidence.
func isWireOutputStub(toolInfo *ToolCallInfo) bool {
	return toolInfo.CallCount == 1 && toolInfo.MaxInputSize == 0 && toolInfo.MaxOutputSize > 0
}

// extractWireRequestOutputs parses a [DEBUG] Wire request: JSON block and updates
// MaxOutputSize and OutputSample for each tool that has a function_call_output entry.
//
// The wireApi=responses format includes the full conversation history in each request's
// "input" array. Completed tool calls appear as consecutive function_call / function_call_output
// pairs, letting us extract both the tool name (from function_call.name) and the tool
// response (from function_call_output.output) in a single pass.
func (e *CopilotEngine) extractWireRequestOutputs(jsonStr string, toolCallMap map[string]*ToolCallInfo, verbose bool) {
	inputs, ok := parseWireRequestInputs(jsonStr, verbose)
	if !ok {
		return
	}
	applyWireRequestOutputs(inputs, buildWireRequestCallIDMap(inputs), toolCallMap, verbose)
}

func parseWireRequestInputs(jsonStr string, verbose bool) ([]any, bool) {
	clean := sanitizeJSONBlock(jsonStr)
	if clean == "" {
		return nil, false
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(clean), &data); err != nil {
		if verbose {
			copilotLogsLog.Printf("Failed to parse Wire request JSON: %v", err)
		}
		return nil, false
	}
	inputs, ok := data["input"].([]any)
	return inputs, ok
}

func buildWireRequestCallIDMap(inputs []any) map[string]string {
	callIDToTool := make(map[string]string, len(inputs))
	for _, item := range inputs {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if typ, ok := itemMap["type"].(string); ok && typ == "function_call" {
			callID, callIDOK := itemMap["call_id"].(string)
			name, nameOK := itemMap["name"].(string)
			if callIDOK && nameOK && callID != "" && name != "" {
				callIDToTool[callID] = name
			}
		}
	}
	return callIDToTool
}

func applyWireRequestOutputs(inputs []any, callIDToTool map[string]string, toolCallMap map[string]*ToolCallInfo, verbose bool) {
	for _, item := range inputs {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if typ, ok := itemMap["type"].(string); !ok || typ != "function_call_output" {
			continue
		}
		callID, callIDOK := itemMap["call_id"].(string)
		output, outputOK := itemMap["output"].(string)
		toolName := callIDToTool[callID]
		if !callIDOK || !outputOK || callID == "" || output == "" || toolName == "" {
			continue
		}
		recordWireRequestOutput(toolCallMap, toolName, output, verbose)
	}
}

func recordWireRequestOutput(toolCallMap map[string]*ToolCallInfo, toolName, output string, verbose bool) {
	outputSize := len(output)
	if toolInfo, exists := toolCallMap[toolName]; exists {
		if outputSize > toolInfo.MaxOutputSize {
			toolInfo.MaxOutputSize = outputSize
			toolInfo.OutputSample = truncateOutputSample(output)
			if verbose {
				copilotLogsLog.Printf("Updated %s MaxOutputSize to %d bytes with sample", toolName, outputSize)
			}
		}
		return
	}
	toolCallMap[toolName] = &ToolCallInfo{
		Name:          toolName,
		CallCount:     1,
		MaxOutputSize: outputSize,
		OutputSample:  truncateOutputSample(output),
	}
	if verbose {
		copilotLogsLog.Printf("Created stub entry for %s from wire request output (%d bytes)", toolName, outputSize)
	}
}

// parseCopilotToolCallsWithSequence extracts tool call information from Copilot CLI log lines and returns tool name.
// It also updates toolCallMap with the tool execution count for statistics tracking.
func (e *CopilotEngine) parseCopilotToolCallsWithSequence(line string, toolCallMap map[string]*ToolCallInfo) string {
	// Look for "Executing tool:" pattern in Copilot logs
	if strings.Contains(line, "Executing tool:") {
		// Extract tool name from "Executing tool: <name>" format
		parts := strings.Split(line, "Executing tool:")
		if len(parts) > 1 {
			toolName := strings.TrimSpace(parts[1])
			if toolName == "" {
				return ""
			}
			// Update toolCallMap: this captures tool calls from execution log lines.
			// This is the primary source of tool call data in the Copilot CLI debug log
			// format, since JSON response blocks often have empty tool_calls arrays.
			if toolInfo, exists := toolCallMap[toolName]; exists {
				toolInfo.CallCount++
			} else {
				toolCallMap[toolName] = &ToolCallInfo{
					Name:      toolName,
					CallCount: 1,
				}
			}
			return toolName
		}
	}

	return ""
}

// GetLogParserScriptId returns the JavaScript script name for parsing Copilot logs
func (e *CopilotEngine) GetLogParserScriptId() string {
	return "parse_copilot_log"
}

// GetErrorDetectionScriptId returns the JavaScript script name for detecting agent errors
// from the agent stdio log. The script runs on the host runner after the AWF container exits,
// allowing it to write GITHUB_OUTPUT values that are not accessible inside the container.
func (e *CopilotEngine) GetErrorDetectionScriptId() string {
	return "detect_agent_errors"
}

// GetLogFileForParsing returns the log directory for Copilot CLI logs
// Copilot writes detailed debug logs to /tmp/gh-aw/sandbox/agent/logs/
func (e *CopilotEngine) GetLogFileForParsing() string {
	return constants.TmpSandboxAgentLogsDir
}
