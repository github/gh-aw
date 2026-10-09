package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const modelRoutingJSONLPath = "api-proxy-logs/model-routing.jsonl"

type ModelRoutingLabels struct {
	TaskType       string `json:"task_type,omitempty"`
	Scope          string `json:"scope,omitempty"`
	TaskComplexity string `json:"task_complexity,omitempty"`
}

type ModelRoutingChoice struct {
	ID       string `json:"id,omitempty"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Effort   string `json:"effort,omitempty"`
}

type ModelRoutingFailure struct {
	Code   string `json:"code,omitempty"`
	Detail string `json:"detail,omitempty"`
}

type ModelRoutingCost struct {
	Requests         int     `json:"requests"`
	InputTokens      int     `json:"input_tokens"`
	OutputTokens     int     `json:"output_tokens"`
	CacheReadTokens  int     `json:"cache_read_tokens"`
	CacheWriteTokens int     `json:"cache_write_tokens"`
	AIC              float64 `json:"aic"`
}

type ModelRoutingDeviation struct {
	RequestedModel  string `json:"requested_model,omitempty"`
	RequestedEffort string `json:"requested_effort,omitempty"`
	SelectedModel   string `json:"selected_model,omitempty"`
	SelectedEffort  string `json:"selected_effort,omitempty"`
	Deviation       string `json:"deviation,omitempty"`
	Count           int    `json:"count"`
}

type ModelRoutingAgentCost struct {
	AgentName       string   `json:"agent_name"`
	AgentType       string   `json:"agent_type"`
	InstanceCount   int      `json:"instance_count"`
	CompletedCount  int      `json:"completed_count,omitempty"`
	FailedCount     int      `json:"failed_count,omitempty"`
	IncompleteCount int      `json:"incomplete_count,omitempty"`
	Effort          string   `json:"effort,omitempty"`
	Models          []string `json:"models,omitempty"`
	ModelRoutingCost
}

type ModelRoutingSummary struct {
	Status                          string                  `json:"status"`
	Schema                          string                  `json:"schema,omitempty"`
	Objective                       string                  `json:"objective,omitempty"`
	ObjectiveMode                   string                  `json:"objective_mode,omitempty"`
	Provider                        string                  `json:"provider,omitempty"`
	Labels                          ModelRoutingLabels      `json:"labels"`
	Mode                            string                  `json:"mode,omitempty"`
	ClassifierModel                 string                  `json:"classifier_model,omitempty"`
	ClassifierEffort                string                  `json:"classifier_effort,omitempty"`
	ClassifierAttempts              int                     `json:"classifier_attempts,omitempty"`
	DegradedClassification          bool                    `json:"degraded_classification,omitempty"`
	DegradedReason                  string                  `json:"degraded_reason,omitempty"`
	SelectedID                      string                  `json:"selected_id,omitempty"`
	SelectedProvider                string                  `json:"selected_provider,omitempty"`
	SelectedModel                   string                  `json:"selected_model,omitempty"`
	SelectedEffort                  string                  `json:"selected_effort,omitempty"`
	WireModel                       string                  `json:"wire_model,omitempty"`
	Endpoint                        string                  `json:"endpoint,omitempty"`
	EffectiveEndpoint               string                  `json:"effective_endpoint,omitempty"`
	SelectedEndpoint                string                  `json:"selected_endpoint,omitempty"`
	TopChoices                      []ModelRoutingChoice    `json:"top_choices,omitempty"`
	RouterName                      string                  `json:"router_name,omitempty"`
	RouterVersion                   string                  `json:"router_version,omitempty"`
	LatencyMs                       int                     `json:"latency_ms,omitempty"`
	Failure                         *ModelRoutingFailure    `json:"failure,omitempty"`
	RoutedCounts                    map[string]int          `json:"routed_counts,omitempty"`
	OutcomeCounts                   map[string]int          `json:"outcome_counts,omitempty"`
	Deviations                      []ModelRoutingDeviation `json:"deviations,omitempty"`
	ClassifierCost                  ModelRoutingCost        `json:"classifier_cost"`
	SelectedModelCost               ModelRoutingCost        `json:"selected_model_cost"`
	DeviatedTrafficCost             ModelRoutingCost        `json:"deviated_traffic_cost"`
	MainAgentCost                   ModelRoutingCost        `json:"main_agent_cost,omitzero"`
	SubagentCosts                   []ModelRoutingAgentCost `json:"subagent_costs,omitempty"`
	EndpointOnlyDeviationNormalized bool                    `json:"endpoint_only_deviation_normalized,omitempty"`
}

type effectiveModelAttribution struct {
	Model          string
	RequestedModel string
	Effort         string
	RoutingStatus  string
}

func resolveEffectiveModelAttribution(runDir string, info *AwInfo, routing *ModelRoutingSummary, usage *TokenUsageSummary) effectiveModelAttribution {
	var result effectiveModelAttribution
	info = mergeSessionModelRoutingAwInfo(runDir, info)
	if info == nil {
		return result
	}
	result.RequestedModel = info.RequestedModel
	if info.ModelRouting != nil {
		result.RoutingStatus = info.ModelRouting.Status
		result.Effort = firstNonEmpty(info.ModelRouting.AppliedEffort, info.ModelRouting.Effort)
	}
	if routing != nil {
		if result.RoutingStatus == "" || result.RoutingStatus == "not_routed" {
			result.RoutingStatus = routing.Status
		}
		if result.Effort == "" {
			result.Effort = routing.SelectedEffort
		}
	}
	if result.RequestedModel == "" && (info.ModelRouting != nil || (routing != nil && routing.Status != "" && routing.Status != "not_routed")) {
		result.RequestedModel = info.Model
	}

	primaryModel := primaryTokenUsageModel(usage)
	routingActive := (info.ModelRouting != nil && info.ModelRouting.Status != "" && info.ModelRouting.Status != "not_routed") ||
		(routing != nil && routing.Status != "" && routing.Status != "not_routed")
	switch {
	case info.FallbackModel != "":
		result.Model = info.FallbackModel
	case info.ModelRouting != nil && info.ModelRouting.Status == "selected":
		result.Model = firstNonEmpty(info.ModelRouting.WireModel, info.ModelRouting.Model)
	case routing != nil && routing.Status == "selected":
		result.Model = firstNonEmpty(routing.WireModel, routing.SelectedModel)
	case primaryModel != "" && routingActive && result.RoutingStatus == "selected":
		result.Model = primaryModel
	case !routingActive:
		result.Model = info.Model
	}
	return result
}

func mergeSessionModelRoutingAwInfo(runDir string, info *AwInfo) *AwInfo {
	sessionRouting, found, err := readSessionModelRouting(runDir)
	if err != nil {
		tokenUsageSubagentLog.Printf("failed to read unified session model routing: %v", err)
	}
	if !found || sessionRouting == nil {
		return info
	}
	var sessionInfo AwInfo
	if info != nil {
		sessionInfo = *info
	}
	if workflowInfo := sessionRouting.WorkflowInfo; workflowInfo != nil {
		if workflowInfo.EngineID != "" {
			sessionInfo.EngineID = workflowInfo.EngineID
		}
		if workflowInfo.Model != "" {
			sessionInfo.Model = workflowInfo.Model
		}
		if workflowInfo.RequestedModel != "" {
			sessionInfo.RequestedModel = workflowInfo.RequestedModel
		}
	}
	if modelRouting := sessionRouting.modelRouting(); modelRouting != nil {
		sessionInfo.ModelRouting = modelRouting
	}
	return &sessionInfo
}

func primaryTokenUsageModel(usage *TokenUsageSummary) string {
	if usage == nil {
		return ""
	}
	bestModel := ""
	bestAIC := float64(-1)
	for model, details := range usage.ByModel {
		if details == nil || model == "" || model == "unknown" {
			continue
		}
		if details.AIC > bestAIC || (details.AIC == bestAIC && (bestModel == "" || model < bestModel)) {
			bestModel = model
			bestAIC = details.AIC
		}
	}
	return bestModel
}

type modelRoutingRecord struct {
	Schema           string `json:"_schema"`
	Stage            string `json:"stage"`
	Purpose          string `json:"purpose"`
	Attempt          int    `json:"attempt"`
	ClassifierModel  string `json:"classifier_model"`
	ClassifierEffort string `json:"classifier_effort"`
	Objective        struct {
		Goal string `json:"goal"`
		Mode string `json:"mode"`
	} `json:"objective"`
	Provider               string             `json:"provider"`
	Labels                 ModelRoutingLabels `json:"labels"`
	Mode                   string             `json:"mode"`
	ClassifierAttempts     int                `json:"classifier_attempts"`
	DegradedClassification bool               `json:"degraded_classification"`
	DegradedReason         string             `json:"degraded_reason"`
	SelectedID             string             `json:"selected_id"`
	SelectedProvider       string             `json:"selected_provider"`
	SelectedModel          string             `json:"selected_model"`
	SelectedEffort         string             `json:"selected_effort"`
	WireModel              string             `json:"wire_model"`
	Endpoint               string             `json:"endpoint"`
	RankedChoices          []json.RawMessage  `json:"ranked_choices"`
	Router                 struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"router"`
	LatencyMs       int      `json:"latency_ms"`
	Code            string   `json:"code"`
	Detail          string   `json:"detail"`
	RequestID       string   `json:"request_id"`
	Routed          string   `json:"routed"`
	Deviations      []string `json:"deviations"`
	RequestedModel  string   `json:"requested_model"`
	RequestedEffort string   `json:"requested_effort"`
	Outcome         string   `json:"outcome"`
}

