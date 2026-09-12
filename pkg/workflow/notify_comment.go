package workflow

import (
	"fmt"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var notifyCommentLog = logger.New("workflow:notify_comment")

// buildConclusionJob creates a job that handles workflow completion tasks
// This job is generated when safe-outputs are configured and handles:
// - Updating status comments (if status-comment: true)
// - Processing noop messages
// - Handling agent failures
// - Recording missing tools
// This job runs when:
// 1. always() - runs even if agent fails
// 2. Agent job was not skipped
// 3. NO add_comment output was produced by the agent (avoids duplicate updates)
// This job depends on all safe output jobs to ensure it runs last
func (c *Compiler) buildConclusionJob(data *WorkflowData, mainJobName string, safeOutputJobNames []string) (*Job, error) {
	notifyCommentLog.Printf("Building conclusion job: main_job=%s, safe_output_jobs_count=%d", mainJobName, len(safeOutputJobNames))
	// Always create this job when safe-outputs exist (because noop is always enabled)
	// This ensures noop messages can be handled even without reactions
	if data.SafeOutputs == nil {
		notifyCommentLog.Printf("Skipping job: no safe-outputs configured")
		return nil, nil // No safe-outputs configured, no need for conclusion job
	}
	steps, err := c.buildConclusionJobSteps(data, mainJobName, safeOutputJobNames)
	if err != nil {
		return nil, err
	}
	needs := buildConclusionJobNeeds(data, mainJobName, safeOutputJobNames)
	// If any message template references needs.pre_activation.outputs.*, add pre_activation
	// as a dependency so that GitHub Actions can resolve the expression at runtime.
	if data.SafeOutputs != nil && messagesContainPreActivationRef(data.SafeOutputs.Messages) {
		if _, exists := c.jobManager.GetJob(string(constants.PreActivationJobName)); exists {
			preActName := string(constants.PreActivationJobName)
			if !slices.Contains(needs, preActName) {
				needs = append(needs, preActName)
				notifyCommentLog.Print("Added pre_activation dependency to conclusion job (messages reference pre_activation outputs)")
			}
		}
	}
	notifyCommentLog.Printf("Job built successfully: dependencies_count=%d", len(needs))
	conclusionPerms := computeConclusionJobPermissions(data)
	return &Job{
		Name:        "conclusion",
		If:          RenderCondition(c.buildConclusionJobCondition(data, mainJobName, safeOutputJobNames)),
		RunsOn:      c.formatFrameworkJobRunsOn(data),
		Environment: c.indentYAMLLines(resolveSafeOutputsEnvironment(data), "    "),
		Permissions: conclusionPerms.RenderToYAML(),
		Concurrency: c.buildConclusionJobConcurrency(data),
		Steps:       steps,
		Needs:       needs,
		Outputs:     buildConclusionJobOutputs(data),
	}, nil
}

// buildConclusionJobSteps assembles the ordered step list for the conclusion job: setup and
// usage-artifact steps, noop/missing-tool/incomplete reporting, agent-failure and
// failed-jobs reporting, the optional status-comment update, and the steering-issue step.
func (c *Compiler) buildConclusionJobSteps(data *WorkflowData, mainJobName string, safeOutputJobNames []string) ([]string, error) {
	steps := c.buildConclusionSetupSteps(data)
	steps = append(steps, c.buildConclusionNoOpStep(data, mainJobName)...)
	steps = append(steps, c.buildConclusionDetectionRunsStep(data, mainJobName)...)
	steps = append(steps, c.buildConclusionMissingToolStep(data, mainJobName)...)
	steps = append(steps, c.buildConclusionReportIncompleteStep(data, mainJobName)...)
	messagesJSON := serializeConclusionMessagesJSON(data)
	steeringTokenSteps, steeringToken := c.buildConclusionSteeringIssueTokenSteps(data)
	steps = append(steps, steeringTokenSteps...)
	agentFailureSteps, err := c.buildAgentFailureStep(data, mainJobName, messagesJSON, steeringToken)
	if err != nil {
		return nil, err
	}
	steps = append(steps, agentFailureSteps...)
	steps = append(steps, c.buildConclusionReportFailedJobsStep(data, mainJobName)...)
	// Only add the conclusion update step if status comments are explicitly enabled
	if data.StatusComment != nil && *data.StatusComment {
		var token string
		if data.SafeOutputs != nil && data.SafeOutputs.AddComments != nil {
			token = data.SafeOutputs.AddComments.GitHubToken
		}
		steps = append(steps, c.buildGitHubScriptStepWithoutDownload(data, GitHubScriptStepConfig{
			StepName:      "Update reaction comment with completion status",
			StepID:        "conclusion",
			MainJobName:   mainJobName,
			CustomEnvVars: c.buildConclusionScriptEnvVars(data, mainJobName, safeOutputJobNames, messagesJSON),
			Script:        getNotifyCommentErrorScript(),
			ScriptFile:    "notify_comment_error.cjs",
			CustomToken:   token,
		})...)
	}
	steps = append(steps, c.buildConclusionSteeringIssueStep(data, mainJobName, steeringToken)...)
	if c.actionMode.IsScript() {
		steps = append(steps, c.generateScriptModeCleanupStep())
	}
	return steps, nil
}

// computeConclusionJobPermissions resolves the GITHUB_TOKEN permissions for the conclusion
// job: the base safe-outputs permissions plus the extra scopes required by the OTLP OIDC
// token, the daily-AIC cache save step, the report-failed-jobs step, and any issue-creating
// conclusion mechanism.
func computeConclusionJobPermissions(data *WorkflowData) *Permissions {
	conclusionPerms := ComputePermissionsForSafeOutputs(data.SafeOutputs)
	// When observability.otlp.github-app is configured without app-id/private-key
	// credentials, id-token: write is needed so the conclusion job can mint the OTLP
	// OIDC token via core.getIDToken(audience) (mirrors threat_detection_job.go).
	if hasOTLPGitHubOIDCAuth(data.ParsedFrontmatter, data.RawFrontmatter) {
		conclusionPerms.Set(PermissionIdToken, PermissionWrite)
	}
	// The report-failed-jobs step lists workflow run jobs (actions: read) when the
	// feature is enabled (default: true).
	if conclusionReportFailedJobsEnabled(data) {
		if level, ok := conclusionPerms.Get(PermissionActions); !ok || level == PermissionNone {
			conclusionPerms.Set(PermissionActions, PermissionRead)
		}
	}
	// Only request issues: write when at least one conclusion-job mechanism can actually
	// create/update an issue and that path is not already covered by
	// ComputePermissionsForSafeOutputs (report-failed-jobs, agent-failure reporting,
	// noop reporting, or missing-tool issue reporting). This keeps the permission grant
	// derived from the resolved configuration instead of being emitted unconditionally.
	if conclusionMayCreateIssue(data) {
		if level, ok := conclusionPerms.Get(PermissionIssues); !ok || level != PermissionWrite {
			conclusionPerms.Set(PermissionIssues, PermissionWrite)
		}
		if isSteeringIssueEnabled(data) {
			conclusionPerms.Set(PermissionIssues, PermissionWrite)
		}
	}
	return conclusionPerms
}

// conclusionReportFailedJobsEnabled returns true unless safe-outputs.report-failed-jobs is
// explicitly set to false. Defaults to true.
func conclusionReportFailedJobsEnabled(data *WorkflowData) bool {
	return data.SafeOutputs == nil || data.SafeOutputs.ReportFailedJobs == nil || *data.SafeOutputs.ReportFailedJobs
}

// conclusionReportFailureAsIssueEnabled returns true unless safe-outputs.report-failure-as-issue
// is explicitly set to false. Defaults to true.
func conclusionReportFailureAsIssueEnabled(data *WorkflowData) bool {
	if data.SafeOutputs == nil || data.SafeOutputs.ReportFailureAsIssue == nil {
		return true
	}
	return !strings.EqualFold(strings.TrimSpace(data.SafeOutputs.ReportFailureAsIssue.String()), "false")
}

// conclusionMissingToolCreateIssueEnabled returns true unless
// safe-outputs.missing-tool.create-issue is explicitly set to false.
func conclusionMissingToolCreateIssueEnabled(data *WorkflowData) bool {
	return data.SafeOutputs != nil && issueReportingCreateIssueEnabled(data.SafeOutputs.MissingTool)
}

func issueReportingCreateIssueEnabled(config *IssueReportingConfig) bool {
	return config != nil && (config.CreateIssue == nil || !strings.EqualFold(strings.TrimSpace(*config.CreateIssue), "false"))
}

// conclusionMayCreateIssue returns true if at least one conclusion-job mechanism can create or
// update an issue: report-failed-jobs, agent-failure reporting (report-failure-as-issue), noop
// reporting (noop.report-as-issue), or missing-tool issue reporting. This mirrors the resolved
// configuration so that disabling every issue-creating path removes issues: write from the
// compiled conclusion job's permissions.
func conclusionMayCreateIssue(data *WorkflowData) bool {
	if conclusionReportFailedJobsEnabled(data) {
		return true
	}
	if conclusionReportFailureAsIssueEnabled(data) {
		return true
	}
	if data.SafeOutputs != nil && data.SafeOutputs.NoOp != nil && isNoOpReportAsIssueEnabled(data.SafeOutputs.NoOp.ReportAsIssue) {
		return true
	}
	if conclusionMissingToolCreateIssueEnabled(data) {
		return true
	}
	return false
}

// buildUsageArtifactInputDownloadSteps creates the artifact download steps that feed the
// usage artifact: the safe-outputs items manifest (used by
// generate_usage_activity_summary.cjs) and, when the workflow declares evals, the evals
// artifact. Grader results need no dedicated download because the conclusion job already
// downloads the unified agent artifact, which contains them.
func buildUsageArtifactInputDownloadSteps(prefix string, hasEvals bool, pinAction func(string) string) []string {
	safeOutputsItemsArtifactName := prefix + constants.SafeOutputItemsArtifactName.String()
	safeOutputsDownloadAction := pinAction("actions/download-artifact")
	steps := []string{
		"      - name: Download Safe Outputs Items Manifest\n",
		"        id: download-safe-outputs-manifest\n",
		"        if: always()\n",
		"        continue-on-error: true\n",
		fmt.Sprintf("        uses: %s\n", safeOutputsDownloadAction),
		"        with:\n",
	}
	steps = append(steps, downloadArtifactInputLines(safeOutputsItemsArtifactName, safeOutputsDownloadAction)...)
	steps = append(steps, "          path: /tmp/gh-aw/\n")
	if !hasEvals {
		return steps
	}
	evalsArtifactName := prefix + constants.EvalsArtifactName.String()
	evalsDownloadAction := pinAction("actions/download-artifact")
	steps = append(steps,
		"      - name: Download evals artifact\n",
		"        id: download-evals-artifact\n",
		"        if: always()\n",
		"        continue-on-error: true\n",
		fmt.Sprintf("        uses: %s\n", evalsDownloadAction),
		"        with:\n",
	)
	steps = append(steps, downloadArtifactInputLines(evalsArtifactName, evalsDownloadAction)...)
	return append(steps, "          path: /tmp/gh-aw/evals/\n")
}

// buildUsageArtifactUploadSteps creates steps that collect and upload a compact usage artifact.
// The artifact includes aw_info.json, aw-info.jsonl, agent_usage.json, agent_usage.jsonl, detection_usage.jsonl,
// evals.jsonl, grader results, and agent/detection token usage JSONL files (when present).
// It also downloads the safe-outputs-items artifact so that generate_usage_activity_summary.cjs
// can include safe-output item counts in the activity summary without requiring a separate artifact download.
func buildUsageArtifactUploadSteps(prefix string, hasEvals bool, pinAction func(string) string) []string {
	usageArtifactName := prefix + "usage"
	steps := buildUsageArtifactInputDownloadSteps(prefix, hasEvals, pinAction)
	steps = append(steps,
		"      - name: Collect usage artifact files\n",
		"        if: always()\n",
		"        continue-on-error: true\n",
		fmt.Sprintf("        run: bash \"%s/collect_usage_artifact_files.sh\"\n", SetupActionDestinationShell),
		"      - name: Upload usage artifact\n",
		"        if: always()\n",
		"        continue-on-error: true\n",
		fmt.Sprintf("        uses: %s\n", pinAction("actions/upload-artifact")),
		"        with:\n",
		fmt.Sprintf("          name: %s\n", usageArtifactName),
		"          path: |\n",
		"            /tmp/gh-aw/usage/aw_info.json\n",
		"            /tmp/gh-aw/usage/aw-info.jsonl\n",
		"            /tmp/gh-aw/usage/agent_usage.json\n",
		"            /tmp/gh-aw/usage/agent_usage.jsonl\n",
		"            /tmp/gh-aw/usage/detection_usage.jsonl\n",
		"            /tmp/gh-aw/usage/evals.jsonl\n",
		"            /tmp/gh-aw/usage/graders/grader_manifest.json\n",
		"            /tmp/gh-aw/usage/graders/grader_results.json\n",
		"            /tmp/gh-aw/usage/github_rate_limits.jsonl\n",
		"            /tmp/gh-aw/usage/agent/token_usage.jsonl\n",
		"            /tmp/gh-aw/usage/agent/execution.json\n",
		"            /tmp/gh-aw/usage/detection/token_usage.jsonl\n",
		"            /tmp/gh-aw/usage/detection/execution.json\n",
		"            /tmp/gh-aw/usage/evals/token_usage.jsonl\n",
		"            /tmp/gh-aw/usage/activity/summary.json\n",
		"          if-no-files-found: ignore\n",
	)
	return steps
}

// isGroupConcurrencyQueueEnabled reports whether compiler-generated concurrency groups
// should include queue: max. The feature is enabled by default and can be disabled
// with features.group-concurrency-queue: false.
func isGroupConcurrencyQueueEnabled(data *WorkflowData) bool {
	flag := strings.ToLower(strings.TrimSpace(string(constants.GroupConcurrencyQueueFeatureFlag)))
	if data != nil && data.Features != nil {
		for key, value := range data.Features {
			if strings.EqualFold(key, flag) {
				return parseGroupConcurrencyQueueFeatureValue(value)
			}
		}
	}
	return true
}

func parseGroupConcurrencyQueueFeatureValue(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		normalized := strings.ToLower(strings.TrimSpace(v))
		switch normalized {
		case "false", "0", "off", "no":
			return false
		default:
			return true
		}
	default:
		return true
	}
}
