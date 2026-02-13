package workflow

import (
	"fmt"
	"strings"
	"sync"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/goccy/go-yaml"
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
//   ├── SupportsToolsAllowlist()
//   ├── SupportsHTTPTransport()
//   ├── SupportsMaxTurns()
//   ├── SupportsWebFetch()
//   ├── SupportsWebSearch()
//   └── SupportsFirewall()
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
}

// CapabilityProvider detects what capabilities an engine supports
// Engines can optionally implement this to indicate feature support
type CapabilityProvider interface {
	// SupportsToolsAllowlist returns true if this engine supports MCP tool allow-listing
	SupportsToolsAllowlist() bool

	// SupportsHTTPTransport returns true if this engine supports HTTP transport for MCP servers
	SupportsHTTPTransport() bool

	// SupportsMaxTurns returns true if this engine supports the max-turns feature
	SupportsMaxTurns() bool

	// SupportsWebFetch returns true if this engine has built-in support for the web-fetch tool
	SupportsWebFetch() bool

	// SupportsWebSearch returns true if this engine has built-in support for the web-search tool
	SupportsWebSearch() bool

	// SupportsFirewall returns true if this engine supports network firewalling/sandboxing
	// When true, the engine can enforce network restrictions defined in the workflow
	SupportsFirewall() bool

	// SupportsPlugins returns true if this engine supports plugin installation
	// When true, plugins can be installed using the engine's plugin install command
	SupportsPlugins() bool

	// SupportsLLMGateway returns true if this engine supports an LLM gateway
	// When true, the engine can use a gateway service to proxy LLM API calls
	// In strict mode, engines without LLM gateway support require additional security constraints
	SupportsLLMGateway() bool
}

// WorkflowExecutor handles workflow compilation and execution
// All engines must implement this to generate GitHub Actions steps
type WorkflowExecutor interface {
	// GetDeclaredOutputFiles returns a list of output files that this engine may produce
	// These files will be automatically uploaded as artifacts if they exist
	GetDeclaredOutputFiles() []string

	// GetInstallationSteps returns the GitHub Actions steps needed to install this engine
	GetInstallationSteps(workflowData *WorkflowData) []GitHubActionStep

	// GetExecutionSteps returns the GitHub Actions steps for executing this engine
	GetExecutionSteps(workflowData *WorkflowData, logFile string) []GitHubActionStep
}

// MCPConfigProvider handles MCP (Model Context Protocol) configuration
// Engines that support MCP servers should implement this
type MCPConfigProvider interface {
	// RenderMCPConfig renders the MCP configuration for this engine to the given YAML builder
	RenderMCPConfig(yaml *strings.Builder, tools map[string]any, mcpTools []string, workflowData *WorkflowData)
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
}

// SecurityProvider handles security-related configuration
// Engines can optionally implement this to provide security features
type SecurityProvider interface {
	// GetDefaultDetectionModel returns the default model to use for threat detection
	// If empty, no default model is applied and the engine uses its standard default
	GetDefaultDetectionModel() string

	// GetRequiredSecretNames returns the list of secret names that this engine needs for execution
	// This includes engine-specific auth tokens and the MCP gateway API key when MCP servers are present
	// Returns: slice of secret names (e.g., ["COPILOT_GITHUB_TOKEN", "MCP_GATEWAY_API_KEY"])
	GetRequiredSecretNames(workflowData *WorkflowData) []string
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
}