func findModelRoutingFile(runDir string) string {
	primary := filepath.Join(runDir, "sandbox", "firewall", "logs", modelRoutingJSONLPath)
	if modelRoutingFileExists(primary) {
		return primary
	}
	audit := filepath.Join(runDir, "sandbox", "firewall", "audit", modelRoutingJSONLPath)
	if modelRoutingFileExists(audit) {
		return audit
	}
	if legacy := findLegacyAPIProxyLogFile(runDir, modelRoutingJSONLPath); legacy != "" {
		return legacy
	}
	var found string
	_ = filepath.WalkDir(runDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if entry.Name() == "model-routing.jsonl" {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

func modelRoutingFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func analyzeModelRouting(runDir string) *ModelRoutingSummary {
	sessionRouting, sessionFound, sessionErr := readSessionModelRouting(runDir)
	if sessionErr != nil {
		tokenUsageSubagentLog.Printf("failed to read unified session model routing: %v", sessionErr)
	}
	routingPath := findModelRoutingFile(runDir)
	if routingPath == "" && (!sessionFound || sessionRouting.modelRouting() == nil) {
		return nil
	}
	proxyEntries := tokenUsageEntriesForRun(runDir)
	var summary *ModelRoutingSummary
	if routingPath != "" {
		summary = parseModelRoutingFile(routingPath, proxyEntries)
	} else {
		summary = &ModelRoutingSummary{Status: "not_routed"}
	}
	applySessionModelRouting(summary, sessionRouting)
	_, _, agents, found, err := readSessionSubagentModelsDetailed(runDir)
	if err == nil && found {
		models := make([]string, 0, len(proxyEntries))
		for _, entry := range proxyEntries {
			models = appendUnique(models, entry.Model)
		}

		agentSummary := &TokenUsageSummary{AgentUsage: resolveAgentUsageModels(agents, newModelIdentityResolver(runDir), models...)}
		matchPiAgentUsageCredits(agentSummary, proxyEntries, newModelIdentityResolver(runDir))
		applyAgentUsageToModelRouting(summary, agentSummary.AgentUsage)
	}
	return summary
}

func applySessionModelRouting(summary *ModelRoutingSummary, session *sessionModelRoutingAttribution) {
	if summary == nil || session == nil {
		return
	}
	routing := session.modelRouting()
	if routing == nil {
		return
	}
	if routing.Status != "" {
		summary.Status = routing.Status
	}
	if routing.WireModel != "" {
		summary.WireModel = routing.WireModel
	}
	if summary.SelectedModel == "" {
		summary.SelectedModel = firstNonEmpty(routing.Model, routing.WireModel)
	}
	if routing.Effort != "" && summary.SelectedEffort == "" {
		summary.SelectedEffort = routing.Effort
	}
	if routing.Endpoint != "" {
		summary.EffectiveEndpoint = routing.Endpoint
	}
	if routing.SelectedEndpoint != "" {
		summary.SelectedEndpoint = routing.SelectedEndpoint
	}
	if routing.Mode != "" && summary.Mode == "" {
		summary.Mode = routing.Mode
	}
	if routing.SelectedID != "" && summary.SelectedID == "" {
		summary.SelectedID = routing.SelectedID
	}
	if routing.RouterVersion != "" && summary.RouterVersion == "" {
		summary.RouterVersion = routing.RouterVersion
	}
	if routing.FailureCode != "" && summary.Failure == nil {
		summary.Failure = &ModelRoutingFailure{Code: routing.FailureCode}
	}
}

func applyAgentUsageToModelRouting(summary *ModelRoutingSummary, agents []AgentUsageBreakdown) {
	for _, agent := range agents {
		cost := ModelRoutingCost{
			Requests: agent.Requests, InputTokens: agent.InputTokens, OutputTokens: agent.OutputTokens,
			CacheReadTokens: agent.CacheReadTokens, CacheWriteTokens: agent.CacheWriteTokens, AIC: agent.AIC,
		}
		if agent.AgentType == "main" {
			summary.MainAgentCost = cost
			continue
		}
		models := append([]string(nil), agent.ServedModels...)
		slices.Sort(models)
		summary.SubagentCosts = append(summary.SubagentCosts, ModelRoutingAgentCost{
			AgentName: agent.AgentName, AgentType: agent.AgentType, InstanceCount: agent.InstanceCount,
			CompletedCount: agent.CompletedCount, FailedCount: agent.FailedCount,
			IncompleteCount: agent.IncompleteCount, Effort: agent.Effort, Models: models, ModelRoutingCost: cost,
		})
	}
	slices.SortFunc(summary.SubagentCosts, func(a, b ModelRoutingAgentCost) int {
		return strings.Compare(a.AgentName, b.AgentName)
	})
}

func tokenUsageEntriesForRun(runDir string) []TokenUsageEntry {
	entries, err := readUnifiedTokenUsageEntries(runDir)
	if err != nil {
		tokenUsageSubagentLog.Printf("failed to read unified firewall token usage: %v", err)
		return nil
	}
	return entries
}

func parseModelRoutingFile(path string, usageEntries []TokenUsageEntry) *ModelRoutingSummary {
	file, err := os.Open(path)
	if err != nil {
		return &ModelRoutingSummary{Status: "unavailable"}
	}
	defer file.Close()

	summary := &ModelRoutingSummary{
		Status:        "not_routed",
		RoutedCounts:  make(map[string]int),
		OutcomeCounts: make(map[string]int),
	}
	records, err := readModelRoutingRecords(file)
	if err != nil {
		return &ModelRoutingSummary{Status: "unavailable"}
	}
	requests := make([]modelRoutingRecord, 0, len(records))
	for _, record := range records {
		applyModelRoutingRecord(summary, &requests, record)
	}
	if summary.Status == "not_routed" && len(requests) > 0 {
		summary.Status = "selected"
	}
	for _, request := range requests {
		normalizeLegacyEndpointDeviation(summary, &request)
		if request.Routed != "" {
			summary.RoutedCounts[request.Routed]++
		}
		if request.Outcome != "" {
			summary.OutcomeCounts[request.Outcome]++
		}
		if request.Routed == "deviated" && len(request.Deviations) > 0 {
			appendRoutingDeviation(summary, request, strings.Join(request.Deviations, ","))
		}
	}
	aggregateModelRoutingCosts(summary, requests, usageEntries)
	return summary
}

func readModelRoutingRecords(source io.Reader) ([]modelRoutingRecord, error) {
	scanner := bufio.NewScanner(source)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	records := make([]modelRoutingRecord, 0)
	for scanner.Scan() {
		var record modelRoutingRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			continue
		}
		if !strings.HasPrefix(record.Schema, "model-routing/") {
			continue
		}
		records = append(records, record)
	}
	return records, scanner.Err()
}

func applyModelRoutingRecord(summary *ModelRoutingSummary, requests *[]modelRoutingRecord, record modelRoutingRecord) {
	if summary.Schema == "" {
		summary.Schema = record.Schema
	}
	switch record.Stage {
	case "classification":
		if summary.ClassifierModel == "" {
			summary.ClassifierModel = record.ClassifierModel
			summary.ClassifierEffort = record.ClassifierEffort
			summary.ClassifierAttempts = record.Attempt
		}
	case "selection":
		summary.Status = "selected"
		summary.Objective = record.Objective.Goal
		summary.ObjectiveMode = record.Objective.Mode
		summary.Provider = record.Provider
		summary.Labels = record.Labels
		summary.Mode = record.Mode
		summary.ClassifierModel = record.ClassifierModel
		summary.ClassifierEffort = record.ClassifierEffort
		summary.ClassifierAttempts = record.ClassifierAttempts
		summary.DegradedClassification = record.DegradedClassification
		summary.DegradedReason = record.DegradedReason
		summary.SelectedID = record.SelectedID
		summary.SelectedProvider = record.SelectedProvider
		summary.SelectedModel = record.SelectedModel
		summary.SelectedEffort = record.SelectedEffort
		summary.WireModel = record.WireModel
		summary.Endpoint = record.Endpoint
		summary.RouterName = record.Router.Name
		summary.RouterVersion = record.Router.Version
		summary.LatencyMs = record.LatencyMs
		summary.TopChoices = topModelRoutingChoices(record.RankedChoices)
	case "failure":
		summary.Status = "failed"
		summary.Failure = &ModelRoutingFailure{Code: record.Code, Detail: record.Detail}
	case "request":
		*requests = append(*requests, record)
	}
}

func topModelRoutingChoices(rawChoices []json.RawMessage) []ModelRoutingChoice {
	choices := make([]ModelRoutingChoice, 0, min(3, len(rawChoices)))
	for _, raw := range rawChoices {
		if len(choices) == 3 {
			break
		}
		var choice map[string]any
		if json.Unmarshal(raw, &choice) != nil {
			continue
		}
		choices = append(choices, ModelRoutingChoice{
			ID:       routingChoiceString(choice, "id", "choice_id", "selected_id"),
			Provider: routingChoiceString(choice, "provider", "selected_provider"),
			Model:    routingChoiceString(choice, "model", "selected_model"),
			Effort:   routingChoiceString(choice, "effort", "selected_effort"),
		})
	}
	return choices
}

func routingChoiceString(choice map[string]any, names ...string) string {
	for _, name := range names {
		if value, ok := choice[name].(string); ok {
			return value
		}
	}
	return ""
}

func isAWFBefore02839(schema string) bool {
	var major, minor, patch int
	_, err := fmt.Sscanf(strings.TrimPrefix(strings.TrimPrefix(schema, "model-routing/"), "v"), "%d.%d.%d", &major, &minor, &patch)
	return err == nil && major == 0 && (minor < 28 || (minor == 28 && patch < 39))
}

func appendRoutingDeviation(summary *ModelRoutingSummary, request modelRoutingRecord, deviation string) {
	for i := range summary.Deviations {
		entry := &summary.Deviations[i]
		if entry.RequestedModel == request.RequestedModel && entry.RequestedEffort == request.RequestedEffort &&
			entry.SelectedModel == summary.SelectedModel && entry.SelectedEffort == summary.SelectedEffort && entry.Deviation == deviation {
			entry.Count++
			return
		}
	}
	summary.Deviations = append(summary.Deviations, ModelRoutingDeviation{
		RequestedModel: request.RequestedModel, RequestedEffort: request.RequestedEffort,
		SelectedModel: summary.SelectedModel, SelectedEffort: summary.SelectedEffort,
		Deviation: deviation, Count: 1,
	})
}

func aggregateModelRoutingCosts(summary *ModelRoutingSummary, requests []modelRoutingRecord, usageEntries []TokenUsageEntry) {
	requestKinds := make(map[string]string, len(requests))
	for _, request := range requests {
		normalizeLegacyEndpointDeviation(summary, &request)
		if request.RequestID == "" {
			continue
		}
		switch request.Routed {
		case "as_selected":
			requestKinds[request.RequestID] = "selected"
		case "deviated":
			requestKinds[request.RequestID] = "deviated"
		}
	}

	for _, entry := range usageEntries {
		var bucket *ModelRoutingCost
		switch {
		case entry.Purpose == "routing_classification":
			bucket = &summary.ClassifierCost
		case requestKinds[entry.RequestID] == "selected":
			bucket = &summary.SelectedModelCost
		case requestKinds[entry.RequestID] == "deviated":
			bucket = &summary.DeviatedTrafficCost
		default:
			continue
		}
		bucket.Requests++
		bucket.InputTokens += entry.InputTokens
		bucket.OutputTokens += entry.OutputTokens
		bucket.CacheReadTokens += entry.CacheReadTokens
		bucket.CacheWriteTokens += entry.CacheWriteTokens
		delta, _, valid := parseOptionalNonNegativeFloat(entry.AICreditsThisResponse)
		if !valid {
			inputTokensIncludeCache, _, _ := parseOptionalBool(entry.InputTokensIncludeCache)
			delta = computeModelInferenceAICWithCacheSemantics(entry.Provider, entry.Model, entry.InputTokens, entry.OutputTokens, entry.CacheReadTokens, entry.CacheWriteTokens, entry.ReasoningTokens, inputTokensIncludeCache)
		}
		bucket.AIC += delta
	}
}

func normalizeLegacyEndpointDeviation(summary *ModelRoutingSummary, request *modelRoutingRecord) {
	if request.Routed == "deviated" && slices.Equal(request.Deviations, []string{"endpoint"}) &&
		isAWFBefore02839(summary.Schema) {
		request.Routed = "as_selected"
		summary.EndpointOnlyDeviationNormalized = true
	}
}

type modelRoutingRouteKey struct {
	taskType, scope, complexity, mode, model, effort, routerVersion, effectiveEndpoint, selectedEndpoint string
}

type modelRoutingRouteTotals struct {
	count int
	aic   float64
}

func buildModelRoutingLogsSummary(runs []ProcessedRun) *ModelRoutingLogsSummary {
	totals := make(map[modelRoutingRouteKey]modelRoutingRouteTotals)
	subagentTotals := make(map[string]ModelRoutingAgentCost)
	result := &ModelRoutingLogsSummary{}
	for _, run := range runs {
		routing := run.ModelRouting
		if sessionRouting, found, err := readSessionModelRouting(run.Run.LogsPath); err != nil {
			tokenUsageSubagentLog.Printf("failed to read unified session model routing: %v", err)
		} else if found && sessionRouting.modelRouting() != nil {
			if routing == nil {
				routing = &ModelRoutingSummary{}
			} else {
				routingCopy := *routing
				routing = &routingCopy
			}
			applySessionModelRouting(routing, sessionRouting)
		}
		if routing == nil {
			continue
		}
		result.EndpointOnlyNormalized = result.EndpointOnlyNormalized || routing.EndpointOnlyDeviationNormalized
		result.ClassifierAIC += routing.ClassifierCost.AIC
		addModelRoutingCost(&result.MainAgentCost, routing.MainAgentCost)
		addModelRoutingSubagentTotals(subagentTotals, routing.SubagentCosts)
		requests := 0
		for _, count := range routing.RoutedCounts {
			requests += count
		}
		result.TotalRequests += requests
		result.DeviatedRequests += routing.RoutedCounts["deviated"]
		if routing.Status != "selected" || routing.SelectedModel == "" {
			continue
		}
		key := modelRoutingRouteKey{
			routing.Labels.TaskType, routing.Labels.Scope, routing.Labels.TaskComplexity,
			routing.Mode, routing.SelectedModel, routing.SelectedEffort, routing.RouterVersion,
			routing.EffectiveEndpoint, routing.SelectedEndpoint,
		}
		route := totals[key]
		route.count++
		route.aic += routing.ClassifierCost.AIC + routing.SelectedModelCost.AIC + routing.DeviatedTrafficCost.AIC
		totals[key] = route
	}
	if len(totals) == 0 && result.TotalRequests == 0 && result.ClassifierAIC == 0 && result.MainAgentCost.Requests == 0 && len(subagentTotals) == 0 {
		return nil
	}
	appendModelRoutingSubagentCosts(result, subagentTotals)
	appendModelRoutingRouteSummaries(result, totals)
	if result.TotalRequests > 0 {
		result.DeviatedTrafficShare = float64(result.DeviatedRequests) / float64(result.TotalRequests)
	}
	return result
}

func addModelRoutingSubagentTotals(totals map[string]ModelRoutingAgentCost, agents []ModelRoutingAgentCost) {
	for _, agent := range agents {
		key := agent.AgentName + "\x00" + agent.Effort
		total := totals[key]
		if total.AgentName == "" {
			total.AgentName = agent.AgentName
			total.AgentType = agent.AgentType
			total.Effort = agent.Effort
		}
		total.InstanceCount += agent.InstanceCount
		total.CompletedCount += agent.CompletedCount
		total.FailedCount += agent.FailedCount
		total.IncompleteCount += agent.IncompleteCount
		total.Models = appendUniqueStrings(total.Models, agent.Models...)
		addModelRoutingCost(&total.ModelRoutingCost, agent.ModelRoutingCost)
		totals[key] = total
	}
}

func appendModelRoutingSubagentCosts(summary *ModelRoutingLogsSummary, totals map[string]ModelRoutingAgentCost) {
	for _, cost := range totals {
		slices.Sort(cost.Models)
		summary.SubagentCosts = append(summary.SubagentCosts, cost)
	}
	slices.SortFunc(summary.SubagentCosts, func(a, b ModelRoutingAgentCost) int {
		if order := strings.Compare(a.AgentName, b.AgentName); order != 0 {
			return order
		}
		return strings.Compare(a.Effort, b.Effort)
	})
}

func appendModelRoutingRouteSummaries(summary *ModelRoutingLogsSummary, totals map[modelRoutingRouteKey]modelRoutingRouteTotals) {
	for key, route := range totals {
		summary.Routes = append(summary.Routes, ModelRoutingRouteSummary{
			TaskType: key.taskType, Scope: key.scope, Complexity: key.complexity, Mode: key.mode,
			Model: key.model, Effort: key.effort, RouterVersion: key.routerVersion,
			EffectiveEndpoint: key.effectiveEndpoint, SelectedEndpoint: key.selectedEndpoint,
			RunCount: route.count, TotalAIC: route.aic, AverageAIC: route.aic / float64(route.count),
		})
	}
	slices.SortFunc(summary.Routes, func(left, right ModelRoutingRouteSummary) int {
		leftKey := strings.Join([]string{left.TaskType, left.Scope, left.Complexity, left.Mode, left.Model, left.Effort, left.RouterVersion}, "\x00")
		rightKey := strings.Join([]string{right.TaskType, right.Scope, right.Complexity, right.Mode, right.Model, right.Effort, right.RouterVersion}, "\x00")
		return strings.Compare(leftKey, rightKey)
	})
}

func addModelRoutingCost(total *ModelRoutingCost, next ModelRoutingCost) {
	total.Requests += next.Requests
	total.InputTokens += next.InputTokens
	total.OutputTokens += next.OutputTokens
	total.CacheReadTokens += next.CacheReadTokens
	total.CacheWriteTokens += next.CacheWriteTokens
	total.AIC += next.AIC
}

func appendUniqueStrings(values []string, next ...string) []string {
	for _, value := range next {
		if value != "" && !slices.Contains(values, value) {
			values = append(values, value)
		}
	}
	return values
}
