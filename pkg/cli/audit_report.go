package cli

import (
	"context"
	"math"
	"path/filepath"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/timeutil"
)

var auditReportLog = logger.New("cli:audit_report")

// buildAuditData creates structured audit data from workflow run information
func buildAuditData(_ context.Context, processedRun ProcessedRun, metrics LogMetrics, mcpToolUsage *MCPToolUsageData) AuditData {
	auditData, _ := buildLocalAuditData(processedRun, metrics, mcpToolUsage)
	return auditData
}

// buildLocalAuditData creates an audit exclusively from downloaded run data.
// Callers such as logs --audit use this path to avoid additional GitHub API calls.
func buildLocalAuditData(processedRun ProcessedRun, metrics LogMetrics, mcpToolUsage *MCPToolUsageData) (AuditData, []CreatedItemReport) {
	run := processedRun.Run
	auditReportLog.Printf("Building audit data for run ID %d", run.DatabaseID)
	mcpToolUsage = normalizeMCPPayloadStats(mcpToolUsage)
	expData := extractExperimentData(run.LogsPath)
	overview := buildAuditOverview(run, expData)
	metricsData, inferredEngineID := buildAuditMetrics(processedRun, metrics)
	jobs := buildAuditJobs(processedRun.JobDetails)
	errors := extractAuditErrors(run)
	downloadedFiles := extractDownloadedFiles(run.LogsPath)
	toolUsage := buildAuditToolUsage(metrics, mcpToolUsage, processedRun.SkillActivations)
	createdItems := resolveCreatedItems(run.LogsPath, processedRun.SafeOutputs)
	taskDomain, behaviorFingerprint, agenticAssessments := buildAuditAssessments(processedRun, metricsData, toolUsage, createdItems, overview.AwContext)
	findings, recommendations, observabilityInsights := buildAuditNarrative(processedRun, metricsData, errors, toolUsage, createdItems, agenticAssessments)
	auditData := assembleAuditData(auditDataInputs{
		processedRun:          processedRun,
		metrics:               metrics,
		mcpToolUsage:          mcpToolUsage,
		expData:               expData,
		inferredEngineID:      inferredEngineID,
		overview:              overview,
		metricsData:           metricsData,
		jobs:                  jobs,
		downloadedFiles:       downloadedFiles,
		errors:                errors,
		toolUsage:             toolUsage,
		createdItems:          createdItems,
		taskDomain:            taskDomain,
		behaviorFingerprint:   behaviorFingerprint,
		agenticAssessments:    agenticAssessments,
		findings:              findings,
		recommendations:       recommendations,
		observabilityInsights: observabilityInsights,
	})
	auditData.ModelRouting = applyAwInfoModelRouting(processedRun.ModelRouting, processedRun.Run.LogsPath)
	auditData.SteeringNotices = tokenUsageSteeringNotices(processedRun.TokenUsage)
	auditData.SteeringNoticesUnavailable = tokenUsageSteeringNoticesUnavailable(processedRun.TokenUsage)
	auditData.ModelEndpointMismatches = readSessionModelEndpointMismatches(processedRun.Run.LogsPath)
	auditData.DetectionModelUsage = readDetectionModelUsage(processedRun.Run.LogsPath)
	appendAuditDetectionIssues(&auditData)
	return auditData, createdItems
}

func buildAuditOverview(run WorkflowRun, expData *ExperimentData) OverviewData {
	overview := OverviewData{
		RunID:        run.DatabaseID,
		WorkflowName: run.WorkflowName,
		Status:       run.Status,
		Conclusion:   run.Conclusion,
		CreatedAt:    run.CreatedAt,
		StartedAt:    run.StartedAt,
		UpdatedAt:    run.UpdatedAt,
		Event:        run.Event,
		Branch:       run.HeadBranch,
		URL:          run.URL,
		Experiment:   formatExperimentLabel(expData),
	}
	if run.LogsPath != "" {
		overview.LogsPath = run.LogsPath
	}
	if run.Duration > 0 {
		overview.Duration = timeutil.FormatDuration(run.Duration)
	}
	if run.LogsPath == "" {
		return overview
	}
	awInfoPath := filepath.Join(run.LogsPath, "aw_info.json")
	if awInfo, err := parseAwInfo(awInfoPath, false); err == nil && awInfo != nil {
		overview.AwContext = awInfo.Context
	}
	return overview
}

