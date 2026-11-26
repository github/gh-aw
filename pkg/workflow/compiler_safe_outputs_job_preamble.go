package workflow

import (
	"fmt"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

// stepNameLinePrefix matches the canonical YAML line emitted by this compiler for
// step starts in job.Steps (6-space indent + "- name: ").
const stepNameLinePrefix = "      - name: "

// uploadArtifactStagingDownloadStepCount is the number of YAML string entries emitted by the
// upload-artifact staging download step block (name, if, continue-on-error, uses, with, name, path).
// It must match the literal slice appended in buildPreambleTokenSteps.
const uploadArtifactStagingDownloadStepCount = 7

// buildPreambleTokenSteps builds GitHub App token minting steps that must be inserted before
// checkout and safe-output handler steps. It also mutates outputs to track minting failures.
func (c *Compiler) buildPreambleTokenSteps(data *WorkflowData, outputs map[string]string) []string {
	var preambleTokenSteps []string
	if usesTokenBrokerSafeOutputs(data) {
		preambleTokenSteps = append(preambleTokenSteps, c.buildGitHubBrokerTokenStep(data, "safe-outputs-app-token")...)
	}
	if data.SafeOutputs.GitHubApp != nil {
		appPermissions := computePermissionsForSafeOutputs(data.SafeOutputs, true)
		if !globalAppTokenNeededForPermissions(appPermissions) {
			// No enabled handler (e.g. a connector-only configuration) consumes GitHub
			// permissions from this global app, so skip minting an unrelated
			// installation token purely because a top-level github-app was
			// configured or auto-copied via applyTopLevelGitHubAppFallbacks.
			safeOutputsPermissionsLog.Print("No GitHub-backed safe output handler enabled; skipping global GitHub App token minting")
		} else {
			addPRLifecycleReadPermission(data, appPermissions)
			outputs["app_token_minting_failed"] = "${{ steps.safe-outputs-app-token.outcome == 'failure' }}"
			var appTokenFallbackRepo string
			if hasWorkflowCallTrigger(data.On) {
				appTokenFallbackRepo = activationTargetRepoNameExpr
			}
			preambleTokenSteps = append(preambleTokenSteps, c.buildGitHubAppTokenMintStepForRepository(
				"safe_outputs",
				data.SafeOutputs.GitHubApp,
				appPermissions,
				appTokenFallbackRepo,
				inferSingleCheckoutRepositoryForGitHubAppOwner(data),
			)...)
		}
	}
	if headApp := getSafeOutputsHeadApp(data.SafeOutputs); headApp != nil {
		headRepoSlug := getSafeOutputsHeadRepoSlug(data.SafeOutputs)
		preambleTokenSteps = append(preambleTokenSteps, c.buildGitHubAppTokenMintStepForJob(
			"safe_outputs",
			headApp,
			NewPermissionsContentsWritePRWrite(),
			headRepoNameFromSlug(headRepoSlug),
			headRepoSlug,
			"Generate GitHub App head token",
			"safe-outputs-head-app-token",
		)...)
	}
	return preambleTokenSteps
}

// insertPreambleTokenStepsIntoSteps inserts preambleTokenSteps at the correct position
// within steps: after setup/download steps but before checkout and safe-output handler steps.
func (c *Compiler) insertPreambleTokenStepsIntoSteps(steps []string, preambleTokenSteps []string, data *WorkflowData, agentArtifactPrefix string) []string {
	insertIndex := c.calculatePreambleInsertIndex(steps, data, agentArtifactPrefix)

	// The insertion index is line-oriented; if it lands in the middle of a
	// multi-line run/with block, move it to the next step boundary.
	for insertIndex < len(steps) && !strings.HasPrefix(steps[insertIndex], stepNameLinePrefix) { //nolint:uncheckedsliceindex // The loop condition guards the indexed access.
		insertIndex++
	}
	if insertIndex == len(steps) {
		consolidatedSafeOutputsJobLog.Printf(
			"WARN: preamble-token insertion reached end of steps slice (len=%d); step ordering may be incorrect",
			len(steps),
		)
	}

	var newSteps []string
	newSteps = append(newSteps, steps[:insertIndex]...)
	newSteps = append(newSteps, preambleTokenSteps...)
	newSteps = append(newSteps, steps[insertIndex:]...)
	return newSteps
}

// calculatePreambleInsertIndex computes the line-offset index at which preamble token steps
// should be inserted: after setup, OTLP mask, artifact download, and patch download steps.
func (c *Compiler) calculatePreambleInsertIndex(steps []string, data *WorkflowData, agentArtifactPrefix string) int {
	if isWorkQueueEnabled(data) {
		// Queue workflows mint preamble tokens right before the trusted control step.
		for i, line := range steps {
			if strings.Contains(line, "id: work_queue_controls") {
				for start, candidate := range slices.Backward(steps[:i+1]) {
					if strings.HasPrefix(candidate, stepNameLinePrefix) {
						return start
					}
				}
			}
		}
	}

	insertIndex := 0
	setupActionRef := c.resolveActionReference("./actions/setup", data)
	if setupActionRef != "" {
		insertIndex += len(c.generateCheckoutActionsFolder(data))
		countTraceID := fmt.Sprintf("${{ needs.%s.outputs.setup-trace-id }}", constants.ActivationJobName)
		countParentSpanID := setupParentSpanNeedsExpr(constants.ActivationJobName)
		insertIndex += len(c.generateSetupStep(data, setupActionRef, SetupActionDestination, data.SafeOutputs != nil && data.SafeOutputs.UploadArtifact != nil, countTraceID, countParentSpanID))
	}
	if isOTLPHeadersPresent(data) {
		insertIndex += strings.Count(generateOTLPHeadersMaskStep(), stepNameLinePrefix)
	}
	if isOTLPAttributesPresent(data) {
		insertIndex += strings.Count(generateOTLPAttributesMaskStep(), stepNameLinePrefix)
	}
	insertIndex += len(buildAgentOutputDownloadSteps(agentArtifactPrefix, c.getActionPin))
	if usesSafeOutputsArtifactStaging(data.SafeOutputs) {
		// The staging download step has uploadArtifactStagingDownloadStepCount YAML string entries.
		insertIndex += uploadArtifactStagingDownloadStepCount
	}
	if usesPatchesAndCheckouts(data.SafeOutputs) {
		patchDownloadSteps := buildArtifactDownloadSteps(ArtifactDownloadConfig{
			ArtifactName: agentArtifactPrefix + constants.AgentArtifactName.String(),
			DownloadPath: constants.TmpGhAwDirSlash,
			SetupEnvStep: false,
			StepName:     "Download patch artifact",
		}, c.getActionPin)
		insertIndex += len(patchDownloadSteps)
	}
	return insertIndex
}

// appendFinalSafeOutputSteps appends the manifest upload, dev-mode restore, and script-mode
// cleanup steps that must come after all safe-output handler steps.
func (c *Compiler) appendFinalSafeOutputSteps(data *WorkflowData, steps []string, agentArtifactPrefix string) []string {
	isStaged := c.trialMode || templatableBoolIsTrue(data.SafeOutputs.Staged)
	hasMergeDecisions := data.SafeOutputs.MergePullRequest != nil
	if !isStaged || hasMergeDecisions {
		claimArtifacts := isWorkQueueWorker(data)
		if claimArtifacts || hasMergeDecisions {
			var redaction strings.Builder
			c.generateTrackedSecretRedactionStep(&redaction, strings.Join(steps, ""), data, false)
			steps = append(steps, redaction.String())
		}
		steps = append(steps, buildSafeOutputManifestUploadStep(agentArtifactPrefix, c.getActionPin, claimArtifacts, data, !isStaged)...)
	}
	if usesTokenBrokerSafeOutputs(data) {
		steps = append(steps, c.buildBrokerInvalidationStep(data)...)
	} else {
		steps = append(steps, c.buildTokenBrokerInvalidationStep(data)...)
	}
	if c.actionMode.IsDev() && usesPatchesAndCheckouts(data.SafeOutputs) {
		steps = append(steps, c.generateRestoreActionsSetupStep())
		consolidatedSafeOutputsJobLog.Print("Added restore actions folder step to safe_outputs job (dev mode with checkout)")
	}
	if c.actionMode.IsScript() {
		steps = append(steps, c.generateScriptModeCleanupStep(data))
	}
	return steps
}
