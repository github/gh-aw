package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

func (models *subagentSessionModels) observePiDispatch(event sessionSubagentEvent, data sessionSubagentData) error {
	identity := models.piInvocationIdentity(event, data, true)
	agentName := firstNonEmptyModel(data.Agent, data.AgentName, "unknown")
	row := models.agents[identity]
	if row == nil {
		row = &SubagentModelRequest{InvocationCount: 1, IncompleteCount: 1}
		models.agents[identity] = row
	} else {
		row.IncompleteCount = max(row.IncompleteCount, 1)
	}
	row.AgentName = agentName
	row.RequestedModel = data.RequestedModel
	row.ResolvedModel = data.ResolvedModel
	agentUsage := models.agentUsage[identity]
	if agentUsage == nil {
		agentUsage = &AgentUsageBreakdown{AgentName: agentName, AgentType: "subagent"}
		models.agentUsage[identity] = agentUsage
	}
	agentUsage.AgentName = agentName
	agentUsage.AgentType = "subagent"
	agentUsage.SourceEngine = models.sourceEngine
	agentUsage.InstanceCount = 1
	agentUsage.IncompleteCount = max(agentUsage.IncompleteCount, 1)
	agentUsage.RequestedModels = appendUnique(agentUsage.RequestedModels, data.RequestedModel)
	agentUsage.ResolvedModels = appendUnique(agentUsage.ResolvedModels, data.ResolvedModel)
	agentUsage.Effort = data.ReasoningEffort
	models.found = true
	return nil
}

func (models *subagentSessionModels) observePiUsage(event sessionSubagentEvent, data sessionSubagentData) error {
	var payload piSubagentEventPayload
	if err := json.Unmarshal(data.Event, &payload); err != nil {
		return err
	}
	identity := models.piInvocationIdentity(event, data, false)
	agentName := firstNonEmptyModel(data.Agent, data.AgentName, "unknown")
	row := models.agents[identity]
	if row == nil {
		row = &SubagentModelRequest{AgentName: agentName, InvocationCount: 1, IncompleteCount: 1}
		models.agents[identity] = row
	}
	failed := payload.Message.StopReason == "error" || payload.Message.StopReason == "aborted" || payload.Message.ErrorMessage != ""
	if failed {
		row.Error = sanitizeSubagentError(firstNonEmptyModel(payload.Message.ErrorMessage, payload.Message.StopReason))
	}
	models.found = true
	usage := payload.Message.Usage
	if failed && usage.Input == 0 && usage.Output == 0 && usage.CacheRead == 0 && usage.CacheWrite == 0 {
		return nil
	}
	model := firstNonEmptyModel(payload.Message.ResponseModel, payload.Message.Model)
	if model == "" {
		return nil
	}
	byModel := models.actualModels[identity]
	if byModel == nil {
		byModel = make(map[string]SubagentModelActual)
		models.actualModels[identity] = byModel
	}
	actual := byModel[model]
	actual.Model = model
	actual.Requests++
	actual.InputTokens += payload.Message.Usage.Input
	actual.OutputTokens += payload.Message.Usage.Output
	actual.CacheReadTokens += payload.Message.Usage.CacheRead
	actual.CacheWriteTokens += payload.Message.Usage.CacheWrite
	byModel[model] = actual
	models.recordPiAgentUsage(identity, agentName, model, event, payload)
	counts := models.actualCounts[identity]
	if counts == nil {
		counts = make(map[string]int)
		models.actualCounts[identity] = counts
	}
	counts[model] = actual.Requests
	return nil
}

func (models *subagentSessionModels) recordPiAgentUsage(identity, agentName, model string, event sessionSubagentEvent, payload piSubagentEventPayload) {
	agentUsage := models.agentUsage[identity]
	if agentUsage == nil {
		agentUsage = &AgentUsageBreakdown{AgentName: agentName, AgentType: "subagent", InstanceCount: 1, IncompleteCount: 1}
		models.agentUsage[identity] = agentUsage
	}
	agentUsage.AgentName = firstNonEmptyModel(agentUsage.AgentName, agentName)
	agentUsage.AgentType = "subagent"
	agentUsage.SourceEngine = models.sourceEngine
	agentUsage.Requests++
	agentUsage.InputTokens += payload.Message.Usage.Input
	agentUsage.OutputTokens += payload.Message.Usage.Output
	agentUsage.CacheReadTokens += payload.Message.Usage.CacheRead
	agentUsage.CacheWriteTokens += payload.Message.Usage.CacheWrite
	agentUsage.Models = appendAgentModelUsage(agentUsage.Models, AgentModelUsage{
		Model: model, Requests: 1,
		TokenCoreMetrics: TokenCoreMetrics{
			InputTokens: payload.Message.Usage.Input, OutputTokens: payload.Message.Usage.Output,
			CacheReadTokens: payload.Message.Usage.CacheRead, CacheWriteTokens: payload.Message.Usage.CacheWrite,
		},
	})
	timestampEvent := event
	if len(timestampEvent.Timestamp) == 0 || bytes.Equal(timestampEvent.Timestamp, []byte("null")) {
		timestampEvent.Timestamp = payload.Message.Timestamp
	}
	timestamp, _ := subagentSessionStart(timestampEvent)
	agentUsage.requestUsages = append(agentUsage.requestUsages, agentRequestUsage{
		Model: model, Timestamp: timestamp,
		TokenCoreMetrics: TokenCoreMetrics{
			InputTokens: payload.Message.Usage.Input, OutputTokens: payload.Message.Usage.Output,
			CacheReadTokens: payload.Message.Usage.CacheRead, CacheWriteTokens: payload.Message.Usage.CacheWrite,
		},
	})
}

