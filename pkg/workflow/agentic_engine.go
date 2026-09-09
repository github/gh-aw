package workflow

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/sliceutil"
)

var agenticEngineLog = logger.New("workflow:agentic_engine")

// GitHubActionStep represents the YAML lines for a single step in a GitHub Actions workflow
type GitHubActionStep []string

// Interface Segregation Architecture
//
// The agentic engine interfaces follow the Interface Segregation Principle (ISP) to avoid
// forcing implementations to depend on methods they don't use. The architecture uses interface
// composition to provide flexibility while maintaining backward compatibility.
//
// Core Principles:
// 1. Focused interfaces with single responsibilities
// 2. Composition over monolithic interfaces
// 3. Backward compatibility via composite interface
// 4. Optional capabilities via interface type assertions
//
// Interface Hierarchy:
//
//   Engine (core identity - required by all)
//   ├── GetID()
//   ├── GetDisplayName()
//   ├── GetDescription()
//   └── IsExperimental()
//
//   CapabilityProvider (feature detection - optional)
//   └── GetCapabilities()
//
//   WorkflowExecutor (compilation - required)
//   ├── GetDeclaredOutputFiles()
//   ├── GetInstallationSteps()
//   └── GetExecutionSteps()
//
//   MCPConfigProvider (MCP servers - optional)
//   └── RenderMCPConfig()
//
//   LogParser (log analysis - optional)
//   ├── ParseLogMetrics()
//   ├── GetLogParserScriptId()
//   └── GetLogFileForParsing()
//
//   SecurityProvider (security features - optional)
//   ├── GetDefaultDetectionModel()
//   └── GetRequiredSecretNames()
//
//   CodingAgentEngine (composite - backward compatibility)
//   └── Composes all above interfaces
//
// Usage Patterns:
//
// 1. For code that only needs identity information:
//    func processEngine(e Engine) { ... }
//
// 2. For code that needs capability checks:
//    func checkCapabilities(cp CapabilityProvider) { ... }
//
// 3. For backward compatibility (existing code):
//    func compile(engine CodingAgentEngine) { ... }
//
// Implementation:
//
// All engines embed BaseEngine which provides default implementations for all methods.
// Engines can override specific methods to provide custom behavior.
//
// Example:
//   type MyEngine struct {
//       BaseEngine
//   }
//
//   func (e *MyEngine) GetInstallationSteps(...) []GitHubActionStep {
//       // Custom implementation
//   }

// Engine represents the core identity of an AI coding agent
// All engines must implement this interface to provide basic identification
type Engine interface {
	// GetID returns the unique identifier for this engine
	GetID() string

	// GetDisplayName returns the human-readable name for this engine
	GetDisplayName() string

	// GetDescription returns a description of this engine's capabilities
	GetDescription() string

	// IsExperimental returns true if this engine is experimental
	IsExperimental() bool

	// GetGHSkillAgentName returns the gh skill --agent value for this engine.
	// Returns an empty string when gh skill install does not support the engine.
	GetGHSkillAgentName() string
}