func buildAuditMetrics(processedRun ProcessedRun, metrics LogMetrics) (MetricsData, string) {
	run := processedRun.Run
	metricsData := MetricsData{
		TokenUsage:   run.TokenUsage,
		Turns:        run.Turns,
		ErrorCount:   run.ErrorCount,
		WarningCount: run.WarningCount,
	}
	if run.Conclusion == "failure" && metricsData.ErrorCount == 0 {
		metricsData.ErrorCount = 1
	}
	if processedRun.TokenUsage != nil {
		// DeclaredSubagentModels mirrors requests and must not be counted again.
		for _, request := range processedRun.TokenUsage.SubagentModelRequests {
			metricsData.SubagentFailureCount += request.FailedCount
		}
	}

	fallbackMetrics, inferredEngineID := lookupFallbackMetrics(run.LogsPath, metricsData)
	applyFallbackMetrics(&metricsData, processedRun, metrics, fallbackMetrics)
	populateAuditMetricContext(&metricsData, processedRun.TokenUsage)
	metricsData.WorkingSet = processedRun.WorkingSet
	metricsData.Friction = processedRun.Friction
	return metricsData, inferredEngineID
}

func lookupFallbackMetrics(logsPath string, metricsData MetricsData) (LogMetrics, string) {
	if logsPath == "" {
		return LogMetrics{}, ""
	}
	needsFallbackMetrics := metricsData.TokenUsage == 0 || metricsData.Turns == 0
	needsFallbackEngineConfig := findAwInfoPath(logsPath) == ""
	if !needsFallbackMetrics && !needsFallbackEngineConfig {
		return LogMetrics{}, ""
	}
	return inferFallbackLogMetrics(logsPath)
}

func applyFallbackMetrics(metricsData *MetricsData, processedRun ProcessedRun, metrics LogMetrics, fallbackMetrics LogMetrics) {
	if metricsData.TokenUsage == 0 && processedRun.TokenUsage != nil {
		metricsData.TokenUsage = processedRun.TokenUsage.TotalInputTokens + processedRun.TokenUsage.TotalOutputTokens
	}
	if metricsData.TokenUsage == 0 && metrics.TokenUsage > 0 {
		metricsData.TokenUsage = metrics.TokenUsage
	}
	if metricsData.Turns == 0 && metrics.Turns > 0 {
		metricsData.Turns = metrics.Turns
	}
	if metricsData.TokenUsage == 0 && fallbackMetrics.TokenUsage > 0 {
		metricsData.TokenUsage = fallbackMetrics.TokenUsage
	}
	if metricsData.Turns == 0 && fallbackMetrics.Turns > 0 {
		metricsData.Turns = fallbackMetrics.Turns
	}
}

func populateAuditMetricContext(metricsData *MetricsData, tokenUsage *TokenUsageSummary) {
	if tokenUsage != nil && tokenUsage.TotalAIC > 0 {
		metricsData.AIC = tokenUsage.TotalAIC
	}
	if tokenUsage != nil && tokenUsage.AmbientContext != nil {
		metricsData.AmbientContext = tokenUsage.AmbientContext
	}
}

func buildAuditJobs(jobDetails []JobInfoWithDuration) []JobData {
	return sliceutil.Map(jobDetails, func(jobDetail JobInfoWithDuration) JobData {
		job := JobData{
			Name:       jobDetail.Name,
			Status:     jobDetail.Status,
			Conclusion: jobDetail.Conclusion,
			Steps:      jobDetail.Steps,
		}
		if jobDetail.Duration > 0 {
			job.Duration = timeutil.FormatDuration(jobDetail.Duration)
		}
		return job
	})
}

