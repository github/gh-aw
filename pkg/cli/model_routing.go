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
	DisplayNames    []string `json:"display_names,omitempty"`
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
	SelectionSource                 string                  `json:"selection_source,omitempty"`
	FallbackReason                  string                  `json:"fallback_reason,omitempty"`
	FallbackTier                    string                  `json:"fallback_tier,omitempty"`
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
	Observation                     *RoutingObservation     `json:"observation,omitempty"`
	EndpointOnlyDeviationNormalized bool                    `json:"endpoint_only_deviation_normalized,omitempty"`
}

// RoutingObservation is the unified session's verdict on a routed run (T-UAS-094):
// whether its result counts as evidence about the selected pair, and if not, why.
type RoutingObservation struct {
	Outcome string `json:"outcome"`
	Reason  string `json:"reason,omitempty"`
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
	result.RequestedModel, _, _ = strings.Cut(info.RequestedModel, "?")
	if result.RequestedModel == "" && info.ModelSelection != nil {
		result.RequestedModel, _, _ = strings.Cut(info.ModelSelection.ConfiguredModel, "?")
	}
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
	// A router model value (for example router/economy) is what the workflow requested;
	// show it next to the model the router picked.
	if requested := info.requestedRouterValue(); requested != "" {
		result.RequestedModel = requested
	}

	primaryModel := primaryTokenUsageModel(usage)
	routingActive := (info.ModelRouting != nil && info.ModelRouting.Status != "" && info.ModelRouting.Status != "not_routed") ||
		(routing != nil && routing.Status != "" && routing.Status != "not_routed")
	selection := info.ModelSelection
	switch {
	case selection != nil && selection.Status == "unavailable":
		result.Model = ""
	case selection != nil && selection.Status == "substituted":
		result.Model = selection.EffectiveModel
	case info.FallbackModel != "":
		result.Model = info.FallbackModel
	case selection != nil && selection.Status == "observed":
		result.Model = selection.EffectiveModel
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
		if workflowInfo.Model != "" || workflowInfo.ModelSelection != nil {
			sessionInfo.Model = workflowInfo.Model
		}
		if workflowInfo.ModelSelection != nil {
			sessionInfo.ModelSelection = workflowInfo.ModelSelection.awInfo()
		}
		if workflowInfo.RequestedModel != "" {
			sessionInfo.RequestedModel = workflowInfo.RequestedModel
		}
	}
	if modelRouting := sessionRouting.modelRouting(); modelRouting != nil {
		sessionInfo.ModelRouting = modelRouting
	}
	if sessionRouting.ModelConfig != nil {
		sessionInfo.ModelConfig = sessionRouting.ModelConfig
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
	if err := filepath.WalkDir(runDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return nil
		}
		if entry.Name() == "model-routing.jsonl" {
			found = path
			return filepath.SkipAll
		}
		return nil
	}); err != nil {
		tokenUsageSubagentLog.Printf("failed to search for model routing records: %v", err)
	}
	return found
}

func modelRoutingFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func analyzeModelRouting(runDir string) *ModelRoutingSummary {
	sessionRouting, _, sessionErr := readSessionModelRouting(runDir)
	if sessionErr != nil {
		tokenUsageSubagentLog.Printf("failed to read unified session model routing: %v", sessionErr)
	}
	routingPath := findModelRoutingFile(runDir)
	proxyEntries := tokenUsageEntriesForRun(runDir)
	var summary *ModelRoutingSummary
	fromSideLog := false
	hasFirewallProvenance := sessionRouting != nil && sessionRouting.FirewallSelection != nil && sessionRouting.FirewallSelection.SelectionSource != ""
	hasWorkflowProvenance := sessionRouting != nil && sessionRouting.modelRouting() != nil && sessionRouting.modelRouting().SelectionSource != ""
	hasUnifiedProvenance := hasFirewallProvenance || hasWorkflowProvenance
	if hasUnifiedProvenance {
		summary = &ModelRoutingSummary{Status: "not_routed"}
	} else if routingPath != "" {
		summary = parseModelRoutingFile(routingPath, proxyEntries)
		fromSideLog = true
	} else if sessionRouting != nil && (sessionRouting.modelRouting() != nil || sessionRouting.FirewallSelection != nil) {
		summary = &ModelRoutingSummary{Status: "not_routed"}
	}
	summary = applyAwInfoModelRouting(summary, runDir)
	applySessionModelRouting(summary, sessionRouting)
	if !fromSideLog && summary != nil && sessionRouting != nil {
		if len(sessionRouting.FirewallRequests) > 0 {
			applyModelRoutingRequests(summary, sessionRouting.FirewallRequests, proxyEntries)
		} else if routingPath != "" {
			if sideRequests := readSideLogRequestsIfMatchingSelection(routingPath, summary); len(sideRequests) > 0 {
				applyModelRoutingRequests(summary, sideRequests, proxyEntries)
			} else if len(proxyEntries) > 0 {
				applyModelRoutingRequests(summary, nil, proxyEntries)
			}
		} else if len(proxyEntries) > 0 && (sessionRouting.FirewallSelection != nil || hasWorkflowProvenance) {
			applyModelRoutingRequests(summary, nil, proxyEntries)
		}
	}
	_, _, agents, found, err := readSessionSubagentModelsDetailed(runDir)
	if summary != nil && err == nil && found {
		models := make([]string, 0, len(proxyEntries))
		for _, entry := range proxyEntries {
			models = appendUnique(models, entry.Model)
		}

		agentSummary := &TokenUsageSummary{AgentUsage: mergeAgentUsageByDefinition(resolveAgentUsageModels(agents, newModelIdentityResolver(runDir), models...))}
		matchPiAgentUsageCredits(agentSummary, proxyEntries, newModelIdentityResolver(runDir))
		applyAgentUsageToModelRouting(summary, agentSummary.AgentUsage)
	}
	return summary
}

func applyAwInfoModelRouting(routing *ModelRoutingSummary, runDir string) *ModelRoutingSummary {
	if runDir == "" {
		return routing
	}
	awInfoPath := findAwInfoPath(runDir)
	if filepath.Clean(awInfoPath) == filepath.Join(runDir, "agent", "aw_info.json") {
		return routing
	}
	awInfo, err := parseAwInfo(awInfoPath, false)
	if err != nil {
		return routing
	}
	return applyAwInfoModelRoutingInfo(routing, awInfo)
}

func applyAwInfoModelRoutingInfo(routing *ModelRoutingSummary, awInfo *AwInfo) *ModelRoutingSummary {
	if awInfo == nil || awInfo.ModelRouting == nil {
		return routing
	}
	if routing == nil {
		routing = &ModelRoutingSummary{Status: awInfo.ModelRouting.Status}
	} else {
		copy := *routing
		routing = &copy
	}
	routing.EffectiveEndpoint = awInfo.ModelRouting.Endpoint
	routing.SelectedEndpoint = awInfo.ModelRouting.SelectedEndpoint
	return routing
}