// EngineCapabilities captures optional engine features.
// New capabilities should be added here so existing engines inherit the zero-value default.
type EngineCapabilities struct {
	// ToolsAllowlist reports whether the engine supports MCP tool allow-listing.
	ToolsAllowlist bool

	// MCP reports whether the engine supports MCP servers directly.
	MCP bool

	// MaxTurns reports whether the engine supports the max-turns feature.
	MaxTurns bool

	// WebSearch reports whether the engine has built-in support for the web-search tool.
	WebSearch bool

	// MaxContinuations reports whether the engine supports the max-continuations feature.
	// When true, max-continuations > 1 enables autopilot/multi-run mode for the engine.
	MaxContinuations bool

	// NativeAgentFile reports whether the engine handles agent-file imports natively
	// in its own execution steps (reading the file, stripping frontmatter, and prepending the
	// content to the prompt at runtime). When false, the compiler is responsible for including
	// the agent file content in prompt.txt during the activation job so that the engine just
	// reads the standard /tmp/gh-aw/aw-prompts/prompt.txt as usual.
	NativeAgentFile bool

	// BareMode reports whether the engine supports the bare mode feature (engine.bare: true),
	// which suppresses automatic loading of context and custom instructions. When false,
	// specifying bare: true emits a warning and has no effect.
	BareMode bool

	// BashCommandAllowlist reports whether the engine enforces a bash command allowlist
	// derived from tools.bash: [cmd1, cmd2, ...]. When true, the engine maps the
	// allowlist to its own CLI syntax (e.g. --allowed-tools Bash(cmd), run_shell_command(cmd)).
	// When false, a restricted tools.bash allowlist is silently ignored at runtime,
	// so the compiler emits an error to prevent the allowlist illusion.
	BashCommandAllowlist bool

	// BashDisable reports whether the engine can fully refuse all bash/shell tool calls
	// when tools.bash is completely disabled (tools.bash: false, or tools.bash: []).
	// This is a coarser capability than BashCommandAllowlist: an engine may be unable to
	// enforce a partial per-command allowlist yet still be able to turn shell execution
	// off entirely (e.g. Codex's `features.shell_tool=false` config flag). When true, the
	// compiler allows a fully-disabled tools.bash even if BashCommandAllowlist is false.
	BashDisable bool

	// Plugins reports whether the engine can install Agent Plugins.
	Plugins bool
}

// PluginInstallationProvider generates installation steps for Agent Plugins.
type PluginInstallationProvider interface {
	GetPluginInstallationSteps(workflowData *WorkflowData) []GitHubActionStep
}

// CapabilityProvider detects what capabilities an engine supports.
// Engines can optionally implement this to indicate feature support.
type CapabilityProvider interface {
	GetCapabilities() EngineCapabilities
}

// MCPProxyEngine provides the identity and MCP capability information needed to
// configure CLI proxy tools.
type MCPProxyEngine interface {
	Engine
	CapabilityProvider
}

// WorkflowExecutor handles workflow compilation and execution
// All engines must implement this to generate GitHub Actions steps
type WorkflowExecutor interface {
	// GetDeclaredOutputFiles returns a list of output files that this engine may produce
	// These files will be automatically uploaded as artifacts if they exist
	GetDeclaredOutputFiles() []string

	// GetInstallationSteps returns the GitHub Actions steps needed to install this engine
	GetInstallationSteps(workflowData *WorkflowData) []GitHubActionStep

	// GetSecretValidationStep returns the step that validates required secrets are available.
	// This step is added to the activation job before context variable validation.
	// Returns an empty GitHubActionStep if no secret validation is needed.
	GetSecretValidationStep(workflowData *WorkflowData) GitHubActionStep

	// GetSecretFailureMessage returns an engine-specific markdown message shown in the
	// agentic failure issue when the secret validation step fails. Return an empty string
	// if the engine has no specific guidance beyond the default error message.
	GetSecretFailureMessage(workflowData *WorkflowData) string

	// GetExecutionSteps returns the GitHub Actions steps for executing this engine
	GetExecutionSteps(workflowData *WorkflowData, logFile string) []GitHubActionStep

	// GetFirewallLogsCollectionStep returns steps that collect firewall-related log files
	// before secret redaction runs. Engines that copy session or firewall state files should
	// override this; the default implementation returns an empty slice.
	GetFirewallLogsCollectionStep(workflowData *WorkflowData) []GitHubActionStep

	// GetAPMTarget returns the APM target value to use when packing dependencies with
	// microsoft/apm-action. Supported values are "copilot", "claude", and "all".
	// The default implementation returns "all" (packs all primitive types).
	GetAPMTarget() string

	// GetPreBundleSteps returns GitHub Actions steps that must run before the unified artifact
	// upload. These steps are injected prior to secret redaction so any engine-produced files
	// are moved into /tmp/gh-aw/ where they will be scanned and picked up by the artifact
	// upload under the correct common-ancestor path.
	// The default implementation returns an empty slice.
	GetPreBundleSteps(workflowData *WorkflowData) []GitHubActionStep
}

