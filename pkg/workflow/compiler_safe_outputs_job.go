package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var consolidatedSafeOutputsJobLog = logger.New("workflow:compiler_safe_outputs_job")

// buildConsolidatedSafeOutputsJob builds a single job containing all safe output operations
// as separate steps within that job. This reduces the number of jobs in the workflow
// while maintaining observability through distinct step names, IDs, and outputs.
//
// File mode: Instead of inlining bundled JavaScript in YAML, this function:
// 1. Collects all JavaScript files needed by enabled safe outputs
// 2. Generates a "Setup JavaScript files" step to write them to /tmp/gh-aw/scripts/
// 3. Each safe output step requires from the local filesystem
func (c *Compiler) buildConsolidatedSafeOutputsJob(data *WorkflowData, mainJobName, markdownPath string) (*Job, []string, error) {
	if data.SafeOutputs == nil {
		if !isWorkQueueParticipant(data) {
			consolidatedSafeOutputsJobLog.Print("No safe outputs configured, skipping consolidated job")
			return nil, nil, nil
		}
		data.SafeOutputs = &SafeOutputsConfig{}
	}

	consolidatedSafeOutputsJobLog.Print("Building consolidated safe outputs job with file mode")

	permissions, threatDetectionEnabled := safeOutputsJobPermissions(data)

	// Compute artifact prefix once; it is referenced in all three phases.
	agentArtifactPrefix := artifactPrefixExprForDownstreamJob(data)

	// Phase 1: Setup action, artifact downloads, and user-provided steps
	setupSteps, err := c.buildSafeOutputsSetupAndDownloadSteps(data, agentArtifactPrefix)
	if err != nil {
		return nil, nil, err
	}

	// Phase 2: Handler manager, SARIF, custom actions, and named outputs
	handlerSteps, outputs, safeOutputStepNames, err := c.buildSafeOutputsHandlerOutputsAndActionSteps(data, agentArtifactPrefix, markdownPath)
	if err != nil {
		return nil, nil, err
	}

	// Early return when no safe output handler steps were emitted
	if len(safeOutputStepNames) == 0 && !isWorkQueueParticipant(data) {
		consolidatedSafeOutputsJobLog.Print("No safe output steps were added")
		return nil, nil, nil
	}

	// Combine the setup steps with the handler steps
	steps := append(setupSteps, handlerSteps...)

	// Phase 3: App-token insertion, finalization, job condition/deps, and job construction
	return c.buildSafeOutputsJobFromParts(buildSafeOutputsJobFromPartsOptions{
		data:                   data,
		mainJobName:            mainJobName,
		markdownPath:           markdownPath,
		agentArtifactPrefix:    agentArtifactPrefix,
		steps:                  steps,
		outputs:                outputs,
		safeOutputStepNames:    safeOutputStepNames,
		permissions:            permissions,
		threatDetectionEnabled: threatDetectionEnabled,
	})
}

func safeOutputsJobPermissions(data *WorkflowData) (*Permissions, bool) {
	permissions := ComputePermissionsForSafeOutputs(data.SafeOutputs)
	addPRLifecycleReadPermission(data, permissions)
	// OIDC auth mints the OTLP token from the safe-outputs job.
	if hasOTLPGitHubOIDCAuth(data.ParsedFrontmatter, data.RawFrontmatter) {
		permissions.Set(PermissionIdToken, PermissionWrite)
	}
	// Ledger compaction dispatches the Agentic Maintenance workflow.
	if buildLedgerRequestCompactionHandlerConfig(data.LedgerConfig) != nil {
		permissions.Set(PermissionActions, PermissionWrite)
	}
	if isWorkQueueParticipant(data) {
		if safeOutputsAnyAuthorizationNeedsOIDC(data.SafeOutputs) {
			permissions.Set(PermissionIdToken, PermissionWrite)
		}
		permissions.Set(PermissionActions, PermissionWrite)
		if data.SafeOutputs != nil && data.SafeOutputs.UploadCodeCoverage != nil && !isHandlerStaged(templatableBoolIsTrue(data.SafeOutputs.Staged), data.SafeOutputs.UploadCodeCoverage.Staged) {
			permissions.Set(PermissionCodeQuality, PermissionWrite)
		}
		permissions.Set(PermissionContents, PermissionWrite)
	}
	return permissions, IsDetectionJobEnabled(data.SafeOutputs)
}

func addPRLifecycleReadPermission(data *WorkflowData, permissions *Permissions) {
	if onSectionHasAnyTrigger(data.On, "pull_request", "pull_request_review", "pull_request_review_comment", "issue_comment", "workflow_call") {
		if level, exists := permissions.Get(PermissionPullRequests); !exists || level == PermissionNone {
			permissions.Set(PermissionPullRequests, PermissionRead)
		}
	}
}

