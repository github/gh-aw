package workflow

import (
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"strings"
	"time"
)

type codexJSONLogMetrics struct {
	metrics   LogMetrics
	hasTokens bool
	hasTurns  bool
}

type codexJSONMetricsState struct {
	engine         *CodexEngine
	parsed         codexJSONLogMetrics
	tools          map[string]*ToolCallInfo
	items          map[string]string
	completedTurns map[string]struct{}
	usage          map[string]any
	sequence       []string
	scope          int
	turns          int
}

// parseCodexJSONLMetrics accepts native --json records and canonical session
// snapshots. Result projections replace accounting fields rather than adding a
// second copy of the native turn usage.
func (e *CodexEngine) parseCodexJSONLMetrics(content string) codexJSONLogMetrics {
	state := codexJSONMetricsState{
		engine: e, tools: make(map[string]*ToolCallInfo), items: make(map[string]string),
		completedTurns: make(map[string]struct{}), usage: make(map[string]any),
	}
	for index, record := range codexJSONRecords(content) {
		state.observeRecord(index, record)
	}
	state.parsed.metrics.Turns = state.turns
	FinalizeToolCallsAndSequence(&state.parsed.metrics, state.tools, state.sequence)
	return state.parsed
}

func (state *codexJSONMetricsState) observeRecord(index int, record map[string]any) {
	kind := codexJSONString(record["type"])
	switch kind {
	case "thread.started", "session.init":
		state.flushSequence()
		state.scope++
	case "turn.started":
		state.flushSequence()
	case "turn.completed":
		state.observeCompletedTurn(index, record)
	case "session.result", "result":
		state.observeSnapshot(kind, record)
	case "item.started", "item.updated", "item.completed":
		state.observeNativeTool(index, record)
	case "tool.execution_start", "tool.execution_complete":
		state.observeCanonicalTool(index, kind, record)
	}
}

func (state *codexJSONMetricsState) flushSequence() {
	if len(state.sequence) > 0 {
		state.parsed.metrics.ToolSequences = append(state.parsed.metrics.ToolSequences, state.sequence)
		state.sequence = nil
	}
}

func (state *codexJSONMetricsState) observeCompletedTurn(index int, record map[string]any) {
	key := codexJSONRecordKey(state.scope, "turn", record, index, "turn_id", "id")
	if _, completed := state.completedTurns[key]; completed {
		return
	}
	state.completedTurns[key] = struct{}{}
	state.turns++
	state.parsed.hasTurns = true
	state.applyUsage(codexJSONMap(record["usage"]), true)
}

func (state *codexJSONMetricsState) observeSnapshot(kind string, record map[string]any) {
	data := record
	turnField := "num_turns"
	if kind == "session.result" {
		data = codexJSONMap(record["data"])
		turnField = "numTurns"
	}
	if count, valid := codexJSONTokenCount(data[turnField]); valid {
		state.turns = count
		state.parsed.hasTurns = true
	}
	state.applyUsage(codexJSONMap(data["usage"]), false)
	if kind == "result" {
		state.parsed.metrics.EstimatedCost = ExtractJSONCost(data)
	} else if cost, ok := data["totalCostUsd"].(float64); ok && cost >= 0 {
		state.parsed.metrics.EstimatedCost = cost
	}
}

func (state *codexJSONMetricsState) applyUsage(report map[string]any, incremental bool) {
	if overflowed, ok := report["overflowed_tokens"].([]any); ok {
		for _, field := range overflowed {
			if name, ok := field.(string); ok {
				delete(state.usage, name)
			}
		}
	}
	for _, field := range []string{"input_tokens", "output_tokens", "total_tokens", "cache_read_input_tokens", "cache_creation_input_tokens"} {
		state.applyUsageField(report, field, incremental)
	}
	if includeCache, ok := report["input_tokens_include_cache"].(bool); ok {
		state.usage["input_tokens_include_cache"] = includeCache
	}
	if total, valid := codexJSONUsageTotal(state.usage); valid {
		state.parsed.metrics.TokenUsage = total
		state.parsed.hasTokens = true
	} else if len(state.usage) > 0 {
		state.parsed.metrics.TokenUsage = 0
	}
}

