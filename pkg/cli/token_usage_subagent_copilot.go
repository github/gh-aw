package cli

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
)

func (models *subagentSessionModels) observe(event sessionSubagentEvent) error {
	switch event.Type {
	case "session.start", "session.init", "subagent.started", "subagent.configured", "subagent.request", "subagent.completed", "subagent.failed", "session.model_change", "session.result", "session.shutdown", "pi.subagent_dispatch", "pi.subagent_event", "pi.subagent_result", "tool.execution_start", "tool.execution_complete":
	default:
		return nil
	}
	var data sessionSubagentData
	if err := json.Unmarshal(event.Data, &data); err != nil {
		return err
	}
	if event.Type == "session.init" && (data.SourceEngine == "copilot" || data.SourceEngine == "pi") {
		models.sourceEngine = data.SourceEngine
		if data.SessionID == "" && event.AgentID == "" {
			models.agents = make(map[string]*SubagentModelRequest)
			models.actualCounts = make(map[string]map[string]int)
			models.actualModels = make(map[string]map[string]SubagentModelActual)
			models.agentUsage = make(map[string]*AgentUsageBreakdown)
			models.legacyPiActive = make(map[string]string)
			models.legacyPiCount = make(map[string]int)
			models.legacyPiTools = make(map[string]string)
			models.starts = nil
			models.metricNames = nil
		}
		models.found = true
	}
	if (event.Type == "session.start" || event.Type == "session.init") && event.AgentID == "" && data.SessionID != "" {
		models.sessionID = data.SessionID
	}
	if event.Type == "session.start" {
		models.found = true
	}
	if err := models.observeMetrics(data); err != nil {
		return err
	}
	switch event.Type {
	case "pi.subagent_dispatch":
		return models.observePiDispatch(event, data)
	case "pi.subagent_event":
		return models.observePiUsage(event, data)
	case "pi.subagent_result":
		return models.observePiResult(event, data)
	case "tool.execution_start", "tool.execution_complete":
		return models.observePiToolEvent(event, data)
	}
	if strings.HasPrefix(event.Type, "subagent.") || (event.Type == "session.model_change" && event.AgentID != "") {
		if event.Type == "subagent.request" {
			return models.observeSubagentRequest(event, data)
		}
		return models.observeLifecycle(event, data)
	}
	return nil
}

func (models *subagentSessionModels) observeSubagentRequest(event sessionSubagentEvent, data sessionSubagentData) error {
	identity := firstNonEmptyModel(event.AgentID, data.InvocationID)
	if identity == "" {
		return errors.New("missing sub-agent invocation identity")
	}
	model := data.Model
	if model == "" {
		return nil
	}
	name := firstNonEmptyModel(data.AgentName, data.AgentDisplayName, "unknown")
	row := models.agents[identity]
	if row == nil {
		row = &SubagentModelRequest{AgentName: name, InvocationCount: 1, IncompleteCount: 1}
		models.agents[identity] = row
	}
	if row.AgentName == "" {
		row.AgentName = name
	}
	models.found = true
	usage, incomplete, err := subagentRequestTokenMetrics(data)
	if err != nil {
		return err
	}
	timestamp, err := subagentSessionStart(event)
	if err != nil {
		return err
	}
	models.recordSubagentRequest(identity, row.AgentName, model, usage, incomplete, timestamp)
	if data.AgentName != "" {
		models.agentUsage[identity].definitionName = data.AgentName
	}
	return nil
}

func subagentRequestTokenMetrics(data sessionSubagentData) (TokenCoreMetrics, bool, error) {
	usage := TokenCoreMetrics{}
	incomplete := false
	for _, metric := range []*int{data.InputTokens, data.OutputTokens, data.CacheReadTokens, data.CacheWriteTokens} {
		if metric == nil {
			incomplete = true
			continue
		}
		if *metric < 0 {
			return TokenCoreMetrics{}, false, errors.New("negative sub-agent token count")
		}
	}
	if data.InputTokens != nil {
		usage.InputTokens = *data.InputTokens
	}
	if data.OutputTokens != nil {
		usage.OutputTokens = *data.OutputTokens
	}
	if data.CacheReadTokens != nil {
		usage.CacheReadTokens = *data.CacheReadTokens
	}
	if data.CacheWriteTokens != nil {
		usage.CacheWriteTokens = *data.CacheWriteTokens
	}
	return usage, incomplete, nil
}

