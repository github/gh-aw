// Package workflow - step builders for the standard (inline) threat detection flow.
package workflow

import (
	"fmt"
	"maps"
	"strconv"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/typeutil"
)

// buildDetectionJobSteps builds the threat detection steps to be run in the separate detection job.
// These steps run after the agent job completes and analyze agent output for threats using the
// same agentic engine with sandbox.agent and fully blocked network.
// The detection job downloads the agent artifact to access the output files.
func (c *Compiler) buildDetectionJobSteps(data *WorkflowData) []string { //nolint:largefunc // Detection step order remains intentionally explicit.
	threatLog.Print("Building threat detection steps for detection job")
	if data.SafeOutputs == nil || data.SafeOutputs.ThreatDetection == nil {
		return nil
	}

	var steps []string

	// Comment separator
	steps = append(steps, "      # --- Threat Detection ---\n")
	steps = append(steps, generateComponentExecutionEvidenceStep("detection", "not_started", detectionExecutionEvidencePath, "")...)

	// Step 0: Remove agent state that must not be attributed to detection.
	steps = append(steps, c.buildClearInheritedCopilotSessionStateStep()...)

	// Step 1: Clean stale firewall files left by the agent artifact download.
	// The agent artifact populates sandbox/firewall/logs and sandbox/firewall/audit
	// with files that cause the squid container to crash on start-up.
	steps = append(steps, c.buildCleanFirewallDirsStep()...)

	// Step 2: Pull AWF container images - the detection engine runs inside AWF (firewall),
	// so pre-pulling the containers speeds up execution and avoids on-demand pulls.
	//
	// For the inline Codex detection path (gh-aw-detection feature disabled), MCP setup
	// generation already emits this step via generateDownloadDockerImagesStep, so skip here
	// to avoid duplicate step names in the detection job.
	// For the external detector path (gh-aw-detection: true), MCP setup is not called for
	// the detection job, so the download step must be emitted here unconditionally.
	usingExternalDetector := isFeatureEnabled(constants.GHAWDetectionFeatureFlag, data)
	if c.getThreatDetectionEngineID(data) != "codex" || usingExternalDetector {
		steps = append(steps, c.buildPullAWFContainersStep(data)...)
	}

	// Step 2: Detection guard - determines whether detection should run
	steps = append(steps, c.buildDetectionGuardStep()...)

	// Step 3: Clear MCP configuration files so the detection engine runs without MCP servers
	steps = append(steps, c.buildClearMCPConfigStep()...)

	// Step 4: Prepare files - copies agent output files to expected paths
	steps = append(steps, c.buildPrepareDetectionFilesStep()...)

	// Step 5: Custom pre-steps if configured (run before engine execution)
	if len(data.SafeOutputs.ThreatDetection.Steps) > 0 {
		steps = append(steps, c.buildCustomThreatDetectionSteps(data.SafeOutputs.ThreatDetection.Steps)...)
	}

	// Step 6: Setup threat detection (github-script)
	//
	// On the external detector path this step is still emitted because it exports GH_AW_PROMPT,
	// GH_AW_DETECTION_CONTINUE_ON_ERROR, HAS_PATCH and the workflow-context vars, and performs
	// artifact validation. Its prompt is unused by threat-detect, so only its step-summary write
	// is suppressed (see GH_AW_DETECTION_SKIP_PROMPT_SUMMARY in buildThreatDetectionAnalysisStep).
	// Decision: retiring the step entirely on the external path is deferred until the remaining
	// responsibilities move to the execution step and the detector's own validation.
	steps = append(steps, c.buildThreatDetectionAnalysisStep(data)...)

	if isFeatureEnabled(constants.GHAWDetectionFeatureFlag, data) {
		// External detector path (features: gh-aw-detection: true)

		// Step 7: Install AWF binary (required for the detection AWF invocation)
		steps = append(steps, c.buildInstallAWFForExternalDetectorStep(data)...)

		// Step 8: Install the selected agentic engine binary for threat-detect execution
		steps = append(steps, c.buildInstallDetectionEngineForExternalDetectorStep(data)...)

		// Step 9: Prepare any engine-specific config files needed by threat-detect.
		steps = append(steps, c.buildPrepareDetectionEngineConfigForExternalDetectorStep(data)...)

		// Step 10: Install the threat-detect binary from GitHub Releases
		steps = append(steps, c.buildInstallThreatDetectStep(data)...)

		// Step 11: Run threat-detect under AWF with a read-write mount for the result file
		steps = append(steps, c.buildExternalDetectorExecutionStep(data)...)

		// Step 11a: Render detection.log to the Actions log wrapped in group/stop-commands macros.
		steps = append(steps, c.buildRenderDetectionLogStep(data)...)

		// Step 11b: Copy the detection AWF run's own firewall proxy/audit logs into the
		// threat-detection working directory so they can be bundled in the detection artifact.
		steps = append(steps, c.buildCopyDetectionFirewallLogsStep(data)...)

		// Step 12: Custom post-steps if configured (run after detection execution)
		if len(data.SafeOutputs.ThreatDetection.PostSteps) > 0 {
			steps = append(steps, c.buildCustomThreatDetectionSteps(data.SafeOutputs.ThreatDetection.PostSteps)...)
		}

		// Step 13: Parse threat-detection token usage for step summary and downstream footer rendering.
		steps = append(steps, c.buildDetectionTokenUsageSummaryStep(data)...)

		// Step 14: Upload detection_result.json and accounting as the detection artifact.
		steps = append(steps, c.buildUploadDetectionArtifactStep(data)...)

		// Step 15: Conclude via threat-detect conclude (no .cjs)
		steps = append(steps, c.buildExternalDetectorConcludeStep(data)...)
	} else {
		// Inline engine path (default)

		// Step 7: Engine execution (AWF, no network)
		steps = append(steps, c.buildDetectionEngineExecutionStep(data)...)

		// Step 7a: Echo detection step summary so the GitHub runner can mask any secrets.
		// The AWF execution step writes the detection agent's step-summary content to
		// ThreatDetectionStepSummaryPath (overriding $GITHUB_STEP_SUMMARY at the step level).
		// Echoing the file content here lets the runner apply its secret-masking pass.
		steps = append(steps, c.buildDetectionStepSummaryEchoStep()...)

		// Step 7b: Render detection.log to the Actions log wrapped in group/stop-commands macros.
		steps = append(steps, c.buildRenderDetectionLogStep(data)...)

		// Step 7c: Copy the detection AWF run's own firewall proxy/audit logs into the
		// threat-detection working directory so they can be bundled in the detection artifact.
		steps = append(steps, c.buildCopyDetectionFirewallLogsStep(data)...)

		// Step 8: Custom post-steps if configured (run after engine execution)
		if len(data.SafeOutputs.ThreatDetection.PostSteps) > 0 {
			steps = append(steps, c.buildCustomThreatDetectionSteps(data.SafeOutputs.ThreatDetection.PostSteps)...)
		}

		// Step 9: Parse threat-detection token usage for step summary and downstream footer rendering.
		steps = append(steps, c.buildDetectionTokenUsageSummaryStep(data)...)

		// Step 10: Upload detection-artifact
		steps = append(steps, c.buildUploadDetectionLogStep(data)...)

		// Step 11: Parse results, log extensively, and set job conclusion (single JS step)
		steps = append(steps, c.buildDetectionConclusionStep(data)...)
	}

	threatLog.Printf("Generated %d detection job step lines", len(steps))
	return steps
}

