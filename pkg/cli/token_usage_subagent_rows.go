package cli

import (
	"slices"
	"strings"
)

func (models *subagentSessionModels) rows() ([]SubagentModelRequest, []SubagentModelActual) {
	grouped := make(map[subagentModelKey]SubagentModelRequest)
	effectiveModels := make(map[subagentModelKey]map[string]struct{})
	for agentID, row := range models.agents {
		if row.AgentName != "" {
			addSubagentRequestGroup(grouped, effectiveModels, row, models.actualCounts[agentID])
		}
	}
	applySubagentEffectiveModels(grouped, effectiveModels)
	requests := sortedSubagentRequests(grouped, len(models.agents))
	return requests, models.actualRows()
}

func addSubagentRequestGroup(grouped map[subagentModelKey]SubagentModelRequest, effectiveModels map[subagentModelKey]map[string]struct{}, row *SubagentModelRequest, counts map[string]int) {
	effectiveModel := ""
	observedModels := make([]string, 0, len(counts))
	for model := range counts {
		observedModels = append(observedModels, model)
	}
	slices.Sort(observedModels)
	for _, model := range observedModels {
		if counts[model] > 0 {
			effectiveModel = model
			break
		}
	}
	key := subagentModelKey{agent: row.AgentName, model: row.RequestedModel}
	if effectiveModel != "" {
		if effectiveModels[key] == nil {
			effectiveModels[key] = make(map[string]struct{})
		}
		effectiveModels[key][effectiveModel] = struct{}{}
	}
	current := *row
	current.EffectiveModel = ""
	current.ReasonCode = ""
	combined, exists := grouped[key]
	if !exists {
		combined = current
	} else {
		combined.InvocationCount += current.InvocationCount
		combined.CompletedCount += current.CompletedCount
		combined.FailedCount += current.FailedCount
		combined.IncompleteCount += current.IncompleteCount
		combined.Effort = combineSubagentEffort(combined.Effort, current.Effort)
		if combined.Error == "" && current.Error != "" {
			combined.Error = current.Error
		}
		if combined.ErrorCode == "" {
			combined.ErrorCode = current.ErrorCode
		}
	}
	grouped[key] = combined
}

func applySubagentEffectiveModels(grouped map[subagentModelKey]SubagentModelRequest, effectiveModels map[subagentModelKey]map[string]struct{}) {
	for key, row := range grouped {
		switch len(effectiveModels[key]) {
		case 1:
			for model := range effectiveModels[key] {
				row.EffectiveModel = model
			}
		case 0:
			if row.FailedCount > 0 && row.CompletedCount == 0 {
				row.ReasonCode = modelMismatchReasonSubagentFailed
			}
		}
		grouped[key] = row
	}
}

func sortedSubagentRequests(grouped map[subagentModelKey]SubagentModelRequest, capacity int) []SubagentModelRequest {
	requests := make([]SubagentModelRequest, 0, capacity)
	for _, row := range grouped {
		requests = append(requests, row)
	}
	slices.SortFunc(requests, func(a, b SubagentModelRequest) int {
		if order := strings.Compare(a.AgentName, b.AgentName); order != 0 {
			return order
		}
		if order := strings.Compare(a.RequestedModel, b.RequestedModel); order != 0 {
			return order
		}
		return strings.Compare(a.EffectiveModel, b.EffectiveModel)
	})
	return requests
}

// agentRows returns one row per agent display name and definition. Model and
// request attribution use these rows so that each instance keeps its own
// evidence; mergeAgentUsageByDefinition builds the public rows afterwards.
func (models *subagentSessionModels) agentRows() []AgentUsageBreakdown {
	grouped := make(map[string]*AgentUsageBreakdown)
	for agentID, usage := range models.agentUsage {
		if usage.AgentName == "" {
			usage.AgentName = "subagent"
		}
		definition := ""
		if usage.AgentType == "subagent" {
			definition = usage.definitionName
		}
		key := usage.AgentType + "\x00" + usage.AgentName + "\x00" + definition
		row := grouped[key]
		if row == nil {
			row = &AgentUsageBreakdown{AgentName: usage.AgentName, AgentType: usage.AgentType, SourceEngine: usage.SourceEngine, definitionName: definition}
			grouped[key] = row
		}
		if definition != "" {
			for _, displayName := range usage.instanceDisplayNames {
				row.instanceDisplayNames = appendUnique(row.instanceDisplayNames, displayName)
			}
		}
		mergeAgentUsageBreakdown(row, usage, models.agents[agentID])
	}
	result := make([]AgentUsageBreakdown, 0, len(grouped))
	for _, row := range grouped {
		result = append(result, *row)
	}
	return sortAgentUsageRows(result)
}