func (models *subagentSessionModels) observePiResult(event sessionSubagentEvent, data sessionSubagentData) error {
	identity := models.piInvocationIdentity(event, data, false)
	agentName := firstNonEmptyModel(data.Agent, data.AgentName, "unknown")
	row := models.agents[identity]
	if row == nil {
		row = &SubagentModelRequest{AgentName: agentName, InvocationCount: 1}
		models.agents[identity] = row
	}
	row.IncompleteCount = max(0, row.IncompleteCount-1)
	if data.Outcome == "failed" || strings.HasPrefix(data.Outcome, "SUBAGENT_") || len(data.Error) > 0 || data.ErrorMessage != "" {
		row.FailedCount = 1
		row.Error = sanitizeSubagentError(firstNonEmptyModel(subagentErrorMessage(data.Error), data.ErrorMessage))
		row.ErrorCode = firstNonEmptyModel(data.ErrorCode, func() string {
			if strings.HasPrefix(data.Outcome, "SUBAGENT_") {
				return data.Outcome
			}
			return ""
		}())
	} else {
		row.CompletedCount = 1
	}
	agentUsage := models.agentUsage[identity]
	if agentUsage == nil {
		agentUsage = &AgentUsageBreakdown{AgentName: agentName, AgentType: "subagent", InstanceCount: 1}
		models.agentUsage[identity] = agentUsage
	}
	agentUsage.AgentName = firstNonEmptyModel(agentUsage.AgentName, agentName)
	if row.FailedCount > 0 {
		agentUsage.FailedCount++
	} else {
		agentUsage.CompletedCount++
	}
	agentUsage.IncompleteCount = max(0, agentUsage.IncompleteCount-1)
	if models.legacyPiActive[data.Agent] == identity {
		delete(models.legacyPiActive, data.Agent)
	}
	return nil
}

func (models *subagentSessionModels) observePiToolEvent(event sessionSubagentEvent, data sessionSubagentData) error {
	if models.sourceEngine != "pi" {
		return nil
	}
	if models.legacyPiTools == nil {
		models.legacyPiTools = make(map[string]string)
	}
	if event.Type == "tool.execution_start" {
		if data.ToolName != "subagent" {
			return nil
		}
		var input struct {
			Agent string `json:"agent"`
		}
		if err := json.Unmarshal(data.Input, &input); err != nil {
			return err
		}
		if input.Agent != "" && data.ToolCallID != "" {
			models.legacyPiTools[data.ToolCallID] = input.Agent
		}
		return nil
	}
	agentName := models.legacyPiTools[data.ToolCallID]
	delete(models.legacyPiTools, data.ToolCallID)
	identity := models.legacyPiActive[agentName]
	if agentName == "" || identity == "" || data.Success == nil {
		return nil
	}
	agentData := sessionSubagentData{Agent: agentName}
	if *data.Success {
		agentData.Outcome = "completed"
	} else {
		agentData.Outcome = "failed"
		agentData.Error = data.Error
	}
	return models.observePiResult(event, agentData)
}

func (models *subagentSessionModels) piInvocationIdentity(event sessionSubagentEvent, data sessionSubagentData, dispatch bool) string {
	if identity := firstNonEmptyModel(data.InvocationID, event.AgentID); identity != "" {
		return identity
	}
	agent := firstNonEmptyModel(data.Agent, data.AgentName, "unknown")
	if models.legacyPiActive == nil {
		models.legacyPiActive = make(map[string]string)
	}
	if !dispatch {
		if identity := models.legacyPiActive[agent]; identity != "" {
			return identity
		}
	}
	if models.legacyPiCount == nil {
		models.legacyPiCount = make(map[string]int)
	}
	models.legacyPiCount[agent]++
	identity := fmt.Sprintf("legacy:%s:%d", agent, models.legacyPiCount[agent])
	models.legacyPiActive[agent] = identity
	return identity
}

func subagentErrorMessage(raw json.RawMessage) string {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	var message string
	if json.Unmarshal(raw, &message) == nil {
		return message
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return ""
	}
	for _, key := range []string{"message", "error_description", "detail", "error"} {
		if value := fields[key]; len(value) > 0 {
			if message := subagentErrorMessage(value); message != "" {
				return message
			}
		}
	}
	return ""
}

func sanitizeSubagentError(message string) string {
	message = strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\t' {
			return ' '
		}
		return r
	}, message)), " ")
	const maxLength = 300
	if len(message) > maxLength {
		message = message[:maxLength] + "…"
	}
	return message
}