// BaseEngine provides common functionality for agentic engines
type BaseEngine struct {
	id                     string
	displayName            string
	description            string
	experimental           bool
	supportsToolsAllowlist bool
	supportsHTTPTransport  bool
	supportsMaxTurns       bool
	supportsWebFetch       bool
	supportsWebSearch      bool
	supportsFirewall       bool
	supportsPlugins        bool
	supportsLLMGateway     bool
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

func (e *BaseEngine) SupportsToolsAllowlist() bool {
	return e.supportsToolsAllowlist
}

func (e *BaseEngine) SupportsHTTPTransport() bool {
	return e.supportsHTTPTransport
}

func (e *BaseEngine) SupportsMaxTurns() bool {
	return e.supportsMaxTurns
}

func (e *BaseEngine) SupportsWebFetch() bool {
	return e.supportsWebFetch
}

func (e *BaseEngine) SupportsWebSearch() bool {
	return e.supportsWebSearch
}

func (e *BaseEngine) SupportsFirewall() bool {
	return e.supportsFirewall
}

func (e *BaseEngine) SupportsPlugins() bool {
	return e.supportsPlugins
}

func (e *BaseEngine) SupportsLLMGateway() bool {
	return e.supportsLLMGateway
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

// GetLogFileForParsing returns the default log file path for parsing
// Engines can override this to use engine-specific log files
func (e *BaseEngine) GetLogFileForParsing() string {
	// Default to agent-stdio.log which contains stdout/stderr
	return "/tmp/gh-aw/agent-stdio.log"
}

// GetRequiredSecretNames returns an empty list by default
// Engines must override this to specify their required secrets
func (e *BaseEngine) GetRequiredSecretNames(workflowData *WorkflowData) []string {
	return []string{}
}

// convertStepToYAML converts a step map to YAML string - uses proper YAML serialization
// This is a shared implementation inherited by all engines that embed BaseEngine
func (e *BaseEngine) convertStepToYAML(stepMap map[string]any) (string, error) {
	return ConvertStepToYAML(stepMap)
}

// EngineRegistry manages available agentic engines
type EngineRegistry struct {
	engines map[string]CodingAgentEngine
}

var (
	globalRegistry   *EngineRegistry
	registryInitOnce sync.Once
)

// NewEngineRegistry creates a new engine registry with built-in engines
func NewEngineRegistry() *EngineRegistry {
	agenticEngineLog.Print("Creating new engine registry")

	registry := &EngineRegistry{
		engines: make(map[string]CodingAgentEngine),
	}

	// Register built-in engines
	registry.Register(NewClaudeEngine())
	registry.Register(NewCodexEngine())
	registry.Register(NewCopilotEngine())
	registry.Register(NewCopilotSDKEngine())
	registry.Register(NewCustomEngine())

	agenticEngineLog.Printf("Registered %d engines", len(registry.engines))
	return registry
}

// GetGlobalEngineRegistry returns the singleton engine registry
func GetGlobalEngineRegistry() *EngineRegistry {
	registryInitOnce.Do(func() {
		globalRegistry = NewEngineRegistry()
	})
	return globalRegistry
}

// Register adds an engine to the registry
func (r *EngineRegistry) Register(engine CodingAgentEngine) {
	agenticEngineLog.Printf("Registering engine: id=%s, name=%s", engine.GetID(), engine.GetDisplayName())
	r.engines[engine.GetID()] = engine
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

// GetSupportedEngines returns a list of all supported engine IDs
func (r *EngineRegistry) GetSupportedEngines() []string {
	var engines []string
	for id := range r.engines {
		engines = append(engines, id)
	}
	return engines
}

// IsValidEngine checks if an engine ID is valid
func (r *EngineRegistry) IsValidEngine(id string) bool {
	_, exists := r.engines[id]
	return exists
}

// GetDefaultEngine returns the default engine (Copilot)
func (r *EngineRegistry) GetDefaultEngine() CodingAgentEngine {
	return r.engines["copilot"]
}

// GetEngineByPrefix returns an engine that matches the given prefix
// This is useful for backward compatibility with strings like "codex-experimental"
func (r *EngineRegistry) GetEngineByPrefix(prefix string) (CodingAgentEngine, error) {
	for id, engine := range r.engines {
		if strings.HasPrefix(prefix, id) {
			return engine, nil
		}
	}
	return nil, fmt.Errorf("no engine found matching prefix: %s", prefix)
}

// GenerateSecretValidationStep creates a GitHub Actions step that validates required secrets are available
// secretName: the name of the secret to validate (e.g., "ANTHROPIC_API_KEY")
// engineName: the display name of the engine (e.g., "Claude Code")
// docsURL: URL to the documentation page for setting up the secret
func GenerateSecretValidationStep(secretName, engineName, docsURL string) GitHubActionStep {
	stepLines := []string{
		fmt.Sprintf("      - name: Validate %s secret", secretName),
		"        run: |",
		fmt.Sprintf("          if [ -z \"$%s\" ]; then", secretName),
		fmt.Sprintf("            echo \"Error: %s secret is not set\"", secretName),
		fmt.Sprintf("            echo \"The %s engine requires the %s secret to be configured.\"", engineName, secretName),
		"            echo \"Please configure this secret in your repository settings.\"",
		fmt.Sprintf("            echo \"Documentation: %s\"", docsURL),
		"            exit 1",
		"          fi",
		"          ",
		"          # Log success in collapsible section",
		"          echo \"<details>\"",
		"          echo \"<summary>Agent Environment Validation</summary>\"",
		"          echo \"\"",
		fmt.Sprintf("          echo \"✅ %s: Configured\"", secretName),
		"          echo \"</details>\"",
		"        env:",
		fmt.Sprintf("          %s: ${{ secrets.%s }}", secretName, secretName),
	}
	return GitHubActionStep(stepLines)
}

// GenerateMultiSecretValidationStep creates a GitHub Actions step that validates at least one of multiple secrets is available
// secretNames: slice of secret names to validate (e.g., []string{"CODEX_API_KEY", "OPENAI_API_KEY"})
// engineName: the display name of the engine (e.g., "Codex")
// docsURL: URL to the documentation page for setting up the secret
func GenerateMultiSecretValidationStep(secretNames []string, engineName, docsURL string) GitHubActionStep {
	if len(secretNames) == 0 {
		// This is a programming error - engine configurations should always provide secrets
		// Log the error and return empty step to avoid breaking compilation
		agenticEngineLog.Printf("ERROR: GenerateMultiSecretValidationStep called with empty secretNames for engine %s", engineName)
		return GitHubActionStep{}
	}

	// Build the step name
	stepName := fmt.Sprintf("      - name: Validate %s secret", strings.Join(secretNames, " or "))

	// Build the command to call the validation script
	// The script expects: SECRET_NAME1 [SECRET_NAME2 ...] ENGINE_NAME DOCS_URL
	// Use shellJoinArgs to properly escape multi-word engine names and special characters
	scriptArgs := append(secretNames, engineName, docsURL)
	scriptArgsStr := shellJoinArgs(scriptArgs)

	stepLines := []string{
		stepName,
		"        id: validate-secret",
		"        run: /opt/gh-aw/actions/validate_multi_secret.sh " + scriptArgsStr,
		"        env:",
	}

	// Add env section with all secrets
	for _, secretName := range secretNames {
		stepLines = append(stepLines, fmt.Sprintf("          %s: ${{ secrets.%s }}", secretName, secretName))
	}

	return GitHubActionStep(stepLines)
}

// GetAllEngines returns all registered engines
func (r *EngineRegistry) GetAllEngines() []CodingAgentEngine {
	var engines []CodingAgentEngine
	for _, engine := range r.engines {
		engines = append(engines, engine)
	}
	return engines
}

// GetCopilotAgentPlaywrightTools returns the list of playwright tools available in the copilot agent
// This matches the tools available in the copilot agent MCP server configuration
// This is a shared function used by all engines for consistent playwright tool configuration
func GetCopilotAgentPlaywrightTools() []any {
	tools := []string{
		"browser_click",
		"browser_close",
		"browser_console_messages",
		"browser_drag",
		"browser_evaluate",
		"browser_file_upload",
		"browser_fill_form",
		"browser_handle_dialog",
		"browser_hover",
		"browser_install",
		"browser_navigate",
		"browser_navigate_back",
		"browser_network_requests",
		"browser_press_key",
		"browser_resize",
		"browser_select_option",
		"browser_snapshot",
		"browser_tabs",
		"browser_take_screenshot",
		"browser_type",
		"browser_wait_for",
	}

	// Convert []string to []any for compatibility with the configuration system
	result := make([]any, len(tools))
	for i, tool := range tools {
		result[i] = tool
	}
	return result
}

// ConvertStepToYAML converts a step map to YAML string with proper indentation
// This is a shared utility function used by all engines and the compiler
func ConvertStepToYAML(stepMap map[string]any) (string, error) {
	// Use OrderMapFields to get ordered MapSlice
	orderedStep := OrderMapFields(stepMap, constants.PriorityStepFields)

	// Wrap in array for step list format and marshal with proper options
	yamlBytes, err := yaml.MarshalWithOptions([]yaml.MapSlice{orderedStep}, DefaultMarshalOptions...)
	if err != nil {
		return "", fmt.Errorf("failed to marshal step to YAML: %w", err)
	}

	// Convert to string and adjust base indentation to match GitHub Actions format
	yamlStr := string(yamlBytes)

	// Post-process to move version comments outside of quoted uses values
	// This handles cases like: uses: "slug@sha # v1"  ->  uses: slug@sha # v1
	yamlStr = unquoteUsesWithComments(yamlStr)

	// Add 6 spaces to the beginning of each line to match GitHub Actions step indentation
	lines := strings.Split(strings.TrimSpace(yamlStr), "\n")
	var result strings.Builder

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			result.WriteString("\n")
		} else {
			result.WriteString("      " + line + "\n")
		}
	}

	return result.String(), nil
}

// unquoteUsesWithComments removes quotes from uses values that contain version comments
// Transforms: uses: "slug@sha # v1"  ->  uses: slug@sha # v1
// This is needed because the YAML marshaller quotes strings containing #, but GitHub Actions
// expects unquoted uses values with inline comments
func unquoteUsesWithComments(yamlStr string) string {
	lines := strings.Split(yamlStr, "\n")
	for i, line := range lines {
		// Look for uses: followed by a quoted string containing a # comment
		// This handles various indentation levels and formats
		trimmed := strings.TrimSpace(line)

		// Check if line contains uses: with a quoted value
		if !strings.Contains(trimmed, "uses: \"") {
			continue
		}

		// Check if the quoted value contains a version comment
		if !strings.Contains(trimmed, " # ") {
			continue
		}

		// Find the position of uses: " in the original line
		usesIdx := strings.Index(line, "uses: \"")
		if usesIdx == -1 {
			continue
		}

		// Extract the part before uses: (indentation)
		prefix := line[:usesIdx]

		// Find the opening and closing quotes
		quoteStart := usesIdx + 7 // len("uses: \"")
		quoteEnd := strings.Index(line[quoteStart:], "\"")
		if quoteEnd == -1 {
			continue
		}
		quoteEnd += quoteStart

		// Extract the quoted content
		quotedContent := line[quoteStart:quoteEnd]

		// Extract any content after the closing quote
		suffix := line[quoteEnd+1:]

		// Reconstruct the line without quotes
		lines[i] = prefix + "uses: " + quotedContent + suffix
	}
	return strings.Join(lines, "\n")
}