// MCPConfigProvider handles MCP (Model Context Protocol) configuration
// Engines that support MCP servers should implement this
type MCPConfigProvider interface {
	// RenderMCPConfig renders the MCP configuration for this engine to the given YAML builder
	RenderMCPConfig(yaml *strings.Builder, tools map[string]any, mcpTools []string, workflowData *WorkflowData) error
}

// LogParser handles parsing and analyzing engine logs
// Engines can optionally implement this to provide detailed log parsing
type LogParser interface {
	// ParseLogMetrics extracts metrics from engine-specific log content
	ParseLogMetrics(logContent string, verbose bool) LogMetrics

	// GetLogParserScriptId returns the name of the JavaScript script to parse logs for this engine
	GetLogParserScriptId() string

	// GetLogFileForParsing returns the log file path to use for JavaScript parsing in the workflow
	// This may be different from the stdout/stderr log file if the engine produces separate detailed logs
	GetLogFileForParsing() string

	// GetErrorDetectionScriptId returns the name of the JavaScript script used to detect engine
	// errors from the agent stdio log after execution. The script runs on the host runner (outside
	// the AWF sandbox container) so it can write to GITHUB_OUTPUT. Returns empty string if the
	// engine does not provide a post-execution error detection step.
	GetErrorDetectionScriptId() string

	// GetInternalLogsDir returns the host-runner path of a directory containing the engine's own
	// internal tracing/diagnostic log files (as opposed to the captured stdout/stderr agent log),
	// or empty string if the engine does not write such logs. When non-empty, the error detection
	// script (see GetErrorDetectionScriptId) tails the most recently modified log file under this
	// directory into the step log when the execution step failed, surfacing diagnosable output for
	// crashes that print nothing to stdout/stderr.
	GetInternalLogsDir() string
}

// SecurityProvider handles security-related configuration
// Engines can optionally implement this to provide security features
type SecurityProvider interface {
	// GetDefaultDetectionModel returns the default model to use for threat detection
	// If empty, no default model is applied and the engine uses its standard default
	GetDefaultDetectionModel() string

	// GetRequiredSecretNames returns the list of secret names that this engine needs for execution
	// This includes engine-specific auth tokens and the MCP gateway agent ID when MCP servers are present
	// Returns: slice of secret names (e.g., ["COPILOT_GITHUB_TOKEN", "MCP_GATEWAY_AGENT_ID"])
	GetRequiredSecretNames(workflowData *WorkflowData) []string

	// GetSupportedEnvVarKeys returns the list of engine.env variable names that this engine
	// explicitly supports as documented in the AWF specification. These keys are allowed to carry
	// secrets in engine.env without triggering the strict-mode secret-leakage check.
	// Unlike GetRequiredSecretNames, this list is static and contains only the env var keys
	// that are purposefully exposed to the engine container via engine.env.
	GetSupportedEnvVarKeys() []string
}

// ModelEnvVarProvider is implemented by engines whose CLIs natively read a specific
// environment variable for model selection (e.g., COPILOT_MODEL, ANTHROPIC_MODEL).
// The default implementation in BaseEngine returns "" (no native env var).
type ModelEnvVarProvider interface {
	// GetModelEnvVarName returns the name of the native environment variable the CLI
	// uses for model selection (e.g., "COPILOT_MODEL", "ANTHROPIC_MODEL", "GEMINI_MODEL").
	// Returns an empty string if the engine does not support a native model env var.
	GetModelEnvVarName() string
}

// InferenceProviderResolver is implemented by engines that support selecting
// different inference providers at runtime (for example engine.provider).
// This interface is intentionally separate from CodingAgentEngine so provider
// concerns remain decoupled from core engine execution capabilities.
type InferenceProviderResolver interface {
	// ResolveLLMProvider returns the effective provider for the workflow
	// (for example "github", "anthropic", or "openai").
	ResolveLLMProvider(workflowData *WorkflowData) LLMProvider
}

