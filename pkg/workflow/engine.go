package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/typeutil"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
)

var engineLog = logger.New("workflow:engine")

const WorkflowCallNetworkAllowedEnvVar = "GH_AW_WORKFLOW_CALL_NETWORK_ALLOWED"

func injectWorkflowCallNetworkAllowedEnv(env map[string]string, workflowData *WorkflowData) {
	if shouldUseWorkflowCallNetworkAllowedInput(workflowData) {
		env[WorkflowCallNetworkAllowedEnvVar] = fmt.Sprintf("${{ inputs.%s }}", NetworkAllowedInputName)
	}
}

func toEngineEnvValueString(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		return v, true
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32), true
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%v", v), true
	default:
		return "", false
	}
}

// EngineConfig represents the parsed engine configuration
type EngineConfig struct {
	ID                 string
	Version            string
	LLMProvider        LLMProvider // Inference provider override for this engine (engine.provider / engine.model-provider)
	PermissionMode     string
	MaxTurns           string
	MaxToolDenials     string // Maximum repeated tool denials before stopping inference (copilot SDK mode only)
	MaxRuns            int    // Maximum number of LLM invocations per run (AWF apiProxy.maxRuns)
	MaxTurnCacheMisses int    // Maximum number of consecutive cache misses per run (AWF apiProxy.maxCacheMisses)
	MaxContinuations   int    // Maximum number of continuations for autopilot mode (copilot engine only; > 1 enables --autopilot)
	MaxAICredits       int64  // Maximum allowed AI credits per run for AWF apiProxy firewall enforcement
	Concurrency        string // Agent job-level concurrency configuration (YAML format)
	UserAgent          string
	Command            string // Custom executable path (when set, skip installation steps)
	HarnessScript      string // Custom Node.js harness script filename (replaces engine default harness script when supported)
	Driver             string // Custom driver script filename or command. For the copilot engine (engine.driver), supports .js/.cjs/.mjs (Node.js), .py (Python), .ts/.mts (TypeScript), .rb (Ruby), or a bare command name. For the pi engine (engine.driver), supports .js/.cjs/.mjs or a bare basename resolved from the setup-action directory.
	InlineDriver       *InlineEngineDriver
	Env                map[string]string
	Auth               *EngineAuthConfig // Engine-level auth config (mapped to AWF_AUTH_* env vars for API proxy sidecar auth)
	Config             string
	Args               []string
	Agent              string // Agent identifier for copilot --agent flag (copilot engine only)
	APITarget          string // Custom API endpoint hostname (e.g., "api.acme.ghe.com" or "api.enterprise.githubcopilot.com")
	Bare               bool   // When true, disables automatic loading of context/instructions (copilot: --no-custom-instructions, claude: --bare, codex: --no-system-prompt, gemini: GEMINI_SYSTEM_MD=/dev/null)
	// Inline definition fields (populated when engine.runtime is specified in frontmatter)
	IsInlineDefinition bool   // true when the engine is defined inline via engine.runtime + optional engine.provider
	InlineProviderID   string // engine.provider.id  (e.g. "openai", "anthropic")

	// Extended inline auth fields (engine.provider.auth.* beyond the simple secret)
	InlineProviderAuth *AuthDefinition // full auth definition parsed from engine.provider.auth

	// Extended inline request shaping fields (engine.provider.request.*)
	InlineProviderRequest *RequestShape // request shaping parsed from engine.provider.request

	// MCP gateway configuration from engine.mcp sub-object
	MCPSessionTimeout string // session-timeout: Go duration string for MCP gateway sessions (e.g. "4h", "30m")
	MCPToolTimeout    string // tool-timeout: Go duration string for individual MCP tool calls (e.g. "2m", "30s")

	// Extensions is a list of engine-specific plugin names to install before launching the engine.
	// Currently used by the Pi engine: each entry is passed to `pi install <extension>`.
	Extensions []string

	// CopilotSDK enables the GitHub Copilot SDK integration.
	// When true the compiler enables a harness-managed Copilot CLI headless sidecar
	// and sets COPILOT_SDK_URI on child processes so the SDK can connect to it.
	CopilotSDK bool

	// Cwd is a templatable string that overrides the working directory for the engine's
	// spawned process. When set, it is passed as GH_AW_ENGINE_CWD to the execution
	// environment. JS harness engines read this variable in preference to GITHUB_WORKSPACE;
	// non-harness engines use it as the target of the shell-level cd prefix.
	// Defaults to the repository workspace (GITHUB_WORKSPACE) when empty.
	Cwd string

	// Harness policy fields — templatable integers (literal value or ${{ expr }}).
	// When set, the value is injected as the corresponding GH_AW_HARNESS_* env var so
	// that all harness scripts (copilot, claude, codex) can read it from the environment.
	// The harness falls back to its built-in default when the env var is absent.
	// These are populated from the engine.harness sub-object keys.
	HarnessMaxRetries        string // engine.harness.max-retries        → GH_AW_HARNESS_MAX_RETRIES
	HarnessInitialDelayMs    string // engine.harness.initial-delay-ms   → GH_AW_HARNESS_INITIAL_DELAY_MS
	HarnessBackoffMultiplier string // engine.harness.backoff-multiplier → GH_AW_HARNESS_BACKOFF_MULTIPLIER
	HarnessMaxDelayMs        string // engine.harness.max-delay-ms       → GH_AW_HARNESS_MAX_DELAY_MS
	HarnessWatchdogTimeoutMs string // engine.harness.watchdog-timeout (seconds) → GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS
}

