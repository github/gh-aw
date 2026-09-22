package cli

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/scanfindings"
	"github.com/github/gh-aw/pkg/timeutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var auditAgenticLog = logger.New("cli:audit_agentic_analysis")

// TaskDomainInfo describes the dominant task type inferred for a workflow run.
type TaskDomainInfo struct {
	Name   string `json:"name"`
	Label  string `json:"label"`
	Reason string `json:"reason,omitempty"`
}

// BehaviorFingerprint summarizes the run's execution profile in compact dimensions.
type BehaviorFingerprint struct {
	ExecutionStyle  string  `json:"execution_style"`
	ToolBreadth     string  `json:"tool_breadth"`
	ActuationStyle  string  `json:"actuation_style"`
	ResourceProfile string  `json:"resource_profile"`
	DispatchMode    string  `json:"dispatch_mode"`
	AgenticFraction float64 `json:"agentic_fraction"` // Ratio of reasoning turns to total turns (0.0-1.0)
}

// AgenticAssessment captures an actionable judgment about the run's behavior.
type AgenticAssessment struct {
	Kind           string `json:"kind"`
	Severity       string `json:"severity"`
	Summary        string `json:"summary"`
	Evidence       string `json:"evidence,omitempty"`
	Recommendation string `json:"recommendation,omitempty"`
}

func buildToolUsageInfo(metrics LogMetrics) []ToolUsageInfo {
	toolStats := make(map[string]*ToolUsageInfo)

	for _, toolCall := range metrics.ToolCalls {
		displayKey := workflow.PrettifyToolName(toolCall.Name)
		if existing, exists := toolStats[displayKey]; exists {
			existing.CallCount += toolCall.CallCount
			if toolCall.MaxInputSize > existing.MaxInputSize {
				existing.MaxInputSize = toolCall.MaxInputSize
			}
			if toolCall.MaxOutputSize > existing.MaxOutputSize {
				existing.MaxOutputSize = toolCall.MaxOutputSize
				// Keep the sample from the call with the largest output.
				if toolCall.OutputSample != "" {
					existing.OutputSample = toolCall.OutputSample
				}
			}
			if toolCall.MaxDuration > 0 {
				maxDuration := timeutil.FormatDuration(toolCall.MaxDuration)
				if existing.MaxDuration == "" || toolCall.MaxDuration > parseDurationString(existing.MaxDuration) {
					existing.MaxDuration = maxDuration
				}
			}
			continue
		}

		info := &ToolUsageInfo{
			Name:          displayKey,
			CallCount:     toolCall.CallCount,
			MaxInputSize:  toolCall.MaxInputSize,
			MaxOutputSize: toolCall.MaxOutputSize,
			OutputSample:  toolCall.OutputSample,
		}
		if toolCall.MaxDuration > 0 {
			info.MaxDuration = timeutil.FormatDuration(toolCall.MaxDuration)
		}
		toolStats[displayKey] = info
	}

	toolUsage := make([]ToolUsageInfo, 0, len(toolStats))
	for _, info := range toolStats {
		toolUsage = append(toolUsage, *info)
	}

	slices.SortFunc(toolUsage, func(a, b ToolUsageInfo) int {
		if a.CallCount != b.CallCount {
			return b.CallCount - a.CallCount
		}
		return strings.Compare(a.Name, b.Name)
	})

	return toolUsage
}

func mergeMCPToolUsageInfo(toolUsage []ToolUsageInfo, mcpToolUsage *MCPToolUsageData) []ToolUsageInfo {
	if mcpToolUsage == nil {
		return toolUsage
	}
	toolStats := cloneToolUsageStats(toolUsage)
	if len(mcpToolUsage.Summary) > 0 {
		mergeMCPToolUsageSummaries(toolStats, mcpToolUsage.Summary)
	} else {
		mergeMCPToolUsageCalls(toolStats, mcpToolUsage.ToolCalls)
	}
	return sortedToolUsageStats(toolStats)
}

