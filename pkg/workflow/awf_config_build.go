// This file builds the AWF configuration file JSON from workflow data.
// See awf_config.go for the config file types, awf_config_schema.go for schema
// validation, and awf_config_policy.go for model policy and domain resolution.

package workflow

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/jsonutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
)

var awfConfigLog = logger.New("workflow:awf_config")

// BuildAWFConfigJSON generates a compact JSON config file for AWF from the provided
// command configuration. The JSON is single-line (no indentation) for safe embedding
// in a shell printf command.
//
// The caller is responsible for writing the returned JSON to disk at the path expected
// by the AWF --config flag. See BuildAWFCommand for how this is wired together.
func BuildAWFConfigJSON(config AWFCommandConfig) (string, error) { //nolint:largefunc // Assembles the full AWF config by section.
	awfConfigLog.Printf("Building AWF config JSON: engine=%s, allowed_domains=%q", config.EngineName, config.AllowedDomains)

	// Resolve firewall config once — used for both the schema URL and the container image tag.
	firewallConfig := getFirewallConfig(config.WorkflowData)

	awfConfig := AWFConfigFile{
		Schema: buildAWFConfigSchemaURL(firewallConfig),
	}
	if config.WorkflowData != nil {
		awfConfig.Enclaves = buildAWFEnclavesConfig(config.WorkflowData.Enclaves)
	}

	// ── Runner section ──────────────────────────────────────────────────────
	if topology := getRunnerTopology(config.WorkflowData); topology != "" {
		awfConfig.Runner = &AWFRunnerConfig{Topology: string(topology)}
		awfConfigLog.Printf("Runner section: topology=%s", topology)
	}

	// ── Network section ──────────────────────────────────────────────────────
	if config.AllowedDomains != "" {
		allowList := splitDomainList(config.AllowedDomains)
		awfConfig.Network = &AWFNetworkConfig{
			AllowDomains: allowList,
		}
		awfConfigLog.Printf("Network section: %d allowed domains", len(allowList))

		// Blocked domains (if configured in the workflow)
		if config.WorkflowData != nil {
			blockedDomainsStr := formatBlockedDomains(config.WorkflowData.NetworkPermissions)
			if blockedDomainsStr != "" {
				blockList := splitDomainList(blockedDomainsStr)
				awfConfig.Network.BlockDomains = blockList
				awfConfigLog.Printf("Network section: %d blocked domains", len(blockList))
			}
		}
	}

	if isAWFNetworkIsolationEnabled(config.WorkflowData) {
		if awfConfig.Network == nil {
			awfConfig.Network = &AWFNetworkConfig{}
		}
		awfConfig.Network.Isolation = true
		awfConfig.Network.TopologyAttach = buildAWFTopologyAttachList(config.WorkflowData)
		awfConfigLog.Printf("Network section: isolation enabled with %d topology attachments", len(awfConfig.Network.TopologyAttach))
	}

	// Docker sbx microVMs resolve host services via
	// host.docker.internal
	// (the Docker bridge gateway, 172.17.0.1). Allow this domain so AWF's network
	// policy permits connections from the microVM to the api-proxy, MCP gateway, and
	// Squid proxy that are all published on the host bridge.
	if isDockerSbxRuntime(config.WorkflowData) {
		if awfConfig.Network == nil {
			awfConfig.Network = &AWFNetworkConfig{}
		}
		const hostDockerInternal = "host.docker.internal"
		if !slices.Contains(awfConfig.Network.AllowDomains, hostDockerInternal) {
			awfConfig.Network.AllowDomains = append(awfConfig.Network.AllowDomains, hostDockerInternal)
			awfConfigLog.Printf("Network section: added %s for microVM runtime routing", hostDockerInternal)
		}
		if awfSupportsVerifySbxEgress(firewallConfig) {
			awfConfig.Network.VerifySbxEgress = true
		} else {
			awfConfigLog.Printf("Skipping network.verifySbxEgress: AWF version %q requires at least %s", getAWFImageTag(firewallConfig), constants.AWFVerifySbxEgressMinVersion)
		}
	}

	// ── Filesystem section ───────────────────────────────────────────────────
	if config.WorkflowData != nil &&
		config.WorkflowData.SandboxConfig != nil &&
		config.WorkflowData.SandboxConfig.Agent != nil &&
		config.WorkflowData.SandboxConfig.Agent.Config != nil &&
		config.WorkflowData.SandboxConfig.Agent.Config.Filesystem != nil &&
		config.WorkflowData.SandboxConfig.Agent.Config.Filesystem.AllowWrite != nil {
		allowWrite := config.WorkflowData.SandboxConfig.Agent.Config.Filesystem.AllowWrite
		if awfEmitsFilesystemAllowWrite(config.WorkflowData, firewallConfig) {
			awfConfig.Filesystem = &AWFFilesystemConfig{AllowWrite: allowWrite}
			awfConfigLog.Printf("Filesystem section: %d writable path(s)", len(allowWrite))
		} else if isCloudHypervisorRuntime(config.WorkflowData) {
			awfConfigLog.Printf("Skipping filesystem.allowWrite: AWF version %q requires at least %s for the cloud-hypervisor runtime",
				getAWFImageTag(firewallConfig), constants.AWFCloudHypervisorFilesystemAllowWriteMinVersion)
		} else {
			awfConfigLog.Print("Skipping filesystem.allowWrite: only the cloud-hypervisor runtime enforces it without breaking the agent container")
		}
	}

	if platformType := extractPlatformType(config.WorkflowData); platformType != "" {
		awfConfig.Platform = &AWFPlatformConfig{Type: platformType}
		awfConfigLog.Printf("Platform section: type=%s", platformType)
	}

	// ── API proxy section ─────────────────────────────────────────────────────
	// maxAICredits is taken from frontmatter/imports only; when unset (0) the
	// runtime value is resolved from vars.GH_AW_DEFAULT_MAX_AI_CREDITS via a
	// GitHub Actions expression injected directly into the JSON string in
	// BuildAWFCommand (see injectMaxAICreditsExpression in awf_helpers.go).
	maxAICredits := int64(0)
	maxRuns := constants.DefaultMaxRuns
	// GetMaxTurnCacheMisses handles nil receiver and env-var fallback, so pre-init
	// via the nil receiver avoids a redundant os.Getenv when EngineConfig is set.
	maxTurnCacheMisses := (*EngineConfig)(nil).GetMaxTurnCacheMisses()
	if config.WorkflowData != nil && config.WorkflowData.EngineConfig != nil {
		if config.WorkflowData.EngineConfig.MaxAICredits != 0 {
			maxAICredits = config.WorkflowData.EngineConfig.MaxAICredits
		}
		maxRuns = config.WorkflowData.EngineConfig.GetMaxRuns()
		maxTurnCacheMisses = config.WorkflowData.EngineConfig.GetMaxTurnCacheMisses()
	}

	// Token steering is enabled by default. Setting max-ai-credits to a negative
	// value (-1) omits that budget from the AWF config and disables token steering.
	// When maxAICredits is 0 (runtime default), token steering stays enabled here.
	enableTokenSteering := maxAICredits >= 0
	if config.WorkflowData != nil && config.WorkflowData.SandboxConfig != nil && config.WorkflowData.SandboxConfig.Agent != nil && config.WorkflowData.SandboxConfig.Agent.TokenSteering != nil {
		enableTokenSteering = *config.WorkflowData.SandboxConfig.Agent.TokenSteering
	}
	if maxAICredits < 0 {
		// Negative signals "disabled" — omit the budget from the AWF config.
		maxAICredits = 0
	}
	var tokenSteeringEnabled *bool
	if awfSupportsTokenSteering(firewallConfig) && (enableTokenSteering || (config.WorkflowData != nil && config.WorkflowData.SandboxConfig != nil && config.WorkflowData.SandboxConfig.Agent != nil && config.WorkflowData.SandboxConfig.Agent.TokenSteering != nil)) {
		tokenSteeringEnabled = &enableTokenSteering
	}

	apiProxy := &AWFAPIProxyConfig{
		Enabled:             true,
		MaxRuns:             maxRuns,
		MaxTurnCacheMisses:  maxTurnCacheMisses,
		MaxAICredits:        maxAICredits,
		EnableTokenSteering: tokenSteeringEnabled,
	}

	if !enableTokenSteering {
		awfConfigLog.Print("Disabling apiProxy.enableTokenSteering")
	} else if !awfSupportsTokenSteering(firewallConfig) {
		awfConfigLog.Printf("Skipping apiProxy.enableTokenSteering: AWF version %q requires at least %s", getAWFImageTag(firewallConfig), constants.AWFTokenSteeringMinVersion)
	}

	if mf := extractModelFallback(config.WorkflowData); mf != nil {
		apiProxy.ModelFallback = mf
		enabledDisplay := "<unset>"
		if mf.Enabled != nil {
			enabledDisplay = mf.Enabled.String()
		}
		awfConfigLog.Printf("API proxy: modelFallback configured: enabled=%s", enabledDisplay)
	} else if hasCustomLLMAPITarget(config.WorkflowData) {
		// Custom OpenAI/Anthropic-compatible providers (e.g. OpenRouter, internal LLM
		// routers, Azure OpenAI) expose model identifiers that are absent from the
		// built-in AWF model catalog. Letting AWF rewrite the requested model then
		// yields HTTP 404 model_not_found upstream, so pass the configured model
		// through verbatim unless the workflow explicitly opts back in.
		disabled := TemplatableBool("false")
		apiProxy.ModelFallback = &AWFModelFallbackConfig{Enabled: &disabled}
		awfConfigLog.Print("API proxy: modelFallback disabled by default: custom LLM API target configured")
	}

	if pricing := extractDefaultAiCreditsPricing(config.WorkflowData); pricing != nil {
		apiProxy.DefaultAiCreditsPricing = pricing
		awfConfigLog.Printf("API proxy: defaultAiCreditsPricing configured: input=%g, output=%g", pricing.Input, pricing.Output)
	}

	targets := map[string]*AWFAPITargetConfig{}

	if openaiTarget := extractAPIProxyTargetHost(config.WorkflowData, "OPENAI_BASE_URL", firewallConfig); openaiTarget != "" {
		targets["openai"] = &AWFAPITargetConfig{Host: openaiTarget}
		awfConfigLog.Printf("API proxy: custom openai target=%s", openaiTarget)
	}
	if anthropicTarget := extractAPIProxyTargetHost(config.WorkflowData, "ANTHROPIC_BASE_URL", firewallConfig); anthropicTarget != "" {
		targets["anthropic"] = &AWFAPITargetConfig{Host: anthropicTarget}
		awfConfigLog.Printf("API proxy: custom anthropic target=%s", anthropicTarget)
	}

	// Apply authHeader overrides from sandbox.agent.targets frontmatter.
	// These are independent of the host/env-var settings: authHeader can be set
	// even when no custom host is configured.
	for _, provider := range []string{"openai", "anthropic"} {
		authHeader := extractAPITargetAuthHeader(config.WorkflowData, provider)
		if authHeader == "" {
			continue
		}
		if existing, ok := targets[provider]; ok {
			existing.AuthHeader = authHeader
		} else {
			targets[provider] = &AWFAPITargetConfig{AuthHeader: authHeader}
		}
		awfConfigLog.Printf("API proxy: custom %s authHeader=%s", provider, authHeader)
	}
	if copilotTarget := GetCopilotAPITarget(config.WorkflowData); copilotTarget != "" {
		targets["copilot"] = &AWFAPITargetConfig{Host: copilotTarget}
		awfConfigLog.Printf("API proxy: custom copilot target=%s", copilotTarget)
	}

	// Apply BYOK supplemental fields from sandbox.agent.targets.copilot frontmatter.
	// extraHeaders, extraBodyFields, and sessionId are Copilot-specific and map to
	// AWF_BYOK_EXTRA_HEADERS, AWF_BYOK_EXTRA_BODY_FIELDS, and AWF_PROVIDER_SESSION_ID.
	if copilotFrontmatter := extractCopilotTargetConfig(config.WorkflowData); copilotFrontmatter != nil {
		existing, ok := targets["copilot"]
		if !ok {
			existing = &AWFAPITargetConfig{}
			targets["copilot"] = existing
		}
		if copilotFrontmatter.AuthHeader != "" {
			existing.AuthHeader = copilotFrontmatter.AuthHeader
			awfConfigLog.Printf("API proxy: copilot authHeader=%s", copilotFrontmatter.AuthHeader)
		}
		if len(copilotFrontmatter.ExtraHeaders) > 0 {
			existing.ExtraHeaders = copilotFrontmatter.ExtraHeaders
			awfConfigLog.Printf("API proxy: copilot extraHeaders configured (%d header(s))", len(copilotFrontmatter.ExtraHeaders))
		}
		if len(copilotFrontmatter.ExtraBodyFields) > 0 {
			existing.ExtraBodyFields = copilotFrontmatter.ExtraBodyFields
			awfConfigLog.Printf("API proxy: copilot extraBodyFields configured (%d field(s))", len(copilotFrontmatter.ExtraBodyFields))
		}
		if copilotFrontmatter.SessionId != "" {
			existing.SessionId = copilotFrontmatter.SessionId
			awfConfigLog.Printf("API proxy: copilot sessionId configured")
		}
	}
	geminiTarget := extractAPIProxyTargetHost(config.WorkflowData, "GEMINI_API_BASE_URL", firewallConfig)
	if geminiTarget == "" {
		geminiTarget = GetGeminiAPITarget(config.WorkflowData, config.EngineName)
	}
	if geminiTarget != "" {
		awfConfigLog.Printf("API proxy: custom gemini target=%s", geminiTarget)
		targets["gemini"] = &AWFAPITargetConfig{Host: geminiTarget}
	}

	if len(targets) > 0 {
		apiProxy.Targets = targets
		awfConfigLog.Printf("API proxy: %d custom targets configured", len(targets))
	}

	if providers := extractModelCostProviders(config.WorkflowData); len(providers) > 0 {
		if awfSupportsAPIProxyProviders(firewallConfig) {
			apiProxy.Providers = providers
			awfConfigLog.Printf("API proxy: %d model-cost provider override(s) configured", len(providers))
		} else {
			awfConfigLog.Printf("Skipping apiProxy.providers: AWF version %q requires at least %s", getAWFImageTag(firewallConfig), constants.AWFAPIProxyProvidersMinVersion)
		}
	}

	// ── Models section (nested under apiProxy per AWF config schema) ──────────
	if config.WorkflowData != nil && len(config.WorkflowData.ModelMappings) > 0 {
		apiProxy.Models = config.WorkflowData.ModelMappings
		awfConfigLog.Printf("Models section: %d alias entries", len(config.WorkflowData.ModelMappings))
	}
	allowedModels, disallowedModels := resolveModelPolicyForAWFConfig(config.WorkflowData)
	if len(allowedModels) > 0 {
		apiProxy.AllowedModels = allowedModels
		awfConfigLog.Printf("Models policy: %d allowed model pattern(s)", len(allowedModels))
	}
	if len(disallowedModels) > 0 {
		apiProxy.DisallowedModels = disallowedModels
		awfConfigLog.Printf("Models policy: %d disallowed model pattern(s)", len(disallowedModels))
	}

	if caCert := extractAPIProxyCACert(config.WorkflowData); caCert != "" {
		if awfSupportsAPIProxyCACert(firewallConfig) {
			apiProxy.CACert = caCert
			awfConfigLog.Printf("API proxy: caCert configured")
		} else {
			awfConfigLog.Printf("Skipping apiProxy.caCert: AWF version %q requires at least %s", getAWFImageTag(firewallConfig), constants.AWFAPIProxyCACertMinVersion)
		}
	}

	awfConfig.APIProxy = apiProxy

	// ── Container section ─────────────────────────────────────────────────────
	awfImageTag := buildAWFImageTagWithDigests(getAWFImageTag(firewallConfig), config.WorkflowData)
	// A custom image manifest (sandbox.agent.images) is a closed set of digest-pinned
	// references. AWF rejects it alongside imageTag, which would select a different
	// effective image, so the compiler-owned tag is suppressed when it is configured.
	containerImages := getSandboxAgentImages(config.WorkflowData)
	if len(containerImages) > 0 {
		awfImageTag = ""
	}
	agentRuntime := getAgentContainerRuntime(config.WorkflowData)
	agentTimeout := 0
	if isDockerSbxRuntime(config.WorkflowData) || isCloudHypervisorRuntime(config.WorkflowData) {
		agentTimeout = resolveAWFContainerAgentTimeoutMinutes(config.WorkflowData)
	}
	// containerRuntime is only emitted when the effective AWF version supports it.
	// Gate here to avoid sending an unrecognised field to older AWF binaries.
	if !awfSupportsContainerRuntime(firewallConfig) {
		if agentRuntime != "" {
			awfConfigLog.Printf("Skipping containerRuntime: AWF version %q requires at least %s (gh-aw-firewall#6093)", getAWFImageTag(firewallConfig), constants.AWFContainerRuntimeMinVersion)
		}
		agentRuntime = ""
	}
	if awfImageTag != "" || isArcDindTopology(config.WorkflowData) || agentRuntime != "" || agentTimeout > 0 || len(containerImages) > 0 {
		container := &AWFContainerConfig{
			ImageTag:         awfImageTag,
			AgentTimeout:     agentTimeout,
			ContainerRuntime: agentRuntime,
			Images:           containerImages,
		}
		// NOTE: dockerHostPathPrefix is intentionally NOT set for arc-dind topology.
		// With sysroot-stage active, the Docker daemon can access all needed paths:
		//  - Workspace & RUNNER_TEMP: on the shared work volume (/home/runner/_work/)
		//  - System binaries: provided by the sysroot named volume (not bind mounts)
		//  - Kernel VFS (/dev, /sys): daemon's own kernel
		// Setting a prefix would incorrectly translate the workspace mount source to
		// a non-existent path (e.g. /prefix/home/runner/_work/repo → empty dir),
		// causing the agent to see an empty workspace. See gh-aw#34896.
		awfConfig.Container = container
		if awfImageTag != "" {
			awfConfigLog.Printf("Container section: image_tag=%s", awfImageTag)
		}
		if agentRuntime != "" {
			awfConfigLog.Printf("Container section: containerRuntime=%s", agentRuntime)
		}
		if agentTimeout > 0 {
			awfConfigLog.Printf("Container section: agentTimeout=%d", agentTimeout)
		}
		if len(containerImages) > 0 {
			awfConfigLog.Printf("Container section: custom image manifest with %d role(s)", len(containerImages))
		}
	}

	if isCloudHypervisorRuntime(config.WorkflowData) {
		awfConfig.CloudHypervisor = buildAWFCloudHypervisorConfig()
	}

	// ── Logging section ──────────────────────────────────────────────────────
	// Logging paths are set in config. For ARC/DinD, the config file is written at runtime,
	// so ${RUNNER_TEMP} can be preserved for shell expansion before AWF reads the JSON.
	awfConfig.Logging = &AWFLoggingConfig{
		ProxyLogsDir: string(constants.AWFProxyLogsDir),
		AuditDir:     string(constants.AWFAuditDir),
	}

	if isArcDindTopology(config.WorkflowData) {
		awfConfig.Logging.ProxyLogsDir = awfArcDindProxyLogsDirExpr
		awfConfig.Logging.AuditDir = awfArcDindAuditDirExpr
	}
	awfConfigLog.Printf("Logging section: proxyLogsDir=%s, auditDir=%s", awfConfig.Logging.ProxyLogsDir, awfConfig.Logging.AuditDir)

	jsonStr, err := jsonutil.MarshalCompactNoHTMLEscape(awfConfig)
	if err != nil {
		return "", fmt.Errorf("invalid AWF config values: expected generated output to be JSON-serializable; encountered serialization error: %w. This indicates a compiler bug; please report it", err)
	}

	awfConfigLog.Printf("AWF config JSON generated: %d bytes", len(jsonStr))

	if config.WorkflowData != nil && config.WorkflowData.ValidateAWFConfig {
		if err := validateAWFConfigJSON(jsonStr); err != nil {
			return "", fmt.Errorf("invalid generated AWF config: expected awf-config JSON to satisfy the embedded schema; review the referenced field path and fix that workflow/frontmatter value: %w", err)
		}
	}

	return jsonStr, nil
}