// buildDetectionGuardStep creates a guard step that checks if detection should run.
// Uses always() to run even if the agent job failed (detection still analyzes whatever output exists).
// In the separate detection job, output metadata is read from the agent job's outputs.
func (c *Compiler) buildDetectionGuardStep() []string {
	return []string{
		"      - name: Check if detection needed\n",
		"        id: detection_guard\n",
		"        if: always()\n",
		"        env:\n",
		"          OUTPUT_TYPES: ${{ needs.agent.outputs.output_types }}\n",
		"          HAS_PATCH: ${{ needs.agent.outputs.has_patch }}\n",
		"        run: |\n",
		"          if [[ -n \"$OUTPUT_TYPES\" || \"$HAS_PATCH\" == \"true\" ]]; then\n",
		"            echo \"run_detection=true\" >> \"$GITHUB_OUTPUT\"\n",
		"            echo \"Detection will run: output_types=$OUTPUT_TYPES, has_patch=$HAS_PATCH\"\n",
		"          else\n",
		"            echo \"run_detection=false\" >> \"$GITHUB_OUTPUT\"\n",
		"            echo \"Detection skipped: no agent outputs or patches to analyze\"\n",
		"          fi\n",
	}
}

// buildClearMCPConfigStep creates a step that removes MCP configuration files.
// This ensures the detection engine runs without any MCP servers.
func (c *Compiler) buildClearMCPConfigStep() []string {
	return []string{
		"      - name: Clear MCP Config for detection\n",
		fmt.Sprintf("        if: %s\n", detectionStepCondition),
		"        run: |\n",
		"          rm -f \"${RUNNER_TEMP}/gh-aw/mcp-config/mcp-servers.json\"\n",
		// Use $HOME (not the literal /home/runner) so this works on self-hosted
		// or containerized runners where HOME differs from the GitHub-hosted default.
		"          rm -f \"$HOME/.copilot/mcp-config.json\"\n",
		"          rm -f \"$GITHUB_WORKSPACE/.gemini/settings.json\"\n",
	}
}