func extractAuditErrors(run WorkflowRun) []ValidationIssue {
	if run.Conclusion != "failure" || run.LogsPath == "" {
		return nil
	}
	if stepErrors := extractPreAgentStepErrors(run.LogsPath); len(stepErrors) > 0 {
		return stepErrors
	}
	return nil
}

func buildAuditToolUsage(metrics LogMetrics, mcpToolUsage *MCPToolUsageData, skillActivations []SkillActivation) []ToolUsageInfo {
	usage := mergeMCPToolUsageInfo(buildToolUsageInfo(metrics), mcpToolUsage)
	skillCounts := make(map[string]int, len(skillActivations))
	for _, activation := range skillActivations {
		name := "skill:" + activation.Name
		count := activation.InvocationCount
		if count <= 0 {
			count = 1
		}
		skillCounts[name] += count
	}
	merged := make([]ToolUsageInfo, 0, len(usage)+len(skillCounts))
	for _, tool := range usage {
		if count, ok := skillCounts[tool.Name]; ok {
			tool.CallCount += count
			delete(skillCounts, tool.Name)
		}
		merged = append(merged, tool)
	}
	for _, activation := range skillActivations {
		name := "skill:" + activation.Name
		if count, ok := skillCounts[name]; ok {
			merged = append(merged, ToolUsageInfo{Name: name, CallCount: count})
			delete(skillCounts, name)
		}
	}
	return merged
}

func buildAuditAssessments(processedRun ProcessedRun, metricsData MetricsData, toolUsage []ToolUsageInfo, createdItems []CreatedItemReport, awContext *AwContext) (*TaskDomainInfo, *BehaviorFingerprint, []AgenticAssessment) {
	taskDomain := detectTaskDomain(processedRun, createdItems, toolUsage, awContext)
	behaviorFingerprint := buildBehaviorFingerprint(processedRun, metricsData, toolUsage, createdItems, awContext)
	agenticAssessments := buildAgenticAssessments(processedRun, metricsData, toolUsage, createdItems, taskDomain, behaviorFingerprint, awContext)
	return taskDomain, behaviorFingerprint, agenticAssessments
}

func buildAuditNarrative(processedRun ProcessedRun, metricsData MetricsData, errors []ValidationIssue, toolUsage []ToolUsageInfo, createdItems []CreatedItemReport, agenticAssessments []AgenticAssessment) ([]AuditFinding, []Recommendation, []ObservabilityInsight) {
	findings := generateFindings(processedRun, metricsData, errors)
	findings = append(findings, generateAgenticAssessmentFindings(agenticAssessments)...)

	recommendations := generateRecommendations(processedRun, metricsData, findings)
	recommendations = append(recommendations, generateAgenticAssessmentRecommendations(agenticAssessments)...)

	observabilityInsights := buildAuditObservabilityInsights(processedRun, metricsData, toolUsage, createdItems)
	observabilityInsights = append(observabilityInsights, buildDrain3Insights(processedRun, metricsData, toolUsage)...)
	return findings, recommendations, observabilityInsights
}

type auditDataInputs struct {
	processedRun          ProcessedRun
	metrics               LogMetrics
	mcpToolUsage          *MCPToolUsageData
	expData               *ExperimentData
	inferredEngineID      string
	overview              OverviewData
	metricsData           MetricsData
	jobs                  []JobData
	downloadedFiles       []FileInfo
	errors                []ValidationIssue
	toolUsage             []ToolUsageInfo
	createdItems          []CreatedItemReport
	taskDomain            *TaskDomainInfo
	behaviorFingerprint   *BehaviorFingerprint
	agenticAssessments    []AgenticAssessment
	findings              []AuditFinding
	recommendations       []Recommendation
	observabilityInsights []ObservabilityInsight
}