// mergeAgentUsageByDefinition merges attributed agent rows into the public rows,
// one per agent definition. Rows without a definition keep their name.
func mergeAgentUsageByDefinition(agents []AgentUsageBreakdown) []AgentUsageBreakdown {
	grouped := make(map[string]*AgentUsageBreakdown)
	order := make([]string, 0, len(agents))
	for i := range agents {
		agent := &agents[i]
		name := agent.AgentName
		if agent.definitionName != "" {
			name = agent.definitionName
		}
		key := agent.AgentType + "\x00" + name
		row := grouped[key]
		if row == nil {
			row = &AgentUsageBreakdown{AgentName: name, AgentType: agent.AgentType, SourceEngine: agent.SourceEngine, definitionName: agent.definitionName}
			grouped[key] = row
			order = append(order, key)
		}
		for _, displayName := range agent.instanceDisplayNames {
			row.DisplayNames = appendUnique(row.DisplayNames, displayName)
		}
		mergeAgentUsageTotals(row, agent)
		for _, model := range agent.ServedModels {
			row.ServedModels = appendUnique(row.ServedModels, model)
		}
	}
	result := make([]AgentUsageBreakdown, 0, len(grouped))
	for _, key := range order {
		result = append(result, *grouped[key])
	}
	return sortAgentUsageRows(result)
}

func sortAgentUsageRows(result []AgentUsageBreakdown) []AgentUsageBreakdown {
	for i := range result {
		row := &result[i]
		slices.Sort(row.DisplayNames)
		slices.Sort(row.RequestedModels)
		slices.Sort(row.ResolvedModels)
		slices.Sort(row.ServedModels)
		slices.SortFunc(row.Models, func(a, b AgentModelUsage) int { return strings.Compare(a.Model, b.Model) })
	}
	slices.SortFunc(result, func(a, b AgentUsageBreakdown) int {
		if a.AgentType != b.AgentType {
			if a.AgentType == "main" {
				return -1
			}
			if b.AgentType == "main" {
				return 1
			}
		}
		return strings.Compare(a.AgentName, b.AgentName)
	})
	return result
}

func mergeAgentUsageBreakdown(row, usage *AgentUsageBreakdown, request *SubagentModelRequest) {
	mergeAgentUsageTotals(row, usage)
	for _, model := range usage.Models {
		if model.Requests > 0 {
			row.ServedModels = appendUnique(row.ServedModels, model.Model)
		}
	}
	if request != nil {
		row.RequestedModels = appendUnique(row.RequestedModels, request.RequestedModel)
		row.ResolvedModels = appendUnique(row.ResolvedModels, request.ResolvedModel)
		row.ServedModels = appendUnique(row.ServedModels, request.EffectiveModel)
		row.Effort = combineSubagentEffort(row.Effort, request.Effort)
	}
}

func mergeAgentUsageTotals(row, usage *AgentUsageBreakdown) {
	row.InstanceCount += usage.InstanceCount
	if row.SourceEngine == "" {
		row.SourceEngine = usage.SourceEngine
	}
	row.CompletedCount += usage.CompletedCount
	row.FailedCount += usage.FailedCount
	row.IncompleteCount += usage.IncompleteCount
	row.Requests += usage.Requests
	row.InputTokens += usage.InputTokens
	row.OutputTokens += usage.OutputTokens
	row.CacheReadTokens += usage.CacheReadTokens
	row.CacheWriteTokens += usage.CacheWriteTokens
	row.ReasoningTokens += usage.ReasoningTokens
	row.AIC += usage.AIC
	row.TotalApiDurationMs += usage.TotalApiDurationMs
	row.Effort = combineSubagentEffort(row.Effort, usage.Effort)
	row.requestUsages = append(row.requestUsages, usage.requestUsages...)
	for _, model := range usage.RequestedModels {
		row.RequestedModels = appendUnique(row.RequestedModels, model)
	}
	for _, model := range usage.ResolvedModels {
		row.ResolvedModels = appendUnique(row.ResolvedModels, model)
	}
	for _, model := range usage.Models {
		row.Models = appendAgentModelUsage(row.Models, model)
	}
}

func appendAgentModelUsage(models []AgentModelUsage, next AgentModelUsage) []AgentModelUsage {
	for i := range models {
		model := &models[i]
		if model.Model != next.Model {
			continue
		}
		model.Requests += next.Requests
		model.InputTokens += next.InputTokens
		model.OutputTokens += next.OutputTokens
		model.CacheReadTokens += next.CacheReadTokens
		model.CacheWriteTokens += next.CacheWriteTokens
		model.ReasoningTokens += next.ReasoningTokens
		model.AIC += next.AIC
		for _, requestModel := range next.requestModels {
			model.requestModels = appendUnique(model.requestModels, requestModel)
		}
		return models
	}
	next.requestModels = slices.Clone(next.requestModels)
	return append(models, next)
}

func (models *subagentSessionModels) actualRows() []SubagentModelActual {
	counts := make(map[string]SubagentModelActual)
	for _, byModel := range models.actualModels {
		for model, usage := range byModel {
			actual := counts[model]
			actual.Model = model
			actual.Requests += usage.Requests
			actual.InputTokens += usage.InputTokens
			actual.OutputTokens += usage.OutputTokens
			actual.CacheReadTokens += usage.CacheReadTokens
			actual.CacheWriteTokens += usage.CacheWriteTokens
			actual.ReasoningTokens += usage.ReasoningTokens
			actual.AIC += usage.AIC
			counts[model] = actual
		}
	}
	actuals := make([]SubagentModelActual, 0, len(counts))
	for _, actual := range counts {
		actuals = append(actuals, actual)
	}
	slices.SortFunc(actuals, func(a, b SubagentModelActual) int { return strings.Compare(a.Model, b.Model) })
	return actuals
}