func buildAWFCloudHypervisorConfig() *AWFCloudHypervisorConfig {
	return &AWFCloudHypervisorConfig{
		PreviewEnabled: true,
		MountPolicy:    "workspace-and-tool-cache",
		VCPUCount:      constants.DefaultCloudHypervisorVCPUs,
		MemoryMiB:      constants.DefaultCloudHypervisorMemoryMiB,
	}
}

func resolveAWFContainerAgentTimeoutMinutes(workflowData *WorkflowData) int {
	// Reuse the workflow-level default timeout so docker-sbx inherits the same
	// runtime ceiling when top-level timeout-minutes is omitted or non-numeric.
	defaultTimeout := compilerenv.ResolveDefaultTimeoutMinutes(int(constants.DefaultAgenticWorkflowTimeout / time.Minute))
	if workflowData == nil || workflowData.TimeoutMinutes == "" {
		return defaultTimeout
	}

	rawTimeout := strings.TrimSpace(workflowData.TimeoutMinutes)
	if after, ok := strings.CutPrefix(rawTimeout, "timeout-minutes:"); ok {
		rawTimeout = strings.TrimSpace(after)
	}

	timeoutMinutes, err := strconv.Atoi(rawTimeout)
	if err == nil && timeoutMinutes > 0 {
		return timeoutMinutes
	}

	if rawTimeout != "" {
		// agentTimeout is integer-only, so an expression-backed timeout (e.g. the
		// vars.GH_AW_DEFAULT_TIMEOUT_MINUTES default) cannot be emitted here.
		// Omitting it keeps the sandbox bounded by the step/job timeout instead of
		// terminating the agent earlier than the runtime value requests.
		awfConfigLog.Printf("Container section: non-numeric timeout-minutes %q (e.g. a GitHub Actions expression) cannot be emitted in integer-only agentTimeout; omitting agentTimeout so the step timeout governs", rawTimeout)
		return 0
	}
	return defaultTimeout
}

