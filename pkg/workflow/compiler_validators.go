package workflow

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/goccy/go-yaml"
)

// validateExpressions checks expression safety and runtime-import file references
// embedded in the workflow's markdown content. It is the first validator called in
// validateWorkflowData and guards against unsafe GitHub Actions expressions.
func (c *Compiler) validateExpressions(workflowData *WorkflowData, markdownPath string) error {
	if envMap := parseEnvYAMLSection(workflowData.Env); len(envMap) > 0 {
		if err := validateTopLevelEnvExpressions(envMap); err != nil {
			return formatCompilerError(markdownPath, "error", err.Error(), err)
		}
	}

	// Check for secrets serialization expressions FIRST — before the general allowlist —
	// to provide a specific, actionable error/warning message.
	// In strict mode this returns an error that stops further validation.
	// In non-strict mode it emits a warning and continues.
	if err := c.validateSecretsSerializationExpressions(workflowData); err != nil {
		return formatCompilerError(markdownPath, "error", err.Error(), err)
	}

	// Validate expression safety - check that all GitHub Actions expressions are in the allowed list.
	// In non-strict mode, ${{ toJSON(secrets) }} occurrences were already warned about above;
	// neutralize them so the allowlist does not re-surface them as errors.
	if strings.Contains(workflowData.MarkdownContent, "${{") {
		workflowLog.Printf("Validating expression safety")
		markdownForAllowlist := workflowData.MarkdownContent
		if !c.effectiveStrictMode(workflowData.RawFrontmatter) {
			markdownForAllowlist = neutralizeSecretsSerializationExpressions(markdownForAllowlist)
		}
		if err := validateExpressionSafety(markdownForAllowlist); err != nil {
			return formatCompilerError(markdownPath, "error", err.Error(), err)
		}
	}

	// Validate expressions in runtime-import files at compile time
	runtimeImportValidationSeed := runtimeImportValidationMarkdown(workflowData)
	if strings.Contains(runtimeImportValidationSeed, "{{#runtime-import") || strings.Contains(runtimeImportValidationSeed, "{{#import") {
		workflowLog.Printf("Validating runtime-import files")
		workspaceDir := resolveWorkspaceRoot(markdownPath)
		subAgentWarnings, err := validateRuntimeImportFiles(runtimeImportValidationSeed, workspaceDir)
		// Emit best-effort sub-agent frontmatter warnings through the normal warning path
		// so they are counted and consistently formatted with all other warnings.
		for _, w := range subAgentWarnings {
			expressionValidationLog.Printf("%s", w)
			fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(w))
			c.IncrementWarningCount()
		}
		if err != nil {
			return formatCompilerError(markdownPath, "error", err.Error(), err)
		}
	}

	// Warn when the prompt explicitly references /tmp/ or /tmp/gh-aw/ directly instead
	// of the recommended /tmp/gh-aw/agent/ subtree.
	c.validatePromptTmpPaths(workflowData, markdownPath)

	// Detect agent-job step outputs referenced in the prompt. The prompt is rendered
	// in the activation job; agent-job steps run later and their outputs are not
	// available at prompt-creation time.
	if err := validateStepsOutputsNotInPrompt(workflowData); err != nil {
		return formatCompilerError(markdownPath, "error", err.Error(), err)
	}

	return nil
}

func runtimeImportValidationMarkdown(workflowData *WorkflowData) string {
	if workflowData == nil {
		return ""
	}

	var seed strings.Builder
	seed.WriteString(workflowData.MarkdownContent)
	if workflowData.MainWorkflowMarkdown != "" {
		seed.WriteByte('\n')
		seed.WriteString(workflowData.MainWorkflowMarkdown)
	}
	for _, importPath := range workflowData.ImportPaths {
		seed.WriteString("\n{{#runtime-import ")
		seed.WriteString(filepath.ToSlash(importPath))
		seed.WriteString("}}")
	}
	for _, entry := range workflowData.PromptImports {
		if entry.ImportPath == "" {
			continue
		}
		seed.WriteString("\n{{#runtime-import ")
		seed.WriteString(filepath.ToSlash(entry.ImportPath))
		seed.WriteString("}}")
	}
	return seed.String()
}

// tmpNeedle is the literal prefix to scan for in prompt content.
const tmpNeedle = "/tmp/"

// tmpSafePrefix is the /tmp subtree managed by the gh-aw framework.
// All paths under /tmp/gh-aw/ are considered safe (agent output, cache-memory,
// repo-memory, comment-memory, aw-mcp, mcp-scripts, etc.).
const tmpSafePrefix = "/tmp/gh-aw/"

// warnPromptTmpPaths returns a non-empty advisory message when content contains
// /tmp/ references that are not under /tmp/gh-aw/.
// Returns an empty string when no problematic patterns are found.
func warnPromptTmpPaths(content string) string {
	if !strings.Contains(content, tmpNeedle) {
		return ""
	}
	// Walk every /tmp/ occurrence; warn as soon as one is not under /tmp/gh-aw/.
	rest := content
	for {
		pos := strings.Index(rest, tmpNeedle)
		if pos < 0 {
			break
		}
		segment := rest[pos:]
		if !strings.HasPrefix(segment, tmpSafePrefix) {
			return "Prompt references /tmp/ directly. " +
				"Use /tmp/gh-aw/agent/ as the root for all temporary files " +
				"generated by the agent — its contents are uploaded as a run artifact."
		}
		rest = rest[pos+len(tmpNeedle):]
	}
	return ""
}