func (c *Compiler) buildClearInheritedCopilotSessionStateStep() []string {
	return []string{
		"      - name: Clear inherited Copilot session state\n",
		"        run: rm -rf " + shellEscapeArg(constants.TmpSandboxAgentLogsDir+"copilot-session-state") + "\n",
	}
}

// buildCleanFirewallDirsStep creates a step that removes stale firewall files
// from the directories populated by the agent artifact download. When the agent
// artifact is extracted to /tmp/gh-aw/, it pre-populates the sandbox/firewall/logs
// and sandbox/firewall/audit directories with files from the agent job (squid.conf,
// cache.log, access.log, etc.). If these files are present when AWF starts the
// squid container in the detection job, squid fails to initialise (exit code 1).
// Cleaning these directories before pulling containers avoids the crash.
func (c *Compiler) buildCleanFirewallDirsStep() []string {
	return []string{
		"      - name: Clean stale firewall files from agent artifact\n",
		"        run: |\n",
		fmt.Sprintf("          rm -rf %s\n", constants.AWFProxyLogsDir),
		fmt.Sprintf("          rm -rf %s\n", constants.AWFAuditDir),
	}
}

// detectionFirewallLogsDir is the directory (relative to ThreatDetectionDir) where the
// detection job's own AWF firewall proxy/audit logs are copied before upload. Namespacing
// them under threat-detection/ (rather than uploading the raw AWFProxyLogsDir/AWFAuditDir
// paths directly) avoids colliding with the agent job's identically-named firewall log
// files when both artifacts are extracted into the same /tmp/gh-aw/ root in the conclusion
// job — and matches the layout collect_usage_artifact_files.sh expects for detection usage.
const detectionFirewallLogsDir = constants.ThreatDetectionDir + "/sandbox/firewall"