// buildAWFTopologyAttachList returns container names that AWF should attach to
// the internal awf-net network when network isolation mode is enabled.
// The list always includes the MCP gateway and conditionally includes the
// host-started CLI proxy sidecar when gh-proxy mode is active. Cloud Hypervisor
// omits the CLI proxy until its control peer supports the proxy's TCP port.
func buildAWFTopologyAttachList(workflowData *WorkflowData) []string {
	targets := []string{"awmg-mcpg"}
	if !isCloudHypervisorRuntime(workflowData) && isCliProxyNeeded(workflowData) {
		targets = append(targets, "awmg-cli-proxy")
	}
	return targets
}

// extractPlatformType returns sandbox.agent.platform only for enabled AWF sandbox
// agents, or an empty string to let AWF fall back to its default platform logic.
func extractPlatformType(workflowData *WorkflowData) string {
	if workflowData == nil || workflowData.SandboxConfig == nil || workflowData.SandboxConfig.Agent == nil {
		return ""
	}
	if workflowData.SandboxConfig.Agent.Disabled {
		return ""
	}
	if !isSupportedSandboxType(getAgentType(workflowData.SandboxConfig.Agent)) {
		return ""
	}
	return workflowData.SandboxConfig.Agent.Platform
}

// extractAPIProxyCACert returns sandbox.agent.ca-cert, the host path to an
// additional CA certificate for api-proxy upstream TLS verification, or an
// empty string when unset.
func extractAPIProxyCACert(workflowData *WorkflowData) string {
	if workflowData == nil || workflowData.SandboxConfig == nil || workflowData.SandboxConfig.Agent == nil {
		return ""
	}
	return workflowData.SandboxConfig.Agent.CACert
}