// validatePromptTmpPaths emits an advisory warning when the workflow markdown body
// references /tmp/ or /tmp/gh-aw/ instead of the recommended /tmp/gh-aw/agent/ root.
func (c *Compiler) validatePromptTmpPaths(workflowData *WorkflowData, markdownPath string) {
	if msg := warnPromptTmpPaths(workflowData.MarkdownContent); msg != "" {
		fmt.Fprintln(os.Stderr, formatCompilerMessage(markdownPath, "warning", msg))
		c.IncrementWarningCount()
	}
}

// validateFeatureConfig validates feature flags declared in the workflow frontmatter
// and applies any action-mode override specified via the "action-mode" feature flag.
func (c *Compiler) validateFeatureConfig(workflowData *WorkflowData, markdownPath string) error {
	// Validate feature flags
	workflowLog.Printf("Validating feature flags")
	if err := validateFeatures(workflowData); err != nil {
		return formatCompilerError(markdownPath, "error", err.Error(), err)
	}

	// Check for action-mode feature flag override
	if workflowData.Features != nil {
		if actionModeVal, exists := workflowData.Features["action-mode"]; exists {
			if actionModeStr, ok := actionModeVal.(string); ok && actionModeStr != "" {
				mode := ActionMode(actionModeStr)
				if !mode.IsValid() {
					return formatCompilerError(markdownPath, "error", fmt.Sprintf("invalid action-mode feature flag '%s'. Must be 'dev', 'release', or 'script'", actionModeStr), nil)
				}
				workflowLog.Printf("Overriding action mode from feature flag: %s", mode)
				c.SetActionMode(mode)
			}
		}
	}

	return nil
}

// validateToolConfiguration validates safe-outputs settings, on.needs and safe-job
// declarations, network configuration, labels, concurrency expressions, sandbox
// security constraints, GitHub tool-to-toolset alignment, the agentic-workflows
// permission requirement, and dispatch/call-workflow configurations.
// workflowPermissions is the *Permissions value returned by validatePermissions.
func (c *Compiler) validateToolConfiguration(workflowData *WorkflowData, markdownPath string, workflowPermissions *Permissions) error {
	workflowLog.Printf("Validating agent file if specified")
	if err := c.validateAgentFile(workflowData, markdownPath); err != nil {
		return err
	}
	if err := c.validateCoreToolConfiguration(workflowData, markdownPath); err != nil {
		return err
	}
	if err := validateSteeringIssuePermissions(workflowData, workflowPermissions); err != nil {
		return formatCompilerError(markdownPath, "error", err.Error(), err)
	}
	if err := c.validateConcurrencyConfiguration(workflowData, markdownPath); err != nil {
		return err
	}
	c.emitGeneralToolWarnings(workflowData, markdownPath)
	c.resolveFrontmatterSkillRefs(workflowData, markdownPath)
	if err := c.validatePlugins(workflowData); err != nil {
		return err
	}
	if err := c.validatePluginSupport(workflowData); err != nil {
		return err
	}
	if err := c.resolveFrontmatterPluginRefs(workflowData); err != nil {
		return err
	}
	if err := c.validateThreatDetectionSandboxRequirement(workflowData, markdownPath); err != nil {
		return err
	}
	if err := c.validateGitHubToolsAndPermissions(workflowData, markdownPath, workflowPermissions); err != nil {
		return err
	}
	return c.validateResourcesAndDispatches(workflowData, markdownPath)
}

