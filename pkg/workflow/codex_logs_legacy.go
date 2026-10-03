package workflow

import (
	"encoding/json"
	"strings"
	"time"
)

type codexLegacyMetricsState struct {
	metrics         LogMetrics
	tokens          int
	tokenSnapshot   *int
	usageOverflowed bool
	turns           int
	inThinking      bool
	tools           map[string]*ToolCallInfo
	sequence        []string
	lastToolName    string
	execTools       map[string]string
}

func (e *CodexEngine) parseCodexLegacyMetrics(content string) codexLegacyMetricsState {
	state := codexLegacyMetricsState{
		tools: make(map[string]*ToolCallInfo), execTools: make(map[string]string),
	}
	if json.Valid([]byte(content)) {
		return state
	}
	lines := strings.Split(content, "\n")
	skipThrough := -1
	for index, line := range lines {
		if index <= skipThrough || strings.TrimSpace(line) == "" || json.Valid([]byte(line)) {
			continue
		}
		state.observeThinking(line)
		state.observeInvocation(e, line)
		skipThrough = state.observeResult(e, line, lines, index)
		state.observeTokens(line, codexLogLine(lines, index+1))
	}
	return state
}

func (state *codexLegacyMetricsState) observeThinking(line string) {
	trimmed := strings.TrimSpace(line)
	if strings.Contains(line, "] thinking") || trimmed == "thinking" {
		if !state.inThinking {
			state.turns++
			state.inThinking = true
			if len(state.sequence) > 0 {
				state.metrics.ToolSequences = append(state.metrics.ToolSequences, state.sequence)
				state.sequence = nil
			}
		}
	} else if strings.Contains(line, "] tool") || strings.Contains(line, "] exec") || strings.Contains(line, "] codex") ||
		strings.HasPrefix(trimmed, "tool ") || strings.HasPrefix(trimmed, "exec ") {
		state.inThinking = false
	}
}

func (state *codexLegacyMetricsState) observeInvocation(engine *CodexEngine, line string) {
	if name := engine.parseCodexToolCallsWithSequence(line, state.tools); name != "" {
		state.sequence = append(state.sequence, name)
		state.lastToolName = name
		if command := codexLegacyExecCommand(line); command != "" {
			state.execTools[command] = name
		}
	}
}

func (state *codexLegacyMetricsState) observeResult(engine *CodexEngine, line string, lines []string, index int) int {
	match := codexLegacyResultPattern.FindStringSubmatch(codexLegacyPayload(line))
	if len(match) == 0 {
		return index
	}
	target := codexRegexpGroup(match, 1)
	if info := state.tools[state.resultToolName(target)]; info != nil {
		if duration, err := time.ParseDuration(codexRegexpGroup(match, 3) + codexRegexpGroup(match, 4)); err == nil {
			info.MaxDuration = max(info.MaxDuration, duration)
		}
		size := engine.extractOutputSizeFromResult(line, lines, index)
		if _, commandResult := state.execTools[target]; commandResult && size == 0 {
			size = codexLegacyCommandOutputSize(lines, index)
		}
		info.MaxOutputSize = max(info.MaxOutputSize, size)
	}
	if _, end, found := codexLegacyResultJSON(lines, index); found {
		return end
	}
	return index
}

func (state *codexLegacyMetricsState) resultToolName(target string) string {
	if commandTool, exists := state.execTools[target]; exists {
		return commandTool
	}
	if name, _, found := strings.Cut(target, "("); found {
		return normalizeCodexToolName(name)
	}
	if target == "" {
		return state.lastToolName
	}
	return ""
}

func (state *codexLegacyMetricsState) observeTokens(line, nextLine string) {
	if count, found := extractCodexLegacyTokenCount(line); found {
		if codexLegacyTokensPattern.MatchString(codexLegacyPayload(line)) {
			if float64(state.tokens)+float64(count) > 9007199254740991 {
				state.tokens = 0
				state.usageOverflowed = true
			} else if !state.usageOverflowed {
				state.tokens += count
			}
		} else {
			state.tokenSnapshot = &count
		}
	} else if strings.EqualFold(codexLegacyPayload(line), "tokens used") {
		if count, found := parseCodexGroupedCount(strings.TrimSpace(nextLine)); found {
			state.tokenSnapshot = &count
		}
	}
}

func (state *codexLegacyMetricsState) mergeStructured(structured codexJSONLogMetrics) {
	// Inline reports are incremental; pretty/TokenCount totals are snapshots.
	if state.tokenSnapshot != nil {
		state.tokens = *state.tokenSnapshot
	}
	if structured.hasTokens {
		state.tokens = structured.metrics.TokenUsage
	}
	if structured.hasTurns {
		state.turns = structured.metrics.Turns
	}
	state.metrics.EstimatedCost = structured.metrics.EstimatedCost
	state.metrics.ToolSequences = append(state.metrics.ToolSequences, structured.metrics.ToolSequences...)
	for _, tool := range structured.metrics.ToolCalls {
		if previous, exists := state.tools[tool.Name]; exists {
			previous.CallCount += tool.CallCount
			previous.MaxInputSize = max(previous.MaxInputSize, tool.MaxInputSize)
			previous.MaxOutputSize = max(previous.MaxOutputSize, tool.MaxOutputSize)
			previous.MaxDuration = max(previous.MaxDuration, tool.MaxDuration)
		} else {
			state.tools[tool.Name] = &tool
		}
	}
}

func codexLogLine(lines []string, index int) string {
	if index >= 0 && index < len(lines) {
		return lines[index]
	}
	return ""
}
