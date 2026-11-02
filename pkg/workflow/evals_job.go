// Package workflow - BinEval evaluation job assembler.
package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var evalsJobLog = logger.New("workflow:evals_job")

const evalsStateDir = "/tmp/gh-aw/evals-state"

func evalsBranchName(workflowID string) string {
	return WorkflowStateBranchName(constants.EvalsBranchPrefix, workflowID)
}

// buildEvalsJob creates a separate evals job that runs after the agent job (and detection
// job when enabled), allowing it to run in parallel with safe_outputs.
// The job downloads the agent artifact to access output files, runs a BinEval
// multi-question evaluation via the Decisions API, and uploads evals.jsonl as an artifact.
// Returns nil if evals are not declared in the workflow frontmatter.
func (c *Compiler) buildEvalsJob(data *WorkflowData) (*Job, error) {
	if !data.Evals.HasEvals() {
		evalsJobLog.Print("No evals declared; skipping evals job")
		return nil, nil
	}
	evalsJobLog.Print("Building evals job")

	steps := c.buildEvalsPreparationSteps(data)

	// Add the standalone Decisions invocation and result publication steps.
	steps = append(steps, c.buildEvalsJobSteps(data)...)

	if c.actionMode.IsDev() {
		steps = append(steps, c.generateRestoreActionsSetupStep())
	}

	// Determine job dependencies.
	// Evals always depends on agent and activation, and additionally on detection when the detection job is enabled.
	// This allows evals to run in parallel with safe_outputs.
	needs := []string{string(constants.AgentJobName), string(constants.ActivationJobName)}
	if IsDetectionJobEnabled(data.SafeOutputs) {
		needs = append(needs, string(constants.DetectionJobName))
	}
	evalsJobLog.Printf("Evals job dependencies resolved: needs=%v", needs)

	// Evals job condition: run only after the agent job succeeds.
	alwaysFunc := BuildFunctionCall("always")
	agentSucceeded := BuildEquals(
		BuildPropertyAccess(fmt.Sprintf("needs.%s.result", constants.AgentJobName)),
		BuildStringLiteral("success"),
	)
	jobConditionNode := BuildAnd(alwaysFunc, agentSucceeded)
	jobCondition := RenderCondition(jobConditionNode)

	// Determine runs-on: use evals override if set, otherwise ubuntu-latest.
	runsOn := "runs-on: ubuntu-latest"
	if data.Evals != nil && data.Evals.RunsOn != "" {
		runsOn = normalizeRunsOnSnippet(data.Evals.RunsOn)
	}

	evalsAuthData, permissions := c.buildEvalsJobPermissions(data)
	steps = append(steps, c.buildTokenBrokerInvalidationStep(evalsAuthData)...)

	job := &Job{
		Name:        string(constants.EvalsJobName),
		Needs:       needs,
		If:          jobCondition,
		RunsOn:      c.indentYAMLLines(runsOn, "    "),
		Environment: c.indentYAMLLines(data.Environment, "    "),
		Permissions: permissions,
		Outputs: map[string]string{
			"aic": "${{ steps.parse_evals_token_usage.outputs.aic }}",
		},
		Steps: steps,
	}

	return job, nil
}

func (c *Compiler) buildEvalsPreparationSteps(data *WorkflowData) []string {
	var steps []string

	// Add setup action steps for the shared runtime scripts.
	setupActionRef := c.resolveActionReference("./actions/setup", data)
	if setupActionRef != "" || c.actionMode.IsScript() {
		// For dev mode (local action path), checkout the actions folder first.
		steps = append(steps, c.generateCheckoutActionsFolder(data)...)
		// Reuse the activation job trace ID so all jobs share one OTLP trace.
		evalsTraceID := fmt.Sprintf("${{ needs.%s.outputs.setup-trace-id }}", constants.ActivationJobName)
		evalsParentSpanID := setupParentSpanNeedsExpr(constants.ActivationJobName)
		steps = append(steps, c.generateSetupStepForJob("evals", data, setupActionRef, SetupActionDestination, false, evalsTraceID, evalsParentSpanID, "")...)
	}

	// Download agent output for the shared evaluation evidence.
	// Use activation-derived prefix since this job always depends on activation.
	agentArtifactPrefix := artifactPrefixExprForDownstreamJob(data)
	steps = append(steps, buildAgentOutputDownloadSteps(agentArtifactPrefix, c.getActionPin)...)

	// Download the current experiment variant assignments.
	return append(steps, buildExperimentArtifactDownloadSteps(data, c.getActionPin)...)
}