// buildCopyDetectionFirewallLogsStep creates a step that copies the detection AWF run's
// own proxy/audit logs (written to the same well-known AWFProxyLogsDir/AWFAuditDir paths
// the agent job uses) into the threat-detection working directory. Without this, the
// detection job's firewall/api-proxy token-usage data never leaves its ephemeral runner:
// it is not part of any uploaded artifact, so the usage artifact and the AI-credits budget
// cap have nothing to observe for the detection phase (see gh-aw#54047/#54046).
func (c *Compiler) buildCopyDetectionFirewallLogsStep(data *WorkflowData) []string {
	if !isFirewallEnabled(data) {
		return nil
	}

	proxyLogsDir := constants.AWFProxyLogsDir.String()
	auditDir := constants.AWFAuditDir.String()
	if isArcDindTopology(data) {
		proxyLogsDir = rewriteArcDindPath(proxyLogsDir)
		auditDir = rewriteArcDindPath(auditDir)
	}

	return []string{
		"      - name: Copy detection firewall logs\n",
		fmt.Sprintf("        if: %s\n", detectionStepCondition),
		"        continue-on-error: true\n",
		"        run: |\n",
		fmt.Sprintf("          mkdir -p %s\n", detectionFirewallLogsDir),
		fmt.Sprintf("          if [ -d %s ]; then mkdir -p %s/logs && cp -r %s/. %s/logs/; fi\n", proxyLogsDir, detectionFirewallLogsDir, proxyLogsDir, detectionFirewallLogsDir),
		fmt.Sprintf("          if [ -d %s ]; then mkdir -p %s/audit && cp -r %s/. %s/audit/; fi\n", auditDir, detectionFirewallLogsDir, auditDir, detectionFirewallLogsDir),
	}
}

// buildPrepareDetectionFilesStep creates a step that stages agent output files
// for the threat-detect binary. In the separate detection job, files are
// available after downloading the agent artifact.
func (c *Compiler) buildPrepareDetectionFilesStep() []string {
	return []string{
		"      - name: Prepare threat detection files\n",
		fmt.Sprintf("        if: %s\n", detectionStepCondition),
		"        run: |\n",
		"          bash \"${RUNNER_TEMP}/gh-aw/actions/prepare_threat_detection_files.sh\"\n",
	}
}

// resolveThreatDetectionContinueOnError determines the continue-on-error mode for
// threat-detection steps (default: true — detection failures produce warnings). When
// ContinueOnErrorExpr is set the value is resolved at runtime; compile-time we use true as
// a safe default so the step-level continue-on-error is included (permissive).
func resolveThreatDetectionContinueOnError(data *WorkflowData) (bool, *string) {
	continueOnError := true
	var continueOnErrorExpr *string
	if data.SafeOutputs != nil && data.SafeOutputs.ThreatDetection != nil {
		continueOnError = data.SafeOutputs.ThreatDetection.IsContinueOnError()
		continueOnErrorExpr = data.SafeOutputs.ThreatDetection.ContinueOnErrorExpr
	}
	return continueOnError, continueOnErrorExpr
}

// buildDetectionConclusionStep creates the combined parse-and-conclude step for threat detection.
// This single JS step consolidates what was previously two steps:
//  1. Parsing the detection log (parse_detection_results)
//  2. Setting the final job conclusion (detection_conclusion)
//
// It always runs (always()) so that job outputs are set regardless of prior step outcomes.
// The RUN_DETECTION env var lets the script short-circuit with conclusion=skipped when
// the detection guard determined there was no output to analyze.
func (c *Compiler) buildDetectionConclusionStep(data *WorkflowData) []string {
	// Determine continue-on-error mode (default: true — detection failures produce warnings).
	// When ContinueOnErrorExpr is set the value is resolved at runtime; compile-time we use
	// true as a safe default so the step-level continue-on-error is included (permissive).
	continueOnError, continueOnErrorExpr := resolveThreatDetectionContinueOnError(data)

	steps := []string{
		"      - name: Parse and conclude threat detection\n",
		"        id: detection_conclusion\n",
		"        if: always()\n",
	}
	// In warn mode (continue-on-error: true), add continue-on-error to the parse step so that
	// an unexpected exception in the parse script never causes the detection job to fail. The
	// script already handles all expected error cases via setDetectionFailure(), but adding
	// continue-on-error here as a defence-in-depth measure prevents the detection job from
	// blocking safe_outputs due to an unanticipated runtime error in the parse step.
	// In strict mode (continue-on-error: false), we intentionally leave this off so that
	// a parse failure in strict mode keeps the detection job result as failure.
	// When the value is an expression, emit it unquoted; when the value is a literal, only
	// emit if true (permissive default). In either expression or literal-true case the step
	// is included, so the two paths are distinct.
	if continueOnErrorExpr != nil {
		// Expression form: GitHub Actions evaluates this at runtime.
		steps = append(steps, fmt.Sprintf("        continue-on-error: %s\n", *continueOnErrorExpr))
	} else if continueOnError {
		steps = append(steps, "        continue-on-error: true\n")
	}

	steps = append(steps, []string{
		fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)),
		"        env:\n",
		"          RUN_DETECTION: ${{ steps.detection_guard.outputs.run_detection }}\n",
		"          DETECTION_AGENTIC_EXECUTION_OUTCOME: ${{ steps.detection_agentic_execution.outcome }}\n",
		buildThreatDetectionContinueOnErrorEnvLine(continueOnError, continueOnErrorExpr),
		"        with:\n",
		"          script: |\n",
	}...)

	script := c.buildResultsParsingScriptRequire()
	formattedScript := FormatJavaScriptForYAML(script)
	steps = append(steps, formattedScript...)

	return steps
}