func (c *Compiler) validateCoreToolConfiguration(workflowData *WorkflowData, markdownPath string) error {
	validations := []struct {
		logMessage string
		validateFn func() error
	}{
		{logMessage: "Validating sandbox configuration", validateFn: func() error { return validateSandboxConfig(workflowData) }},
		{logMessage: "Validating GitHub CLI proxy version", validateFn: func() error { return validateGitHubCLIProxyVersion(workflowData) }},
		{logMessage: "Validating safe-outputs target fields", validateFn: func() error { return validateSafeOutputsTarget(workflowData.SafeOutputs) }},
		{logMessage: "Validating safe-outputs max fields", validateFn: func() error { return validateSafeOutputsMax(workflowData.SafeOutputs) }},
		{logMessage: "Validating steering issue configuration", validateFn: func() error { return validateSteeringIssue(workflowData) }},
		{logMessage: "Validating safe-outputs data schema", validateFn: func() error { return validateSafeOutputsDataSchema(workflowData.SafeOutputs) }},
		{logMessage: "Validating safe-outputs samples entries against MCP tool schemas", validateFn: func() error { return validateSafeOutputsSamples(workflowData.SafeOutputs) }},
		{logMessage: "Validating safe-outputs urls policy", validateFn: func() error { return validateSafeOutputsURLs(workflowData.SafeOutputs) }},
		{logMessage: "Validating safe-outputs allowed-domains", validateFn: func() error { return c.validateSafeOutputsAllowedDomains(workflowData.SafeOutputs) }},
		{logMessage: "Validating safe-outputs merge-pull-request", validateFn: func() error { return validateSafeOutputsMergePullRequest(workflowData.SafeOutputs) }},
		{logMessage: "Validating safe-outputs add-labels permissions", validateFn: func() error { return validateAddLabelsPermissions(workflowData.SafeOutputs) }},
		{logMessage: "Validating safe-outputs remove-labels permissions", validateFn: func() error { return validateRemoveLabelsPermissions(workflowData.SafeOutputs) }},
		{logMessage: "Validating safe-outputs needs declarations", validateFn: func() error { return validateSafeOutputsNeeds(workflowData) }},
		{logMessage: "Validating on.needs declarations", validateFn: func() error { return c.validateOnNeeds(workflowData) }},
		{logMessage: "Validating safe-job needs declarations", validateFn: func() error { return validateSafeJobNeeds(workflowData) }},
		{logMessage: "Validating safe-outputs allowed-labels glob scope", validateFn: func() error { return c.validateSafeOutputsAllowedLabelsGlobScope(workflowData.SafeOutputs) }},
		{logMessage: "Validating network allowed domains", validateFn: func() error { return c.validateNetworkAllowedDomains(workflowData.NetworkPermissions) }},
		{logMessage: "Validating network firewall configuration", validateFn: func() error { return validateNetworkFirewallConfig(workflowData.NetworkPermissions) }},
		{logMessage: "Validating safe-outputs allow-workflows", validateFn: func() error { return validateSafeOutputsAllowWorkflows(workflowData.SafeOutputs) }},
		{logMessage: "Validating safe-outputs approve-workflow-run authentication", validateFn: func() error { return validateSafeOutputsApproveWorkflowRun(workflowData.SafeOutputs) }},
		{logMessage: "Validating OTLP resource attributes", validateFn: func() error { return validateOTLPResourceAttributes(workflowData) }},
		{logMessage: "Validating labels", validateFn: func() error { return validateLabels(workflowData) }},
		{logMessage: "Validating workflow_dispatch input requirements for command triggers", validateFn: func() error { return validateCommandWorkflowDispatchInputs(workflowData) }},
		{logMessage: "Validating max-daily-ai-credits frontmatter", validateFn: func() error { return validateMaxDailyAICFrontmatter(workflowData) }},
		{logMessage: "Validating private-to-public-flows string value", validateFn: func() error { return validatePrivateToPublicFlowsStringValue(workflowData) }},
		{logMessage: "Validating private-to-public-flows server IDs", validateFn: func() error { return validatePrivateToPublicFlowsServerIDs(workflowData) }},
		{logMessage: "Validating GCP WIF engine auth required fields", validateFn: func() error { return validateGCPWIFEngineAuth(workflowData) }},
		{logMessage: "Validating OTLP workload identity configuration", validateFn: func() error { return validateOTLPWorkloadIdentity(workflowData) }},
		{logMessage: "Validating default AI credits pricing values", validateFn: func() error { return validateDefaultAiCreditsPricing(workflowData) }},
		{logMessage: "Validating enclaves configuration", validateFn: func() error { return validateEnclavesConfig(workflowData) }},
		{logMessage: "Validating drive-memory runtime", validateFn: func() error { return validateDriveMemoryRuntime(workflowData) }},
	}
	// This validation is intentionally outside the table below because strict mode
	// turns the same validation result into either an error or a warning.
	workflowLog.Printf("Validating safe-outputs steps for shell expansion patterns")
	if err := validateSafeOutputsStepsShellExpansion(workflowData.SafeOutputs); err != nil {
		if c.strictMode {
			return formatCompilerError(markdownPath, "error", err.Error(), err)
		}
		fmt.Fprintln(os.Stderr, formatCompilerMessage(markdownPath, "warning", err.Error()))
		c.IncrementWarningCount()
	}
	workflowLog.Printf("Validating cross-repo checkout paths")
	c.deriveAndWarnCrossRepoCheckoutPaths(workflowData.CheckoutConfigs, markdownPath)
	workflowLog.Printf("Validating push-to-pull-request-branch configuration")
	c.validatePushToPullRequestBranchWarnings(workflowData.SafeOutputs, workflowData.CheckoutConfigs)
	for _, validation := range validations {
		workflowLog.Printf("%s", validation.logMessage)
		if err := validation.validateFn(); err != nil {
			return formatCompilerError(markdownPath, "error", err.Error(), err)
		}
	}
	return nil
}

func validateGitHubCLIProxyVersion(workflowData *WorkflowData) error {
	if !isGitHubCLIModeEnabled(workflowData) {
		return nil
	}
	firewallConfig := getFirewallConfig(workflowData)
	if awfVersionAtLeast(firewallConfig, constants.AWFCliProxyGHListMinVersion) {
		return nil
	}
	effectiveVersion := string(constants.DefaultFirewallVersion)
	if firewallConfig != nil && firewallConfig.Version != "" {
		effectiveVersion = firewallConfig.Version
	}
	return fmt.Errorf(
		"tools.github.mode: gh-proxy requires AWF %s or newer because earlier CLI proxy versions do not support gh issue list or gh pr list; the effective AWF version is %s",
		constants.AWFCliProxyGHListMinVersion,
		effectiveVersion,
	)
}