// extractModelFallback returns an AWFModelFallbackConfig if the workflow has configured
// sandbox.agent.model-fallback, or nil if the field is absent (letting AWF use its default).
func extractModelFallback(workflowData *WorkflowData) *AWFModelFallbackConfig {
	if workflowData == nil {
		return nil
	}
	if workflowData.SandboxConfig == nil {
		return nil
	}
	if workflowData.SandboxConfig.Agent == nil {
		return nil
	}
	mf := workflowData.SandboxConfig.Agent.ModelFallback
	if mf == nil {
		return nil
	}
	return &AWFModelFallbackConfig{
		Enabled: mf,
	}
}

// hasCustomLLMAPITarget reports whether the workflow routes the agentic engine to a
// custom OpenAI-compatible or Anthropic-compatible provider through an engine.env base
// URL (OPENAI_BASE_URL / ANTHROPIC_BASE_URL). Such providers (OpenRouter, internal LLM
// routers, Azure OpenAI deployments) use model identifiers that are not present in the
// AWF built-in model catalog.
func hasCustomLLMAPITarget(workflowData *WorkflowData) bool {
	for _, envVar := range []string{"OPENAI_BASE_URL", "ANTHROPIC_BASE_URL"} {
		if engineEnvHasNonEmptyValue(workflowData, envVar) {
			return true
		}
	}
	return false
}