// buildDetectionTokenUsageSummaryStep creates a step that parses threat-detection
// firewall token usage, appends a separate table to the detection job summary,
// and exposes AI Credits for downstream jobs.
func (c *Compiler) buildDetectionTokenUsageSummaryStep(data *WorkflowData) []string {
	return []string{
		"      - name: Parse threat detection token usage for step summary\n",
		"        id: parse_detection_token_usage\n",
		"        if: always()\n",
		"        continue-on-error: true\n",
		fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)),
		"        env:\n",
		"          GH_AW_TOKEN_USAGE_SUMMARY_TITLE: Threat Detection Token Usage\n",
		"          GH_AW_AGENT_USAGE_PATH: " + constants.TmpGhAwDir + "/threat-detection/detection_usage.json\n",
		"          GH_AW_AGENT_USAGE_JSONL_PATH: " + constants.TmpGhAwDir + "/threat-detection/detection_usage.jsonl\n",
		"          GH_AW_WRITE_EMPTY_USAGE: \"true\"\n",
		"        with:\n",
		"          script: |\n",
		"            const { setupGlobals } = require('" + SetupActionDestination + "/setup_globals.cjs');\n",
		"            setupGlobals(core, github, context, exec, io, getOctokit);\n",
		"            const { main } = require('" + SetupActionDestination + "/parse_token_usage.cjs');\n",
		"            await main();\n",
	}
}