// InlineEngineDriver represents an inline engine.driver source block that gh-aw materializes
// into runtime files before launching the engine.
type InlineEngineDriver struct {
	Runtime         string
	Source          string
	MultipleRuntime bool // true when the driver map contained more than one runtime key
}

// EngineAuthConfig represents engine.auth frontmatter settings that map to
// AWF_AUTH_* environment variables consumed by the AWF API proxy sidecar.
type EngineAuthConfig struct {
	Type     string `json:"type"`
	Audience string `json:"audience"`
	Provider string `json:"provider"` // "azure", "anthropic", or "gcp"
	// Azure WIF fields
	AzureTenantID string `json:"azure-tenant-id"`
	AzureClientID string `json:"azure-client-id"`
	AzureScope    string `json:"azure-scope"`
	AzureCloud    string `json:"azure-cloud"`
	// Anthropic WIF fields
	AnthropicFederationRuleID string `json:"federation-rule-id"`
	AnthropicOrganizationID   string `json:"organization-id"`
	AnthropicServiceAccountID string `json:"service-account-id"`
	AnthropicWorkspaceID      string `json:"workspace-id"`
	// Google / Vertex AI WIF fields
	GoogleWorkloadIdentityProvider string `json:"workload-identity-provider"`
	GoogleServiceAccount           string `json:"service-account"`
	GoogleProject                  string `json:"project"`
	GoogleLocation                 string `json:"location"`
}

// NetworkPermissions represents network access permissions for workflow execution
// Controls which domains the workflow can access during execution.
//
// The Allowed field specifies which domains/ecosystems are permitted:
//   - nil/not set: Use default ecosystem domains (backwards compatibility)
//   - []: Empty list means deny all network access
//   - ["defaults"]: Use default ecosystem domains
//   - ["defaults", "github", "python"]: Expand and merge multiple ecosystems
//   - ["example.com"]: Allow specific domain only
//
// Examples:
//
//  1. String format - use default domains only:
//     network: defaults
//     Result: NetworkPermissions{Allowed: ["defaults"], ExplicitlyDefined: true}
//
//  2. Object format - specify allowed ecosystems/domains:
//     network:
//     allowed:
//     - defaults      # Expands to default ecosystem domains (certs, JSON schema, Ubuntu, etc.)
//     - github        # Expands to GitHub ecosystem domains (*.githubusercontent.com, etc.)
//     - example.com   # Literal domain
//     Result: NetworkPermissions{Allowed: ["defaults", "github", "example.com"], ExplicitlyDefined: true}
//
//  3. Empty object - deny all network access:
//     network: {}
//     Result: NetworkPermissions{Allowed: [], ExplicitlyDefined: true}
//
// Ecosystem identifiers in the Allowed list are expanded to their corresponding domain lists.
// See GetAllowedDomains() for the list of supported ecosystem identifiers.
type NetworkPermissions struct {
	Allowed           []string        `yaml:"allowed,omitempty"` // List of allowed domains or ecosystem identifiers (e.g., "defaults", "github", "python")
	AllowedInput      bool            `yaml:"allowed-input,omitempty"`
	Blocked           []string        `yaml:"blocked,omitempty"`  // List of blocked domains (takes precedence over allowed)
	Firewall          *FirewallConfig `yaml:"firewall,omitempty"` // AWF firewall configuration (see firewall.go)
	ExplicitlyDefined bool            `yaml:"-"`                  // Internal flag: true if network field was explicitly set in frontmatter
}