func (models *subagentSessionModels) recordSubagentRequest(
	identity, agentName, model string,
	usage TokenCoreMetrics,
	incomplete bool,
	timestamp time.Time,
) {
	byModel := models.actualModels[identity]
	if byModel == nil {
		byModel = make(map[string]SubagentModelActual)
		models.actualModels[identity] = byModel
	}
	actual := byModel[model]
	actual.Model = model
	actual.Requests++
	actual.InputTokens += usage.InputTokens
	actual.OutputTokens += usage.OutputTokens
	actual.CacheReadTokens += usage.CacheReadTokens
	actual.CacheWriteTokens += usage.CacheWriteTokens
	byModel[model] = actual
	if models.actualCounts[identity] == nil {
		models.actualCounts[identity] = make(map[string]int)
	}
	models.actualCounts[identity][model] = actual.Requests

	agentUsage := models.agentUsage[identity]
	if agentUsage == nil {
		agentUsage = &AgentUsageBreakdown{AgentName: agentName, AgentType: "subagent", InstanceCount: 1, IncompleteCount: 1}
		models.agentUsage[identity] = agentUsage
	}
	agentUsage.AgentName = agentName
	agentUsage.AgentType = "subagent"
	agentUsage.SourceEngine = models.sourceEngine
	agentUsage.InstanceCount = max(agentUsage.InstanceCount, 1)
	agentUsage.Requests++
	agentUsage.InputTokens += usage.InputTokens
	agentUsage.OutputTokens += usage.OutputTokens
	agentUsage.CacheReadTokens += usage.CacheReadTokens
	agentUsage.CacheWriteTokens += usage.CacheWriteTokens
	agentUsage.Models = appendAgentModelUsage(agentUsage.Models, AgentModelUsage{
		Model: model, Requests: 1, TokenCoreMetrics: usage,
	})
	agentUsage.requestUsages = append(agentUsage.requestUsages, agentRequestUsage{
		Model: model, Timestamp: timestamp, UsageIncomplete: incomplete, TokenCoreMetrics: usage,
	})
}

func (models *subagentSessionModels) observeMetrics(data sessionSubagentData) error {
	if data.AgentMetrics != nil {
		models.found = true
		models.actualCounts = make(map[string]map[string]int)
		models.actualModels = make(map[string]map[string]SubagentModelActual)
		models.metricNames = make(map[string]subagentMetricNames, len(data.AgentMetrics))
		models.resetAgentUsageMetrics()
	}
	for agentID, metric := range data.AgentMetrics {
		if err := models.observeAgentMetric(agentID, metric); err != nil {
			return err
		}
	}
	return nil
}

func (models *subagentSessionModels) observeAgentMetric(agentID string, metric sessionSubagentAgentMetric) error {
	name := firstNonEmptyModel(metric.AgentDisplayName, metric.AgentName)
	role := "subagent"
	if agentID == "main" {
		name, role = "main", "main"
	} else {
		if name == "" {
			name = agentID
		}
		row := models.agents[agentID]
		if row == nil {
			row = &SubagentModelRequest{AgentName: name}
			models.agents[agentID] = row
		} else if row.AgentName == "" {
			row.AgentName = name
		}
		if row.RequestedModel == "" {
			row.RequestedModel = agentMetricRequestedModel(metric)
		}
		models.metricNames[agentID] = subagentMetricNames{definition: metric.AgentName, displayName: metric.AgentDisplayName}
	}
	agentUsage := models.agentUsage[agentID]
	if agentUsage == nil {
		agentUsage = &AgentUsageBreakdown{}
		models.agentUsage[agentID] = agentUsage
	}
	if name == "" && models.agents[agentID] != nil {
		name = models.agents[agentID].AgentName
	}
	agentUsage.AgentName = name
	agentUsage.AgentType = role
	if role == "subagent" {
		agentUsage.observeIdentity(metric.AgentName, metric.AgentDisplayName)
	}
	agentUsage.SourceEngine = models.sourceEngine
	agentUsage.InstanceCount = max(agentUsage.InstanceCount, 1)
	agentUsage.AIC = float64(metric.TotalNanoAiu) / 1e9
	agentUsage.TotalApiDurationMs = metric.TotalApiDurationMs
	agentUsage.Requests, agentUsage.InputTokens, agentUsage.OutputTokens = 0, 0, 0
	agentUsage.CacheReadTokens, agentUsage.CacheWriteTokens, agentUsage.ReasoningTokens = 0, 0, 0
	agentUsage.Models = nil
	return models.addAgentMetricModelUsage(agentID, metric, agentUsage)
}