// buildThreatDetectionAnalysisStep creates the main threat analysis step
func (c *Compiler) buildThreatDetectionAnalysisStep(data *WorkflowData) []string {
	var steps []string

	// Determine continue-on-error mode (same logic as buildDetectionConclusionStep).
	continueOnError, continueOnErrorExpr := resolveThreatDetectionContinueOnError(data)

	// Setup step
	steps = append(steps, []string{
		"      - name: Setup threat detection\n",
		fmt.Sprintf("        if: %s\n", detectionStepCondition),
		fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)),
		"        env:\n",
	}...)
	steps = append(steps, c.buildThreatDetectionContextEnvVars(data, continueOnError, continueOnErrorExpr)...)

	// On the external detector path the prompt rendered by this step is never used: threat-detect
	// renders its own embedded template and appends it to the step summary. Suppress the setup
	// step's summary write so a single detection run does not display two different prompts.
	if isFeatureEnabled(constants.GHAWDetectionFeatureFlag, data) {
		steps = append(steps, "          GH_AW_DETECTION_SKIP_PROMPT_SUMMARY: \"true\"\n")
	}

	steps = append(steps, []string{
		"        with:\n",
		"          script: |\n",
	}...)

	// Require the setup_threat_detection.cjs module and call main with the template
	setupScript := c.buildSetupScriptRequire()
	formattedSetupScript := FormatJavaScriptForYAML(setupScript)
	steps = append(steps, formattedSetupScript...)

	// Add a small shell step in YAML to ensure the output directory and log file exist.
	// The step-summary reset/touch is only needed on the inline path: the inline engine
	// execution step overrides GITHUB_STEP_SUMMARY to this path so the AWF sandbox can
	// write to it. The external threat-detect binary (v0.4.5+) no longer writes any
	// step-summary output (github/gh-aw-threat-detection#792), so resetting the file
	// there would be dead code.
	ensureSteps := []string{
		"      - name: Ensure threat-detection directory and log\n",
		fmt.Sprintf("        if: %s\n", detectionStepCondition),
		"        run: |\n",
		"          mkdir -p /tmp/gh-aw/threat-detection\n",
		"          touch /tmp/gh-aw/threat-detection/detection.log\n",
	}
	if !isFeatureEnabled(constants.GHAWDetectionFeatureFlag, data) {
		ensureSteps = append(ensureSteps,
			fmt.Sprintf("          rm -f %s\n", constants.ThreatDetectionStepSummaryPath),
			fmt.Sprintf("          touch %s\n", constants.ThreatDetectionStepSummaryPath),
		)
	}
	steps = append(steps, ensureSteps...)

	return steps
}

// buildSetupScriptRequire creates the setup script that requires the .cjs module
func (c *Compiler) buildSetupScriptRequire() string {
	// Build a simple require statement that calls the main function
	// The template is now read from file at runtime by the JavaScript module
	script := `const { setupGlobals } = require('` + SetupActionDestination + `/setup_globals.cjs');
setupGlobals(core, github, context, exec, io, getOctokit);
const { main } = require('` + SetupActionDestination + `/setup_threat_detection.cjs');
await main();`

	return script
}

// buildWorkflowContextEnvVars creates environment variables for workflow context
func (c *Compiler) buildWorkflowContextEnvVars(data *WorkflowData) []string {
	workflowName := data.Name
	if workflowName == "" {
		workflowName = "Unnamed Workflow"
	}

	workflowDescription := data.Description
	if workflowDescription == "" {
		workflowDescription = "No description provided"
	}

	return []string{
		fmt.Sprintf("          WORKFLOW_NAME: %q\n", workflowName),
		fmt.Sprintf("          WORKFLOW_DESCRIPTION: %q\n", workflowDescription),
	}
}

func (c *Compiler) buildThreatDetectionContextEnvVars(data *WorkflowData, continueOnError bool, continueOnErrorExpr *string) []string {
	envVars := c.buildWorkflowContextEnvVars(data)
	envVars = append(envVars,
		"          HAS_PATCH: ${{ needs.agent.outputs.has_patch }}\n",
		buildThreatDetectionContinueOnErrorEnvLine(continueOnError, continueOnErrorExpr),
	)

	if data.SafeOutputs != nil && data.SafeOutputs.ThreatDetection != nil && data.SafeOutputs.ThreatDetection.Prompt != "" {
		envVars = append(envVars, fmt.Sprintf("          CUSTOM_PROMPT: %q\n", data.SafeOutputs.ThreatDetection.Prompt))
	}

	return envVars
}

func buildThreatDetectionContinueOnErrorEnvLine(continueOnError bool, continueOnErrorExpr *string) string {
	if continueOnErrorExpr != nil {
		return fmt.Sprintf("          GH_AW_DETECTION_CONTINUE_ON_ERROR: %s\n", *continueOnErrorExpr)
	}
	return fmt.Sprintf("          GH_AW_DETECTION_CONTINUE_ON_ERROR: %q\n", strconv.FormatBool(continueOnError))
}