func (state *codexJSONMetricsState) applyUsageField(report map[string]any, field string, incremental bool) {
	value, present := report[field]
	if !present {
		aliases := map[string]string{
			"input_tokens": "inputTokens", "output_tokens": "outputTokens",
			"cache_read_input_tokens": "cacheReadInputTokens", "cache_creation_input_tokens": "cacheCreationInputTokens",
		}
		value = report[aliases[field]]
	}
	count, valid := codexJSONTokenCount(value)
	if !valid {
		return
	}
	if incremental {
		previous, _ := codexJSONTokenCount(state.usage[field])
		if count > math.MaxInt-previous || float64(count)+float64(previous) > 9007199254740991 {
			delete(state.usage, field)
			return
		}
		count += previous
	}
	state.usage[field] = count
}

func (state *codexJSONMetricsState) observeNativeTool(index int, record map[string]any) {
	item := codexJSONMap(record["item"])
	itemType := codexJSONString(item["type"])
	if itemType != "command_execution" && itemType != "mcp_tool_call" {
		return
	}
	key := codexJSONRecordKey(state.scope, itemType, item, index, "tool_call_id", "id")
	data, invocation := codexNativeToolData(item, itemType)
	state.observeTool(key, data, invocation)
}

func codexNativeToolData(item map[string]any, kind string) (map[string]any, bool) {
	data := map[string]any{"durationMs": item["duration_ms"]}
	invocation := false
	if kind == "command_execution" {
		data["toolName"] = "bash"
		if command, present := item["command"]; present {
			data["command"], data["input"] = command, command
			invocation = true
		}
		if output, present := item["aggregated_output"]; present {
			data["output"] = output
		}
	} else {
		data["toolName"], data["mcpServerName"] = item["tool"], item["server"]
		if input, present := item["arguments"]; present {
			data["input"] = input
			_, invocation = item["tool"]
		}
		if output, present := item["result"]; present {
			data["output"] = output
		}
	}
	return data, invocation
}

func (state *codexJSONMetricsState) observeCanonicalTool(index int, kind string, record map[string]any) {
	data := codexJSONMap(record["data"])
	key := codexJSONRecordKey(state.scope, "tool", data, index, "toolCallId")
	if _, present := data["command"]; !present {
		if input := codexJSONMap(data["input"]); input["command"] != nil {
			data = maps.Clone(data)
			data["command"] = input["command"]
		}
	}
	state.observeTool(key, data, kind == "tool.execution_start")
}

func (state *codexJSONMetricsState) observeTool(key string, data map[string]any, invocation bool) {
	name := state.items[key]
	if name == "" && invocation {
		name = codexJSONToolName(data)
		if name == "" {
			return
		}
		state.items[key] = name
		if state.tools[name] == nil {
			state.tools[name] = &ToolCallInfo{Name: name}
		}
		state.tools[name].CallCount++
		state.sequence = append(state.sequence, name)
	}
	if name != "" {
		state.updateToolObservation(state.tools[name], data)
	}
}

func codexJSONToolName(data map[string]any) string {
	tool := codexJSONString(data["toolName"])
	server := codexJSONString(data["mcpServerName"])
	if tool == "bash" {
		if command, ok := data["command"].(string); ok {
			return "bash_" + ShortenCommand(command)
		}
		return "bash"
	}
	if server != "" && tool != "" {
		return normalizeCodexToolName(server + "." + tool)
	}
	return normalizeCodexToolName(tool)
}

