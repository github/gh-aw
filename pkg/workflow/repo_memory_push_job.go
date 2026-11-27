package workflow

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

// buildPushRepoMemoryJob creates a job that downloads repo-memory artifacts and pushes them to git branches
// This job runs after the agent job completes, unless agent success is required and it fails.
// If threat detection is enabled, only runs if no threats were detected
func (c *Compiler) buildPushRepoMemoryJob(data *WorkflowData, threatDetectionEnabled bool) (*Job, error) {
	if data.RepoMemoryConfig == nil || len(data.RepoMemoryConfig.Memories) == 0 {
		return nil, nil
	}

	repoMemoryLog.Printf("Building push_repo_memory job for %d memories (threatDetectionEnabled=%v)", len(data.RepoMemoryConfig.Memories), threatDetectionEnabled)

	setupActionRef := c.resolveActionReference("./actions/setup", data)
	steps := c.buildPushRepoMemorySetupAndCheckoutSteps(data, setupActionRef)
	_, hasConsolidatedSafeOutputsJob := c.jobManager.GetJob(string(constants.SafeOutputsJobName))
	steps = append(steps, c.buildPushRepoMemoryDownloadSteps(data, hasConsolidatedSafeOutputsJob)...)
	hasEvals := data.Evals != nil && data.Evals.HasEvals()
	steps = append(steps, c.generateDailyAICRepoMemoryLedgerSteps(data, hasEvals)...)

	contentsPermission := "read"
	environment := ""
	for _, memory := range data.RepoMemoryConfig.Memories {
		if memory.githubApp(PermissionRead) != nil || memory.githubApp(PermissionWrite) != nil {
			environment = c.indentYAMLLines(data.Environment, "    ")
		}
		app := memory.githubApp(PermissionWrite)
		if app != nil {
			steps = append(steps, c.buildRepoMemoryAppTokenMintStep(memory, PermissionWrite, pushRepoMemoryJobName)...)
		}
		if app == nil || app.IgnoreIfMissing {
			contentsPermission = "write"
		}
		steps = append(steps, c.buildSinglePushRepoMemoryStep(data, memory, setupActionRef != "", hasConsolidatedSafeOutputsJob))
	}
	if c.actionMode.IsDev() {
		steps = append(steps, c.generateRestoreActionsSetupStep())
	}

	jobCondition, jobNeeds := c.buildPushRepoMemoryJobCondition(threatDetectionEnabled, data.SafeOutputs)
	if hasConsolidatedSafeOutputsJob {
		jobNeeds = append(jobNeeds, string(constants.SafeOutputsJobName))
	}
	if hasEvals {
		jobNeeds = append(jobNeeds, string(constants.EvalsJobName))
	}

	// No job-level concurrency group is set here on purpose. GitHub Actions keeps at
	// most one running and one pending job per concurrency group: when more than two
	// runs sharing a memory branch finish close together, every additional pending job
	// is cancelled and its memory writes are silently lost. Instead of serialising the
	// pushes, the push script converges optimistically: createCommitOnBranch is a
	// compare-and-swap on the expected head OID, and a rejected push re-reads the remote
	// head, rebases with a JSONL merge=union policy and retries with exponential backoff.

	return &Job{
		Name:           pushRepoMemoryJobName,
		DisplayName:    "",
		RunsOn:         c.formatFrameworkJobRunsOn(data),
		Environment:    environment,
		If:             jobCondition,
		Permissions:    "permissions:\n      contents: " + contentsPermission,
		TimeoutMinutes: pushRepoMemoryTimeoutMinutes,
		Needs:          jobNeeds,
		Steps:          steps,
		Outputs:        buildPushRepoMemoryOutputs(data.RepoMemoryConfig.Memories),
	}, nil
}