func agentMetricRequestedModel(metric sessionSubagentAgentMetric) string {
	models := make([]string, 0, len(metric.ModelMetrics))
	for model := range metric.ModelMetrics {
		models = append(models, model)
	}
	slices.Sort(models)
	for _, model := range models {
		if metric.ModelMetrics[model].Requests.Count != nil && *metric.ModelMetrics[model].Requests.Count > 0 {
			return model
		}
	}
	if len(models) > 0 {
		return models[0]
	}
	return ""
}

func (models *subagentSessionModels) addAgentMetricModelUsage(agentID string, metric sessionSubagentAgentMetric, agentUsage *AgentUsageBreakdown) error {
	counts := make(map[string]int)
	actuals := make(map[string]SubagentModelActual)
	for model, usage := range metric.ModelMetrics {
		requestCount := 0
		if usage.Requests.Count != nil {
			requestCount = *usage.Requests.Count
		}
		if requestCount < 0 {
			return errors.New("negative agent request count")
		}
		counts[model] = requestCount
		actual := SubagentModelActual{
			Model: model, Requests: requestCount,
			InputTokens: usage.Usage.InputTokens, OutputTokens: usage.Usage.OutputTokens,
			CacheReadTokens: usage.Usage.CacheReadTokens, CacheWriteTokens: usage.Usage.CacheWriteTokens,
			ReasoningTokens: usage.Usage.ReasoningTokens,
		}
		actual.AIC = float64(usage.TotalNanoAiu) / 1e9
		if agentID != "main" {
			actuals[model] = actual
		}
		agentUsage.Models = append(agentUsage.Models, AgentModelUsage{
			Model: model, Requests: requestCount, TokenCoreMetrics: actual.TokenCoreMetrics, AIC: actual.AIC,
		})
		accumulateAgentMetricTokenUsage(agentUsage, usage, requestCount)
	}
	if agentID != "main" {
		models.actualCounts[agentID] = counts
		models.actualModels[agentID] = actuals
	}
	models.agentUsage[agentID] = agentUsage
	return nil
}

func accumulateAgentMetricTokenUsage(agentUsage *AgentUsageBreakdown, usage sessionSubagentModelMetric, requests int) {
	agentUsage.Requests += requests
	agentUsage.InputTokens += usage.Usage.InputTokens
	agentUsage.OutputTokens += usage.Usage.OutputTokens
	agentUsage.CacheReadTokens += usage.Usage.CacheReadTokens
	agentUsage.CacheWriteTokens += usage.Usage.CacheWriteTokens
	agentUsage.ReasoningTokens += usage.Usage.ReasoningTokens
}

func (models *subagentSessionModels) resetAgentUsageMetrics() {
	reset := make(map[string]*AgentUsageBreakdown, len(models.agentUsage))
	for agentID, previous := range models.agentUsage {
		copy := *previous
		copy.Requests = 0
		copy.TokenCoreMetrics = TokenCoreMetrics{}
		copy.AIC = 0
		copy.TotalApiDurationMs = 0
		copy.Models = nil
		reset[agentID] = &copy
	}
	models.agentUsage = reset
}