func cloneToolUsageStats(toolUsage []ToolUsageInfo) map[string]*ToolUsageInfo {
	toolStats := make(map[string]*ToolUsageInfo)
	for _, info := range toolUsage {
		cloned := info
		toolStats[info.Name] = &cloned
	}
	return toolStats
}

func addOrUpdateToolUsage(toolStats map[string]*ToolUsageInfo, name string, callCount, maxInputSize, maxOutputSize int, maxDuration string) {
	normalizedName := strings.TrimSpace(name)
	if normalizedName == "" {
		return
	}
	displayKey := workflow.PrettifyToolName(normalizedName)
	if existing, exists := toolStats[displayKey]; exists {
		updateExistingToolUsage(existing, callCount, maxInputSize, maxOutputSize, maxDuration)
		return
	}
	toolStats[displayKey] = &ToolUsageInfo{
		Name:          displayKey,
		CallCount:     callCount,
		MaxInputSize:  maxInputSize,
		MaxOutputSize: maxOutputSize,
		MaxDuration:   maxDuration,
	}
}

func updateExistingToolUsage(existing *ToolUsageInfo, callCount, maxInputSize, maxOutputSize int, maxDuration string) {
	existing.CallCount += callCount
	if maxInputSize > existing.MaxInputSize {
		existing.MaxInputSize = maxInputSize
	}
	if maxOutputSize > existing.MaxOutputSize {
		existing.MaxOutputSize = maxOutputSize
	}
	if maxDuration == "" {
		return
	}
	maxDurationValue := parseDurationString(maxDuration)
	if existing.MaxDuration == "" || maxDurationValue > parseDurationString(existing.MaxDuration) {
		existing.MaxDuration = maxDuration
	}
}

func mergeMCPToolUsageSummaries(toolStats map[string]*ToolUsageInfo, summaries []MCPToolSummary) {
	for _, summary := range summaries {
		toolSummary := summary
		toolSummary.syncFieldsFromBase()
		switch {
		case toolSummary.ServerName != "" && toolSummary.ToolName != "":
			addOrUpdateToolUsage(toolStats, toolSummary.ServerName+"."+toolSummary.ToolName, toolSummary.CallCount, toolSummary.MaxInputSize, toolSummary.MaxOutputSize, toolSummary.MaxDuration)
		case toolSummary.ToolName != "":
			addOrUpdateToolUsage(toolStats, toolSummary.ToolName, toolSummary.CallCount, toolSummary.MaxInputSize, toolSummary.MaxOutputSize, toolSummary.MaxDuration)
		}
	}
}

func mergeMCPToolUsageCalls(toolStats map[string]*ToolUsageInfo, calls []MCPToolCall) {
	for _, call := range calls {
		switch {
		case call.ServerName != "" && call.ToolName != "":
			addOrUpdateToolUsage(toolStats, call.ServerName+"."+call.ToolName, 1, call.InputSize, call.OutputSize, call.Duration)
		case call.ToolName != "":
			addOrUpdateToolUsage(toolStats, call.ToolName, 1, call.InputSize, call.OutputSize, call.Duration)
		}
	}
}

func sortedToolUsageStats(toolStats map[string]*ToolUsageInfo) []ToolUsageInfo {
	mergedToolUsage := make([]ToolUsageInfo, 0, len(toolStats))
	for _, info := range toolStats {
		mergedToolUsage = append(mergedToolUsage, *info)
	}
	slices.SortFunc(mergedToolUsage, func(a, b ToolUsageInfo) int {
		if a.CallCount != b.CallCount {
			return b.CallCount - a.CallCount
		}
		return strings.Compare(a.Name, b.Name)
	})
	return mergedToolUsage
}