// buildPushRepoMemorySetupAndCheckoutSteps builds setup, checkout, and git configuration steps.
func (c *Compiler) buildPushRepoMemorySetupAndCheckoutSteps(data *WorkflowData, setupActionRef string) []string {
	var steps []string
	if setupActionRef != "" || c.actionMode.IsScript() {
		steps = append(steps, c.generateCheckoutActionsFolder(data)...)
		repoMemoryTraceID := fmt.Sprintf("${{ needs.%s.outputs.setup-trace-id }}", constants.ActivationJobName)
		repoMemoryParentSpanID := setupParentSpanNeedsExpr(constants.ActivationJobName)
		steps = append(steps, c.generateSetupStepForJob("push_repo_memory", data, setupActionRef, SetupActionDestination, false, repoMemoryTraceID, repoMemoryParentSpanID, "")...)
	}
	var checkoutStep strings.Builder
	checkoutStep.WriteString("      - name: Checkout repository\n")
	fmt.Fprintf(&checkoutStep, "        uses: %s\n", getActionPin("actions/checkout")) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	checkoutStep.WriteString("        with:\n")
	checkoutStep.WriteString("          persist-credentials: false\n")
	checkoutStep.WriteString("          sparse-checkout: .\n")
	steps = append(steps, checkoutStep.String())
	return append(steps, c.generateGitConfigurationStepsWithToken("${{ github.token }}", "${{ github.repository }}")...)
}

// buildPushRepoMemoryDownloadSteps builds download-artifact steps for all memory entries.
func (c *Compiler) buildPushRepoMemoryDownloadSteps(data *WorkflowData, hasConsolidatedSafeOutputsJob bool) []string {
	repoMemoryPrefix := artifactPrefixExprForAgentDownstreamJob(data)
	var steps []string
	if hasConsolidatedSafeOutputsJob {
		steps = append(steps,
			"      - name: Download safe-output temporary ID map\n",
			fmt.Sprintf("        uses: %s\n", c.getActionPin("actions/download-artifact")),
			"        continue-on-error: true\n",
			"        with:\n",
			fmt.Sprintf("          name: %s%s\n", repoMemoryPrefix, constants.SafeOutputItemsArtifactName),
			"          path: /tmp/gh-aw/safe-outputs-items\n",
		)
	}
	for _, memory := range data.RepoMemoryConfig.Memories {
		sanitizedID := SanitizeWorkflowIDForCacheKey(memory.ID)
		var step strings.Builder
		if memory.Wiki {
			fmt.Fprintf(&step, "      - name: Download wiki-memory artifact (%s)\n", memory.ID) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
		} else {
			fmt.Fprintf(&step, "      - name: Download repo-memory artifact (%s)\n", memory.ID) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
		}
		fmt.Fprintf(&step, "        uses: %s\n", c.getActionPin("actions/download-artifact")) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
		step.WriteString("        continue-on-error: true\n")
		step.WriteString("        with:\n")
		fmt.Fprintf(&step, "          name: %srepo-memory-%s\n", repoMemoryPrefix, sanitizedID) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
		fmt.Fprintf(&step, "          path: /tmp/gh-aw/repo-memory/%s\n", memory.ID)            //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
		steps = append(steps, step.String())
	}
	return steps
}

