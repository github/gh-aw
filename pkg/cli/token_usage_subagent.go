package cli

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var tokenUsageSubagentLog = logger.New("cli:token_usage_subagent")

var subagentDispatchPattern = regexp.MustCompile(`^●\s+([A-Za-z0-9][A-Za-z0-9._ -]*?)\s*\((?:model:\s*)?([A-Za-z0-9][A-Za-z0-9._:-]*)\)`)

type subagentDispatchKey struct {
	agent    string
	model    string
	resolved string
}

func augmentSubagentModelAttribution(runDir string, summary *TokenUsageSummary) {
	if summary == nil {
		return
	}
	requests, actuals, found, err := readSessionSubagentModels(runDir)
	if err != nil {
		addTokenUsageWarning(summary, "failed to parse unified subagent information: "+err.Error())
		tokenUsageSubagentLog.Printf("failed to parse unified subagent information: %v", err)
	}
	if found {
		resolver := newModelIdentityResolver(runDir)
		summary.SubagentModelRequests = resolveSubagentRequestModels(requests, actuals, resolver)
		summary.SubagentModelActuals = resolveSubagentActualModels(actuals, resolver)
		summary.MismatchCount = 0
		augmentDeclaredSubagentModels(runDir, summary)
		return
	}

	augmentDeclaredSubagentModels(runDir, summary)
	requests = extractSubagentModelRequests(runDir)
	if len(requests) == 0 {
		tokenUsageSubagentLog.Print("no subagent model dispatch requests found, skipping attribution")
		return
	}
	augmentHeuristicSubagentModelAttribution(requests, summary, newModelIdentityResolver(runDir))
}

func augmentHeuristicSubagentModelAttribution(requests []SubagentModelRequest, summary *TokenUsageSummary, resolver *modelIdentityResolver) {
	addTokenUsageWarning(summary, subagentStdioWarning)

	actuals, _ := collectSubagentModelActuals(summary)
	actuals = filterSubagentActualModels(actuals, requests, resolver)
	actuals = resolveSubagentActualModels(actuals, resolver)
	summary.SubagentModelActuals = actuals
	observedModels := make(map[string]string, len(actuals))
	for _, actual := range actuals {
		if actual.Requests > 0 {
			observedModels[actual.Model] = actual.Provider
		}
	}

	requestRows := make([]SubagentModelRequest, 0, len(requests))
	mismatchCount := 0
	for _, row := range requests {
		requested := row.RequestedModel
		if row.ResolvedModel != "" {
			requested = row.ResolvedModel
		}
		var observedNames []string
		for model := range observedModels {
			observedNames = append(observedNames, model)
		}
		resolved := resolver.resolve(requested, "", observedNames)
		if _, ok := observedModels[resolved]; ok {
			row.ResolvedModel = resolved
			row.EffectiveModel = resolved
			row.ServedModels = []string{resolved}
		} else {
			if len(observedModels) == 0 {
				row.ReasonCode = modelMismatchReasonTokenUsageMissing
			} else {
				row.ReasonCode = modelMismatchReasonModelNotObserved
			}
			mismatchCount += row.InvocationCount
		}
		requestRows = append(requestRows, row)
	}
	summary.SubagentModelRequests = requestRows
	summary.MismatchCount = mismatchCount
	tokenUsageSubagentLog.Printf("attributed %d subagent request(s), %d mismatch(es)", len(requestRows), mismatchCount)
}

func filterSubagentActualModels(actuals []SubagentModelActual, requests []SubagentModelRequest, resolver *modelIdentityResolver) []SubagentModelActual {
	filtered := make([]SubagentModelActual, 0, len(actuals))
	for _, actual := range actuals {
		for _, request := range requests {
			model := firstNonEmptyModel(request.ResolvedModel, request.RequestedModel)
			if resolver.matches(model, actual.Model, actual.Provider) {
				filtered = append(filtered, actual)
				break
			}
		}
	}
	return filtered
}

func resolveSubagentRequestModels(requests []SubagentModelRequest, actuals []SubagentModelActual, resolver *modelIdentityResolver) []SubagentModelRequest {
	observed := make([]string, 0, len(actuals)*2)
	for _, actual := range actuals {
		observed = append(observed, actual.Model)
		observed = append(observed, actual.ServedModels...)
	}
	for i := range requests {
		row := &requests[i]
		row.ResolvedModel = resolver.resolve(firstNonEmptyModel(row.ResolvedModel, row.RequestedModel), "", observed)
		if row.EffectiveModel != "" {
			row.EffectiveModel = resolver.resolve(row.EffectiveModel, "", observed)
			row.ServedModels = []string{row.EffectiveModel}
		}
		if row.FailedCount > 0 && row.CompletedCount == 0 && row.EffectiveModel == "" {
			row.ReasonCode = modelMismatchReasonSubagentFailed
		}
	}
	return requests
}

