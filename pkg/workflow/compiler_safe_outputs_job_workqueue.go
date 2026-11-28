package workflow

import (
	"fmt"
	"slices"

	"github.com/github/gh-aw/pkg/constants"
)

func (c *Compiler) buildWorkQueueSafeOutputDownloadSteps(data *WorkflowData, agentArtifactPrefix string) []string {
	var steps []string
	steps = append(steps, buildArtifactDownloadSteps(ArtifactDownloadConfig{
		ArtifactName: artifactPrefixExprForActivationJob(data) + constants.ActivationArtifactName.String(),
		DownloadPath: constants.TmpGhAwDirSlash,
		SetupEnvStep: false,
		StepName:     "Download activation artifact for work queue",
	}, c.getActionPin)...)
	steps = append(steps, c.buildWorkQueuePreparedAdapterDownloadSteps(data)...)
	if data.SafeOutputs.UploadAssets != nil {
		steps = append(steps,
			"      - name: Download Claim-scoped staged assets\n",
			"        continue-on-error: true\n",
			fmt.Sprintf("        uses: %s\n", c.getActionPin("actions/download-artifact")),
			"        with:\n",
			"          name: "+agentArtifactPrefix+"safe-outputs-assets\n",
			"          path: "+constants.TmpGhAwAssetsDir+"\n",
		)
	}
	if data.SafeOutputs.UploadCodeCoverage != nil {
		steps = append(steps,
			"      - name: Download Claim-scoped coverage reports\n",
			"        continue-on-error: true\n",
			fmt.Sprintf("        uses: %s\n", c.getActionPin("actions/download-artifact")),
			"        with:\n",
			"          name: "+agentArtifactPrefix+SafeOutputsUploadCodeCoverageStagingArtifactName+"\n",
			"          path: ${{ runner.temp }}/gh-aw/safeoutputs/upload-code-coverage/\n",
		)
	}
	return steps
}

func (c *Compiler) buildWorkQueuePreparedAdapterDownloadSteps(data *WorkflowData) []string {
	var steps []string
	for _, name := range workQueuePreparedAdapterNames(data) {
		artifactID := fmt.Sprintf("needs.%s.outputs.artifact_id", workQueuePreparedJobName(name))
		steps = append(steps,
			fmt.Sprintf("      - name: Download isolated Claim adapter preparation %s\n", name),
			fmt.Sprintf("        if: always() && %s != ''\n", artifactID),
			"        continue-on-error: true\n",
			fmt.Sprintf("        uses: %s\n", c.getActionPin("actions/download-artifact")),
			"        with:\n",
			fmt.Sprintf("          artifact-ids: ${{ %s }}\n", artifactID),
			fmt.Sprintf("          path: %s/claim-adapters/%s/0/claims/\n", constants.TmpGhAwDir, name),
		)
	}
	return steps
}

// buildWorkQueueFinishStep publishes the worker's finish commit after Process Safe Outputs
// has applied (and recorded) the Claim-scoped effects.
func (c *Compiler) buildWorkQueueFinishStep(data *WorkflowData) []string {
	if !isWorkQueueParticipant(data) || !workQueueRequiresAssignment(data) {
		return nil
	}
	steps := []string{
		"      - name: Finish work queue claim\n",
		"        id: work_queue_finish\n",
		fmt.Sprintf("        uses: %s\n", c.getActionPin("actions/github-script")),
	}
	env := workQueuePolicyEnvironment(data)
	if len(env) == 0 {
		env = append(env, "        env:\n")
	}
	env = append(env,
		"          GH_AW_JOB_CHECK_RUN_ID: ${{ job.check_run_id }}\n",
		"          GH_AW_WORK_QUEUE_FINISH_INTENT: "+constants.WorkQueueFinishIntentPath+"\n",
		"          GH_AW_WORK_QUEUE_EFFECTS: "+constants.WorkQueueEffectsPath+"\n",
	)
	var staged *TemplatableBool
	if data.SafeOutputs != nil {
		staged = data.SafeOutputs.Staged
	}
	if value := resolveSafeOutputsStagedValue(c.trialMode, staged); value != nil {
		if isExpression(*value) {
			env = append(env, "          GH_AW_SAFE_OUTPUTS_STAGED: "+*value+"\n")
		} else {
			env = append(env, "          GH_AW_SAFE_OUTPUTS_STAGED: \"true\"\n")
		}
	}
	if isQueueRoutedRequestContinuation(data.SafeOutputs) {
		env = append(env, c.workQueueContinuationFinishEnv(data)...)
	}
	steps = append(steps, env...)
	return append(steps,
		"        with:\n",
		"          script: |\n",
		"            const { setupGlobals } = require('"+SetupActionDestination+"/setup_globals.cjs');\n",
		"            setupGlobals(core, github, context, exec, io, getOctokit);\n",
		"            const { main } = require('${{ runner.temp }}/gh-aw/actions/finish_work_queue_claim.cjs');\n",
		"            await main({ core, github, context, requireAssignment: true });\n",
	)
}