// buildSinglePushRepoMemoryStep builds a single push-repo-memory step for one memory entry.
func (c *Compiler) buildSinglePushRepoMemoryStep(data *WorkflowData, memory RepoMemoryEntry, useRequire, hasConsolidatedSafeOutputsJob bool) string {
	targetRepo := repoMemoryTargetRepo(memory, PermissionWrite)
	artifactDir := constants.TmpRepoMemoryDir + memory.ID
	fileGlobFilter := ""
	if len(memory.FileGlob) > 0 {
		fileGlobFilter = strings.Join(memory.FileGlob, " ")
	}
	var step strings.Builder
	if memory.Wiki {
		fmt.Fprintf(&step, "      - name: Push wiki-memory changes (%s)\n", memory.ID) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	} else {
		fmt.Fprintf(&step, "      - name: Push repo-memory changes (%s)\n", memory.ID) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	}
	fmt.Fprintf(&step, "        id: push_repo_memory_%s\n", memory.ID) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	condition := "always()"
	if targetCondition := repoMemoryAppTargetCondition(memory, PermissionWrite); targetCondition != "" {
		condition = combineGitHubIfExpressions(condition, targetCondition)
	}
	fmt.Fprintf(&step, "        if: %s\n", condition)                                           //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	fmt.Fprintf(&step, "        uses: %s\n", getCachedActionPin("actions/github-script", data)) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	step.WriteString("        env:\n")
	step.WriteString(buildRepoMemoryGitHubEnv(data, hasConsolidatedSafeOutputsJob, memory))
	fmt.Fprintf(&step, "          ARTIFACT_DIR: %s\n", artifactDir)      //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	fmt.Fprintf(&step, "          MEMORY_ID: %s\n", memory.ID)           //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	fmt.Fprintf(&step, "          TARGET_REPO: %s\n", targetRepo)        //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	fmt.Fprintf(&step, "          BRANCH_NAME: %s\n", memory.BranchName) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	if ledgerMemory, ok := dailyAICRepoMemoryEntry(data); ok && ledgerMemory.ID == memory.ID {
		step.WriteString("          GH_AW_DAILY_AIC_REPO_MEMORY_WRITER: ${{ steps.daily-aic-repo-memory-reset.outcome == 'success' }}\n")
	}
	if memory.Wiki || (memory.githubApp(PermissionWrite) != nil && memory.TargetRepo != "") {
		fmt.Fprintf(&step, "          REPO_MEMORY_ALLOWED_REPOS: %s\n", targetRepo) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	}
	fmt.Fprintf(&step, "          MAX_FILE_SIZE: %d\n", memory.MaxFileSize)     //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	fmt.Fprintf(&step, "          MAX_FILE_COUNT: %d\n", memory.MaxFileCount)   //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	fmt.Fprintf(&step, "          MAX_PATCH_SIZE: %d\n", memory.MaxPatchSize)   //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	allowedExtsJSON, _ := json.Marshal(memory.AllowedExtensions)                //nolint:jsonmarshalignoredeerror // marshaling a string slice cannot fail
	fmt.Fprintf(&step, "          ALLOWED_EXTENSIONS: '%s'\n", allowedExtsJSON) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	if fileGlobFilter != "" {
		fmt.Fprintf(&step, "          FILE_GLOB_FILTER: \"%s\"\n", fileGlobFilter) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	}
	if memory.FormatJSON {
		step.WriteString("          FORMAT_JSON: 'true'\n")
	}
	appendMemoryValidationEnvironment(&step, memory.Validation)
	appendRepoMemoryLedgerLimitsEnv(&step, memory.Ledger)
	step.WriteString("        with:\n")
	if memory.githubApp(PermissionWrite) != nil {
		fmt.Fprintf(&step, "          github-token: %s\n", repoMemoryAppToken(memory, PermissionWrite)) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	}
	step.WriteString("          script: |\n")
	step.WriteString("            const { setupGlobals } = require('" + SetupActionDestination + "/setup_globals.cjs');\n")
	step.WriteString("            setupGlobals(core, github, context, exec, io, getOctokit);\n")
	if useRequire {
		step.WriteString("            const { main } = require('" + SetupActionDestination + "/push_repo_memory.cjs');\n")
		step.WriteString("            await main();\n")
	} else {
		for _, line := range FormatJavaScriptForYAML("const { main } = require('${{ runner.temp }}/gh-aw/actions/push_repo_memory.cjs'); await main();") {
			step.WriteString(line)
		}
	}
	return step.String()
}

func appendRepoMemoryLedgerLimitsEnv(step *strings.Builder, ledger *RepoMemoryLedgerConfig) {
	if ledger != nil {
		if ledger.MaxSegmentKB > 0 {
			fmt.Fprintf(step, "          GH_AW_LEDGER_MAX_SEGMENT_KB: %d\n", ledger.MaxSegmentKB) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
		}
		if ledger.MaxRecordKB > 0 {
			fmt.Fprintf(step, "          GH_AW_LEDGER_MAX_RECORD_KB: %d\n", ledger.MaxRecordKB) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
		}
		if ledger.MaxPatchKB > 0 {
			fmt.Fprintf(step, "          GH_AW_LEDGER_MAX_PATCH_KB: %d\n", ledger.MaxPatchKB) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
		}
	}
}