func applySessionModelRouting(summary *ModelRoutingSummary, session *sessionModelRoutingAttribution) {
	if summary == nil || session == nil {
		return
	}
	summary.Observation = session.Observation
	hasFirewallProvenance := session.FirewallSelection != nil && session.FirewallSelection.SelectionSource != ""
	if sel := session.FirewallSelection; sel != nil {
		applySessionFirewallSelection(summary, sel)
	}
	if hasFirewallProvenance && session.FirewallFailure != nil {
		failure := *session.FirewallFailure
		summary.Status = "failed"
		summary.Failure = &failure
	}
	routing := session.modelRouting()
	if routing == nil {
		return
	}
	if routing.Status != "" {
		summary.Status = routing.Status
	}
	if routing.Endpoint != "" {
		summary.EffectiveEndpoint = routing.Endpoint
	}
	if routing.SelectedEndpoint != "" {
		summary.SelectedEndpoint = routing.SelectedEndpoint
	}
	if routing.FailureCode != "" && summary.Failure == nil {
		summary.Failure = &ModelRoutingFailure{Code: routing.FailureCode}
	}
	if hasFirewallProvenance {
		return
	}
	if routing.SelectionSource != "" {
		// When workflow.info carries explicit provenance without a firewall selection
		// event, apply its selection metadata as an authoritative unit.
		summary.SelectionSource = routing.SelectionSource
		summary.FallbackReason = routing.FallbackReason
		summary.FallbackTier = routing.FallbackTier
		summary.Provider = routing.Provider
		summary.WireModel = routing.WireModel
		summary.SelectedModel = firstNonEmpty(routing.Model, routing.WireModel)
		summary.SelectedEffort = routing.Effort
		summary.Mode = routing.Mode
		summary.SelectedID = routing.SelectedID
		summary.RouterVersion = routing.RouterVersion
		return
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
	if routing.Mode != "" && summary.Mode == "" {
		summary.Mode = routing.Mode
	}
	if routing.SelectedID != "" && summary.SelectedID == "" {
		summary.SelectedID = routing.SelectedID
	}
	if routing.RouterVersion != "" && summary.RouterVersion == "" {
		summary.RouterVersion = routing.RouterVersion
	}
	if summary.SelectionSource != "router" {
		if routing.FallbackReason != "" && summary.FallbackReason == "" {
			summary.FallbackReason = routing.FallbackReason
		}
		if routing.FallbackTier != "" && summary.FallbackTier == "" {
			summary.FallbackTier = routing.FallbackTier
		}
	}
}

func readSideLogRequestsIfMatchingSelection(path string, summary *ModelRoutingSummary) []modelRoutingRecord {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			tokenUsageSubagentLog.Printf("failed to close model routing records: %v", closeErr)
		}
	}()
	records, err := readModelRoutingRecords(file)
	if err != nil {
		return nil
	}
	var lastSelection *modelRoutingRecord
	var requests []modelRoutingRecord
	for i := range records {
		switch records[i].Stage {
		case "selection":
			lastSelection = &records[i]
		case "request":
			requests = append(requests, records[i])
		}
	}
	if lastSelection != nil {
		sideModel := firstNonEmpty(lastSelection.SelectedModel, lastSelection.WireModel)
		summaryModel := firstNonEmpty(summary.SelectedModel, summary.WireModel)
		if sideModel != "" && summaryModel != "" && sideModel != summaryModel &&
			lastSelection.WireModel != summary.WireModel {
			return nil
		}
		if lastSelection.SelectedEffort != "" && summary.SelectedEffort != "" &&
			lastSelection.SelectedEffort != summary.SelectedEffort {
			return nil
		}
	}
	return requests
}

