package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
)

func hasLegacyPiObservations(content []byte) (bool, error) {
	for line := range bytes.SplitSeq(content, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var event struct {
			Type string         `json:"type"`
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			return false, fmt.Errorf("failed to read published session observation: %w", err)
		}
		if sourceEngine, present := event.Data["sourceEngine"]; present && sourceEngine != "pi" {
			continue
		}
		switch event.Type {
		case "pi.message_snapshot", "pi.message_update", "pi.tool_execution_update", "pi.error",
			"pi.subagent_dispatch", "pi.subagent_event", "pi.subagent_result",
			"pi.agent_start", "pi.agent_settled", "pi.auto_retry_start", "pi.auto_retry_end",
			"pi.compaction_start", "pi.compaction_end", "pi.queue_update", "pi.entry_appended",
			"pi.thinking_level_changed", "pi.summarization_retry_scheduled",
			"pi.summarization_retry_attempt_start", "pi.summarization_retry_finished":
			return true, nil
		}
	}
	return false, nil
}