// buildResultsParsingScriptRequire creates the parsing script that requires the .cjs module.
// The generated code wraps the require() and main() calls in a try/catch so that module load
// failures (e.g. parse_threat_detection_results.cjs not found, setup_globals.cjs missing) still
// set the detection_* outputs to a safe "warning" state instead of leaving them unset.  Unset
// outputs would cause downstream conditions that reference steps.detection_conclusion.outputs.*
// to evaluate to empty strings and could silently bypass the detection gate.
func (c *Compiler) buildResultsParsingScriptRequire() string {
	script := `try {
  const { setupGlobals } = require('` + SetupActionDestination + `/setup_globals.cjs');
  setupGlobals(core, github, context, exec, io, getOctokit);
  const { main } = require('` + SetupActionDestination + `/parse_threat_detection_results.cjs');
  await main();
} catch (loadErr) {
  const continueOnError = process.env.GH_AW_DETECTION_CONTINUE_ON_ERROR !== 'false';
  const detectionExecutionFailed = process.env.DETECTION_AGENTIC_EXECUTION_OUTCOME === 'failure';
  const msg = 'ERR_SYSTEM: \u274C Unexpected error loading threat detection module: ' + (loadErr && loadErr.message ? loadErr.message : String(loadErr));
  core.error(msg);
  core.setOutput('reason', 'parse_error');
  if (continueOnError && !detectionExecutionFailed) {
    core.warning('\u26A0\uFE0F ' + msg);
    core.setOutput('conclusion', 'warning');
    core.setOutput('success', 'false');
  } else {
    core.setOutput('conclusion', 'failure');
    core.setOutput('success', 'false');
    core.setFailed(msg);
  }
}`

	return script
}

// buildCustomThreatDetectionSteps builds YAML steps from user-configured threat detection steps.
// It injects the detection guard condition into each step unless an explicit if: condition is
// already set, ensuring custom steps only run when the detection_guard determines that detection
// should proceed and preventing unexpected side effects in runs with no agent outputs to analyze.
func (c *Compiler) buildCustomThreatDetectionSteps(steps []any) []string {
	var result []string
	for _, step := range steps {
		if stepMap, ok := step.(map[string]any); ok {
			// Inject the detection guard condition unless the user already provided an if: condition.
			if _, hasIf := stepMap["if"]; !hasIf {
				// Clone the map to avoid mutating the original config.
				injected := make(map[string]any, typeutil.SafeAllocationCapacity(len(stepMap), 1))
				maps.Copy(injected, stepMap)
				injected["if"] = detectionStepCondition
				stepMap = injected
			}
			if stepYAML, err := ConvertStepToYAML(stepMap); err == nil {
				result = append(result, stepYAML)
			}
		}
	}
	return result
}

// buildUploadDetectionLogStep creates the step to upload the detection-artifact.
// In workflow_call context, the artifact name is prefixed to avoid name clashes when the
// same reusable workflow is called multiple times within a single workflow run.
// The prefix comes from the agent job output since the detection job depends on the agent job.
func (c *Compiler) buildUploadDetectionLogStep(data *WorkflowData) []string {
	detectionArtifactName := artifactPrefixExprForAgentDownstreamJob(data) + constants.DetectionArtifactName.String()
	steps := []string{
		"      - name: Upload threat detection log\n",
		"        if: always()\n",
		fmt.Sprintf("        uses: %s\n", c.getActionPin("actions/upload-artifact")),
		"        with:\n",
		"          name: " + detectionArtifactName + "\n",
		"          path: |\n",
		"            /tmp/gh-aw/threat-detection/detection.log\n",
		"            " + detectionExecutionEvidencePath + "\n",
		"            " + constants.TmpGhAwDir + "/threat-detection/detection_usage.json\n",
		"            " + constants.TmpGhAwDir + "/threat-detection/detection_usage.jsonl\n",
	}
	if isFirewallEnabled(data) {
		steps = append(steps,
			"            "+detectionFirewallLogsDir+"/logs/\n",
			"            "+detectionFirewallLogsDir+"/audit/\n",
		)
	}
	steps = append(steps, "          if-no-files-found: ignore\n")
	return steps
}