// EngineNetworkConfig combines engine configuration with top-level network permissions
type EngineNetworkConfig struct {
	Engine  *EngineConfig
	Network *NetworkPermissions
}

type engineTopLevelConfig struct {
	maxTurns           string
	maxToolDenials     string
	maxAICredits       int64
	maxTurnCacheMisses int
	maxRuns            int
	model              string
}

// GetMaxAICredits returns the configured engine AI credits budget, falling back to the default.
func (e *EngineConfig) GetMaxAICredits() int64 {
	if e == nil || e.MaxAICredits == 0 {
		return constants.DefaultMaxAICredits
	}
	return e.MaxAICredits
}

// GetMaxRuns returns the configured AWF max-runs value, falling back to the default.
func (e *EngineConfig) GetMaxRuns() int {
	if e == nil || e.MaxRuns <= 0 {
		return constants.DefaultMaxRuns
	}
	return e.MaxRuns
}

// GetMaxTurnCacheMisses returns the configured AWF max-turn-cache-misses value, falling back
// to the enterprise override or built-in default.
func (e *EngineConfig) GetMaxTurnCacheMisses() int {
	if e == nil || e.MaxTurnCacheMisses <= 0 {
		return compilerenv.ResolveDefaultMaxTurnCacheMisses(constants.DefaultMaxTurnCacheMisses)
	}
	return e.MaxTurnCacheMisses
}

// ExtractEngineConfig extracts engine configuration from frontmatter, supporting both string and object formats.
// It returns the resolved engine setting, the parsed engine configuration, and the resolved model string.
func (c *Compiler) ExtractEngineConfig(frontmatter map[string]any) (string, *EngineConfig, string) {
	topLevel := parseTopLevelEngineConfig(frontmatter)
	engine, exists := frontmatter["engine"]
	if !exists {
		return buildTopLevelOnlyEngineConfig(topLevel)
	}
	engineLog.Print("Extracting engine configuration from frontmatter")
	if engineStr, ok := engine.(string); ok {
		return extractStringEngineConfig(engineStr, topLevel)
	}
	if engineObj, ok := engine.(map[string]any); ok {
		return extractObjectEngineConfig(engineObj, topLevel)
	}
	return buildTopLevelOnlyEngineConfig(topLevel)
}

func parseTopLevelEngineConfig(frontmatter map[string]any) engineTopLevelConfig {
	topLevel := engineTopLevelConfig{
		maxTurns:           parseMaxTurnsValue(frontmatter["max-turns"]),
		maxToolDenials:     parseMaxToolDenialsValue(frontmatter["max-tool-denials"]),
		maxAICredits:       parseMaxAICreditsValue(frontmatter["max-ai-credits"]),
		maxTurnCacheMisses: parseMaxTurnCacheMissesValue(frontmatter["max-turn-cache-misses"]),
		maxRuns:            parseMaxRunsValue(frontmatter["max-turns"]),
	}
	if topLevel.maxRuns == 0 {
		topLevel.maxRuns = parseMaxRunsValue(frontmatter["max-runs"])
	}
	topLevel.model, _ = frontmatter["model"].(string)
	return topLevel
}

func extractStringEngineConfig(engineStr string, topLevel engineTopLevelConfig) (string, *EngineConfig, string) {
	engineLog.Printf("Found engine in string format: %s", engineStr)
	return engineStr, &EngineConfig{
		ID:                 engineStr,
		MaxTurns:           topLevel.maxTurns,
		MaxToolDenials:     topLevel.maxToolDenials,
		MaxRuns:            topLevel.maxRuns,
		MaxTurnCacheMisses: topLevel.maxTurnCacheMisses,
		MaxAICredits:       topLevel.maxAICredits,
	}, topLevel.model
}

