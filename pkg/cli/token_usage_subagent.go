package cli

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var tokenUsageSubagentLog = logger.New("cli:token_usage_subagent")

func augmentSubagentModelAttribution(runDir string, summary *TokenUsageSummary) {
	if summary == nil {
		return
	}
	requests, actuals, agentUsage, found, err := readSessionSubagentModelsDetailed(runDir)
	if err != nil {
		addTokenUsageWarning(summary, "failed to parse unified subagent information: "+err.Error())
		tokenUsageSubagentLog.Printf("failed to parse unified subagent information: %v", err)
	}
	if found {
		resolver := newModelIdentityResolver(runDir)
		modelCandidates := tokenUsageModelCandidates(summary)
		entries, usageErr := readUnifiedTokenUsageEntries(runDir)
		if usageErr != nil {
			addTokenUsageWarning(summary, "failed to read unified firewall token usage: "+usageErr.Error())
			tokenUsageSubagentLog.Printf("failed to read unified firewall token usage: %v", usageErr)
		}
		summary.AgentUsage = resolveAgentUsageModels(agentUsage, resolver, modelCandidates...)
		agentActuals := subagentActualsFromAgentUsage(summary.AgentUsage)
		if len(agentActuals) == 0 {
			agentActuals = actuals
		}
		summary.SubagentModelRequests = resolveSubagentRequestModels(requests, agentActuals, resolver, modelCandidates...)
		summary.SubagentModelActuals = resolveSubagentActualModels(agentActuals, resolver, modelCandidates...)
		matchPiAgentUsageCredits(summary, entries, resolver)
		reconcileAgentUsageCredits(summary, entries)
		summary.MismatchCount = 0
		augmentDeclaredSubagentModels(runDir, summary)
		return
	}

	augmentDeclaredSubagentModels(runDir, summary)
	tokenUsageSubagentLog.Print("no structured subagent information found, skipping attribution")
}

func subagentActualsFromAgentUsage(agents []AgentUsageBreakdown) []SubagentModelActual {
	var actuals []SubagentModelActual
	for _, agent := range agents {
		if agent.AgentType != "subagent" {
			continue
		}
		for _, model := range agent.Models {
			actuals = append(actuals, SubagentModelActual{
				Model: model.Model, ResolvedModel: model.ResolvedModel, Requests: model.Requests,
				TokenCoreMetrics: model.TokenCoreMetrics, AIC: model.AIC, agentName: agent.AgentName,
				identityEvidence: agent.ResolvedModels,
			})
		}
	}
	return actuals
}

func resolveSubagentRequestModels(requests []SubagentModelRequest, actuals []SubagentModelActual, resolver *modelIdentityResolver, candidates ...string) []SubagentModelRequest {
	for i := range requests {
		row := &requests[i]
		localObserved := make([]string, 0)
		localServed := make([]string, 0)
		for _, actual := range actuals {
			if actual.agentName != "" && !strings.EqualFold(actual.agentName, row.AgentName) {
				continue
			}
			localObserved = append(localObserved, actual.Model)
			localObserved = append(localObserved, actual.ServedModels...)
			if actual.Requests > 0 {
				localServed = append(localServed, actual.Model)
				localServed = append(localServed, actual.ServedModels...)
			}
		}
		slices.Sort(localObserved)
		slices.Sort(localServed)
		requested := firstNonEmptyModel(row.ResolvedModel, row.RequestedModel)
		row.ResolvedModel = resolver.resolve(requested, "", localObserved)
		if row.EffectiveModel != "" {
			row.EffectiveModel = resolver.resolve(row.EffectiveModel, "", localObserved)
			for _, candidate := range localServed {
				if !isModelIdentityAlias(resolver, candidate) && resolver.matches(firstNonEmptyModel(row.ResolvedModel, row.RequestedModel), candidate, "") {
					row.ServedModels = appendUnique(row.ServedModels, candidate)
				}
			}
		} else {
			row.EffectiveModel = firstMatchingObservedModel(requested, localServed, resolver)
			for _, candidate := range localServed {
				if !isModelIdentityAlias(resolver, candidate) && resolver.matches(requested, candidate, "") {
					row.ServedModels = appendUnique(row.ServedModels, candidate)
				}
			}
		}
		if row.EffectiveModel != "" {
			for _, candidate := range candidates {
				if !isModelIdentityAlias(resolver, candidate) && resolver.matches(row.EffectiveModel, candidate, "") {
					row.ServedModels = appendUnique(row.ServedModels, candidate)
				}
			}
		}
		if row.FailedCount > 0 && row.CompletedCount == 0 && row.EffectiveModel == "" {
			row.ReasonCode = modelMismatchReasonSubagentFailed
		}
	}
	return requests
}