func deriveRunAgenticAnalysis(processedRun ProcessedRun, metrics LogMetrics) (*AwContext, []ToolUsageInfo, []CreatedItemReport, *TaskDomainInfo, *BehaviorFingerprint, []AgenticAssessment) {
	auditAgenticLog.Printf("Deriving agentic analysis for run: id=%d workflow=%s", processedRun.Run.DatabaseID, processedRun.Run.WorkflowName)
	var awContext *AwContext
	if processedRun.AwContext != nil {
		awContext = processedRun.AwContext
	} else if processedRun.Run.LogsPath != "" {
		awInfoPath := filepath.Join(processedRun.Run.LogsPath, "aw_info.json")
		if info, err := parseAwInfo(awInfoPath, false); err == nil && info != nil {
			awContext = info.Context
		}
	}

	toolUsage := buildToolUsageInfo(metrics)
	createdItems := resolveCreatedItems(processedRun.Run.LogsPath, processedRun.SafeOutputs)
	metricsData := MetricsData{
		TokenUsage:    processedRun.Run.TokenUsage,
		ActionMinutes: processedRun.Run.ActionMinutes,
		Turns:         processedRun.Run.Turns,
		ErrorCount:    processedRun.Run.ErrorCount,
		WarningCount:  processedRun.Run.WarningCount,
	}

	taskDomain := detectTaskDomain(processedRun, createdItems, toolUsage, awContext)
	behaviorFingerprint := buildBehaviorFingerprint(processedRun, metricsData, toolUsage, createdItems, awContext)
	agenticAssessments := buildAgenticAssessments(processedRun, metricsData, toolUsage, createdItems, taskDomain, behaviorFingerprint, awContext)

	auditAgenticLog.Printf("Agentic analysis complete: tool_types=%d created_items=%d assessments=%d", len(toolUsage), len(createdItems), len(agenticAssessments))
	return awContext, toolUsage, createdItems, taskDomain, behaviorFingerprint, agenticAssessments
}

func detectTaskDomain(processedRun ProcessedRun, createdItems []CreatedItemReport, toolUsage []ToolUsageInfo, awContext *AwContext) *TaskDomainInfo {
	auditAgenticLog.Printf("Detecting task domain for run: workflow=%s event=%s", processedRun.Run.WorkflowName, processedRun.Run.Event)
	combined := strings.ToLower(strings.Join([]string{
		processedRun.Run.WorkflowName,
		processedRun.Run.WorkflowPath,
		processedRun.Run.Event,
	}, " "))

	createdTypes := make([]string, 0, len(createdItems))
	for _, item := range createdItems {
		createdTypes = append(createdTypes, strings.ToLower(item.Type))
	}
	createdJoined := strings.Join(createdTypes, " ")

	toolNames := make([]string, 0, len(toolUsage))
	for _, tool := range toolUsage {
		toolNames = append(toolNames, strings.ToLower(tool.Name))
	}
	toolJoined := strings.Join(toolNames, " ")

	switch {
	case containsAny(combined, "release", "deploy", "publish", "backport", "changelog"):
		return &TaskDomainInfo{Name: "release_ops", Label: "Release / Ops", Reason: "Workflow metadata matches release or operational automation."}
	case containsAny(combined, "research", "investigat", "analysis", "analy", "report", "audit"):
		return &TaskDomainInfo{Name: "research", Label: "Research", Reason: "Workflow naming and instructions suggest exploratory analysis or reporting."}
	case containsAny(combined, "triage", "label", "classif", "route") || containsAny(createdJoined, "add_labels", "remove_labels", "set_issue_type"):
		return &TaskDomainInfo{Name: "triage", Label: "Triage", Reason: "The run focused on classification, routing, or issue state updates."}
	case containsAny(combined, "fix", "patch", "repair", "refactor", "swe", "code", "review") || containsAny(createdJoined, "create_pull_request_review_comment", "submit_pull_request_review"):
		return &TaskDomainInfo{Name: "code_fix", Label: "Code Fix", Reason: "The workflow appears oriented toward code changes or pull request review."}
	case containsAny(combined, "cleanup", "maint", "update", "deps", "sync", "housekeeping"):
		return &TaskDomainInfo{Name: "repo_maintenance", Label: "Repo Maintenance", Reason: "Workflow metadata matches repository maintenance or update work."}
	case containsAny(combined, "issue", "discussion", "comment", "support", "reply") || containsAny(createdJoined, "add_comment", "create_discussion"):
		return &TaskDomainInfo{Name: "issue_response", Label: "Issue Response", Reason: "The run is primarily interacting with issue, discussion, or comment threads."}
	case awContext != nil:
		return &TaskDomainInfo{Name: "delegated_automation", Label: "Delegated Automation", Reason: "The run was dispatched from an upstream workflow and is acting as a delegated task."}
	case containsAny(toolJoined, "github_issue_read", "github-discussion-query"):
		return &TaskDomainInfo{Name: "issue_response", Label: "Issue Response", Reason: "Tool usage centers on repository conversations and issue context."}
	default:
		return &TaskDomainInfo{Name: "general_automation", Label: "General Automation", Reason: "The run does not strongly match a narrower workflow domain yet."}
	}
}