func extractObjectEngineConfig(engineObj map[string]any, topLevel engineTopLevelConfig) (string, *EngineConfig, string) {
	engineLog.Print("Found engine in object format, parsing configuration")
	if runtime, hasRuntime := engineObj["runtime"]; hasRuntime {
		return extractInlineEngineConfig(runtime, engineObj, topLevel)
	}
	return extractReferencedEngineConfig(engineObj, topLevel)
}

func extractInlineEngineConfig(runtime any, engineObj map[string]any, topLevel engineTopLevelConfig) (string, *EngineConfig, string) {
	engineLog.Print("Found inline engine definition (engine.runtime sub-object)")
	config := &EngineConfig{IsInlineDefinition: true}
	if runtimeObj, ok := runtime.(map[string]any); ok {
		if id, ok := runtimeObj["id"].(string); ok {
			config.ID = id
			engineLog.Printf("Inline engine runtime.id: %s", config.ID)
		}
		if version, hasVersion := runtimeObj["version"]; hasVersion {
			config.Version = stringutil.ParseVersionValue(version)
		}
	}
	resolvedModel := extractInlineProviderConfig(config, engineObj["provider"])
	applyInlineEngineFields(config, engineObj, topLevel)
	resolvedModel = resolveEngineModel(engineObj, topLevel, resolvedModel)
	engineLog.Printf("Extracted inline engine definition: runtimeID=%s, providerID=%s", config.ID, config.InlineProviderID)
	return config.ID, config, resolvedModel
}

func extractInlineProviderConfig(config *EngineConfig, provider any) string {
	switch providerTyped := provider.(type) {
	case string:
		config.InlineProviderID = string(normalizeEngineProvider(providerTyped))
	case map[string]any:
		if id, ok := providerTyped["id"].(string); ok {
			config.InlineProviderID = id
		}
		model, _ := providerTyped["model"].(string)
		if authObj, ok := providerTyped["auth"].(map[string]any); ok {
			if authDef := parseNonEmptyAuthDefinition(authObj); authDef != nil {
				config.InlineProviderAuth = authDef
			}
		}
		if requestObj, ok := providerTyped["request"].(map[string]any); ok {
			config.InlineProviderRequest = parseRequestShape(requestObj)
		}
		return model
	}
	return ""
}

func parseNonEmptyAuthDefinition(authObj map[string]any) *AuthDefinition {
	authDef := parseAuthDefinition(authObj)
	if authDef.Strategy != "" || authDef.Secret != "" || authDef.TokenURL != "" ||
		authDef.ClientIDRef != "" || authDef.ClientSecretRef != "" || authDef.HeaderName != "" ||
		authDef.TokenField != "" {
		return authDef
	}
	return nil
}

func applyInlineEngineFields(config *EngineConfig, engineObj map[string]any, topLevel engineTopLevelConfig) {
	applyEngineBareField(config, engineObj)
	applyEnginePermissionMode(config, engineObj)
	config.MaxTurns = topLevel.maxTurns
	config.MaxToolDenials = topLevel.maxToolDenials
	config.MaxRuns = topLevel.maxRuns
	config.MaxTurnCacheMisses = topLevel.maxTurnCacheMisses
	config.MaxAICredits = topLevel.maxAICredits
}

func extractReferencedEngineConfig(engineObj map[string]any, topLevel engineTopLevelConfig) (string, *EngineConfig, string) {
	config := &EngineConfig{}
	if id, ok := engineObj["id"].(string); ok {
		config.ID = id
	}
	if version, hasVersion := engineObj["version"]; hasVersion {
		config.Version = stringutil.ParseVersionValue(version)
	}
	resolvedModel := resolveEngineModel(engineObj, topLevel, "")
	applyReferencedEngineFields(config, engineObj, topLevel)
	engineLog.Printf("Extracted engine configuration: ID=%s", config.ID)
	return config.ID, config, resolvedModel
}