func assembleAuditData(inputs auditDataInputs) AuditData {
	run := inputs.processedRun.Run
	metricsData := inputs.metricsData
	if run.ActionMinutes > 0 {
		metricsData.ActionMinutes = run.ActionMinutes
	} else if run.Duration > 0 {
		metricsData.ActionMinutes = math.Ceil(run.Duration.Minutes())
	}

	performanceMetrics := generatePerformanceMetrics(inputs.processedRun, metricsData, inputs.toolUsage)
	chainMetrics := buildSafeOutputChainMetrics(run.LogsPath)
	engineConfig := extractEngineConfigWithInferredEngine(run.LogsPath, inputs.inferredEngineID)
	sessionAnalysis := buildSessionAnalysis(inputs.processedRun, inputs.metrics)
	safeOutputSummary := buildSafeOutputSummary(inputs.createdItems, chainMetrics)
	mcpServerHealth := buildMCPServerHealth(inputs.mcpToolUsage, inputs.processedRun.MCPFailures)

	logAssembledAuditData(inputs)

	return AuditData{
		SchemaVersion:           auditSchemaVersion,
		Retries:                 readAuditRetrySummary(run.LogsPath),
		DurableSession:          readDurableSessionReport(run.LogsPath, run, hasDurableRestoreStep(inputs.processedRun.JobDetails)),
		Overview:                inputs.overview,
		TaskDomain:              inputs.taskDomain,
		BehaviorFingerprint:     inputs.behaviorFingerprint,
		AgenticAssessments:      inputs.agenticAssessments,
		Metrics:                 metricsData,
		Friction:                inputs.processedRun.Friction,
		Ledger:                  inputs.processedRun.Ledger,
		WorkQueue:               inputs.processedRun.WorkQueue,
		KeyFindings:             inputs.findings,
		Recommendations:         inputs.recommendations,
		ObservabilityInsights:   inputs.observabilityInsights,
		PerformanceMetrics:      performanceMetrics,
		EngineConfig:            engineConfig,
		PromptAnalysis:          extractPromptAnalysis(run.LogsPath),
		SessionAnalysis:         sessionAnalysis,
		SafeOutputSummary:       safeOutputSummary,
		MCPServerHealth:         mcpServerHealth,
		Jobs:                    inputs.jobs,
		DownloadedFiles:         inputs.downloadedFiles,
		MissingTools:            inputs.processedRun.MissingTools,
		MissingData:             inputs.processedRun.MissingData,
		Noops:                   inputs.processedRun.Noops,
		MCPFailures:             inputs.processedRun.MCPFailures,
		SkillActivations:        inputs.processedRun.SkillActivations,
		GatewaySteeringEvents:   inputs.processedRun.GatewaySteeringEvents,
		FirewallTokenUsage:      inputs.processedRun.TokenUsage,
		GitHubRateLimitUsage:    inputs.processedRun.GitHubRateLimitUsage,
		FirewallAnalysis:        inputs.processedRun.FirewallAnalysis,
		PolicyAnalysis:          inputs.processedRun.PolicyAnalysis,
		RedactedDomainsAnalysis: inputs.processedRun.RedactedDomainsAnalysis,
		Errors:                  inputs.errors,
		Warnings:                auditEngineConfigWarnings(engineConfig),
		ToolUsage:               inputs.toolUsage,
		MCPToolUsage:            inputs.mcpToolUsage,
		CreatedItems:            inputs.createdItems,
		Experiments:             inputs.expData,
		Graders:                 extractGradersData(run.LogsPath),
		Evals:                   buildEvalsSummary(run),
	}
}

func logAssembledAuditData(inputs auditDataInputs) {
	if auditReportLog.Enabled() {
		auditReportLog.Printf("Built audit data: %d jobs, %d errors, %d tool types, %d findings, %d recommendations", len(inputs.jobs), len(inputs.errors), len(inputs.toolUsage), len(inputs.findings), len(inputs.recommendations))
	}
}

func auditEngineConfigWarnings(config *AuditEngineConfig) []ValidationIssue {
	if config == nil || config.RoutingWarning == "" {
		return nil
	}
	return []ValidationIssue{{Type: "model_routing_outcome_disagreement", Message: config.RoutingWarning}}
}
