package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

// collectArtifactPaths gathers all paths for the unified artifact upload.
// It starts from the initial paths already accumulated by generateAgentRunSteps and appends
// engine-declared output paths, log directories, observability files, safe-outputs files,
// patch/bundle paths, and firewall audit paths.
func (c *Compiler) collectArtifactPaths(data *WorkflowData, engine CodingAgentEngine, logFileFull string, initialPaths []string) []string { //nolint:largefunc // Existing artifact policy remains explicit and ordered.
	paths := initialPaths

	// Merge engine-declared output files into the unified artifact instead of creating a
	// separate agent_outputs artifact.
	paths = append(paths, getEngineArtifactPaths(engine)...)

	// Collect MCP logs.
	paths = append(paths, constants.TmpMcpLogsDir)

	// Collect DIFC proxy logs (proxy-tls certs + container stderr) when proxy was injected
	paths = append(paths, difcProxyLogPaths(data)...)

	// Collect MCPScripts logs path if mcp-scripts is enabled
	if IsMCPScriptsEnabled(data.MCPScripts) {
		paths = append(paths, constants.TmpMcpScriptsLogsDir)
	}

	// Include the aggregated agent_usage.json in the agent artifact so third-party
	// tools can consume structured token data without parsing the step summary.
	// Requires AWF v0.25.8+
	if isFirewallEnabled(data) {
		paths = append(paths, constants.TmpGhAwDirSlash+constants.TokenUsageFilename.String())
	}

	// Collect agent stdio logs path for unified upload
	paths = append(paths, logFileFull)

	// Include the pre-agent audit file (file listing of agent-related directories captured
	// before agent execution) so it is available in the agent artifact for post-run inspection.
	paths = append(paths, constants.PreAgentAuditFilePath.String())

	// Collect GitHub API rate-limit log for observability.
	// Written by github_rate_limit_logger.cjs during REST API calls.
	paths = append(paths, constants.TmpGhAwDirSlash+constants.GithubRateLimitsFilename.String())

	// Collect OTLP span mirror — enables post-hoc trace debugging without a live collector.
	// Written by send_otlp_span.cjs; each line is a full OTLP/HTTP JSON traces payload.
	// Only included when OTLP is configured for this workflow.
	if isOTLPEnabled(data) {
		paths = append(paths, constants.TmpGhAwDirSlash+constants.OtelJsonlFilename.String())
		paths = append(paths, constants.TmpGhAwDirSlash+constants.OtlpExportErrorsFilename.String())
	}

	// Collect grader manifest and results when graders are configured.
	if data.Graders != nil && data.Graders.HasGraders() {
		paths = append(paths, collectGraderArtifactPaths(data.Graders)...)
	}

	// Collect safe outputs and agent output paths for the unified artifact.
	// These were previously uploaded as separate safe-output and agent-output artifacts.
	if data.SafeOutputs != nil {
		// Raw safe-output NDJSON (copied to /tmp/gh-aw/ by generateOutputCollectionStep)
		paths = append(paths, constants.TmpGhAwDirSlash+constants.SafeOutputsFilename.String())
		// Processed agent output JSON produced by collect_ndjson_output.cjs
		paths = append(paths, constants.TmpGhAwDirSlash+constants.AgentOutputFilename.String())
		if data.CommentMemoryConfig != nil {
			paths = append(paths, constants.TmpCommentMemoryDir)
		}
	}

	// Collect git patch path if safe-outputs with PR operations is configured.
	// NOTE: Git patch generation has been moved to the safe-outputs MCP server.
	// The patch is now generated when create_pull_request or push_to_pull_request_branch
	// tools are called, providing immediate error feedback if no changes are present.
	// Include patches in the artifact when:
	// 1. Safe outputs needs them for checkout (non-staged create_pull_request/push_to_pull_request_branch)
	// 2. Threat detection is enabled (detection job needs patches for security analysis, even when the
	//    safe-output handler is staged and doesn't need checkout itself)
	threatDetectionNeedsPatches := IsDetectionJobEnabled(data.SafeOutputs)
	if usesPatchesAndCheckouts(data.SafeOutputs) || threatDetectionNeedsPatches {
		paths = append(paths, constants.TmpAwPatchGlob)
		// Bundle files are generated when patch-format: bundle is configured and are
		// required downstream to apply changes while preserving merge topology.
		// Bundles are binary and cannot be text-scanned directly, but they are built
		// from the same git commit range as the accompanying .patch file, so the
		// .patch scanning above already covers the underlying diff content
		// (see isKnownUnscannedButAllowedForUpload in step_order_validation.go). The
		// artifact upload step already sets if-no-files-found: ignore, so including
		// the bundle glob here is safe even when no bundle files exist.
		paths = append(paths, constants.TmpAwBundleGlob)
	}

	// Include firewall audit/observability logs in the unified agent artifact
	// so all agent job outputs ship as a single artifact (AWF v0.25.0+).
	if isFirewallEnabled(data) {
		if isArcDindTopology(data) {
			// On ARC/DinD, logs are under ${{ runner.temp }}/gh-aw (daemon-visible path).
			// Use ${{ runner.temp }} because `with:` blocks expand Actions expressions, not shell vars.
			paths = append(paths, constants.AWFConfigFilePathExpr)
			paths = append(paths, constants.AWFProxyLogsDirExpr+"/")
			paths = append(paths, constants.AWFAuditDirExpr+"/")
			paths = append(paths, constants.AWFReflectFilePathExpr)
		} else {
			paths = append(paths, constants.AWFConfigFilePath.String())
			paths = append(paths, constants.AWFProxyLogsDir.String()+"/")
			paths = append(paths, constants.AWFAuditDir.String()+"/")
			// Include the AWF /reflect payload persisted by the agent harness.
			// Co-located under /tmp/gh-aw/sandbox/firewall/ so the existing
			// chmod -R a+rX step covers its permissions before upload.
			paths = append(paths, constants.AWFReflectFilePath.String())
		}
	}

	// For ARC/DinD, rewrite all /tmp/gh-aw/ paths to ${{ runner.temp }}/gh-aw/ so
	// the artifact upload has a single root. A consolidation step (emitted before upload)
	// copies the files from /tmp/gh-aw/ to the runner.temp location. See gh-aw#34896 Bug B.
	if isArcDindTopology(data) {
		paths = rewriteTmpGhAwPathsForArcDind(paths)
	}

	compilerYamlLog.Printf("Collected %d artifact path(s) for unified agent upload", len(paths))
	return paths
}