func (c *Compiler) validateThreatDetectionSandboxRequirement(workflowData *WorkflowData, markdownPath string) error {
	if workflowData.SafeOutputs != nil && workflowData.SafeOutputs.ThreatDetection != nil && isAgentSandboxDisabled(workflowData) {
		return formatCompilerError(markdownPath, "error", "threat detection requires sandbox.agent to be enabled. Threat detection runs inside the agent sandbox (AWF) with fully blocked network. Either enable sandbox.agent or use 'threat-detection: false' to disable the threat-detection configuration in safe-outputs.", errors.New("threat detection requires sandbox.agent"))
	}
	return nil
}

func (c *Compiler) validateConcurrencyConfiguration(workflowData *WorkflowData, markdownPath string) error {
	workflowLog.Printf("Validating workflow-level concurrency configuration")
	if err := validateWorkflowConcurrency(workflowData, markdownPath); err != nil {
		return err
	}
	if workflowData.ConcurrencyJobDiscriminator != "" {
		if err := validateConcurrencyGroupExpression(workflowData.ConcurrencyJobDiscriminator); err != nil {
			return formatCompilerError(markdownPath, "error", "concurrency.job-discriminator validation failed: "+err.Error(), err)
		}
	}
	workflowLog.Printf("Validating engine-level concurrency configuration")
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Concurrency != "" {
		if err := validateConcurrencyQueueConfiguration(workflowData.EngineConfig.Concurrency); err != nil {
			return formatCompilerError(markdownPath, "error", "engine.concurrency validation failed: "+err.Error(), err)
		}
		groupExpr := extractConcurrencyGroupFromYAML(workflowData.EngineConfig.Concurrency)
		if groupExpr != "" {
			if err := validateConcurrencyGroupExpression(groupExpr); err != nil {
				return formatCompilerError(markdownPath, "error", "engine.concurrency validation failed: "+err.Error(), err)
			}
		}
	}
	if workflowData.SafeOutputs != nil && workflowData.SafeOutputs.ConcurrencyGroup != "" {
		if err := validateConcurrencyGroupExpression(workflowData.SafeOutputs.ConcurrencyGroup); err != nil {
			return formatCompilerError(markdownPath, "error", "safe-outputs.concurrency-group validation failed: "+err.Error(), err)
		}
	}
	return nil
}

func validateWorkflowConcurrency(workflowData *WorkflowData, markdownPath string) error {
	if workflowData.Concurrency == "" {
		return nil
	}
	if err := validateConcurrencyQueueConfiguration(workflowData.Concurrency); err != nil {
		return formatCompilerError(markdownPath, "error", "workflow-level concurrency validation failed: "+err.Error(), err)
	}
	if workflowData.CachedConcurrencyGroupExprSet {
		if workflowData.CachedConcurrencyGroupExprErr != nil {
			return formatCompilerError(markdownPath, "error", "workflow-level concurrency validation failed: "+workflowData.CachedConcurrencyGroupExprErr.Error(), workflowData.CachedConcurrencyGroupExprErr)
		}
		return nil
	}
	groupExpr := extractConcurrencyGroupFromYAML(workflowData.Concurrency)
	if groupExpr == "" {
		return nil
	}
	if err := validateConcurrencyGroupExpression(groupExpr); err != nil {
		return formatCompilerError(markdownPath, "error", "workflow-level concurrency validation failed: "+err.Error(), err)
	}
	return nil
}

// emitSandboxRuntimeWarnings warns about sandbox runtime choices that need human
// review or whose configuration the compiler cannot honour.
func (c *Compiler) emitSandboxRuntimeWarnings(workflowData *WorkflowData, markdownPath string) {
	agentConfig := getAgentConfig(workflowData)
	if agentConfig != nil {
		switch agentConfig.Runtime {
		case AgentRuntimeGVisor, AgentRuntimeDockerSbx:
			fmt.Fprintln(os.Stderr, formatCompilerMessage(markdownPath, "warning",
				fmt.Sprintf("sandbox.agent.runtime: %s is deprecated and will be removed in a future release. "+
					"Use sandbox.agent.runtime: docker instead.", agentConfig.Runtime)))
			c.IncrementWarningCount()
		}
	}
	if isCloudHypervisorRuntime(workflowData) {
		fmt.Fprintln(os.Stderr, formatCompilerMessage(markdownPath, "warning",
			"sandbox.agent.runtime: cloud-hypervisor uses a privileged KVM preview path with an attached MCP gateway topology. "+
				"Require a human security review before merge or rollout, and record explicit approval in your change process."))
		c.IncrementWarningCount()
	}
	if declaresIgnoredFilesystemAllowWrite(workflowData) {
		fmt.Fprintln(os.Stderr, formatCompilerMessage(markdownPath, "warning",
			"sandbox.agent.config.filesystem.allowWrite is ignored for this runtime and was not written to the AWF config. "+
				"Only sandbox.agent.runtime: cloud-hypervisor enforces the policy without breaking the agent container: "+
				"the Docker and gVisor runtimes narrow AWF's own writable bind mounts (including its internal /tmp/awf-init mount) "+
				"to read-only, and docker-sbx rejects the policy outright."))
		c.IncrementWarningCount()
	}
}