func (c *Compiler) buildWorkQueueControlProcessingStep(data *WorkflowData) ([]string, error) {
	if !isWorkQueueParticipant(data) || data.WorkQueuePolicy == nil {
		return nil, nil
	}
	names := workQueueControlWorkflows(data)
	config := map[string]any{"work_queue_enabled": true, "work_queue_workflows": names, "aw_context_workflows": names}
	if data.SafeOutputs != nil && data.SafeOutputs.DispatchWorkflow != nil {
		config = handlerRegistry["dispatch_workflow"](data.SafeOutputs)
		config["work_queue_workflows"] = names
		config["aw_context_workflows"] = names
	}
	config["work_queue_enabled"] = true
	encoded, err := marshalSafeOutputsConfig(config)
	if err != nil {
		return nil, fmt.Errorf("work-queue: serialize protected control configuration: %w", err)
	}
	steps := []string{
		"      - name: Process trusted work queue controls\n",
		"        id: work_queue_controls\n",
		fmt.Sprintf("        uses: %s\n", c.getActionPin("actions/github-script")),
	}
	env := workQueuePolicyEnvironment(data)
	env = append(env, "          GH_AW_JOB_CHECK_RUN_ID: ${{ job.check_run_id }}\n")
	var staged *TemplatableBool
	if data.SafeOutputs != nil {
		staged = data.SafeOutputs.Staged
	}
	if value := resolveSafeOutputsStagedValue(c.trialMode, staged); value != nil {
		env = append(env, "          GH_AW_SAFE_OUTPUTS_STAGED: "+fmt.Sprintf("%q", *value)+"\n")
	}
	env = append(env, fmt.Sprintf("          GH_AW_GITHUB_TOKEN_SOURCE: %s\n", c.safeOutputGitHubTokenSource(data)))
	env = append(env, fmt.Sprintf("          GH_AW_WORK_QUEUE_TOKEN: %s\n", resolveSafeOutputGitHubTokenForConfig(data, "", resolveSafeOutputGitHubToken, true)))
	env = append(env, "          GH_AW_WORK_QUEUE_INTENTS: "+constants.WorkQueueIntentPath+"\n")
	env = append(env, "          GH_AW_WORK_QUEUE_CONTROL_CONFIG: "+fmt.Sprintf("%q", string(encoded))+"\n")
	steps = append(steps, env...)
	assignmentRequirement := ""
	if isWorkQueueWorker(data) {
		assignmentRequirement = ", requireAssignment: true"
	}
	steps = append(steps, "        with:\n")
	c.addSafeOutputGitHubTokenForConfig(&steps, data, "")
	return append(steps,
		"          script: |\n",
		"            const { setupGlobals } = require('"+SetupActionDestination+"/setup_globals.cjs');\n",
		"            setupGlobals(core, github, context, exec, io, getOctokit, process.env.GH_AW_GITHUB_TOKEN_SOURCE);\n",
		"            const { main } = require('${{ runner.temp }}/gh-aw/actions/work_queue_control_adapter.cjs');\n",
		"            const config = JSON.parse(process.env.GH_AW_WORK_QUEUE_CONTROL_CONFIG);\n",
		"            await main({ core, github, context, config, maxDispatches: Number(process.env.GH_AW_WORK_QUEUE_DISPATCH_BUDGET || 1)"+assignmentRequirement+" });\n",
	), nil
}

func workQueueControlWorkflows(data *WorkflowData) []string {
	if data.SafeOutputs != nil && data.SafeOutputs.DispatchWorkflow != nil {
		return slices.Clone(data.SafeOutputs.DispatchWorkflow.Workflows)
	}
	return nil
}
