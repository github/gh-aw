# Adding New Agentic Engines to gh-aw

This guide provides instructions for adding a new agentic engine (AI coding agent) to GitHub Agentic Workflows.

## Table of Contents

1. [Architecture Overview](#architecture-overview)
2. [Interface Architecture](#interface-architecture)
3. [Implementation Checklist](#implementation-checklist)
4. [Step-by-Step Guide](#step-by-step-guide)
5. [Testing Requirements](#testing-requirements)
6. [Documentation Requirements](#documentation-requirements)
7. [Examples](#examples)

## Architecture Overview

The agentic engine architecture follows the Interface Segregation Principle (ISP), using focused interfaces composed together rather than a single monolithic interface. This allows engines to implement only the capabilities they support.

### Key Files

- **`pkg/workflow/agentic_engine.go`** - Core interfaces and base engine implementation
- **`pkg/workflow/engine.go`** - Engine configuration parsing and helpers
- **`pkg/workflow/engine_validation.go`** - Engine validation logic
- **`pkg/workflow/engine_helpers.go`** - Shared helper functions
- **`pkg/workflow/<engine>_engine.go`** - Individual engine implementations
- **`pkg/constants/constants.go`** - Engine names and defaults

## Interface Architecture

### Core Interfaces

#### 1. Engine (Required)
Core identity interface - all engines must implement this.

```go
type Engine interface {
    GetID() string           // Unique identifier (e.g., "copilot", "claude")
    GetDisplayName() string  // Human-readable name (e.g., "GitHub Copilot CLI")
    GetDescription() string  // Brief description of capabilities
    IsExperimental() bool    // Experimental status flag
}
```

#### 2. CapabilityProvider (Required via BaseEngine)
Feature detection interface - indicates what capabilities an engine supports.

```go
type EngineCapabilities struct {
    ToolsAllowlist   bool
    MaxTurns         bool
    WebSearch        bool
    MaxContinuations bool
    NativeAgentFile  bool
    BareMode         bool
}

type CapabilityProvider interface {
    GetCapabilities() EngineCapabilities
}
```

#### 3. WorkflowExecutor (Required)
Workflow compilation interface - generates GitHub Actions steps.

```go
type WorkflowExecutor interface {
    GetDeclaredOutputFiles() []string                                    // Output files to collect
    GetInstallationSteps(workflowData *WorkflowData) []GitHubActionStep // Install steps
    GetExecutionSteps(workflowData *WorkflowData, logFile string) []GitHubActionStep // Execution steps
}
```

#### 4. MCPConfigProvider (Required)
MCP server configuration interface - renders MCP server configuration.

```go
type MCPConfigProvider interface {
    RenderMCPConfig(yaml *strings.Builder, tools map[string]any, mcpTools []string, workflowData *WorkflowData)
}
```

#### 5. LogParser (Required via BaseEngine)
Log parsing interface - extracts metrics from engine logs.

```go
type LogParser interface {
    ParseLogMetrics(logContent string, verbose bool) LogMetrics  // Extract metrics
    GetLogParserScriptId() string                                // JavaScript parser ID
    GetLogFileForParsing() string                                // Log file path
}
```

#### 6. SecurityProvider (Required via BaseEngine)
Security configuration interface - manages secrets and detection.

```go
type SecurityProvider interface {
    GetDefaultDetectionModel() string                          // Default model for threat detection
    GetRequiredSecretNames(workflowData *WorkflowData) []string // Required GitHub secrets
}
```

#### 7. CodingAgentEngine (Composite)
Composite interface combining all focused interfaces for backward compatibility.

```go
type CodingAgentEngine interface {
    Engine
    CapabilityProvider
    WorkflowExecutor
    MCPConfigProvider
    LogParser
    SecurityProvider
}
```

### BaseEngine

All engines embed `BaseEngine` which provides default implementations for all interface methods. This allows engines to override only the methods they need to customize.

```go
type MyEngine struct {
    BaseEngine
}
```

## Implementation Checklist

### Phase 1: Core Implementation

- [ ] Create `<engine>_engine.go` file in `pkg/workflow/`
- [ ] Define engine struct embedding `BaseEngine`
- [ ] Implement constructor function `NewMyEngine()`
- [ ] Set engine ID, display name, description, and experimental status
- [ ] Configure `BaseEngine.capabilities` (`ToolsAllowlist`, `MaxTurns`, `WebSearch`, `MaxContinuations`, `NativeAgentFile`, `BareMode`) as needed
- [ ] Add engine constant to `pkg/constants/constants.go` (optional but recommended)
- [ ] Register default firewall domains in `pkg/workflow/domains.go` (see [Firewall Domain Registration](#firewall-domain-registration))

### Phase 2: Installation & Execution

- [ ] Implement `GetInstallationSteps()` for engine setup
- [ ] Implement `GetExecutionSteps()` for agent execution
- [ ] Implement `GetDeclaredOutputFiles()` for artifact collection
- [ ] Handle custom command support (skip installation if `command` is specified)
- [ ] Support custom args, env vars, and configuration fields

### Phase 3: MCP Configuration

- [ ] Implement `RenderMCPConfig()` for MCP server configuration
- [ ] Choose appropriate format (JSON for Copilot/Claude, TOML for Codex)
- [ ] Use unified MCP renderer (`MCPConfigRendererUnified`) for consistency
- [ ] Handle tool-specific configuration (GitHub, Playwright, custom servers)
- [ ] Support safe-outputs and safe-inputs integration

### Phase 4: Security & Secrets

- [ ] Implement `GetRequiredSecretNames()` to declare required secrets
- [ ] Add secret validation steps in `GetInstallationSteps()`
- [ ] Use `GenerateSecretValidationStep()` or `GenerateMultiSecretValidationStep()`
- [ ] Implement `GetDefaultDetectionModel()` for threat detection (optional)
- [ ] Handle MCP gateway API key if MCP servers are present

### Phase 5: Log Parsing

- [ ] Implement `ParseLogMetrics()` to extract metrics from logs
- [ ] Implement `GetLogParserScriptId()` to return JavaScript parser ID
- [ ] Implement `GetLogFileForParsing()` to return log file path (optional - defaults to `/tmp/gh-aw/agent-stdio.log`)
- [ ] Create JavaScript log parser in `actions/setup/js/` (optional)

### Phase 6: Registration & Validation

- [ ] Register engine in `NewEngineRegistry()` in `agentic_engine.go`
- [ ] Add engine ID to documentation in `docs/src/content/docs/reference/engines.md`
- [ ] Update `docs/src/content/docs/reference/custom-engines.md` if needed

### Phase 7: Testing

- [ ] Create `<engine>_engine_test.go` with unit tests
- [ ] Test installation steps generation
- [ ] Test execution steps generation
- [ ] Test MCP configuration rendering
- [ ] Test secret validation
- [ ] Test capability detection
- [ ] Add integration tests if needed
- [ ] Add a compile-time interface assertion (for example, `var _ CodingAgentEngine = NewMyEngine()`)

### Phase 8: Documentation

- [ ] Add engine documentation to `docs/src/content/docs/reference/engines.md`
- [ ] Document required secrets and setup steps
- [ ] Provide example configurations
- [ ] Document custom configuration options (version, model, args, env, etc.)
- [ ] Update AGENTS.md with engine-specific guidelines (if needed)

## Step-by-Step Guide

### Step 1: Create Engine File

Create a new file `pkg/workflow/<engine>_engine.go`:

```go
package workflow

import (
    "github.com/github/gh-aw/pkg/constants"
    "github.com/github/gh-aw/pkg/logger"
)

var myEngineLog = logger.New("workflow:my_engine")

// MyEngine represents the My AI agentic engine
type MyEngine struct {
    BaseEngine
}

func NewMyEngine() *MyEngine {
    return &MyEngine{
        BaseEngine: BaseEngine{
            id:           "my-engine",
            displayName:  "My AI Engine",
            description:  "Uses My AI with MCP server support",
            experimental: false, // Set to true for experimental engines
            capabilities: EngineCapabilities{
                ToolsAllowlist:   true,
                MaxTurns:         true,
                WebSearch:        true,
                MaxContinuations: false,
                NativeAgentFile:  false,
                BareMode:         false,
            },
        },
    }
}
```

### Step 2: Implement Required Secrets

Override `GetRequiredSecretNames()` to declare required secrets:

```go
// GetRequiredSecretNames returns the list of secrets required by the engine
func (e *MyEngine) GetRequiredSecretNames(workflowData *WorkflowData) []string {
    secrets := []string{"MY_ENGINE_API_KEY"}

    // Add MCP gateway API key if MCP servers are present
    if HasMCPServers(workflowData) {
        secrets = append(secrets, "MCP_GATEWAY_API_KEY")
    }

    // Add safe-inputs secret names
    if IsSafeInputsEnabled(workflowData.SafeInputs, workflowData) {
        safeInputsSecrets := collectSafeInputsSecrets(workflowData.SafeInputs)
        for varName := range safeInputsSecrets {
            secrets = append(secrets, varName)
        }
    }

    return secrets
}
```

### Step 3: Implement Installation Steps

Override `GetInstallationSteps()` to generate installation steps:

```go
func (e *MyEngine) GetInstallationSteps(workflowData *WorkflowData) []GitHubActionStep {
    myEngineLog.Printf("Generating installation steps: workflow=%s", workflowData.Name)

    // Skip installation if custom command is specified
    if workflowData.EngineConfig != nil && workflowData.EngineConfig.Command != "" {
        myEngineLog.Printf("Skipping installation: custom command specified (%s)", workflowData.EngineConfig.Command)
        return []GitHubActionStep{}
    }

    var steps []GitHubActionStep

    // Add secret validation step
    secretValidation := GenerateMultiSecretValidationStep(
        []string{"MY_ENGINE_API_KEY"},
        "My AI Engine",
        "https://github.github.com/gh-aw/reference/engines/#my-ai-engine",
    )
    steps = append(steps, secretValidation)

    // Determine engine version
    version := "1.0.0" // Default version
    if workflowData.EngineConfig != nil && workflowData.EngineConfig.Version != "" {
        version = workflowData.EngineConfig.Version
    }

    // Add installation steps (example: npm package)
    npmSteps := GenerateNpmInstallSteps(
        "@my-company/my-engine",
        version,
        "Install My AI Engine",
        "my-engine",
        true, // Include Node.js setup
    )
    steps = append(steps, npmSteps...)

    // Add firewall installation if enabled
    if isFirewallEnabled(workflowData) {
        firewallConfig := getFirewallConfig(workflowData)
        agentConfig := getAgentConfig(workflowData)
        var awfVersion string
        if firewallConfig != nil {
            awfVersion = firewallConfig.Version
        }

        awfInstall := generateAWFInstallationStep(awfVersion, agentConfig)
        if len(awfInstall) > 0 {
            steps = append(steps, awfInstall)
        }
    }

    return steps
}
```

### Step 4: Implement Execution Steps

Override `GetExecutionSteps()` to generate execution steps:

```go
func (e *MyEngine) GetExecutionSteps(workflowData *WorkflowData, logFile string) []GitHubActionStep {
    myEngineLog.Printf("Generating execution steps: workflow=%s", workflowData.Name)

    var steps []GitHubActionStep

    // Build command
    commandName := "my-engine"
    if workflowData.EngineConfig != nil && workflowData.EngineConfig.Command != "" {
        commandName = workflowData.EngineConfig.Command
    }

    // Build arguments
    var args []string
    args = append(args, "--prompt", "/tmp/gh-aw/aw-prompts/prompt.txt")

    // Add model if specified
    if workflowData.EngineConfig != nil && workflowData.EngineConfig.Model != "" {
        args = append(args, "--model", workflowData.EngineConfig.Model)
    }

    // Add MCP config if MCP servers are present
    if HasMCPServers(workflowData) {
        args = append(args, "--mcp-config", "/tmp/gh-aw/mcp-config/mcp-servers.json")
    }

    // Add custom args
    if workflowData.EngineConfig != nil && len(workflowData.EngineConfig.Args) > 0 {
        args = append(args, workflowData.EngineConfig.Args...)
    }

    // Build command string
    commandParts := append([]string{commandName}, args...)
    command := shellJoinArgs(commandParts)

    // Add logging
    command = fmt.Sprintf("%s 2>&1 | tee %s", command, logFile)

    // Build environment variables
    env := map[string]string{
        "MY_ENGINE_API_KEY": "${{ secrets.MY_ENGINE_API_KEY }}",
        "GH_AW_PROMPT":      "/tmp/gh-aw/aw-prompts/prompt.txt",
    }

    // Add GH_AW_SAFE_OUTPUTS if needed
    applySafeOutputEnvToMap(env, workflowData)

    // Add custom env vars
    if workflowData.EngineConfig != nil && len(workflowData.EngineConfig.Env) > 0 {
        for key, value := range workflowData.EngineConfig.Env {
            env[key] = value
        }
    }

    // Generate step
    var stepLines []string
    stepLines = append(stepLines, "      - name: Execute My AI Engine")
    stepLines = append(stepLines, "        id: agentic_execution")

    // Filter environment variables for security
    allowedSecrets := e.GetRequiredSecretNames(workflowData)
    filteredEnv := FilterEnvForSecrets(env, allowedSecrets)

    // Format step with command and filtered environment variables
    stepLines = FormatStepWithCommandAndEnv(stepLines, command, filteredEnv)

    steps = append(steps, GitHubActionStep(stepLines))

    return steps
}
```

### Step 5: Implement MCP Configuration

Override `RenderMCPConfig()` to render MCP server configuration:

```go
func (e *MyEngine) RenderMCPConfig(yaml *strings.Builder, tools map[string]any, mcpTools []string, workflowData *WorkflowData) {
    myEngineLog.Print("Rendering MCP configuration")

    // Use unified renderer with engine-specific options
    createRenderer := func(isLast bool) *MCPConfigRendererUnified {
        return NewMCPConfigRenderer(MCPRendererOptions{
            IncludeCopilotFields: false, // Set based on engine requirements
            InlineArgs:           false, // Set based on engine format preferences
            Format:               "json", // or "toml" based on engine
            IsLast:               isLast,
            ActionMode:           GetActionModeFromWorkflowData(workflowData),
        })
    }

    // Use shared JSON MCP config renderer
    _ = RenderJSONMCPConfig(yaml, tools, mcpTools, workflowData, JSONMCPConfigOptions{
        ConfigPath:    "/tmp/gh-aw/mcp-config/mcp-servers.json",
        GatewayConfig: buildMCPGatewayConfig(workflowData),
        Renderers: MCPToolRenderers{
            RenderGitHub: func(yaml *strings.Builder, githubTool any, isLast bool, workflowData *WorkflowData) {
                renderer := createRenderer(isLast)
                renderer.RenderGitHubMCP(yaml, githubTool, workflowData)
            },
            RenderPlaywright: func(yaml *strings.Builder, playwrightTool any, isLast bool) {
                renderer := createRenderer(isLast)
                renderer.RenderPlaywrightMCP(yaml, playwrightTool)
            },
            // ... other renderers
            RenderCustomMCPConfig: func(yaml *strings.Builder, toolName string, toolConfig map[string]any, isLast bool) error {
                return renderCustomMCPConfigWrapperWithContext(yaml, toolName, toolConfig, isLast, workflowData)
            },
        },
    })
}
```

### Step 6: Implement Log Parsing (Optional)

Override log parsing methods if needed:

```go
func (e *MyEngine) ParseLogMetrics(logContent string, verbose bool) LogMetrics {
    myEngineLog.Printf("Parsing log metrics: log_size=%d bytes", len(logContent))

    var metrics LogMetrics
    // Parse engine-specific log format
    // Extract turns, token usage, errors, etc.
    return metrics
}

func (e *MyEngine) GetLogParserScriptId() string {
    return "parse_my_engine_log"
}

func (e *MyEngine) GetLogFileForParsing() string {
    return "/tmp/gh-aw/my-engine/debug.log"
}
```

### Step 7: Register Engine

Add engine registration in `pkg/workflow/agentic_engine.go`:

```go
func NewEngineRegistry() *EngineRegistry {
    registry := &EngineRegistry{
        engines: make(map[string]CodingAgentEngine),
    }

    // Register built-in engines
    registry.Register(NewClaudeEngine())
    registry.Register(NewCodexEngine())
    registry.Register(NewCopilotEngine())
    registry.Register(NewCustomEngine())
    registry.Register(NewMyEngine()) // Add your engine here

    return registry
}
```

### Step 8: Add Engine Constant (Optional)

Add engine constant to `pkg/constants/constants.go`:

```go
const (
    // ... existing constants
    MyEngineID EngineName = "my-engine"
)
```

### Step 9: Add Tests

Create `pkg/workflow/<engine>_engine_test.go`:

```go
//go:build !integration

package workflow

import (
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestMyEngineBasicProperties(t *testing.T) {
    engine := NewMyEngine()

    assert.Equal(t, "my-engine", engine.GetID())
    assert.Equal(t, "My AI Engine", engine.GetDisplayName())
    assert.NotEmpty(t, engine.GetDescription())
}

func TestMyEngineCapabilities(t *testing.T) {
    engine := NewMyEngine()
    capabilities := engine.GetCapabilities()

    // Test capability flags match constructor
    assert.True(t, capabilities.ToolsAllowlist)
    assert.True(t, capabilities.MaxTurns)
    assert.True(t, capabilities.WebSearch)
    assert.False(t, capabilities.MaxContinuations)
    assert.False(t, capabilities.NativeAgentFile)
    assert.False(t, capabilities.BareMode)
}

func TestMyEngineInstallationSteps(t *testing.T) {
    engine := NewMyEngine()
    workflowData := &WorkflowData{
        Name:        "test-workflow",
        ParsedTools: &ToolsConfig{},
    }

    steps := engine.GetInstallationSteps(workflowData)
    assert.NotNil(t, steps)
    assert.NotEmpty(t, steps) // Should have at least secret validation
}

func TestMyEngineExecutionSteps(t *testing.T) {
    engine := NewMyEngine()
    workflowData := &WorkflowData{
        Name:        "test-workflow",
        ParsedTools: &ToolsConfig{},
    }

    steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")
    assert.NotNil(t, steps)
    assert.NotEmpty(t, steps)
}

func TestMyEngineRequiredSecrets(t *testing.T) {
    engine := NewMyEngine()
    workflowData := &WorkflowData{
        ParsedTools: &ToolsConfig{},
    }

    secrets := engine.GetRequiredSecretNames(workflowData)
    require.NotNil(t, secrets)
    assert.Contains(t, secrets, "MY_ENGINE_API_KEY")
}
```

### Step 10: Update Documentation

Update `docs/src/content/docs/reference/engines.md`:

```markdown
## Using My AI Engine

To use My AI Engine:

1. Request the use of My AI Engine in your workflow frontmatter:

   ```yaml wrap
   engine: my-engine
   ```

2. Configure `MY_ENGINE_API_KEY` GitHub Actions secret.

   Create an API key and add it to your repository:

   ```bash wrap
   gh aw secrets set MY_ENGINE_API_KEY --value "<your-api-key>"
   ```

### Extended Configuration

```yaml wrap
engine:
  id: my-engine
  version: 1.0.0
  model: default-model
  args: ["--verbose"]
  env:
    DEBUG: "true"
```
```

## Testing Requirements

### Unit Tests

Every engine must have unit tests covering:

1. **Basic properties**: ID, display name, description
2. **Capability flags**: All `Supports*()` methods
3. **Installation steps**: Generate valid GitHub Actions steps
4. **Execution steps**: Generate valid GitHub Actions steps with proper env vars
5. **Required secrets**: Return correct secret names
6. **MCP configuration**: Render valid MCP config (if MCP servers are used)
7. **Log parsing**: Extract metrics correctly (if custom parser implemented)

### Integration Tests

Integration tests should validate:

1. **Workflow compilation**: Engine workflows compile to valid `.lock.yml` files
2. **Secret validation**: Missing secrets are caught during installation
3. **Custom configuration**: Version, model, args, env vars are applied correctly
4. **MCP integration**: MCP servers are configured properly
5. **Safe-outputs integration**: Output handlers work correctly

### Interface Compliance Tests

Add a small compile-time assertion in your engine test file to ensure the constructor still returns a `CodingAgentEngine`:

```go
func TestMyEngine_ImplementsCodingAgentEngine(t *testing.T) {
    var _ CodingAgentEngine = NewMyEngine()
}
```

## Documentation Requirements

### Reference Documentation

Update `docs/src/content/docs/reference/engines.md`:

1. Add section for new engine with setup instructions
2. Document required secrets and how to obtain them
3. Provide example configurations (basic and extended)
4. Document engine-specific features and limitations

### Code Documentation

Ensure all public methods have godoc comments:

```go
// GetInstallationSteps returns the GitHub Actions steps needed to install My AI Engine
// Includes secret validation and npm package installation
func (e *MyEngine) GetInstallationSteps(workflowData *WorkflowData) []GitHubActionStep {
    // ...
}
```

### AGENTS.md Updates

If the engine has specific guidelines or patterns, add to `AGENTS.md`:

```markdown
### My AI Engine

**Default version**: 1.0.0
**Required secrets**: MY_ENGINE_API_KEY
**Capabilities**: Tools allowlist, max turns, web search

**Example usage**:
```yaml
engine: my-engine
tools:
  github:
    toolsets: [repos, issues]
```
```

## Examples

### Minimal Engine (No MCP Support)

```go
type SimpleEngine struct {
    BaseEngine
}

func NewSimpleEngine() *SimpleEngine {
    return &SimpleEngine{
        BaseEngine: BaseEngine{
            id:           "simple",
            displayName:  "Simple Engine",
            description:  "Basic execution without MCP",
            experimental: false,
            capabilities: EngineCapabilities{
                ToolsAllowlist: false,
                MaxTurns:       false,
                WebSearch:      false,
            },
        },
    }
}

func (e *SimpleEngine) GetRequiredSecretNames(workflowData *WorkflowData) []string {
    return []string{"SIMPLE_API_KEY"}
}

func (e *SimpleEngine) GetInstallationSteps(workflowData *WorkflowData) []GitHubActionStep {
    return []GitHubActionStep{
        GenerateSecretValidationStep("SIMPLE_API_KEY", "Simple Engine", "https://docs.example.com"),
    }
}

func (e *SimpleEngine) GetExecutionSteps(workflowData *WorkflowData, logFile string) []GitHubActionStep {
    command := fmt.Sprintf("simple-cli --prompt /tmp/gh-aw/aw-prompts/prompt.txt 2>&1 | tee %s", logFile)
    env := map[string]string{
        "SIMPLE_API_KEY": "${{ secrets.SIMPLE_API_KEY }}",
    }

    var stepLines []string
    stepLines = append(stepLines, "      - name: Execute Simple Engine")
    stepLines = FormatStepWithCommandAndEnv(stepLines, command, env)

    return []GitHubActionStep{GitHubActionStep(stepLines)}
}

func (e *SimpleEngine) RenderMCPConfig(yaml *strings.Builder, tools map[string]any, mcpTools []string, workflowData *WorkflowData) {
    // No MCP support - leave empty
}
```

### Full-Featured Engine (With MCP and additional capabilities)

See existing implementations:
- **Copilot**: MCP allowlist, max continuations, bare mode - `copilot_engine.go`
- **Claude**: MCP allowlist, max turns, web search, bare mode - `claude_engine.go`
- **Codex**: MCP allowlist, web search, TOML config - `codex_engine.go`

## Common Patterns

### npm Package Installation

Use `GenerateNpmInstallSteps()` for npm-based engines:

```go
npmSteps := GenerateNpmInstallSteps(
    "@company/engine",
    version,
    "Install Engine",
    "engine-cli",
    true, // Include Node.js setup
)
steps = append(steps, npmSteps...)
```

### Secret Validation

Use helper functions for secret validation:

```go
// Single secret
step := GenerateSecretValidationStep("API_KEY", "Engine Name", "https://docs.url")

// Multiple secrets (at least one required)
step := GenerateMultiSecretValidationStep(
    []string{"API_KEY_1", "API_KEY_2"},
    "Engine Name",
    "https://docs.url",
)
```

### Firewall Domain Registration

Engines that rely on AWF/network sandboxing should declare their default allowed domains in `pkg/workflow/domains.go`. The `engineDefaultDomains` map provides the centralized registry:

```go
// In pkg/workflow/domains.go

// MyEngineDefaultDomains are the default domains required for My AI Engine operation
var MyEngineDefaultDomains = []string{
    "api.my-engine.com",
    "github.com",
    "host.docker.internal",
    "raw.githubusercontent.com",
    "registry.npmjs.org",
}

// Add to the engineDefaultDomains map
var engineDefaultDomains = map[constants.EngineName][]string{
    constants.CopilotEngine:  CopilotDefaultDomains,
    constants.ClaudeEngine:   ClaudeDefaultDomains,
    constants.CodexEngine:    CodexDefaultDomains,
    constants.GeminiEngine:   GeminiDefaultDomains,
    constants.MyEngine:       MyEngineDefaultDomains, // Add your engine here
}
```

For engines with **model/provider-specific** domain requirements (where the API domain depends on the selected model's provider prefix, e.g., `openai/gpt-4.1`), implement a `getMyEngineDefaultDomains(model string) ([]string, error)` function instead and handle it in `GetDefaultDomainsForEngine()`:

```go
func GetDefaultDomainsForEngine(engine constants.EngineName, model string) ([]string, error) {
    // ... existing cases ...
    if engine == constants.MyEngine {
        return getMyEngineDefaultDomains(model)
    }
    return engineDefaultDomains[engine], nil
}
```

These domains are automatically merged with user-configured `network.allowed` entries via `GetAllowedDomainsForEngine()`.

### Firewall Integration

Check if firewall is enabled and add AWF installation:

```go
if isFirewallEnabled(workflowData) {
    firewallConfig := getFirewallConfig(workflowData)
    agentConfig := getAgentConfig(workflowData)
    var awfVersion string
    if firewallConfig != nil {
        awfVersion = firewallConfig.Version
    }

    awfInstall := generateAWFInstallationStep(awfVersion, agentConfig)
    if len(awfInstall) > 0 {
        steps = append(steps, awfInstall)
    }
}
```

### Custom Command Support

Always check for custom command to skip installation:

```go
if workflowData.EngineConfig != nil && workflowData.EngineConfig.Command != "" {
    // Use custom command instead of default
    commandName = workflowData.EngineConfig.Command
    // Skip installation steps
    return []GitHubActionStep{}
}
```

### Safe-Outputs Integration

Add safe-outputs environment variable:

```go
env := map[string]string{
    "GH_AW_PROMPT": "/tmp/gh-aw/aw-prompts/prompt.txt",
}

applySafeOutputEnvToMap(env, workflowData)
```

### MCP Server Detection

Check if MCP servers are present:

```go
if HasMCPServers(workflowData) {
    // Add MCP configuration
    args = append(args, "--mcp-config", "/tmp/gh-aw/mcp-config/mcp-servers.json")
    
    // Add MCP gateway API key
    secrets = append(secrets, "MCP_GATEWAY_API_KEY")
}
```

## Best Practices

### 1. Logging

Use structured logging with the engine logger:

```go
var myEngineLog = logger.New("workflow:my_engine")

myEngineLog.Printf("Generating installation steps: workflow=%s", workflowData.Name)
myEngineLog.Print("Adding MCP configuration")
```

### 2. Error Handling

Return error messages with context and documentation links:

```go
return fmt.Errorf("engine configuration error: %w\n\nSee: %s", err, constants.DocsEnginesURL)
```

### 3. Capability Flags

Set capability flags accurately in the constructor. These flags are used by:
- Validation logic (e.g., `validatePluginSupport()`)
- Documentation generation
- Feature detection

### 4. Security

- Always validate secrets in installation steps
- Filter environment variables using `FilterEnvForSecrets()`
- Declare all required secrets in `GetRequiredSecretNames()`
- Handle safe-inputs secrets properly

### 5. Backward Compatibility

- Don't break existing engines when adding new ones
- Use BaseEngine defaults for new interface methods
- Test that existing workflows still compile

### 6. Modular Organization

Consider splitting large engine implementations:
- `<engine>_engine.go` - Core interface and constructor
- `<engine>_engine_installation.go` - Installation steps
- `<engine>_engine_execution.go` - Execution steps and runtime
- `<engine>_engine_tools.go` - Tool permissions and arguments
- `<engine>_logs.go` - Log parsing and metrics
- `<engine>_mcp.go` - MCP configuration rendering

See `copilot_engine.go` for an example of modular organization.

## Troubleshooting

### Engine Not Found

If `gh aw compile` reports "unknown engine":
1. Verify engine is registered in `NewEngineRegistry()`
2. Check engine ID matches exactly (case-sensitive)
3. Run tests to verify interface compliance

### Compilation Errors

If workflows fail to compile:
1. Check installation steps generate valid YAML
2. Verify execution steps have proper env vars
3. Test MCP configuration rendering separately
4. Enable debug logging: `DEBUG=workflow:* gh aw compile`

### Secret Validation Fails

If secret validation fails:
1. Verify secret names match exactly in `GetRequiredSecretNames()`
2. Check secret validation steps use same secret names
3. Ensure secrets are added to repository: `gh aw secrets list`

## Related Documentation

- [Interface Segregation Architecture](../pkg/workflow/agentic_engine.go) - Core interface design
- [Engine Validation](../pkg/workflow/engine_validation.go) - Validation logic
- [Engine Helpers](../pkg/workflow/engine_helpers.go) - Shared utilities
- [Code Organization](./code-organization.md) - File organization patterns
- [Testing Guide](./testing.md) - Testing patterns and conventions