func applySessionFirewallSelection(summary *ModelRoutingSummary, sel *sessionFirewallModelRoutingSelection) {
	if summary.Status == "" || summary.Status == "not_routed" {
		summary.Status = "selected"
	}
	// When the unified session carries explicit selection provenance, apply the
	// entire selection as a coherent unit so contradictory side-log fields cannot
	// leak into the provenance-bearing unified record.
	if sel.SelectionSource != "" {
		summary.SelectionSource = sel.SelectionSource
		summary.FallbackReason = sel.FallbackReason
		summary.FallbackTier = sel.FallbackTier
		if sel.Schema != "" {
			summary.Schema = sel.Schema
		}
		if sel.Objective != nil {
			summary.Objective = sel.Objective.Goal
			summary.ObjectiveMode = sel.Objective.Mode
		} else {
			summary.Objective = ""
			summary.ObjectiveMode = ""
		}
		summary.Provider = sel.Provider
		if sel.Labels != nil {
			summary.Labels = *sel.Labels
		} else {
			summary.Labels = ModelRoutingLabels{}
		}
		summary.Mode = sel.Mode
		summary.ClassifierModel = sel.ClassifierModel
		summary.ClassifierEffort = sel.ClassifierEffort
		summary.ClassifierAttempts = sel.ClassifierAttempts
		summary.DegradedClassification = sel.DegradedClassification
		summary.DegradedReason = sel.DegradedReason
		summary.SelectedID = sel.SelectedID
		summary.SelectedProvider = sel.SelectedProvider
		summary.SelectedModel = sel.SelectedModel
		summary.SelectedEffort = sel.SelectedEffort
		summary.WireModel = sel.WireModel
		summary.Endpoint = sel.Endpoint
		if sel.Router != nil {
			summary.RouterName = sel.Router.Name
			summary.RouterVersion = sel.Router.Version
		} else {
			summary.RouterName = ""
			summary.RouterVersion = ""
		}
		summary.LatencyMs = sel.LatencyMs
		if len(sel.RankedChoices) > 0 {
			summary.TopChoices = topModelRoutingChoices(sel.RankedChoices)
		} else {
			summary.TopChoices = nil
		}
		return
	}
	if sel.FallbackReason != "" {
		summary.FallbackReason = sel.FallbackReason
	}
	if sel.FallbackTier != "" {
		summary.FallbackTier = sel.FallbackTier
	}
	if summary.Schema == "" {
		summary.Schema = sel.Schema
	}
	if sel.Objective != nil {
		if summary.Objective == "" {
			summary.Objective = sel.Objective.Goal
		}
		if summary.ObjectiveMode == "" {
			summary.ObjectiveMode = sel.Objective.Mode
		}
	}
	if summary.Provider == "" {
		summary.Provider = sel.Provider
	}
	if sel.Labels != nil && summary.Labels == (ModelRoutingLabels{}) {
		summary.Labels = *sel.Labels
	}
	if summary.Mode == "" {
		summary.Mode = sel.Mode
	}
	if summary.ClassifierModel == "" {
		summary.ClassifierModel = sel.ClassifierModel
	}
	if summary.ClassifierEffort == "" {
		summary.ClassifierEffort = sel.ClassifierEffort
	}
	if summary.ClassifierAttempts == 0 {
		summary.ClassifierAttempts = sel.ClassifierAttempts
	}
	if !summary.DegradedClassification && sel.DegradedClassification {
		summary.DegradedClassification = sel.DegradedClassification
	}
	if summary.DegradedReason == "" {
		summary.DegradedReason = sel.DegradedReason
	}
	if summary.SelectedID == "" {
		summary.SelectedID = sel.SelectedID
	}
	if summary.SelectedProvider == "" {
		summary.SelectedProvider = sel.SelectedProvider
	}
	if summary.SelectedModel == "" {
		summary.SelectedModel = sel.SelectedModel
	}
	if summary.SelectedEffort == "" {
		summary.SelectedEffort = sel.SelectedEffort
	}
	if summary.WireModel == "" {
		summary.WireModel = sel.WireModel
	}
	if summary.Endpoint == "" {
		summary.Endpoint = sel.Endpoint
	}
	if sel.Router != nil {
		if summary.RouterName == "" {
			summary.RouterName = sel.Router.Name
		}
		if summary.RouterVersion == "" {
			summary.RouterVersion = sel.Router.Version
		}
	}
	if summary.LatencyMs == 0 {
		summary.LatencyMs = sel.LatencyMs
	}
	if len(summary.TopChoices) == 0 && len(sel.RankedChoices) > 0 {
		summary.TopChoices = topModelRoutingChoices(sel.RankedChoices)
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
		displayNames := append([]string(nil), agent.DisplayNames...)
		slices.Sort(displayNames)
		summary.SubagentCosts = append(summary.SubagentCosts, ModelRoutingAgentCost{
			AgentName: agent.AgentName, DisplayNames: displayNames, AgentType: agent.AgentType, InstanceCount: agent.InstanceCount,
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
	if len(entries) > 0 {
		return entries
	}
	rawPath := findTokenUsageFile(runDir)
	if rawPath == "" {
		return entries
	}
	entries, _, err = scanTokenUsageEntries(rawPath)
	if err != nil {
		tokenUsageSubagentLog.Printf("failed to read raw firewall token usage: %v", err)
		return nil
	}
	return entries
}

func parseModelRoutingFile(path string, usageEntries []TokenUsageEntry) *ModelRoutingSummary {
	file, err := os.Open(path)
	if err != nil {
		return &ModelRoutingSummary{Status: "unavailable"}
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			tokenUsageSubagentLog.Printf("failed to close model routing records: %v", closeErr)
		}
	}()

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
	applyModelRoutingRequests(summary, requests, usageEntries)
	return summary
}

func applyModelRoutingRequests(summary *ModelRoutingSummary, requests []modelRoutingRecord, usageEntries []TokenUsageEntry) {
	if summary.RoutedCounts == nil {
		summary.RoutedCounts = make(map[string]int)
	}
	if summary.OutcomeCounts == nil {
		summary.OutcomeCounts = make(map[string]int)
	}
	for _, request := range requests {
		if summary.Schema == "" && request.Schema != "" {
			summary.Schema = request.Schema
		}
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

	for _, entry := range tokenUsageEntriesWithCreditDeltas(usageEntries) {
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
		delta := tokenUsageEntryCredits(entry)
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
	taskType, scope, complexity, mode, model, effort, routerVersion string
	selectionSource, fallbackReason, fallbackTier                   string
	endpoint, effectiveEndpoint, selectedEndpoint                   string
}

type modelRoutingRouteTotals struct {
	count          int
	aic            float64
	explicitRouter int
}

func buildModelRoutingLogsSummary(runs []ProcessedRun) *ModelRoutingLogsSummary {
	totals := make(map[modelRoutingRouteKey]modelRoutingRouteTotals)
	subagentTotals := make(map[string]ModelRoutingAgentCost)
	result := &ModelRoutingLogsSummary{}
	for _, run := range runs {
		hadPrecomputedRouting := run.ModelRouting != nil
		routing := applyAwInfoModelRouting(run.ModelRouting, run.Run.LogsPath)
		if sessionRouting, _, err := readSessionModelRouting(run.Run.LogsPath); err != nil {
			tokenUsageSubagentLog.Printf("failed to read unified session model routing: %v", err)
		} else if sessionRouting != nil && (sessionRouting.modelRouting() != nil || sessionRouting.FirewallSelection != nil) {
			if routing == nil {
				routing = &ModelRoutingSummary{}
			} else {
				routingCopy := *routing
				routing = &routingCopy
			}
			applySessionModelRouting(routing, sessionRouting)
			if !hadPrecomputedRouting && sessionRouting.FirewallSelection != nil && len(sessionRouting.FirewallRequests) > 0 {
				applyModelRoutingRequests(routing, sessionRouting.FirewallRequests, tokenUsageEntriesForRun(run.Run.LogsPath))
			}
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
		routeSource := normalizedRouteSelectionSource(routing.SelectionSource)
		fallbackReason, fallbackTier := "", ""
		if routeSource == "fallback" {
			fallbackReason = routing.FallbackReason
			fallbackTier = routing.FallbackTier
		}
		key := modelRoutingRouteKey{
			routing.Labels.TaskType, routing.Labels.Scope, routing.Labels.TaskComplexity,
			routing.Mode, routing.SelectedModel, routing.SelectedEffort, routing.RouterVersion,
			routeSource, fallbackReason, fallbackTier,
			routing.Endpoint, routing.EffectiveEndpoint, routing.SelectedEndpoint,
		}
		route := totals[key]
		route.count++
		if routing.SelectionSource == "router" {
			route.explicitRouter++
		}
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
		total.DisplayNames = appendUniqueStrings(total.DisplayNames, agent.DisplayNames...)
		addModelRoutingCost(&total.ModelRoutingCost, agent.ModelRoutingCost)
		totals[key] = total
	}
}

func appendModelRoutingSubagentCosts(summary *ModelRoutingLogsSummary, totals map[string]ModelRoutingAgentCost) {
	for _, cost := range totals {
		slices.Sort(cost.Models)
		slices.Sort(cost.DisplayNames)
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
		selectionSource := key.selectionSource
		if selectionSource == "" && route.explicitRouter == route.count {
			selectionSource = "router"
		}
		summary.Routes = append(summary.Routes, ModelRoutingRouteSummary{
			TaskType: key.taskType, Scope: key.scope, Complexity: key.complexity, Mode: key.mode,
			Model: key.model, Effort: key.effort, RouterVersion: key.routerVersion,
			SelectionSource: selectionSource, FallbackReason: key.fallbackReason, FallbackTier: key.fallbackTier,
			Endpoint: key.endpoint, EffectiveEndpoint: key.effectiveEndpoint, SelectedEndpoint: key.selectedEndpoint,
			RunCount: route.count, TotalAIC: route.aic, AverageAIC: route.aic / float64(route.count),
		})
	}
	slices.SortFunc(summary.Routes, func(left, right ModelRoutingRouteSummary) int {
		leftKey := strings.Join([]string{left.TaskType, left.Scope, left.Complexity, left.Mode, left.Model, left.Effort, left.RouterVersion, left.SelectionSource, left.FallbackReason, left.FallbackTier, left.Endpoint, left.EffectiveEndpoint, left.SelectedEndpoint}, "\x00")
		rightKey := strings.Join([]string{right.TaskType, right.Scope, right.Complexity, right.Mode, right.Model, right.Effort, right.RouterVersion, right.SelectionSource, right.FallbackReason, right.FallbackTier, right.Endpoint, right.EffectiveEndpoint, right.SelectedEndpoint}, "\x00")
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