func (c *Compiler) emitPiThreatDetectionAuthWarning(workflowData *WorkflowData, markdownPath string) {
	if !c.shouldEmitPiThreatDetectionAuthWarning(workflowData) {
		return
	}
	message := `Threat detection for engine: pi runs on the GitHub Copilot CLI. This workflow does not grant permissions.copilot-requests: write, so detection requires a COPILOT_GITHUB_TOKEN secret. Without that secret, threat detection will fail with "No authentication information found".`
	fmt.Fprintln(os.Stderr, formatCompilerMessage(markdownPath, "warning", message))
	c.IncrementWarningCount()
}

func (c *Compiler) shouldEmitPiThreatDetectionAuthWarning(workflowData *WorkflowData) bool {
	if workflowData == nil || hasCopilotRequestsWritePermission(workflowData) ||
		!IsDetectionJobEnabled(workflowData.SafeOutputs) {
		return false
	}

	threatDetection := workflowData.SafeOutputs.ThreatDetection
	if threatDetection.EngineDisabled {
		return false
	}

	configuredEngineID := ResolveEngineID(workflowData)
	var detectionEnv map[string]string
	if threatDetection.EngineConfig != nil {
		if threatDetection.EngineConfig.ID != "" {
			configuredEngineID = threatDetection.EngineConfig.ID
		}
		detectionEnv = threatDetection.EngineConfig.Env
	}
	if configuredEngineID != "pi" || c.getThreatDetectionEngineID(workflowData) != "copilot" {
		return false
	}

	effectiveEnv := mergeThreatDetectionEngineEnv(workflowData, detectionEnv)
	if strings.TrimSpace(effectiveEnv[constants.CopilotGitHubToken]) != "" {
		return false
	}
	return strings.TrimSpace(effectiveEnv[constants.CopilotProviderBaseURL]) == "" &&
		strings.TrimSpace(effectiveEnv[constants.CopilotProviderAPIKey]) == "" &&
		strings.TrimSpace(effectiveEnv[constants.CopilotProviderBearerToken]) == ""
}

// emitCustomEngineThreatDetectionWarning reports when threat detection falls back to a
// built-in engine because the workflow uses a custom engine that the detection analyzer
// cannot run. Without this notice the detection job fails at runtime with a config_error
// while the run stays green (the job is continue-on-error by default).
func (c *Compiler) emitCustomEngineThreatDetectionWarning(workflowData *WorkflowData, markdownPath string) {
	if workflowData == nil || !IsDetectionJobEnabled(workflowData.SafeOutputs) {
		return
	}
	threatDetection := workflowData.SafeOutputs.ThreatDetection
	if threatDetection.EngineDisabled {
		return
	}
	// An explicit safe-outputs.threat-detection.engine is the author's own choice.
	if threatDetection.EngineConfig != nil && threatDetection.EngineConfig.ID != "" {
		return
	}

	configuredEngineID := ResolveEngineID(workflowData)
	if configuredEngineID == "" || configuredEngineID == "pi" || isThreatDetectionCapableEngineID(configuredEngineID) {
		return
	}
	// An engine definition that declares its own detection engine ships a working default.
	if def := c.engineCatalog.Get(configuredEngineID); def != nil && def.DetectionEngine != "" &&
		slices.Contains(declarableDetectionEngineIDs, def.DetectionEngine) {
		return
	}

	detectionEngineID := c.getThreatDetectionEngineID(workflowData)
	message := fmt.Sprintf(
		"Threat detection does not support the custom engine %q, so it runs on the built-in %q engine instead, which requires that engine's credentials. "+
			"Set safe-outputs.threat-detection.engine to a built-in engine (or false to skip AI analysis), or declare detection-engine in the engine definition.",
		configuredEngineID, detectionEngineID)
	fmt.Fprintln(os.Stderr, formatCompilerMessage(markdownPath, "warning", message))
	c.IncrementWarningCount()
}