func (state *codexJSONMetricsState) updateToolObservation(info *ToolCallInfo, data map[string]any) {
	if input, present := data["input"]; present {
		info.MaxInputSize = max(info.MaxInputSize, codexJSONValueSize(input))
	}
	if output, present := data["output"]; present {
		size := 0
		if text, ok := output.(string); ok {
			size = len(text)
		} else if encoded, err := json.Marshal(output); err == nil {
			size = state.engine.extractOutputSizeFromJSON(string(encoded))
		} else {
			codexLogsLog.Printf("Could not encode Codex tool output: %v", err)
		}
		info.MaxOutputSize = max(info.MaxOutputSize, size)
	}
	if ms, ok := data["durationMs"].(float64); ok && !math.IsNaN(ms) && !math.IsInf(ms, 0) && ms >= 0 && ms < float64(math.MaxInt64)/float64(time.Millisecond) {
		info.MaxDuration = max(info.MaxDuration, time.Duration(ms*float64(time.Millisecond)))
	}
}

func codexJSONMap(value any) map[string]any {
	if object, ok := value.(map[string]any); ok {
		return object
	}
	return nil
}

func codexJSONString(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func codexJSONRecords(content string) []map[string]any {
	var records []map[string]any
	appendRecord := func(value any) {
		switch value := value.(type) {
		case map[string]any:
			records = append(records, value)
		case []any:
			for _, entry := range value {
				if record, ok := entry.(map[string]any); ok {
					records = append(records, record)
				}
			}
		}
	}
	var document any
	if json.Unmarshal([]byte(content), &document) == nil {
		appendRecord(document)
		return records
	}
	lines := strings.Split(content, "\n")
	skipThrough := -1
	for index, line := range lines {
		if index <= skipThrough {
			continue
		}
		if codexLegacyResultPattern.MatchString(codexLegacyPayload(line)) {
			if _, end, found := codexLegacyResultJSON(lines, index); found {
				skipThrough = end
			}
			continue
		}
		var value any
		if json.Unmarshal([]byte(line), &value) == nil {
			appendRecord(value)
		}
	}
	return records
}

func codexJSONRecordKey(scope int, kind string, record map[string]any, index int, fields ...string) string {
	for _, field := range fields {
		if value, present := record[field]; present && value != nil {
			encoded, err := json.Marshal(value)
			if err == nil {
				return fmt.Sprintf("%d:%s:id:%s", scope, kind, encoded)
			}
			codexLogsLog.Printf("Could not encode Codex record ID: %v", err)
		}
	}
	return fmt.Sprintf("%d:%s:observation:%d", scope, kind, index)
}

func codexJSONTokenCount(value any) (int, bool) {
	switch number := value.(type) {
	case int:
		return number, number >= 0 && float64(number) <= 9007199254740991
	case float64:
		if !math.IsNaN(number) && !math.IsInf(number, 0) && number >= 0 && number <= 9007199254740991 && number <= float64(math.MaxInt) && math.Trunc(number) == number {
			return int(number), true
		}
	}
	return 0, false
}

func codexJSONUsageTotal(usage map[string]any) (int, bool) {
	if total, valid := codexJSONTokenCount(usage["total_tokens"]); valid {
		return total, true
	}
	_, inputPresent := codexJSONTokenCount(usage["input_tokens"])
	_, outputPresent := codexJSONTokenCount(usage["output_tokens"])
	if !inputPresent && !outputPresent {
		return 0, false
	}
	total := 0
	present := false
	fields := []string{"input_tokens", "output_tokens"}
	if usage["input_tokens_include_cache"] == false {
		fields = append(fields, "cache_read_input_tokens", "cache_creation_input_tokens")
	}
	for _, field := range fields {
		if count, valid := codexJSONTokenCount(usage[field]); valid {
			if count > math.MaxInt-total {
				return 0, false
			}
			total += count
			present = true
		}
	}
	return total, present
}

func codexJSONValueSize(value any) int {
	if text, ok := value.(string); ok {
		return len(text)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		codexLogsLog.Printf("Could not encode Codex tool input: %v", err)
		return 0
	}
	return len(encoded)
}