func applyReferencedEngineFields(config *EngineConfig, engineObj map[string]any, topLevel engineTopLevelConfig) {
	applyEngineProviderFields(config, engineObj)
	applyEnginePermissionMode(config, engineObj)
	applyEngineTurnFields(config, engineObj, topLevel)
	applyEngineConcurrencyField(config, engineObj)
	applyEngineStringFields(config, engineObj)
	applyEngineHarnessField(config, engineObj)
	applyEngineEnvField(config, engineObj)
	applyEngineAuthField(config, engineObj)
	applyEngineArgsField(config, engineObj)
	applyEngineMCPField(config, engineObj)
	applyEngineExtensionsField(config, engineObj)
	applyEngineBooleanFields(config, engineObj)
	applyEngineTopLevelOverrides(config, topLevel)
}

func resolveEngineModel(engineObj map[string]any, topLevel engineTopLevelConfig, fallback string) string {
	if modelStr, ok := engineObj["model"].(string); ok && modelStr != "" {
		// engine.model is an explicit override for this engine instance and takes
		// precedence over the top-level model (e.g. threat-detection.engine.model
		// selecting a different model than the main agent engine).
		return modelStr
	}
	if topLevel.model != "" {
		return topLevel.model
	}
	return fallback
}

func applyEngineProviderFields(config *EngineConfig, engineObj map[string]any) {
	if providerStr, ok := engineObj["model-provider"].(string); ok {
		config.LLMProvider = normalizeEngineProvider(providerStr)
	}
	if providerStr, ok := engineObj["provider"].(string); ok && !config.IsInlineDefinition {
		config.LLMProvider = normalizeEngineProvider(providerStr)
	}
}

func normalizeEngineProvider(provider string) LLMProvider {
	return LLMProvider(strings.ToLower(strings.TrimSpace(provider)))
}

func applyEnginePermissionMode(config *EngineConfig, engineObj map[string]any) {
	if permissionMode, ok := engineObj["permission-mode"].(string); ok {
		config.PermissionMode = permissionMode
	}
}

func applyEngineTurnFields(config *EngineConfig, engineObj map[string]any, topLevel engineTopLevelConfig) {
	if maxTurns, hasMaxTurns := engineObj["max-turns"]; hasMaxTurns {
		config.MaxTurns = parseMaxTurnsValue(maxTurns)
	}
	if topLevel.maxTurns != "" {
		config.MaxTurns = topLevel.maxTurns
	}
	config.MaxToolDenials = topLevel.maxToolDenials
	if maxCont, hasMaxCont := engineObj["max-continuations"]; hasMaxCont {
		if val, ok := typeutil.ParseIntValue(maxCont); ok {
			config.MaxContinuations = val
		} else if maxContStr, ok := maxCont.(string); ok {
			if parsed, err := strconv.Atoi(maxContStr); err == nil {
				config.MaxContinuations = parsed
			}
		}
	}
}

func applyEngineConcurrencyField(config *EngineConfig, engineObj map[string]any) {
	concurrency, hasConcurrency := engineObj["concurrency"]
	if !hasConcurrency {
		return
	}
	if concurrencyStr, ok := concurrency.(string); ok {
		config.Concurrency = fmt.Sprintf("concurrency:\n  group: \"%s\"", concurrencyStr)
		return
	}
	concurrencyObj, ok := concurrency.(map[string]any)
	if !ok {
		return
	}
	if groupStr, ok := concurrencyObj["group"].(string); ok {
		config.Concurrency = fmt.Sprintf("concurrency:\n  group: \"%s\"", groupStr)
	}
	if cancelBool, ok := concurrencyObj["cancel-in-progress"].(bool); ok && cancelBool && config.Concurrency != "" {
		config.Concurrency += "\n  cancel-in-progress: true"
	}
	if queueStr, ok := concurrencyObj["queue"].(string); ok && queueStr != "" && config.Concurrency != "" {
		config.Concurrency += "\n  queue: " + queueStr
	}
}

func applyEngineStringFields(config *EngineConfig, engineObj map[string]any) {
	if userAgent, ok := engineObj["user-agent"].(string); ok {
		config.UserAgent = userAgent
	}
	if command, ok := engineObj["command"].(string); ok {
		config.Command = command
	}
	applyEngineDriverField(config, engineObj)
	if configStr, ok := engineObj["config"].(string); ok {
		config.Config = configStr
	}
	if agent, ok := engineObj["agent"].(string); ok {
		config.Agent = agent
		engineLog.Printf("Extracted agent identifier: %s", agent)
	}
	if apiTarget, ok := engineObj["api-target"].(string); ok && apiTarget != "" {
		config.APITarget = apiTarget
		engineLog.Printf("Extracted api-target: %s", config.APITarget)
	}
	if cwd, ok := engineObj["cwd"].(string); ok && cwd != "" {
		config.Cwd = cwd
		engineLog.Printf("Extracted engine.cwd: %s", config.Cwd)
	}
}