// buildDetectionStepSummaryEchoStep creates a step that echoes the detection engine's
// step-summary file content so the GitHub runner can mask any sensitive values it contains.
//
// The detection engine execution step overrides $GITHUB_STEP_SUMMARY at the step level
// to ThreatDetectionStepSummaryPath so the AWF chroot can write to a reachable path.
// This step then echoes the file content; the runner will apply secret masking to the output.
func (c *Compiler) buildDetectionStepSummaryEchoStep() []string {
	summaryPath := constants.ThreatDetectionStepSummaryPath
	return []string{
		"      - name: Echo detection step summary\n",
		fmt.Sprintf("        if: %s\n", detectionStepCondition),
		"        continue-on-error: true\n",
		"        run: |\n",
		fmt.Sprintf("          if [ -s %s ]; then\n", shellEscapeArg(summaryPath)),
		fmt.Sprintf("            cat %s\n", shellEscapeArg(summaryPath)),
		"          fi\n",
	}
}

// buildRenderDetectionLogStep creates a step that reads detection.log and pipes
// it to stdout wrapped in GitHub Actions group and stop-commands macros so that:
//   - the output is folded into a collapsible section in the Actions log UI, and
//   - any workflow-command-shaped lines in the log are not interpreted by the runner.
//
// Secret redaction (built-in patterns + MCP gateway tokens) is applied before
// the content is written, providing a defence-in-depth layer on top of the
// file-level redaction performed by redact_secrets.cjs.
func (c *Compiler) buildRenderDetectionLogStep(data *WorkflowData) []string {
	steps := []string{
		"      - name: Render detection log\n",
		fmt.Sprintf("        if: %s\n", detectionStepCondition),
		"        continue-on-error: true\n",
		fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)),
		"        with:\n",
		"          script: |\n",
		"            const { setupGlobals } = require('" + SetupActionDestination + "/setup_globals.cjs');\n",
		"            setupGlobals(core, github, context, exec, io, getOctokit);\n",
		"            const { main } = require('" + SetupActionDestination + "/render_detection_log.cjs');\n",
		"            await main();\n",
	}
	return steps
}

// buildInstallThreatDetectStep creates a step that installs the threat-detect binary
// from GitHub Releases at the pinned version. This is used when the gh-aw-detection
// feature flag is set, replacing the inline engine installation steps.
//
// The detection job already tolerates a missing threat-detect binary when continue-on-error
// (warn mode) is in effect: buildDetectionConclusionStep and buildThreatDetectionAnalysisStep
// treat a failed/absent binary as a non-fatal detection failure via
// GH_AW_DETECTION_CONTINUE_ON_ERROR. Without continue-on-error on this install step, a
// transient download failure (e.g. a GitHub Releases CDN blip) would still mark this step —
// and therefore the whole detection job — as `failure`, even though the workflow logic
// already treats a missing binary as non-fatal. Marking the step itself continue-on-error
// in warn mode keeps the job conclusion consistent with that tolerance.
func (c *Compiler) buildInstallThreatDetectStep(data *WorkflowData) []string {
	version := string(constants.DefaultThreatDetectVersion)

	// Determine continue-on-error mode (same logic as buildDetectionConclusionStep).
	continueOnError, continueOnErrorExpr := resolveThreatDetectionContinueOnError(data)

	steps := []string{
		"      - name: Install threat-detect binary\n",
		fmt.Sprintf("        if: %s\n", detectionStepCondition),
	}
	if continueOnErrorExpr != nil {
		steps = append(steps, fmt.Sprintf("        continue-on-error: %s\n", *continueOnErrorExpr))
	} else if continueOnError {
		steps = append(steps, "        continue-on-error: true\n")
	}
	steps = append(steps,
		"        run: |\n",
		fmt.Sprintf("          bash \"${RUNNER_TEMP}/gh-aw/actions/install_threat_detect_binary.sh\" %s\n", version),
	)
	return steps
}