// LLMProviderResolver is kept as a backward-compatible alias.
type LLMProviderResolver = InferenceProviderResolver

// AgentFileProvider is an optional interface implemented by engines that have
// engine-specific instruction or configuration files that should be treated as
// security-sensitive manifests.  The compiler uses these lists to extend the
// global manifest-file protection so that engine-specific files (e.g. CLAUDE.md,
// .claude/) are automatically protected alongside dependency manifests.
type AgentFileProvider interface {
	// GetAgentManifestFiles returns the basenames of files that are specific to
	// this engine's instruction / configuration format (e.g. "CLAUDE.md").
	// Matching is by filename only, regardless of directory depth.
	GetAgentManifestFiles() []string

	// GetAgentManifestPathPrefixes returns path prefixes (relative to the repo
	// root) for directories that contain engine-specific configuration.
	// Any file whose diff path starts with one of these prefixes is treated as a
	// protected file (e.g. ".claude/").
	GetAgentManifestPathPrefixes() []string
}

// ConfigRenderer is an optional hook that runtimes may implement to emit generated
// config files or metadata before execution steps run.
type ConfigRenderer interface {
	// RenderConfig optionally generates runtime config files or metadata.
	// Returns a slice of GitHub Actions steps that write config to disk.
	// Implementations that don't need config files should return nil, nil.
	RenderConfig(target *ResolvedEngineTarget) ([]map[string]any, error)
}

// HarnessRunner is an optional interface implemented by engines that provide a
// JavaScript harness script to wrap CLI execution with retry and recovery logic.
// The harness is placed in the setup actions directory and executed via Node.js
// as a transparent subprocess wrapper around the engine CLI.
type HarnessRunner interface {
	// GetHarnessScriptName returns the filename of the JavaScript harness script
	// (located in the setup actions directory) used to wrap CLI execution.
	// Returns an empty string if no harness is needed.
	GetHarnessScriptName() string
}

// HarnessProvider is kept as a backward-compatible alias.
type HarnessProvider = HarnessRunner

// MCPConfigAdapterProvider is an optional interface implemented by engines that provide
// a JavaScript config-adapter script to convert the MCP gateway's raw output
// configuration into the engine-specific format (e.g. Goose's .goose/mcp.json).
// The script is written to the setup actions directory and executed by
// start_mcp_gateway.cjs in place of a built-in per-engine converter.
type MCPConfigAdapterProvider interface {
	// GetMCPConfigAdapterWriteStep returns a GitHub Actions step that writes the
	// config-adapter script to disk, or nil if the engine has no config-adapter script.
	GetMCPConfigAdapterWriteStep() GitHubActionStep
	// GetMCPConfigAdapterFilename returns the filename (not path) of the
	// config-adapter script located in the setup actions directory, or an empty
	// string if the engine has no config-adapter script.
	GetMCPConfigAdapterFilename() string
}

// engineRequiresNodeHarness reports whether the engine's execution command wraps
// the CLI with a harness script launched via node (see nodeRuntimeResolutionCommand
// in copilot_engine_execution.go). Used by call sites that must ensure node is on
// PATH before the harness runs — notably the detection job, which does not go
// through DetectRuntimeRequirements.
func engineRequiresNodeHarness(engine CodingAgentEngine) bool {
	if engine == nil {
		return false
	}
	hp, ok := engine.(HarnessRunner)
	if !ok {
		return false
	}
	return hp.GetHarnessScriptName() != ""
}

// CodingAgentEngine is a composite interface that combines all focused interfaces
// This maintains backward compatibility with existing code while allowing more flexibility
// Implementations can choose to implement only the interfaces they need by embedding BaseEngine
type CodingAgentEngine interface {
	Engine
	CapabilityProvider
	WorkflowExecutor
	MCPConfigProvider
	LogParser
	SecurityProvider
	ModelEnvVarProvider
	ConfigRenderer
}