func (c *Compiler) emitGeneralToolWarnings(workflowData *WorkflowData, markdownPath string) {
	if workflowData.SafeOutputs != nil && hasWorkflowDispatchInputs(workflowData.On) && workflowData.ConcurrencyJobDiscriminator == "" {
		fmt.Fprintln(os.Stderr, formatCompilerMessage(markdownPath, "warning",
			"workflow_dispatch workflow has no concurrency.job-discriminator; the generated conclusion concurrency group is shared by all dispatches of this workflow. "+
				"Set a discriminator (for example, `${{ github.run_id }}`) to give each dispatch its own slot."))
		c.IncrementWarningCount()
	}
	if workflowData.Concurrency != "" && strings.Contains(workflowData.Concurrency, "cancel-in-progress: true") && hasBotSelfCancelRisk(workflowData) {
		fmt.Fprintln(os.Stderr, formatCompilerMessage(markdownPath, "warning",
			"Custom workflow-level concurrency with cancel-in-progress: true may cause self-cancellation.\n"+
				"safe-outputs.github-app can post comments that re-trigger this workflow via issue_comment,\n"+
				"and those passive bot-authored runs can collide with the primary run's concurrency group.\n"+
				"Add `contains(github.actor, '[bot]') && github.run_id ||` at the start of your concurrency\n"+
				"group expression to route bot-triggered runs to a unique key and prevent self-cancellation.\n"+
				"See: https://gh.io/gh-aw/reference/concurrency for details."))
		c.IncrementWarningCount()
	}
	c.emitSandboxRuntimeWarnings(workflowData, markdownPath)
	c.emitPiThreatDetectionAuthWarning(workflowData, markdownPath)
	c.emitCustomEngineThreatDetectionWarning(workflowData, markdownPath)
	c.emitPlaywrightBrowserInstallWarning(workflowData, markdownPath)
	if workflowData.SafeOutputs != nil && workflowData.SafeOutputs.AssignToAgent != nil &&
		workflowData.SafeOutputs.GitHubApp != nil && workflowData.SafeOutputs.AssignToAgent.GitHubToken == "" {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(
			"assign-to-agent does not support GitHub App tokens. "+
				"The Copilot assignment API requires a fine-grained PAT. "+
				"The token fallback chain (GH_AW_AGENT_TOKEN || GH_AW_GITHUB_TOKEN || GITHUB_TOKEN) will be used automatically. "+
				"Add github-token: to your assign-to-agent config to specify a different token."))
		c.IncrementWarningCount()
	}

	c.emitExperimentalFeatureWarnings(workflowData)
	c.emitSamplesCoverageWarnings(workflowData, markdownPath)
	if len(workflowData.Command) > 0 && len(workflowData.Bots) > 0 {
		fmt.Fprintln(os.Stderr, formatCompilerMessage(markdownPath, "warning",
			"Both slash_command and bots triggers are configured. If a bot listed in bots: "+
				"posts a comment that starts with the slash command text (e.g., /command-name), "+
				"it will trigger the workflow and occupy the concurrency slot, potentially "+
				"blocking simultaneous manual invocations. To ensure the workflow only runs on "+
				"explicit user commands, remove the 'bots:' field."))
		c.IncrementWarningCount()
	}
	if workflowData.Redirect != "" {
		fmt.Fprintln(os.Stderr, formatCompilerMessage(markdownPath, "info", "workflow redirect configured: updates move to "+workflowData.Redirect))
	}
}

func hasWorkflowDispatchInputs(onYAML string) bool {
	var parsedData map[string]any
	if err := yaml.Unmarshal([]byte(onYAML), &parsedData); err != nil {
		return false
	}
	onMap, ok := parsedData["on"].(map[string]any)
	if !ok {
		return false
	}
	workflowDispatch, ok := onMap["workflow_dispatch"].(map[string]any)
	if !ok {
		return false
	}
	inputs, ok := workflowDispatch["inputs"].(map[string]any)
	return ok && len(inputs) > 0
}

func (c *Compiler) emitExperimentalFeatureWarnings(workflowData *WorkflowData) {
	c.emitExperimentalFeatureWarningsTo(workflowData, os.Stderr)
}

func (c *Compiler) emitExperimentalFeatureWarningsTo(workflowData *WorkflowData, writer io.Writer) {
	_, detectionConfigured := getFeatureValueFromFrontmatter(string(constants.GHAWDetectionFeatureFlag), workflowData, false)
	if !detectionConfigured {
		detectionConfigured = isFeatureInEnvironment(string(constants.GHAWDetectionFeatureFlag), false)
	}
	warnings := []struct {
		enabled bool
		message string
	}{
		{enabled: workflowData.RateLimit != nil, message: "Using experimental feature: rate limiting"},
		{enabled: workflowData.Graders != nil && workflowData.Graders.HasGraders(), message: "Using experimental feature: graders"},
		{enabled: workflowData.SafeOutputs != nil && workflowData.SafeOutputs.DispatchRepository != nil, message: "Using experimental feature: dispatch-repository"},
		{enabled: workflowData.SafeOutputs != nil && workflowData.SafeOutputs.MergePullRequest != nil, message: "Using experimental feature: merge-pull-request"},
		{enabled: workflowData.SafeOutputs != nil && workflowData.SafeOutputs.ApproveWorkflowRun != nil, message: "Using experimental feature: approve-workflow-run"},
		{enabled: workflowData.SafeOutputs != nil && workflowData.SafeOutputs.ReplaceLabel != nil, message: "Using experimental feature: replace-label"},
		{enabled: workflowData.SafeOutputs != nil && workflowData.SafeOutputs.UploadCodeCoverage != nil, message: "Using experimental feature: upload-code-coverage"},
		{enabled: workflowData.SafeOutputs != nil && workflowData.SafeOutputs.CreateWorkItems != nil, message: "Using experimental feature: ado-create-work-item"},
		{enabled: workflowData.SafeOutputs != nil && workflowData.SafeOutputs.UpdateWorkItems != nil, message: "Using experimental feature: ado-update-work-item"},
		{enabled: workflowData.SafeOutputs != nil && workflowData.SafeOutputs.CommentOnWorkItems != nil, message: "Using experimental feature: ado-comment-on-work-item"},
		{enabled: workflowData.SafeOutputs != nil && workflowData.SafeOutputs.AssignWorkItems != nil, message: "Using experimental feature: ado-assign-work-item"},
		{enabled: workflowData.SafeOutputs != nil && workflowData.SafeOutputs.LinkWorkItems != nil, message: "Using experimental feature: ado-link-work-items"},
		{enabled: workflowData.SafeOutputs != nil && workflowData.SafeOutputs.UploadWorkItemAttachments != nil, message: "Using experimental feature: ado-upload-workitem-attachment"},
		{enabled: hasLinearSafeOutputs(workflowData.SafeOutputs), message: "Using experimental feature: Linear safe outputs"},
		{enabled: detectionConfigured && isFeatureEnabled(constants.GHAWDetectionFeatureFlag, workflowData), message: "Using experimental feature: gh-aw-detection"},
		{enabled: len(workflowData.LSP) > 0, message: "Using experimental feature: lsp"},
		{enabled: len(workflowData.Plugins) > 0, message: "Using experimental feature: plugins"},
		{enabled: workflowData.DriveMemoryConfig != nil && len(workflowData.DriveMemoryConfig.Drives) > 0, message: "Using experimental feature: drive-memory"},
		{enabled: hasContinualExperiment(workflowData.ExperimentConfigs), message: "Using experimental feature: continual experiments"},
		{enabled: workflowData.SafeOutputs != nil && workflowData.SafeOutputs.Steer, message: "Using experimental feature: safe-outputs steer"},
	}
	for _, warning := range warnings {
		if warning.enabled {
			if c.batchMode {
				c.featureUsage[warning.message]++
			} else {
				fmt.Fprintln(writer, console.FormatWarningMessageStderr(warning.message))
			}
			c.IncrementWarningCount()
		}
	}

	if shouldWarnSparseInteractionCells(workflowData) {
		fmt.Fprintln(writer, console.FormatWarningMessageStderr(
			"experiments: potential sparse interaction cells detected (multiple active experiments with weighted traffic). "+
				"Reporting should include factorial K1×K2 cell diagnostics before recommending promotion."))
		c.IncrementWarningCount()
	}
}