// generateSummarySteps emits all GITHUB_STEP_SUMMARY log-parsing steps for the agent job.
// It covers agent log parsing, MCP scripts, MCP gateway, firewall logs, token usage,
// AWF reflect summary, and observability summary.
func (c *Compiler) generateSummarySteps(yaml *strings.Builder, data *WorkflowData, engine CodingAgentEngine) {
	// Parse agent logs for GITHUB_STEP_SUMMARY
	c.generateLogParsing(yaml, data, engine)

	// Parse mcp-scripts logs for GITHUB_STEP_SUMMARY (if mcp-scripts is enabled)
	if IsMCPScriptsEnabled(data.MCPScripts) {
		c.generateMCPScriptsLogParsing(yaml, data)
	}

	// Parse MCP gateway logs for GITHUB_STEP_SUMMARY.
	// The MCP gateway is always enabled, even when agent sandbox is disabled.
	c.generateMCPGatewayLogParsing(yaml, data)

	// Add firewall log parsing for all firewall-enabled engines.
	// This replaces the previous per-engine blocks (Copilot, Codex, Claude) and extends
	// support to all engines (including Gemini) so every agentic workflow uploads audit logs.
	if isFirewallEnabled(data) {
		firewallLogParsing := generateFirewallLogParsingStep(data.Name, data)
		for _, line := range firewallLogParsing {
			yaml.WriteString(line)
			yaml.WriteByte('\n')
		}
	}

	// Parse token-usage.jsonl and append to step summary (requires AWF v0.25.8+)
	if isFirewallEnabled(data) {
		c.generateTokenUsageSummary(yaml, data)
	}

	// Append AWF API proxy reflection data (available endpoints and models) to step summary.
	// This data is fetched from the /reflect endpoint by copilot_harness.cjs before the
	// agent exits and persisted to /tmp/gh-aw/awf-reflect.json.
	if isFirewallEnabled(data) {
		c.generateAWFReflectSummary(yaml, data)
	}

	// Synthesize a compact observability section from runtime artifacts when OTLP is enabled.
	c.generateObservabilitySummary(yaml, data)
}