// BaseEngine provides common functionality for agentic engines
type BaseEngine struct {
	id                      string
	displayName             string
	description             string
	experimental            bool
	ghSkillAgentName        string
	capabilities            EngineCapabilities
	dedicatedLLMGatewayPort int
}

func (e *BaseEngine) GetID() string {
	return e.id
}

func (e *BaseEngine) GetDisplayName() string {
	return e.displayName
}

func (e *BaseEngine) GetDescription() string {
	return e.description
}

func (e *BaseEngine) IsExperimental() bool {
	return e.experimental
}

func (e *BaseEngine) GetGHSkillAgentName() string {
	return e.ghSkillAgentName
}

func (e *BaseEngine) GetCapabilities() EngineCapabilities {
	return e.capabilities
}

func (e *BaseEngine) getDedicatedLLMGatewayPort() int {
	return e.dedicatedLLMGatewayPort
}

// GetDeclaredOutputFiles returns an empty list by default (engines can override)
func (e *BaseEngine) GetDeclaredOutputFiles() []string {
	return []string{}
}

// GetDefaultDetectionModel returns empty string by default (no default model)
// Engines can override this to provide a cost-effective default for detection jobs
func (e *BaseEngine) GetDefaultDetectionModel() string {
	return ""
}

// GetModelEnvVarName returns empty string by default (no native model env var).
// Engines whose CLI natively supports a model env var (e.g. COPILOT_MODEL, ANTHROPIC_MODEL)
// should override this to return that variable name.
func (e *BaseEngine) GetModelEnvVarName() string {
	return ""
}

// GetLogFileForParsing returns the default log file path for parsing
// Engines can override this to use engine-specific log files
func (e *BaseEngine) GetLogFileForParsing() string {
	// Default to agent-stdio.log which contains stdout/stderr
	return constants.AgentStdioLogPath
}

// GetRequiredSecretNames returns an empty list by default
// Engines must override this to specify their required secrets
func (e *BaseEngine) GetRequiredSecretNames(workflowData *WorkflowData) []string {
	return []string{}
}

// GetSupportedEnvVarKeys returns an empty list by default.
// Engines should override this to enumerate the engine.env keys they support,
// as defined in the AWF specification (engine_constants.go).
func (e *BaseEngine) GetSupportedEnvVarKeys() []string {
	return []string{}
}

// GetSecretValidationStep returns an empty step by default.
// Engines that require secret validation must override this method.
func (e *BaseEngine) GetSecretValidationStep(workflowData *WorkflowData) GitHubActionStep {
	return GitHubActionStep{}
}

// GetSecretFailureMessage returns an empty string by default.
// Engines that want to provide custom guidance when secret validation fails must override this method.
func (e *BaseEngine) GetSecretFailureMessage(workflowData *WorkflowData) string {
	return ""
}

// GetFirewallLogsCollectionStep returns an empty slice by default.
// Firewall logs are written to a known location (/tmp/gh-aw/sandbox/firewall/logs/)
// and do not require a separate collection step. The method is still called from
// compiler_yaml_main_job.go to maintain a consistent interface; engines that need
// to copy session or firewall state files before secret redaction should override it.
func (e *BaseEngine) GetFirewallLogsCollectionStep(workflowData *WorkflowData) []GitHubActionStep {
	return []GitHubActionStep{}
}

// GetAPMTarget returns "all" by default (packs all primitive types).
// CopilotEngine overrides this to return "copilot"; ClaudeEngine overrides to return "claude".
func (e *BaseEngine) GetAPMTarget() string {
	return "all"
}

// GetPreBundleSteps returns an empty slice by default.
// Engines that need to relocate output files into /tmp/gh-aw/ before the unified artifact
// upload should override this method.
func (e *BaseEngine) GetPreBundleSteps(workflowData *WorkflowData) []GitHubActionStep {
	return []GitHubActionStep{}
}

