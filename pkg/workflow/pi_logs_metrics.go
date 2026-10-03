package workflow

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

type piMetricState struct {
	tokens, turns, turnIndex, compactions int
	cost                                  float64
	messageTokens                         map[string]int
	messageCosts                          map[string]float64
	tools                                 map[string]int
	seenTools                             map[string]struct{}
}

func newPiMetricState() *piMetricState {
	return &piMetricState{
		messageTokens: make(map[string]int), messageCosts: make(map[string]float64),
		tools: make(map[string]int), seenTools: make(map[string]struct{}),
	}
}

func (s *piMetricState) consume(event piLogEvent) {
	switch event.Type {
	case "turn_start":
		s.turnIndex++
	case "message_end", "turn_end":
		s.recordMessage(event.Message)
	case "agent_end":
		for _, message := range event.Messages {
			s.recordMessage(message)
		}
	case "compaction_end":
		s.recordUsage(fmt.Sprintf("compaction:%d", s.compactions), piUsageMap(event.Result["usage"]))
		s.compactions++
	case "entry_appended":
		if event.Entry["type"] == "usage" {
			s.recordUsage(fmt.Sprintf("usage:%v", event.Entry["id"]), piUsageMap(event.Entry["usage"]))
		}
	case "tool_execution_start":
		if _, seen := s.seenTools[event.NativeToolID]; !seen && event.NativeToolName != "" {
			s.tools[event.NativeToolName]++
			s.seenTools[event.NativeToolID] = struct{}{}
		}
	case "assistant":
		if !event.Delta && strings.TrimSpace(event.Content) != "" {
			s.turns++
		}
	case "tool_use":
		if event.ToolName != "" {
			s.tools[event.ToolName]++
		}
	case "result":
		s.tokens += piUsageTokens(event.Stats)
	}
}

func (s *piMetricState) recordMessage(message map[string]any) {
	role, ok := message["role"].(string)
	if !ok || (role != "assistant" && role != "toolResult") {
		return
	}
	key := piMessageMetricKey(message, s.turnIndex)
	if _, seen := s.messageTokens[key]; !seen && role == "assistant" {
		s.turns++
	}
	s.recordUsage(key, piUsageMap(message["usage"]))
}

func (s *piMetricState) recordUsage(key string, usage map[string]any) {
	tokens := piUsageTokens(usage)
	s.tokens += tokens - s.messageTokens[key]
	s.messageTokens[key] = tokens
	cost := piUsageMap(usage["cost"])
	if total, ok := cost["total"].(float64); ok && total >= 0 && !math.IsInf(total, 0) {
		s.cost += total - s.messageCosts[key]
		s.messageCosts[key] = total
	}
}

func piUsageMap(raw any) map[string]any {
	value, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	return value
}

func piUsageTokens(usage map[string]any) int {
	for _, field := range []string{"totalTokens", "total_tokens"} {
		if total, ok := piTokenInteger(usage[field]); ok {
			return total
		}
	}
	total := 0
	for _, fields := range [][]string{{"input", "input_tokens"}, {"output", "output_tokens"}, {"cacheRead", "cache_read_input_tokens"}, {"cacheWrite", "cache_creation_input_tokens"}} {
		for _, field := range fields {
			if value, ok := piTokenInteger(usage[field]); ok {
				total += value
				break
			}
		}
	}
	return total
}

func piTokenInteger(raw any) (int, bool) {
	value, ok := raw.(float64)
	if !ok || value < 0 || value >= float64(math.MaxInt) || value != math.Trunc(value) {
		return 0, false
	}
	return int(value), true
}

func piMessageMetricKey(message map[string]any, turnIndex int) string {
	if message["role"] == "toolResult" {
		return fmt.Sprintf("tool:%v", message["toolCallId"])
	}
	for _, field := range []string{"id", "responseId", "timestamp"} {
		if value, ok := message[field]; ok {
			return fmt.Sprintf("assistant:%s:%v", field, value)
		}
	}
	content, err := json.Marshal(message["content"])
	if err != nil {
		panic(fmt.Sprintf("BUG: cannot encode decoded Pi message: %v", err))
	}
	return fmt.Sprintf("assistant:turn:%d:%s", turnIndex, content)
}