func firstMatchingObservedModel(requested string, observed []string, resolver *modelIdentityResolver) string {
	for _, model := range observed {
		if resolver.matches(requested, model, "") {
			return normalizeModelIdentity(modelNameWithoutProvider(model))
		}
	}
	return ""
}

func firstNonEmptyModel(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func resolveSubagentActualModels(actuals []SubagentModelActual, resolver *modelIdentityResolver, candidates ...string) []SubagentModelActual {
	observedByAgent := make(map[string][]string)
	servedByAgent := make(map[string][]string)
	for _, actual := range actuals {
		observedByAgent[actual.agentName] = append(observedByAgent[actual.agentName], actual.Model)
		observedByAgent[actual.agentName] = append(observedByAgent[actual.agentName], actual.ServedModels...)
		observedByAgent[actual.agentName] = append(observedByAgent[actual.agentName], actual.identityEvidence...)
		if actual.Requests > 0 {
			if actual.ResolvedModel == "" || normalizeModelIdentity(actual.Model) != normalizeModelIdentity(actual.ResolvedModel) {
				servedByAgent[actual.agentName] = append(servedByAgent[actual.agentName], actual.Model)
			}
			servedByAgent[actual.agentName] = append(servedByAgent[actual.agentName], actual.ServedModels...)
		}
	}
	for agent := range observedByAgent {
		slices.Sort(observedByAgent[agent])
	}
	grouped := make(map[string]SubagentModelActual, len(actuals))
	for _, actual := range actuals {
		localObserved := observedByAgent[actual.agentName]
		resolved := resolver.resolve(actual.Model, actual.Provider, localObserved)
		if resolved == "" {
			resolved = normalizeModelIdentity(actual.Model)
		}
		combined := grouped[resolved]
		combined.Model = resolved
		combined.ResolvedModel = resolved
		combined.Provider = actual.Provider
		combined.agentName = ""
		combined.Requests += actual.Requests
		combined.InputTokens += actual.InputTokens
		combined.OutputTokens += actual.OutputTokens
		combined.CacheReadTokens += actual.CacheReadTokens
		combined.CacheWriteTokens += actual.CacheWriteTokens
		combined.ReasoningTokens += actual.ReasoningTokens
		combined.AIC += actual.AIC
		combined.TotalDurationMs += actual.TotalDurationMs
		addSubagentServedModels(&combined, actual, resolved, servedByAgent[actual.agentName], resolver, candidates)
		grouped[resolved] = combined
	}
	result := make([]SubagentModelActual, 0, len(grouped))
	for _, actual := range grouped {
		result = append(result, actual)
	}
	sortSubagentModelActuals(result)
	return result
}

func addSubagentServedModels(combined *SubagentModelActual, actual SubagentModelActual, resolved string, agentCandidates []string, resolver *modelIdentityResolver, candidates []string) {
	if actual.Requests == 0 {
		return
	}
	if !isModelIdentityAlias(resolver, actual.Model) &&
		(actual.ResolvedModel == "" || normalizeModelIdentity(actual.Model) != normalizeModelIdentity(actual.ResolvedModel)) {
		combined.ServedModels = appendUnique(combined.ServedModels, actual.Model)
	}
	for _, model := range actual.ServedModels {
		if !isModelIdentityAlias(resolver, model) {
			combined.ServedModels = appendUnique(combined.ServedModels, model)
		}
	}
	appendMatchingServedModels(combined, resolved, actual.Provider, agentCandidates, resolver)
	appendMatchingServedModels(combined, resolved, actual.Provider, candidates, resolver)
}

func appendMatchingServedModels(combined *SubagentModelActual, resolved, provider string, candidates []string, resolver *modelIdentityResolver) {
	for _, candidate := range candidates {
		if !isModelIdentityAlias(resolver, candidate) && resolver.matches(resolved, candidate, provider) {
			combined.ServedModels = appendUnique(combined.ServedModels, candidate)
		}
	}
}

func isModelIdentityAlias(resolver *modelIdentityResolver, model string) bool {
	if resolver == nil {
		return false
	}
	name := modelNameWithoutProvider(model)
	lowered := strings.ToLower(strings.TrimSpace(name))
	normalized := normalizeModelIdentity(name)
	return lowered == normalized && len(resolver.patternsFor(normalized)) > 0
}

func appendUnique(values []string, value string) []string {
	if value == "" || slices.Contains(values, value) {
		return values
	}
	return append(values, value)
}

func resolveAgentUsageModels(agents []AgentUsageBreakdown, resolver *modelIdentityResolver, candidates ...string) []AgentUsageBreakdown {
	for i := range agents {
		agent := &agents[i]
		agent.ServedModels = nil
		observed := make([]string, 0, len(agent.Models)+len(agent.ResolvedModels))
		observed = append(observed, agent.ResolvedModels...)
		actuals := make([]SubagentModelActual, 0, len(agent.Models))
		for _, model := range agent.Models {
			observed = append(observed, model.Model)
			actuals = append(actuals, SubagentModelActual{
				Model: model.Model, Requests: model.Requests, TokenCoreMetrics: model.TokenCoreMetrics, AIC: model.AIC,
				agentName: agent.AgentName, identityEvidence: agent.ResolvedModels,
			})
		}
		resolved := resolveSubagentActualModels(actuals, resolver)
		agent.Models = make([]AgentModelUsage, 0, len(resolved))
		for _, model := range resolved {
			agent.Models = append(agent.Models, AgentModelUsage{
				Model: model.Model, ResolvedModel: model.ResolvedModel, Requests: model.Requests,
				TokenCoreMetrics: model.TokenCoreMetrics, AIC: model.AIC,
			})
			for _, served := range append(model.ServedModels, candidates...) {
				if model.Requests > 0 && !isModelIdentityAlias(resolver, served) {
					if resolver.matches(firstNonEmptyModel(model.ResolvedModel, model.Model), served, "") {
						agent.ServedModels = appendUnique(agent.ServedModels, served)
					}
				}
			}
		}
		for _, requested := range agent.RequestedModels {
			agent.ResolvedModels = appendUnique(agent.ResolvedModels, resolver.resolve(requested, "", observed))
		}
		slices.Sort(agent.RequestedModels)
		slices.Sort(agent.ResolvedModels)
		slices.Sort(agent.ServedModels)
	}
	return agents
}

func tokenUsageModelCandidates(summary *TokenUsageSummary) []string {
	if summary == nil {
		return nil
	}
	models := make([]string, 0, len(summary.ByModel))
	for model := range summary.ByModel {
		models = append(models, model)
	}
	slices.Sort(models)
	return models
}

func reconcileAgentUsageCredits(summary *TokenUsageSummary, entries []TokenUsageEntry) {
	if summary == nil || !summary.AICFound || len(summary.AgentUsage) == 0 {
		return
	}
	var attributed float64
	for _, agent := range summary.AgentUsage {
		attributed += agent.AIC
	}
	expected := summary.TotalAIC
	var endpoints []string
	for _, entry := range entries {
		if entry.Purpose == "routing_classification" {
			expected -= tokenUsageEntryCredits(entry)
		} else if entry.Path != "" {
			endpoints = appendUnique(endpoints, entry.Path)
		}
	}
	if attributed == 0 || math.Abs(attributed-expected) <= 0.01 {
		return
	}
	slices.Sort(endpoints)
	endpoint := strings.Join(endpoints, ", ")
	if endpoint == "" {
		endpoint = summary.endpoint
		if endpoint == "" {
			endpoint = "unknown endpoint"
		}
	}
	addTokenUsageWarning(summary, fmt.Sprintf("per-agent AI credits (%.3f) differ from non-classifier proxy total (%.3f) for endpoint %s", attributed, expected, endpoint))
}

type indexedPiRequest struct {
	agent *AgentUsageBreakdown
	usage agentRequestUsage
}

func matchPiAgentUsageCredits(summary *TokenUsageSummary, entries []TokenUsageEntry, resolver *modelIdentityResolver) {
	if summary == nil || len(entries) == 0 {
		return
	}
	piAgents, requests := collectPiAgentRequests(summary)
	if len(piAgents) == 0 || (len(requests) == 0 && !confirmedZeroUsagePiFailures(piAgents)) {
		return
	}
	slices.SortFunc(requests, func(a, b indexedPiRequest) int {
		if order := a.usage.Timestamp.Compare(b.usage.Timestamp); order != 0 {
			return order
		}
		return strings.Compare(a.agent.AgentName, b.agent.AgentName)
	})
	entries = append([]TokenUsageEntry(nil), entries...)
	sortProxyUsageEntries(entries)
	used := matchPiSubagentRequests(summary, entries, requests, resolver)
	appendPiMainAgentUsage(summary, entries, used, piAgents, requests, resolver)
}

func collectPiAgentRequests(summary *TokenUsageSummary) ([]*AgentUsageBreakdown, []indexedPiRequest) {
	agents := make([]*AgentUsageBreakdown, 0)
	requests := make([]indexedPiRequest, 0)
	for i := range summary.AgentUsage {
		agent := &summary.AgentUsage[i]
		if agent.SourceEngine != "pi" || agent.AgentType != "subagent" {
			continue
		}
		agents = append(agents, agent)
		for _, request := range agent.requestUsages {
			requests = append(requests, indexedPiRequest{agent: agent, usage: request})
		}
	}
	return agents, requests
}

func confirmedZeroUsagePiFailures(agents []*AgentUsageBreakdown) bool {
	if len(agents) == 0 {
		return false
	}
	for _, agent := range agents {
		if agent.FailedCount == 0 {
			return false
		}
	}
	return true
}

func sortProxyUsageEntries(entries []TokenUsageEntry) {
	slices.SortStableFunc(entries, func(a, b TokenUsageEntry) int {
		aTime, aValid := parseTokenUsageTimestamp(a.Timestamp)
		bTime, bValid := parseTokenUsageTimestamp(b.Timestamp)
		if !aValid {
			if !bValid {
				return 0
			}
			return 1
		}
		if !bValid {
			return -1
		}
		return aTime.Compare(bTime)
	})
}

func matchPiSubagentRequests(summary *TokenUsageSummary, entries []TokenUsageEntry, requests []indexedPiRequest, resolver *modelIdentityResolver) map[int]bool {
	used := make(map[int]bool)
	for _, indexed := range requests {
		request := indexed.usage
		entryIndex, entry, matched := matchingProxyUsageEntry(entries, used, request, resolver)
		if !matched {
			addTokenUsageWarning(summary, fmt.Sprintf("could not match pi sub-agent %s model %s usage to proxy token usage; credits were not inferred", indexed.agent.AgentName, request.Model))
			continue
		}
		used[entryIndex] = true
		credits := tokenUsageEntryCredits(entry)
		indexed.agent.AIC += credits
		indexed.agent.TotalApiDurationMs += entry.DurationMs
		for modelIndex := range indexed.agent.Models {
			model := &indexed.agent.Models[modelIndex]
			if resolver.matches(model.Model, request.Model, entry.Provider) {
				model.AIC += credits
				break
			}
		}
	}
	return used
}

func matchingProxyUsageEntry(entries []TokenUsageEntry, used map[int]bool, request agentRequestUsage, resolver *modelIdentityResolver) (int, TokenUsageEntry, bool) {
	if request.UsageIncomplete {
		return -1, TokenUsageEntry{}, false
	}
	for index, entry := range entries {
		if used[index] || entry.Purpose == "routing_classification" || entry.Model == "" {
			continue
		}
		if !resolver.matches(request.Model, modelNameWithoutProvider(entry.Model), entry.Provider) {
			continue
		}
		if request.InputTokens == entry.InputTokens && request.OutputTokens == entry.OutputTokens &&
			request.CacheReadTokens == entry.CacheReadTokens && request.CacheWriteTokens == entry.CacheWriteTokens {
			return index, entry, true
		}
	}
	return -1, TokenUsageEntry{}, false
}

func piSubagentModelIdentities(agents []*AgentUsageBreakdown, requests []indexedPiRequest) []string {
	models := make([]string, 0, len(requests))
	for _, request := range requests {
		models = appendUnique(models, request.usage.Model)
	}
	for _, agent := range agents {
		models = append(models, agent.RequestedModels...)
		models = append(models, agent.ResolvedModels...)
		for _, model := range agent.Models {
			if model.Requests > 0 {
				models = appendUnique(models, model.Model)
			}
		}
	}
	return models
}

func appendPiMainAgentUsage(summary *TokenUsageSummary, entries []TokenUsageEntry, used map[int]bool, agents []*AgentUsageBreakdown, requests []indexedPiRequest, resolver *modelIdentityResolver) {
	if len(requests) == 0 && !confirmedZeroUsagePiFailures(agents) {
		return
	}
	subagentModels := piSubagentModelIdentities(agents, requests)
	main := AgentUsageBreakdown{AgentName: "main", AgentType: "main", SourceEngine: "pi", InstanceCount: 1}
	for index, entry := range entries {
		if used[index] || entry.Purpose == "routing_classification" || entry.Model == "" {
			continue
		}
		if isPiSubagentModel(entry, subagentModels, resolver) {
			continue
		}
		accumulatePiMainUsage(&main, entry)
	}
	summary.AgentUsage = append(summary.AgentUsage, main)
}

func isPiSubagentModel(entry TokenUsageEntry, models []string, resolver *modelIdentityResolver) bool {
	for _, model := range models {
		if resolver.matches(model, modelNameWithoutProvider(entry.Model), entry.Provider) {
			return true
		}
	}
	return false
}

func accumulatePiMainUsage(main *AgentUsageBreakdown, entry TokenUsageEntry) {
	credits := tokenUsageEntryCredits(entry)
	main.Requests++
	main.InputTokens += entry.InputTokens
	main.OutputTokens += entry.OutputTokens
	main.CacheReadTokens += entry.CacheReadTokens
	main.CacheWriteTokens += entry.CacheWriteTokens
	main.ReasoningTokens += entry.ReasoningTokens
	main.AIC += credits
	main.TotalApiDurationMs += entry.DurationMs
	main.Models = appendAgentModelUsage(main.Models, AgentModelUsage{
		Model: entry.Model, ResolvedModel: normalizeModelIdentity(modelNameWithoutProvider(entry.Model)),
		Requests: 1, TokenCoreMetrics: TokenCoreMetrics{
			InputTokens: entry.InputTokens, OutputTokens: entry.OutputTokens,
			CacheReadTokens: entry.CacheReadTokens, CacheWriteTokens: entry.CacheWriteTokens,
			ReasoningTokens: entry.ReasoningTokens,
		}, AIC: credits,
	})
}

func tokenUsageEntryCredits(entry TokenUsageEntry) float64 {
	if credits, _, valid := parseOptionalNonNegativeFloat(entry.AICreditsThisResponse); valid {
		return credits
	}
	inputTokensIncludeCache, _, _ := parseOptionalBool(entry.InputTokensIncludeCache)
	return computeModelInferenceAICWithCacheSemantics(
		entry.Provider, entry.Model, entry.InputTokens, entry.OutputTokens,
		entry.CacheReadTokens, entry.CacheWriteTokens, entry.ReasoningTokens, inputTokensIncludeCache,
	)
}

func sortSubagentModelActuals(actuals []SubagentModelActual) {
	slices.SortStableFunc(actuals, func(a, b SubagentModelActual) int {
		if a.Requests > b.Requests {
			return -1
		}
		if a.Requests < b.Requests {
			return 1
		}
		return strings.Compare(a.Model, b.Model)
	})
}

func addTokenUsageWarning(summary *TokenUsageSummary, warning string) {
	if summary == nil || warning == "" {
		return
	}
	if slices.Contains(summary.Warnings, warning) {
		return
	}
	summary.Warnings = append(summary.Warnings, warning)
}

type subagentModelKey struct {
	agent string
	model string
}