// generatePostAgentCollectionAndUpload orchestrates the post-agent phase:
// engine output cleanup, access log collection, artifact path accumulation via collectArtifactPaths,
// step-summary generation via generateSummarySteps, safe-outputs/memory/staging artifact uploads,
// post-steps, the unified artifact upload, token invalidation, dev-mode actions restore,
// and step-order validation.
func (c *Compiler) generatePostAgentCollectionAndUpload(yaml *strings.Builder, data *WorkflowData, engine CodingAgentEngine, artifactPaths []string, logFileFull string, checkoutMgr *CheckoutManager) error { //nolint:largefunc // Existing post-agent orchestration preserves generated step order.
	compilerYamlLog.Print("Generating post-agent collection and upload steps")
	// Generate engine output cleanup step so workspace files are removed after collection.
	// The engine-declared output paths are gathered by collectArtifactPaths below.
	if len(getEngineArtifactPaths(engine)) > 0 {
		c.generateEngineOutputCleanup(yaml, engine)
	}

	// Extract and upload squid access logs (if any proxy tools were used)
	c.generateExtractAccessLogs(yaml, data.Tools)
	c.generateUploadAccessLogs(yaml, data.Tools)

	// Collect all artifact paths for the unified upload.
	artifactPaths = c.collectArtifactPaths(data, engine, logFileFull, artifactPaths)

	// Emit all GITHUB_STEP_SUMMARY log-parsing steps.
	c.generateSummarySteps(yaml, data, engine)

	// Run deterministic graders after trace data is available.
	c.generateGradersStep(yaml, data)

	// Re-scan grader output files for leaked secrets when custom grader scripts
	// are present. Custom scripts evaluate trace data that may contain
	// credential-bearing strings written after the initial workspace redaction.
	c.generateGraderRedactionStep(yaml, yaml.String(), data)

	// Write a minimal agent_output.json placeholder when the engine fails before
	// producing any safe outputs, so downstream safe_outputs and conclusion jobs
	// receive a valid (empty) JSON file instead of an ENOENT error.
	// The placeholder is only written if the engine did not already write the file.
	if data.SafeOutputs != nil {
		c.generateAgentOutputPlaceholderStep(yaml)
	}

	// Add post-execution cleanup step for Copilot engine
	if copilotEngine, ok := engine.(*CopilotEngine); ok {
		cleanupStep := copilotEngine.GetCleanupStep(data)
		for _, line := range cleanupStep {
			yaml.WriteString(line)
			yaml.WriteByte('\n')
		}
	}

	// Add repo-memory artifact upload to save state for push job
	generateRepoMemoryArtifactUpload(yaml, data, c.getActionPin)

	// Add cache-memory git commit steps (after agent execution, before validation)
	// This commits agent-written changes to the current integrity branch.
	generateCacheMemoryGitCommitSteps(yaml, data)

	// Add cache-memory validation (after agent execution)
	// This validates file types before cache is saved or uploaded
	generateCacheMemoryValidation(yaml, data)

	// Add cache-memory artifact upload (after agent execution)
	// This ensures artifacts are uploaded after the agent has finished modifying the cache
	generateCacheMemoryArtifactUpload(yaml, data, c.getActionPin)

	// Commit and validate drive-memory changes, then either publish them directly or
	// stage them for the post-detection update job.
	generateDriveMemoryGitCommitSteps(yaml, data)
	generateDriveMemoryValidation(yaml, data)
	generateDriveMemoryPersistence(yaml, data, c.getActionPin)

	// Add safe-outputs assets artifact upload (after agent execution)
	// This creates a separate artifact for assets that will be downloaded by upload_assets job
	generateSafeOutputsAssetsArtifactUpload(yaml, data, c.getActionPin)

	// Add safe-outputs upload-artifact staging upload (after agent execution)
	// This creates a separate artifact for files the model staged for artifact upload,
	// to be downloaded and processed by the upload_artifact job
	generateSafeOutputsArtifactStagingUpload(yaml, data, c.getActionPin)

	// Add safe-outputs upload-code-coverage staging upload (after agent execution)
	// This creates a separate artifact for the coverage report file the model staged,
	// to be downloaded and processed by the upload_code_coverage job
	generateSafeOutputsCodeCoverageStagingUpload(yaml, data, c.getActionPin)

	// Add post-steps (if any) after AI execution
	c.generatePostSteps(yaml, data)

	// For ARC/DinD, consolidate all artifact files under ${{ runner.temp }}/gh-aw/
	// before upload. Without this, upload-artifact receives paths from two roots
	// (/tmp/gh-aw/ and ${{ runner.temp }}/gh-aw/), computes "/" as the common ancestor,
	// and creates a nested directory layout that breaks downstream artifact downloads.
	// See gh-aw#34896 Bug B.
	if isArcDindTopology(data) {
		c.generateArcDindArtifactConsolidationStep(yaml)
	}

	// Generate single unified artifact upload with all collected paths.
	// In workflow_call context, apply the per-invocation prefix to avoid name clashes.
	agentArtifactPrefix := artifactPrefixExprForDownstreamJob(data)
	compilerYamlLog.Printf("Emitting unified agent artifact upload with %d path(s)", len(artifactPaths))
	c.generateAgentOutputFallbackUpload(yaml, data, agentArtifactPrefix)
	c.generateUnifiedArtifactUpload(yaml, artifactPaths, agentArtifactPrefix)

	// In dev mode the setup action is referenced via a local path (./actions/setup), so its files
	// live in the workspace. When a checkout: entry targets an external repository without a path
	// (e.g. "checkout: [{repository: owner/other-repo}]"), actions/checkout replaces the workspace
	// root with the external repository content, removing the actions/setup directory.
	// Without restoring it, the runner's post-step for Setup Scripts would fail with
	// "Can't find 'action.yml', 'action.yaml' or 'Dockerfile' under .../actions/setup".
	// We add a restore checkout step (if: always()) as the final step so the post-step
	// can always find action.yml and complete its /tmp/gh-aw cleanup.
	if c.actionMode.IsDev() && checkoutMgr.HasExternalRootCheckout() {
		yaml.WriteString(c.generateRestoreActionsSetupStep())
		compilerYamlLog.Print("Added restore actions folder step to agent job (dev mode with external root checkout)")
	}

	// Validate step ordering - this is a compiler check to ensure security
	if err := c.stepOrderTracker.ValidateStepOrdering(); err != nil {
		// This is a compiler bug if validation fails
		return fmt.Errorf("step ordering validation failed: %w", err)
	}
	return nil
}