func hasContinualExperiment(configs map[string]*ExperimentConfig) bool {
	for _, config := range configs {
		if config != nil && config.Continual != nil {
			return true
		}
	}
	return false
}

func (c *Compiler) validateGitHubToolsAndPermissions(workflowData *WorkflowData, markdownPath string, workflowPermissions *Permissions) error {
	workflowLog.Printf("Validating GitHub tools against enabled toolsets")
	if workflowData.ParsedTools != nil && workflowData.ParsedTools.GitHub != nil {
		allowedTools := workflowData.ParsedTools.GitHub.Allowed.ToStringSlice()
		enabledToolsets := workflowData.CachedParsedToolsets
		if enabledToolsets == nil {
			enabledToolsets = ParseGitHubToolsets(strings.Join(workflowData.ParsedTools.GitHub.Toolset.ToStringSlice(), ","))
		}
		if err := ValidateGitHubToolsAgainstToolsets(allowedTools, enabledToolsets); err != nil {
			return formatCompilerError(markdownPath, "error", err.Error(), err)
		}
		originalToolsets := workflowData.ParsedTools.GitHub.Toolset.ToStringSlice()
		if slices.Contains(originalToolsets, "projects") {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr("The 'projects' toolset requires additional authentication."))
			fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr("See: https://github.github.com/gh-aw/reference/auth-projects/"))
		}
	}
	workflowLog.Printf("Validating permissions for agentic-workflows tool")
	if _, hasAgenticWorkflows := workflowData.Tools["agentic-workflows"]; hasAgenticWorkflows {
		actionsLevel, hasActions := workflowPermissions.Get(PermissionActions)
		if !hasActions || actionsLevel == PermissionNone {
			message := "ERROR: Missing required permission for agentic-workflows tool:\n"
			message += "  - actions: read\n\n"
			message += "The agentic-workflows tool requires actions: read permission to access GitHub Actions data.\n\n"
			message += "Suggested fix: Add the following to your workflow frontmatter:\n"
			message += "permissions:\n"
			message += "  actions: read"
			return formatCompilerError(markdownPath, "error", message, nil)
		}
	}
	return nil
}

func (c *Compiler) validateResourcesAndDispatches(workflowData *WorkflowData, markdownPath string) error {
	workflowLog.Printf("Validating resources field")
	if workflowData.ParsedFrontmatter != nil {
		for _, r := range workflowData.ParsedFrontmatter.Resources {
			if strings.Contains(r, "${{") {
				return formatCompilerError(markdownPath, "error",
					fmt.Sprintf("resources entry %q contains GitHub Actions expression syntax (${{) which is not allowed; use static paths only", r), nil)
			}
		}
	}
	dispatchValidators := []struct {
		logMessage string
		errPrefix  string
		validateFn func(*WorkflowData, string) error
	}{
		{logMessage: "Validating dispatch-workflow configuration", errPrefix: "dispatch-workflow validation failed: ", validateFn: c.validateDispatchWorkflow},
		{logMessage: "Validating dispatch_repository configuration", errPrefix: "dispatch_repository validation failed: ", validateFn: c.validateDispatchRepository},
		{logMessage: "Validating call-workflow configuration", errPrefix: "call-workflow validation failed: ", validateFn: c.validateCallWorkflow},
	}
	for _, validator := range dispatchValidators {
		workflowLog.Print(validator.logMessage)
		if err := validator.validateFn(workflowData, markdownPath); err != nil {
			return formatCompilerError(markdownPath, "error", validator.errPrefix+err.Error(), err)
		}
	}
	return nil
}