func buildBehaviorFingerprint(processedRun ProcessedRun, metrics MetricsData, toolUsage []ToolUsageInfo, createdItems []CreatedItemReport, awContext *AwContext) *BehaviorFingerprint {
	auditAgenticLog.Printf("Building behavior fingerprint: run_id=%d turns=%d tool_types=%d created_items=%d", processedRun.Run.DatabaseID, metrics.Turns, len(toolUsage), len(createdItems))
	toolTypes := len(toolUsage)
	writeCount := len(createdItems) + processedRun.Run.SafeItemsCount

	executionStyle := "directed"
	switch {
	case metrics.Turns >= 10 || toolTypes >= 6:
		executionStyle = "exploratory"
	case metrics.Turns >= 5 || toolTypes >= 4:
		executionStyle = "adaptive"
	}

	toolBreadth := "narrow"
	switch {
	case toolTypes >= 6:
		toolBreadth = "broad"
	case toolTypes >= 3:
		toolBreadth = "moderate"
	}

	actuationStyle := "read_only"
	switch {
	case writeCount >= 6:
		actuationStyle = "write_heavy"
	case writeCount > 0:
		actuationStyle = "selective_write"
	}

	resourceProfile := "lean"
	switch {
	case processedRun.Run.Duration >= 15*time.Minute || metrics.Turns >= 12 || toolTypes >= 6 || writeCount >= 8:
		resourceProfile = "heavy"
	case processedRun.Run.Duration >= 5*time.Minute || metrics.Turns >= 6 || toolTypes >= 4 || writeCount >= 3:
		resourceProfile = "moderate"
	}

	dispatchMode := "standalone"
	if awContext != nil {
		dispatchMode = "delegated"
	}

	agenticFraction := computeAgenticFraction(processedRun)

	auditAgenticLog.Printf("Behavior fingerprint: execution=%s breadth=%s actuation=%s resource=%s dispatch=%s agentic_fraction=%.2f", executionStyle, toolBreadth, actuationStyle, resourceProfile, dispatchMode, agenticFraction)
	return &BehaviorFingerprint{
		ExecutionStyle:  executionStyle,
		ToolBreadth:     toolBreadth,
		ActuationStyle:  actuationStyle,
		ResourceProfile: resourceProfile,
		DispatchMode:    dispatchMode,
		AgenticFraction: agenticFraction,
	}
}

func buildAgenticAssessments(processedRun ProcessedRun, metrics MetricsData, toolUsage []ToolUsageInfo, createdItems []CreatedItemReport, domain *TaskDomainInfo, fingerprint *BehaviorFingerprint, awContext *AwContext) []AgenticAssessment {
	if domain == nil || fingerprint == nil {
		auditAgenticLog.Print("Skipping agentic assessments: missing domain or fingerprint")
		return nil
	}
	auditAgenticLog.Printf("Building agentic assessments: run_id=%d domain=%s resource=%s execution=%s", processedRun.Run.DatabaseID, domain.Name, fingerprint.ResourceProfile, fingerprint.ExecutionStyle)

	assessments := make([]AgenticAssessment, 0, 4)
	toolTypes := len(toolUsage)
	frictionEvents := len(processedRun.MissingTools) + len(processedRun.MCPFailures) + len(processedRun.MissingData)
	writeCount := len(createdItems) + processedRun.Run.SafeItemsCount

	assessments = appendResourceHeavyAssessment(assessments, processedRun, metrics, domain, fingerprint, toolTypes, writeCount)
	assessments = appendOverkillAssessment(assessments, metrics, domain, fingerprint, toolTypes)
	assessments = appendPoorControlAssessment(assessments, domain, fingerprint, frictionEvents, writeCount)
	assessments = appendPartiallyReducibleAssessment(assessments, metrics, fingerprint)
	assessments = appendModelDowngradeAssessment(assessments, domain, fingerprint)
	assessments = appendDelegatedContextAssessment(assessments, awContext)

	auditAgenticLog.Printf("Built %d agentic assessments", len(assessments))
	return assessments
}

