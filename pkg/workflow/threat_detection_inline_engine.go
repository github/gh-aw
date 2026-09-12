// Package workflow - inline (non-external) engine execution step for threat detection.
package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
)

// buildDetectionEngineExecutionStep creates the engine execution step for inline threat detection.
// It uses the same agentic engine already installed in the agent job, but runs it through
// sandbox.agent (AWF) with no allowed domains (network fully blocked) and no MCP configured.
//
//nolint:largefunc // Existing inline detection step assembly is intentionally kept together.
func (c *Compiler) buildDetectionEngineExecutionStep(data *WorkflowData) []string {
	// Check if threat detection has engine explicitly disabled
	if data.SafeOutputs != nil && data.SafeOutputs.ThreatDetection != nil {
		if data.SafeOutputs.ThreatDetection.EngineDisabled {
			// Engine explicitly disabled with engine: false
			threatLog.Print("Threat detection engine explicitly disabled via engine: false")
			return []string{
				"      # AI engine disabled for threat detection (engine: false)\n",
			}
		}
	}

	// Determine which engine to use: threat detection engine from frontmatter,
	// otherwise main engine.
	engineSetting := c.getThreatDetectionEngineID(data)

	engineConfig := data.EngineConfig
	hasThreatDetectionEngineConfig := data.SafeOutputs != nil &&
		data.SafeOutputs.ThreatDetection != nil &&
		data.SafeOutputs.ThreatDetection.EngineConfig != nil
	if hasThreatDetectionEngineConfig {
		engineConfig = data.SafeOutputs.ThreatDetection.EngineConfig
	}
	// Preserve the original engine identity before Pi is normalized to Copilot for
	// detection. Precedence matches runtime engine resolution: explicit
	// threat-detection.engine.id overrides the main engine config, which overrides
	// the legacy top-level AI field.
	originalEngineID := data.AI
	if data.EngineConfig != nil && data.EngineConfig.ID != "" {
		originalEngineID = data.EngineConfig.ID
	}
	if hasThreatDetectionEngineConfig && data.SafeOutputs.ThreatDetection.EngineConfig.ID != "" {
		originalEngineID = data.SafeOutputs.ThreatDetection.EngineConfig.ID
	}

	// Get the engine instance
	engine, err := c.getAgenticEngine(engineSetting)
	if err != nil {
		threatLog.Printf("Detection engine %q not found, skipping execution: %v", engineSetting, err)
		return []string{"      # Engine not found, skipping execution\n"}
	}

	// Build a detection engine config inheriting ID, Version, Env, Config, Args, APITarget.
	// MaxTurns, Concurrency, UserAgent, Firewall, Agent, and MaxAICredits are intentionally
	// omitted — MaxAICredits is set independently below from safe-outputs.threat-detection
	// so the detection budget is always resolved from its own default expression rather than
	// silently reusing the main agent budget.
	detectionEngineConfig := engineConfig
	if detectionEngineConfig == nil {
		detectionEngineConfig = &EngineConfig{ID: engineSetting}
	} else {
		detectionEngineConfig = &EngineConfig{
			ID:                       detectionEngineConfig.ID,
			Version:                  detectionEngineConfig.Version,
			Env:                      detectionEngineConfig.Env,
			Config:                   detectionEngineConfig.Config,
			Args:                     detectionEngineConfig.Args,
			APITarget:                detectionEngineConfig.APITarget,
			HarnessScript:            detectionEngineConfig.HarnessScript,
			Driver:                   detectionEngineConfig.Driver,
			HarnessMaxRetries:        detectionEngineConfig.HarnessMaxRetries,
			HarnessInitialDelayMs:    detectionEngineConfig.HarnessInitialDelayMs,
			HarnessBackoffMultiplier: detectionEngineConfig.HarnessBackoffMultiplier,
			HarnessMaxDelayMs:        detectionEngineConfig.HarnessMaxDelayMs,
			HarnessWatchdogTimeoutMs: detectionEngineConfig.HarnessWatchdogTimeoutMs,
		}
	}
	if detectionEngineConfig.ID == "" {
		detectionEngineConfig.ID = engineSetting
	}
	if data.SafeOutputs != nil && data.SafeOutputs.ThreatDetection != nil && data.SafeOutputs.ThreatDetection.MaxAICredits != 0 {
		detectionEngineConfig.MaxAICredits = data.SafeOutputs.ThreatDetection.MaxAICredits
	}
	// Threat detection is a bounded scan of already-completed agent output, not the
	// primary task. If the harness policy was not explicitly configured (via
	// engine.harness or threat-detection.engine.harness), default to zero retries so a
	// failing detection attempt (e.g. a sandboxed cleanup command failing inside the
	// read-only /tmp/gh-aw mount) does not silently retry the whole run up to
	// DEFAULT_MAX_RETRIES times with backoff, burning significant time and model spend
	// re-sending the growing transcript. Explicit user configuration always wins.
	if detectionEngineConfig.HarnessMaxRetries == "" {
		detectionEngineConfig.HarnessMaxRetries = "0"
	}

	resolvedDetectionModel := inheritedDetectionModel(data)
	if data.SafeOutputs != nil && data.SafeOutputs.ThreatDetection != nil && data.SafeOutputs.ThreatDetection.Model != "" {
		resolvedDetectionModel = data.SafeOutputs.ThreatDetection.Model
	}

	// Apply enterprise and engine default detection models when no model was explicitly configured.
	// GetDefaultDetectionModel() returns a cost-effective model optimised for detection
	// (e.g. "gpt-5.1-codex-mini" for Copilot). Other engines return "" (no default).
	// This was accidentally removed in commit a93e36ea4 while fixing engine.agent propagation.
	if resolvedDetectionModel == "" {
		if defaultModel := compilerenv.ResolveDefaultDetectionModel(""); defaultModel != "" {
			resolvedDetectionModel = defaultModel
		} else if defaultModel := engine.GetDefaultDetectionModel(); defaultModel != "" {
			resolvedDetectionModel = defaultModel
		}
	}
	if resolvedDetectionModel == "" {
		resolvedDetectionModel = "detection"
	}

	// Inherit APITarget from the main engine config for GHE/custom endpoints if not already set.
	// This ensures the threat detection AWF invocation receives the same --copilot-api-target
	// and GHE-specific domains in --allow-domains as the main agent AWF invocation.
	if detectionEngineConfig.APITarget == "" && data.EngineConfig != nil && data.EngineConfig.APITarget != "" {
		detectionEngineConfig.APITarget = data.EngineConfig.APITarget
	}
	if engineSetting == "copilot" && originalEngineID == "pi" {
		// Pi requires provider/model syntax (for example "copilot/gpt-5.4"), but the
		// Copilot CLI expects only the model ID. extractPiModelID preserves bare model
		// names unchanged, so empty or already-normalized values keep their current
		// fallback behavior while provider-scoped Pi models become Copilot-compatible.
		resolvedDetectionModel = extractPiModelID(resolvedDetectionModel)
	}

	threatLog.Printf("Resolved inline detection engine %q (original=%q) with model %q", engineSetting, originalEngineID, resolvedDetectionModel)

	// Create minimal WorkflowData for threat detection.
	// SandboxConfig with AWF enabled ensures the engine runs inside the firewall.
	// NetworkPermissions.Allowed preserves only literal user-specified domains when Copilot
	// BYOK is enabled so secret-backed provider URLs can still be paired with an explicit
	// provider hostname in network.allowed without re-opening whole ecosystem allow-lists.
	// No MCP servers are configured for detection.
	// bash: ["*"] allows all shell commands — AWF's network firewall is the primary
	// constraint, so restricting individual bash commands inside the sandbox adds friction
	// without meaningful security benefit.
	// ModelMappings is propagated so the detection awf-config.json includes the alias map
	// (apiProxy.models). Without it, copilot_harness.cjs cannot resolve alias model names
	// (e.g. "small") to concrete ids before spawning the Copilot CLI in the detection job.
	threatDetectionData := buildThreatDetectionWorkflowData(data, engineSetting)
	threatDetectionData.Tools = map[string]any{
		"bash": []any{"*"},
	}
	threatDetectionData.Model = resolvedDetectionModel
	threatDetectionData.EngineConfig = detectionEngineConfig
	threatDetectionData.ModelMappings = data.ModelMappings // propagate alias map so detection awf-config.json can resolve model aliases
	var detectionFirewall *FirewallConfig
	if threatDetectionData.NetworkPermissions != nil {
		detectionFirewall = threatDetectionData.NetworkPermissions.Firewall
	}
	threatDetectionData.NetworkPermissions = &NetworkPermissions{
		Allowed:  getThreatDetectionAdditionalAllowedDomains(data),
		Firewall: detectionFirewall,
	}

	var steps []string

	// Install the engine in the detection job. The detection job runs on a separate fresh
	// runner where the agent's installed tools are not available, so we must install them here.
	installSteps := engine.GetInstallationSteps(threatDetectionData)

	// Ensure node is on PATH when the engine's execution wraps the CLI with a harness
	// script (see engineRequiresNodeHarness). The detection job does not go through
	// DetectRuntimeRequirements, so the setup must be emitted here explicitly. Guard
	// against engines whose install steps already bundle Setup Node.js (Claude/Codex
	// via BuildStandardNpmEngineInstallSteps) — a duplicate would trip
	// JobManager.ValidateDuplicateSteps and hard-fail the compile.
	if engineRequiresNodeHarness(engine) && !installStepsContainNodeSetup(installSteps) {
		threatLog.Print("Injecting Node.js setup step for detection engine harness")
		for _, line := range GenerateNodeJsSetupStep() {
			steps = append(steps, line+"\n")
		}
	}

	for _, step := range installSteps {
		for _, line := range step {
			steps = append(steps, line+"\n")
		}
	}

	// Codex detection runs with no MCP tools, but still needs MCP gateway/config bootstrap
	// so config.toml includes the OpenAI proxy provider used by AWF API proxy mode.
	if engine.GetID() == "codex" {
		var mcpSetup strings.Builder
		if err := c.generateMCPSetup(&mcpSetup, threatDetectionData.Tools, engine, threatDetectionData); err == nil {
			for line := range strings.SplitSeq(mcpSetup.String(), "\n") {
				if line != "" {
					steps = append(steps, line+"\n")
				}
			}
		} else {
			threatLog.Printf("Failed to generate MCP setup for Codex detection; OpenAI proxy configuration may be incomplete: %v", err)
		}
	}

	logFile := constants.ThreatDetectionLogPath
	executionSteps := engine.GetExecutionSteps(threatDetectionData, logFile)
	for _, step := range executionSteps {
		// Determine whether this is the AWF engine execution step so we can inject the
		// GITHUB_STEP_SUMMARY env override. The override is needed because the AWF
		// entrypoint reads $GITHUB_STEP_SUMMARY from the host environment before the
		// chroot sandbox env is applied. Without the override it still points to the real
		// runner file-commands path, which is not accessible inside the chroot and causes
		// "config_error exit=2" (THREAT_DETECTION_STATUS: reason=config_error).
		isAWFExecutionStep := false
		for _, line := range step {
			if strings.Contains(line, "id: agentic_execution") {
				isAWFExecutionStep = true
				step = injectComponentExecutionStarted(step, "detection", detectionExecutionEvidencePath)
				break
			}
		}

		// Pre-scan: determine whether this step already has a step-level env: block.
		// The Copilot engine (and some others) emit env: AFTER run:, so we cannot rely
		// on encountering env: before run: in the forward pass.
		hasStepLevelEnv := false
		if isAWFExecutionStep {
			for _, l := range step {
				if strings.TrimSpace(l) == "env:" {
					hasStepLevelEnv = true
					break
				}
			}
		}

		stepSummaryEnvInjected := false
		skippedAlwaysIf := false
		for i, line := range step {
			// Prefix step IDs with "detection_" to avoid conflicts with agent job steps
			// (e.g., "agentic_execution" is already used by the main engine execution step).
			// Also rewrite any "steps.agentic_execution." expression references (e.g. from a
			// later step in the same engine, like the Codex render-logs step) to the
			// renamed ID so they keep resolving correctly within this job.
			prefixed := strings.Replace(line, "id: agentic_execution", "id: detection_agentic_execution", 1)
			prefixed = strings.ReplaceAll(prefixed, "steps.agentic_execution.", "steps.detection_agentic_execution.")
			// Inject the if condition and continue-on-error after the first line (- name:).
			// continue-on-error: true ensures that infrastructure failures (e.g. unhealthy
			// AWF container, Claude API errors) do not mark the detection job as failed.
			// The "Parse and conclude" step always runs (if: always()) and handles the
			// missing/incomplete detection log as parse_error in warn mode (exit 0).
			if i == 0 {
				steps = append(steps, prefixed+"\n")
				steps = append(steps, fmt.Sprintf("        if: %s\n", detectionStepCondition))
				steps = append(steps, "        continue-on-error: true\n")
				continue
			}
			// If the step already had an "if: always()" field right after its "- name:" line
			// (e.g. behavior-defined engine log-parser write steps emit "if: always()" to
			// ensure file materialization), skip it to avoid a duplicate YAML mapping key.
			// The detection condition injected above supersedes it. We check specifically
			// for "if: always()" to avoid silently dropping a legitimate custom condition
			// from unrelated steps. Uses a flag rather than a hard-coded index so it still
			// works if a step ever has fields interleaved before "if:".
			if i == 1 && strings.HasPrefix(strings.TrimSpace(step[0]), "- name:") && strings.TrimSpace(line) == "if: always()" {
				skippedAlwaysIf = true
				continue
			}
			// A step whose own "if: always()" was just skipped (above) may also define its
			// own "continue-on-error: true" immediately after (e.g. our render-logs step).
			// Skip it too so it isn't duplicated alongside the continue-on-error injected at
			// i == 0 above, which would otherwise produce a duplicate YAML mapping key.
			if skippedAlwaysIf && strings.TrimSpace(line) == "continue-on-error: true" {
				skippedAlwaysIf = false
				continue
			}
			// For the AWF execution step, inject/replace GITHUB_STEP_SUMMARY in the
			// step-level env to the detection-specific writable path.
			// This overrides the value at the GitHub Actions step level so the AWF
			// entrypoint (and the chroot) inherits the writable path instead of the
			// real runner file-commands path (which is not accessible inside the chroot
			// and causes "config_error exit=2").
			//
			// Two cases determined by the pre-scan above:
			//   a) The step already has an env: block (hasStepLevelEnv == true):
			//      emit the env: key then inject our entry as the first item; skip any
			//      subsequent GITHUB_STEP_SUMMARY entry to avoid a duplicate key.
			//   b) No env: block exists: inject a new env: block immediately before run:.
			if isAWFExecutionStep && !stepSummaryEnvInjected {
				trimmed := strings.TrimSpace(line)
				if hasStepLevelEnv && trimmed == "env:" {
					// Case (a): merge into existing env block.
					steps = append(steps, prefixed+"\n")
					steps = append(steps, fmt.Sprintf("          GITHUB_STEP_SUMMARY: %s\n", constants.ThreatDetectionStepSummaryPath))
					stepSummaryEnvInjected = true
					continue
				}
				if !hasStepLevelEnv && strings.HasPrefix(trimmed, "run:") {
					// Case (b): no env block — inject one before run:.
					steps = append(steps, "        env:\n")
					steps = append(steps, fmt.Sprintf("          GITHUB_STEP_SUMMARY: %s\n", constants.ThreatDetectionStepSummaryPath))
					stepSummaryEnvInjected = true
				}
			}
			// After injecting our GITHUB_STEP_SUMMARY, skip any GITHUB_STEP_SUMMARY
			// entry that the engine may have already placed in its env block so we
			// don't produce a duplicate mapping key.
			if isAWFExecutionStep && stepSummaryEnvInjected &&
				strings.HasPrefix(strings.TrimSpace(line), "GITHUB_STEP_SUMMARY:") {
				continue
			}
			steps = append(steps, prefixed+"\n")
		}
	}

	return steps
}