func applyEngineDriverField(config *EngineConfig, engineObj map[string]any) {
	driverValue, exists := engineObj["driver"]
	if !exists {
		return
	}

	if driver, ok := driverValue.(string); ok {
		config.Driver = driver
		engineLog.Printf("Extracted engine.driver: %s", driver)
		return
	}

	driverMap, ok := driverValue.(map[string]any)
	if !ok {
		return
	}

	// Count how many runtime keys are present in the map. Multiple keys are
	// rejected via validateInlineEngineDriver; a single key is extracted.
	var matched []string
	for _, runtime := range []string{"node", "python", "go", "java"} {
		if _, ok := driverMap[runtime].(string); ok {
			matched = append(matched, runtime)
		}
	}

	if len(matched) == 0 {
		return
	}

	// Pick the first match. Validation will reject the map when len > 1.
	runtime := matched[0]
	source, _ := driverMap[runtime].(string)
	// Preserve runtime even for empty source so validateInlineEngineDriver
	// can reject it with a clear error rather than silently bypassing checks.
	config.InlineDriver = &InlineEngineDriver{
		Runtime:         runtime,
		Source:          source,
		MultipleRuntime: len(matched) > 1,
	}
	config.Driver = inlineCopilotSDKDriverWrapperPath
	engineLog.Printf("Extracted inline engine.driver runtime: %s", runtime)
}

func applyEngineHarnessField(config *EngineConfig, engineObj map[string]any) {
	harness, hasHarness := engineObj["harness"]
	if !hasHarness {
		return
	}
	switch h := harness.(type) {
	case string:
		config.HarnessScript = h
	case map[string]any:
		if use, ok := h["use"].(string); ok {
			config.HarnessScript = use
		}
		if v, ok := h["max-retries"]; ok {
			config.HarnessMaxRetries = parseHarnessMaxRetriesValue(v)
		}
		if v, ok := h["initial-delay-ms"]; ok {
			config.HarnessInitialDelayMs = parseMaxTurnsValue(v)
		}
		if v, ok := h["backoff-multiplier"]; ok {
			config.HarnessBackoffMultiplier = parseMaxTurnsValue(v)
		}
		if v, ok := h["max-delay-ms"]; ok {
			config.HarnessMaxDelayMs = parseMaxTurnsValue(v)
		}
		if v, ok := h["watchdog-timeout"]; ok {
			config.HarnessWatchdogTimeoutMs = parseHarnessWatchdogTimeoutValue(v)
		}
	}
}

func applyEngineEnvField(config *EngineConfig, engineObj map[string]any) {
	envMap, ok := engineObj["env"].(map[string]any)
	if !ok {
		return
	}
	config.Env = make(map[string]string)
	for key, value := range envMap {
		if valueStr, ok := toEngineEnvValueString(value); ok {
			config.Env[key] = valueStr
		}
	}
}

func applyEngineAuthField(config *EngineConfig, engineObj map[string]any) {
	authObj, ok := engineObj["auth"].(map[string]any)
	if !ok {
		return
	}
	config.Auth = parseEngineAuthConfig(authObj)
	applyEngineAuthEnv(config)
}

func applyEngineArgsField(config *EngineConfig, engineObj map[string]any) {
	switch args := engineObj["args"].(type) {
	case []any:
		config.Args = make([]string, 0, len(args))
		for _, arg := range args {
			if argStr, ok := arg.(string); ok {
				config.Args = append(config.Args, argStr)
			}
		}
	case []string:
		config.Args = args
	}
}