// ParseLogMetrics provides a default no-op implementation for log parsing
// Engines can override this to provide detailed log parsing and metrics extraction
func (e *BaseEngine) ParseLogMetrics(logContent string, verbose bool) LogMetrics {
	return LogMetrics{}
}

// GetLogParserScriptId returns empty string by default (no JavaScript parser)
// Engines can override this to provide a JavaScript parser for log analysis
func (e *BaseEngine) GetLogParserScriptId() string {
	return ""
}

// GetErrorDetectionScriptId returns empty string by default (no post-execution error detection)
// Engines can override this to provide a host-runner script that detects errors in the agent
// stdio log and writes them as GITHUB_OUTPUT values after the AWF container exits.
func (e *BaseEngine) GetErrorDetectionScriptId() string {
	return ""
}

// GetInternalLogsDir returns empty string by default (no engine-internal log directory).
// Engines that write their own tracing/diagnostic logs to files (separate from the captured
// stdout/stderr) can override this to have those logs tailed into the step log on failure.
func (e *BaseEngine) GetInternalLogsDir() string {
	return ""
}

// RenderMCPConfig provides a default no-op implementation for MCP configuration
// Engines can override this to provide custom MCP server configuration
func (e *BaseEngine) RenderMCPConfig(yaml *strings.Builder, tools map[string]any, mcpTools []string, workflowData *WorkflowData) error {
	// Default implementation does nothing - engines that support MCP should override this
	return nil
}

// GetAgentManifestFiles returns nil by default (no engine-specific manifest files).
// Engines with dedicated instruction files (e.g. CLAUDE.md, AGENTS.md) should override this.
func (e *BaseEngine) GetAgentManifestFiles() []string {
	return nil
}

// GetAgentManifestPathPrefixes returns nil by default (no engine-specific config directories).
// Engines with a dedicated config directory (e.g. .claude/) should override this.
func (e *BaseEngine) GetAgentManifestPathPrefixes() []string {
	return nil
}

// RenderConfig returns nil by default — engines that need to write config files before
// execution (e.g. provider/model config files) should override this method.
func (e *BaseEngine) RenderConfig(_ *ResolvedEngineTarget) ([]map[string]any, error) {
	return nil, nil
}

// EngineRegistry manages available agentic engines
type EngineRegistry struct {
	engines map[string]CodingAgentEngine

	// Cached results for GetAllAgentManifestFiles and GetAllAgentManifestFolders.
	// These are computed once on first call and never change after engine registration.
	cachedManifestFiles   []string
	cachedManifestFolders []string
}

var (
	globalRegistry   *EngineRegistry
	registryInitOnce sync.Once
)

// NewEngineRegistry creates a new engine registry with built-in engines.
// Panics on invalid built-in engine registration.
func NewEngineRegistry() *EngineRegistry {
	agenticEngineLog.Print("Creating new engine registry")

	registry := &EngineRegistry{
		engines: make(map[string]CodingAgentEngine),
	}

	// Register built-in engines
	builtins := []CodingAgentEngine{
		NewClaudeEngine(),
		NewCodexEngine(),
		NewCopilotEngine(),
		NewGeminiEngine(),
		NewPiEngine(),
	}
	for _, engine := range builtins {
		if err := registry.Register(engine); err != nil {
			// Build-time invariant: Register only rejects a negative
			// dedicatedLLMGatewayPort, and every built-in engine above is constructed
			// with a compile-time constant port, so this is a programming error caught
			// by CI (TestBuiltinEnginesRegisterWithoutError), never user input.
			panic(fmt.Sprintf("BUG: failed to register built-in engine: %v", err))
		}
	}

	agenticEngineLog.Printf("Registered %d engines", len(registry.engines))

	// Pre-compute and cache the manifest file/folder lists now that all engines are
	// registered. These lists are derived solely from the registered engine set, which
	// is fixed after construction.  Pre-computing here guarantees thread-safe access:
	// callers that read the cached slices later see a fully-initialised, immutable
	// value and never trigger concurrent writes.
	registry.cachedManifestFolders = registry.computeAllAgentManifestFolders()
	registry.cachedManifestFiles = registry.computeAllAgentManifestFiles()

	return registry
}

