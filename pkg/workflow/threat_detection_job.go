// Package workflow - top-level detection job assembler.
package workflow

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/sliceutil"
)

// buildDetectionJob creates a separate detection job that runs after the agent job.
// The job downloads the agent artifact to access output files, then runs all threat detection
// steps. It outputs detection_success and detection_conclusion for downstream jobs.
// Returns nil if threat detection is not configured.
func (c *Compiler) buildDetectionJob(data *WorkflowData) (*Job, error) {
	threatLog.Print("Building separate detection job")
	if data.SafeOutputs == nil || data.SafeOutputs.ThreatDetection == nil {
		threatLog.Print("Threat detection not configured, skipping detection job")
		return nil, nil
	}

	// When the engine is explicitly disabled and there are no custom steps,
	// there is nothing to run in the detection job — skip it entirely.
	// The detection job would only create an empty detection.log and the parser
	// would correctly fail with "No THREAT_DETECTION_RESULT found".
	if !IsDetectionJobEnabled(data.SafeOutputs) {
		threatLog.Print("Threat detection engine disabled with no custom steps, skipping detection job")
		return nil, nil
	}

	var steps []string

	// Add setup action steps (same as agent job - installs the agentic engine)
	setupActionRef := c.resolveActionReference("./actions/setup", data)
	if setupActionRef != "" || c.actionMode.IsScript() {
		// For dev mode (local action path), checkout the actions folder first
		steps = append(steps, c.generateCheckoutActionsFolder(data)...)
		// Detection job depends on agent job; reuse the agent's trace ID so all jobs share one OTLP trace
		detectionTraceID := fmt.Sprintf("${{ needs.%s.outputs.setup-trace-id }}", constants.ActivationJobName)
		detectionParentSpanID := setupParentSpanNeedsExpr(constants.ActivationJobName)
		steps = append(steps, c.generateSetupStepForJob("detection", data, setupActionRef, SetupActionDestination, false, detectionTraceID, detectionParentSpanID, "")...)
	}

	// Download the activation artifact first because it is the durable source of the
	// generated prompt files. The larger agent artifact also contains prompt files,
	// but that upload is best-effort and may be unavailable when detection runs.
	steps = append(steps, buildDetectionActivationArtifactDownloadSteps(data, c.getActionPin)...)

	// Download agent output artifact to access output files (agent_output.json, patches).
	// Use agent-downstream prefix since this job depends on the agent job.
	agentArtifactPrefix := artifactPrefixExprForAgentDownstreamJob(data)
	steps = append(steps, buildAgentOutputDownloadSteps(agentArtifactPrefix, c.getActionPin)...)

	// Download experiment artifact so the detection agent can read the current variant assignments.
	// The experiment artifact is uploaded by the activation job.
	steps = append(steps, buildExperimentArtifactDownloadSteps(data, c.getActionPin)...)

	// Conditionally checkout the target repository so the detection engine can
	// analyze patches in the context of the surrounding codebase.
	steps = append(steps, c.buildWorkspaceCheckoutForDetectionStep(data)...)

	// Add all threat detection steps
	detectionStepsContent := c.buildDetectionJobSteps(data)
	steps = append(steps, detectionStepsContent...)

	// Build job outputs
	outputs := map[string]string{
		"detection_success":    "${{ steps.detection_conclusion.outputs.success }}",
		"detection_conclusion": "${{ steps.detection_conclusion.outputs.conclusion }}",
		"detection_reason":     "${{ steps.detection_conclusion.outputs.reason }}",
		"aic":                  "${{ steps.parse_detection_token_usage.outputs.aic }}",
	}

	// Detection job depends on agent job and activation job (for trace ID)
	needs := []string{string(constants.AgentJobName), string(constants.ActivationJobName)}

	// Scan the effective detection engine env values for needs.<customJob>.outputs.*
	// expressions and add the referenced custom jobs as direct dependencies of the
	// detection job. Use the merged env (main + detection-specific overrides) so
	// that expressions from the main engine.env are not missed when a detection
	// engine config exists.
	var detectionSpecificEnv map[string]string
	if data.SafeOutputs != nil && data.SafeOutputs.ThreatDetection != nil && data.SafeOutputs.ThreatDetection.EngineConfig != nil {
		detectionSpecificEnv = data.SafeOutputs.ThreatDetection.EngineConfig.Env
	}
	effectiveDetectionEnv := mergeThreatDetectionEngineEnv(data, detectionSpecificEnv)
	if len(effectiveDetectionEnv) > 0 {
		var engineEnvBuilder strings.Builder
		for _, envValue := range effectiveDetectionEnv {
			engineEnvBuilder.WriteByte('\n')
			engineEnvBuilder.WriteString(envValue)
		}
		engineEnvContent := engineEnvBuilder.String()
		hasNeedsReference := strings.Contains(engineEnvContent, "needs.")
		if len(data.Jobs) > 0 {
			engineEnvJobs := c.getReferencedCustomJobs(engineEnvContent, data.Jobs)
			for _, jobName := range engineEnvJobs {
				if isBuiltinJobName(jobName) {
					continue
				}
				if !slices.Contains(needs, jobName) {
					needs = append(needs, jobName)
					threatLog.Printf("Added custom job '%s' to detection needs because it's referenced in engine.env", jobName)
				}
			}
		}
		if hasNeedsReference {
			for _, builtinJobName := range sliceutil.SortedKeys(constants.KnownBuiltInJobNames) {
				if slices.Contains(needs, builtinJobName) {
					continue
				}
				if strings.Contains(engineEnvContent, fmt.Sprintf("needs.%s.", builtinJobName)) {
					warningMsg := fmt.Sprintf(
						"engine.env references built-in job '%s' in a detection-job needs expression. "+
							"Built-in jobs are managed by the compiler and cannot be added as direct detection dependencies; "+
							"this expression will silently evaluate to an empty string at runtime.",
						builtinJobName,
					)
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(warningMsg))
					c.IncrementWarningCount()
				}
			}
		}
	}

	// Determine runs-on: use threat detection override if set, otherwise ubuntu-latest.
	// The detection job runs on a fresh runner separate from the agent job, so it does
	// not need the same custom runner as safe-outputs.
	runsOn := "runs-on: ubuntu-latest"
	if data.SafeOutputs.ThreatDetection.RunsOn != "" {
		runsOn = normalizeRunsOnSnippet(data.SafeOutputs.ThreatDetection.RunsOn)
	}

	// Detection job condition: always run whenever the agent job was not skipped,
	// regardless of whether the agent produced outputs (output_types) or a patch.
	// This ensures detection is never bypassed even when the agent calls noop/boop —
	// the detection_guard step inside the job handles the no-output case by setting
	// run_detection=false, and detection_conclusion short-circuits with conclusion=skipped,
	// success=true, so downstream jobs (safe_outputs, update_cache_memory) see
	// needs.detection.result == 'success' and behave correctly.
	alwaysFunc := BuildFunctionCall("always")
	agentNotSkipped := BuildNotEquals(
		BuildPropertyAccess(fmt.Sprintf("needs.%s.result", constants.AgentJobName)),
		BuildStringLiteral("skipped"),
	)
	jobConditionNode := BuildAnd(alwaysFunc, agentNotSkipped)

	// When detection is expression-controlled, add the caller expression to the condition so
	// GitHub Actions skips the detection job at runtime when the expression evaluates to false.
	if data.SafeOutputs.ThreatDetection.EnabledExpr != nil {
		rawExpr := extractRawExpression(*data.SafeOutputs.ThreatDetection.EnabledExpr)
		jobConditionNode = BuildAnd(jobConditionNode, &ExpressionNode{Expression: rawExpr})
		threatLog.Printf("Detection job condition includes runtime expression: %s", rawExpr)
	}

	jobCondition := RenderCondition(jobConditionNode)

	// Determine permissions for the detection job.
	// - Always grant contents: read because the workspace checkout (for patch context)
	//   requires it, and contents: read is a minimal read-only permission.
	//   The checkout is conditional on has_patch at runtime, but permissions cannot
	//   be set conditionally in GitHub Actions.
	// - In dev/script mode, contents: read is also needed for the actions folder checkout.
	// - When permissions.copilot-requests is set to write, the detection job runs the Copilot CLI
	//   and requires copilot-requests: write for authentication.
	// - When the engine uses GitHub OIDC (WIF) auth, the detection job's api-proxy also needs
	//   to mint a GitHub OIDC token for the token exchange. Without id-token: write,
	//   ACTIONS_ID_TOKEN_REQUEST_URL/TOKEN are not set in the runner environment and the
	//   api-proxy returns HTTP 401 on every request (mirrors validateOIDCPermissions logic).
	// - When observability.otlp.github-app is configured without app-id/private-key
	//   credentials, id-token: write is also needed (mirrors validateOIDCPermissions).
	copilotRequestsEnabled := hasCopilotRequestsWritePermission(data)
	perms := NewPermissionsContentsRead()
	if copilotRequestsEnabled {
		perms.Set(PermissionCopilotRequests, PermissionWrite)
	}
	if data.EngineConfig != nil && data.EngineConfig.Auth != nil && data.EngineConfig.Auth.Type == "github-oidc" {
		perms.Set(PermissionIdToken, PermissionWrite)
	}
	if hasOTLPGitHubOIDCAuth(data.ParsedFrontmatter, data.RawFrontmatter) {
		perms.Set(PermissionIdToken, PermissionWrite)
	}
	permissions := perms.RenderToYAML()

	// Determine environment: use threat detection override if set, otherwise inherit from
	// the top-level environment (matching the same unconditional fallback used by agent
	// and safe-output jobs so that environment-scoped secrets are accessible).
	environment := data.Environment
	if data.SafeOutputs.ThreatDetection.Environment != "" {
		// ThreatDetectionConfig.Environment holds the raw environment name; normalize it to
		// a YAML field so Job.Environment renders as "environment: <name>" not just "<name>".
		environment = "environment: " + data.SafeOutputs.ThreatDetection.Environment
	}

	job := &Job{
		Name:                     string(constants.DetectionJobName),
		Needs:                    needs,
		If:                       jobCondition,
		RunsOn:                   c.indentYAMLLines(runsOn, "    "),
		Environment:              c.indentYAMLLines(environment, "    "),
		Permissions:              permissions,
		Steps:                    steps,
		Outputs:                  outputs,
		TimeoutMinutesExpression: resolveDetectionJobTimeoutValue(data),
	}

	threatLog.Printf("Built detection job with %d steps, depends on: %v", len(steps), needs)
	return job, nil
}

func buildDetectionActivationArtifactDownloadSteps(data *WorkflowData, pinAction func(string) string) []string {
	return buildArtifactDownloadSteps(ArtifactDownloadConfig{
		ArtifactName: artifactPrefixExprForDownstreamJob(data) + constants.ActivationArtifactName.String(),
		DownloadPath: constants.TmpGhAwDir,
		StepName:     "Download activation artifact",
	}, pinAction)
}