func appendResourceHeavyAssessment(assessments []AgenticAssessment, processedRun ProcessedRun, metrics MetricsData, domain *TaskDomainInfo, fingerprint *BehaviorFingerprint, toolTypes, writeCount int) []AgenticAssessment {
	if fingerprint.ResourceProfile == "heavy" {
		severity := "medium"
		if metrics.Turns >= 14 || toolTypes >= 7 || processedRun.Run.Duration >= 20*time.Minute {
			severity = "high"
		}
		assessments = append(assessments, AgenticAssessment{
			Kind:           "resource_heavy_for_domain",
			Severity:       severity,
			Summary:        fmt.Sprintf("This %s run consumed a heavy execution profile for its task shape.", domain.Label),
			Evidence:       fmt.Sprintf("turns=%d tool_types=%d duration=%s write_actions=%d", metrics.Turns, toolTypes, formatAssessmentDuration(processedRun.Run.Duration), writeCount),
			Recommendation: "Compare this run to similar successful runs and trim unnecessary turns, tools, or write actions.",
		})
	}
	return assessments
}

func appendOverkillAssessment(assessments []AgenticAssessment, metrics MetricsData, domain *TaskDomainInfo, fingerprint *BehaviorFingerprint, toolTypes int) []AgenticAssessment {
	if (domain.Name == "triage" || domain.Name == "repo_maintenance" || domain.Name == "issue_response") && fingerprint.ResourceProfile == "lean" && fingerprint.ExecutionStyle == "directed" && fingerprint.ToolBreadth == "narrow" {
		assessments = append(assessments, AgenticAssessment{
			Kind:           "overkill_for_agentic",
			Severity:       "low",
			Summary:        fmt.Sprintf("This %s run looks stable enough that deterministic automation may be a simpler fit.", domain.Label),
			Evidence:       fmt.Sprintf("turns=%d tool_types=%d actuation=%s", metrics.Turns, toolTypes, fingerprint.ActuationStyle),
			Recommendation: "Consider whether a scripted rule or deterministic workflow step could replace this agentic path.",
		})
	}
	return assessments
}

func appendPoorControlAssessment(assessments []AgenticAssessment, domain *TaskDomainInfo, fingerprint *BehaviorFingerprint, frictionEvents, writeCount int) []AgenticAssessment {
	if frictionEvents >= 3 || (frictionEvents > 0 && writeCount >= 3) || ((domain.Name == "triage" || domain.Name == "repo_maintenance" || domain.Name == "issue_response") && fingerprint.ExecutionStyle == "exploratory") {
		severity := "medium"
		if frictionEvents >= 4 || (frictionEvents > 0 && fingerprint.ActuationStyle == "write_heavy") {
			severity = "high"
		}
		assessments = append(assessments, AgenticAssessment{
			Kind:           "poor_agentic_control",
			Severity:       severity,
			Summary:        "The run showed signs of broad or weakly controlled agentic behavior.",
			Evidence:       fmt.Sprintf("friction=%d execution=%s actuation=%s", frictionEvents, fingerprint.ExecutionStyle, fingerprint.ActuationStyle),
			Recommendation: "Tighten instructions, reduce unnecessary tools, or delay write actions until the workflow has stronger evidence.",
		})
	}
	return assessments
}