// GetGlobalEngineRegistry returns the singleton engine registry
func GetGlobalEngineRegistry() *EngineRegistry {
	registryInitOnce.Do(func() {
		globalRegistry = NewEngineRegistry()
	})
	return globalRegistry
}

// Register adds an engine to the registry. It returns an error if the engine
// has an invalid configuration (e.g., dedicatedLLMGatewayPort < 0).
func (r *EngineRegistry) Register(engine CodingAgentEngine) error {
	type portProvider interface{ getDedicatedLLMGatewayPort() int }
	if p, ok := engine.(portProvider); ok && p.getDedicatedLLMGatewayPort() < 0 {
		return fmt.Errorf("engine '%s': dedicatedLLMGatewayPort must be >= 0, got %d; expected a non-negative port number or 0 to disable the dedicated gateway", engine.GetID(), p.getDedicatedLLMGatewayPort())
	}
	agenticEngineLog.Printf("Registering engine: id=%s, name=%s", engine.GetID(), engine.GetDisplayName())
	r.engines[engine.GetID()] = engine
	// Invalidate the pre-computed manifest caches so engines registered after
	// construction (e.g. behavior-defined engines imported from shared workflows)
	// contribute their manifest files and folders.
	r.cachedManifestFolders = nil
	r.cachedManifestFiles = nil
	return nil
}

// GetEngine retrieves an engine by ID
func (r *EngineRegistry) GetEngine(id string) (CodingAgentEngine, error) {
	agenticEngineLog.Printf("Looking up engine: id=%s", id)
	engine, exists := r.engines[id]
	if !exists {
		agenticEngineLog.Printf("Engine not found: id=%s", id)
		return nil, fmt.Errorf("unknown engine: %s", id)
	}
	agenticEngineLog.Printf("Found engine: id=%s, name=%s", id, engine.GetDisplayName())
	return engine, nil
}