// buildSafeOutputsSetupAndDownloadSteps builds the initial steps for the consolidated safe
// outputs job: setup action (with optional actions-folder checkout), OTLP header masking,
// agent artifact downloads, patch artifact download (when PR operations are configured),
// shared PR checkout, GH Enterprise host configuration, and user-provided steps.
func (c *Compiler) buildSafeOutputsSetupAndDownloadSteps(data *WorkflowData, agentArtifactPrefix string) ([]string, error) {
	var steps []string

	steps = append(steps, c.buildSafeOutputsSetupSteps(data)...)
	steps = append(steps, c.buildSafeOutputsDownloadSteps(data, agentArtifactPrefix)...)
	controls, err := c.buildWorkQueueControlProcessingStep(data)
	if err != nil {
		return nil, err
	}
	steps = append(steps, controls...)
	if isWorkQueueEnabled(data) && usesPatchesAndCheckouts(data.SafeOutputs) {
		consolidatedSafeOutputsJobLog.Print("Adding shared checkout step for PR operations after claim reconciliation")
		steps = append(steps, c.buildSharedPRCheckoutSteps(data)...)
	}

	// Configure GH_HOST for GHES/GHEC compatibility.
	// The safe-outputs job runs as an independent GitHub Actions job and does not
	// inherit GITHUB_ENV from the agent job. User-provided steps (below) and future
	// safe-output handlers that invoke the gh CLI need GH_HOST to target the
	// correct enterprise instance.
	steps = append(steps, generateGHESHostConfigurationStep())

	userSteps, err := c.buildSafeOutputsUserProvidedSteps(data)
	if err != nil {
		return nil, err
	}
	steps = append(steps, userSteps...)

	return steps, nil
}

// buildSafeOutputsSetupSteps returns the setup action and OTLP header/attribute
// masking steps that must run first in the consolidated safe-outputs job.
func (c *Compiler) buildSafeOutputsSetupSteps(data *WorkflowData) []string {
	var steps []string

	// Add setup action to copy JavaScript files
	setupActionRef := c.resolveActionReference("./actions/setup", data)
	if setupActionRef != "" || c.actionMode.IsScript() {
		// For dev mode (local action path), checkout the actions folder first
		steps = append(steps, c.generateCheckoutActionsFolder(data)...)

		// Enable artifact client flag if upload-artifact safe output is configured
		enableArtifactClient := data.SafeOutputs != nil && data.SafeOutputs.UploadArtifact != nil

		// Safe outputs job depends on agent job; reuse the agent's trace ID so all jobs share one OTLP trace
		safeOutputsTraceID := fmt.Sprintf("${{ needs.%s.outputs.setup-trace-id }}", constants.ActivationJobName)
		safeOutputsParentSpanID := setupParentSpanNeedsExpr(constants.ActivationJobName)
		steps = append(steps, c.generateSetupStepForJob("safe_outputs", data, setupActionRef, SetupActionDestination, enableArtifactClient, safeOutputsTraceID, safeOutputsParentSpanID, "")...)
	}

	// Mask OTLP telemetry headers immediately after setup so authentication tokens cannot
	// leak into runner debug logs for any subsequent step in the safe outputs job.
	if isOTLPHeadersPresent(data) {
		steps = append(steps, generateOTLPHeadersMaskStep())
	}
	// Mask custom OTLP attribute values so user-supplied values cannot leak into runner logs.
	if isOTLPAttributesPresent(data) {
		steps = append(steps, generateOTLPAttributesMaskStep())
	}

	return steps
}

// buildSafeOutputsDownloadSteps returns the agent output artifact download steps,
// plus (when applicable) the patch artifact download and shared PR checkout steps.
func (c *Compiler) buildSafeOutputsDownloadSteps(data *WorkflowData, agentArtifactPrefix string) []string {
	var steps []string

	// Add artifact download steps after setup.
	// In workflow_call context, use the per-invocation prefix to avoid artifact name clashes.
	steps = append(steps, buildAgentOutputDownloadSteps(agentArtifactPrefix, c.getActionPin)...)
	if isWorkQueueEnabled(data) {
		steps = append(steps, c.buildWorkQueueSafeOutputDownloadSteps(data, agentArtifactPrefix)...)
	}

	// Add patch artifact download if create-pull-request or push-to-pull-request-branch is enabled.
	if usesPatchesAndCheckouts(data.SafeOutputs) {
		consolidatedSafeOutputsJobLog.Print("Adding patch artifact download for create-pull-request or push-to-pull-request-branch")
		steps = append(steps, buildArtifactDownloadSteps(ArtifactDownloadConfig{
			ArtifactName: agentArtifactPrefix + constants.AgentArtifactName.String(),
			DownloadPath: constants.TmpGhAwDirSlash,
			SetupEnvStep: false,
			StepName:     "Download patch artifact",
		}, c.getActionPin)...)
		// Work-queue jobs add the checkout after claim reconciliation and App token minting.
		if !isWorkQueueEnabled(data) {
			consolidatedSafeOutputsJobLog.Print("Adding shared checkout step for PR operations")
			steps = append(steps, c.buildSharedPRCheckoutSteps(data)...)
		}
	}
	return steps
}