func applyEngineMCPField(config *EngineConfig, engineObj map[string]any) {
	mcpObj, ok := engineObj["mcp"].(map[string]any)
	if !ok {
		return
	}
	if sessionTimeout, ok := mcpObj["session-timeout"].(string); ok && sessionTimeout != "" {
		config.MCPSessionTimeout = sessionTimeout
		engineLog.Printf("Extracted engine.mcp.session-timeout: %s", config.MCPSessionTimeout)
	}
	if toolTimeout, ok := mcpObj["tool-timeout"].(string); ok && toolTimeout != "" {
		config.MCPToolTimeout = toolTimeout
		engineLog.Printf("Extracted engine.mcp.tool-timeout: %s", config.MCPToolTimeout)
	}
}

func applyEngineExtensionsField(config *EngineConfig, engineObj map[string]any) {
	switch v := engineObj["extensions"].(type) {
	case []any:
		config.Extensions = make([]string, 0, len(v))
		for _, ext := range v {
			if extStr, ok := ext.(string); ok && extStr != "" {
				config.Extensions = append(config.Extensions, extStr)
			}
		}
	case []string:
		config.Extensions = make([]string, 0, len(v))
		for _, ext := range v {
			if ext != "" {
				config.Extensions = append(config.Extensions, ext)
			}
		}
	case nil:
		return
	default:
		engineLog.Printf("Unexpected type for engine.extensions: %T, ignoring", engineObj["extensions"])
		return
	}
	engineLog.Printf("Extracted engine.extensions: %v", config.Extensions)
}

func applyEngineBooleanFields(config *EngineConfig, engineObj map[string]any) {
	applyEngineBareField(config, engineObj)
	if sdkVal, ok := engineObj["copilot-sdk"].(bool); ok {
		config.CopilotSDK = sdkVal
		engineLog.Printf("Extracted copilot-sdk: %v", config.CopilotSDK)
	}
	if config.Driver != "" && config.ID == "copilot" && !config.CopilotSDK {
		config.CopilotSDK = true
		engineLog.Print("Enabled copilot-sdk because driver is configured for copilot engine")
	}
}

func applyEngineBareField(config *EngineConfig, engineObj map[string]any) {
	if bare, ok := engineObj["bare"].(bool); ok {
		config.Bare = bare
		engineLog.Printf("Extracted bare mode: %v", config.Bare)
	}
}

func applyEngineTopLevelOverrides(config *EngineConfig, topLevel engineTopLevelConfig) {
	if topLevel.maxTurns != "" {
		config.MaxTurns = topLevel.maxTurns
	}
	config.MaxRuns = topLevel.maxRuns
	config.MaxTurnCacheMisses = topLevel.maxTurnCacheMisses
	config.MaxAICredits = topLevel.maxAICredits
}

func buildTopLevelOnlyEngineConfig(topLevel engineTopLevelConfig) (string, *EngineConfig, string) {
	if topLevel.maxTurns != "" || topLevel.maxToolDenials != "" || topLevel.maxAICredits != 0 ||
		topLevel.maxRuns > 0 || topLevel.maxTurnCacheMisses > 0 || topLevel.model != "" {
		return "", &EngineConfig{
			MaxTurns:           topLevel.maxTurns,
			MaxToolDenials:     topLevel.maxToolDenials,
			MaxRuns:            topLevel.maxRuns,
			MaxTurnCacheMisses: topLevel.maxTurnCacheMisses,
			MaxAICredits:       topLevel.maxAICredits,
		}, topLevel.model
	}
	engineLog.Print("No engine configuration found in frontmatter")
	return "", nil, ""
}