func (models *subagentSessionModels) observeLifecycle(event sessionSubagentEvent, data sessionSubagentData) error {
	models.found = true
	identity := firstNonEmptyModel(event.AgentID, data.InvocationID, data.ToolCallID)
	if identity == "" {
		tokenUsageSubagentLog.Printf("ignoring %s without sub-agent identity", event.Type)
		return nil
	}
	row := models.agents[identity]
	if row == nil {
		row = &SubagentModelRequest{InvocationCount: 1}
		models.agents[identity] = row
	}
	if data.AgentDisplayName != "" {
		row.AgentName = data.AgentDisplayName
	} else if row.AgentName == "" && data.AgentName != "" {
		row.AgentName = data.AgentName
	}
	agentUsage := models.agentUsage[identity]
	if agentUsage == nil {
		agentUsage = &AgentUsageBreakdown{AgentName: row.AgentName, AgentType: "subagent"}
		models.agentUsage[identity] = agentUsage
	}
	agentUsage.AgentName = row.AgentName
	agentUsage.AgentType = "subagent"
	agentUsage.SourceEngine = models.sourceEngine
	agentUsage.observeIdentity(data.AgentName, lifecycleDisplayName(event.Type, data.AgentDisplayName))
	if event.Type == "subagent.started" {
		models.recordStartEvidence(identity, event, data)
		row.RequestedModel = data.Model
		row.ResolvedModel = data.ResolvedModel
		row.IncompleteCount = 1
		row.Effort = data.ReasoningEffort
		agentUsage := models.agentUsage[identity]
		if agentUsage == nil {
			agentUsage = &AgentUsageBreakdown{AgentName: row.AgentName, AgentType: "subagent"}
			models.agentUsage[identity] = agentUsage
		}
		agentUsage.InstanceCount = 1
		agentUsage.IncompleteCount = max(agentUsage.IncompleteCount, 1)
		agentUsage.RequestedModels = appendUnique(agentUsage.RequestedModels, data.Model)
		agentUsage.ResolvedModels = appendUnique(agentUsage.ResolvedModels, data.ResolvedModel)
		agentUsage.Effort = data.ReasoningEffort
	}
	if event.Type == "subagent.configured" {
		if data.Model != "" {
			row.ResolvedModel = data.Model
			agentUsage.ResolvedModels = appendUnique(agentUsage.ResolvedModels, data.Model)
		}
		if data.ReasoningEffort != "" {
			row.Effort = data.ReasoningEffort
			if agentUsage := models.agentUsage[identity]; agentUsage != nil {
				agentUsage.Effort = data.ReasoningEffort
			}
		}
	}
	if event.Type == "subagent.completed" || event.Type == "subagent.failed" {
		observeLifecycleOutcome(row, agentUsage, event, data)
	}
	return nil
}

// lifecycleDisplayName returns the display name chosen for an instance; only
// started, completed, and failed events carry it.
func lifecycleDisplayName(eventType, displayName string) string {
	switch eventType {
	case "subagent.started", "subagent.completed", "subagent.failed":
		return displayName
	}
	return ""
}

// observeIdentity records the agent definition (agentName) and the display
// name chosen for this instance, so rows can be keyed by definition.
func (usage *AgentUsageBreakdown) observeIdentity(definition, displayName string) {
	if definition != "" {
		usage.definitionName = definition
	}
	usage.instanceDisplayNames = appendUnique(usage.instanceDisplayNames, displayName)
}

func observeLifecycleOutcome(row *SubagentModelRequest, agentUsage *AgentUsageBreakdown, event sessionSubagentEvent, data sessionSubagentData) {
	if event.Type == "subagent.completed" && data.Cancelled && (row.CompletedCount > 0 || row.FailedCount > 0) {
		return
	}
	if event.Type == "subagent.completed" {
		row.CompletedCount++
		row.IncompleteCount = max(0, row.IncompleteCount-1)
		agentUsage.CompletedCount++
		agentUsage.IncompleteCount = max(0, agentUsage.IncompleteCount-1)
	}
	if event.Type == "subagent.failed" {
		row.FailedCount++
		row.IncompleteCount = max(0, row.IncompleteCount-1)
		row.Error = sanitizeSubagentError(firstNonEmptyModel(subagentErrorMessage(data.Error), data.ErrorMessage))
		row.ErrorCode = data.ErrorCode
		agentUsage.FailedCount++
		agentUsage.IncompleteCount = max(0, agentUsage.IncompleteCount-1)
	}
}