// buildSafeOutputsUserProvidedSteps converts the user-provided safe-outputs.steps
// frontmatter entries into pinned, YAML-rendered workflow steps.
func (c *Compiler) buildSafeOutputsUserProvidedSteps(data *WorkflowData) ([]string, error) {
	if isWorkQueueParticipant(data) {
		return nil, nil
	}
	var steps []string

	if len(data.SafeOutputs.Steps) == 0 {
		return steps, nil
	}

	consolidatedSafeOutputsJobLog.Printf("Adding %d user-provided steps to safe-outputs job", len(data.SafeOutputs.Steps))
	for i, step := range data.SafeOutputs.Steps {
		stepMap, ok := step.(map[string]any)
		if !ok {
			consolidatedSafeOutputsJobLog.Printf("Warning: safe-outputs step at index %d is not a valid step object (must be a map with properties like name, run, uses). Skipping this step.", i)
			continue
		}
		typedStep, err := MapToStep(stepMap)
		if err != nil {
			return nil, fmt.Errorf("failed to convert safe-outputs step at index %d to typed step: %w", i, err)
		}
		pinnedStep, err := applyActionPinToTypedStep(typedStep, data)
		if err != nil {
			return nil, fmt.Errorf("failed to pin action for safe-outputs step at index %d: %w", i, err)
		}
		stepYAML, err := ConvertStepToYAML(pinnedStep.ToMap())
		if err != nil {
			return nil, fmt.Errorf("failed to convert safe-outputs step at index %d to YAML: %w", i, err)
		}
		steps = append(steps, stepYAML)
	}

	return steps, nil
}

// buildSafeOutputsJobFromPartsOptions bundles the inputs required by buildSafeOutputsJobFromParts
// to assemble the final safe_outputs job.
type buildSafeOutputsJobFromPartsOptions struct {
	data                   *WorkflowData
	mainJobName            string
	markdownPath           string
	agentArtifactPrefix    string
	steps                  []string
	outputs                map[string]string
	safeOutputStepNames    []string
	permissions            *Permissions
	threatDetectionEnabled bool
}

// buildSafeOutputsJobFromParts finalizes the step list (app-token insertion, token invalidation,
// items-manifest upload, dev-mode restore, script-mode cleanup), builds the job condition and
// dependency list, and assembles the Job struct for the safe_outputs job.
func (c *Compiler) buildSafeOutputsJobFromParts(
	opts buildSafeOutputsJobFromPartsOptions,
) (*Job, []string, error) {
	data := opts.data
	mainJobName := opts.mainJobName
	markdownPath := opts.markdownPath
	agentArtifactPrefix := opts.agentArtifactPrefix
	steps := opts.steps
	outputs := opts.outputs
	if usesPatchesAndCheckouts(data.SafeOutputs) {
		if checkoutError := dynamicCheckoutFailureOutput(data.CheckoutConfigs); checkoutError != "" {
			outputs["checkout_error"] = checkoutError
		}
	}
	safeOutputStepNames := opts.safeOutputStepNames
	permissions := opts.permissions
	if usesTokenBrokerTasks(data) || usesTokenBrokerSafeOutputs(data) {
		permissions.Set(PermissionIdToken, PermissionWrite)
	}
	threatDetectionEnabled := opts.threatDetectionEnabled

	// Build and insert preamble token minting steps (GitHub App tokens) before checkout/safe-output steps.
	preambleTokenSteps := c.buildPreambleTokenSteps(data, outputs)
	if len(preambleTokenSteps) > 0 {
		steps = c.insertPreambleTokenStepsIntoSteps(steps, preambleTokenSteps, data, agentArtifactPrefix)
	}

	steps = c.appendFinalSafeOutputSteps(data, steps, agentArtifactPrefix)

	jobCondition := buildSafeOutputsJobCondition(data, threatDetectionEnabled)
	needs := c.buildSafeOutputsJobNeeds(data, mainJobName, threatDetectionEnabled)
	workflowID := GetWorkflowIDFromPath(markdownPath)
	jobEnv := c.buildJobLevelSafeOutputEnvVars(data, workflowID)

	concurrencyConfig, err := generateSafeOutputsConcurrencyConfig(data)
	if err != nil {
		return nil, nil, err
	}
	concurrency := c.indentYAMLLines(concurrencyConfig, "    ")

	job := &Job{
		Name:           "safe_outputs",
		If:             RenderCondition(jobCondition),
		RunsOn:         c.formatFrameworkJobRunsOn(data),
		Environment:    c.indentYAMLLines(resolveSafeOutputsEnvironment(data), "    "),
		Permissions:    permissions.RenderToYAML(),
		TimeoutMinutes: safeOutputsJobTimeoutMinutes(data),
		Concurrency:    concurrency,
		Env:            jobEnv,
		Steps:          steps,
		Outputs:        outputs,
		Needs:          needs,
	}

	consolidatedSafeOutputsJobLog.Printf("Built consolidated safe outputs job with %d steps", len(safeOutputStepNames))

	return job, safeOutputStepNames, nil
}