// extractDefaultAiCreditsPricing returns an AiCreditsPricingConfig if the workflow has
// configured models.default-ai-credits-pricing, or nil if the field is absent.
// This fallback pricing is used when maxAiCredits is active and the requested model is not in
// the built-in pricing table, preventing HTTP 400 unknown_model_ai_credits for BYOK/self-hosted models.
func extractDefaultAiCreditsPricing(workflowData *WorkflowData) *AiCreditsPricingConfig {
	if workflowData == nil {
		return nil
	}
	p := workflowData.DefaultAiCreditsPricing
	if p == nil {
		return nil
	}
	return &AiCreditsPricingConfig{
		Input:       p.Input,
		Output:      p.Output,
		CachedInput: p.CachedInput,
		CacheWrite:  p.CacheWrite,
	}
}

func extractModelCostProviders(workflowData *WorkflowData) map[string]any {
	if workflowData == nil || len(workflowData.ModelCosts) == 0 {
		return nil
	}
	providers, ok := workflowData.ModelCosts["providers"].(map[string]any)
	if !ok {
		awfConfigLog.Printf("API proxy: models.providers has unexpected type %T; skipping provider overlay", workflowData.ModelCosts["providers"])
		return nil
	}
	if len(providers) == 0 {
		return nil
	}
	clone := make(map[string]any, len(providers))
	maps.Copy(clone, providers)
	return clone
}

// getRunnerTopology extracts the runner topology from WorkflowData.
// Returns an empty string when no topology is configured.
func getRunnerTopology(workflowData *WorkflowData) RunnerTopology {
	if workflowData == nil || workflowData.RunnerConfig == nil {
		return ""
	}
	return workflowData.RunnerConfig.Topology
}

// isArcDindTopology returns true when the workflow targets ARC/DinD runners.
func isArcDindTopology(workflowData *WorkflowData) bool {
	return getRunnerTopology(workflowData) == RunnerTopologyArcDind
}