// EnginesWithCapability returns a sorted list of engine IDs for which the given capability
// predicate returns true. It is used to build accurate, registry-driven lists of supported
// engines in error messages and documentation so those lists stay correct as engines evolve.
func (r *EngineRegistry) EnginesWithCapability(predicate func(EngineCapabilities) bool) []string {
	var ids []string
	for id, engine := range r.engines {
		if predicate(engine.GetCapabilities()) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// GetSupportedEngines returns a list of all supported engine IDs
func (r *EngineRegistry) GetSupportedEngines() []string {
	agenticEngineLog.Print("Getting list of supported engines")
	engines := sliceutil.SortedKeys(r.engines)
	return engines
}

// IsValidEngine checks if an engine ID is valid
func (r *EngineRegistry) IsValidEngine(id string) bool {
	_, exists := r.engines[id]
	return exists
}

// GetDefaultEngine returns the default engine configured by constants.DefaultEngine
func (r *EngineRegistry) GetDefaultEngine() CodingAgentEngine {
	return r.engines[string(constants.DefaultEngine)]
}

// GetAllAgentManifestFolders returns the union of all engines' GetAgentManifestPathPrefixes()
// with trailing slashes stripped, plus ".agents" as the gh-aw platform agent directory.
// The returned list is sorted and deduplicated, making the engine implementations the
// single source of truth for which directories the save/restore scripts protect.
//
// When created via NewEngineRegistry the result is pre-computed at construction time
// so subsequent calls are allocation-free.  Registries created directly (e.g. in tests)
// fall back to computing on demand.
func (r *EngineRegistry) GetAllAgentManifestFolders() []string {
	if r.cachedManifestFolders != nil {
		return r.cachedManifestFolders
	}
	return r.computeAllAgentManifestFolders()
}

// computeAllAgentManifestFolders computes the manifest folders list from the registered engines.
// Called once during NewEngineRegistry to populate cachedManifestFolders.
func (r *EngineRegistry) computeAllAgentManifestFolders() []string {
	seen := map[string]struct {
	}{}
	var result []string
	for _, engine := range r.engines {
		provider, ok := engine.(AgentFileProvider)
		if !ok {
			continue
		}
		for _, prefix := range provider.GetAgentManifestPathPrefixes() {
			folder := strings.TrimSuffix(prefix, "/")
			if folder != "" && !setutil.Contains(seen, folder) {
				seen[folder] = struct {
				}{}
				result = append(result, folder)
			}
		}
	}
	// Always include .agents — the gh-aw platform agent directory.
	// It is not owned by any specific engine but must always be snapshotted.
	if !setutil.Contains(seen, ".agents") {
		result = append(result, ".agents")
	}
	sort.Strings(result)
	return result
}

// computeAllAgentManifestFiles computes the manifest files list from the registered engines.
// Called once during NewEngineRegistry to populate cachedManifestFiles.
func (r *EngineRegistry) computeAllAgentManifestFiles() []string {
	seen := map[string]struct {
	}{}
	var result []string
	for _, engine := range r.engines {
		provider, ok := engine.(AgentFileProvider)
		if !ok {
			continue
		}
		for _, file := range provider.GetAgentManifestFiles() {
			if !setutil.Contains(seen, file) {
				seen[file] = struct {
				}{}
				result = append(result, file)
			}
		}
	}
	sort.Strings(result)
	return result
}

// GetEngineByPrefix returns an engine that matches the given prefix
// This is useful for backward compatibility with strings like "codex-experimental"
func (r *EngineRegistry) GetEngineByPrefix(prefix string) (CodingAgentEngine, error) {
	agenticEngineLog.Printf("Looking up engine by prefix: %s", prefix)
	type engineCandidate struct {
		id     string
		engine CodingAgentEngine
	}
	var candidates []engineCandidate
	for id, engine := range r.engines {
		if strings.HasPrefix(prefix, id) {
			candidates = append(candidates, engineCandidate{id, engine})
		}
	}
	if len(candidates) == 0 {
		agenticEngineLog.Printf("No engine found matching prefix: %s", prefix)
		return nil, fmt.Errorf("no engine found matching prefix: %s", prefix)
	}
	slices.SortFunc(candidates, func(a, b engineCandidate) int {
		switch {
		case a.id < b.id:
			return -1
		case a.id > b.id:
			return 1
		default:
			return 0
		}
	})
	agenticEngineLog.Printf("Found %d engine candidate(s) for prefix %s, using: %s", len(candidates), prefix, candidates[0].id)
	return candidates[0].engine, nil
}

// resolveStepTimeoutValue returns the timeout value string to emit on an
// agentic_execution step.  It never resolves job-level overrides: the agent job
// timeout is bounded independently by resolveAgentJobTimeoutValue.
func resolveStepTimeoutValue(workflowData *WorkflowData) string {
	defaultValue := strconv.Itoa(int(constants.DefaultAgenticWorkflowTimeout / time.Minute))
	if workflowData == nil {
		return defaultValue
	}
	// The detection step is the only step of the detection job, so it shares that
	// job's budget instead of the top-level agentic step timeout.
	if workflowData.IsDetectionRun {
		return resolveDetectionJobTimeoutValue(workflowData)
	}
	if workflowData.ParsedFrontmatter != nil && workflowData.ParsedFrontmatter.TimeoutMinutes != nil {
		if v := workflowData.ParsedFrontmatter.TimeoutMinutes.String(); v != "" {
			return v
		}
	}
	if raw := strings.TrimSpace(workflowData.TimeoutMinutes); raw != "" {
		if after, ok := strings.CutPrefix(raw, "timeout-minutes:"); ok {
			raw = strings.TrimSpace(after)
		}
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return raw
		}
		if isExpression(raw) {
			return raw
		}
		if raw != "" {
			agenticEngineLog.Printf("resolveStepTimeoutValue: ignoring non-integer, non-expression timeout-minutes %q; using default %s", raw, defaultValue)
		}
	}
	return defaultValue
}