// shouldWarnSparseInteractionCells reports whether the compiler should emit a
// sparse-cell interaction warning.
func shouldWarnSparseInteractionCells(workflowData *WorkflowData) bool {
	if workflowData == nil || len(workflowData.Experiments) <= 1 {
		return false
	}
	return hasWeightedTrafficExperiment(workflowData.ExperimentConfigs)
}

// hasWeightedTrafficExperiment returns true when any declared experiment config
// includes a well-formed weight vector (same length as variants, at least one value).
func hasWeightedTrafficExperiment(configs map[string]*ExperimentConfig) bool {
	if len(configs) == 0 {
		return false
	}
	for _, cfg := range configs {
		if cfg == nil || len(cfg.Variants) == 0 {
			continue
		}
		if len(cfg.Weight) == len(cfg.Variants) {
			return true
		}
	}
	return false
}

// validateGCPWIFEngineAuth returns an error when engine.auth declares
// provider=gcp with type=github-oidc but is missing one or more of the three
// required fields (workload-identity-provider, service-account, project).
// Without these fields the WIF exchange cannot succeed and GEMINI_API_KEY will
// also be absent, causing a guaranteed runtime failure that is hard to diagnose.
func validateGCPWIFEngineAuth(workflowData *WorkflowData) error {
	if workflowData == nil || workflowData.EngineConfig == nil || workflowData.EngineConfig.Auth == nil {
		return nil
	}
	auth := workflowData.EngineConfig.Auth
	if auth.Type != "github-oidc" || auth.Provider != "gcp" {
		return nil
	}

	var missing []string
	if auth.GoogleWorkloadIdentityProvider == "" {
		missing = append(missing, "workload-identity-provider")
	}
	if auth.GoogleServiceAccount == "" {
		missing = append(missing, "service-account")
	}
	if auth.GoogleProject == "" {
		missing = append(missing, "project")
	}
	if len(missing) > 0 {
		return fmt.Errorf("engine.auth with provider=gcp requires the following fields: %s", strings.Join(missing, ", "))
	}
	return nil
}

func validateOTLPWorkloadIdentity(workflowData *WorkflowData) error {
	workloadIdentity := getOTLPWorkloadIdentity(workflowData.ParsedFrontmatter, workflowData.RawFrontmatter)
	if workloadIdentity == nil {
		return nil
	}
	if !strings.EqualFold(strings.TrimSpace(workloadIdentity.Provider), "google") {
		return errors.New("observability.otlp.workload-identity.provider must be google. Example:\n\nobservability:\n  otlp:\n    workload-identity:\n      provider: google\n      audience: my-audience")
	}
	if strings.TrimSpace(workloadIdentity.Audience) == "" {
		return errors.New("observability.otlp.workload-identity.audience is required. Example:\n\nobservability:\n  otlp:\n    workload-identity:\n      provider: google\n      audience: my-audience")
	}
	if getOTLPGitHubAppTokenConfig(workflowData.RawFrontmatter) != nil {
		return errors.New("observability.otlp.workload-identity cannot be combined with GitHub App credentials; use one authentication method only. Example:\n\nobservability:\n  otlp:\n    workload-identity:\n      provider: google\n      audience: my-audience")
	}
	return nil
}

// emitSamplesCoverageWarnings warns when samples replay is active but one or
// more enabled safe outputs declare no `samples:` entries. Without samples the
// deterministic replay driver never calls those handlers, so the run silently
// succeeds without performing the configured operation — a failure mode that is
// otherwise only visible by inspecting `GH_AW_SAMPLES` in the lock file.
func (c *Compiler) emitSamplesCoverageWarnings(workflowData *WorkflowData, markdownPath string) {
	if workflowData == nil || !workflowData.UseSamples {
		return
	}
	missing := safeOutputsMissingSamples(workflowData.SafeOutputs)
	if len(missing) == 0 {
		return
	}
	fmt.Fprintln(os.Stderr, formatCompilerMessage(markdownPath, "warning",
		fmt.Sprintf("samples replay is enabled but no samples are configured for: %s. "+
			"These safe outputs will not be exercised — the replay driver replaces the agent, "+
			"so the run succeeds without producing any output for them. "+
			"Add a `samples:` list under each safe output to replay it deterministically.",
			strings.Join(missing, ", "))))
	c.IncrementWarningCount()
}

func parseEnvYAMLSection(envYAML string) map[string]any {
	if strings.TrimSpace(envYAML) == "" {
		return nil
	}
	var raw map[string]any
	if err := yaml.Unmarshal([]byte(envYAML), &raw); err != nil {
		return nil
	}
	if envMap, ok := raw["env"].(map[string]any); ok {
		return envMap
	}
	return raw
}