func buildRepoMemoryGitHubEnv(data *WorkflowData, hasConsolidatedSafeOutputsJob bool, memory RepoMemoryEntry) string {
	token := "${{ github.token }}"
	if memory.githubApp(PermissionWrite) != nil {
		token = repoMemoryAppToken(memory, PermissionWrite)
	}
	env := fmt.Sprintf("          GH_TOKEN: %s\n", token) +
		"          GITHUB_RUN_ID: ${{ github.run_id }}\n" +
		"          GITHUB_SERVER_URL: ${{ github.server_url }}\n" +
		fmt.Sprintf("          REPO_MEMORY_BASE_COMMIT: ${{ needs.%s.outputs.%s }}\n", constants.AgentJobName, repoMemoryBaseCommitOutput(memory.ID))
	if memory.Validation != nil && memory.Validation.Script != "" {
		env += fmt.Sprintf("          GH_AW_MEMORY_SOURCE_JOB_RESULT: ${{ needs.%s.result }}\n", constants.AgentJobName)
	}
	if hasConsolidatedSafeOutputsJob {
		env += "          GH_AW_TEMPORARY_ID_MAP_FILE: /tmp/gh-aw/safe-outputs-items/" + constants.TemporaryIdMapFilename.String() + "\n"
	}
	return env
}

// buildPushRepoMemoryJobCondition computes the job condition and needs list.
func (c *Compiler) buildPushRepoMemoryJobCondition(threatDetectionEnabled bool, safeOutputs *SafeOutputsConfig) (string, []string) {
	agentCompleted := BuildNotEquals(
		BuildPropertyAccess(fmt.Sprintf("needs.%s.result", constants.AgentJobName)),
		BuildStringLiteral("skipped"),
	)
	notCancelled := &NotNode{Child: BuildFunctionCall("cancelled")}
	jobNeeds := []string{string(constants.AgentJobName), string(constants.ActivationJobName)}
	var jobCondition ConditionNode
	if threatDetectionEnabled {
		jobCondition = BuildAnd(BuildAnd(BuildAnd(BuildFunctionCall("always"), notCancelled), buildDetectionPassedCondition()), agentCompleted)
		jobNeeds = append(jobNeeds, string(constants.DetectionJobName))
	} else {
		// Without a detection job, only cancellation prevents this cleanup job from running.
		jobCondition = BuildAnd(BuildFunctionCall("always"), notCancelled)
	}
	return RenderCondition(requireSafeOutputsAgentSuccess(jobCondition, safeOutputs)), jobNeeds
}

// buildPushRepoMemoryOutputs builds the persistence and validation outputs from all memory steps.
func buildPushRepoMemoryOutputs(memories []RepoMemoryEntry) map[string]string {
	outputs := make(map[string]string)
	for _, memory := range memories {
		stepID := "push_repo_memory_" + memory.ID
		outputs["save_status_"+memory.ID] = fmt.Sprintf("${{ steps.%s.outputs.save_status }}", stepID)
		outputs["save_outcome_"+memory.ID] = fmt.Sprintf("${{ steps.%s.outcome }}", stepID)
		outputs["save_branch_"+memory.ID] = fmt.Sprintf("${{ steps.%s.outputs.save_branch }}", stepID)
		outputs["save_commit_"+memory.ID] = fmt.Sprintf("${{ steps.%s.outputs.save_commit }}", stepID)
		outputs["validation_failed_"+memory.ID] = fmt.Sprintf("${{ steps.%s.outputs.validation_failed }}", stepID)
		outputs["validation_error_"+memory.ID] = fmt.Sprintf("${{ steps.%s.outputs.validation_error }}", stepID)
		outputs["patch_size_exceeded_"+memory.ID] = fmt.Sprintf("${{ steps.%s.outputs.patch_size_exceeded }}", stepID)
	}
	return outputs
}