func appendPartiallyReducibleAssessment(assessments []AgenticAssessment, metrics MetricsData, fingerprint *BehaviorFingerprint) []AgenticAssessment {
	// Partially reducible: the workflow has a low agentic fraction, meaning
	// many turns are data-gathering that could be moved to deterministic steps:
	// or post-steps: in the frontmatter. Only flag when there's substantive work
	// (not lean/directed runs which overkill_for_agentic already covers).
	if fingerprint.AgenticFraction > 0 && fingerprint.AgenticFraction < 0.6 &&
		fingerprint.ResourceProfile != "lean" {
		severity := "low"
		if fingerprint.AgenticFraction < 0.4 {
			severity = "medium"
		}
		deterministicPct := int((1.0 - fingerprint.AgenticFraction) * 100)
		assessments = append(assessments, AgenticAssessment{
			Kind:           "partially_reducible",
			Severity:       severity,
			Summary:        fmt.Sprintf("About %d%% of this run's turns appear to be data-gathering that could move to deterministic steps.", deterministicPct),
			Evidence:       fmt.Sprintf("agentic_fraction=%.2f turns=%d", fingerprint.AgenticFraction, metrics.Turns),
			Recommendation: "Move data-fetching work to frontmatter steps: (pre-agent) writing to /tmp/gh-aw/agent/ or post-steps: (post-agent) to reduce inference cost. See the DeterministicOps guide.",
		})
	}
	return assessments
}

func appendModelDowngradeAssessment(assessments []AgenticAssessment, domain *TaskDomainInfo, fingerprint *BehaviorFingerprint) []AgenticAssessment {
	// Model downgrade suggestion: the run uses a heavy resource profile but
	// the task domain is simple enough that a smaller model would likely suffice.
	if fingerprint.ResourceProfile != "lean" &&
		(domain.Name == "triage" || domain.Name == "repo_maintenance" || domain.Name == "issue_response") &&
		fingerprint.ActuationStyle != "write_heavy" {
		assessments = append(assessments, AgenticAssessment{
			Kind:           "model_downgrade_available",
			Severity:       "low",
			Summary:        fmt.Sprintf("This %s run may not need a frontier model. A smaller model (e.g. gpt-4.1-mini, claude-haiku-4-5) could handle the task at lower cost.", domain.Label),
			Evidence:       fmt.Sprintf("domain=%s resource_profile=%s actuation=%s", domain.Name, fingerprint.ResourceProfile, fingerprint.ActuationStyle),
			Recommendation: "Try engine.model: gpt-4.1-mini or claude-haiku-4-5 in the workflow frontmatter.",
		})
	}
	return assessments
}

func appendDelegatedContextAssessment(assessments []AgenticAssessment, awContext *AwContext) []AgenticAssessment {
	if awContext != nil {
		assessments = append(assessments, AgenticAssessment{
			Kind:           "delegated_context_present",
			Severity:       "info",
			Summary:        "The run preserved upstream dispatch context, which helps trace multi-workflow episodes.",
			Evidence:       fmt.Sprintf("workflow_call_id=%s event_type=%s", awContext.WorkflowCallID, awContext.EventType),
			Recommendation: "Use this context when comparing downstream runs so follow-up workflows are evaluated as part of one task chain.",
		})
	}
	return assessments
}

func generateAgenticAssessmentFindings(assessments []AgenticAssessment) []AuditFinding {
	findings := make([]AuditFinding, 0, len(assessments))
	for _, assessment := range assessments {
		category := "agentic"
		impact := "Review recommended"
		var code AuditFindingCode
		switch assessment.Kind {
		case "resource_heavy_for_domain":
			code = AuditFindingAgenticResourceHeavy
			category = "performance"
			impact = "Higher cost and latency than a comparable well-behaved run"
		case "overkill_for_agentic":
			code = AuditFindingAgenticOverkill
			category = "optimization"
			impact = "A deterministic implementation may be cheaper and easier to govern"
		case "poor_agentic_control":
			code = AuditFindingAgenticPoorControl
			category = "agentic"
			impact = "Broad or weakly controlled behavior can reduce trust even when the run succeeds"
		case "partially_reducible":
			code = AuditFindingAgenticPartiallyReducible
			category = "optimization"
			impact = "Moving data-gathering turns to deterministic steps reduces inference cost"
		case "model_downgrade_available":
			code = AuditFindingAgenticModelDowngrade
			category = "optimization"
			impact = "A smaller model could reduce per-run cost significantly for this task domain"
		case "delegated_context_present":
			code = AuditFindingAgenticDelegatedContext
			category = "coordination"
			impact = "Context continuity improves downstream debugging and auditability"
		}
		findings = append(findings, AuditFinding{
			Code:        code,
			Category:    category,
			Severity:    scanfindings.ParseSeverity(assessment.Severity),
			Title:       prettifyAssessmentKind(assessment.Kind),
			Description: assessment.Summary,
			Impact:      impact,
		})
	}
	return findings
}