func firstNonEmptyModel(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func resolveSubagentActualModels(actuals []SubagentModelActual, resolver *modelIdentityResolver) []SubagentModelActual {
	observed := make([]string, 0, len(actuals))
	for _, actual := range actuals {
		observed = append(observed, actual.Model)
		observed = append(observed, actual.ServedModels...)
	}
	grouped := make(map[string]SubagentModelActual, len(actuals))
	for _, actual := range actuals {
		resolved := resolver.resolve(actual.Model, actual.Provider, observed)
		if resolved == "" {
			resolved = normalizeModelIdentity(actual.Model)
		}
		combined := grouped[resolved]
		combined.Model = resolved
		combined.ResolvedModel = resolved
		combined.Provider = actual.Provider
		combined.Requests += actual.Requests
		combined.InputTokens += actual.InputTokens
		combined.OutputTokens += actual.OutputTokens
		combined.CacheReadTokens += actual.CacheReadTokens
		combined.CacheWriteTokens += actual.CacheWriteTokens
		combined.ReasoningTokens += actual.ReasoningTokens
		combined.AIC += actual.AIC
		combined.TotalDurationMs += actual.TotalDurationMs
		combined.ServedModels = appendUnique(combined.ServedModels, actual.Model)
		for _, model := range actual.ServedModels {
			combined.ServedModels = appendUnique(combined.ServedModels, model)
		}
		grouped[resolved] = combined
	}
	result := make([]SubagentModelActual, 0, len(grouped))
	for _, actual := range grouped {
		result = append(result, actual)
	}
	sortSubagentModelActuals(result)
	return result
}

func appendUnique(values []string, value string) []string {
	if value == "" || slices.Contains(values, value) {
		return values
	}
	return append(values, value)
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

func extractSubagentModelRequests(runDir string) []SubagentModelRequest {
	agentStdioPath := findAgentStdioFile(runDir)
	if agentStdioPath == "" {
		tokenUsageSubagentLog.Printf("no agent stdio file found under %s", runDir)
		return nil
	}

	file, err := os.Open(agentStdioPath)
	if err != nil {
		tokenUsageSubagentLog.Printf("failed to open agent stdio file %s: %v", agentStdioPath, err)
		return nil
	}
	defer file.Close()

	counts := make(map[subagentDispatchKey]int)

	reader := bufio.NewReader(file)
	for {
		line, readErr := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line != "" {
			countSubagentDispatchLine(counts, line)
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			tokenUsageSubagentLog.Printf("failed to read agent stdio file %s: %v", agentStdioPath, readErr)
			return nil
		}
	}

	return subagentModelRequestRows(counts)
}

func subagentModelRequestRows(counts map[subagentDispatchKey]int) []SubagentModelRequest {
	rows := make([]SubagentModelRequest, 0, len(counts))
	for k, n := range counts {
		rows = append(rows, SubagentModelRequest{
			AgentName:       k.agent,
			RequestedModel:  k.model,
			ResolvedModel:   k.resolved,
			InvocationCount: n,
		})
	}
	slices.SortStableFunc(rows, func(a, b SubagentModelRequest) int {
		if a.AgentName != b.AgentName {
			if a.AgentName < b.AgentName {
				return -1
			}
			return 1
		}
		switch {
		case a.RequestedModel < b.RequestedModel:
			return -1
		case a.RequestedModel > b.RequestedModel:
			return 1
		default:
			return strings.Compare(a.ResolvedModel, b.ResolvedModel)
		}
	})
	return rows
}

func collectSubagentModelActuals(summary *TokenUsageSummary) ([]SubagentModelActual, map[string]string) {
	actuals := make([]SubagentModelActual, 0, len(summary.ByModel))
	observedModels := make(map[string]string, len(summary.ByModel))
	for model, usage := range subagentObservedModels(summary) {
		if usage == nil || model == "" || usage.Requests == 0 {
			continue
		}
		actuals = append(actuals, SubagentModelActual{
			Model: model, Provider: usage.Provider, Requests: usage.Requests,
			TokenCoreMetrics: usage.TokenCoreMetrics, AIC: usage.AIC,
			TotalDurationMs: usage.DurationMs,
		})
		observedModels[model] = usage.Provider
	}
	sortSubagentModelActuals(actuals)
	return actuals, observedModels
}

func countSubagentDispatchLine(counts map[subagentDispatchKey]int, line string) {
	var dispatch struct {
		Type      string `json:"type"`
		Agent     string `json:"agent"`
		Requested string `json:"requested_model"`
		Resolved  string `json:"resolved_model"`
	}
	if json.Unmarshal([]byte(line), &dispatch) == nil && dispatch.Type == "gh_aw_subagent_dispatch" {
		if dispatch.Agent != "" && dispatch.Requested != "" {
			counts[subagentDispatchKey{agent: dispatch.Agent, model: dispatch.Requested, resolved: dispatch.Resolved}]++
		}
		return
	}
	agentName, requestedModel := "", ""
	for index, match := range subagentDispatchPattern.FindStringSubmatch(line) {
		switch index {
		case 1:
			agentName = strings.TrimSpace(match)
		case 2:
			requestedModel = strings.TrimSpace(match)
		}
	}
	if agentName != "" && requestedModel != "" {
		counts[subagentDispatchKey{agent: agentName, model: requestedModel}]++
	}
}