// getAgenticEngine returns the agentic engine for the given engine setting
func (c *Compiler) getAgenticEngine(engineSetting string) (CodingAgentEngine, error) {
	if engineSetting == "" {
		defaultEngine := c.engineRegistry.GetDefaultEngine()
		engineLog.Printf("Using default engine: %s", defaultEngine.GetID())
		return defaultEngine, nil
	}

	engineLog.Printf("Getting agentic engine for setting: %s", engineSetting)

	// First try exact match
	if c.engineRegistry.IsValidEngine(engineSetting) {
		engine, err := c.engineRegistry.GetEngine(engineSetting)
		if err == nil {
			engineLog.Printf("Found engine by exact match: %s", engine.GetID())
		}
		return engine, err
	}

	// Try prefix match for backward compatibility
	engine, err := c.engineRegistry.GetEngineByPrefix(engineSetting)
	if err == nil {
		engineLog.Printf("Found engine by prefix match: %s", engine.GetID())
		return engine, nil
	}

	engineLog.Printf("Failed to find engine for setting %s: %v", engineSetting, err)

	validEngines := c.engineRegistry.GetSupportedEngines()
	suggestions := parser.FindClosestMatches(engineSetting, validEngines, 1)
	enginesStr := strings.Join(validEngines, ", ")

	errMsg := fmt.Sprintf("invalid engine: %s. Valid engines are: %s.\n\nExample:\nengine: copilot\n\nSee: %s",
		engineSetting, enginesStr, constants.DocsEnginesURL)
	if len(suggestions) > 0 {
		errMsg = fmt.Sprintf("invalid engine: %s. Valid engines are: %s.\n\nDid you mean: %s?\n\nExample:\nengine: copilot\n\nSee: %s",
			engineSetting, enginesStr, suggestions[0], constants.DocsEnginesURL)
	}

	return nil, errors.New(errMsg)
}

// extractEngineConfigFromJSON parses engine configuration from JSON string (from included files).
func (c *Compiler) extractEngineConfigFromJSON(engineJSON string) (*EngineConfig, string, error) {
	if engineJSON == "" {
		return nil, "", nil
	}

	var engineData any
	if err := json.Unmarshal([]byte(engineJSON), &engineData); err != nil {
		return nil, "", fmt.Errorf("engine configuration must contain valid JSON: %w", err)
	}

	// Use the existing ExtractEngineConfig function by creating a temporary frontmatter map
	tempFrontmatter := map[string]any{
		"engine": engineData,
	}

	_, config, model := c.ExtractEngineConfig(tempFrontmatter)
	return config, model, nil
}

// applyEngineAuthEnv populates config.Env with AWF_AUTH_* environment variables
// derived from config.Auth. Existing config.Env values take precedence so users
// can explicitly override auth-derived values via engine.env.
func applyEngineAuthEnv(config *EngineConfig) {
	if config == nil || config.Auth == nil {
		return
	}
	if config.Env == nil {
		config.Env = make(map[string]string)
	}
	setEngineAuthEnv(config.Env, "AWF_AUTH_TYPE", config.Auth.Type)
	setEngineAuthEnv(config.Env, "AWF_AUTH_OIDC_AUDIENCE", config.Auth.Audience)
	setEngineAuthEnv(config.Env, "AWF_AUTH_AZURE_TENANT_ID", config.Auth.AzureTenantID)
	setEngineAuthEnv(config.Env, "AWF_AUTH_AZURE_CLIENT_ID", config.Auth.AzureClientID)
	setEngineAuthEnv(config.Env, "AWF_AUTH_AZURE_SCOPE", config.Auth.AzureScope)
	setEngineAuthEnv(config.Env, "AWF_AUTH_AZURE_CLOUD", config.Auth.AzureCloud)
	setEngineAuthEnv(config.Env, "AWF_AUTH_PROVIDER", config.Auth.Provider)
	setEngineAuthEnv(config.Env, "AWF_AUTH_ANTHROPIC_FEDERATION_RULE_ID", config.Auth.AnthropicFederationRuleID)
	setEngineAuthEnv(config.Env, "AWF_AUTH_ANTHROPIC_ORGANIZATION_ID", config.Auth.AnthropicOrganizationID)
	setEngineAuthEnv(config.Env, "AWF_AUTH_ANTHROPIC_SERVICE_ACCOUNT_ID", config.Auth.AnthropicServiceAccountID)
	setEngineAuthEnv(config.Env, "AWF_AUTH_ANTHROPIC_WORKSPACE_ID", config.Auth.AnthropicWorkspaceID)
	setEngineAuthEnv(config.Env, "AWF_AUTH_GCP_WORKLOAD_IDENTITY_PROVIDER", config.Auth.GoogleWorkloadIdentityProvider)
	setEngineAuthEnv(config.Env, "AWF_AUTH_GCP_SERVICE_ACCOUNT", config.Auth.GoogleServiceAccount)
}

func setEngineAuthEnv(env map[string]string, key, value string) {
	if value == "" {
		return
	}
	if _, exists := env[key]; !exists {
		env[key] = value
	}
}