func generateAgenticAssessmentRecommendations(assessments []AgenticAssessment) []Recommendation {
	recommendations := make([]Recommendation, 0, len(assessments))
	for _, assessment := range assessments {
		if assessment.Recommendation == "" || assessment.Severity == "info" {
			continue
		}
		priority := "medium"
		if assessment.Severity == "high" {
			priority = "high"
		}
		recommendations = append(recommendations, Recommendation{
			Priority: priority,
			Action:   assessment.Recommendation,
			Reason:   assessment.Summary,
		})
	}
	return recommendations
}

// computeAgenticFraction classifies tool sequences into reasoning vs. data-gathering
// turns and returns the ratio of reasoning turns to total turns.
// Reasoning turns are those where the agent makes decisions, writes output, or uses
// multiple tool types. Data-gathering turns use only read-oriented tools.
func computeAgenticFraction(processedRun ProcessedRun) float64 {
	run := processedRun.Run
	if run.Turns <= 0 {
		auditAgenticLog.Printf("Computing agentic fraction: run_id=%d turns=0, returning 0.0", processedRun.Run.DatabaseID)
		return 0.0
	}
	auditAgenticLog.Printf("Computing agentic fraction: run_id=%d turns=%d safe_items=%d", processedRun.Run.DatabaseID, run.Turns, run.SafeItemsCount)

	// If no tool sequence data, estimate from write actions
	writeCount := run.SafeItemsCount
	if writeCount > 0 && run.Turns > 0 {
		// At minimum, the fraction of turns that produced write actions are agentic
		// and non-write turns doing data gathering are deterministic-reducible
		agenticTurns := writeCount
		// Add reasoning turns: at least 1 turn of reasoning per write action,
		// plus initial planning turn
		reasoningTurns := min(writeCount+1, run.Turns)
		agenticTurns = max(agenticTurns, reasoningTurns)
		agenticTurns = min(agenticTurns, run.Turns)
		return float64(agenticTurns) / float64(run.Turns)
	}

	// No write actions: if the run is read-only with few turns, nearly all
	// turns are reasoning (the AI is analyzing, not just gathering data)
	if run.Turns <= 3 {
		return 1.0
	}

	// For longer read-only runs, assume early turns gather context and
	// later turns do analysis. Heuristic: half the turns are reasoning.
	return 0.5
}

func containsAny(value string, terms ...string) bool {
	for _, term := range terms {
		if strings.Contains(value, term) {
			return true
		}
	}
	return false
}

func prettifyAssessmentKind(kind string) string {
	switch kind {
	case "resource_heavy_for_domain":
		return "Resource Heavy For Domain"
	case "overkill_for_agentic":
		return "Potential Deterministic Alternative"
	case "poor_agentic_control":
		return "Weak Agentic Control"
	case "delegated_context_present":
		return "Dispatch Context Preserved"
	case "partially_reducible":
		return "Partially Reducible To Deterministic"
	case "model_downgrade_available":
		return "Cheaper Model Available"
	default:
		return strings.ReplaceAll(kind, "_", " ")
	}
}

func formatAssessmentDuration(duration time.Duration) string {
	if duration <= 0 {
		return "n/a"
	}
	return duration.String()
}