func (c *Compiler) buildEvalsJobPermissions(data *WorkflowData) (*WorkflowData, string) {
	evalsAuthData := c.buildEvalsDecisionData(data)
	brokeredInference := usesTokenBrokerInference(evalsAuthData)
	copilotRequestsEnabled := evalsAuthData.AI == "copilot" && hasCopilotRequestsWritePermission(data) && !brokeredInference
	perms := NewPermissionsContentsRead()
	if copilotRequestsEnabled {
		perms.Set(PermissionCopilotRequests, PermissionWrite)
	}
	if brokeredInference {
		perms.Set(PermissionIdToken, PermissionWrite)
	}
	if hasOTLPGitHubOIDCAuth(data.ParsedFrontmatter, data.RawFrontmatter) {
		perms.Set(PermissionIdToken, PermissionWrite)
	}
	return evalsAuthData, perms.RenderToYAML()
}

// buildPushEvalsStateJob creates a job that downloads the evals artifact and commits it to a
// git branch ("evals/{sanitizedID}") so eval results can be read even when artifacts are absent.
func (c *Compiler) buildPushEvalsStateJob(data *WorkflowData) (*Job, error) {
	if data.Evals == nil || !data.Evals.HasEvals() {
		return nil, nil
	}

	evalsJobLog.Printf("Building push_evals_state job (branch=%s)", evalsBranchName(data.WorkflowID))

	var steps []string

	setupActionRef := c.resolveActionReference("./actions/setup", data)
	if setupActionRef != "" || c.actionMode.IsScript() {
		steps = append(steps, c.generateCheckoutActionsFolder(data)...)
		traceID := fmt.Sprintf("${{ needs.%s.outputs.setup-trace-id }}", constants.ActivationJobName)
		parentSpanID := setupParentSpanNeedsExpr(constants.ActivationJobName)
		steps = append(steps, c.generateSetupStepForJob("push_evals_state", data, setupActionRef, SetupActionDestination, false, traceID, parentSpanID, "")...)
	}

	steps = append(steps,
		"      - name: Checkout repository\n",
		fmt.Sprintf("        uses: %s\n", getActionPin("actions/checkout")),
		"        with:\n",
		"          persist-credentials: false\n",
		"          sparse-checkout: .\n",
	)

	steps = append(steps, c.generateGitConfigurationSteps()...)

	evalsArtifactName := artifactPrefixExprForDownstreamJob(data) + constants.EvalsArtifactName.String()
	downloadAction := c.getActionPin("actions/download-artifact")
	steps = append(steps,
		"      - name: Download evals artifact\n",
		fmt.Sprintf("        uses: %s\n", downloadAction),
		"        continue-on-error: true\n",
		"        with:\n",
	)
	steps = append(steps, downloadArtifactInputLines(evalsArtifactName, downloadAction)...)
	steps = append(steps, fmt.Sprintf("          path: %s\n", evalsStateDir))

	steps = append(steps, buildPushEvalsStateScript(data)...)

	if c.actionMode.IsDev() {
		steps = append(steps, c.generateRestoreActionsSetupStep())
	}

	evalsFinished := BuildNotEquals(
		BuildPropertyAccess(fmt.Sprintf("needs.%s.result", constants.EvalsJobName)),
		BuildStringLiteral("skipped"),
	)
	notCancelled := &NotNode{Child: BuildFunctionCall("cancelled")}
	jobCondition := RenderCondition(BuildAnd(BuildAnd(BuildFunctionCall("always"), notCancelled), evalsFinished))

	job := &Job{
		Name:        pushEvalsStateJobName,
		RunsOn:      c.formatFrameworkJobRunsOn(data),
		If:          jobCondition,
		Permissions: "permissions:\n      contents: write",
		Needs:       []string{string(constants.EvalsJobName), string(constants.ActivationJobName)},
		Steps:       steps,
	}

	return job, nil
}

// buildPushEvalsStateScript builds the step that pushes evals.jsonl to the evals state
// branch. push_evals_state.cjs skips not-evaluated input (evals_status.json) before the
// generic state writer runs, so the branch keeps answer history only.
func buildPushEvalsStateScript(data *WorkflowData) []string {
	return []string{
		"      - name: Push evals results to git\n",
		"        id: push_evals_state\n",
		"        if: always()\n",
		fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)),
		"        env:\n",
		"          GH_TOKEN: ${{ github.token }}\n",
		"          GITHUB_RUN_ID: ${{ github.run_id }}\n",
		"          GITHUB_SERVER_URL: ${{ github.server_url }}\n",
		fmt.Sprintf("          GH_AW_STATE_DIR: %s\n", evalsStateDir),
		fmt.Sprintf("          GH_AW_STATE_BRANCH: %s\n", evalsBranchName(data.WorkflowID)),
		fmt.Sprintf("          GH_AW_STATE_FILES: %s\n", constants.EvalsResultFilename),
		"          GH_AW_STATE_LABEL: evals results\n",
		"        with:\n",
		"          script: |\n",
		"            const { setupGlobals } = require('" + SetupActionDestination + "/setup_globals.cjs');\n",
		"            setupGlobals(core, github, context, exec, io, getOctokit);\n",
		"            const { main } = require('" + SetupActionDestination + "/push_evals_state.cjs');\n",
		"            await main();\n",
	}
}
