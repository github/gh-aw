# workflow Package

> Workflow compilation, validation, engine integration, safe-outputs, and GitHub Actions YAML generation for agentic workflow files.

## Overview

The `workflow` package is the compilation core of `gh-aw`. It transforms parsed markdown frontmatter (from `pkg/parser`) and markdown body text into complete GitHub Actions `.lock.yml` files. Compilation covers the full lifecycle: frontmatter parsing into strongly-typed configuration structs, multi-pass validation (schema, permissions, security, strict mode), engine-specific step generation (Copilot, Claude, Codex, Gemini, custom), safe-output job construction, and final YAML serialization.

The package is organized around three major subsystems:

1. **Compiler** (`compiler*.go`, `compiler_types.go`): The `Compiler` struct drives the main compilation pipeline. It accepts a markdown file path (or pre-parsed `WorkflowData`), builds the full GitHub Actions workflow YAML, and writes the `.lock.yml` file only when the content has changed.

2. **Engine registry** (`agentic_engine.go`, `*_engine.go`): A pluggable engine architecture where each AI engine (`copilot`, `claude`, `codex`, `gemini`, `pi`, `custom`) implements a set of focused interfaces (`Engine`, `CapabilityProvider`, `WorkflowExecutor`, `MCPConfigProvider`, etc.). Engines are registered in a global `EngineRegistry` and looked up by name at compile time.

3. **Validation** (`validation.go`, `strict_mode_*.go`, `*_validation.go`): A layered validation system organized by domain. Each validator is a focused file under 300 lines. Validation runs both at compile time and optionally in strict mode for production deployments.

The package is intentionally large (~520 source files) because it encodes all GitHub Actions generation logic, including per-action job builders for every supported safe-output type (add comment, add labels, assign to user, close issue, update PR, etc.).

## Public API

### Core Compiler Types

| Type | Kind | Description |
|------|------|-------------|
| `Compiler` | struct | Main compilation engine; use `NewCompiler(opts...)` |
| `CompilerOption` | func type | Functional option for configuring a `Compiler` |
| `WorkflowData` | struct | Complete in-memory representation of a compiled workflow |
| `FileCreationTracker` | interface | Abstraction for tracking written files |

#### `Compiler` Methods

| Method | Signature | Description |
|--------|-----------|-------------|
| `CompileWorkflow` | `func(*Compiler) CompileWorkflow(markdownPath string) error` | Compiles a markdown file and writes the `.lock.yml` |
| `CompileWorkflowData` | `func(*Compiler) CompileWorkflowData(workflowData *WorkflowData, markdownPath string) error` | Compiles pre-parsed `WorkflowData` |

#### Compiler Options

| Function | Description |
|----------|-------------|
| `WithVerbose(bool)` | Enable verbose diagnostic output |
| `WithEngineOverride(string)` | Override the AI engine |
| `WithSkipValidation(bool)` | Skip schema validation |
| `WithNoEmit(bool)` | Validate without writing lock files |
| `WithFailFast(bool)` | Stop at first validation error |
| `WithWorkflowIdentifier(string)` | Set the workflow identifier |
| `NewCompiler(opts ...CompilerOption)` | Creates a new `Compiler` |
| `WithVersion(string) CompilerOption` | Sets a specific compiler version |

### Engine Architecture

| Type | Kind | Description |
|------|------|-------------|
| `Engine` | interface | Core identity: `GetID()`, `GetDisplayName()`, `GetDescription()`, `IsExperimental()` |
| `CapabilityProvider` | interface | Optional feature detection via `GetCapabilities()` |
| `WorkflowExecutor` | interface | Compilation: `GetDeclaredOutputFiles`, `GetInstallationSteps`, `GetExecutionSteps` |
| `MCPConfigProvider` | interface | MCP configuration generation |
| `LogParser` | interface | Log parsing for audit/metrics |
| `SecurityProvider` | interface | Security-related configuration |
| `ModelEnvVarProvider` | interface | Model environment variable mapping |
| `AgentFileProvider` | interface | Custom agent file support |
| `ConfigRenderer` | interface | Configuration file rendering |
| `DriverProvider` | interface | Driver-level execution configuration |
| `HarnessProvider` | interface | Harness script configuration — returns the Node.js harness filename or empty string |
| `CodingAgentEngine` | interface | Composite interface combining all engine capabilities |
| `BaseEngine` | struct | Base implementation shared by all engines |
| `EngineRegistry` | struct | Global registry mapping engine names to implementations |
| `CopilotEngine` | struct | Copilot coding agent engine |
| `ClaudeEngine` | struct | Claude coding agent engine |
| `CodexEngine` | struct | OpenAI Codex coding agent engine |
| `GeminiEngine` | struct | Google Gemini CLI coding agent engine |
| `PiEngine` | struct | Pi coding agent engine |
| `UniversalLLMBackend` | string alias | Universal LLM backend identifier (`claude`, `codex`) |
| `UniversalLLMConsumerEngine` | struct | Shared implementation for universal LLM backends |
| `UniversalCLIEngineExecutionConfig` | struct | Execution configuration for universal LLM CLI engines |
| `EngineCatalog` | struct | Catalog of engine definitions with lookup and resolution helpers |
| `EngineDefinition` | struct | Declarative metadata for an AI engine (ID, display name, provider, models) |
| `ResolvedEngineTarget` | struct | Result of `EngineCatalog.Resolve()`: definition + config + runtime adapter |
| `AuthStrategy` | string alias | Authentication strategy (`"api-key"`, `"oauth-client-credentials"`, `"bearer"`) |
| `AuthDefinition` | struct | Authentication configuration for an engine provider (strategy, secret, OAuth params) |
| `RequestShape` | struct | Non-standard URL and body transformations for provider API calls |
| `ProviderSelection` | struct | AI provider identity with optional auth and request-shaping configuration |
| `ModelSelection` | struct | Default and supported model names for an engine |
| `EngineInstallConfig` | struct | Configuration for engine installation steps (secrets, npm package, version) |

#### Engine Registry Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `NewEngineRegistry` | `func() *EngineRegistry` | Creates a new engine registry |
| `GetGlobalEngineRegistry` | `func() *EngineRegistry` | Returns the singleton global engine registry |
| `NewCopilotEngine` | `func() *CopilotEngine` | Creates the Copilot engine |
| `NewClaudeEngine` | `func() *ClaudeEngine` | Creates the Claude engine |
| `NewCodexEngine` | `func() *CodexEngine` | Creates the Codex engine |
| `NewGeminiEngine` | `func() *GeminiEngine` | Creates the Gemini engine |
| `NewPiEngine` | `func() *PiEngine` | Creates the Pi engine |
| `NewEngineCatalog` | `func(registry *EngineRegistry) *EngineCatalog` | Creates an engine catalog from an engine registry |

### Frontmatter Configuration Types

| Type | Kind | Description |
|------|------|-------------|
| `FrontmatterConfig` | struct | Full parsed frontmatter with typed and legacy fields |
| `RuntimeConfig` | struct | Single runtime version configuration (version string) |
| `RuntimesConfig` | struct | All runtime versions (node, python, go, uv, bun, deno) |
| `PermissionsConfig` | struct | GitHub Actions permissions (shorthand + detailed fields) |
| `GitHubActionsPermissionsConfig` | struct | Detailed permissions with all scope fields |
| `GitHubAppPermissionsConfig` | struct | GitHub App permission scopes |
| `CheckoutConfig` | struct | Parsed `checkout:` entry controlling repository/ref/path/auth/fetch behavior for generated checkout steps |
| `CheckoutManager` | struct | Merges and resolves one or more `checkout:` entries into deterministic checkout-step plans |
| `ObservabilityConfig` | struct | OTLP/observability configuration |
| `RateLimitConfig` | struct | Rate limit settings |
| `OTLPConfig` | struct | OpenTelemetry protocol configuration |
| `EngineConfig` | struct | Parsed `engine:` frontmatter block — see [Engine Configuration Fields](#engine-configuration-fields) |
| `EngineAuthConfig` | struct | Engine-level auth config (`engine.auth.*` → `AWF_AUTH_*` env vars for API proxy) |
| `NetworkPermissions` | struct | Parsed `network:` frontmatter block; controls allowed/blocked domain lists |
| `EngineNetworkConfig` | struct | Combines `*EngineConfig` and `*NetworkPermissions` for engine helpers that need both |

#### Checkout configuration

The `checkout:` frontmatter key is parsed by `ParseCheckoutConfigs` into one or more `CheckoutConfig` values. Each entry may target the current repository or a cross-repository checkout, and supports authentication via `github-token` or `github-app` (mutually exclusive) plus optional `safe-outputs-github-app`.

- `github-app` changes the authentication used by the generated `actions/checkout` step itself.
- `safe-outputs-github-app` mints a GitHub App token only for later `safe_outputs` git operations (fetch/push) against the checkout target; it does **not** change activation or agent-job checkout authentication.
- `CheckoutManager` merges compatible checkout requests, unions sparse-checkout patterns, deduplicates overlapping repo/ref pairs, and tracks whether any checkout requires GitHub App token minting for later safe_outputs operations.

#### Engine Configuration Fields

`EngineConfig` is populated by `ExtractEngineConfig` from the `engine:` frontmatter key. It is stored on `EngineNetworkConfig.Engine` and forwarded to each engine's `GetExecutionSteps` / `GetInstallationSteps` implementations.

| Field | Type | YAML key | Description |
|-------|------|----------|-------------|
| `ID` | `string` | `engine` | Engine identifier (e.g. `"copilot"`, `"claude"`, `"codex"`) |
| `Version` | `string` | `engine.version` | Pinned engine/CLI version |
| `Model` | `string` | `engine.model` | LLM model name |
| `PermissionMode` | `string` | `engine.permission-mode` | Agent permission mode |
| `MaxTurns` | `string` | `engine.max-turns` | Maximum agent turns |
| `MaxToolDenials` | `string` | `engine.max-tool-denials` | Max repeated tool denials before stopping (Copilot SDK mode only) |
| `MaxRuns` | `int` | `engine.max-runs` | Maximum LLM invocations per run (AWF `apiProxy.maxRuns`) |
| `MaxContinuations` | `int` | `engine.max-continuations` | Maximum autopilot continuations (copilot engine; `> 1` enables `--autopilot`) |
| `MaxAICredits` | `int64` | `engine.max-ai-credits` | Maximum AI credits per run for AWF API-proxy firewall enforcement |
| `Concurrency` | `string` | `engine.concurrency` | Agent job-level concurrency YAML |
| `UserAgent` | `string` | `engine.user-agent` | Custom user-agent string |
| `Command` | `string` | `engine.command` | Custom executable path; skips installation steps when set |
| `HarnessScript` | `string` | `engine.harness-script` | Custom Node.js harness script filename (replaces engine default) |
| `CopilotSDK` | `bool` | `engine.copilot-sdk` | Enables GitHub Copilot SDK integration. When `true`, the compiler starts a headless Copilot CLI sidecar and sets `COPILOT_SDK_URI` on child processes so the SDK can connect to it. Also enabled automatically for the copilot engine when `Driver` is non-empty. |
| `Driver` | `string` | `engine.driver` | **(Experimental)** Custom driver script filename or command. Supports `.js`/`.cjs`/`.mjs` (Node.js), `.py` (Python), `.ts`/`.mts` (TypeScript), `.rb` (Ruby), or a bare command name for an arbitrary executable on `PATH`. Setting this field implies `copilot-sdk: true` for the copilot engine. |
| `Env` | `map[string]string` | `engine.env` | Extra environment variables injected into the agent job |
| `Auth` | `*EngineAuthConfig` | `engine.auth` | Engine-level auth config for the API proxy sidecar |
| `Config` | `string` | `engine.config` | Inline engine configuration JSON/YAML string |
| `Args` | `[]string` | `engine.args` | Extra CLI arguments passed to the engine |
| `Agent` | `string` | `engine.agent` | Agent identifier for `copilot --agent` flag (copilot engine only) |
| `APITarget` | `string` | `engine.api-target` | Custom API endpoint hostname |
| `Bare` | `bool` | `engine.bare` | Disables automatic loading of context/instructions |
| `IsInlineDefinition` | `bool` | _(internal)_ | `true` when engine is defined inline via `engine.runtime` |
| `MCPSessionTimeout` | `string` | `engine.mcp.session-timeout` | Go duration for MCP gateway sessions (e.g. `"4h"`) |
| `MCPToolTimeout` | `string` | `engine.mcp.tool-timeout` | Go duration for individual MCP tool calls (e.g. `"2m"`) |
| `Extensions` | `[]string` | `engine.extensions` | Engine-specific plugin names to install before launching (Pi engine) |

### Permissions System

| Type | Kind | Description |
|------|------|-------------|
| `Permissions` | struct | Runtime permissions representation for the compiled workflow |
| `PermissionLevel` | string alias | Permission level: `read`, `write`, `none` |
| `PermissionScope` | string alias | Permission scope (e.g., `contents`, `issues`, `pull-requests`) |
| `PermissionsParser` | struct | Parses YAML permissions blocks into `Permissions` |
| `PermissionsValidationResult` | struct | Result of `ValidatePermissions` |

#### Permissions Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `NewPermissionsParser` | `func(permissionsYAML string) *PermissionsParser` | Creates a parser from YAML text |
| `NewPermissionsParserFromValue` | `func(permissionsValue any) *PermissionsParser` | Creates a parser from parsed YAML value |
| `ValidatePermissions` | `func(*Permissions, ValidatableTool) *PermissionsValidationResult` | Validates permissions for a given tool |
| `FormatValidationMessage` | `func(*PermissionsValidationResult, bool) string` | Formats a validation result as a human-readable message |
| `ComputePermissionsForSafeOutputs` | `func(*SafeOutputsConfig) *Permissions` | Computes required permissions for safe-output types |
| `SortPermissionScopes` | `func([]PermissionScope)` | Sorts permission scopes alphabetically |

#### Permissions Factory (common combinations)

| Function | Description |
|----------|-------------|
| `NewPermissionsContentsWritePRWrite()` | contents:write + pull-requests:write |
| `NewPermissionsContentsWriteIssuesWritePRWrite()` | contents:write + issues:write + pull-requests:write |
| `NewPermissionsContentsReadDiscussionsWrite()` | contents:read + discussions:write |
| `NewPermissionsContentsReadPRWrite()` | contents:read + pull-requests:write |
| `NewPermissionsContentsReadSecurityEventsWrite()` | contents:read + security-events:write |

### Tools Configuration

| Type | Kind | Description |
|------|------|-------------|
| `ToolsConfig` | struct | Parsed `tools:` block with all tool configurations |
| `Tools` | type alias | Alias for `ToolsConfig` |
| `GitHubToolConfig` | struct | GitHub MCP tool configuration (toolsets, allowed tools, integrity) |
| `PlaywrightToolConfig` | struct | Playwright browser automation tool config |
| `PlaywrightDockerArgs` | struct | Docker image version and MCP package version for Playwright container configuration |
| `BashToolConfig` | struct | Bash execution tool config |
| `WebFetchToolConfig` | struct | Web fetch tool config |
| `WebSearchToolConfig` | struct | Web search tool config |
| `EditToolConfig` | struct | File edit tool config |
| `AgenticWorkflowsToolConfig` | struct | Nested agentic workflows tool config |
| `CacheMemoryToolConfig` | struct | Cache-memory persistence tool config |
| `CommentMemoryToolConfig` | struct | Comment-memory tool config wrapper (raw value dispatched to `comment_memory.go`) |
| `RepoMemoryToolConfig` | struct | Repository-memory tool config wrapper (raw value dispatched to `repo_memory.go`) |
| `MCPServerConfig` | struct | Generic MCP server configuration |
| `MCPGatewayRuntimeConfig` | struct | MCP Gateway runtime configuration |
| `GitHubToolName` | string alias | Named GitHub MCP tool (e.g., `"issue_read"`) |
| `GitHubAllowedTools` | `[]GitHubToolName` | Typed slice with conversion helpers |
| `GitHubToolset` | string alias | Named GitHub toolset (e.g., `"default"`, `"repos"`) |
| `GitHubToolsets` | `[]GitHubToolset` | Typed slice with conversion helpers |
| `GitHubIntegrityLevel` | string alias | Integrity level (`"low"`, `"medium"`, `"high"`) |

#### Tools Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `NewTools` | `func(map[string]any) *Tools` | Creates a `Tools` from a raw map |
| `ParseToolsConfig` | `func(map[string]any) (*ToolsConfig, error)` | Parses the `tools:` frontmatter section |
| `ValidateGitHubToolsAgainstToolsets` | `func([]string, []string) error` | Validates tool names against enabled toolsets |
| `GetPlaywrightTools` | `func() []any` | Returns the standard Playwright tool definitions |
| `GetSafeOutputToolOptions` | `func() []SafeOutputToolOption` | Returns valid safe-output tool option definitions |
| `GetValidationConfigJSON` | `func(enabledTypes []string) (string, error)` | Returns JSON validation config for given safe-output types |

### Safe Outputs

| Type | Kind | Description |
|------|------|-------------|
| `SafeOutputsConfig` | struct | Parsed `safe-outputs:` configuration |
| `SafeOutputTargetConfig` | struct | Target configuration for a safe-output job |
| `SafeOutputFilterConfig` | struct | Filter configuration for a safe-output job |
| `SafeOutputToolOption` | struct | A valid safe-output tool option |

#### Safe Output Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `HasSafeOutputsEnabled` | `func(*SafeOutputsConfig) bool` | Returns whether any safe-output type is enabled |
| `ParseTargetConfig` | `func(map[string]any) (SafeOutputTargetConfig, bool)` | Parses a target configuration block |
| `ParseFilterConfig` | `func(map[string]any) SafeOutputFilterConfig` | Parses a filter configuration block |
| `SafeOutputsConfigFromKeys` | `func([]string) *SafeOutputsConfig` | Creates a config from a list of type keys |

#### Safe Output Tool Configuration Types

Each safe-output tool type has its own configuration struct parsed from the `safe-outputs:` frontmatter block. All embed `BaseSafeOutputConfig` (which provides the `max` field) and many also embed `SafeOutputTargetConfig` or `SafeOutputFilterConfig`.

| Type | Kind | Description |
|------|------|-------------|
| `AddCommentsConfig` | struct | Configuration for creating issue/PR/discussion comments |
| `AddCommentConfig` | type alias | Deprecated alias for `AddCommentsConfig` |
| `AddLabelsConfig` | struct | Configuration for adding labels to issues/PRs |
| `AddReviewerConfig` | struct | Configuration for adding reviewers to PRs |
| `AssignMilestoneConfig` | struct | Configuration for assigning milestones to issues |
| `AssignToAgentConfig` | struct | Configuration for assigning Copilot coding agents to issues |
| `AssignToUserConfig` | struct | Configuration for assigning users to issues |
| `AutofixCodeScanningAlertConfig` | struct | Configuration for adding autofixes to code scanning alerts |
| `CloseEntityType` | string alias | Identifies the entity type to close (`issue`, `pull_request`, `discussion`) |
| `CloseEntityConfig` | struct | Shared configuration for close-entity operations |
| `CloseEntityJobParams` | struct | Internal job parameters for close-entity operations |
| `CloseIssuesConfig` | type alias | `= CloseEntityConfig` — close issues |
| `ClosePullRequestsConfig` | type alias | `= CloseEntityConfig` — close pull requests |
| `CloseDiscussionsConfig` | type alias | `= CloseEntityConfig` — close discussions |
| `CommentMemoryConfig` | struct | Configuration for the `comment_memory` safe output (persistent comment-based memory) |
| `CreateAgentSessionConfig` | struct | Configuration for creating GitHub Copilot coding agent sessions |
| `CreateCheckRunOutputConfig` | struct | Static defaults for check run output fields (title, summary, text) |
| `CreateCheckRunConfig` | struct | Configuration for creating GitHub check runs |
| `CreateDiscussionsConfig` | struct | Configuration for creating GitHub discussions |
| `CreateIssuesConfig` | struct | Configuration for creating GitHub issues |
| `CreatePullRequestReviewCommentsConfig` | struct | Configuration for creating PR review comments |
| `CreateProjectsConfig` | struct | Configuration for creating GitHub Projects v2 |
| `CreateProjectStatusUpdateConfig` | struct | Configuration for creating GitHub project status updates |
| `CreatePullRequestsConfig` | struct | Configuration for creating GitHub pull requests |
| `DispatchRepositoryToolConfig` | struct | Single named tool within a `dispatch_repository` configuration |
| `DispatchRepositoryConfig` | struct | Configuration for dispatching `repository_dispatch` events |
| `DispatchWorkflowConfig` | struct | Configuration for dispatching GitHub Actions workflows |
| `HideCommentConfig` | struct | Configuration for hiding/minimizing GitHub comments |
| `IssueReportingConfig` | struct | Shared configuration base for `missing_data`, `missing_tool`, and `report_incomplete` safe outputs |
| `MissingDataConfig` | type alias | `= IssueReportingConfig` for the `missing_data` safe output |
| `MissingToolConfig` | type alias | `= IssueReportingConfig` for the `missing_tool` safe output |
| `ReportIncompleteConfig` | type alias | `= IssueReportingConfig` for the `report_incomplete` safe output |
| `LinkSubIssueConfig` | struct | Configuration for linking issues as sub-issues |
| `MarkPullRequestAsReadyForReviewConfig` | struct | Configuration for marking draft PRs as ready for review |
| `MergePullRequestConfig` | struct | Configuration for merging pull requests |
| `PushToPullRequestBranchConfig` | struct | Configuration for pushing agent-generated changes to a PR branch |
| `RemoveLabelsConfig` | struct | Configuration for removing labels from issues/PRs |
| `ReplyToPullRequestReviewCommentConfig` | struct | Configuration for replying to PR review comments |
| `RepoMemoryConfig` | struct | Configuration for the `repo_memory` safe output (repository-scoped persistent memory) |
| `RepoMemoryEntry` | struct | A single key/value entry in repository memory |
| `ResolvePullRequestReviewThreadConfig` | struct | Configuration for resolving PR review threads |
| `SetIssueFieldConfig` | struct | Configuration for setting a single issue field |
| `SetIssueTypeConfig` | struct | Configuration for setting an issue's type |
| `SubmitPullRequestReviewConfig` | struct | Configuration for submitting PR reviews (approve, request changes, comment) |
| `UnassignFromUserConfig` | struct | Configuration for removing assignees from issues |
| `UpdateDiscussionsConfig` | struct | Configuration for updating GitHub discussions |
| `UpdateIssuesConfig` | struct | Configuration for updating GitHub issues |
| `ProjectView` | struct | GitHub Projects v2 view configuration |
| `ProjectFieldDefinition` | struct | A field definition for a GitHub Projects v2 board |
| `UpdateProjectConfig` | struct | Configuration for updating GitHub Projects v2 boards |
| `UpdatePullRequestsConfig` | struct | Configuration for updating GitHub pull requests |
| `UpdateReleaseConfig` | struct | Configuration for updating GitHub releases |
| `UploadArtifactConfig` | struct | Configuration for uploading GitHub Actions artifacts from agent output |
| `ArtifactFiltersConfig` | struct | Include/exclude glob patterns for artifact file selection |
| `ArtifactDefaultsConfig` | struct | Default request settings applied when the model omits a field (e.g. `if-no-files`) |
| `UploadAssetsConfig` | struct | Configuration for publishing assets to an orphaned git branch |
| `CreateCodeScanningAlertsConfig` | struct | Configuration for creating repository code scanning alerts (SARIF format) |
| `ReplaceLabelConfig` | struct | Configuration for replacing one label with another on issues/PRs |
| `LabelTransition` | struct | An allowed label state transition (`from` → `to` pair) |
| `SafeScriptConfig` | struct | A custom safe-output handler script that runs inside the consolidated safe-outputs job |
| `DismissPullRequestReviewConfig` | struct | Configuration for dismissing pull request reviews |
| `CommentEventMapping` | struct | Maps comment event types to trigger conditions |
| `CreateParseOptions` | struct | Options controlling `create-*` safe-output parsing behavior |
| `GitHubToolsetValidationError` | struct | Validation error for unknown GitHub MCP toolsets |

### Sandbox Configuration

The sandbox subsystem controls which agent firewall (AWF) or sandbox runtime is used during workflow execution.

| Type | Kind | Description |
|------|------|-------------|
| `SandboxType` | string alias | Sandbox type identifier (`"awf"`, `"default"`) |
| `SandboxConfig` | struct | Top-level sandbox configuration; supports new `agent`/`mcp` fields and legacy `type`/`config` fields |
| `AgentSandboxConfig` | struct | Agent-side sandbox configuration (ID, version, command, mounts, memory, env) |
| `SandboxRuntimeConfig` | struct | Anthropic Sandbox Runtime (SRT) configuration (filesystem, network, violations) |
| `SRTNetworkConfig` | struct | Network configuration for SRT (allowed/blocked domains, Unix sockets) |
| `SRTFilesystemConfig` | struct | Filesystem configuration for SRT (denyRead, allowWrite, denyWrite) |

#### Sandbox Constants

| Name | Type | Description |
|------|------|-------------|
| `SandboxTypeAWF` | `SandboxType` | AWF sandbox type (`"awf"`) |
| `SandboxTypeDefault` | `SandboxType` | Alias for AWF for backward compatibility (`"default"`) |

### MCP Scripts

The MCP Scripts subsystem provides inline custom tool definitions (JavaScript, shell, Python, or Go) that are compiled into a local MCP server at workflow runtime.

| Type | Kind | Description |
|------|------|-------------|
| `MCPScriptsConfig` | struct | Parsed `mcp-scripts:` block; holds transport mode and a map of tool configurations |
| `MCPScriptToolConfig` | struct | Configuration for a single MCP script tool (description, inputs, script/run/py/go, env, timeout) |
| `MCPScriptParam` | struct | An input parameter for a script tool (type, description, required, default) |
| `MCPScriptsToolJSON` | struct | Tool entry serialized to `tools.json` for the MCP server |
| `MCPScriptsConfigJSON` | struct | Top-level `tools.json` structure (serverName, version, logDir, tools list) |

#### MCP Scripts Constants

| Name | Type | Description |
|------|------|-------------|
| `MCPScriptsModeHTTP` | `string` | The only supported transport mode for MCP scripts (`"http"`) |
| `MCPScriptsDirectory` | `string` | Runtime directory where MCP scripts files are generated |

#### MCP Scripts Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `HasMCPScripts` | `func(*MCPScriptsConfig) bool` | Returns whether any MCP script tools are configured |
| `IsMCPScriptsEnabled` | `func(*MCPScriptsConfig) bool` | Returns whether MCP scripts are enabled (currently equivalent to `HasMCPScripts`) |
| `GenerateMCPScriptsToolsConfig` | `func(*MCPScriptsConfig) string` | Generates the `tools.json` configuration file content for the MCP scripts server |
| `GenerateMCPScriptsMCPServerScript` | `func(*MCPScriptsConfig) string` | Generates the HTTP entry-point script for the MCP scripts server |
| `GenerateMCPScriptJavaScriptToolScript` | `func(*MCPScriptToolConfig) string` | Generates the `.cjs` tool handler for a JavaScript `script:` tool |
| `GenerateMCPScriptShellToolScript` | `func(*MCPScriptToolConfig) string` | Generates the `.sh` tool handler for a `run:` shell tool |
| `GenerateMCPScriptPythonToolScript` | `func(*MCPScriptToolConfig) string` | Generates the `.py` tool handler for a `py:` Python tool |
| `GenerateMCPScriptGoToolScript` | `func(*MCPScriptToolConfig) string` | Generates the `.go` tool handler for a `go:` Go tool |

### MCP Renderer

The MCP renderer subsystem provides unified rendering for MCP server configurations across engines (Claude, Copilot, Codex, custom).

| Type | Kind | Description |
|------|------|-------------|
| `MCPRendererOptions` | struct | Options for the unified MCP renderer (format, inline args, write-sink guard policies) |
| `MCPConfigRendererUnified` | struct | Provides unified rendering methods across all engines |
| `MCPToolRenderers` | struct | Holds engine-specific rendering functions for each MCP tool type |
| `JSONMCPConfigOptions` | struct | Configuration for JSON-based MCP config rendering (path, renderers, gateway) |
| `GitHubMCPDockerOptions` | struct | Options for rendering the GitHub MCP server in Docker/stdio mode |
| `GitHubMCPRemoteOptions` | struct | Options for rendering the GitHub MCP server in remote/HTTP mode |
| `RenderCustomMCPToolConfigHandler` | func type | Function type for rendering custom MCP tool configurations |

### Network Permissions

| Type | Kind | Description |
|------|------|-------------|
| `NetworkPermissions` | struct | Parsed `network:` block with `allowed` and `blocked` domain lists |

#### Network Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `GetAllowedDomains` | `func(*NetworkPermissions) []string` | Returns the full list of allowed domains |
| `GetDomainEcosystem` | `func(domain string) string` | Returns the ecosystem name for a domain |
| `GetDefaultDomainsForEngine` | `func(EngineName, model string) ([]string, error)` | Returns the engine's default required domains (model-aware for Pi and engines declaring `behaviors.network`) |
| `GetAllowedDomainsForEngine` | `func(EngineName, *NetworkPermissions, ...) string` | Returns allowed domains for a specific engine |
| `GetAllowedDomainsForEngineWithModel` | `func(EngineName, model string, *NetworkPermissions, ...) (string, error)` | Returns allowed domains for a model-aware engine |
| `GetThreatDetectionAllowedDomains` | `func(*NetworkPermissions) string` | Allowed domains for threat detection jobs |

### Error Types

| Type | Kind | Description |
|------|------|-------------|
| `WorkflowValidationError` | struct | Validation error with field, value, reason, and suggestion |
| `OperationError` | struct | Error from a workflow operation with entity context |
| `ConfigurationError` | struct | Configuration error with config key and suggested fix |
| `ErrorCollector` | struct | Collects multiple errors; supports `failFast` mode |
| `SharedWorkflowError` | struct | Error for shared/reusable workflow violations |
| `RedirectOnlyWorkflowError` | struct | Error indicating a workflow only contains redirect/import steps |
| `FieldLocation` | type alias | `= console.ErrorPosition` — source location for validation errors |

#### Error Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `NewValidationError` | `func(field, value, reason, suggestion string) *WorkflowValidationError` | Creates a validation error |
| `NewOperationError` | `func(operation, entityType, entityID string, cause error, suggestion string) *OperationError` | Creates an operation error |
| `NewConfigurationError` | `func(configKey, value, reason, suggestion string) *ConfigurationError` | Creates a configuration error |
| `NewErrorCollector` | `func(failFast bool) *ErrorCollector` | Creates an error collector |

### Workflow Resolution

| Function | Signature | Description |
|----------|-----------|-------------|
| `ResolveWorkflowName` | `func(string) (string, error)` | Resolves a workflow input string to a canonical name |
| `FindWorkflowName` | `func(string) (string, error)` | Finds the workflow name from a string or file path |
| `GetWorkflowLockFileName` | `func(string) (string, error)` | Returns the `.lock.yml` path for a workflow |
| `GetAllWorkflows` | `func() ([]WorkflowNameMatch, error)` | Returns all installed workflow names |
| `GetWorkflowIDFromPath` | `func(string) string` | Derives the workflow ID from its markdown path |

### Markdown Security Scanner

Detects dangerous patterns in externally-sourced markdown files (e.g., from `gh aw add` or `gh aw trial`) and produces hard errors that cannot be overridden.

| Type | Kind | Description |
|------|------|-------------|
| `SecurityFindingCategory` | string alias | Category of a security finding (`"unicode-abuse"`, `"html-abuse"`, etc.) |
| `SecurityFinding` | struct | A single security issue (category, description, 1-based line, snippet) |

| Function | Signature | Description |
|----------|-----------|-------------|
| `ScanMarkdownSecurity` | `func(content string) []SecurityFinding` | Scans markdown body for dangerous patterns; non-empty result means the content MUST be rejected |

### Package Extraction

| Type | Kind | Description |
|------|------|-------------|
| `PackageExtractor` | struct | Reusable parser for extracting package names from package-manager command strings (npm, pip, uv, go, etc.) |

### Expression Safety Validation

| Type | Kind | Description |
|------|------|-------------|
| `ExpressionValidationOptions` | struct | Options for validating a single GitHub Actions expression (compiled regex patterns for allowed expression shapes) |

### Action Pinning

| Type | Kind | Description |
|------|------|-------------|
| `ActionPin` | struct | An action pin (repo + SHA) |
| `ActionPinsData` | struct | Map of all action pins |
| `ActionMode` | string alias | Action reference mode (`dev`, `release`, `script`, `action`) |
| `ActionCache` | struct | Cache for resolved action SHAs |
| `ActionResolver` | struct | Resolves action SHAs from GitHub |

#### `ActionResolver` Methods

| Method | Signature | Description |
|--------|-----------|-------------|
| `NewActionResolver` | `func(cache *ActionCache) *ActionResolver` | Creates a new `ActionResolver` backed by the given cache |
| `ResolveSHA` | `func(ctx context.Context, repo, version string) (string, error)` | Resolves a GitHub Action repo+version to its full commit SHA; serves cache hits first |

#### `ActionCache` Constructors

| Function | Signature | Description |
|----------|-----------|-------------|
| `NewActionCache` | `func(repoRoot string) *ActionCache` | Creates a new action cache instance backed by the on-disk cache file in `repoRoot` |

| Function | Signature | Description |
|----------|-----------|-------------|
| `GetActionPin` | `func(actionRepo string) string` | Returns the pinned SHA for an action |
| `DetectActionMode` | `func(version string) ActionMode` | Detects the action reference mode |
| `ParseTagRefTSV` | `func(line string) (sha, objType string, err error)` | Parses tab-separated tag ref output into SHA and object type |
| `ResolveGhAwRef` | `func(ctx context.Context, ref string) (string, error)` | Resolves a branch, tag, or SHA ref in `github/gh-aw` to its full 40-character commit SHA; passes full SHAs through unchanged |
| `ExtractActionsFromLockFile` | `func(lockFilePath string) ([]ActionUsage, error)` | Extracts action usages from a lock file |
| `CheckActionSHAUpdates` | `func(actions []ActionUsage, resolver *ActionResolver) []ActionUpdateCheck` | Checks whether action SHAs need updates |
| `ApplyActionPinsToTypedSteps` | `func([]*WorkflowStep, *WorkflowData) []*WorkflowStep` | Applies pins to all steps |
| `ValidateActionSHAsInLockFile` | `func(string, *ActionCache, bool) error` | Validates action SHAs in a lock file |

### String Utilities (Workflow-Specific)

| Function | Signature | Description |
|----------|-----------|-------------|
| `SanitizeName` | `func(string, *SanitizeOptions) string` | Sanitizes a name for use in GitHub Actions |
| `SanitizeWorkflowName` | `func(string) string` | Sanitizes a workflow name |
| `SanitizeIdentifier` | `func(string) string` | Sanitizes a generic identifier |
| `SanitizeWorkflowIDForCacheKey` | `func(string) string` | Sanitizes a workflow ID for use as a cache key |
| `PrettifyToolName` | `func(string) string` | Returns a human-readable tool name |
| `ShortenCommand` | `func(string) string` | Shortens a long command for display |
| `GenerateHeredocDelimiterFromContent` | `func(name, content string) string` | Generates a stable heredoc delimiter |
| `ValidateHeredocContent` | `func(content, delimiter string) error` | Validates heredoc content safety |
| `ValidateHeredocDelimiter` | `func(string) error` | Validates a heredoc delimiter |

### Secret Handling

| Function | Signature | Description |
|----------|-----------|-------------|
| `ExtractSecretName` | `func(string) string` | Extracts the secret name from a `${{ secrets.NAME }}` expression |
| `ExtractSecretsFromValue` | `func(string) map[string]string` | Extracts all secrets from a template value |
| `ReplaceSecretsWithEnvVars` | `func(string, map[string]string) string` | Replaces secret references with env var references |
| `ExtractGitHubContextExpressionsFromValue` | `func(string) map[string]string` | Extracts GitHub context expressions |
| `CollectSecretReferences` | `func(string) []string` | Collects all secret references from YAML content |
| `CollectActionReferences` | `func(string) []string` | Collects all action references from YAML content |

### Concurrency & Scheduling

| Function | Signature | Description |
|----------|-----------|-------------|
| `GenerateConcurrencyConfig` | `func(*WorkflowData, bool) string` | Generates `concurrency:` YAML for a workflow |
| `GenerateJobConcurrencyConfig` | `func(*WorkflowData) string` | Generates job-level concurrency YAML |
| `ResolveRelativeDate` | `func(dateStr string, baseTime time.Time) (string, error)` | Resolves relative date strings (e.g., "2 weeks ago") |

### YAML Utilities

| Function | Signature | Description |
|----------|-----------|-------------|
| `UnquoteYAMLKey` | `func(yamlStr, key string) string` | Removes unnecessary quotes from a YAML key |
| `MarshalWithFieldOrder` | `func(map[string]any, []string) ([]byte, error)` | Marshals a map with priority-ordered fields |
| `OrderMapFields` | `func(map[string]any, []string) yaml.MapSlice` | Returns an ordered map slice |
| `CleanYAMLNullValues` | `func(string) string` | Removes null values from YAML output |
| `ConvertStepToYAML` | `func(map[string]any) (string, error)` | Converts a step map to YAML text |

### Trigger Parsing

| Type | Kind | Description |
|------|------|-------------|
| `TriggerIR` | struct | Intermediate representation of a workflow trigger |

| Function | Signature | Description |
|----------|-----------|-------------|
| `ParseTriggerShorthand` | `func(string) (*TriggerIR, error)` | Parses a trigger shorthand string |

### AWF Command Building

| Type | Kind | Description |
|------|------|-------------|
| `AWFCommandConfig` | struct | Configuration for building `gh aw` CLI commands (workflow path, flags, engine override) |

| Function | Signature | Description |
|----------|-----------|-------------|
| `BuildAWFCommand` | `func(AWFCommandConfig) string` | Builds the `gh aw` command string for a workflow step |
| `BuildAWFArgs` | `func(AWFCommandConfig) []string` | Builds CLI argument list for `gh aw` |
| `GetAWFCommandPrefix` | `func(*WorkflowData) string` | Returns the `gh aw` command prefix |
| `WrapCommandInShell` | `func(string) string` | Wraps a command in a shell `run:` block |
| `GetCopilotAPITarget` | `func(*WorkflowData) string` | Returns the Copilot API target URL |
| `GetGeminiAPITarget` | `func(*WorkflowData, string) string` | Returns the Gemini API target hostname |
| `ComputeAWFExcludeEnvVarNames` | `func(*WorkflowData, []string) []string` | Computes secret-backed env var names to exclude from AWF |

### Versioning

| Function | Signature | Description |
|----------|-----------|-------------|
| `SetVersion` | `func(string)` | Sets the package version at startup |
| `GetVersion` | `func() string` | Returns the current package version |
| `SetIsRelease` | `func(bool)` | Marks whether this is a release build |
| `IsRelease` | `func() bool` | Returns whether this is a release build |
| `IsReleasedVersion` | `func(string) bool` | Checks whether a version string is a release |

### Workflow Header Generation

| Function | Signature | Description |
|----------|-----------|-------------|
| `GenerateWorkflowHeader` | `func(sourceFile, generatedBy, customInstructions string) string` | Generates the standard ASCII-art + regeneration-instructions header comment for compiled lock files; `sourceFile` is the `.md` source path, `generatedBy` names the generator, and `customInstructions` is appended verbatim |

### Validation Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `ValidateEventFilters` | `func(map[string]any) error` | Validates `on:` event filter patterns |
| `ValidateGlobPatterns` | `func(map[string]any) error` | Validates glob patterns in trigger filters |
| `validateFileGlobPatterns` | `func([]string) error` | Validates `file-glob` patterns for a repo-memory entry; rejects absolute paths (starting with `/`) |

### Step Types

| Type | Kind | Description |
|------|------|-------------|
| `WorkflowStep` | struct | A single GitHub Actions step with all standard fields |
| `GitHubActionStep` | `[]string` | A multi-line run step (slice of command strings) |

| Function | Signature | Description |
|----------|-----------|-------------|
| `MapToStep` | `func(map[string]any) (*WorkflowStep, error)` | Converts a YAML map to a typed `WorkflowStep` |
| `SliceToSteps` | `func([]any) ([]*WorkflowStep, error)` | Converts a YAML slice to typed steps |
| `StepsToSlice` | `func([]*WorkflowStep) []any` | Converts typed steps back to a YAML slice |

### Repository Configuration

| Type | Kind | Description |
|------|------|-------------|
| `RepoConfig` | struct | Repository-level configuration from `.github/gh-aw.yml` |

| Function | Signature | Description |
|----------|-----------|-------------|
| `LoadRepoConfig` | `func(gitRoot string) (*RepoConfig, error)` | Loads and parses the repo config file |
| `FormatRunsOn` | `func(RunsOnValue, string) string` | Formats a `runs-on:` value for YAML output |

### Threat Detection

| Type | Kind | Description |
|------|------|-------------|
| `ThreatDetectionConfig` | struct | Configuration for the threat detection job |

| Function | Signature | Description |
|----------|-----------|-------------|
| `IsDetectionJobEnabled` | `func(*SafeOutputsConfig) bool` | Returns whether threat detection is enabled |

### Safe Update Manifest

| Type | Kind | Description |
|------|------|-------------|
| `GHAWManifest` | struct | Signed manifest embedded in lock files for integrity checking |

| Function | Signature | Description |
|----------|-----------|-------------|
| `NewGHAWManifest` | `func(secretNames, actionRefs []string, containers []GHAWManifestContainer) *GHAWManifest` | Creates a new manifest |
| `ExtractGHAWManifestFromLockFile` | `func(string) (*GHAWManifest, error)` | Extracts the manifest from a lock file |
| `EnforceSafeUpdate` | `func(*GHAWManifest, []string, []string) error` | Validates that a lock file update passes manifest checks |

## Usage Examples

### Compile a workflow file

```go
compiler := workflow.NewCompiler(
    workflow.WithVerbose(true),
    workflow.WithEngineOverride("copilot"),
)
err := compiler.CompileWorkflow(".github/workflows/my-workflow.md")
```

### Look up an engine

```go
registry := workflow.GetGlobalEngineRegistry()
engine, err := registry.GetEngine("copilot")
if err == nil {
    steps := engine.GetExecutionSteps(workflowData)
}
```

### Compute permissions for safe outputs

```go
perms := workflow.ComputePermissionsForSafeOutputs(safeOutputsConfig)
```

### Resolve a workflow name

```go
name, err := workflow.ResolveWorkflowName("my-workflow")
lockFile, err := workflow.GetWorkflowLockFileName(name)
```

## Architecture

```
markdown file
    │
    ▼
pkg/parser ─── ExtractFrontmatterFromContent
    │               ProcessImportsFromFrontmatterWithSource
    │
    ▼
pkg/workflow ── FrontmatterConfig (typed structs)
    │               Compiler.CompileWorkflow()
    │                 ├─ schema validation
    │                 ├─ permissions computation
    │                 ├─ engine step generation
    │                 ├─ safe-output job generation
    │                 ├─ YAML serialization
    │                 └─ lock file write (if changed)
    │
    ▼
.github/workflows/my-workflow.lock.yml
```

## Design Decisions

- **File-per-domain decomposition**: Each validation concern and job-builder lives in its own file. The 300-line limit is enforced by convention; validation files exceeding it SHOULD be split.
- **Functional compiler options**: `CompilerOption` functions follow the standard Go functional-options pattern, keeping `NewCompiler` signature stable as options are added.
- **Engine interface composition**: Rather than one monolithic `Engine` interface, capabilities are split into focused interfaces (`CapabilityProvider`, `WorkflowExecutor`, etc.) and combined via `CodingAgentEngine`. This prevents engines from being forced to implement unused methods.
- **Content-addressed lock files**: Lock files are only written when the normalized YAML content changes (heredoc delimiters are normalized before comparison). This avoids unnecessary git churn.
- **YAML 1.1/1.2 compatibility**: The package uses `goccy/go-yaml` for all GitHub Actions YAML generation to ensure compatibility with GitHub Actions' YAML parser.

## Dependencies

**Internal**:
- `github.com/github/gh-aw/pkg/parser` — frontmatter extraction and import processing
- `github.com/github/gh-aw/pkg/constants` — engine names, feature flags, job/step IDs
- `github.com/github/gh-aw/pkg/console` — terminal formatting
- `github.com/github/gh-aw/pkg/logger` — debug logging
- `github.com/github/gh-aw/pkg/actionpins` — action pin data and pin lookup helpers
- `github.com/github/gh-aw/pkg/jsonutil` — compact JSON marshaling for AWF configuration serialization
- `github.com/github/gh-aw/pkg/semverutil` — semantic version helpers
- `github.com/github/gh-aw/pkg/typeutil` — safe type conversions
- `github.com/github/gh-aw/pkg/tty` — terminal capability detection
- `github.com/github/gh-aw/pkg/workflow/compilerenv` — enterprise compiler-default and model-override helpers
- `github.com/github/gh-aw/pkg/stringutil`, `github.com/github/gh-aw/pkg/fileutil`, `github.com/github/gh-aw/pkg/gitutil`, `github.com/github/gh-aw/pkg/sliceutil` — utilities
- `github.com/github/gh-aw/pkg/syncutil` — thread-safe one-shot caching (used for repository feature cache)
- `github.com/github/gh-aw/pkg/types` — shared MCP types
- `github.com/github/gh-aw/pkg/importinpututil` — input-value resolution and formatting for `@import` directives
- `github.com/github/gh-aw/pkg/repoutil` — repository slug parsing and normalization
- `github.com/github/gh-aw/pkg/setutil` — set operations backed by `map[K]struct{}`
- `github.com/github/gh-aw/pkg/ctxutil` — non-nil `context.Context` fallback for optional context options

**Test-only**:
- `github.com/github/gh-aw/pkg/testutil` — shared test fixtures and assertion helpers used by workflow package tests

**External**:
- `github.com/goccy/go-yaml` — YAML 1.1/1.2 compatible marshaling
- `github.com/cli/go-gh/v2` — GitHub CLI API and repository integration
- `github.com/santhosh-tekuri/jsonschema/v6` — JSON schema validation

## Thread Safety

`Compiler` instances are NOT safe for concurrent use. Create a new `Compiler` for each concurrent compilation. The `GetGlobalEngineRegistry()` singleton is initialized once at startup and is safe for concurrent reads thereafter.

Constants (`MaxLockFileSize`) and action pin data are read-only after initialization and are safe for concurrent access.

### Additional exported helpers

- Safe-output builder methods on `handlerConfigBuilder`: `AddBoolPtr`, `AddBoolPtrOrDefault`, `AddDefault`, `AddIfNotEmpty`, `AddIfPositive`, `AddIfTrue`, `AddMapSlice`, `AddStringPtr`, `AddStringSlice`, `AddTemplatableBool`, `AddTemplatableBoolOrInt`, `AddTemplatableInt`, `AddTemplatableStringSlice`, `Build`
- Permission factory helpers: `NewPermissionsChecksWrite`, `NewPermissionsChecksWritePRRead`, `NewPermissionsDiscussionsWrite`, `NewPermissionsIssuesWrite`, `NewPermissionsIssuesWriteDiscussionsWrite`, `NewPermissionsIssuesWritePRWrite`, `NewPermissionsOrganizationProjWrite`, `NewPermissionsOrganizationProjWriteIssuesRead`, `NewPermissionsPRWrite`, `NewPermissionsSecurityEventsWrite`, `NewPermissionsSecurityEventsWriteActionsRead`
- Compiler/runtime methods and helpers: `SetBatchMode`, `SetCopilotTipNeeded`, `CopilotRequestsTipNeeded`, `SetExperimentalFeatureUsage`, `GetExperimentalFeatureUsage`, `WorkflowStateBranchName`, `GetCompiledVersionForEmission`, `GetValidationConfigJSONWithDataSchema`, `GenerateDockerSbxNpmCLIInstallStep`, `GetDockerSbxNpmCLIPathSetup`, `ContainsJobOutputExpr`, `IsIntentionalFailure`, `IsInPayload`, `ParseEvalsFromFrontmatter`, `ParseExperimentMetricEvalReference`, `GetSecretFailureMessage`, `HasAnyWriteScope`
- Exported `Get` methods are provided on multiple types: `ActionCache.Get`, `EngineCatalog.Get`, and `Permissions.Get`

<!-- BEGIN SOURCE-VERIFIED EXPORT COVERAGE -->
## Source-verified export coverage

This appendix is generated from the current non-test Go source files in this package and records any exported top-level symbols that are not already described above.

| Category | Count |
|----------|------:|
| Types | 337 |
| Constants | 151 |
| Variables | 43 |
| Functions and methods | 670 |
| Additional symbols documented in this appendix | 857 |

### Additional types

| File | Symbol | Declaration | Description |
|------|--------|-------------|-------------|
| `action_cache.go` | `ActionCacheEntry` | `type ActionCacheEntry struct { Repo string `json:"repo"` Version string `json:"version"` SHA string `json:"sha"` ReleasedAt *time.Time `json:"released_at,omitempty"` // publication date of this release, used for cooldown checks Inputs map[string]*ActionYAMLInput `json:"inputs,omitempty"` // cached inputs from action.yml ActionDescription string `json:"action_description,omitempty"` // cached description from action.yml }` | ActionCacheEntry represents a cached action pin resolution. |
| `action_pins.go` | `ActionYAMLInput` | `type ActionYAMLInput actionpins.ActionYAMLInput` | ActionYAMLInput is defined in pkg/actionpins; aliased here so all files in pkg/workflow (action_cache. |
| `action_pins.go` | `ContainerPin` | `type ContainerPin actionpins.ContainerPin` | ContainerPin is the pinned container image type from pkg/actionpins. |
| `action_pins.go` | `SHAResolver` | `type SHAResolver actionpins.SHAResolver` | SHAResolver is the interface for resolving a GitHub Action's commit SHA for a given version tag. |
| `agentic_engine.go` | `EngineCapabilities` | `type EngineCapabilities struct { // ToolsAllowlist reports whether the engine supports MCP tool allow-listing. ToolsAllowlist bool // MaxTurns reports whether the engine supports the max-turns feature. MaxTurns bool // WebSearch reports whether the engine has built-in support for the web-search tool. WebSearch bool // MaxContinuations reports whether the engine supports the max-continuations feature. // When true, max-continuations > 1 enables autopilot/multi-run mode for the engine. MaxContinuations bool // NativeAgentFile reports whether the engine handles agent-file imports natively // in its own execution steps (reading the file, stripping frontmatter, and prepending the // content to the prompt at runtime). When false, the compiler is responsible for including // the agent file content in prompt.txt during the activation job so that the engine just // reads the standard /tmp/gh-aw/aw-prompts/prompt.txt as usual. NativeAgentFile bool // BareMode reports whether the engine supports the bare mode feature (engine.bare: true), // which suppresses automatic loading of context and custom instructions. When false, // specifying bare: true emits a warning and has no effect. BareMode bool }` | EngineCapabilities captures optional engine features. |
| `agentic_engine.go` | `LLMProviderResolver` | `type LLMProviderResolver interface { // ResolveLLMProvider returns the effective provider for the workflow // (for example "github", "anthropic", or "openai"). ResolveLLMProvider(workflowData *WorkflowData) string }` | LLMProviderResolver is implemented by engines that support selecting different inference providers at runtime (for example engine. |
| `artifact_manager.go` | `ArtifactDownload` | `type ArtifactDownload struct { // Name is the artifact name to download (optional if using Pattern) Name string // Pattern is a glob pattern to match multiple artifacts (v4 feature) Pattern string // Path is where the artifact will be downloaded Path string // MergeMultiple determines if multiple artifacts should be merged // into the same directory (only applies when using Pattern) MergeMultiple bool // JobName is the name of the job downloading this artifact JobName string // DependsOn lists job names this job depends on (from needs:) DependsOn []string }` | ArtifactDownload represents an artifact download operation |
| `artifact_manager.go` | `ArtifactFile` | `type ArtifactFile struct { // ArtifactName is the name of the artifact containing this file ArtifactName string // OriginalPath is the path as uploaded OriginalPath string // DownloadPath is the computed path after download DownloadPath string // JobName is the job that uploaded this file JobName string }` | ArtifactFile represents a file within an artifact |
| `artifact_manager.go` | `ArtifactManager` | `type ArtifactManager struct { // uploads tracks all artifact uploads by job name uploads map[string][]*ArtifactUpload // downloads tracks all artifact downloads by job name downloads map[string][]*ArtifactDownload // currentJob tracks the job currently being processed currentJob string }` | ArtifactManager simulates the behavior of actions/upload-artifact and actions/download-artifact to track artifacts and compute actual file locations during compilation. |
| `artifact_manager.go` | `ArtifactUpload` | `type ArtifactUpload struct { // Name is the artifact name (e.g., "agent") Name string // Paths are the file/directory paths being uploaded // These can be absolute paths or glob patterns Paths []string // NormalizedPaths are the paths after common parent directory removal // This simulates GitHub Actions behavior where the common parent is stripped NormalizedPaths map[string]string // IfNoFilesFound specifies behavior when no files match // Values: "warn", "error", "ignore" IfNoFilesFound string // IncludeHiddenFiles determines if hidden files are included IncludeHiddenFiles bool // JobName is the name of the job uploading this artifact JobName string }` | ArtifactUpload represents an artifact upload operation |
| `artifacts.go` | `ArtifactDownloadConfig` | `type ArtifactDownloadConfig struct { ArtifactName string // Name of the artifact to download (e.g., "agent-output", "prompt") ArtifactFilename string // Filename inside the artifact directory (e.g., "agent_output.json", "prompt.txt") DownloadPath string // Path where artifact will be downloaded (e.g., "/tmp/gh-aw/safeoutputs/") SetupEnvStep bool // Whether to add environment variable setup step EnvVarName string // Environment variable name to set (e.g., "GH_AW_AGENT_OUTPUT") StepName string // Optional custom step name (defaults to "Download {artifact} artifact") IfCondition string // Optional conditional expression for the step (e.g., "needs.agent.outputs.has_patch == 'true'") StepID string // Optional step ID; when set, the env-setup step is gated on this step's success }` | ArtifactDownloadConfig holds configuration for building artifact download steps |
| `auto_update_workflow.go` | `GenerateAutoUpdateWorkflowOptions` | `type GenerateAutoUpdateWorkflowOptions struct { // Context is used for action reference resolution in non-dev modes. // When nil, context.Background() is used. Context context.Context // WorkflowDir is the directory where the workflow file will be written. WorkflowDir string // Enabled indicates whether auto-updates are enabled in the repo config. Enabled bool // RepoSlug is the owner/repo slug used to deterministically scatter the // weekly cron schedule across different repositories. Pass an empty string // when the slug is not available; scattering will still succeed using only // the workflow identifier as seed. RepoSlug string // SetupActionRef is the resolved reference for the gh-aw actions/setup action. // For example: "./actions/setup" (dev mode) or "github/gh-aw/actions/setup@<sha>" (release mode). // When empty, "./actions/setup" is used as a fallback. SetupActionRef string // GitHubScriptPin is the pinned reference for actions/github-script. // When empty, getActionPin("actions/github-script") is used as a fallback. GitHubScriptPin string // ActionMode controls how CLI install steps and command prefixes are generated. // Defaults to ActionModeDev when empty. ActionMode ActionMode // Version is the gh-aw version used by generateInstallCLISteps in non-dev modes. Version string // ActionTag optionally overrides the setup-cli version tag in non-dev modes. ActionTag string // Resolver optionally resolves setup-cli action tags to SHA-pinned refs. Resolver SHAResolver }` | GenerateAutoUpdateWorkflowOptions configures an auto-update workflow generation run. |
| `awf_config.go` | `AWFAPIProxyConfig` | `type AWFAPIProxyConfig struct { // Enabled enables the API proxy sidecar for LLM gateway credential isolation. // Maps to: --enable-api-proxy Enabled bool `json:"enabled"` // EnableTokenSteering enables budget-warning system message injection near ET budget exhaustion. EnableTokenSteering bool `json:"enableTokenSteering,omitempty"` // MaxRuns is the maximum number of LLM invocations allowed for a run. MaxRuns int `json:"maxRuns,omitempty"` // MaxTurnCacheMisses is the maximum number of consecutive cache misses allowed for a run. MaxTurnCacheMisses int `json:"maxCacheMisses,omitempty"` // MaxAICredits is the explicit per-run AI credits budget enforced by the API proxy. MaxAICredits int64 `json:"maxAiCredits,omitempty"` // ModelFallback configures the model fallback policy for unresolved model selections. // When nil, the AWF default (enabled=true, strategy=middle_power) is used. // Set enabled=false to prevent AWF from silently rewriting deployment names, which // is needed for BYOK Azure OpenAI deployments where rewriting causes HTTP 404. ModelFallback *AWFModelFallbackConfig `json:"modelFallback,omitempty"` // ModelMultipliers configures per-model ET accounting multipliers in AWF. ModelMultipliers map[string]float64 `json:"modelMultipliers,omitempty"` // Targets holds per-provider API target overrides. // Supported keys: "openai", "anthropic", "copilot", "gemini" Targets map[string]*AWFAPITargetConfig `json:"targets,omitempty"` // Models contains model alias and fallback policy definitions. // Keys are alias names (empty string "" = default policy); values are ordered // lists of vendor/modelid patterns or other alias names to try in sequence. // AWF resolves aliases recursively; loops are not permitted. // Per the AWF config schema, this lives under apiProxy.models. Models map[string][]string `json:"models,omitempty"` // AllowedModels is the explicit allowlist policy for model names/patterns. AllowedModels []string `json:"allowedModels,omitempty"` // DisallowedModels is the explicit denylist policy for model names/patterns. DisallowedModels []string `json:"disallowedModels,omitempty"` }` | AWFAPIProxyConfig is the "apiProxy" section of the AWF config file. |
| `awf_config.go` | `AWFAPITargetConfig` | `type AWFAPITargetConfig struct { // Host is the hostname (and optional port) of the API endpoint. Host string `json:"host,omitempty"` // AuthHeader is the custom authentication header name sent with API requests. // When set, the raw agent ID is sent as "<authHeader>: <key>" instead of the // provider default (e.g. "Authorization: ******" for OpenAI, or // "x-api-key: <key>" for Anthropic). This supports gateways like Azure OpenAI // that require "api-key: <rawkey>" in place of the standard provider scheme. // Maps to: --openai-api-auth-header / --anthropic-api-auth-header AuthHeader string `json:"authHeader,omitempty"` // ExtraHeaders holds additional non-sensitive headers injected on Copilot BYOK upstream // requests. Only valid for the "copilot" provider target. // Maps to AWF_BYOK_EXTRA_HEADERS in the sidecar. ExtraHeaders map[string]string `json:"extraHeaders,omitempty"` // ExtraBodyFields holds additional non-sensitive JSON body fields injected on Copilot BYOK // upstream requests. Only valid for the "copilot" provider target. // Maps to AWF_BYOK_EXTRA_BODY_FIELDS in the sidecar. ExtraBodyFields map[string]string `json:"extraBodyFields,omitempty"` // SessionId is an opt-in session identifier injected as the x-session-id request header // and session_id body field on Copilot BYOK upstream requests. Only valid for the // "copilot" provider target. // Maps to AWF_PROVIDER_SESSION_ID in the sidecar. SessionId string `json:"sessionId,omitempty"` }` | AWFAPITargetConfig is a single API proxy target entry. |
| `awf_config.go` | `AWFChrootConfig` | `type AWFChrootConfig struct { // BinariesSourcePath is the runner-side directory to overlay at /usr/local/bin // inside chroot mode for split-filesystem ARC/DinD runners. BinariesSourcePath string `json:"binariesSourcePath,omitempty"` // Identity configures identity values applied after chroot pivot to override // HOME/USER/LOGNAME defaults inside chroot mode. Identity *AWFChrootIdentityConfig `json:"identity,omitempty"` }` | AWFChrootConfig is the "chroot" section of the AWF config file. |
| `awf_config.go` | `AWFChrootIdentityConfig` | `type AWFChrootIdentityConfig struct { // User is the USER/LOGNAME string to export inside chroot mode. User string `json:"user,omitempty"` // UID is the UID hint used for chroot identity synthesis and user switching. // Must be >= 1 (root is not supported). UID int `json:"uid,omitempty"` // GID is the GID hint used for chroot identity synthesis and user switching. // Must be >= 1. GID int `json:"gid,omitempty"` // Home is the home directory path to export inside chroot mode. Home string `json:"home,omitempty"` }` | AWFChrootIdentityConfig is the "chroot. |
| `awf_config.go` | `AWFConfigFile` | `type AWFConfigFile struct { // Schema is the JSON schema reference for IDE auto-complete support. Schema string `json:"$schema,omitempty"` // Runner contains runner topology metadata that AWF uses to activate // topology-specific behaviors (split-filesystem handling, network isolation, // tool cache redirection, sysroot image selection). Runner *AWFRunnerConfig `json:"runner,omitempty"` // Network contains network egress control configuration. Network *AWFNetworkConfig `json:"network,omitempty"` // Platform contains GitHub deployment metadata used by AWF auth handling. Platform *AWFPlatformConfig `json:"platform,omitempty"` // APIProxy contains API proxy (LLM gateway) configuration. APIProxy *AWFAPIProxyConfig `json:"apiProxy,omitempty"` // Container contains container execution configuration. Container *AWFContainerConfig `json:"container,omitempty"` // Logging contains logging and diagnostics configuration. Logging *AWFLoggingConfig `json:"logging,omitempty"` // Chroot contains chroot execution overrides for split-filesystem ARC/DinD runners. // This field is not populated at compile time; it is injected at runtime when DinD topology is detected. Chroot *AWFChrootConfig `json:"chroot,omitempty"` }` | AWFConfigFile represents the AWF configuration file schema. |
| `awf_config.go` | `AWFContainerConfig` | `type AWFContainerConfig struct { // ImageTag is the pinned AWF Docker image tag, with optional digest metadata. // Format: "<tag>" or "<tag>,squid=sha256:...,agent=sha256:..." // Maps to: --image-tag <value> ImageTag string `json:"imageTag,omitempty"` // DockerHostPathPrefix prefixes bind-mount source paths so the Docker daemon can // resolve runner filesystem paths. Required for ARC DinD sidecar runners where the // runner and daemon have separate filesystems. // Maps to: --docker-host-path-prefix <value> DockerHostPathPrefix string `json:"dockerHostPathPrefix,omitempty"` // ContainerRuntime specifies the OCI runtime for the agent container. // "gvisor" enables gVisor's runsc runtime for additional kernel-level isolation. // AWF translates "gvisor" → "runsc" internally. ContainerRuntime string `json:"containerRuntime,omitempty"` }` | AWFContainerConfig is the "container" section of the AWF config file. |
| `awf_config.go` | `AWFLoggingConfig` | `type AWFLoggingConfig struct { // ProxyLogsDir is the directory path for Squid proxy access logs. // Maps to: --proxy-logs-dir <path> ProxyLogsDir string `json:"proxyLogsDir,omitempty"` // AuditDir is the directory path for audit logs (policy-manifest.json, squid.conf, etc). // Maps to: --audit-dir <path> AuditDir string `json:"auditDir,omitempty"` }` | AWFLoggingConfig is the "logging" section of the AWF config file. |
| `awf_config.go` | `AWFModelFallbackConfig` | `type AWFModelFallbackConfig struct { // Enabled controls whether middle-power fallback is applied when model resolution fails. // It accepts literal booleans and GitHub Actions expressions. A nil value omits the field, // letting AWF use its default. Enabled *TemplatableBool `json:"enabled,omitempty"` }` | AWFModelFallbackConfig is the "apiProxy. |
| `awf_config.go` | `AWFNetworkConfig` | `type AWFNetworkConfig struct { // AllowDomains is the list of allowed egress domains. // Supports wildcards (e.g. "*.github.com") and exact matches. // Maps to: --allow-domains <comma-separated> AllowDomains []string `json:"allowDomains,omitempty"` // BlockDomains is the list of explicitly blocked egress domains. // Maps to: --block-domains <comma-separated> BlockDomains []string `json:"blockDomains,omitempty"` // Isolation enables topology-based egress isolation mode. // Maps to: --network-isolation Isolation bool `json:"isolation,omitempty"` // TopologyAttach lists container names AWF should attach to awf-net. // Maps to: --topology-attach <name> (repeatable) TopologyAttach []string `json:"topologyAttach,omitempty"` }` | AWFNetworkConfig is the "network" section of the AWF config file. |
| `awf_config.go` | `AWFPlatformConfig` | `type AWFPlatformConfig struct { // Type is the GitHub deployment type consumed by AWF for auth behavior. Type string `json:"type,omitempty"` }` | AWFPlatformConfig is the "platform" section of the AWF config file. |
| `awf_config.go` | `AWFRunnerConfig` | `type AWFRunnerConfig struct { // Topology identifies the runner execution topology. // Currently supported values: "arc-dind" (ARC with Docker-in-Docker sidecar). // When set to "arc-dind", AWF activates split-filesystem handling, network // isolation, sysroot image staging, and DinD pre-staging automatically. Topology string `json:"topology,omitempty"` }` | AWFRunnerConfig is the "runner" section of the AWF config file. |
| `behavior_defined_engine.go` | `BehaviorDefinedEngine` | `type BehaviorDefinedEngine struct { UniversalLLMConsumerEngine definition *EngineDefinition }` | BehaviorDefinedEngine is a declarative CodingAgentEngine built from an engine definition's behaviors block. |
| `cache.go` | `CacheMemoryConfig` | `type CacheMemoryConfig struct { Caches []CacheMemoryEntry `yaml:"caches,omitempty"` // cache configurations }` | CacheMemoryConfig holds configuration for cache-memory functionality |
| `cache.go` | `CacheMemoryEntry` | `type CacheMemoryEntry struct { ID string `yaml:"id"` // cache identifier (required for array notation) Key string `yaml:"key,omitempty"` // custom cache key Description string `yaml:"description,omitempty"` // optional description for this cache RetentionDays *int `yaml:"retention-days,omitempty"` // retention days for upload-artifact action RestoreOnly bool `yaml:"restore-only,omitempty"` // if true, only restore cache without saving Scope string `yaml:"scope,omitempty"` // scope for restore keys: "workflow" (default) or "repo" AllowedExtensions []string `yaml:"allowed-extensions,omitempty"` // allowed file extensions (default: [".json", ".jsonl", ".txt", ".md", ".csv"]) }` | CacheMemoryEntry represents a single cache-memory configuration |
| `call_workflow.go` | `CallWorkflowConfig` | `type CallWorkflowConfig struct { BaseSafeOutputConfig `yaml:",inline"` Workflows []string `yaml:"workflows,omitempty"` // List of workflow names (without .md extension) to allow calling WorkflowFiles map[string]string `yaml:"workflow_files,omitempty"` // Map of workflow name to file path (relative, e.g. ./.github/workflows/x.lock.yml) - populated at compile time }` | CallWorkflowConfig holds configuration for calling workflows via workflow_call chaining. |
| `compiler_experiments.go` | `ExperimentStorageMode` | `type ExperimentStorageMode string` | ExperimentStorageMode controls how experiment state is persisted across runs. |
| `copilot_logs.go` | `SessionContent` | `type SessionContent struct { Type string `json:"type"` Text string `json:"text,omitempty"` ID string `json:"id,omitempty"` Name string `json:"name,omitempty"` Input map[string]any `json:"input,omitempty"` ToolUseID string `json:"tool_use_id,omitempty"` Content string `json:"content,omitempty"` }` | SessionContent represents content items in messages |
| `copilot_logs.go` | `SessionEntry` | `type SessionEntry struct { Type string `json:"type"` Subtype string `json:"subtype,omitempty"` Message *SessionMessage `json:"message,omitempty"` Usage *SessionUsage `json:"usage,omitempty"` NumTurns int `json:"num_turns,omitempty"` RawData map[string]any `json:"-"` }` | SessionEntry represents a single entry in a Copilot session JSONL file |
| `copilot_logs.go` | `SessionMessage` | `type SessionMessage struct { Content []SessionContent `json:"content"` }` | SessionMessage represents the message field in session entries |
| `copilot_logs.go` | `SessionUsage` | `type SessionUsage struct { InputTokens int `json:"input_tokens"` OutputTokens int `json:"output_tokens"` }` | SessionUsage represents token usage in a session result entry |
| `dependabot.go` | `DependabotConfig` | `type DependabotConfig struct { Version int `yaml:"version"` Updates []DependabotUpdateEntry `yaml:"updates"` }` | DependabotConfig represents the structure of . |
| `dependabot.go` | `DependabotUpdateEntry` | `type DependabotUpdateEntry struct { PackageEcosystem string `yaml:"package-ecosystem"` Directory string `yaml:"directory"` Schedule struct { Interval string `yaml:"interval"` } `yaml:"schedule"` }` | DependabotUpdateEntry represents a single update configuration in dependabot. |
| `dependabot.go` | `GoDependency` | `type GoDependency struct { Path string // import path (e.g., github.com/user/repo) Version string // version or pseudo-version }` | GoDependency represents a parsed Go package |
| `dependabot.go` | `NpmDependency` | `type NpmDependency struct { Name string Version string // semver range or specific version }` | NpmDependency represents a parsed npm package with version |
| `dependabot.go` | `PackageJSON` | `type PackageJSON struct { Name string `json:"name"` Private bool `json:"private"` License string `json:"license,omitempty"` Dependencies map[string]string `json:"dependencies,omitempty"` DevDependencies map[string]string `json:"devDependencies,omitempty"` }` | PackageJSON represents the structure of a package. |
| `dependabot.go` | `PipDependency` | `type PipDependency struct { Name string Version string // version specifier (e.g., ==1.0.0, >=2.0.0) }` | PipDependency represents a parsed pip package with version |
| `engine_definition.go` | `AuthBinding` | `type AuthBinding struct { Role string `yaml:"role"` Secret string `yaml:"secret"` }` | AuthBinding maps a logical authentication role to a secret name. |
| `engine_definition.go` | `EngineBehaviorDefinition` | `type EngineBehaviorDefinition struct { SecretStrategy string `yaml:"secret-strategy,omitempty"` SupportedEnvVarKeys []string `yaml:"supported-env-var-keys,omitempty"` Capabilities EngineCapabilitiesDefinition `yaml:"capabilities,omitempty"` Manifest *EngineManifestDefinition `yaml:"manifest,omitempty"` Installation *EngineInstallationDefinition `yaml:"installation,omitempty"` ConfigFile *EngineConfigFileDefinition `yaml:"config-file,omitempty"` Execution *EngineExecutionDefinition `yaml:"execution,omitempty"` MCP *EngineMCPDefinition `yaml:"mcp,omitempty"` // HarnessScript is the JavaScript source of a Node.js harness that spawns the // engine CLI. When non-empty the script is written to // ${RUNNER_TEMP}/gh-aw/actions/<engine-id>_harness.cjs before execution and the // engine is launched via: // node <harness-path> <command-name> [args...] // The harness can read process.env.GH_AW_PROMPT for the prompt-file path and // process.env.AWF_REFLECT_ENABLED / the AWF reflect JSON file to dynamically // configure the engine CLI at runtime. HarnessScript string `yaml:"harness-script,omitempty"` }` | EngineBehaviorDefinition captures declarative runtime behaviour for a custom engine definition. |
| `engine_definition.go` | `EngineCapabilitiesDefinition` | `type EngineCapabilitiesDefinition struct { ToolsAllowlist bool `yaml:"tools-allowlist,omitempty"` MaxTurns bool `yaml:"max-turns,omitempty"` WebSearch bool `yaml:"web-search,omitempty"` MaxContinuations bool `yaml:"max-continuations,omitempty"` NativeAgentFile bool `yaml:"native-agent-file,omitempty"` BareMode bool `yaml:"bare-mode,omitempty"` }` | EngineCapabilitiesDefinition captures declarative engine capabilities loaded from engine definition frontmatter. |
| `engine_definition.go` | `EngineConfigFileDefinition` | `type EngineConfigFileDefinition struct { Path string `yaml:"path,omitempty"` StepName string `yaml:"step-name,omitempty"` Content string `yaml:"content,omitempty"` MergeStrategy string `yaml:"merge-strategy,omitempty"` }` | EngineConfigFileDefinition describes a configuration file that should be written before executing the engine CLI. |
| `engine_definition.go` | `EngineExecutionDefinition` | `type EngineExecutionDefinition struct { CommandName string `yaml:"command-name,omitempty"` Args []string `yaml:"args,omitempty"` StepName string `yaml:"step-name,omitempty"` ModelEnvVarName string `yaml:"model-env-var,omitempty"` ModelEnvProviderPrefix string `yaml:"model-env-provider-prefix,omitempty"` ModelFlag string `yaml:"model-flag,omitempty"` MCPConfigEnvVar string `yaml:"mcp-config-env-var,omitempty"` MCPConfigFlag string `yaml:"mcp-config-flag,omitempty"` WriteTimestamp bool `yaml:"write-timestamp,omitempty"` ProviderEnvMode string `yaml:"provider-env-mode,omitempty"` // Env holds additional static environment variables to inject into the // execution step. Values are rendered verbatim and are not filtered // through the secrets allowlist, so they must not contain secret values. Env map[string]string `yaml:"env,omitempty"` }` | EngineExecutionDefinition describes the common CLI execution pattern used by behavior-defined engines. |
| `engine_definition.go` | `EngineInstallationDefinition` | `type EngineInstallationDefinition struct { PackageManager string `yaml:"package-manager,omitempty"` PackageName string `yaml:"package-name,omitempty"` Version string `yaml:"version,omitempty"` StepName string `yaml:"step-name,omitempty"` BinaryName string `yaml:"binary-name,omitempty"` IncludeNodeSetup bool `yaml:"include-node-setup,omitempty"` PostInstallScripts bool `yaml:"post-install-scripts,omitempty"` Cooldown bool `yaml:"cooldown,omitempty"` VerifyCommand string `yaml:"verify-command,omitempty"` VerifyStepName string `yaml:"verify-step-name,omitempty"` DocumentationURL string `yaml:"docs-url,omitempty"` }` | EngineInstallationDefinition describes how an engine CLI is installed. |
| `engine_definition.go` | `EngineMCPDefinition` | `type EngineMCPDefinition struct { ConfigPath string `yaml:"config-path,omitempty"` }` | EngineMCPDefinition describes how to render MCP configuration for a behavior-defined engine. |
| `engine_definition.go` | `EngineManifestDefinition` | `type EngineManifestDefinition struct { Files []string `yaml:"files,omitempty"` PathPrefixes []string `yaml:"path-prefixes,omitempty"` }` | EngineManifestDefinition describes engine-specific files and folders that alter agent behaviour and must be protected from untrusted pull requests. |
| `error_recovery.go` | `ErrorSeverity` | `type ErrorSeverity int` | ErrorSeverity classifies how urgently a compilation error should be fixed. |
| `error_recovery.go` | `PrioritizedError` | `type PrioritizedError struct { Message string Severity ErrorSeverity Category string Suggestion string }` | PrioritizedError describes a single user-facing error after severity sorting. |
| `error_recovery.go` | `PrioritizedErrorReport` | `type PrioritizedErrorReport struct { TotalCount int DisplayedErrors []PrioritizedError HiddenCount int SuppressedCount int RecoveryPlan *RecoveryPlan }` | PrioritizedErrorReport contains the final prioritized compilation report. |
| `error_recovery.go` | `RecoveryPlan` | `type RecoveryPlan struct { Steps []string }` | RecoveryPlan describes the recommended next steps for a set of related errors. |
| `evals_config.go` | `EvalDefinition` | `type EvalDefinition struct { ID string Question string // Model is an optional per-question model override. When set, it takes precedence over // EvalsConfig.Model. Use a model alias such as "small" or a full model ID. Model string }` | EvalDefinition represents a single binary evaluation question in a BinEval workflow. |
| `evals_config.go` | `EvalsConfig` | `type EvalsConfig struct { // Questions is the ordered list of binary evaluation questions. Questions []EvalDefinition // Model is the default LLM model to use for evaluations. Use a model alias such as // "small" or a full model ID. Per-question Model fields override this value. // When empty, the compiler default ("small") is used. Model string // RunsOn allows overriding the runner for the evals job. RunsOn string }` | EvalsConfig holds the configuration for BinEval-style evaluations declared in workflow frontmatter. |
| `expression_extraction.go` | `ExpressionExtractor` | `type ExpressionExtractor struct { mappings map[string]*ExpressionMapping // key is the original expression counter int }` | ExpressionExtractor extracts GitHub Actions expressions from markdown content and creates environment variable mappings for them |
| `expression_extraction.go` | `ExpressionMapping` | `type ExpressionMapping struct { Original string // The original ${{ ... }} expression EnvVar string // The GH_AW_ prefixed environment variable name Content string // The expression content without ${{ }} }` | ExpressionMapping represents a mapping between a GitHub expression and its environment variable |
| `expression_nodes.go` | `AndNode` | `type AndNode struct { Left, Right ConditionNode }` | AndNode represents an AND operation between two conditions |
| `expression_nodes.go` | `BooleanLiteralNode` | `type BooleanLiteralNode struct { Value bool }` | BooleanLiteralNode represents a boolean literal value |
| `expression_nodes.go` | `ComparisonNode` | `type ComparisonNode struct { Left ConditionNode Operator string Right ConditionNode }` | ComparisonNode represents comparison operations like ==, ! |
| `expression_nodes.go` | `ConditionNode` | `type ConditionNode interface { Render() string }` | ConditionNode represents a node in a condition expression tree |
| `expression_nodes.go` | `DisjunctionNode` | `type DisjunctionNode struct { Terms []ConditionNode Multiline bool // If true, render each term on separate line with comments }` | DisjunctionNode represents an OR operation with multiple terms to avoid deep nesting |
| `expression_nodes.go` | `ExpressionNode` | `type ExpressionNode struct { Expression string Description string // Optional comment/description for the expression }` | ExpressionNode represents a leaf expression |
| `expression_nodes.go` | `FunctionCallNode` | `type FunctionCallNode struct { FunctionName string Arguments []ConditionNode }` | FunctionCallNode represents a function call expression like contains(array, value) |
| `expression_nodes.go` | `NotNode` | `type NotNode struct { Child ConditionNode }` | NotNode represents a NOT operation on a condition |
| `expression_nodes.go` | `OrNode` | `type OrNode struct { Left, Right ConditionNode }` | OrNode represents an OR operation between two conditions |
| `expression_nodes.go` | `PropertyAccessNode` | `type PropertyAccessNode struct { PropertyPath string }` | PropertyAccessNode represents property access like github. |
| `expression_nodes.go` | `StringLiteralNode` | `type StringLiteralNode struct { Value string }` | StringLiteralNode represents a string literal value |
| `expression_parser.go` | `ExpressionParser` | `type ExpressionParser struct { tokens []token pos int }` | ExpressionParser handles parsing of expression strings into ConditionNode trees |
| `firewall.go` | `FirewallConfig` | `type FirewallConfig struct { Enabled bool `yaml:"enabled,omitempty"` // Enable/disable AWF (default: true for copilot when network restrictions present) Version string `yaml:"version,omitempty"` // AWF version (empty = latest) Args []string `yaml:"args,omitempty"` // Additional arguments to pass to AWF LogLevel string `yaml:"log_level,omitempty"` // AWF log level (default: "info") SSLBump bool `yaml:"ssl_bump,omitempty"` // AWF-only: Enable SSL Bump for HTTPS content inspection (allows URL path filtering) AllowURLs []string `yaml:"allow_urls,omitempty"` // AWF-only: URL patterns to allow for HTTPS (requires SSLBump), e.g., "https://github.com/githubnext/*" }` | FirewallConfig represents AWF (gh-aw-firewall) configuration for network egress control. |
| `frontmatter_types.go` | `ExperimentConfig` | `type ExperimentConfig struct { // Variants is the ordered list of variant strings for this experiment (required, ≥ 2). Variants []string `json:"variants"` // Description is a human-readable explanation of what the experiment tests. Description string `json:"description,omitempty"` // Hypothesis states the null and alternative hypotheses for the experiment. // e.g. "H0: no change in effective_tokens. H1: concise reduces tokens by >=15%" Hypothesis string `json:"hypothesis,omitempty"` // Metric names the primary metric that should be observed (e.g. "effective_tokens"). Metric string `json:"metric,omitempty"` // SecondaryMetrics lists additional metrics to track alongside the primary metric. SecondaryMetrics []string `json:"secondary_metrics,omitempty"` // GuardrailMetrics defines thresholds that must not degrade during the experiment. // If any guardrail is violated the experiment should be aborted. GuardrailMetrics []GuardrailMetric `json:"guardrail_metrics,omitempty"` // MinSamples is the minimum number of runs required per variant before // statistical analysis is considered reliable. MinSamples int `json:"min_samples,omitempty"` // Weight holds an optional per-variant probability weight. When provided its length // must equal the length of Variants. Values are relative (they need not sum to 100). Weight []int `json:"weight,omitempty"` // Issue is an optional GitHub issue number that tracks this experiment. Issue int `json:"issue,omitempty"` // StartDate is an optional ISO-8601 date (YYYY-MM-DD) before which the experiment // is not active. When today is before this date the control variant (first variant) // is used. StartDate string `json:"start_date,omitempty"` // EndDate is an optional ISO-8601 date (YYYY-MM-DD) after which the experiment is // no longer active. When today is after this date the control variant is used. EndDate string `json:"end_date,omitempty"` // AnalysisType declares the statistical test used by automated reporting tooling. // Valid values: t_test, mann_whitney, proportion_test, bayesian_ab. AnalysisType string `json:"analysis_type,omitempty"` // Tags are free-form labels for filtering experiments in dashboards. Tags []string `json:"tags,omitempty"` // Notify specifies where to post significance alerts when the experiment concludes. Notify *ExperimentNotify `json:"notify,omitempty"` }` | ExperimentConfig represents the rich metadata for a single A/B experiment. |
| `frontmatter_types.go` | `ExperimentNotify` | `type ExperimentNotify struct { // Discussion is a GitHub discussion number to post a significance comment to. Discussion int `json:"discussion,omitempty"` // Issue is a GitHub issue number to post a significance comment to. Issue int `json:"issue,omitempty"` }` | ExperimentNotify specifies where to post significance alerts when an experiment reaches statistical significance. |
| `frontmatter_types.go` | `GuardrailMetric` | `type GuardrailMetric struct { // Name is the metric to guard (e.g. "success_rate", "empty_output_rate"). Name string `json:"name"` // Direction declares whether lower or higher values are preferred ("min" or "max"). Direction string `json:"direction,omitempty"` // Threshold is a comparison expression (e.g. ">=0.95", "==0"). Threshold string `json:"threshold"` }` | GuardrailMetric defines a metric threshold that must not degrade during an experiment. |
| `frontmatter_types.go` | `OTLPEndpointConfig` | `type OTLPEndpointConfig struct { // URL is the OTLP collector endpoint URL (e.g. "https://traces.example.com:4317"). // Supports GitHub Actions expressions such as ${{ secrets.OTLP_ENDPOINT }}. // When a static URL is provided, its hostname is automatically added to the // network firewall allowlist. URL string `json:"url,omitempty"` // Headers holds HTTP headers to include with every OTLP export request for this endpoint. // Same format as OTLPConfig.Headers: preferred map form or deprecated comma-separated string. Headers any `json:"headers,omitempty"` }` | OTLPEndpointConfig holds configuration for a single OTLP endpoint entry used when the `endpoint` field is an object or an element of an array. |
| `frontmatter_types.go` | `OTLPGitHubAppConfig` | `type OTLPGitHubAppConfig struct { // Audience is an optional OIDC audience passed to core.getIDToken(audience). Audience string `json:"audience,omitempty"` }` | OTLPGitHubAppConfig holds optional runtime GitHub app auth configuration for OTLP export. |
| `frontmatter_types.go` | `RunnerConfig` | `type RunnerConfig struct { // Topology identifies the runner execution topology. // Supported values: "arc-dind" (ARC with Docker-in-Docker sidecar). Topology string `json:"topology,omitempty" yaml:"topology,omitempty"` }` | RunnerConfig represents runner topology configuration from the workflow frontmatter. |
| `inputs.go` | `InputDefinition` | `type InputDefinition types.InputDefinition` | InputDefinition defines an input parameter for workflows, safe-jobs, and imported workflows. |
| `jobs.go` | `Job` | `type Job struct { Name string DisplayName string // Optional display name for the job (name property in YAML) RunsOn string If string HasWorkflowRunSafetyChecks bool // If true, the job's if condition includes workflow_run safety checks PermissionsComment string Permissions string TimeoutMinutes int TimeoutMinutesExpression string Concurrency string // Job-level concurrency configuration Environment string // Job environment configuration Strategy string // Job strategy configuration (matrix strategy) Container string // Job container configuration Services string // Job services configuration Env map[string]string // Job-level environment variables ContinueOnError *bool // continue-on-error flag for the job (nil means unset) Steps []string Needs []string // Job dependencies (needs clause) Outputs map[string]string // Reusable workflow call properties Uses string // Path to reusable workflow (e.g., ./.github/workflows/reusable.yml) With map[string]any // Input parameters for reusable workflow Secrets map[string]string // Secrets for reusable workflow (explicit mappings) SecretsInherit bool // When true, emits "secrets: inherit" (passes all caller secrets) }` | Job represents a GitHub Actions job with all its properties |
| `jobs.go` | `JobManager` | `type JobManager struct { jobs map[string]*Job jobOrder []string // Job names in sorted alphabetical order }` | JobManager manages a collection of jobs and handles dependency validation |
| `lock_schema.go` | `AgentMetadataInfo` | `type AgentMetadataInfo struct { AgentID string AgentModel string DetectionAgentID string DetectionAgentModel string EngineVersions map[string]string AgentImageRunner string }` | AgentMetadataInfo holds agent and detection agent information for embedding in lock file metadata |
| `lock_schema.go` | `LockHashInfo` | `type LockHashInfo struct { FrontmatterHash string BodyHash string }` | LockHashInfo groups the hash fields written into lock metadata. |
| `lock_schema.go` | `LockMetadata` | `type LockMetadata struct { SchemaVersion LockSchemaVersion `json:"schema_version"` FrontmatterHash string `json:"frontmatter_hash,omitempty"` BodyHash string `json:"body_hash,omitempty"` StopTime string `json:"stop_time,omitempty"` CompilerVersion string `json:"compiler_version,omitempty"` Strict bool `json:"strict,omitempty"` AgentID string `json:"agent_id,omitempty"` AgentModel string `json:"agent_model,omitempty"` DetectionAgentID string `json:"detection_agent_id,omitempty"` DetectionAgentModel string `json:"detection_agent_model,omitempty"` EngineVersions map[string]string `json:"engine_versions,omitempty"` AgentImageRunner string `json:"agent_image_runner,omitempty"` }` | LockMetadata represents the structured metadata embedded in lock files |
| `lock_schema.go` | `LockSchemaVersion` | `type LockSchemaVersion string` | LockSchemaVersion represents a lock file schema version |
| `lsp_manager.go` | `LSPManager` | `type LSPManager struct { servers map[string]LSPServerConfig }` | LSPManager handles LSP configuration normalization, validation, and generation. |
| `lsp_manager.go` | `LSPServerConfig` | `type LSPServerConfig struct { Command string `json:"command,omitempty"` Args []string `json:"args,omitempty"` FileExtensions map[string]string `json:"fileExtensions,omitempty"` // Version pins the package version for the language server. When set, it overrides the // built-in default version for known LSP servers. Accepts standard semver version strings // (e.g. "5.3.0") without a leading "v". Has no effect for custom servers not in the // built-in install spec table. Version string `json:"version,omitempty"` }` | LSPServerConfig defines a single language server entry under top-level frontmatter "lsp:". |
| `maintenance_workflow.go` | `GenerateMaintenanceWorkflowOptions` | `type GenerateMaintenanceWorkflowOptions struct { WorkflowDataList []*WorkflowData WorkflowDir string Version string ActionMode ActionMode ActionTag string RepoConfig *RepoConfig RepoSlug string }` | GenerateMaintenanceWorkflowOptions configures a maintenance workflow generation run. |
| `mcp_config_types.go` | `MCPConfigRenderer` | `type MCPConfigRenderer struct { // IndentLevel controls the indentation level for properties (e.g., " " for JSON, " " for TOML) IndentLevel string // Format specifies the output format ("json" for JSON-like, "toml" for TOML-like) Format string // RequiresCopilotFields indicates if the engine requires "type" and "tools" fields (true for copilot engine) RequiresCopilotFields bool // RewriteLocalhostToDocker indicates if localhost URLs should be rewritten to host.docker.internal // This is needed when the agent runs inside a firewall container and needs to access MCP servers on the host RewriteLocalhostToDocker bool // GuardPolicies contains the write-sink guard policies to render at the end of the MCP server configuration. // For JSON format, they are added as the last field inside the server object. // For TOML format, they are added as a separate TOML section after the server config. // Nil when no guard policies should be applied. GuardPolicies map[string]any }` | MCPConfigRenderer contains configuration options for rendering MCP config |
| `mcp_config_types.go` | `MapToolConfig` | `type MapToolConfig map[string]any` | MapToolConfig implements ToolConfig for map[string]any |
| `mcp_config_types.go` | `ToolConfig` | `type ToolConfig interface { GetString(key string) (string, bool) GetStringArray(key string) ([]string, bool) GetStringMap(key string) (map[string]string, bool) GetAny(key string) (any, bool) }` | ToolConfig represents a tool configuration interface for type safety |
| `mcp_config_types.go` | `WellKnownContainer` | `type WellKnownContainer struct { Image string // Container image (e.g., "node:lts-alpine") Entrypoint string // Entrypoint command (e.g., "npx") }` | WellKnownContainer represents a container configuration for a well-known command |
| `metrics.go` | `FinalizeToolMetricsOptions` | `type FinalizeToolMetricsOptions struct { Metrics *LogMetrics ToolCallMap map[string]*ToolCallInfo CurrentSequence []string Turns int TokenUsage int }` | FinalizeToolMetricsOptions holds the options for FinalizeToolMetrics |
| `metrics.go` | `LogMetrics` | `type LogMetrics struct { TokenUsage int EstimatedCost float64 Turns int // Number of turns needed to complete the task ToolCalls []ToolCallInfo // Tool call statistics ToolSequences [][]string // Sequences of tool calls preserving order AvgTimeBetweenTurns time.Duration // Mean time between consecutive LLM API calls (computed from per-turn timestamps when available) MaxTimeBetweenTurns time.Duration // Maximum time between any two consecutive LLM API calls MedianTimeBetweenTurns time.Duration // Median time between consecutive LLM API calls // StdDevTimeBetweenTurns is the sample standard deviation (Bessel's correction, n-1 // denominator) of inter-turn intervals, treating the observed turns as a sample of // the agent's execution behaviour rather than an exhaustive population. StdDevTimeBetweenTurns time.Duration }` | LogMetrics represents extracted metrics from log files |
| `metrics.go` | `ToolCallInfo` | `type ToolCallInfo struct { Name string // Prettified tool name (e.g., "github::search_issues", "bash") CallCount int // Number of times this tool was called MaxInputSize int // Maximum input size for any call (engine-dependent units, often bytes/chars) MaxOutputSize int // Maximum output size for any call (engine-dependent units, often bytes/chars) MaxDuration time.Duration // Maximum execution duration for any call OutputSample string // Preview of the largest tool response (first few lines, truncated) }` | ToolCallInfo represents statistics for a single tool |
| `model_identifier.go` | `ParsedModelIdentifier` | `type ParsedModelIdentifier struct { // Raw is the original unparsed string. Raw string // Base is the base identifier (before "?"). Base string // Provider is the provider token (empty for bare identifiers). Provider string // ModelToken is the model-token portion after the "/" (empty for bare identifiers). ModelToken string // IsGlob reports whether the model token contains a "*" wildcard. IsGlob bool // Params holds the URL-style query parameters (key → value). Params map[string]string }` | ParsedModelIdentifier holds the components of a parsed model identifier. |
| `noop.go` | `NoOpConfig` | `type NoOpConfig struct { BaseSafeOutputConfig `yaml:",inline"` ReportAsIssue *string `yaml:"report-as-issue,omitempty"` // Controls whether noop runs are reported as issue comments (default: true) }` | NoOpConfig holds configuration for no-op safe output (logging only) |
| `permissions_toolset_data.go` | `GitHubToolsetPermissions` | `type GitHubToolsetPermissions struct { ReadPermissions []PermissionScope WritePermissions []PermissionScope Tools []string // List of tools in this toolset (for verification) }` | GitHubToolsetPermissions maps GitHub MCP toolsets to their required permissions |
| `permissions_toolset_data.go` | `GitHubToolsetsData` | `type GitHubToolsetsData struct { Version string `json:"version"` Description string `json:"description"` Toolsets map[string]struct { Description string `json:"description"` ReadPermissions []string `json:"read_permissions"` WritePermissions []string `json:"write_permissions"` Tools []string `json:"tools"` } `json:"toolsets"` }` | GitHubToolsetsData represents the structure of the embedded JSON file |
| `repo_config.go` | `MaintenanceCompileConfig` | `type MaintenanceCompileConfig struct { // CreatePullRequestGitHubToken is the secret name used by the compile-workflows // maintenance job for GitHub API calls and branch pushes. When configured, // out-of-sync compiled workflows are reported via a deduplicated pull request // instead of an issue. CreatePullRequestGitHubToken string `json:"create_pull_request_github_token,omitempty"` }` | MaintenanceConfig holds maintenance-workflow-specific settings from aw. |
| `repo_config.go` | `MaintenanceConfig` | `type MaintenanceConfig struct { // RunsOn is the runner label or labels used for all jobs in agentics-maintenance.yml. RunsOn RunsOnValue `json:"runs_on,omitempty"` // ActionFailureIssueExpires configures expiration (in hours) for action // failure issues opened by the conclusion job. Defaults to 168 (7 days). ActionFailureIssueExpires int `json:"action_failure_issue_expires,omitempty"` // LabelTriggers controls all label-triggered jobs (disable_agentic_workflow, // label_apply_safe_outputs, etc.). // The value is treated as an opt-in flag: only true enables the jobs. // nil (omitted) or false both disable label-triggered jobs. // To opt in, set label_triggers: true in aw.json. LabelTriggers *bool `json:"label_triggers,omitempty"` // DisabledJobs lists maintenance job IDs that should be omitted from generated // agentics-maintenance workflows. DisabledJobs []string `json:"disabled_jobs,omitempty"` // Compile controls compile-workflows maintenance job behavior. Compile *MaintenanceCompileConfig `json:"compile,omitempty"` }` | Exported type declared in `repo_config.go`. |
| `repository_features.go` | `RepositoryFeatures` | `type RepositoryFeatures struct { HasDiscussions bool HasIssues bool }` | Exported type declared in `repository_features.go`. |
| `runtime_definitions.go` | `RuntimeRequirement` | `type RuntimeRequirement struct { Runtime *Runtime Version string // Empty string means use default ExtraFields map[string]any // Additional 'with' fields from user's setup step (e.g., cache settings) GoModFile string // Path to go.mod file for Go runtime (Go-specific) IfCondition string // Optional GitHub Actions if condition Cooldown bool // If false, disables default dependency cooldown behavior for installs associated with this runtime }` | RuntimeRequirement represents a detected runtime requirement |
| `safe_jobs.go` | `SafeJobConfig` | `type SafeJobConfig struct { // Standard GitHub Actions job properties Name string `yaml:"name,omitempty"` Description string `yaml:"description,omitempty"` RunsOn RunsOnValue `yaml:"runs-on,omitempty"` If string `yaml:"if,omitempty"` Needs []string `yaml:"needs,omitempty"` Steps []any `yaml:"steps,omitempty"` Env map[string]string `yaml:"env,omitempty"` Permissions map[string]string `yaml:"permissions,omitempty"` // Additional safe-job specific properties Inputs map[string]*InputDefinition `yaml:"inputs,omitempty"` GitHubToken string `yaml:"github-token,omitempty"` Output string `yaml:"output,omitempty"` Max int `yaml:"max,omitempty"` // Maximum number of times this output type may be emitted per run (default: 1) }` | SafeJobConfig defines a safe job configuration with GitHub Actions job properties |
| `safe_outputs_actions.go` | `SafeOutputActionConfig` | `type SafeOutputActionConfig struct { Uses string `yaml:"uses"` Description string `yaml:"description,omitempty"` // optional override of the action's description Env map[string]string `yaml:"env,omitempty"` // additional environment variables for the injected step // Computed at compile time (not from frontmatter): ResolvedRef string `yaml:"-"` // Pinned action reference (e.g., "owner/repo@sha # v1") Inputs map[string]*ActionYAMLInput `yaml:"-"` // Inputs parsed from action.yml ActionDescription string `yaml:"-"` // Description from action.yml }` | SafeOutputActionConfig holds configuration for a single custom safe output action. |
| `safe_outputs_app_config.go` | `GitHubAppConfig` | `type GitHubAppConfig struct { AppID string `yaml:"client-id,omitempty"` // GitHub App client ID (or legacy app ID) (e.g., "${{ vars.APP_ID }}") PrivateKey string `yaml:"private-key,omitempty"` // GitHub App private key (e.g., "${{ secrets.APP_PRIVATE_KEY }}") IgnoreIfMissing bool `yaml:"ignore-if-missing,omitempty"` // If true, skip token minting when client-id/private-key resolve empty Owner string `yaml:"owner,omitempty"` // Optional: owner of the GitHub App installation (defaults to checkout.repository owner when derivable, otherwise current repository owner) Repositories []string `yaml:"repositories,omitempty"` // Optional: comma or newline-separated list of repositories to grant access to Permissions map[string]string `yaml:"permissions,omitempty"` // Optional: extra permission-* fields to merge into the minted token (nested wins over job-level) }` | GitHubAppConfig holds configuration for GitHub App-based token minting |
| `safe_outputs_config_runtime.go` | `SafeOutputStepConfig` | `type SafeOutputStepConfig struct { StepName string // Human-readable step name (e.g., "Create Issue") StepID string // Step ID for referencing outputs (e.g., "create_issue") Script string // JavaScript script to execute (for inline mode) ScriptName string // Name of the script in the registry (for file mode) CustomEnvVars []string // Environment variables specific to this step Condition ConditionNode // Step-level condition (if clause) Token string // GitHub token for this step UseCopilotRequestsToken bool // Whether to use Copilot requests token preference chain UseCopilotCodingAgentToken bool // Whether to use Copilot coding agent token preference chain PreSteps []string // Optional steps to run before the script step PostSteps []string // Optional steps to run after the script step Outputs map[string]string // Outputs from this step ContinueOnError bool // Whether to continue the job even if this step fails (continue-on-error: true) }` | SafeOutputStepConfig holds configuration for building a single safe output step within the consolidated safe-outputs job |
| `safe_outputs_config_types.go` | `MentionsConfig` | `type MentionsConfig struct { // Enabled can be: // true: mentions always allowed (error in strict mode) // false: mentions always escaped // nil: use default behavior with team members and context Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"` // AllowedCollaborators determines if repository collaborators can be mentioned (default: true) AllowedCollaborators *bool `yaml:"allowed-collaborators,omitempty" json:"allowedCollaborators,omitempty"` // AllowContext determines if mentions from event context are allowed (default: true) AllowContext *bool `yaml:"allow-context,omitempty" json:"allowContext,omitempty"` // Allowed is a list of user/bot names always allowed (bots not allowed by default) Allowed []string `yaml:"allowed,omitempty" json:"allowed,omitempty"` // AllowedTeams is a list of team slugs whose members are always allowed to be mentioned. // Accepts "team-slug" (resolved against the current org) or "org/team-slug" format. // Requires the workflow token to have read:org scope (a fine-grained PAT, classic PAT with // read:org, or a GitHub App with the Members:Read permission). The default GITHUB_TOKEN // does not include read:org and will produce a 403/404 warning; team members will be skipped // but the workflow will not fail. AllowedTeams []string `yaml:"allowed-teams,omitempty" json:"allowedTeams,omitempty"` // Max is the maximum number of mentions per message (default: 50) Max *int `yaml:"max,omitempty" json:"max,omitempty"` }` | MentionsConfig holds configuration for @mention filtering in safe outputs |
| `safe_outputs_config_types.go` | `SafeOutputMessagesConfig` | `type SafeOutputMessagesConfig struct { Footer string `yaml:"footer,omitempty" json:"footer,omitempty"` // Custom footer message template FooterInstall string `yaml:"footer-install,omitempty" json:"footerInstall,omitempty"` // Custom installation instructions template FooterWorkflowRecompile string `yaml:"footer-workflow-recompile,omitempty" json:"footerWorkflowRecompile,omitempty"` // Custom footer template for workflow recompile issues FooterWorkflowRecompileComment string `yaml:"footer-workflow-recompile-comment,omitempty" json:"footerWorkflowRecompileComment,omitempty"` // Custom footer template for comments on workflow recompile issues StagedTitle string `yaml:"staged-title,omitempty" json:"stagedTitle,omitempty"` // Custom styled mode title template StagedDescription string `yaml:"staged-description,omitempty" json:"stagedDescription,omitempty"` // Custom staged mode description template AppendOnlyComments bool `yaml:"append-only-comments,omitempty" json:"appendOnlyComments,omitempty"` // If true, post run status as new comments instead of updating the activation comment ActivationComments string `yaml:"activation-comments,omitempty" json:"activationComments,omitempty"` // If "false", disable all activation/fallback comments entirely. Supports templatable boolean values (literal "true"/"false" or GitHub Actions expressions). Empty/unset preserves default enabled behavior. RunStarted string `yaml:"run-started,omitempty" json:"runStarted,omitempty"` // Custom workflow activation message template RunSuccess string `yaml:"run-success,omitempty" json:"runSuccess,omitempty"` // Custom workflow success message template RunFailure string `yaml:"run-failure,omitempty" json:"runFailure,omitempty"` // Custom workflow failure message template DetectionFailure string `yaml:"detection-failure,omitempty" json:"detectionFailure,omitempty"` // Custom detection job failure message template PullRequestCreated string `yaml:"pull-request-created,omitempty" json:"pullRequestCreated,omitempty"` // Custom message template for pull request creation link. Placeholders: {item_number}, {item_url} IssueCreated string `yaml:"issue-created,omitempty" json:"issueCreated,omitempty"` // Custom message template for issue creation link. Placeholders: {item_number}, {item_url} CommitPushed string `yaml:"commit-pushed,omitempty" json:"commitPushed,omitempty"` // Custom message template for commit push link. Placeholders: {commit_sha}, {short_sha}, {commit_url} AgentFailureIssue string `yaml:"agent-failure-issue,omitempty" json:"agentFailureIssue,omitempty"` // Custom footer template for agent failure tracking issues AgentFailureComment string `yaml:"agent-failure-comment,omitempty" json:"agentFailureComment,omitempty"` // Custom footer template for comments on agent failure tracking issues BodyHeader string `yaml:"body-header,omitempty" json:"bodyHeader,omitempty"` // Custom header text prepended to every message body (issues, comments, PRs, discussions). Placeholders: {workflow_name}, {run_url} DisclosureHeader string `yaml:"disclosure-header,omitempty" json:"disclosureHeader,omitempty"` // AI authorship disclosure header prepended to every message body. Set to "true" for built-in default text, or provide a custom template string. Placeholders: {workflow_name}, {run_url} }` | SafeOutputMessagesConfig holds custom message templates for safe-output footer and notification messages |
| `safe_outputs_config_types.go` | `SecretMaskingConfig` | `type SecretMaskingConfig struct { Steps []map[string]any `yaml:"steps,omitempty"` // Additional secret redaction steps to inject after built-in redaction }` | SecretMaskingConfig holds configuration for secret redaction behavior |
| `safe_outputs_jobs.go` | `SafeOutputJobConfig` | `type SafeOutputJobConfig struct { // Job metadata JobName string // e.g., "create_issue" StepName string // e.g., "Create Output Issue" StepID string // e.g., "create_issue" MainJobName string // Main workflow job name for dependencies // Custom environment variables specific to this safe output type CustomEnvVars []string // JavaScript script constant to include in the GitHub Script step Script string // Script name for looking up custom action path (optional) // If provided and action mode is custom, the compiler will use a custom action // instead of inline JavaScript. Example: "create_issue" ScriptName string // Job configuration Permissions *Permissions // Job permissions Outputs map[string]string // Job outputs Condition ConditionNode // Job condition (if clause) Needs []string // Job dependencies PreSteps []string // Optional steps to run before the GitHub Script step PostSteps []string // Optional steps to run after the GitHub Script step Token string // GitHub token for this output type UseCopilotRequestsToken bool // Whether to use Copilot token preference chain UseCopilotCodingAgentToken bool // Whether to use agent token preference chain (config token > GH_AW_AGENT_TOKEN) TargetRepoSlug string // Target repository for cross-repo operations }` | SafeOutputJobConfig holds configuration for building a safe output job This config struct extracts the common parameters across all safe output job builders |
| `safe_outputs_parser.go` | `CloseJobConfig` | `type CloseJobConfig struct { SafeOutputTargetConfig `yaml:",inline"` SafeOutputFilterConfig `yaml:",inline"` }` | CloseJobConfig represents common configuration for close operations (close-issue, close-discussion, close-pull-request) |
| `safe_outputs_parser.go` | `ListJobConfig` | `type ListJobConfig struct { SafeOutputTargetConfig `yaml:",inline"` SafeOutputAllowBlockConfig `yaml:",inline"` }` | ListJobConfig represents common configuration for list-based operations (add-labels, add-reviewer, assign-milestone) |
| `safe_outputs_parser.go` | `SafeOutputAllowBlockConfig` | `type SafeOutputAllowBlockConfig struct { Allowed []string `yaml:"allowed,omitempty"` // Optional list of allowed values Blocked []string `yaml:"blocked,omitempty"` // Optional list of blocked patterns (supports glob patterns) }` | SafeOutputAllowBlockConfig contains common allow/block lists for safe output configurations. |
| `safe_outputs_parser.go` | `SafeOutputDiscussionFilterConfig` | `type SafeOutputDiscussionFilterConfig struct { SafeOutputFilterConfig `yaml:",inline"` RequiredCategory string `yaml:"required-category,omitempty"` // Required category for discussion operations }` | SafeOutputDiscussionFilterConfig extends SafeOutputFilterConfig with discussion-specific fields. |
| `safe_outputs_steps.go` | `GitHubScriptStepConfig` | `type GitHubScriptStepConfig struct { // Step metadata StepName string // e.g., "Create Output Issue" StepID string // e.g., "create_issue" // Main job reference for agent output MainJobName string // Environment variables specific to this safe output type // These are added after GH_AW_AGENT_OUTPUT CustomEnvVars []string // JavaScript script constant to format and include (for inline mode) Script string // ScriptFile is the .cjs filename to require (e.g., "noop.cjs") // If empty, Script will be inlined instead ScriptFile string // CustomToken configuration (passed to addSafeOutputGitHubTokenForConfig or addSafeOutputCopilotGitHubTokenForConfig) CustomToken string // UseCopilotRequestsToken indicates whether to use the Copilot token preference chain // custom token > COPILOT_GITHUB_TOKEN // This should be true for Copilot-related operations like creating agent tasks, // assigning copilot to issues, or adding copilot as PR reviewer UseCopilotRequestsToken bool // UseCopilotCodingAgentToken indicates whether to use the agent token preference chain // (config token > GH_AW_AGENT_TOKEN) // This should be true for agent assignment operations (assign-to-agent) UseCopilotCodingAgentToken bool // StepCondition is an optional `if:` expression for the step. // When non-empty, `if: {StepCondition}` is inserted after the step ID so the // step runs only when the condition is true. Use "always()" to run even after // earlier steps in the same job have failed. StepCondition string }` | GitHubScriptStepConfig holds configuration for building a GitHub Script step |
| `safe_outputs_tools_generation.go` | `ToolsMeta` | `type ToolsMeta struct { // DescriptionSuffixes maps tool name → constraint text to append to the base description. // Example: " CONSTRAINTS: Maximum 5 issue(s) can be created." DescriptionSuffixes map[string]string `json:"description_suffixes"` // RepoParams maps tool name → "repo" inputSchema property definition, only present // when allowed-repos or a wildcard target-repo is configured for that tool. RepoParams map[string]map[string]any `json:"repo_params"` // DynamicTools contains tool definitions for custom safe-jobs, dispatch_workflow // targets, and call_workflow targets. These are workflow-specific and cannot be // derived from the static safe_outputs_tools.json at runtime. DynamicTools []map[string]any `json:"dynamic_tools"` // RequiredFieldRemovals maps tool name → list of field names to remove from the // inputSchema.required array. Used when a field that is required in the static // safe_outputs_tools.json should be optional for this specific workflow (e.g. when // allow-body: false is configured for close_discussion or close_issue). RequiredFieldRemovals map[string][]string `json:"required_field_removals,omitempty"` // RequiredFieldAdditions maps tool name → list of field names to add to the // inputSchema.required array. Used when a field that is optional in the static // safe_outputs_tools.json should be required for this specific workflow. RequiredFieldAdditions map[string][]string `json:"required_field_additions,omitempty"` }` | ToolsMeta is the structure written to tools_meta. |
| `safe_outputs_validation_config.go` | `FieldValidation` | `type FieldValidation struct { Required bool `json:"required,omitempty"` Type string `json:"type,omitempty"` TypeHint string `json:"typeHint,omitempty"` // Overrides the type description in error messages (e.g. "GraphQL node ID string") Sanitize bool `json:"sanitize,omitempty"` MaxLength int `json:"maxLength,omitempty"` MinLength int `json:"minLength,omitempty"` PositiveInteger bool `json:"positiveInteger,omitempty"` OptionalPositiveInteger bool `json:"optionalPositiveInteger,omitempty"` IssueOrPRNumber bool `json:"issueOrPRNumber,omitempty"` IssueNumberOrTemporaryID bool `json:"issueNumberOrTemporaryId,omitempty"` Enum []string `json:"enum,omitempty"` ItemType string `json:"itemType,omitempty"` ItemSanitize bool `json:"itemSanitize,omitempty"` ItemMaxLength int `json:"itemMaxLength,omitempty"` Pattern string `json:"pattern,omitempty"` PatternError string `json:"patternError,omitempty"` TemporaryID bool `json:"temporaryId,omitempty"` // StripOnError marks optional enrichment fields (e.g. confidence, rationale) that should be // silently dropped when they fail validation instead of rejecting the entire item. // Serialised as "x-strip-on-error" to follow the x- extension convention used in JSON Schema. StripOnError bool `json:"x-strip-on-error,omitempty"` }` | FieldValidation defines validation rules for a single field |
| `safe_outputs_validation_config.go` | `TypeValidationConfig` | `type TypeValidationConfig struct { DefaultMax int `json:"defaultMax"` Fields map[string]FieldValidation `json:"fields"` CustomValidation string `json:"customValidation,omitempty"` }` | TypeValidationConfig defines validation configuration for a safe output type |
| `safe_update_manifest.go` | `GHAWManifestAction` | `type GHAWManifestAction struct { Repo string `json:"repo"` SHA string `json:"sha"` Version string `json:"version,omitempty"` }` | GHAWManifestAction represents a single GitHub Action referenced in a compiled workflow. |
| `safe_update_manifest.go` | `GHAWManifestResolutionFailure` | `type GHAWManifestResolutionFailure struct { Repo string `json:"repo"` Ref string `json:"ref"` ErrorType string `json:"error_type"` }` | GHAWManifestResolutionFailure represents an action-ref pinning failure captured during compilation. |
| `samples_replay.go` | `SampleEntry` | `type SampleEntry struct { // Tool is the snake_case MCP tool name (e.g. "create_pull_request"). Tool string `json:"tool"` // Arguments are passed verbatim as the MCP `tools/call` arguments. // Sample sidecar fields (e.g. `patch`) have already been stripped. Arguments map[string]any `json:"arguments"` // Sidecars carries fields stripped from Arguments that need out-of-band // pre-staging by the driver (e.g. `patch` for create_pull_request). Sidecars map[string]any `json:"sidecars,omitempty"` }` | SampleEntry is the per-call payload consumed by apply_samples. |
| `sandbox.go` | `AgentAPIProxyTargetConfig` | `type AgentAPIProxyTargetConfig struct { // AuthHeader is the custom authentication header name sent with API requests. // When set, the raw agent ID is sent as "<authHeader>: <key>" instead of the // provider default ("Authorization" for OpenAI, "x-api-key" for Anthropic). // Example: "api-key" for Azure OpenAI gateways. AuthHeader string `yaml:"authHeader,omitempty"` // ExtraHeaders holds additional non-sensitive headers to include on Copilot BYOK // upstream requests. Applies only to the "copilot" provider target. // Maps to apiProxy.targets.copilot.extraHeaders in the AWF config (AWF_BYOK_EXTRA_HEADERS). ExtraHeaders map[string]string `yaml:"extraHeaders,omitempty"` // ExtraBodyFields holds additional non-sensitive JSON body fields to include on Copilot // BYOK upstream requests. Applies only to the "copilot" provider target. // Maps to apiProxy.targets.copilot.extraBodyFields in the AWF config (AWF_BYOK_EXTRA_BODY_FIELDS). ExtraBodyFields map[string]string `yaml:"extraBodyFields,omitempty"` // SessionId is an opt-in session identifier injected as the x-session-id request header // and session_id body field on Copilot BYOK upstream requests. Applies only to the // "copilot" provider target. // Maps to apiProxy.targets.copilot.sessionId in the AWF config (AWF_PROVIDER_SESSION_ID). SessionId string `yaml:"sessionId,omitempty"` }` | AgentAPIProxyTargetConfig configures a single LLM provider's API proxy target. |
| `sandbox.go` | `AgentRuntime` | `type AgentRuntime string` | AgentRuntime represents the container runtime to use for the agent container. |
| `script_registry.go` | `ScriptRegistry` | `type ScriptRegistry struct { mu sync.RWMutex scripts map[string]*scriptEntry }` | ScriptRegistry manages script metadata and custom action paths. |
| `secret_extraction.go` | `SecretExpression` | `type SecretExpression struct { VarName string // The secret variable name (e.g., "DD_API_KEY") FullExpr string // The full expression (e.g., "${{ secrets.DD_API_KEY }}") }` | SecretExpression represents a parsed secret expression |
| `side_repo_maintenance.go` | `SideRepoTarget` | `type SideRepoTarget struct { // Repository is the static owner/repo slug of the target (e.g. "my-org/main-repo"). // Expression-based repositories (containing "${{") are excluded. Repository string // GitHubToken is the token expression used to authenticate against the target // repository, e.g. "${{ secrets.GH_AW_MAIN_REPO_TOKEN }}". Empty when the // checkout config does not specify a custom token. // Mutually exclusive with GitHubApp. GitHubToken string // GitHubApp carries the GitHub App authentication config discovered from the // source checkout. When set, each cross-repo maintenance job gets a // create-github-app-token mint step and the minted token is used for all // github-token: inputs and GH_TOKEN: env vars. // Mutually exclusive with GitHubToken. GitHubApp *GitHubAppConfig }` | SideRepoTarget represents a target repository inferred from a checkout block with current: true in a compiled workflow. |
| `skills_frontmatter.go` | `SkillReference` | `type SkillReference struct { Skill string `json:"skill,omitempty"` GitHubToken string `json:"github-token,omitempty"` GitHubApp *GitHubAppConfig `json:"github-app,omitempty"` }` | SkillReference describes a single skills[] entry in workflow frontmatter. |
| `step_order_validation.go` | `StepOrderTracker` | `type StepOrderTracker struct { steps []StepRecord nextOrder int secretRedactionAdded bool secretRedactionOrder int afterAgentExecution bool // Track whether we're after agent execution step }` | StepOrderTracker tracks the order of steps generated during compilation |
| `step_order_validation.go` | `StepRecord` | `type StepRecord struct { Type StepType Name string Order int // Order in which this step was added UploadPaths []string // For artifact upload steps, the paths being uploaded }` | StepRecord tracks a step that was generated during compilation |
| `step_order_validation.go` | `StepType` | `type StepType int` | StepType represents the type of step being generated |
| `templatables.go` | `TemplatableBool` | `type TemplatableBool string` | TemplatableBool represents a boolean frontmatter field that also accepts GitHub Actions expression strings (e. |
| `templatables.go` | `TemplatableBoolOrInt` | `type TemplatableBoolOrInt string` | TemplatableBoolOrInt represents a field that accepts a boolean, a non-negative integer (0–100), or a GitHub Actions expression string (e. |
| `templatables.go` | `TemplatableInt32` | `type TemplatableInt32 string` | TemplatableInt32 represents an integer frontmatter field that also accepts GitHub Actions expression strings (e. |
| `template_injection_utils.go` | `TemplateInjectionViolation` | `type TemplateInjectionViolation struct { Expression string // The unsafe expression (e.g., "${{ github.event.issue.title }}") Snippet string // Code snippet showing the violation context Context string // Expression context (e.g., "github.event", "steps.*.outputs") }` | TemplateInjectionViolation represents a detected template injection risk |
| `time_delta.go` | `TimeDelta` | `type TimeDelta struct { Hours int Days int Minutes int Weeks int Months int }` | TimeDelta represents a time duration that can be added to a base time |
| `tools_types.go` | `GitHubMCPMode` | `type GitHubMCPMode string` | GitHubMCPMode represents the MCP transport/deployment mode for the GitHub tool. |
| `tools_types.go` | `GitHubReposScope` | `type GitHubReposScope any` | GitHubReposScope represents the repository scope for guard policy enforcement Can be one of: "all", "public", or an array of repository patterns |
| `unified_prompt_step.go` | `PromptSection` | `type PromptSection struct { // Content is the actual prompt text or a reference to a file Content string // IsFile indicates if Content is a filename (true) or inline text (false) IsFile bool // ShellCondition is an optional bash condition (without 'if' keyword) to wrap this section // Example: "${{ github.event_name == 'issue_comment' }}" becomes a shell condition ShellCondition string // EnvVars contains environment variables needed for expressions in this section EnvVars map[string]string }` | PromptSection represents a section of prompt text to be appended |
| `update_entity_helpers.go` | `FieldParsingMode` | `type FieldParsingMode int` | FieldParsingMode determines how boolean fields are parsed from the config |
| `update_entity_helpers.go` | `UpdateEntityConfig` | `type UpdateEntityConfig struct { BaseSafeOutputConfig `yaml:",inline"` SafeOutputTargetConfig `yaml:",inline"` }` | UpdateEntityConfig holds the configuration for an update entity operation |
| `update_entity_helpers.go` | `UpdateEntityFieldSpec` | `type UpdateEntityFieldSpec struct { Name string // Field name in config (e.g., "title", "body", "status") Mode FieldParsingMode // Parsing mode for this field Dest **bool // Pointer to the destination field (used with FieldParsingKeyExistence / FieldParsingBoolValue) StringDest **string // Pointer to the destination string field (used with FieldParsingTemplatableBool) }` | UpdateEntityFieldSpec defines a boolean field to be parsed from config |
| `update_entity_helpers.go` | `UpdateEntityJobBuilder` | `type UpdateEntityJobBuilder struct { EntityType UpdateEntityType ConfigKey string JobName string StepName string ScriptGetter func() string PermissionsFunc func() *Permissions BuildCustomEnvVars func(*UpdateEntityConfig) []string BuildOutputs func() map[string]string BuildEventCondition func(string) ConditionNode // Optional: builds event condition if target is empty }` | UpdateEntityJobBuilder encapsulates entity-specific configuration for building update jobs |
| `update_entity_helpers.go` | `UpdateEntityJobParams` | `type UpdateEntityJobParams struct { EntityType UpdateEntityType ConfigKey string // e.g., "update-issue", "update-pull-request" JobName string // e.g., "update_issue", "update_pull_request" StepName string // e.g., "Update Issue", "Update Pull Request" ScriptGetter func() string PermissionsFunc func() *Permissions CustomEnvVars []string // Type-specific environment variables Outputs map[string]string // Type-specific outputs Condition ConditionNode // Job condition expression }` | UpdateEntityJobParams holds the parameters needed to build an update entity job |
| `update_entity_helpers.go` | `UpdateEntityParseOptions` | `type UpdateEntityParseOptions struct { EntityType UpdateEntityType // Type of entity being parsed ConfigKey string // Config key (e.g., "update-issue") Logger *logger.Logger // Logger for this entity type Fields []UpdateEntityFieldSpec // Field specifications to parse CustomParser func(map[string]any) // Optional custom field parser }` | UpdateEntityParseOptions holds options for parsing entity-specific configuration |
| `update_entity_helpers.go` | `UpdateEntityType` | `type UpdateEntityType string` | UpdateEntityType represents the type of entity being updated |
| `workflow_data.go` | `SkipIfCheckFailingConfig` | `type SkipIfCheckFailingConfig struct { Include []string // check names to include (empty = all checks) Exclude []string // check names to exclude Branch string // optional branch name to check (defaults to triggering ref or PR base branch) AllowPending bool // if true, pending/in-progress checks are not treated as failing (default: treat pending as failing) }` | SkipIfCheckFailingConfig holds the configuration for skip-if-check-failing conditions |
| `workflow_data.go` | `SkipIfMatchConfig` | `type SkipIfMatchConfig struct { Query string // GitHub search query to check before running workflow Max int // Maximum number of matches before skipping (defaults to 1) Scope string // Scope for the query: "none" disables auto repo:owner/repo scoping }` | SkipIfMatchConfig holds the configuration for skip-if-match conditions |
| `workflow_data.go` | `SkipIfNoMatchConfig` | `type SkipIfNoMatchConfig struct { Query string // GitHub search query to check before running workflow Min int // Minimum number of matches required to proceed (defaults to 1) Scope string // Scope for the query: "none" disables auto repo:owner/repo scoping }` | SkipIfNoMatchConfig holds the configuration for skip-if-no-match conditions |
| `sandbox.go` | `AiCreditsPricingConfig` | `type AiCreditsPricingConfig struct { Input float64 Output float64 CachedInput *float64 CacheWrite *float64 }` | AiCreditsPricingConfig defines per-token pricing inputs used for AI-credit accounting. |
| `repo_config.go` | `ContainerPinTarget` | `type ContainerPinTarget struct { Image string Digest string }` | ContainerPinTarget maps a source image to a pinned digest replacement. |
| `engine_definition.go` | `EngineNetworkDefinition` | `type EngineNetworkDefinition struct { Defaults []string ProviderDomains map[string]string DefaultProvider string }` | EngineNetworkDefinition defines network-domain defaults for declarative engines. |
| `engine_helpers.go` | `EngineSecretValidationConfig` | `type EngineSecretValidationConfig struct { SecretNames []string EngineName string DocsURL string Skip func(*WorkflowData) bool }` | EngineSecretValidationConfig configures shared engine-secret validation steps. |
| `agentic_engine.go` | `HarnessRunner` | `type HarnessRunner interface { GetHarnessScriptName() string }` | HarnessRunner is implemented by engines that execute via harness scripts. |
| `agentic_engine.go` | `InferenceProviderResolver` | `type InferenceProviderResolver interface { ResolveLLMProvider(workflowData *WorkflowData) LLMProvider }` | InferenceProviderResolver resolves the effective inference provider for a workflow run. |
| `engine.go` | `InlineEngineDriver` | `type InlineEngineDriver struct { Runtime string Source string MultipleRuntime bool }` | InlineEngineDriver describes a parsed inline engine driver definition. |
| `llm_provider.go` | `LLMProvider` | `type LLMProvider string` | LLMProvider identifies the model provider selected for an engine invocation. |
| `agentic_engine.go` | `MCPConfigAdapterProvider` | `type MCPConfigAdapterProvider interface { GetMCPConfigAdapterWriteStep() GitHubActionStep GetMCPConfigAdapterFilename() string }` | MCPConfigAdapterProvider is implemented by engines that ship MCP config-adapter scripts. |
| `agentic_engine.go` | `MCPProxyEngine` | `type MCPProxyEngine interface { Engine CapabilityProvider }` | MCPProxyEngine marks engines that support MCP proxy integration. |
| `nodejs.go` | `NPMInstallOptions` | `type NPMInstallOptions struct { IncludeNodeSetup bool IsGlobal bool RunInstallScripts bool CooldownEnabled bool }` | NPMInstallOptions controls Node.js/npm bootstrap behavior in generated jobs. |
| `frontmatter_types.go` | `OTLPWorkloadIdentityConfig` | `type OTLPWorkloadIdentityConfig struct { Provider string Audience string ServiceAccount string }` | OTLPWorkloadIdentityConfig holds workload-identity metadata for OTLP exporters. |
| `safe_outputs_parser.go` | `SafeOutputAllowedLabelsConfig` | `type SafeOutputAllowedLabelsConfig struct { AllowedLabels []string }` | SafeOutputAllowedLabelsConfig configures optional safe-output label allowlists. |

### Additional constants and variables

| File | Kind | Symbol | Declaration | Description |
|------|------|--------|-------------|-------------|
| `action_cache.go` | `const` | `CacheFileName` | `const CacheFileName = "actions-lock.json"` | CacheFileName is the name of the cache file in . |
| `action_mode.go` | `const` | `ActionModeAction` | `const ActionModeAction ActionMode = "action"` | ActionModeAction references custom actions from the github/gh-aw-actions repository using the same release version |
| `action_mode.go` | `const` | `ActionModeDev` | `const ActionModeDev ActionMode = "dev"` | ActionModeDev references custom actions using local paths (development mode, default) |
| `action_mode.go` | `const` | `ActionModeRelease` | `const ActionModeRelease ActionMode = "release"` | ActionModeRelease references custom actions using SHA-pinned remote paths (release mode) |
| `action_mode.go` | `const` | `ActionModeScript` | `const ActionModeScript ActionMode = "script"` | ActionModeScript runs setup. |
| `action_reference.go` | `const` | `GitHubActionsOrgRepo` | `const GitHubActionsOrgRepo = "github/gh-aw-actions"` | GitHubActionsOrgRepo is the organization and repository name for the external gh-aw-actions repository |
| `action_reference.go` | `const` | `GitHubOrgRepo` | `const GitHubOrgRepo = "github/gh-aw"` | GitHubOrgRepo is the organization and repository name for custom action references |
| `auto_update_workflow.go` | `const` | `AutoUpdateWorkflowFileName` | `const AutoUpdateWorkflowFileName = "agentic-auto-upgrade.yml"` | AutoUpdateWorkflowFileName is the filename for the generated auto-upgrade workflow. |
| `close_entity_helpers.go` | `const` | `CloseEntityDiscussion` | `const CloseEntityDiscussion CloseEntityType = "discussion"` | Exported constant declared in `close_entity_helpers.go`. |
| `close_entity_helpers.go` | `const` | `CloseEntityIssue` | `const CloseEntityIssue CloseEntityType = "issue"` | Exported constant declared in `close_entity_helpers.go`. |
| `close_entity_helpers.go` | `const` | `CloseEntityPullRequest` | `const CloseEntityPullRequest CloseEntityType = "pull_request"` | Exported constant declared in `close_entity_helpers.go`. |
| `compiler.go` | `const` | `MaxExpressionSize` | `const MaxExpressionSize = 21000` | MaxExpressionSize is the maximum allowed size for GitHub Actions expression values (21KB) This includes environment variable values, if conditions, and other expression contexts See: https://docs. |
| `compiler.go` | `const` | `MaxPromptChunkSize` | `const MaxPromptChunkSize = 20000` | MaxPromptChunkSize is the maximum size for each chunk when splitting prompt text (20KB) This limit ensures each heredoc block stays under GitHub Actions step size limits (21KB) |
| `compiler.go` | `const` | `MaxPromptChunks` | `const MaxPromptChunks = 5` | MaxPromptChunks is the maximum number of chunks allowed when splitting prompt text This prevents excessive step generation for extremely large prompt texts |
| `compiler_aw_context.go` | `const` | `AwContextInputName` | `const AwContextInputName = "aw_context"` | AwContextInputName is the name of the internal aw_context workflow_dispatch input. |
| `compiler_aw_context.go` | `const` | `NetworkAllowedInputName` | `const NetworkAllowedInputName = "network_allowed"` | NetworkAllowedInputName is the optional workflow_call input that extends the compiled network allowlist at runtime for reusable workflows. |
| `compiler_experiments.go` | `const` | `ExperimentsStorageCache` | `const ExperimentsStorageCache ExperimentStorageMode = "cache"` | ExperimentsStorageCache uses GitHub Actions cache to persist experiment state. |
| `compiler_experiments.go` | `const` | `ExperimentsStorageRepo` | `const ExperimentsStorageRepo ExperimentStorageMode = "repo"` | ExperimentsStorageRepo uses a git branch (repo-memory) to persist experiment state. |
| `engine.go` | `const` | `WorkflowCallNetworkAllowedEnvVar` | `const WorkflowCallNetworkAllowedEnvVar = "GH_AW_WORKFLOW_CALL_NETWORK_ALLOWED"` | Exported constant declared in `engine.go`. |
| `engine_api_targets.go` | `const` | `DefaultGeminiAPITarget` | `const DefaultGeminiAPITarget = "generativelanguage.googleapis.com"` | DefaultGeminiAPITarget is the default Gemini API endpoint hostname. |
| `engine_definition.go` | `const` | `AuthStrategyAPIKey` | `const AuthStrategyAPIKey AuthStrategy = "api-key"` | AuthStrategyAPIKey uses a direct agent ID sent via a header (default when Secret is set). |
| `engine_definition.go` | `const` | `AuthStrategyBearer` | `const AuthStrategyBearer AuthStrategy = "bearer"` | AuthStrategyBearer sends a pre-obtained token as a standard Authorization: Bearer header. |
| `engine_definition.go` | `const` | `AuthStrategyOAuthClientCreds` | `const AuthStrategyOAuthClientCreds AuthStrategy = "oauth-client-credentials"` | AuthStrategyOAuthClientCreds exchanges client credentials for a bearer token before each call. |
| `engine_output.go` | `const` | `AgentCLIStartMsPath` | `const AgentCLIStartMsPath = "/tmp/gh-aw/agent_cli_start_ms.txt"` | AgentCLIStartMsPath is the path where the epoch-millisecond timestamp of the "Execute Agent CLI" step start is written on the host (before the AWF container launches). |
| `engine_output.go` | `const` | `AgentStepSummaryPath` | `const AgentStepSummaryPath = "/tmp/gh-aw/agent-step-summary.md"` | AgentStepSummaryPath is the path used as GITHUB_STEP_SUMMARY inside the agent sandbox. |
| `engine_output.go` | `const` | `RedactedURLsLogPath` | `const RedactedURLsLogPath = "/tmp/gh-aw/redacted-urls.log"` | RedactedURLsLogPath is the path where redacted URL domains are logged during sanitization |
| `error_recovery.go` | `const` | `SeverityCritical` | `const SeverityCritical ErrorSeverity = iota` | Exported constant declared in `error_recovery.go`. |
| `error_recovery.go` | `const` | `SeverityHigh` | `const SeverityHigh` | Exported constant declared in `error_recovery.go`. |
| `error_recovery.go` | `const` | `SeverityLow` | `const SeverityLow` | Exported constant declared in `error_recovery.go`. |
| `error_recovery.go` | `const` | `SeverityMedium` | `const SeverityMedium` | Exported constant declared in `error_recovery.go`. |
| `frontmatter_types.go` | `const` | `RunnerTopologyArcDind` | `const RunnerTopologyArcDind = "arc-dind"` | RunnerTopologyArcDind is the topology value for ARC runners with Docker-in-Docker sidecars. |
| `llm_provider.go` | `const` | `LLMProviderAnthropic` | `const LLMProviderAnthropic = "anthropic"` | Exported constant declared in `llm_provider.go`. |
| `llm_provider.go` | `const` | `LLMProviderGitHub` | `const LLMProviderGitHub = "github"` | Exported constant declared in `llm_provider.go`. |
| `llm_provider.go` | `const` | `LLMProviderOpenAI` | `const LLMProviderOpenAI = "openai"` | Exported constant declared in `llm_provider.go`. |
| `lock_schema.go` | `const` | `LockSchemaV1` | `const LockSchemaV1 LockSchemaVersion = "v1"` | LockSchemaV1 is the legacy lock file schema version (no strict field) |
| `lock_schema.go` | `const` | `LockSchemaV2` | `const LockSchemaV2 LockSchemaVersion = "v2"` | LockSchemaV2 is the lock file schema version that adds the strict field |
| `lock_schema.go` | `const` | `LockSchemaV3` | `const LockSchemaV3 LockSchemaVersion = "v3"` | LockSchemaV3 is the lock file schema version that adds agent id/model and detection agent id/model fields |
| `lock_schema.go` | `const` | `LockSchemaV4` | `const LockSchemaV4 LockSchemaVersion = "v4"` | LockSchemaV4 is the current lock file schema version (adds body_hash for full stale-check coverage) |
| `markdown_security_scanner.go` | `const` | `CategoryEmbeddedFiles` | `const CategoryEmbeddedFiles SecurityFindingCategory = "embedded-files"` | CategoryEmbeddedFiles covers SVG scripts, data-URI image payloads |
| `markdown_security_scanner.go` | `const` | `CategoryHTMLAbuse` | `const CategoryHTMLAbuse SecurityFindingCategory = "html-abuse"` | CategoryHTMLAbuse covers script/iframe/object/embed tags and event handlers |
| `markdown_security_scanner.go` | `const` | `CategoryHiddenContent` | `const CategoryHiddenContent SecurityFindingCategory = "hidden-content"` | CategoryHiddenContent covers HTML comments with payloads, hidden spans, CSS hiding |
| `markdown_security_scanner.go` | `const` | `CategoryObfuscatedLinks` | `const CategoryObfuscatedLinks SecurityFindingCategory = "obfuscated-links"` | CategoryObfuscatedLinks covers data URIs, mismatched links, encoded URLs |
| `markdown_security_scanner.go` | `const` | `CategorySocialEngineering` | `const CategorySocialEngineering SecurityFindingCategory = "social-engineering"` | CategorySocialEngineering covers misleading formatting and disguised commands |
| `markdown_security_scanner.go` | `const` | `CategoryUnicodeAbuse` | `const CategoryUnicodeAbuse SecurityFindingCategory = "unicode-abuse"` | CategoryUnicodeAbuse covers zero-width characters, bidi overrides, and control characters |
| `mcp_gateway_constants.go` | `const` | `DefaultMCPGatewayPort` | `const DefaultMCPGatewayPort = constants.DefaultMCPGatewayPort` | DefaultMCPGatewayPort is the default port for the MCP gateway This is now an alias to the constant defined in pkg/constants for backwards compatibility with existing code. |
| `permissions.go` | `const` | `PermissionActions` | `const PermissionActions PermissionScope = "actions"` | GitHub Actions permission scopes (supported by GITHUB_TOKEN), except organization-projects which is declared here for historical grouping but treated as GitHub App-only by GetAllGitHubAppOnlyScopes/IsGitHubAppOnlyScope. |
| `permissions.go` | `const` | `PermissionAdministration` | `const PermissionAdministration PermissionScope = "administration"` | Repository-level GitHub App permissions |
| `permissions.go` | `const` | `PermissionAttestations` | `const PermissionAttestations PermissionScope = "attestations"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionChecks` | `const PermissionChecks PermissionScope = "checks"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionCodespaces` | `const PermissionCodespaces PermissionScope = "codespaces"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionCodespacesLifecycleAdmin` | `const PermissionCodespacesLifecycleAdmin PermissionScope = "codespaces-lifecycle-admin"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionCodespacesMetadata` | `const PermissionCodespacesMetadata PermissionScope = "codespaces-metadata"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionContents` | `const PermissionContents PermissionScope = "contents"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionCopilotRequests` | `const PermissionCopilotRequests PermissionScope = "copilot-requests"` | PermissionCopilotRequests is a GitHub Actions permission scope that enables use of the GitHub Actions token as the Copilot authentication token. |
| `permissions.go` | `const` | `PermissionDeployments` | `const PermissionDeployments PermissionScope = "deployments"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionDiscussions` | `const PermissionDiscussions PermissionScope = "discussions"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionEmailAddresses` | `const PermissionEmailAddresses PermissionScope = "email-addresses"` | User-level GitHub App permissions |
| `permissions.go` | `const` | `PermissionEnvironments` | `const PermissionEnvironments PermissionScope = "environments"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionGitSigning` | `const PermissionGitSigning PermissionScope = "git-signing"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionIdToken` | `const PermissionIdToken PermissionScope = "id-token"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionIssues` | `const PermissionIssues PermissionScope = "issues"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionMembers` | `const PermissionMembers PermissionScope = "members"` | Organization-level GitHub App permissions |
| `permissions.go` | `const` | `PermissionMetadata` | `const PermissionMetadata PermissionScope = "metadata"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionModels` | `const PermissionModels PermissionScope = "models"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionNone` | `const PermissionNone PermissionLevel = "none"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionOrganizationAdministration` | `const PermissionOrganizationAdministration PermissionScope = "organization-administration"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionOrganizationAnnouncementBanners` | `const PermissionOrganizationAnnouncementBanners PermissionScope = "organization-announcement-banners"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionOrganizationCodespaces` | `const PermissionOrganizationCodespaces PermissionScope = "organization-codespaces"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionOrganizationCopilot` | `const PermissionOrganizationCopilot PermissionScope = "organization-copilot"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionOrganizationCustomOrgRoles` | `const PermissionOrganizationCustomOrgRoles PermissionScope = "organization-custom-org-roles"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionOrganizationCustomProperties` | `const PermissionOrganizationCustomProperties PermissionScope = "organization-custom-properties"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionOrganizationCustomRepositoryRoles` | `const PermissionOrganizationCustomRepositoryRoles PermissionScope = "organization-custom-repository-roles"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionOrganizationEvents` | `const PermissionOrganizationEvents PermissionScope = "organization-events"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionOrganizationHooks` | `const PermissionOrganizationHooks PermissionScope = "organization-hooks"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionOrganizationMembers` | `const PermissionOrganizationMembers PermissionScope = "organization-members"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionOrganizationPackages` | `const PermissionOrganizationPackages PermissionScope = "organization-packages"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionOrganizationPersonalAccessTokenReqs` | `const PermissionOrganizationPersonalAccessTokenReqs PermissionScope = "organization-personal-access-token-requests"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionOrganizationPersonalAccessTokens` | `const PermissionOrganizationPersonalAccessTokens PermissionScope = "organization-personal-access-tokens"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionOrganizationPlan` | `const PermissionOrganizationPlan PermissionScope = "organization-plan"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionOrganizationProj` | `const PermissionOrganizationProj PermissionScope = "organization-projects"` | PermissionOrganizationProj is declared here for constant grouping but is treated as GitHub App-only at runtime (excluded from GetAllPermissionScopes(), included in GetAllGitHubAppOnlyScopes() and IsGitHubAppOnlyScope). |
| `permissions.go` | `const` | `PermissionOrganizationSelfHostedRunners` | `const PermissionOrganizationSelfHostedRunners PermissionScope = "organization-self-hosted-runners"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionOrganizationUserBlocking` | `const PermissionOrganizationUserBlocking PermissionScope = "organization-user-blocking"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionPackages` | `const PermissionPackages PermissionScope = "packages"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionPages` | `const PermissionPages PermissionScope = "pages"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionPullRequests` | `const PermissionPullRequests PermissionScope = "pull-requests"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionRead` | `const PermissionRead PermissionLevel = "read"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionRepositoryCustomProperties` | `const PermissionRepositoryCustomProperties PermissionScope = "repository-custom-properties"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionRepositoryHooks` | `const PermissionRepositoryHooks PermissionScope = "repository-hooks"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionRepositoryProj` | `const PermissionRepositoryProj PermissionScope = "repository-projects"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionSecurityEvents` | `const PermissionSecurityEvents PermissionScope = "security-events"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionSingleFile` | `const PermissionSingleFile PermissionScope = "single-file"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionStatuses` | `const PermissionStatuses PermissionScope = "statuses"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionTeamDiscussions` | `const PermissionTeamDiscussions PermissionScope = "team-discussions"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionVulnerabilityAlerts` | `const PermissionVulnerabilityAlerts PermissionScope = "vulnerability-alerts"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionWorkflows` | `const PermissionWorkflows PermissionScope = "workflows"` | Exported constant declared in `permissions.go`. |
| `permissions.go` | `const` | `PermissionWrite` | `const PermissionWrite PermissionLevel = "write"` | Exported constant declared in `permissions.go`. |
| `pi_engine.go` | `const` | `PiStreamingLogFile` | `const PiStreamingLogFile = "/tmp/gh-aw/pi-streaming.jsonl"` | PiStreamingLogFile is the path where Pi CLI writes its streaming JSONL event log. |
| `publish_artifacts.go` | `const` | `SafeOutputsUploadArtifactStagingArtifactName` | `const SafeOutputsUploadArtifactStagingArtifactName = "safe-outputs-upload-artifacts"` | SafeOutputsUploadArtifactStagingArtifactName is the artifact that carries the staging directory from the main agent job to the upload_artifact job. |
| `repo_config.go` | `const` | `DefaultActionFailureIssueExpiresHours` | `const DefaultActionFailureIssueExpiresHours = 24 * 7` | DefaultActionFailureIssueExpiresHours is the default expiration (in hours) for action failure issues created by the conclusion job. |
| `repo_config.go` | `const` | `RepoConfigFileName` | `const RepoConfigFileName = ".github/workflows/aw.json"` | RepoConfigFileName is the path of the repository-level configuration file relative to the git root. |
| `safe_outputs_validation.go` | `const` | `SafeOutputsURLsPolicyAllowedOnly` | `const SafeOutputsURLsPolicyAllowedOnly = "allowed-only"` | Exported constant declared in `safe_outputs_validation.go`. |
| `safe_outputs_validation.go` | `const` | `SafeOutputsURLsPolicyAllowedOrCodeRegion` | `const SafeOutputsURLsPolicyAllowedOrCodeRegion = "allowed-or-code-region"` | Exported constant declared in `safe_outputs_validation.go`. |
| `safe_outputs_validation_config.go` | `const` | `MaxBodyLength` | `const MaxBodyLength = 65000` | Constants for validation |
| `safe_outputs_validation_config.go` | `const` | `MaxGitHubTeamSlugLength` | `const MaxGitHubTeamSlugLength = 100` | Constants for validation |
| `safe_outputs_validation_config.go` | `const` | `MaxGitHubUsernameLength` | `const MaxGitHubUsernameLength = 39` | Constants for validation |
| `safe_outputs_validation_config.go` | `const` | `MinDiscussionBodyLength` | `const MinDiscussionBodyLength = 64` | Constants for validation |
| `safe_outputs_validation_config.go` | `const` | `MinIssueBodyLength` | `const MinIssueBodyLength = 20` | Constants for validation |
| `safe_outputs_validation_config.go` | `const` | `MinReleaseBodyLength` | `const MinReleaseBodyLength = 20` | Constants for validation |
| `sandbox.go` | `const` | `AgentRuntimeDockerSbx` | `const AgentRuntimeDockerSbx AgentRuntime = "docker-sbx"` | AgentRuntimeDockerSbx runs the agent inside a Docker sbx microVM with hypervisor-level isolation (KVM). |
| `sandbox.go` | `const` | `AgentRuntimeGVisor` | `const AgentRuntimeGVisor AgentRuntime = "gvisor"` | AgentRuntimeGVisor runs the agent container under gVisor's runsc runtime for additional kernel-level isolation. |
| `setup_action_paths.go` | `const` | `GhAwBinaryPath` | `const GhAwBinaryPath = constants.GhAwRootDirShell + "/gh-aw"` | GhAwBinaryPath is the path to the gh-aw binary on the runner |
| `setup_action_paths.go` | `const` | `GhAwMCPScriptsDir` | `const GhAwMCPScriptsDir = constants.GhAwRootDirShell + "/mcp-scripts"` | GhAwMCPScriptsDir is the directory for MCP scripts files on the runner |
| `setup_action_paths.go` | `const` | `SafeJobsDownloadDir` | `const SafeJobsDownloadDir = constants.GhAwRootDirShell + "/safe-jobs/"` | SafeJobsDownloadDir is the directory for safe job files on the runner |
| `setup_action_paths.go` | `const` | `SafeJobsDownloadDirExpr` | `const SafeJobsDownloadDirExpr = constants.GhAwRootDir + "/safe-jobs/"` | SafeJobsDownloadDirExpr is SafeJobsDownloadDir in Actions expression form. |
| `setup_action_paths.go` | `const` | `SafeOutputsDir` | `const SafeOutputsDir = constants.GhAwRootDir + "/safeoutputs"` | SafeOutputsDir is the directory for safe-outputs files on the runner. |
| `setup_action_paths.go` | `const` | `SafeOutputsDirShell` | `const SafeOutputsDirShell = constants.GhAwRootDirShell + "/safeoutputs"` | SafeOutputsDirShell is the same as SafeOutputsDir but uses the shell env var form. |
| `setup_action_paths.go` | `const` | `SafeOutputsUploadArtifactsDir` | `const SafeOutputsUploadArtifactsDir = SafeOutputsDirShell + "/upload-artifacts"` | SafeOutputsUploadArtifactsDir is the upload-artifacts staging directory in shell env var form. |
| `setup_action_paths.go` | `const` | `SetupActionDestination` | `const SetupActionDestination = constants.GhAwRootDir + "/actions"` | SetupActionDestination is the path where the setup action copies script files on the agent runner (e. |
| `setup_action_paths.go` | `const` | `SetupActionDestinationShell` | `const SetupActionDestinationShell = constants.GhAwRootDirShell + "/actions"` | SetupActionDestinationShell is the same as SetupActionDestination but uses the shell env var form for use inside `run:` blocks. |
| `step_order_validation.go` | `const` | `StepTypeArtifactUpload` | `const StepTypeArtifactUpload` | Exported constant declared in `step_order_validation.go`. |
| `step_order_validation.go` | `const` | `StepTypeOther` | `const StepTypeOther` | Exported constant declared in `step_order_validation.go`. |
| `step_order_validation.go` | `const` | `StepTypeSecretRedaction` | `const StepTypeSecretRedaction StepType = iota` | Exported constant declared in `step_order_validation.go`. |
| `time_delta.go` | `const` | `MaxTimeDeltaDays` | `const MaxTimeDeltaDays = 365` | MaxTimeDeltaDays is the maximum allowed days in a time delta (1 year, non-leap) |
| `time_delta.go` | `const` | `MaxTimeDeltaHours` | `const MaxTimeDeltaHours = 8760` | MaxTimeDeltaHours is the maximum allowed hours in a time delta (365 days * 24 hours) |
| `time_delta.go` | `const` | `MaxTimeDeltaMinutes` | `const MaxTimeDeltaMinutes = 525600` | MaxTimeDeltaMinutes is the maximum allowed minutes in a time delta (365 days * 24 hours * 60 minutes) |
| `time_delta.go` | `const` | `MaxTimeDeltaMonths` | `const MaxTimeDeltaMonths = 12` | MaxTimeDeltaMonths is the maximum allowed months in a time delta (1 year) |
| `time_delta.go` | `const` | `MaxTimeDeltaWeeks` | `const MaxTimeDeltaWeeks = 52` | MaxTimeDeltaWeeks is the maximum allowed weeks in a time delta (approximately 1 year) |
| `tools_types.go` | `const` | `GitHubIntegrityApproved` | `const GitHubIntegrityApproved GitHubIntegrityLevel = "approved"` | GitHubIntegrityApproved requires approved-level integrity |
| `tools_types.go` | `const` | `GitHubIntegrityMerged` | `const GitHubIntegrityMerged GitHubIntegrityLevel = "merged"` | GitHubIntegrityMerged requires merged-level integrity |
| `tools_types.go` | `const` | `GitHubIntegrityNone` | `const GitHubIntegrityNone GitHubIntegrityLevel = "none"` | GitHubIntegrityNone allows access with no integrity requirements |
| `tools_types.go` | `const` | `GitHubIntegrityUnapproved` | `const GitHubIntegrityUnapproved GitHubIntegrityLevel = "unapproved"` | GitHubIntegrityUnapproved requires unapproved-level integrity |
| `tools_types.go` | `const` | `GitHubMCPModeCLI` | `const GitHubMCPModeCLI GitHubMCPMode = "cli"` | GitHubMCPModeCLI is a legacy alias for GitHubMCPModeGHProxy. |
| `tools_types.go` | `const` | `GitHubMCPModeGHProxy` | `const GitHubMCPModeGHProxy GitHubMCPMode = "gh-proxy"` | GitHubMCPModeGHProxy routes GitHub operations through the gh CLI proxy. |
| `tools_types.go` | `const` | `GitHubMCPModeLocal` | `const GitHubMCPModeLocal GitHubMCPMode = "local"` | GitHubMCPModeLocal runs the GitHub MCP server as a Docker container on the runner. |
| `tools_types.go` | `const` | `GitHubMCPModeRemote` | `const GitHubMCPModeRemote GitHubMCPMode = "remote"` | GitHubMCPModeRemote connects to the hosted GitHub MCP service. |
| `universal_llm_consumer_engine.go` | `const` | `UniversalLLMBackendAnthropic` | `const UniversalLLMBackendAnthropic UniversalLLMBackend = "anthropic"` | Exported constant declared in `universal_llm_consumer_engine.go`. |
| `universal_llm_consumer_engine.go` | `const` | `UniversalLLMBackendCodex` | `const UniversalLLMBackendCodex UniversalLLMBackend = "codex"` | Exported constant declared in `universal_llm_consumer_engine.go`. |
| `universal_llm_consumer_engine.go` | `const` | `UniversalLLMBackendCopilot` | `const UniversalLLMBackendCopilot UniversalLLMBackend = "copilot"` | Exported constant declared in `universal_llm_consumer_engine.go`. |
| `update_entity_helpers.go` | `const` | `FieldParsingBoolValue` | `const FieldParsingBoolValue` | FieldParsingBoolValue mode: Field's boolean value determines if it can be updated. |
| `update_entity_helpers.go` | `const` | `FieldParsingKeyExistence` | `const FieldParsingKeyExistence FieldParsingMode = iota` | FieldParsingKeyExistence mode: Field presence (even if nil) indicates it can be updated Used by update-issue and update-discussion |
| `update_entity_helpers.go` | `const` | `FieldParsingTemplatableBool` | `const FieldParsingTemplatableBool` | FieldParsingTemplatableBool mode: Field accepts a literal boolean or a GitHub Actions expression string (e. |
| `update_entity_helpers.go` | `const` | `UpdateEntityDiscussion` | `const UpdateEntityDiscussion UpdateEntityType = "discussion"` | Exported constant declared in `update_entity_helpers.go`. |
| `update_entity_helpers.go` | `const` | `UpdateEntityIssue` | `const UpdateEntityIssue UpdateEntityType = "issue"` | Exported constant declared in `update_entity_helpers.go`. |
| `update_entity_helpers.go` | `const` | `UpdateEntityPullRequest` | `const UpdateEntityPullRequest UpdateEntityType = "pull_request"` | Exported constant declared in `update_entity_helpers.go`. |
| `update_entity_helpers.go` | `const` | `UpdateEntityRelease` | `const UpdateEntityRelease UpdateEntityType = "release"` | Exported constant declared in `update_entity_helpers.go`. |
| `domains.go` | `var` | `ClaudeDefaultDomains` | `var ClaudeDefaultDomains = []string{ "*.githubusercontent.com", "anthropic.com", "api.anthropic.com", "api…` | ClaudeDefaultDomains are the default domains required for Claude Code CLI authentication and operation |
| `domains.go` | `var` | `CodexDefaultDomains` | `var CodexDefaultDomains = []string{ "172.30.0.1", "api.github.com", "api.openai.com", "chatgpt.com", "git…` | CodexDefaultDomains are the minimal default domains required for Codex CLI operation |
| `domains.go` | `var` | `CopilotDefaultDomains` | `var CopilotDefaultDomains = []string{ "api.business.githubcopilot.com", "api.enterprise.githubcopilot.com",…` | CopilotDefaultDomains are the default domains required for GitHub Copilot CLI authentication and operation |
| `domains.go` | `var` | `CrushBaseDefaultDomains` | `var CrushBaseDefaultDomains = []string{ "host.docker.internal", "charm.land", "github.com", "raw.githubuserco…` | CrushBaseDefaultDomains are the default domains required for Crush CLI operation. |
| `domains.go` | `var` | `CrushDefaultDomains` | `var CrushDefaultDomains = []string{ "api.githubcopilot.com", "api.openai.com", "generativelanguage.google…` | CrushDefaultDomains are the static default domains for backward compatibility. |
| `domains.go` | `var` | `GeminiDefaultDomains` | `var GeminiDefaultDomains = []string{ "*.googleapis.com", "generativelanguage.googleapis.com", "github.com"…` | GeminiDefaultDomains are the default domains required for Google Gemini CLI authentication and operation. |
| `domains.go` | `var` | `PiBaseDefaultDomains` | `var PiBaseDefaultDomains = []string{ "host.docker.internal", "github.com", "raw.githubusercon…` | PiBaseDefaultDomains are the base domains required for the Pi CLI to operate, independent of the chosen LLM provider. |
| `domains.go` | `var` | `PiDefaultDomains` | `var PiDefaultDomains = []string{ "api.githubcopilot.com", "host.docker.internal", "github…` | PiDefaultDomains are the static default domains for backward compatibility when no model provider prefix is given. |
| `domains.go` | `var` | `PlaywrightDomains` | `var PlaywrightDomains = []string{ "cdn.playwright.dev", "playwright.download.prss.microsoft.com", }` | PlaywrightDomains are the domains required for Playwright browser downloads These domains are needed when Playwright MCP server initializes in the Docker container |
| `expression_patterns.go` | `var` | `AWImportInputsExpressionPattern` | `var AWImportInputsExpressionPattern = regexp.MustCompile(`\$\{\{\s*github\.aw\.import-inputs\.([a-zA-Z0-9_-]+(?:\.[a-…` | AWImportInputsExpressionPattern matches full ${{ github. |
| `expression_patterns.go` | `var` | `AWImportInputsPattern` | `var AWImportInputsPattern = regexp.MustCompile(`^github\.aw\.import-inputs\.[a-zA-Z0-9_-]+(?:\.[a-zA-Z0-9_-…` | AWImportInputsPattern matches github. |
| `expression_patterns.go` | `var` | `AWInputsExpressionPattern` | `var AWInputsExpressionPattern = regexp.MustCompile(`\$\{\{\s*github\.aw\.inputs\.([a-zA-Z0-9_-]+)\s*\}\}`)` | AWInputsExpressionPattern matches full ${{ github. |
| `expression_patterns.go` | `var` | `AWInputsPattern` | `var AWInputsPattern = regexp.MustCompile(`^github\.aw\.inputs\.[a-zA-Z0-9_-]+$`)` | AWInputsPattern matches github. |
| `expression_patterns.go` | `var` | `ComparisonExtractionPattern` | `var ComparisonExtractionPattern = regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_.]*)\s*(?:==\|!=\|<\|>\|<=\|>=)\s*`)` | ComparisonExtractionPattern extracts property accesses from comparison expressions Matches patterns like "github. |
| `expression_patterns.go` | `var` | `EnvPattern` | `var EnvPattern = regexp.MustCompile(`^env\.[a-zA-Z0-9_-]+$`)` | EnvPattern matches env. |
| `expression_patterns.go` | `var` | `ExpressionPattern` | `var ExpressionPattern = regexp.MustCompile(`\$\{\{(.*?)\}\}`)` | ExpressionPattern matches GitHub Actions expressions: ${{ . |
| `expression_patterns.go` | `var` | `ExpressionPatternDotAll` | `var ExpressionPatternDotAll = regexp.MustCompile(`(?s)\$\{\{(.*?)\}\}`)` | ExpressionPatternDotAll matches expressions with dotall mode enabled The (? |
| `expression_patterns.go` | `var` | `InlineExpressionPattern` | `var InlineExpressionPattern = regexp.MustCompile(`\$\{\{[^}]+\}\}`)` | InlineExpressionPattern matches inline ${{ . |
| `expression_patterns.go` | `var` | `InputsPattern` | `var InputsPattern = regexp.MustCompile(`^github\.event\.inputs\.[a-zA-Z0-9_-]+$`)` | InputsPattern matches github. |
| `expression_patterns.go` | `var` | `NeedsStepsPattern` | `var NeedsStepsPattern = regexp.MustCompile(`^(needs\|steps)\.[a-zA-Z0-9_-]+(\.[a-zA-Z0-9_-]+)*$`)` | NeedsStepsPattern matches needs. |
| `expression_patterns.go` | `var` | `NumberLiteralPattern` | `var NumberLiteralPattern = regexp.MustCompile(`^-?\d+(\.\d+)?$`)` | NumberLiteralPattern matches numeric literals (integers and decimals) Example: 42, -3. |
| `expression_patterns.go` | `var` | `OrPattern` | `var OrPattern = regexp.MustCompile(`^(.+?)\s*\\|\\|\s*(.+)$`)` | OrPattern matches logical OR expressions Example: value1 \|\| value2 |
| `expression_patterns.go` | `var` | `RangePattern` | `var RangePattern = regexp.MustCompile(`^\d+-\d+$`)` | RangePattern matches numeric range patterns Example: 1-10, 100-200 |
| `expression_patterns.go` | `var` | `SecretExpressionPattern` | `var SecretExpressionPattern = regexp.MustCompile(`\$\{\{\s*secrets\.([A-Z_][A-Z0-9_]*)\s*(?:\\|\\|.*?)?\s*\}\}`)` | SecretExpressionPattern matches ${{ secrets. |
| `expression_patterns.go` | `var` | `SecretsExpressionPattern` | `var SecretsExpressionPattern = regexp.MustCompile(`^\$\{\{\s*secrets\.[A-Za-z_][A-Za-z0-9_]*(\s*\\|\\|\s*secrets…` | SecretsExpressionPattern validates complete secrets expression syntax Supports chained \|\| fallbacks: ${{ secrets. |
| `expression_patterns.go` | `var` | `StringLiteralPattern` | `var StringLiteralPattern = regexp.MustCompile(`^'[^']*'$\|^"[^"]*"$\|^` + "`[^`]*`$")` | StringLiteralPattern matches string literals in single quotes, double quotes, or backticks Example: 'hello', "world", `template` |
| `expression_patterns.go` | `var` | `TemplateElseIfPattern` | `var TemplateElseIfPattern = regexp.MustCompile(`\{\{#?else[-_]?if\s+((?:\$\{\{[^\}]*\}\}\|[^\}\{]\|\{[^\{])*)…` | TemplateElseIfPattern matches elseif/else-if/else_if template conditionals in all supported syntax variants: {{#elseif expr}} {{#else-if expr}} {{#else_if expr}} {{elseif expr}} {{else-if expr}} {{else_if expr}} Capture… |
| `expression_patterns.go` | `var` | `TemplateIfPattern` | `var TemplateIfPattern = regexp.MustCompile(`\{\{#if\s+((?:\$\{\{[^\}]*\}\}\|[^\}\{]\|\{[^\{])*)\s*\}\}`)` | TemplateIfPattern matches {{#if condition }} template conditionals Captures the condition expression (which may contain ${{ . |
| `expression_patterns.go` | `var` | `UnsafeContextPattern` | `var UnsafeContextPattern = regexp.MustCompile(`\$\{\{\s*(github\.event\.\|steps\.[^}]+\.outputs\.\|inputs\.)…` | UnsafeContextPattern matches potentially unsafe context patterns These patterns may allow injection attacks in templates |
| `expression_patterns.go` | `var` | `WorkflowCallInputsPattern` | `var WorkflowCallInputsPattern = regexp.MustCompile(`^inputs\.[a-zA-Z0-9_-]+$`)` | WorkflowCallInputsPattern matches inputs. |
| `github_toolsets.go` | `var` | `ActionFriendlyGitHubToolsets` | `var ActionFriendlyGitHubToolsets = []string{"context", "repos", "issues", "pull_requests"}` | ActionFriendlyGitHubToolsets defines the default toolsets that work with GitHub Actions tokens. |
| `github_toolsets.go` | `var` | `DefaultGitHubToolsets` | `var DefaultGitHubToolsets = []string{"context", "repos", "issues", "pull_requests"}` | DefaultGitHubToolsets defines the toolsets that are enabled by default when toolsets are not explicitly specified in the GitHub MCP configuration. |
| `github_toolsets.go` | `var` | `GitHubToolsetsExcludedFromAll` | `var GitHubToolsetsExcludedFromAll = []string{"dependabot"}` | GitHubToolsetsExcludedFromAll defines toolsets that are NOT included when "all" is specified. |
| `lock_schema.go` | `var` | `SupportedSchemaVersions` | `var SupportedSchemaVersions = []LockSchemaVersion{ LockSchemaV1, LockSchemaV2, LockSchemaV3, LockSchemaV4, }` | SupportedSchemaVersions lists all schema versions this build can consume |
| `mcp_github_config.go` | `var` | `DefaultDisapprovalReactions` | `var DefaultDisapprovalReactions = []string{"THUMBS_DOWN", "CONFUSED"}` | DefaultDisapprovalReactions are the default disapproval reactions injected when the integrity-reactions feature flag is enabled but no explicit disapproval-reactions are set. |
| `mcp_github_config.go` | `var` | `DefaultEndorsementReactions` | `var DefaultEndorsementReactions = []string{"THUMBS_UP", "HEART"}` | DefaultEndorsementReactions are the default endorsement reactions injected when the integrity-reactions feature flag is enabled but no explicit endorsement-reactions are set. |
| `mcp_github_default_fields.go` | `var` | `GitHubMCPDefaultFields` | `var GitHubMCPDefaultFields = map[string][]string{...}` | GitHubMCPDefaultFields defines default response fields injected for selected GitHub MCP operations. |
| `mcp_github_default_fields.go` | `const` | `GitHubMCPFeatureFieldsParam` | `const GitHubMCPFeatureFieldsParam = "fields_param"` | GitHubMCPFeatureFieldsParam names the feature-flag key that enables default field-parameter injection. |
| `npm_validation_errors.go` | `var` | `ErrNpmNotAvailable` | `var ErrNpmNotAvailable = errors.New("npm not available")` | ErrNpmNotAvailable is returned by validateNpxPackages when npm is not installed on the system. |
| `safe_outputs_validation_config.go` | `var` | `ValidationConfig` | `var ValidationConfig = map[string]TypeValidationConfig{ "create_issue": { DefaultMax: 1, Fields: map[s…` | ValidationConfig contains all safe output type validation rules This is the single source of truth for validation rules |
| `script_registry.go` | `var` | `DefaultScriptRegistry` | `var DefaultScriptRegistry = NewScriptRegistry()` | DefaultScriptRegistry is the global script registry used by the workflow package. |
| `yaml_options.go` | `var` | `DefaultMarshalOptions` | `var DefaultMarshalOptions = []yaml.EncodeOption{ yaml.Indent(2), yaml.UseLiteralStyleIfMultiline(true), }` | DefaultMarshalOptions provides standard YAML formatting options used throughout gh-aw for workflow and frontmatter generation. |

### Additional functions and methods

| File | Symbol | Declaration | Description |
|------|--------|-------------|-------------|
| `engine_helpers.go` | `BuildEngineSecretValidationStep` | `func BuildEngineSecretValidationStep(workflowData *WorkflowData, config EngineSecretValidationConfig) GitHubActionStep` | BuildEngineSecretValidationStep builds the reusable secret-validation step for engine credentials. |
| `engine_registry.go` | `(*EngineRegistry).EnginesWithCapability` | `func (r *EngineRegistry) EnginesWithCapability(predicate func(EngineCapabilities) bool) []string` | EnginesWithCapability returns sorted engine IDs that satisfy a capability predicate. |
| `agent_validation.go` | `HasBashExplicitRestriction` | `func HasBashExplicitRestriction(tools map[string]any) bool` | HasBashExplicitRestriction reports whether `tools.bash` has an explicit restriction value. |
| `repo_config.go` | `(*RepoConfig).IsActionFailureIssueExpiresExplicit` | `func (r *RepoConfig) IsActionFailureIssueExpiresExplicit() bool` | IsActionFailureIssueExpiresExplicit reports whether maintenance expiry was explicitly set in repo config. |
| `action_cache.go` | `(*ActionCache).Delete` | `func (*ActionCache).Delete(repo, version string)` | Delete removes the cache entry for the given repo and version. |
| `action_cache.go` | `(*ActionCache).DeleteByKey` | `func (*ActionCache).DeleteByKey(key string)` | DeleteByKey removes the cache entry with the given raw map key. |
| `action_cache.go` | `(*ActionCache).DeleteContainerPin` | `func (*ActionCache).DeleteContainerPin(image string)` | DeleteContainerPin removes the pin for the given image tag. |
| `action_cache.go` | `(*ActionCache).FindAnyEntryForRepo` | `func (*ActionCache).FindAnyEntryForRepo(repo string) (string, ActionCacheEntry, bool)` | FindAnyEntryForRepo finds any cache entry for the given repo, preferring the newest version (by sorting keys and taking first match). |
| `action_cache.go` | `(*ActionCache).FindEntryBySHA` | `func (*ActionCache).FindEntryBySHA(repo, sha string) (ActionCacheEntry, bool)` | FindEntryBySHA finds a cache entry with the given repo and SHA Returns the entry and true if found, or empty entry and false if not found |
| `action_cache.go` | `(*ActionCache).GetActionDescription` | `func (*ActionCache).GetActionDescription(repo, version string) (string, bool)` | GetActionDescription retrieves the cached action description for the given repo and version. |
| `action_cache.go` | `(*ActionCache).GetByCacheKey` | `func (*ActionCache).GetByCacheKey(key string) (string, bool)` | GetByCacheKey retrieves a cached entry by its pre-computed key. |
| `action_cache.go` | `(*ActionCache).GetCachePath` | `func (*ActionCache).GetCachePath() string` | GetCachePath returns the path to the cache file |
| `action_cache.go` | `(*ActionCache).GetContainerPin` | `func (*ActionCache).GetContainerPin(image string) (ContainerPin, bool)` | GetContainerPin returns the cached pin for the given image tag. |
| `action_cache.go` | `(*ActionCache).GetInputs` | `func (*ActionCache).GetInputs(repo, version string) (map[string]*ActionYAMLInput, bool)` | GetInputs retrieves the cached action inputs for the given repo and version. |
| `action_cache.go` | `(*ActionCache).GetReleasedAt` | `func (*ActionCache).GetReleasedAt(repo, version string) (time.Time, bool)` | GetReleasedAt retrieves the cached release date for the given repo and version. |
| `action_cache.go` | `(*ActionCache).Load` | `func (*ActionCache).Load() error` | Load loads the cache from disk |
| `action_cache.go` | `(*ActionCache).PruneOrphanedEntries` | `func (*ActionCache).PruneOrphanedEntries(referencedKeys map[string]struct{}) int` | PruneOrphanedEntries removes action cache entries whose keys are not present in referencedKeys. |
| `action_cache.go` | `(*ActionCache).PruneStaleContainerPins` | `func (*ActionCache).PruneStaleContainerPins(knownImages map[string]struct { }) int` | PruneStaleContainerPins removes container pin entries whose keys are not present in knownImages. |
| `action_cache.go` | `(*ActionCache).PruneStaleGHAWEntries` | `func (*ActionCache).PruneStaleGHAWEntries(currentVersion string, actionsRepoPrefix string)` | PruneStaleGHAWEntries removes entries from the cache for the gh-aw-actions repository whose version does not match the current compiler version. |
| `action_cache.go` | `(*ActionCache).Save` | `func (*ActionCache).Save() error` | Save saves the cache to disk with sorted entries If the cache is empty, the file is not created or is deleted if it exists Deduplicates entries by keeping only the most precise version reference for each repo+SHA combin… |
| `action_cache.go` | `(*ActionCache).SetActionDescription` | `func (*ActionCache).SetActionDescription(repo, version, description string)` | SetActionDescription stores the action description in the cache entry for the given repo and version. |
| `action_cache.go` | `(*ActionCache).SetContainerPin` | `func (*ActionCache).SetContainerPin(image, digest, pinnedImage string)` | SetContainerPin stores a digest pin for the given image tag. |
| `action_cache.go` | `(*ActionCache).SetInputs` | `func (*ActionCache).SetInputs(repo, version string, inputs map[string]*ActionYAMLInput)` | SetInputs stores the action inputs in the cache entry for the given repo and version. |
| `action_cache.go` | `(*ActionCache).SetReleasedAt` | `func (*ActionCache).SetReleasedAt(repo, version string, t time.Time)` | SetReleasedAt stores the release publication date for the given repo and version. |
| `action_mode.go` | `(ActionMode).IsAction` | `func (ActionMode).IsAction() bool` | IsAction returns true if the action mode is action mode (uses github/gh-aw-actions repo) |
| `action_mode.go` | `(ActionMode).IsDev` | `func (ActionMode).IsDev() bool` | IsDev returns true if the action mode is development mode |
| `action_mode.go` | `(ActionMode).IsScript` | `func (ActionMode).IsScript() bool` | IsScript returns true if the action mode is script mode |
| `action_mode.go` | `(ActionMode).IsValid` | `func (ActionMode).IsValid() bool` | IsValid checks if the action mode is valid |
| `action_mode.go` | `(ActionMode).UsesExternalActions` | `func (ActionMode).UsesExternalActions() bool` | UsesExternalActions returns true (always true since inline mode was removed) |
| `action_mode.go` | `GetActionModeFromWorkflowData` | `func GetActionModeFromWorkflowData(workflowData *WorkflowData) ActionMode` | GetActionModeFromWorkflowData extracts the ActionMode from WorkflowData, defaulting to dev mode if nil |
| `action_reference.go` | `ResolveSetupActionReference` | `func ResolveSetupActionReference(ctx context.Context, actionMode ActionMode, version string, actionTag string, resolver SHAResolver) string` | ResolveSetupActionReference resolves the actions/setup action reference based on action mode and version. |
| `action_resolver.go` | `(*ActionResolver).GetUsedCacheKeys` | `func (*ActionResolver).GetUsedCacheKeys() map[string]struct{}` | GetUsedCacheKeys returns the set of cache keys (in "repo@version" format) that were successfully resolved from the cache or written to the cache during this run. |
| `action_resolver.go` | `(*ActionResolver).MarkCacheKeyAsUsed` | `func (*ActionResolver).MarkCacheKeyAsUsed(cacheKey string)` | MarkCacheKeyAsUsed explicitly marks a cache key as used during this compilation run. |
| `action_resolver.go` | `(*ActionResolver).MarkCompilerGeneratedActionsAsUsed` | `func (*ActionResolver).MarkCompilerGeneratedActionsAsUsed()` | MarkCompilerGeneratedActionsAsUsed scans the cache for any entries matching compiler-generated action repos and marks them as used. |
| `agentic_engine.go` | `(*BaseEngine).GetAPMTarget` | `func (*BaseEngine).GetAPMTarget() string` | GetAPMTarget returns "all" by default (packs all primitive types). |
| `agentic_engine.go` | `(*BaseEngine).GetAgentManifestFiles` | `func (*BaseEngine).GetAgentManifestFiles() []string` | GetAgentManifestFiles returns nil by default (no engine-specific manifest files). |
| `agentic_engine.go` | `(*BaseEngine).GetAgentManifestPathPrefixes` | `func (*BaseEngine).GetAgentManifestPathPrefixes() []string` | GetAgentManifestPathPrefixes returns nil by default (no engine-specific config directories). |
| `agentic_engine.go` | `(*BaseEngine).GetDefaultDetectionModel` | `func (*BaseEngine).GetDefaultDetectionModel() string` | GetDefaultDetectionModel returns empty string by default (no default model) Engines can override this to provide a cost-effective default for detection jobs |
| `agentic_engine.go` | `(*BaseEngine).GetErrorDetectionScriptId` | `func (*BaseEngine).GetErrorDetectionScriptId() string` | GetErrorDetectionScriptId returns empty string by default (no post-execution error detection) Engines can override this to provide a host-runner script that detects errors in the agent stdio log and writes them as GITHU… |
| `agentic_engine.go` | `(*BaseEngine).GetFirewallLogsCollectionStep` | `func (*BaseEngine).GetFirewallLogsCollectionStep(workflowData *WorkflowData) []GitHubActionStep` | GetFirewallLogsCollectionStep returns an empty slice by default. |
| `agentic_engine.go` | `(*BaseEngine).GetGHSkillAgentName` | `func (*BaseEngine).GetGHSkillAgentName() string` | Exported function or method declared in `agentic_engine.go`. |
| `agentic_engine.go` | `(*BaseEngine).GetLogFileForParsing` | `func (*BaseEngine).GetLogFileForParsing() string` | GetLogFileForParsing returns the default log file path for parsing Engines can override this to use engine-specific log files |
| `agentic_engine.go` | `(*BaseEngine).GetLogParserScriptId` | `func (*BaseEngine).GetLogParserScriptId() string` | GetLogParserScriptId returns empty string by default (no JavaScript parser) Engines can override this to provide a JavaScript parser for log analysis |
| `agentic_engine.go` | `(*BaseEngine).GetModelEnvVarName` | `func (*BaseEngine).GetModelEnvVarName() string` | GetModelEnvVarName returns empty string by default (no native model env var). |
| `agentic_engine.go` | `(*BaseEngine).GetPreBundleSteps` | `func (*BaseEngine).GetPreBundleSteps(workflowData *WorkflowData) []GitHubActionStep` | GetPreBundleSteps returns an empty slice by default. |
| `agentic_engine.go` | `(*BaseEngine).GetRequiredSecretNames` | `func (*BaseEngine).GetRequiredSecretNames(workflowData *WorkflowData) []string` | GetRequiredSecretNames returns an empty list by default Engines must override this to specify their required secrets |
| `agentic_engine.go` | `(*BaseEngine).GetSecretValidationStep` | `func (*BaseEngine).GetSecretValidationStep(workflowData *WorkflowData) GitHubActionStep` | GetSecretValidationStep returns an empty step by default. |
| `agentic_engine.go` | `(*BaseEngine).GetSupportedEnvVarKeys` | `func (*BaseEngine).GetSupportedEnvVarKeys() []string` | GetSupportedEnvVarKeys returns an empty list by default. |
| `agentic_engine.go` | `(*BaseEngine).ParseLogMetrics` | `func (*BaseEngine).ParseLogMetrics(logContent string, verbose bool) LogMetrics` | ParseLogMetrics provides a default no-op implementation for log parsing Engines can override this to provide detailed log parsing and metrics extraction |
| `agentic_engine.go` | `(*BaseEngine).RenderConfig` | `func (*BaseEngine).RenderConfig(_ *ResolvedEngineTarget) ([]map[string]any, error)` | RenderConfig returns nil by default — engines that need to write config files before execution (e. |
| `agentic_engine.go` | `(*BaseEngine).RenderMCPConfig` | `func (*BaseEngine).RenderMCPConfig(yaml *strings.Builder, tools map[string]any, mcpTools []string, workflowData *WorkflowData) error` | RenderMCPConfig provides a default no-op implementation for MCP configuration Engines can override this to provide custom MCP server configuration |
| `agentic_engine.go` | `(*EngineRegistry).GetAllAgentManifestFiles` | `func (*EngineRegistry).GetAllAgentManifestFiles() []string` | GetAllAgentManifestFiles returns the union of all engines' GetAgentManifestFiles(). |
| `agentic_engine.go` | `(*EngineRegistry).GetAllAgentManifestFolders` | `func (*EngineRegistry).GetAllAgentManifestFolders() []string` | GetAllAgentManifestFolders returns the union of all engines' GetAgentManifestPathPrefixes() with trailing slashes stripped, plus ". |
| `agentic_engine.go` | `(*EngineRegistry).GetDefaultEngine` | `func (*EngineRegistry).GetDefaultEngine() CodingAgentEngine` | GetDefaultEngine returns the default engine configured by constants. |
| `agentic_engine.go` | `(*EngineRegistry).GetEngine` | `func (*EngineRegistry).GetEngine(id string) (CodingAgentEngine, error)` | GetEngine retrieves an engine by ID |
| `agentic_engine.go` | `(*EngineRegistry).GetEngineByPrefix` | `func (*EngineRegistry).GetEngineByPrefix(prefix string) (CodingAgentEngine, error)` | GetEngineByPrefix returns an engine that matches the given prefix This is useful for backward compatibility with strings like "codex-experimental" |
| `agentic_engine.go` | `(*EngineRegistry).GetSupportedEngines` | `func (*EngineRegistry).GetSupportedEngines() []string` | GetSupportedEngines returns a list of all supported engine IDs |
| `agentic_engine.go` | `(*EngineRegistry).IsValidEngine` | `func (*EngineRegistry).IsValidEngine(id string) bool` | IsValidEngine checks if an engine ID is valid |
| `agentic_engine.go` | `(*EngineRegistry).Register` | `func (*EngineRegistry).Register(engine CodingAgentEngine) error` | Register adds an engine to the registry. |
| `artifact_manager.go` | `(*ArtifactManager).Reset` | `func (*ArtifactManager).Reset()` | Reset clears all tracked uploads and downloads |
| `artifact_manager.go` | `NewArtifactManager` | `func NewArtifactManager() *ArtifactManager` | NewArtifactManager creates a new artifact manager |
| `auto_update_workflow.go` | `GenerateAutoUpdateWorkflow` | `func GenerateAutoUpdateWorkflow(opts GenerateAutoUpdateWorkflowOptions) error` | GenerateAutoUpdateWorkflow generates or removes the agentic-auto-upgrade. |
| `awf_config_build.go` | `BuildAWFConfigJSON` | `func BuildAWFConfigJSON(config AWFCommandConfig) (string, error)` | BuildAWFConfigJSON generates a compact JSON config file for AWF from the provided command configuration. |
| `behavior_defined_engine.go` | `(*BehaviorDefinedEngine).GetAgentManifestFiles` | `func (*BehaviorDefinedEngine).GetAgentManifestFiles() []string` | Exported function or method declared in `behavior_defined_engine.go`. |
| `behavior_defined_engine.go` | `(*BehaviorDefinedEngine).GetAgentManifestPathPrefixes` | `func (*BehaviorDefinedEngine).GetAgentManifestPathPrefixes() []string` | Exported function or method declared in `behavior_defined_engine.go`. |
| `behavior_defined_engine.go` | `(*BehaviorDefinedEngine).GetModelEnvVarName` | `func (*BehaviorDefinedEngine).GetModelEnvVarName() string` | Exported function or method declared in `behavior_defined_engine.go`. |
| `behavior_defined_engine.go` | `(*BehaviorDefinedEngine).GetRequiredSecretNames` | `func (*BehaviorDefinedEngine).GetRequiredSecretNames(workflowData *WorkflowData) []string` | Exported function or method declared in `behavior_defined_engine.go`. |
| `behavior_defined_engine.go` | `(*BehaviorDefinedEngine).GetSecretValidationStep` | `func (*BehaviorDefinedEngine).GetSecretValidationStep(workflowData *WorkflowData) GitHubActionStep` | Exported function or method declared in `behavior_defined_engine.go`. |
| `behavior_defined_engine.go` | `(*BehaviorDefinedEngine).GetSupportedEnvVarKeys` | `func (*BehaviorDefinedEngine).GetSupportedEnvVarKeys() []string` | Exported function or method declared in `behavior_defined_engine.go`. |
| `behavior_defined_engine.go` | `(*BehaviorDefinedEngine).RenderMCPConfig` | `func (*BehaviorDefinedEngine).RenderMCPConfig(sb *strings.Builder, tools map[string]any, mcpTools []string, workflowData *WorkflowData) error` | Exported function or method declared in `behavior_defined_engine.go`. |
| `behavior_defined_engine.go` | `NewBehaviorDefinedEngine` | `func NewBehaviorDefinedEngine(def *EngineDefinition) (*BehaviorDefinedEngine, error)` | Exported function or method declared in `behavior_defined_engine.go`. |
| `central_slash_command_workflow.go` | `GenerateCentralSlashCommandWorkflow` | `func GenerateCentralSlashCommandWorkflow(ctx context.Context, workflowDataList []*WorkflowData, workflowDir string, repoConfig *RepoConfig) error` | GenerateCentralSlashCommandWorkflow generates a single centralized slash-command trigger workflow for workflows that opt into on. |
| `checkout_manager.go` | `(*CheckoutManager).GetCrossRepoTargetRef` | `func (*CheckoutManager).GetCrossRepoTargetRef() string` | GetCrossRepoTargetRef returns the platform ref expression previously set by SetCrossRepoTargetRef, or an empty string if no cross-repo ref was set. |
| `checkout_manager.go` | `(*CheckoutManager).GetCrossRepoTargetRepo` | `func (*CheckoutManager).GetCrossRepoTargetRepo() string` | GetCrossRepoTargetRepo returns the platform repo expression previously set by SetCrossRepoTargetRepo, or an empty string if no cross-repo target was set (same-repo invocation or inlined imports). |
| `checkout_manager.go` | `(*CheckoutManager).GetCurrentCheckoutPath` | `func (*CheckoutManager).GetCurrentCheckoutPath() string` | GetCurrentCheckoutPath returns the current checkout path after trimming leading ". |
| `checkout_manager.go` | `(*CheckoutManager).GetCurrentRepository` | `func (*CheckoutManager).GetCurrentRepository() string` | GetCurrentRepository returns the repository slug for the checkout marked current:true. |
| `checkout_manager.go` | `(*CheckoutManager).GetDefaultCheckoutOverride` | `func (*CheckoutManager).GetDefaultCheckoutOverride() *resolvedCheckout` | GetDefaultCheckoutOverride returns the resolved checkout for the default workspace (empty path, empty repository). |
| `checkout_manager.go` | `(*CheckoutManager).HasAppAuth` | `func (*CheckoutManager).HasAppAuth() bool` | HasAppAuth returns true if any checkout entry uses GitHub App authentication. |
| `checkout_manager.go` | `(*CheckoutManager).HasExternalRootCheckout` | `func (*CheckoutManager).HasExternalRootCheckout() bool` | HasExternalRootCheckout returns true if any checkout entry targets an external repository (non-empty repository field) and writes to the workspace root (empty path). |
| `checkout_manager.go` | `(*CheckoutManager).HasSafeOutputAppAuth` | `func (*CheckoutManager).HasSafeOutputAppAuth() bool` | HasSafeOutputAppAuth returns true if any checkout entry uses safe_outputs-only GitHub App authentication. |
| `checkout_manager.go` | `(*CheckoutManager).ResolveSafeOutputCheckoutTokenExpression` | `func (*CheckoutManager).ResolveSafeOutputCheckoutTokenExpression(targetRepo string) (string, bool)` | ResolveSafeOutputCheckoutTokenExpression returns a safe_outputs checkout token expression derived from checkout. |
| `checkout_manager.go` | `(*CheckoutManager).SetCrossRepoTargetRef` | `func (*CheckoutManager).SetCrossRepoTargetRef(ref string)` | SetCrossRepoTargetRef stores the platform (host) ref expression used for . |
| `checkout_manager.go` | `(*CheckoutManager).SetCrossRepoTargetRepo` | `func (*CheckoutManager).SetCrossRepoTargetRepo(repo string)` | SetCrossRepoTargetRepo stores the platform (host) repository expression used for . |
| `checkout_manager.go` | `(*CheckoutManager).SetKeepCredentialsForPush` | `func (*CheckoutManager).SetKeepCredentialsForPush(keep bool)` | SetKeepCredentialsForPush enables credential retention on all generated checkout steps. |
| `checkout_manager.go` | `(*CheckoutManager).SetPushToken` | `func (*CheckoutManager).SetPushToken(token string)` | SetPushToken sets the token expression persisted into . |
| `checkout_manager.go` | `NewCheckoutManager` | `func NewCheckoutManager(userCheckouts []*CheckoutConfig) *CheckoutManager` | NewCheckoutManager creates a new CheckoutManager pre-loaded with user-supplied CheckoutConfig entries from the frontmatter. |
| `checkout_step_generator.go` | `(*CheckoutManager).GenerateAdditionalCheckoutSteps` | `func (*CheckoutManager).GenerateAdditionalCheckoutSteps(getActionPin func(string) string) []string` | GenerateAdditionalCheckoutSteps generates YAML step lines for all non-default (additional) checkouts — those that target a specific path other than the root. |
| `checkout_step_generator.go` | `(*CheckoutManager).GenerateCheckoutAppTokenSteps` | `func (*CheckoutManager).GenerateCheckoutAppTokenSteps(c *Compiler, permissions *Permissions) []string` | GenerateCheckoutAppTokenSteps generates GitHub App token minting steps for all checkout entries that use app authentication. |
| `checkout_step_generator.go` | `(*CheckoutManager).GenerateCheckoutManifestStep` | `func (*CheckoutManager).GenerateCheckoutManifestStep(getActionPin func(string) string) []string` | GenerateCheckoutManifestStep emits a step that writes a JSON manifest describing each non-default cross-repository checkout, keyed by lowercase repo slug. |
| `checkout_step_generator.go` | `(*CheckoutManager).GenerateConfigureGitCredentialsSteps` | `func (*CheckoutManager).GenerateConfigureGitCredentialsSteps(gitRemoteToken string, condition ConditionNode) []string` | GenerateConfigureGitCredentialsSteps emits the "Configure Git credentials" step that installs a push-capable token. |
| `checkout_step_generator.go` | `(*CheckoutManager).GenerateDefaultCheckoutStep` | `func (*CheckoutManager).GenerateDefaultCheckoutStep( trialMode bool, trialLogicalRepoSlug string, getActionPin func(string) string, ) []string` | GenerateDefaultCheckoutStep emits the default workspace checkout, applying any user-supplied overrides (token, fetch-depth, ref, etc. |
| `checkout_step_generator.go` | `(*CheckoutManager).GenerateGitHubFolderCheckoutStep` | `func (*CheckoutManager).GenerateGitHubFolderCheckoutStep(repository, ref, token string, getActionPin func(string) string, extraPaths ...string) []string` | GenerateGitHubFolderCheckoutStep generates YAML step lines for a sparse checkout of the . |
| `checkout_step_generator.go` | `(*CheckoutManager).GenerateSafeOutputCheckoutAppTokenSteps` | `func (*CheckoutManager).GenerateSafeOutputCheckoutAppTokenSteps(c *Compiler, permissions *Permissions) []string` | GenerateSafeOutputCheckoutAppTokenSteps generates GitHub App token minting steps for checkout. |
| `claude_engine.go` | `(*ClaudeEngine).GetAPMTarget` | `func (*ClaudeEngine).GetAPMTarget() string` | GetAPMTarget returns "claude" so that apm-action packs Claude-specific primitives. |
| `claude_engine.go` | `(*ClaudeEngine).GetAgentManifestFiles` | `func (*ClaudeEngine).GetAgentManifestFiles() []string` | GetAgentManifestFiles returns Claude-specific instruction files that should be treated as security-sensitive manifests. |
| `claude_engine.go` | `(*ClaudeEngine).GetAgentManifestPathPrefixes` | `func (*ClaudeEngine).GetAgentManifestPathPrefixes() []string` | GetAgentManifestPathPrefixes returns Claude-specific config directory prefixes. |
| `claude_engine.go` | `(*ClaudeEngine).GetErrorDetectionScriptId` | `func (*ClaudeEngine).GetErrorDetectionScriptId() string` | GetErrorDetectionScriptId returns the JavaScript script name for detecting post-run agent errors from the host runner (including invalid/unsupported model names). |
| `claude_engine.go` | `(*ClaudeEngine).GetHarnessScriptName` | `func (*ClaudeEngine).GetHarnessScriptName() string` | GetHarnessScriptName returns the filename of the JavaScript harness script that wraps the Claude Code CLI with retry logic for transient Anthropic API errors (overload, rate limit). |
| `claude_engine.go` | `(*ClaudeEngine).GetLogParserScriptId` | `func (*ClaudeEngine).GetLogParserScriptId() string` | GetLogParserScriptId returns the JavaScript script name for parsing Claude logs |
| `claude_engine.go` | `(*ClaudeEngine).GetModelEnvVarName` | `func (*ClaudeEngine).GetModelEnvVarName() string` | GetModelEnvVarName returns the native environment variable name that the Claude Code CLI uses for model selection. |
| `claude_engine.go` | `(*ClaudeEngine).GetRequiredSecretNames` | `func (*ClaudeEngine).GetRequiredSecretNames(workflowData *WorkflowData) []string` | GetRequiredSecretNames returns the list of secrets required by the Claude engine. |
| `claude_engine.go` | `(*ClaudeEngine).GetSecretValidationStep` | `func (*ClaudeEngine).GetSecretValidationStep(workflowData *WorkflowData) GitHubActionStep` | GetSecretValidationStep returns the secret validation step for the Claude engine. |
| `claude_engine.go` | `(*ClaudeEngine).GetSquidLogsSteps` | `func (*ClaudeEngine).GetSquidLogsSteps(workflowData *WorkflowData) []GitHubActionStep` | GetSquidLogsSteps returns the steps for uploading and parsing Squid logs (after secret redaction) |
| `claude_engine.go` | `(*ClaudeEngine).GetSupportedEnvVarKeys` | `func (*ClaudeEngine).GetSupportedEnvVarKeys() []string` | GetSupportedEnvVarKeys returns the engine. |
| `claude_engine.go` | `(*ClaudeEngine).ResolveLLMProvider` | `func (*ClaudeEngine).ResolveLLMProvider(workflowData *WorkflowData) string` | ResolveLLMProvider returns the effective provider for Claude inference. |
| `claude_logs.go` | `(*ClaudeEngine).ParseLogMetrics` | `func (*ClaudeEngine).ParseLogMetrics(logContent string, verbose bool) LogMetrics` | ParseLogMetrics implements engine-specific log parsing for Claude |
| `claude_mcp.go` | `(*ClaudeEngine).RenderMCPConfig` | `func (*ClaudeEngine).RenderMCPConfig(yaml *strings.Builder, tools map[string]any, mcpTools []string, workflowData *WorkflowData) error` | RenderMCPConfig renders the MCP configuration for Claude engine |
| `codex_engine.go` | `(*CodexEngine).GetAgentManifestFiles` | `func (*CodexEngine).GetAgentManifestFiles() []string` | GetAgentManifestFiles returns Codex-specific instruction files that should be treated as security-sensitive manifests. |
| `codex_engine.go` | `(*CodexEngine).GetAgentManifestPathPrefixes` | `func (*CodexEngine).GetAgentManifestPathPrefixes() []string` | GetAgentManifestPathPrefixes returns Codex-specific config directory prefixes. |
| `codex_engine.go` | `(*CodexEngine).GetHarnessScriptName` | `func (*CodexEngine).GetHarnessScriptName() string` | GetHarnessScriptName returns the filename of the JavaScript harness script that wraps Codex CLI execution with retry logic for transient OpenAI API errors. |
| `codex_engine.go` | `(*CodexEngine).GetModelEnvVarName` | `func (*CodexEngine).GetModelEnvVarName() string` | GetModelEnvVarName returns an empty string because the Codex CLI does not support selecting the model via a native environment variable. |
| `codex_engine.go` | `(*CodexEngine).GetRequiredSecretNames` | `func (*CodexEngine).GetRequiredSecretNames(workflowData *WorkflowData) []string` | GetRequiredSecretNames returns the list of secrets required by the Codex engine This includes CODEX_API_KEY, OPENAI_API_KEY, and optionally MCP_GATEWAY_AGENT_ID and mcp-scripts secrets |
| `codex_engine.go` | `(*CodexEngine).GetSecretValidationStep` | `func (*CodexEngine).GetSecretValidationStep(workflowData *WorkflowData) GitHubActionStep` | GetSecretValidationStep returns the secret validation step for the Codex engine. |
| `codex_engine.go` | `(*CodexEngine).GetSquidLogsSteps` | `func (*CodexEngine).GetSquidLogsSteps(workflowData *WorkflowData) []GitHubActionStep` | GetSquidLogsSteps returns the steps for uploading and parsing Squid logs (after secret redaction) |
| `codex_engine.go` | `(*CodexEngine).GetSupportedEnvVarKeys` | `func (*CodexEngine).GetSupportedEnvVarKeys() []string` | GetSupportedEnvVarKeys returns the engine. |
| `codex_engine.go` | `(*CodexEngine).ResolveLLMProvider` | `func (*CodexEngine).ResolveLLMProvider(workflowData *WorkflowData) string` | ResolveLLMProvider returns the effective provider for Codex inference. |
| `codex_logs.go` | `(*CodexEngine).GetErrorDetectionScriptId` | `func (*CodexEngine).GetErrorDetectionScriptId() string` | GetErrorDetectionScriptId returns the JavaScript script name for detecting post-run agent errors from the host runner (including invalid/unsupported model names). |
| `codex_logs.go` | `(*CodexEngine).GetLogParserScriptId` | `func (*CodexEngine).GetLogParserScriptId() string` | GetLogParserScriptId returns the JavaScript script name for parsing Codex logs |
| `codex_logs.go` | `(*CodexEngine).ParseLogMetrics` | `func (*CodexEngine).ParseLogMetrics(logContent string, verbose bool) LogMetrics` | ParseLogMetrics implements engine-specific log parsing for Codex |
| `codex_mcp.go` | `(*CodexEngine).RenderMCPConfig` | `func (*CodexEngine).RenderMCPConfig(yaml *strings.Builder, tools map[string]any, mcpTools []string, workflowData *WorkflowData) error` | RenderMCPConfig generates MCP server configuration for Codex |
| `comment.go` | `FilterCommentEvents` | `func FilterCommentEvents(identifiers []string) []CommentEventMapping` | FilterCommentEvents returns only the comment events specified by the identifiers If identifiers is nil or empty, returns all comment events |
| `comment.go` | `GetActualGitHubEventName` | `func GetActualGitHubEventName(identifier string) string` | GetActualGitHubEventName returns the actual GitHub Actions event name for a given identifier This maps pull_request_comment to issue_comment since that's the actual event in GitHub Actions |
| `comment.go` | `GetAllCommentEvents` | `func GetAllCommentEvents() []CommentEventMapping` | GetAllCommentEvents returns all possible comment-related events for command triggers |
| `comment.go` | `GetCommentEventByIdentifier` | `func GetCommentEventByIdentifier(identifier string) *CommentEventMapping` | GetCommentEventByIdentifier returns the event mapping for a given identifier Uses GitHub Actions event names (e. |
| `comment.go` | `GetCommentEventNames` | `func GetCommentEventNames(mappings []CommentEventMapping) []string` | GetCommentEventNames returns just the event names from a list of mappings |
| `comment.go` | `MergeEventsForYAML` | `func MergeEventsForYAML(mappings []CommentEventMapping) []CommentEventMapping` | MergeEventsForYAML merges comment events for YAML generation, combining pull_request_comment and issue_comment |
| `comment.go` | `ParseCommandEvents` | `func ParseCommandEvents(eventsValue any) []string` | ParseCommandEvents parses the events field from command configuration Returns a list of event identifiers to enable, or nil for default (all events) |
| `compiler_experiments.go` | `ExperimentExpressionMappings` | `func ExperimentExpressionMappings(experiments map[string][]string) []*ExpressionMapping` | ExperimentExpressionMappings generates ExpressionMapping entries for all declared experiments. |
| `compiler_filters_validation.go` | `ValidatePushBranchScope` | `func ValidatePushBranchScope(frontmatter map[string]any) error` | ValidatePushBranchScope ensures that any push event in the on: section specifies a branch or tag ref filter. |
| `compiler_mutators.go` | `(*Compiler).AddSafeUpdateWarning` | `func (*Compiler).AddSafeUpdateWarning(warning string)` | AddSafeUpdateWarning appends a safe update warning to the compiler's accumulated list. |
| `compiler_mutators.go` | `(*Compiler).EffectiveActionsRepo` | `func (*Compiler).EffectiveActionsRepo() string` | EffectiveActionsRepo returns the actions repository used for action mode references. |
| `compiler_mutators.go` | `(*Compiler).GetActionMode` | `func (*Compiler).GetActionMode() ActionMode` | GetActionMode returns the current action mode |
| `compiler_mutators.go` | `(*Compiler).GetActionTag` | `func (*Compiler).GetActionTag() string` | GetActionTag returns the action tag override (empty if not set) |
| `compiler_mutators.go` | `(*Compiler).GetRepositorySlug` | `func (*Compiler).GetRepositorySlug() string` | GetRepositorySlug returns the repository slug (owner/repo) set on this compiler instance. |
| `compiler_mutators.go` | `(*Compiler).GetSafeUpdateWarnings` | `func (*Compiler).GetSafeUpdateWarnings() []string` | GetSafeUpdateWarnings returns all accumulated safe update warnings for this compiler instance. |
| `compiler_mutators.go` | `(*Compiler).GetScheduleWarnings` | `func (*Compiler).GetScheduleWarnings() []string` | GetScheduleWarnings returns all accumulated schedule warnings for this compiler instance |
| `compiler_mutators.go` | `(*Compiler).GetSharedActionCache` | `func (*Compiler).GetSharedActionCache() *ActionCache` | GetSharedActionCache returns the shared action cache used by this compiler instance. |
| `compiler_mutators.go` | `(*Compiler).GetSharedActionResolver` | `func (*Compiler).GetSharedActionResolver() *ActionResolver` | GetSharedActionResolver returns the shared action resolver used by this compiler instance. |
| `compiler_mutators.go` | `(*Compiler).GetWarningCount` | `func (*Compiler).GetWarningCount() int` | GetWarningCount returns the current warning count |
| `compiler_mutators.go` | `(*Compiler).IncrementWarningCount` | `func (*Compiler).IncrementWarningCount()` | IncrementWarningCount increments the warning counter |
| `compiler_mutators.go` | `(*Compiler).IsRepositorySlugLocked` | `func (*Compiler).IsRepositorySlugLocked() bool` | IsRepositorySlugLocked reports whether the repository slug has been locked via LockRepositorySlug and must not be overridden by per-file detection. |
| `compiler_mutators.go` | `(*Compiler).LockRepositorySlug` | `func (*Compiler).LockRepositorySlug()` | LockRepositorySlug marks the repository slug as explicitly set (e. |
| `compiler_mutators.go` | `(*Compiler).ResetWarningCount` | `func (*Compiler).ResetWarningCount()` | ResetWarningCount resets the warning counter to zero |
| `compiler_mutators.go` | `(*Compiler).SetActionMode` | `func (*Compiler).SetActionMode(mode ActionMode)` | SetActionMode configures the action mode for JavaScript step generation |
| `compiler_mutators.go` | `(*Compiler).SetActionTag` | `func (*Compiler).SetActionTag(tag string)` | SetActionTag sets the action tag override for actions/setup |
| `compiler_mutators.go` | `(*Compiler).SetActionsRepo` | `func (*Compiler).SetActionsRepo(repo string)` | SetActionsRepo sets the external actions repository override. |
| `compiler_mutators.go` | `(*Compiler).SetAllowActionRefs` | `func (*Compiler).SetAllowActionRefs(allow bool)` | SetAllowActionRefs configures whether unresolved action refs are warnings. |
| `compiler_mutators.go` | `(*Compiler).SetApprove` | `func (*Compiler).SetApprove(approve bool)` | SetApprove configures whether to skip safe update enforcement via the CLI --approve flag. |
| `compiler_mutators.go` | `(*Compiler).SetContext` | `func (*Compiler).SetContext(ctx context.Context)` | SetContext sets the context used for network operations such as SHA resolution. |
| `compiler_mutators.go` | `(*Compiler).SetFileTracker` | `func (*Compiler).SetFileTracker(tracker FileCreationTracker)` | SetFileTracker sets the file tracker for tracking created files |
| `compiler_mutators.go` | `(*Compiler).SetForceRefreshActionPins` | `func (*Compiler).SetForceRefreshActionPins(force bool)` | SetForceRefreshActionPins configures whether to force refresh of action pins |
| `compiler_mutators.go` | `(*Compiler).SetForceStaged` | `func (*Compiler).SetForceStaged(force bool)` | SetForceStaged configures whether safe-outputs should always compile in staged mode. |
| `compiler_mutators.go` | `(*Compiler).SetGHESCompat` | `func (*Compiler).SetGHESCompat(enabled bool)` | SetGHESCompat enables GHES compatibility mode via the --ghes CLI flag. |
| `compiler_mutators.go` | `(*Compiler).SetModelPricingResolver` | `func (*Compiler).SetModelPricingResolver(fn func(ctx context.Context, provider, model string) (map[string]float64, bool))` | SetModelPricingResolver registers a callback used to resolve pricing for models that are not present in the embedded models. |
| `compiler_mutators.go` | `(*Compiler).SetNoEmit` | `func (*Compiler).SetNoEmit(noEmit bool)` | SetNoEmit configures whether to validate without generating lock files |
| `compiler_mutators.go` | `(*Compiler).SetPriorManifests` | `func (*Compiler).SetPriorManifests(manifests map[string]*GHAWManifest)` | SetPriorManifests replaces the entire pre-cached manifest map. |
| `compiler_mutators.go` | `(*Compiler).SetQuiet` | `func (*Compiler).SetQuiet(quiet bool)` | SetQuiet configures whether to suppress success messages (for interactive mode) |
| `compiler_mutators.go` | `(*Compiler).SetRefreshStopTime` | `func (*Compiler).SetRefreshStopTime(refresh bool)` | SetRefreshStopTime configures whether to force regeneration of stop-after times |
| `compiler_mutators.go` | `(*Compiler).SetRepositorySlug` | `func (*Compiler).SetRepositorySlug(slug string)` | SetRepositorySlug sets the repository slug for schedule scattering |
| `compiler_mutators.go` | `(*Compiler).SetRepositorySlugIfUnlocked` | `func (*Compiler).SetRepositorySlugIfUnlocked(slug string)` | SetRepositorySlugIfUnlocked sets the repository slug only when it has not been locked via LockRepositorySlug. |
| `compiler_mutators.go` | `(*Compiler).SetRequireDocker` | `func (*Compiler).SetRequireDocker(require bool)` | SetRequireDocker configures whether Docker must be available for container image validation. |
| `compiler_mutators.go` | `(*Compiler).SetSkipValidation` | `func (*Compiler).SetSkipValidation(skip bool)` | SetSkipValidation configures whether to skip schema validation |
| `compiler_mutators.go` | `(*Compiler).SetStrictMode` | `func (*Compiler).SetStrictMode(strict bool)` | SetStrictMode configures whether to enable strict validation mode |
| `compiler_mutators.go` | `(*Compiler).SetTrialLogicalRepoSlug` | `func (*Compiler).SetTrialLogicalRepoSlug(repo string)` | SetTrialLogicalRepoSlug configures the target repository for trial mode |
| `compiler_mutators.go` | `(*Compiler).SetTrialMode` | `func (*Compiler).SetTrialMode(trialMode bool)` | SetTrialMode configures whether to run in trial mode (suppresses safe outputs) |
| `compiler_mutators.go` | `(*Compiler).SetUseSamples` | `func (*Compiler).SetUseSamples(use bool)` | SetUseSamples configures whether to replace the agentic step with a deterministic replay driver that feeds `samples` entries to the safe-outputs MCP server via real `tools/call` JSON-RPC. |
| `compiler_mutators.go` | `(*Compiler).SetWorkflowIdentifier` | `func (*Compiler).SetWorkflowIdentifier(identifier string)` | SetWorkflowIdentifier sets the identifier for the current workflow being compiled This is used for deterministic schedule scattering |
| `compiler_orchestrator_workflow.go` | `(*Compiler).ParseWorkflowFile` | `func (*Compiler).ParseWorkflowFile(markdownPath string) (*WorkflowData, error)` | ParseWorkflowFile parses a workflow markdown file and returns a WorkflowData structure. |
| `compiler_string_api.go` | `(*Compiler).CompileToYAML` | `func (*Compiler).CompileToYAML(workflowData *WorkflowData, markdownPath string) (string, error)` | CompileToYAML compiles workflow data and returns the YAML as a string without writing to disk. |
| `compiler_string_api.go` | `(*Compiler).ParseWorkflowString` | `func (*Compiler).ParseWorkflowString(content string, virtualPath string) (*WorkflowData, error)` | ParseWorkflowString parses workflow markdown content from a string rather than a file. |
| `compiler_workflow_helpers.go` | `ContainsCheckout` | `func ContainsCheckout(customSteps string) bool` | ContainsCheckout returns true if the given custom steps contain an actions/checkout step |
| `config_helpers.go` | `ParseBoolFromConfig` | `func ParseBoolFromConfig(m map[string]any, key string, debugLog *logger.Logger) bool` | ParseBoolFromConfig is a generic helper that extracts and validates a boolean value from a map. |
| `config_helpers.go` | `ParseStringArrayFromConfig` | `func ParseStringArrayFromConfig(m map[string]any, key string, debugLog *logger.Logger) []string` | ParseStringArrayFromConfig is a generic helper that extracts and validates a string array from a map Returns a slice of strings, or nil if not present or invalid If log is provided, it will log the extracted values for … |
| `config_helpers.go` | `ParseStringArrayOrExprFromConfig` | `func ParseStringArrayOrExprFromConfig(m map[string]any, key string, debugLog *logger.Logger) []string` | ParseStringArrayOrExprFromConfig is like ParseStringArrayFromConfig but also accepts a GitHub Actions expression string as a valid value. |
| `copilot_engine.go` | `(*CopilotEngine).GetAPMTarget` | `func (*CopilotEngine).GetAPMTarget() string` | GetAPMTarget returns "copilot" so that apm-action packs Copilot-specific primitives. |
| `copilot_engine.go` | `(*CopilotEngine).GetAgentManifestFiles` | `func (*CopilotEngine).GetAgentManifestFiles() []string` | GetAgentManifestFiles returns instruction files that should be treated as security-sensitive manifests to protect against injection attacks in fork PRs. |
| `copilot_engine.go` | `(*CopilotEngine).GetAgentManifestPathPrefixes` | `func (*CopilotEngine).GetAgentManifestPathPrefixes() []string` | GetAgentManifestPathPrefixes returns Copilot-specific config directory prefixes that must be protected from fork PR injection. |
| `copilot_engine.go` | `(*CopilotEngine).GetCleanupStep` | `func (*CopilotEngine).GetCleanupStep(workflowData *WorkflowData) GitHubActionStep` | GetCleanupStep returns the post-execution cleanup step (currently empty) |
| `copilot_engine.go` | `(*CopilotEngine).GetFirewallLogsCollectionStep` | `func (*CopilotEngine).GetFirewallLogsCollectionStep(workflowData *WorkflowData) []GitHubActionStep` | GetFirewallLogsCollectionStep returns steps for collecting firewall logs and copying session state files |
| `copilot_engine.go` | `(*CopilotEngine).GetHarnessScriptName` | `func (*CopilotEngine).GetHarnessScriptName() string` | GetHarnessScriptName returns the filename of the JavaScript harness script that wraps the Copilot CLI with retry logic for transient CAPIError 400 errors. |
| `copilot_engine.go` | `(*CopilotEngine).GetModelEnvVarName` | `func (*CopilotEngine).GetModelEnvVarName() string` | GetModelEnvVarName returns the native environment variable name that the Copilot CLI uses for model selection. |
| `copilot_engine.go` | `(*CopilotEngine).GetRequiredSecretNames` | `func (*CopilotEngine).GetRequiredSecretNames(workflowData *WorkflowData) []string` | GetRequiredSecretNames returns the list of secrets required by the Copilot engine. |
| `copilot_engine.go` | `(*CopilotEngine).GetSquidLogsSteps` | `func (*CopilotEngine).GetSquidLogsSteps(workflowData *WorkflowData) []GitHubActionStep` | GetSquidLogsSteps returns the steps for uploading and parsing Squid logs (after secret redaction) |
| `copilot_engine.go` | `(*CopilotEngine).GetSupportedEnvVarKeys` | `func (*CopilotEngine).GetSupportedEnvVarKeys() []string` | GetSupportedEnvVarKeys returns the engine. |
| `copilot_engine.go` | `(*CopilotEngine).ResolveLLMProvider` | `func (*CopilotEngine).ResolveLLMProvider(workflowData *WorkflowData) string` | ResolveLLMProvider returns the effective provider for Copilot inference. |
| `copilot_engine_installation.go` | `(*CopilotEngine).GetSecretValidationStep` | `func (*CopilotEngine).GetSecretValidationStep(workflowData *WorkflowData) GitHubActionStep` | GetSecretValidationStep returns the secret validation step for the Copilot engine. |
| `copilot_installer.go` | `GenerateCopilotInstallerSteps` | `func GenerateCopilotInstallerSteps(version, stepName string, rootless bool, compiledVersion string) []GitHubActionStep` | GenerateCopilotInstallerSteps creates GitHub Actions steps to install the Copilot CLI using the official installer. |
| `copilot_logs.go` | `(*CopilotEngine).GetErrorDetectionScriptId` | `func (*CopilotEngine).GetErrorDetectionScriptId() string` | GetErrorDetectionScriptId returns the JavaScript script name for detecting agent errors from the agent stdio log. |
| `copilot_logs.go` | `(*CopilotEngine).GetLogFileForParsing` | `func (*CopilotEngine).GetLogFileForParsing() string` | GetLogFileForParsing returns the log directory for Copilot CLI logs Copilot writes detailed debug logs to /tmp/gh-aw/sandbox/agent/logs/ |
| `copilot_logs.go` | `(*CopilotEngine).GetLogParserScriptId` | `func (*CopilotEngine).GetLogParserScriptId() string` | GetLogParserScriptId returns the JavaScript script name for parsing Copilot logs |
| `copilot_logs.go` | `(*CopilotEngine).ParseLogMetrics` | `func (*CopilotEngine).ParseLogMetrics(logContent string, verbose bool) LogMetrics` | ParseLogMetrics implements engine-specific log parsing for Copilot CLI. |
| `copilot_mcp.go` | `(*CopilotEngine).RenderMCPConfig` | `func (*CopilotEngine).RenderMCPConfig(yaml *strings.Builder, tools map[string]any, mcpTools []string, workflowData *WorkflowData) error` | RenderMCPConfig generates MCP server configuration for Copilot CLI |
| `dependabot.go` | `(*Compiler).GenerateDependabotManifests` | `func (*Compiler).GenerateDependabotManifests(workflowDataList []*WorkflowData, workflowDir string, forceOverwrite bool) error` | GenerateDependabotManifests generates manifest files and dependabot. |
| `dependabot.go` | `(*Compiler).ReconcileManagedDependabotIgnores` | `func (*Compiler).ReconcileManagedDependabotIgnores(path string) error` | ReconcileManagedDependabotIgnores updates existing github-actions entries in . |
| `dependabot.go` | `(*Compiler).ReconcileManagedDependabotIgnoresInRepo` | `func (*Compiler).ReconcileManagedDependabotIgnoresInRepo(gitRoot string) error` | ReconcileManagedDependabotIgnoresInRepo reconciles managed ignores in the Dependabot config located under a repository root. |
| `dependabot.go` | `DependabotConfigPath` | `func DependabotConfigPath(gitRoot string) string` | DependabotConfigPath resolves the repository-local Dependabot config path. |
| `domains.go` | `GetAPITargetDomains` | `func GetAPITargetDomains(apiTarget string) []string` | GetAPITargetDomains returns the set of domains to add to the allow-list when engine. |
| `domains.go` | `GetBlockedDomains` | `func GetBlockedDomains(network *NetworkPermissions) []string` | GetBlockedDomains returns the blocked domains from network permissions Returns empty slice if no network permissions configured or no domains blocked The returned list is sorted and deduplicated Supports ecosystem ident… |
| `engine.go` | `(*EngineConfig).GetMaxAICredits` | `func (*EngineConfig).GetMaxAICredits() int64` | GetMaxAICredits returns the configured engine AI credits budget, falling back to the default. |
| `engine.go` | `(*EngineConfig).GetMaxRuns` | `func (*EngineConfig).GetMaxRuns() int` | GetMaxRuns returns the configured AWF max-runs value, falling back to the default. |
| `engine.go` | `(*EngineConfig).GetMaxTurnCacheMisses` | `func (*EngineConfig).GetMaxTurnCacheMisses() int` | GetMaxTurnCacheMisses returns the configured AWF max-turn-cache-misses value, falling back to the enterprise override or built-in default. |
| `engine_api_targets.go` | `GetCopilotAllowlistTargets` | `func GetCopilotAllowlistTargets(workflowData *WorkflowData) []string` | GetCopilotAllowlistTargets returns the Copilot-specific hosts that must be present in the firewall allow-list for execution to succeed. |
| `engine_config_dir.go` | `GetEngineSkillDir` | `func GetEngineSkillDir(engineID string) string` | GetEngineSkillDir returns the relative directory (from repo root / tmp base) used to store inline skill files for a given engine. |
| `engine_config_dir.go` | `GetEngineSubAgentDir` | `func GetEngineSubAgentDir(engineID string) string` | GetEngineSubAgentDir returns the relative directory (from repo root / tmp base) used to store inline sub-agent files for a given engine. |
| `engine_definition.go` | `(*AuthDefinition).RequiredSecretNames` | `func (*AuthDefinition).RequiredSecretNames() []string` | RequiredSecretNames returns the env-var names that must be provided at runtime for this AuthDefinition. |
| `engine_definition.go` | `(*EngineCatalog).Register` | `func (*EngineCatalog).Register(def *EngineDefinition)` | Register adds or replaces an EngineDefinition in the catalog. |
| `engine_definition.go` | `(EngineCapabilitiesDefinition).ToRuntimeCapabilities` | `func (EngineCapabilitiesDefinition).ToRuntimeCapabilities() EngineCapabilities` | ToRuntimeCapabilities converts the declarative capabilities definition into the runtime EngineCapabilities struct used by CodingAgentEngine implementations. |
| `engine_helpers.go` | `BuildDefaultSecretValidationStep` | `func BuildDefaultSecretValidationStep(workflowData *WorkflowData, secrets []string, name, docsURL string) GitHubActionStep` | BuildDefaultSecretValidationStep returns a secret validation step for the given engine configuration, or an empty step when a custom command is specified. |
| `engine_helpers.go` | `FilterEnvForSecrets` | `func FilterEnvForSecrets(env map[string]string, allowedNamesAndKeys []string) map[string]string` | FilterEnvForSecrets filters environment variables to only include allowed secrets. |
| `engine_helpers.go` | `FormatStepWithCommandAndEnv` | `func FormatStepWithCommandAndEnv(stepLines []string, command string, env map[string]string) []string` | RenderCustomMCPToolConfigHandler is a function type that engines must provide to render their specific MCP config FormatStepWithCommandAndEnv formats a GitHub Actions step with command and environment variables. |
| `engine_helpers.go` | `GenerateMultiSecretValidationStep` | `func GenerateMultiSecretValidationStep(secretNames []string, engineName, docsURL string, envOverrides map[string]string) GitHubActionStep` | GenerateMultiSecretValidationStep creates a GitHub Actions step that validates at least one of multiple secrets is available. |
| `engine_helpers.go` | `ResolveEngineID` | `func ResolveEngineID(workflowData *WorkflowData) string` | ResolveEngineID returns the workflow engine ID, preferring engine. |
| `engine_validation.go` | `EngineHasValidateSecretStep` | `func EngineHasValidateSecretStep(engine CodingAgentEngine, data *WorkflowData) bool` | EngineHasValidateSecretStep checks if the engine provides a validate-secret step. |
| `error_recovery.go` | `(ErrorSeverity).Heading` | `func (ErrorSeverity).Heading() string` | Heading returns a human-friendly severity heading for terminal output. |
| `error_recovery.go` | `(ErrorSeverity).Icon` | `func (ErrorSeverity).Icon() string` | Icon returns a terminal-friendly severity icon. |
| `error_recovery.go` | `BuildPrioritizedErrorReportFromMessages` | `func BuildPrioritizedErrorReportFromMessages(messages []string, showAll bool) PrioritizedErrorReport` | BuildPrioritizedErrorReportFromMessages classifies, suppresses, and limits messages. |
| `error_recovery.go` | `ExpandErrorMessages` | `func ExpandErrorMessages(err error) []string` | ExpandErrorMessages unwraps joined compiler errors into individual display messages. |
| `evals_config.go` | `(*EvalsConfig).HasEvals` | `func (*EvalsConfig).HasEvals() bool` | HasEvals returns true when the config contains at least one evaluation question. |
| `event_validation.go` | `ValidateEventTypes` | `func ValidateEventTypes(frontmatter map[string]any) error` | ValidateEventTypes validates that the event types in the 'on:' section of a workflow are recognized GitHub Actions events. |
| `expression_builder.go` | `BuildAnd` | `func BuildAnd(left ConditionNode, right ConditionNode) ConditionNode` | BuildAnd creates an AND node combining two conditions |
| `expression_builder.go` | `BuildBooleanLiteral` | `func BuildBooleanLiteral(value bool) *BooleanLiteralNode` | BuildBooleanLiteral creates a boolean literal node |
| `expression_builder.go` | `BuildComparison` | `func BuildComparison(left ConditionNode, operator string, right ConditionNode) *ComparisonNode` | BuildComparison creates a comparison node with the specified operator |
| `expression_builder.go` | `BuildConditionTree` | `func BuildConditionTree(existingCondition string, draftCondition string) ConditionNode` | BuildConditionTree creates a condition tree from existing if condition and new draft condition |
| `expression_builder.go` | `BuildDisjunction` | `func BuildDisjunction(multiline bool, terms ...ConditionNode) *DisjunctionNode` | BuildDisjunction creates a disjunction node (OR operation) from the given terms Handles arrays of size 0, 1, or more correctly The multiline parameter controls whether to render each term on a separate line |
| `expression_builder.go` | `BuildEquals` | `func BuildEquals(left ConditionNode, right ConditionNode) *ComparisonNode` | BuildEquals creates an equality comparison |
| `expression_builder.go` | `BuildEventTypeEquals` | `func BuildEventTypeEquals(eventType string) *ComparisonNode` | BuildEventTypeEquals creates a condition to check if the event type equals a specific value |
| `expression_builder.go` | `BuildFromAllowedForks` | `func BuildFromAllowedForks(allowedForks []string) ConditionNode` | BuildFromAllowedForks creates a condition to check if a pull request is from an allowed fork Supports glob patterns like "org/*" and exact matches like "org/repo" |
| `expression_builder.go` | `BuildFunctionCall` | `func BuildFunctionCall(functionName string, args ...ConditionNode) *FunctionCallNode` | BuildFunctionCall creates a function call node |
| `expression_builder.go` | `BuildNotEquals` | `func BuildNotEquals(left ConditionNode, right ConditionNode) *ComparisonNode` | BuildNotEquals creates an inequality comparison |
| `expression_builder.go` | `BuildNotFromFork` | `func BuildNotFromFork() *ComparisonNode` | BuildNotFromFork creates a condition to check that a pull request is not from a forked repository This prevents the job from running on forked PRs where write permissions are not available Uses repository ID comparison … |
| `expression_builder.go` | `BuildNullLiteral` | `func BuildNullLiteral() *ExpressionNode` | BuildNullLiteral creates a null literal node |
| `expression_builder.go` | `BuildOr` | `func BuildOr(left ConditionNode, right ConditionNode) ConditionNode` | BuildOr creates an OR node combining two conditions |
| `expression_builder.go` | `BuildPropertyAccess` | `func BuildPropertyAccess(path string) *PropertyAccessNode` | BuildPropertyAccess creates a property access node for GitHub context properties |
| `expression_builder.go` | `BuildReactionConditionForTargets` | `func BuildReactionConditionForTargets(includeIssues bool, includePullRequests bool, includeDiscussions bool, includeWorkflowDispatch bool) ConditionNode` | BuildReactionConditionForTargets creates a condition tree for reactions scoped to target groups. |
| `expression_builder.go` | `BuildSafeOutputType` | `func BuildSafeOutputType(outputType string) ConditionNode` | Exported function or method declared in `expression_builder.go`. |
| `expression_builder.go` | `BuildStatusCommentCondition` | `func BuildStatusCommentCondition(includeIssues bool, includePullRequests bool, includeDiscussions bool, includeWorkflowDispatch bool) ConditionNode` | BuildStatusCommentCondition creates a condition tree for activation status comments. |
| `expression_builder.go` | `BuildStringLiteral` | `func BuildStringLiteral(value string) *StringLiteralNode` | BuildStringLiteral creates a string literal node |
| `expression_builder.go` | `RenderCondition` | `func RenderCondition(node ConditionNode) string` | RenderCondition optimises a ConditionNode and renders it to a string. |
| `expression_builder.go` | `RenderConditionAsIf` | `func RenderConditionAsIf(yaml *strings.Builder, condition ConditionNode, indent string)` | RenderConditionAsIf renders a ConditionNode as an 'if' condition with proper YAML indentation. |
| `expression_extraction.go` | `(*ExpressionExtractor).ExtractExpressions` | `func (*ExpressionExtractor).ExtractExpressions(markdown string) ([]*ExpressionMapping, error)` | ExtractExpressions extracts all ${{ . |
| `expression_extraction.go` | `(*ExpressionExtractor).ReplaceExpressionsWithEnvVars` | `func (*ExpressionExtractor).ReplaceExpressionsWithEnvVars(markdown string) string` | ReplaceExpressionsWithEnvVars replaces all ${{ . |
| `expression_extraction.go` | `ExperimentEnvVarName` | `func ExperimentEnvVarName(experimentName string) string` | ExperimentEnvVarName returns the env-var name used for the given experiment. |
| `expression_extraction.go` | `NewExpressionExtractor` | `func NewExpressionExtractor() *ExpressionExtractor` | NewExpressionExtractor creates a new ExpressionExtractor |
| `expression_extraction.go` | `SubstituteImportInputs` | `func SubstituteImportInputs(content string, importInputs map[string]any) string` | SubstituteImportInputs replaces ${{ github. |
| `expression_nodes.go` | `(*AndNode).Render` | `func (*AndNode).Render() string` | Exported function or method declared in `expression_nodes.go`. |
| `expression_nodes.go` | `(*BooleanLiteralNode).Render` | `func (*BooleanLiteralNode).Render() string` | Exported function or method declared in `expression_nodes.go`. |
| `expression_nodes.go` | `(*ComparisonNode).Render` | `func (*ComparisonNode).Render() string` | Exported function or method declared in `expression_nodes.go`. |
| `expression_nodes.go` | `(*DisjunctionNode).Render` | `func (*DisjunctionNode).Render() string` | Exported function or method declared in `expression_nodes.go`. |
| `expression_nodes.go` | `(*DisjunctionNode).RenderMultiline` | `func (*DisjunctionNode).RenderMultiline() string` | RenderMultiline renders the disjunction with each term on a separate line, including comments for expressions that have descriptions |
| `expression_nodes.go` | `(*ExpressionNode).Render` | `func (*ExpressionNode).Render() string` | Exported function or method declared in `expression_nodes.go`. |
| `expression_nodes.go` | `(*FunctionCallNode).Render` | `func (*FunctionCallNode).Render() string` | Exported function or method declared in `expression_nodes.go`. |
| `expression_nodes.go` | `(*NotNode).Render` | `func (*NotNode).Render() string` | Exported function or method declared in `expression_nodes.go`. |
| `expression_nodes.go` | `(*OrNode).Render` | `func (*OrNode).Render() string` | \|\| has the lowest precedence of any boolean operator, so no child of an OR expression ever needs explicit parentheses to preserve evaluation order. |
| `expression_nodes.go` | `(*PropertyAccessNode).Render` | `func (*PropertyAccessNode).Render() string` | Exported function or method declared in `expression_nodes.go`. |
| `expression_nodes.go` | `(*StringLiteralNode).Render` | `func (*StringLiteralNode).Render() string` | Exported function or method declared in `expression_nodes.go`. |
| `expression_optimizer.go` | `OptimizeExpression` | `func OptimizeExpression(node ConditionNode) ConditionNode` | OptimizeExpression applies boolean algebra simplifications to a ConditionNode tree, returning an equivalent but potentially simpler and shorter expression. |
| `expression_parser.go` | `BreakAtParentheses` | `func BreakAtParentheses(expression string) []string` | BreakAtParentheses attempts to break long lines at parentheses for function calls |
| `expression_parser.go` | `BreakLongExpression` | `func BreakLongExpression(expression string) []string` | BreakLongExpression breaks a long expression into multiple lines at logical points such as after \|\| and && operators for better readability |
| `expression_parser.go` | `ParseExpression` | `func ParseExpression(expression string) (ConditionNode, error)` | ParseExpression parses a string expression into a ConditionNode tree Supports && (AND), \|\| (OR), ! |
| `expression_parser.go` | `VisitExpressionTree` | `func VisitExpressionTree(node ConditionNode, visitor func(expr *ExpressionNode) error) error` | VisitExpressionTree walks through an expression tree and calls the visitor function for each ExpressionNode (literal expression) found in the tree |
| `firewall_validation.go` | `ValidateLogLevel` | `func ValidateLogLevel(level string) error` | ValidateLogLevel validates that a firewall log-level value is one of the allowed enum values. |
| `frontmatter_parsing.go` | `ParseFrontmatterConfig` | `func ParseFrontmatterConfig(frontmatter map[string]any) (*FrontmatterConfig, error)` | ParseFrontmatterConfig creates a FrontmatterConfig from a raw frontmatter map This provides a single entry point for converting untyped frontmatter into a structured configuration with better error handling. |
| `frontmatter_serialization.go` | `(*FrontmatterConfig).ToMap` | `func (*FrontmatterConfig).ToMap() map[string]any` | ToMap converts FrontmatterConfig back to map[string]any for backward compatibility This allows gradual migration from map[string]any to strongly-typed config |
| `frontmatter_serialization.go` | `ExtractMapField` | `func ExtractMapField(frontmatter map[string]any, key string) map[string]any` | ExtractMapField is a convenience wrapper for extracting map[string]any fields from frontmatter. |
| `gemini_engine.go` | `(*GeminiEngine).GetAgentManifestFiles` | `func (*GeminiEngine).GetAgentManifestFiles() []string` | GetAgentManifestFiles returns Gemini-specific instruction files that should be treated as security-sensitive manifests. |
| `gemini_engine.go` | `(*GeminiEngine).GetAgentManifestPathPrefixes` | `func (*GeminiEngine).GetAgentManifestPathPrefixes() []string` | GetAgentManifestPathPrefixes returns Gemini-specific config directory prefixes. |
| `gemini_engine.go` | `(*GeminiEngine).GetModelEnvVarName` | `func (*GeminiEngine).GetModelEnvVarName() string` | GetModelEnvVarName returns the native environment variable name that the Gemini CLI uses for model selection. |
| `gemini_engine.go` | `(*GeminiEngine).GetPreBundleSteps` | `func (*GeminiEngine).GetPreBundleSteps(workflowData *WorkflowData) []GitHubActionStep` | GetPreBundleSteps returns a step that moves Gemini CLI error reports from /tmp/ into /tmp/gh-aw/ before the unified artifact upload. |
| `gemini_engine.go` | `(*GeminiEngine).GetRequiredSecretNames` | `func (*GeminiEngine).GetRequiredSecretNames(workflowData *WorkflowData) []string` | GetRequiredSecretNames returns the list of secrets required by the Gemini engine This includes GEMINI_API_KEY and optionally MCP_GATEWAY_AGENT_ID, GITHUB_MCP_SERVER_TOKEN, HTTP MCP header secrets, and mcp-scripts secrets |
| `gemini_engine.go` | `(*GeminiEngine).GetSecretValidationStep` | `func (*GeminiEngine).GetSecretValidationStep(workflowData *WorkflowData) GitHubActionStep` | GetSecretValidationStep returns the secret validation step for the Gemini engine. |
| `gemini_engine.go` | `(*GeminiEngine).GetSupportedEnvVarKeys` | `func (*GeminiEngine).GetSupportedEnvVarKeys() []string` | GetSupportedEnvVarKeys returns the engine. |
| `gemini_logs.go` | `(*GeminiEngine).GetLogParserScriptId` | `func (*GeminiEngine).GetLogParserScriptId() string` | GetLogParserScriptId returns the script ID for parsing Gemini logs |
| `gemini_logs.go` | `(*GeminiEngine).ParseLogMetrics` | `func (*GeminiEngine).ParseLogMetrics(logContent string, verbose bool) LogMetrics` | ParseLogMetrics parses Gemini CLI log output and extracts metrics. |
| `gemini_mcp.go` | `(*GeminiEngine).RenderMCPConfig` | `func (*GeminiEngine).RenderMCPConfig(yaml *strings.Builder, tools map[string]any, mcpTools []string, workflowData *WorkflowData) error` | RenderMCPConfig renders MCP server configuration for Gemini CLI |
| `git_helpers.go` | `RunGitCombined` | `func RunGitCombined(spinnerMessage string, args ...string) ([]byte, error)` | RunGitCombined executes a git command with an optional spinner, returning combined stdout+stderr. |
| `github_cli.go` | `ExecGH` | `func ExecGH(args ...string) *exec.Cmd` | ExecGH wraps gh CLI calls and ensures proper token configuration. |
| `github_cli.go` | `ExecGHContext` | `func ExecGHContext(ctx context.Context, args ...string) *exec.Cmd` | ExecGHContext wraps gh CLI calls with context support and ensures proper token configuration. |
| `github_cli.go` | `ForceGHHostEnv` | `func ForceGHHostEnv(cmd *exec.Cmd, host string)` | ForceGHHostEnv forces GH_HOST=<host> on the command's environment, overriding any GH_HOST already present in the process environment or cmd. |
| `github_cli.go` | `RunGH` | `func RunGH(spinnerMessage string, args ...string) ([]byte, error)` | RunGH executes a gh CLI command with a spinner and returns the stdout output. |
| `github_cli.go` | `RunGHCombined` | `func RunGHCombined(spinnerMessage string, args ...string) ([]byte, error)` | RunGHCombined executes a gh CLI command with a spinner and returns combined stdout+stderr output. |
| `github_cli.go` | `RunGHCombinedContext` | `func RunGHCombinedContext(ctx context.Context, spinnerMessage string, args ...string) ([]byte, error)` | RunGHCombinedContext executes a gh CLI command with context support (for cancellation/timeout), a spinner, and returns combined stdout+stderr output. |
| `github_cli.go` | `RunGHContext` | `func RunGHContext(ctx context.Context, spinnerMessage string, args ...string) ([]byte, error)` | RunGHContext executes a gh CLI command with context support (for cancellation/timeout), a spinner, and returns the stdout output. |
| `github_cli.go` | `RunGHContextWithHost` | `func RunGHContextWithHost(ctx context.Context, spinnerMessage string, host string, args ...string) ([]byte, error)` | RunGHContextWithHost executes a gh CLI command with context support, a spinner, and an explicit GitHub host. |
| `github_cli.go` | `RunGHInputContext` | `func RunGHInputContext(ctx context.Context, spinnerMessage string, input io.Reader, args ...string) ([]byte, error)` | RunGHInputContext executes a gh CLI command with context, a spinner, and an io. |
| `github_cli.go` | `RunGHWithHost` | `func RunGHWithHost(spinnerMessage string, host string, args ...string) ([]byte, error)` | RunGHWithHost executes a gh CLI command with a spinner, targeting a specific GitHub host. |
| `github_cli.go` | `SetDefaultGHHost` | `func SetDefaultGHHost(host string)` | SetDefaultGHHost sets the default host used by gh CLI helper commands when GH_HOST is not set in the process environment. |
| `github_cli.go` | `SetGHHostEnv` | `func SetGHHostEnv(cmd *exec.Cmd, host string)` | SetGHHostEnv sets the GH_HOST environment variable on the command for non-github. |
| `github_cli_wasm.go` | `ExecGHWithOutput` | `func ExecGHWithOutput(args ...string) (stdout, stderr bytes.Buffer, err error)` | Exported function or method declared in `github_cli_wasm.go`. |
| `github_toolset_validation_error.go` | `NewGitHubToolsetValidationError` | `func NewGitHubToolsetValidationError(missingToolsets map[string][]string) *GitHubToolsetValidationError` | NewGitHubToolsetValidationError creates a new validation error |
| `github_toolsets.go` | `ParseGitHubToolsets` | `func ParseGitHubToolsets(toolsetsStr string) []string` | ParseGitHubToolsets parses the toolsets string and expands "default" and "all" into their constituent toolsets. |
| `imports.go` | `(*Compiler).MergeFeatures` | `func (*Compiler).MergeFeatures(topFeatures map[string]any, importedFeatures []map[string]any) (map[string]any, error)` | MergeFeatures merges features configurations from imports with top-level features Features from top-level take precedence over imported features |
| `imports.go` | `(*Compiler).MergeMCPServers` | `func (*Compiler).MergeMCPServers(topMCPServers map[string]any, importedMCPServersJSON string) (map[string]any, error)` | MergeMCPServers merges mcp-servers from imports with top-level mcp-servers Takes object maps and merges them directly |
| `imports.go` | `(*Compiler).MergeNetworkPermissions` | `func (*Compiler).MergeNetworkPermissions(topNetwork *NetworkPermissions, importedNetworkJSON string) (*NetworkPermissions, error)` | MergeNetworkPermissions merges network permissions from imports with top-level network permissions Combines allowed domains from both sources into a single list |
| `imports.go` | `(*Compiler).MergeSafeOutputs` | `func (*Compiler).MergeSafeOutputs(topSafeOutputs *SafeOutputsConfig, importedSafeOutputsJSON []string, topRawSafeOutputs map[string]any) (*SafeOutputsConfig, error)` | MergeSafeOutputs merges safe-outputs configurations from imports into the top-level safe-outputs. |
| `imports.go` | `(*Compiler).MergeTools` | `func (*Compiler).MergeTools(topTools map[string]any, includedToolsJSON string) (map[string]any, error)` | MergeTools merges two tools maps, combining allowed arrays when keys coincide Handles newline-separated JSON objects from multiple imports/includes |
| `inputs.go` | `ParseInputDefinition` | `func ParseInputDefinition(inputConfig map[string]any) *InputDefinition` | ParseInputDefinition parses an input definition from a map. |
| `inputs.go` | `ParseInputDefinitions` | `func ParseInputDefinitions(inputsMap map[string]any) map[string]*InputDefinition` | ParseInputDefinitions parses a map of input definitions from a frontmatter map. |
| `jobs.go` | `(*JobManager).AddJob` | `func (*JobManager).AddJob(job *Job) error` | AddJob adds a job to the manager |
| `jobs.go` | `(*JobManager).GetAllJobs` | `func (*JobManager).GetAllJobs() map[string]*Job` | GetAllJobs returns all jobs in the manager |
| `jobs.go` | `(*JobManager).GetJob` | `func (*JobManager).GetJob(name string) (*Job, bool)` | GetJob retrieves a job by name |
| `jobs.go` | `(*JobManager).WriteJobsYAML` | `func (*JobManager).WriteJobsYAML(b *strings.Builder)` | WriteJobsYAML writes the jobs section of a GitHub Actions workflow directly to b, avoiding an intermediate string copy. |
| `jobs.go` | `NewJobManager` | `func NewJobManager() *JobManager` | NewJobManager creates a new JobManager instance |
| `jobs_validation.go` | `(*JobManager).ValidateDependencies` | `func (*JobManager).ValidateDependencies() error` | ValidateDependencies checks that all job dependencies exist and there are no cycles |
| `jobs_validation.go` | `(*JobManager).ValidateDuplicateSteps` | `func (*JobManager).ValidateDuplicateSteps() error` | ValidateDuplicateSteps checks that no job has duplicate steps. |
| `js.go` | `FormatJavaScriptForYAML` | `func FormatJavaScriptForYAML(script string) []string` | FormatJavaScriptForYAML formats a JavaScript script with proper indentation for embedding in YAML |
| `js.go` | `GetJavaScriptSources` | `func GetJavaScriptSources() map[string]string` | Exported function or method declared in `js.go`. |
| `js.go` | `GetLogParserBootstrap` | `func GetLogParserBootstrap() string` | Exported function or method declared in `js.go`. |
| `js.go` | `GetLogParserScript` | `func GetLogParserScript(name string) string` | Exported function or method declared in `js.go`. |
| `js.go` | `GetMCPHTTPTransportScript` | `func GetMCPHTTPTransportScript() string` | Exported function or method declared in `js.go`. |
| `js.go` | `GetMCPHandlerPythonScript` | `func GetMCPHandlerPythonScript() string` | Exported function or method declared in `js.go`. |
| `js.go` | `GetMCPHandlerShellScript` | `func GetMCPHandlerShellScript() string` | Exported function or method declared in `js.go`. |
| `js.go` | `GetMCPLoggerScript` | `func GetMCPLoggerScript() string` | Exported function or method declared in `js.go`. |
| `js.go` | `GetMCPScriptsConfigLoaderScript` | `func GetMCPScriptsConfigLoaderScript() string` | Exported function or method declared in `js.go`. |
| `js.go` | `GetMCPScriptsMCPServerHTTPScript` | `func GetMCPScriptsMCPServerHTTPScript() string` | Exported function or method declared in `js.go`. |
| `js.go` | `GetMCPScriptsValidationScript` | `func GetMCPScriptsValidationScript() string` | Exported function or method declared in `js.go`. |
| `js.go` | `GetMCPServerCoreScript` | `func GetMCPServerCoreScript() string` | Exported function or method declared in `js.go`. |
| `js.go` | `GetReadBufferScript` | `func GetReadBufferScript() string` | Exported function or method declared in `js.go`. |
| `js.go` | `WriteJavaScriptToYAML` | `func WriteJavaScriptToYAML(yaml *strings.Builder, script string)` | WriteJavaScriptToYAML writes a JavaScript script with proper indentation to a strings. |
| `known_action_credentials.go` | `DetectKnownCredentialLeakingActions` | `func DetectKnownCredentialLeakingActions(steps []any) map[string]struct { }` | DetectKnownCredentialLeakingActions scans a list of workflow steps and returns a map of environment variable names to true for each known credential-leaking action found. |
| `known_action_credentials.go` | `DetectKnownCredentialLeakingActionsFromWorkflowData` | `func DetectKnownCredentialLeakingActionsFromWorkflowData(workflowData *WorkflowData) map[string]struct { }` | DetectKnownCredentialLeakingActionsFromWorkflowData scans all step collections in workflowData (custom steps, pre-steps, pre-agent-steps) and returns the merged set of environment variables required for the known-action… |
| `label_command.go` | `FilterLabelCommandEvents` | `func FilterLabelCommandEvents(identifiers []string) []string` | FilterLabelCommandEvents returns the label-command events to use based on the specified identifiers. |
| `lock_schema.go` | `(*LockMetadata).ToJSON` | `func (*LockMetadata).ToJSON() (string, error)` | ToJSON converts LockMetadata to a compact JSON string for embedding in comments |
| `lock_schema.go` | `ExtractMetadataFromLockFile` | `func ExtractMetadataFromLockFile(content string) (*LockMetadata, bool, error)` | ExtractMetadataFromLockFile extracts structured metadata from a lock file's comment header Returns metadata and whether legacy format (no metadata) was detected |
| `lock_schema.go` | `GenerateLockMetadata` | `func GenerateLockMetadata(hashInfo LockHashInfo, stopTime string, strict bool, agentInfo AgentMetadataInfo) *LockMetadata` | GenerateLockMetadata creates a LockMetadata struct for embedding in lock files For release builds, the compiler version is included in the metadata |
| `lock_schema.go` | `IsSchemaVersionSupported` | `func IsSchemaVersionSupported(version LockSchemaVersion) bool` | IsSchemaVersionSupported checks if a schema version is supported |
| `lock_validation.go` | `ValidateLockSchemaCompatibility` | `func ValidateLockSchemaCompatibility(content string, lockFilePath string) error` | ValidateLockSchemaCompatibility validates that a lock file's schema is compatible. |
| `lsp_manager.go` | `(*LSPManager).CopilotLSPServers` | `func (*LSPManager).CopilotLSPServers() map[string]LSPServerConfig` | Exported function or method declared in `lsp_manager.go`. |
| `lsp_manager.go` | `(*LSPManager).GenerateInstallSteps` | `func (*LSPManager).GenerateInstallSteps(workflowData *WorkflowData) []GitHubActionStep` | GenerateInstallSteps generates GitHub Actions steps that install the LSP server binary dependencies for all configured LSP servers. |
| `lsp_manager.go` | `(*LSPManager).HasServers` | `func (*LSPManager).HasServers() bool` | Exported function or method declared in `lsp_manager.go`. |
| `lsp_manager.go` | `(*LSPManager).RuntimeRequirements` | `func (*LSPManager).RuntimeRequirements() []RuntimeRequirement` | RuntimeRequirements returns the set of runtime requirements for all configured LSP servers. |
| `lsp_manager.go` | `NewLSPManager` | `func NewLSPManager(servers map[string]LSPServerConfig) *LSPManager` | Exported function or method declared in `lsp_manager.go`. |
| `maintenance_workflow.go` | `FetchDefaultBranch` | `func FetchDefaultBranch(slug string) string` | FetchDefaultBranch queries the GitHub API to determine the default branch of the given repository slug (owner/repo). |
| `maintenance_workflow.go` | `GenerateMaintenanceWorkflow` | `func GenerateMaintenanceWorkflow(ctx context.Context, opts GenerateMaintenanceWorkflowOptions) error` | GenerateMaintenanceWorkflow generates the agentics-maintenance. |
| `markdown_security_scanner.go` | `FormatSecurityFindings` | `func FormatSecurityFindings(findings []SecurityFinding, filePath string) string` | FormatSecurityFindings formats a list of findings into a human-readable error message filePath: the workflow file path to include in error messages |
| `mcp_cli_mount.go` | `GetMCPCLIPathSetup` | `func GetMCPCLIPathSetup(data *WorkflowData) string` | GetMCPCLIPathSetup returns a shell command that adds the MCP CLI bin directory to PATH inside the AWF container. |
| `mcp_config_types.go` | `(MapToolConfig).GetAny` | `func (MapToolConfig).GetAny(key string) (any, bool)` | Exported function or method declared in `mcp_config_types.go`. |
| `mcp_config_types.go` | `(MapToolConfig).GetString` | `func (MapToolConfig).GetString(key string) (string, bool)` | Exported function or method declared in `mcp_config_types.go`. |
| `mcp_config_types.go` | `(MapToolConfig).GetStringArray` | `func (MapToolConfig).GetStringArray(key string) ([]string, bool)` | Exported function or method declared in `mcp_config_types.go`. |
| `mcp_config_types.go` | `(MapToolConfig).GetStringMap` | `func (MapToolConfig).GetStringMap(key string) (map[string]string, bool)` | Exported function or method declared in `mcp_config_types.go`. |
| `mcp_config_validation.go` | `ValidateMCPConfigs` | `func ValidateMCPConfigs(tools map[string]any) error` | ValidateMCPConfigs validates all MCP configurations in the tools section using JSON schema. |
| `mcp_config_validation.go` | `ValidateToolsSection` | `func ValidateToolsSection(tools map[string]any) error` | ValidateToolsSection validates that all entries in the user-facing tools: frontmatter section are recognized built-in tool names. |
| `mcp_detection.go` | `HasMCPServers` | `func HasMCPServers(workflowData *WorkflowData) bool` | HasMCPServers checks if the workflow has any MCP servers configured |
| `mcp_renderer.go` | `HandleCustomMCPToolInSwitch` | `func HandleCustomMCPToolInSwitch( yaml *strings.Builder, toolName string, tools map[string]any, isLast bool, renderFunc RenderCustomMCPToolConfigHandler, ) bool` | HandleCustomMCPToolInSwitch processes custom MCP tools in the default case of a switch statement. |
| `mcp_renderer.go` | `NewMCPConfigRenderer` | `func NewMCPConfigRenderer(opts MCPRendererOptions) *MCPConfigRendererUnified` | NewMCPConfigRenderer creates a new unified MCP config renderer with the specified options |
| `mcp_renderer.go` | `RenderJSONMCPConfig` | `func RenderJSONMCPConfig( yaml *strings.Builder, tools map[string]any, mcpTools []string, workflowData *WorkflowData, options JSONMCPConfigOptions, ) error` | RenderJSONMCPConfig renders MCP configuration in JSON format with the common mcpServers structure. |
| `mcp_renderer_builtin.go` | `(*MCPConfigRendererUnified).RenderAgenticWorkflowsMCP` | `func (*MCPConfigRendererUnified).RenderAgenticWorkflowsMCP(yaml *strings.Builder)` | RenderAgenticWorkflowsMCP generates the Agentic Workflows MCP server configuration |
| `mcp_renderer_builtin.go` | `(*MCPConfigRendererUnified).RenderMCPScriptsMCP` | `func (*MCPConfigRendererUnified).RenderMCPScriptsMCP(yaml *strings.Builder, mcpScripts *MCPScriptsConfig, workflowData *WorkflowData)` | RenderMCPScriptsMCP generates the MCP Scripts server configuration |
| `mcp_renderer_builtin.go` | `(*MCPConfigRendererUnified).RenderSafeOutputsMCP` | `func (*MCPConfigRendererUnified).RenderSafeOutputsMCP(yaml *strings.Builder, workflowData *WorkflowData)` | RenderSafeOutputsMCP generates the Safe Outputs MCP server configuration |
| `mcp_renderer_github.go` | `(*MCPConfigRendererUnified).RenderGitHubMCP` | `func (*MCPConfigRendererUnified).RenderGitHubMCP(yaml *strings.Builder, githubTool map[string]any, workflowData *WorkflowData)` | RenderGitHubMCP generates the GitHub MCP server configuration Supports both local (Docker) and remote (hosted) modes |
| `mcp_renderer_github.go` | `RenderGitHubMCPDockerConfig` | `func RenderGitHubMCPDockerConfig(yaml *strings.Builder, options GitHubMCPDockerOptions)` | RenderGitHubMCPDockerConfig renders the GitHub MCP server configuration for Docker (local mode). |
| `mcp_renderer_github.go` | `RenderGitHubMCPRemoteConfig` | `func RenderGitHubMCPRemoteConfig(yaml *strings.Builder, options GitHubMCPRemoteOptions)` | RenderGitHubMCPRemoteConfig renders the GitHub MCP server configuration for remote (hosted) mode. |
| `metrics.go` | `ExtractJSONCost` | `func ExtractJSONCost(data map[string]any) float64` | ExtractJSONCost extracts cost information from JSON data |
| `metrics.go` | `ExtractJSONMetrics` | `func ExtractJSONMetrics(line string, verbose bool) LogMetrics` | ExtractJSONMetrics extracts metrics from streaming JSON log lines |
| `metrics.go` | `ExtractJSONTokenUsage` | `func ExtractJSONTokenUsage(data map[string]any) int` | ExtractJSONTokenUsage extracts token usage from JSON data |
| `metrics.go` | `FinalizeToolCallsAndSequence` | `func FinalizeToolCallsAndSequence( metrics *LogMetrics, toolCallMap map[string]*ToolCallInfo, currentSequence []string, )` | FinalizeToolCallsAndSequence completes the tool call and sequence finalization. |
| `metrics.go` | `FinalizeToolMetrics` | `func FinalizeToolMetrics(opts FinalizeToolMetricsOptions)` | FinalizeToolMetrics completes the metric collection process by finalizing sequences, converting tool call maps to sorted slices, and optionally counting errors using patterns. |
| `model_alias_validation.go` | `ValidateEffortParam` | `func ValidateEffortParam(value string) error` | ValidateEffortParam validates the "effort" parameter value (V-MAF-002). |
| `model_alias_validation.go` | `ValidateKnownParams` | `func ValidateKnownParams(params map[string]string) error` | ValidateKnownParams validates the known parameters in a parsed identifier. |
| `model_alias_validation.go` | `ValidateTemperatureParam` | `func ValidateTemperatureParam(value string) error` | ValidateTemperatureParam validates the "temperature" parameter value (V-MAF-003). |
| `model_aliases.go` | `BuiltinModelAliases` | `func BuiltinModelAliases() map[string][]string` | BuiltinModelAliases returns the built-in model alias map that covers the main model families supported by gh-aw. |
| `model_aliases.go` | `MergeImportedModelAliases` | `func MergeImportedModelAliases(importedModels []map[string][]string, frontmatterModels map[string][]string) map[string][]string` | MergeImportedModelAliases builds the final model alias map from three layers, with later layers overriding earlier ones (highest priority last): 1. |
| `model_identifier.go` | `ParseModelIdentifier` | `func ParseModelIdentifier(s string) (*ParsedModelIdentifier, error)` | ParseModelIdentifier parses a model identifier string into its components. |
| `model_identifier.go` | `UnrecognizedParams` | `func UnrecognizedParams(params map[string]string) []string` | UnrecognizedParams returns the list of parameter keys in params that are not defined in Section 6 (i. |
| `nodejs.go` | `BuildNpmEngineInstallStepsWithAWF` | `func BuildNpmEngineInstallStepsWithAWF(npmSteps []GitHubActionStep, workflowData *WorkflowData) []GitHubActionStep` | BuildNpmEngineInstallStepsWithAWF injects an AWF installation step between the Node. |
| `nodejs.go` | `BuildStandardNpmEngineInstallSteps` | `func BuildStandardNpmEngineInstallSteps( packageName string, defaultVersion string, stepName string, cacheKeyPrefix string, workflowData *WorkflowData, ) []GitHubActionStep` | BuildStandardNpmEngineInstallSteps creates standard npm installation steps for engines. |
| `nodejs.go` | `BuildStandardNpmEngineInstallStepsNoCooldown` | `func BuildStandardNpmEngineInstallStepsNoCooldown( packageName string, defaultVersion string, stepName string, cacheKeyPrefix string, workflowData *WorkflowData, ) []GitHubActionStep` | BuildStandardNpmEngineInstallStepsNoCooldown creates standard npm installation steps for engines while forcing the default npm release-age cooldown off. |
| `nodejs.go` | `GenerateNodeJsSetupStep` | `func GenerateNodeJsSetupStep() GitHubActionStep` | GenerateNodeJsSetupStep creates a GitHub Actions step for setting up Node. |
| `nodejs.go` | `GenerateNpmInstallSteps` | `func GenerateNpmInstallSteps(packageName, version, stepName, cacheKeyPrefix string, includeNodeSetup bool, runInstallScripts bool, cooldownEnabled bool) []GitHubActionStep` | By default, --ignore-scripts is added to the install command to prevent pre/post install scripts from executing (supply chain security). |
| `nodejs.go` | `GenerateNpmInstallStepsWithScope` | `func GenerateNpmInstallStepsWithScope(packageName, version, stepName, cacheKeyPrefix string, includeNodeSetup bool, isGlobal bool, runInstallScripts bool, cooldownEnabled bool) []GitHubActionStep` | GenerateNpmInstallStepsWithScope generates npm installation steps with control over global vs local installation. |
| `nodejs.go` | `GetNpmBinPathSetup` | `func GetNpmBinPathSetup() string` | GetNpmBinPathSetup returns a simple shell command that adds hostedtoolcache bin directories to PATH. |
| `package_extraction.go` | `(*PackageExtractor).ExtractPackages` | `func (*PackageExtractor).ExtractPackages(commands string) []string` | ExtractPackages extracts package names from command strings using the configured extraction rules. |
| `package_extraction.go` | `(*PackageExtractor).FindPackageName` | `func (*PackageExtractor).FindPackageName(words []string, startIndex int) string` | findPackageName finds and processes the package name starting at the given index. |
| `permissions.go` | `GetAllGitHubAppOnlyScopes` | `func GetAllGitHubAppOnlyScopes() []PermissionScope` | GetAllGitHubAppOnlyScopes returns all GitHub App-only permission scopes. |
| `permissions.go` | `GetAllPermissionScopes` | `func GetAllPermissionScopes() []PermissionScope` | GetAllPermissionScopes returns all GitHub Actions permission scopes (supported by GITHUB_TOKEN). |
| `permissions.go` | `IsGitHubAppOnlyScope` | `func IsGitHubAppOnlyScope(scope PermissionScope) bool` | IsGitHubAppOnlyScope returns true if the scope is a GitHub App-only permission (not supported by GITHUB_TOKEN). |
| `permissions_factory.go` | `(*Permissions).Clone` | `func (*Permissions).Clone() *Permissions` | Clone returns a deep copy of the Permissions object. |
| `permissions_factory.go` | `NewPermissions` | `func NewPermissions() *Permissions` | NewPermissions creates a new Permissions with an empty map |
| `permissions_factory.go` | `NewPermissionsActionsWrite` | `func NewPermissionsActionsWrite() *Permissions` | NewPermissionsActionsWrite creates permissions with actions: write This is required for dispatching workflows via workflow_dispatch |
| `permissions_factory.go` | `NewPermissionsAllRead` | `func NewPermissionsAllRead() *Permissions` | NewPermissionsAllRead creates a Permissions with all: read |
| `permissions_factory.go` | `NewPermissionsContentsRead` | `func NewPermissionsContentsRead() *Permissions` | NewPermissionsContentsRead creates permissions with contents: read |
| `permissions_factory.go` | `NewPermissionsContentsReadIssuesWrite` | `func NewPermissionsContentsReadIssuesWrite() *Permissions` | NewPermissionsContentsReadIssuesWrite creates permissions with contents: read and issues: write |
| `permissions_factory.go` | `NewPermissionsContentsReadIssuesWritePRWrite` | `func NewPermissionsContentsReadIssuesWritePRWrite() *Permissions` | NewPermissionsContentsReadIssuesWritePRWrite creates permissions with contents: read, issues: write, pull-requests: write |
| `permissions_factory.go` | `NewPermissionsContentsReadSecurityEventsWriteActionsRead` | `func NewPermissionsContentsReadSecurityEventsWriteActionsRead() *Permissions` | NewPermissionsContentsReadSecurityEventsWriteActionsRead creates permissions with contents: read, security-events: write, actions: read |
| `permissions_factory.go` | `NewPermissionsContentsWrite` | `func NewPermissionsContentsWrite() *Permissions` | NewPermissionsContentsWrite creates permissions with contents: write |
| `permissions_factory.go` | `NewPermissionsEmpty` | `func NewPermissionsEmpty() *Permissions` | NewPermissionsEmpty creates a Permissions that explicitly renders as "permissions: {}" |
| `permissions_factory.go` | `NewPermissionsFromMap` | `func NewPermissionsFromMap(perms map[PermissionScope]PermissionLevel) *Permissions` | NewPermissionsFromMap creates a Permissions from a map of scopes to levels |
| `permissions_factory.go` | `NewPermissionsNone` | `func NewPermissionsNone() *Permissions` | NewPermissionsNone creates a Permissions with none shorthand |
| `permissions_factory.go` | `NewPermissionsReadAll` | `func NewPermissionsReadAll() *Permissions` | NewPermissionsReadAll creates a Permissions with read-all shorthand |
| `permissions_factory.go` | `NewPermissionsWriteAll` | `func NewPermissionsWriteAll() *Permissions` | NewPermissionsWriteAll creates a Permissions with write-all shorthand |
| `permissions_operations.go` | `(*Permissions).GetExplicit` | `func (*Permissions).GetExplicit(scope PermissionScope) (PermissionLevel, bool)` | GetExplicit returns the permission level only if the scope was explicitly declared in the permissions map. |
| `permissions_operations.go` | `(*Permissions).HasContentsReadAccess` | `func (*Permissions).HasContentsReadAccess() bool` | HasContentsReadAccess returns true if the permissions allow reading the repository contents. |
| `permissions_operations.go` | `(*Permissions).HasCopilotRequestsWrite` | `func (*Permissions).HasCopilotRequestsWrite() bool` | HasCopilotRequestsWrite returns true if the permissions grant copilot-requests: write. |
| `permissions_operations.go` | `(*Permissions).Merge` | `func (*Permissions).Merge(other *Permissions)` | Merge merges another Permissions into this one Write permission takes precedence over read (write implies read) Individual scope permissions override shorthand |
| `permissions_operations.go` | `(*Permissions).RenderToYAML` | `func (*Permissions).RenderToYAML() string` | RenderToYAML renders the Permissions to GitHub Actions YAML format |
| `permissions_operations.go` | `HasCopilotRequestsWriteFromFrontmatter` | `func HasCopilotRequestsWriteFromFrontmatter(frontmatter map[string]any) bool` | HasCopilotRequestsWriteFromFrontmatter returns true when the frontmatter permissions map includes copilot-requests: write. |
| `permissions_parser.go` | `(*PermissionsParser).HasContentsReadAccess` | `func (*PermissionsParser).HasContentsReadAccess() bool` | HasContentsReadAccess returns true if the permissions allow reading contents |
| `permissions_parser.go` | `(*PermissionsParser).IsAllowed` | `func (*PermissionsParser).IsAllowed(scope, level string) bool` | IsAllowed checks if a specific permission scope has the specified access level scope: "contents", "issues", "pull-requests", etc. |
| `permissions_parser.go` | `(*PermissionsParser).ToPermissions` | `func (*PermissionsParser).ToPermissions() *Permissions` | ToPermissions converts a PermissionsParser to a Permissions object |
| `permissions_toolset_data.go` | `(*GitHubToolConfig).GetToolsets` | `func (*GitHubToolConfig).GetToolsets() string` | GetToolsets implements ValidatableTool for GitHubToolConfig |
| `permissions_toolset_data.go` | `(*GitHubToolConfig).IsReadOnly` | `func (*GitHubToolConfig).IsReadOnly() bool` | IsReadOnly implements ValidatableTool for GitHubToolConfig. |
| `permissions_validation.go` | `(*Compiler).ValidateIncludedPermissions` | `func (*Compiler).ValidateIncludedPermissions(topPermissionsYAML string, importedPermissionsJSON string) error` | ValidateIncludedPermissions validates that the main workflow permissions satisfy the imported workflow requirements. |
| `permissions_validation.go` | `ValidatePermissionScopeNames` | `func ValidatePermissionScopeNames(permissionsYAML string) error` | ValidatePermissionScopeNames validates that all permission scope names in the permissions YAML are recognized GitHub Actions permission scopes. |
| `pi_engine.go` | `(*PiEngine).GetAgentManifestFiles` | `func (*PiEngine).GetAgentManifestFiles() []string` | GetAgentManifestFiles returns Pi-specific instruction files treated as security-sensitive manifests. |
| `pi_engine.go` | `(*PiEngine).GetAgentManifestPathPrefixes` | `func (*PiEngine).GetAgentManifestPathPrefixes() []string` | GetAgentManifestPathPrefixes returns Pi-specific config directory prefixes. |
| `pi_engine.go` | `(*PiEngine).GetLogFileForParsing` | `func (*PiEngine).GetLogFileForParsing() string` | GetLogFileForParsing returns the Pi streaming log file path used by the JS log parser. |
| `pi_engine.go` | `(*PiEngine).GetLogParserScriptId` | `func (*PiEngine).GetLogParserScriptId() string` | GetLogParserScriptId returns the script ID for parsing Pi logs. |
| `pi_engine.go` | `(*PiEngine).GetModelEnvVarName` | `func (*PiEngine).GetModelEnvVarName() string` | GetModelEnvVarName returns the legacy Pi model env-var name exposed by gh-aw. |
| `pi_engine.go` | `(*PiEngine).GetRequiredSecretNames` | `func (*PiEngine).GetRequiredSecretNames(workflowData *WorkflowData) []string` | GetRequiredSecretNames returns the list of secrets required by the Pi engine. |
| `pi_engine.go` | `(*PiEngine).GetSecretValidationStep` | `func (*PiEngine).GetSecretValidationStep(workflowData *WorkflowData) GitHubActionStep` | GetSecretValidationStep returns the secret validation step for the Pi engine. |
| `pi_engine.go` | `(*PiEngine).GetSupportedEnvVarKeys` | `func (*PiEngine).GetSupportedEnvVarKeys() []string` | GetSupportedEnvVarKeys returns the engine. |
| `pi_engine.go` | `(*PiEngine).ResolveLLMProvider` | `func (*PiEngine).ResolveLLMProvider(workflowData *WorkflowData) string` | ResolveLLMProvider returns the effective provider for Pi inference. |
| `pi_logs.go` | `(*PiEngine).ParseLogMetrics` | `func (*PiEngine).ParseLogMetrics(logContent string, verbose bool) LogMetrics` | ParseLogMetrics parses Pi streaming JSONL log output and extracts metrics. |
| `pi_mcp.go` | `(*PiEngine).RenderMCPConfig` | `func (*PiEngine).RenderMCPConfig(yaml *strings.Builder, tools map[string]any, mcpTools []string, workflowData *WorkflowData) error` | RenderMCPConfig renders the MCP configuration for Pi engine. |
| `pr.go` | `ShouldGeneratePRCheckoutStep` | `func ShouldGeneratePRCheckoutStep(data *WorkflowData) bool` | ShouldGeneratePRCheckoutStep returns true if the checkout-pr step should be generated based on the workflow permissions. |
| `repo_config.go` | `(*MaintenanceConfig).IsJobDisabled` | `func (*MaintenanceConfig).IsJobDisabled(jobName string) bool` | IsJobDisabled reports whether the provided maintenance job ID is explicitly disabled in aw. |
| `repo_config.go` | `(*MaintenanceConfig).IsLabelTriggerEnabled` | `func (*MaintenanceConfig).IsLabelTriggerEnabled() bool` | IsLabelTriggerEnabled returns true only when label_triggers is explicitly set to true. |
| `repo_config.go` | `(*RepoConfig).ActionFailureIssueExpiresHours` | `func (*RepoConfig).ActionFailureIssueExpiresHours() int` | ActionFailureIssueExpiresHours returns the configured action failure issue expiration in hours, or the default value when unset. |
| `repo_config.go` | `(*RepoConfig).IsAutoUpgradeEnabled` | `func (*RepoConfig).IsAutoUpgradeEnabled() bool` | IsAutoUpgradeEnabled returns true only when auto_upgrade is explicitly set to true. |
| `repo_config.go` | `(*RepoConfig).IsHelpCommandEnabled` | `func (*RepoConfig).IsHelpCommandEnabled() bool` | IsHelpCommandEnabled returns true when the builtin centralized /help command handler should be enabled. |
| `repo_config.go` | `(*RepoConfig).UnmarshalJSON` | `func (*RepoConfig).UnmarshalJSON(data []byte) error` | UnmarshalJSON implements json. |
| `repo_config.go` | `(*RunsOnValue).UnmarshalJSON` | `func (*RunsOnValue).UnmarshalJSON(data []byte) error` | UnmarshalJSON implements json. |
| `runs_on_unmarshal.go` | `(*SafeOutputsConfig).UnmarshalJSON` | `func (*SafeOutputsConfig).UnmarshalJSON(data []byte) error` | UnmarshalJSON supports string/array/object forms for safe-outputs. |
| `runs_on_unmarshal.go` | `(*ThreatDetectionConfig).UnmarshalJSON` | `func (*ThreatDetectionConfig).UnmarshalJSON(data []byte) error` | UnmarshalJSON supports string/array/object forms for safe-outputs. |
| `runtime_deduplication.go` | `DeduplicateRuntimeSetupStepsFromCustomSteps` | `func DeduplicateRuntimeSetupStepsFromCustomSteps(customSteps string, runtimeRequirements []RuntimeRequirement) (string, []RuntimeRequirement, error)` | DeduplicateRuntimeSetupStepsFromCustomSteps removes runtime setup action steps from custom steps to avoid duplication when runtime steps are added before custom steps. |
| `runtime_detection.go` | `DetectRuntimeRequirements` | `func DetectRuntimeRequirements(workflowData *WorkflowData) []RuntimeRequirement` | DetectRuntimeRequirements analyzes workflow data to detect required runtimes |
| `runtime_step_generator.go` | `GenerateRuntimeSetupSteps` | `func GenerateRuntimeSetupSteps(requirements []RuntimeRequirement, data *WorkflowData) []GitHubActionStep` | GenerateRuntimeSetupSteps creates GitHub Actions steps for runtime setup. |
| `safe_update_manifest.go` | `(*GHAWManifest).ToJSON` | `func (*GHAWManifest).ToJSON() (string, error)` | ToJSON serialises the manifest to a compact, single-line JSON string suitable for embedding in a YAML comment header. |
| `script_registry.go` | `(*ScriptRegistry).GetActionPath` | `func (*ScriptRegistry).GetActionPath(name string) string` | GetActionPath retrieves the custom action path for a script, if registered. |
| `script_registry.go` | `GetAllScriptFilenames` | `func GetAllScriptFilenames() []string` | GetAllScriptFilenames returns a sorted list of all . |
| `script_registry.go` | `NewScriptRegistry` | `func NewScriptRegistry() *ScriptRegistry` | NewScriptRegistry creates a new empty script registry. |
| `secret_extraction.go` | `ExtractEnvExpressionsFromMap` | `func ExtractEnvExpressionsFromMap(values map[string]string) map[string]string` | ExtractEnvExpressionsFromMap extracts all env variable expressions from a map of string values Returns a map of environment variable names to their full env expressions (including fallbacks) Example: Input: {"SENTRY_HOS… |
| `secret_extraction.go` | `ExtractEnvExpressionsFromValue` | `func ExtractEnvExpressionsFromValue(value string) map[string]string` | ExtractEnvExpressionsFromValue extracts all GitHub Actions env expressions from a string value Returns a map of environment variable names to their full env expressions Examples: - "${{ env. |
| `secret_extraction.go` | `ExtractSecretsFromMap` | `func ExtractSecretsFromMap(values map[string]string) map[string]string` | ExtractSecretsFromMap extracts all secrets from a map of string values Returns a map of environment variable names to their full secret expressions Example: Input: {"DD_API_KEY": "${{ secrets. |
| `secret_extraction.go` | `ExtractWorkflowInputExpressionsFromValue` | `func ExtractWorkflowInputExpressionsFromValue(value string) map[string]string` | ExtractWorkflowInputExpressionsFromValue extracts simple workflow input expressions from a string value and maps them to deterministic environment variable names. |
| `secret_extraction.go` | `ReplaceTemplateExpressionsWithEnvVars` | `func ReplaceTemplateExpressionsWithEnvVars(value string) string` | ReplaceTemplateExpressionsWithEnvVars replaces all template expressions with environment variable references Handles: secrets. |
| `secret_masking.go` | `(*Compiler).MergeSecretMasking` | `func (*Compiler).MergeSecretMasking(topConfig *SecretMaskingConfig, importedSecretMaskingJSON string) (*SecretMaskingConfig, error)` | MergeSecretMasking merges secret-masking configurations from imports with top-level config |
| `service_ports.go` | `ExtractServicePortExpressions` | `func ExtractServicePortExpressions(servicesYAML string) (string, []string)` | ExtractServicePortExpressions parses the services: YAML string from WorkflowData. |
| `step_order_validation.go` | `(*StepOrderTracker).MarkAgentExecutionComplete` | `func (*StepOrderTracker).MarkAgentExecutionComplete()` | MarkAgentExecutionComplete marks that we've passed the agent execution step Validation only applies to steps after this point |
| `step_order_validation.go` | `(*StepOrderTracker).RecordArtifactUpload` | `func (*StepOrderTracker).RecordArtifactUpload(stepName string, uploadPaths []string)` | RecordArtifactUpload records that an artifact upload step was added |
| `step_order_validation.go` | `(*StepOrderTracker).RecordSecretRedaction` | `func (*StepOrderTracker).RecordSecretRedaction(stepName string)` | RecordSecretRedaction records that a secret redaction step was added |
| `step_order_validation.go` | `(*StepOrderTracker).ValidateStepOrdering` | `func (*StepOrderTracker).ValidateStepOrdering() error` | ValidateStepOrdering validates that secret redaction happens before artifact uploads and that all uploaded paths are covered by secret redaction |
| `step_order_validation.go` | `NewStepOrderTracker` | `func NewStepOrderTracker() *StepOrderTracker` | NewStepOrderTracker creates a new step order tracker |
| `step_types.go` | `(*WorkflowStep).Clone` | `func (*WorkflowStep).Clone() *WorkflowStep` | Clone creates a deep copy of the WorkflowStep |
| `step_types.go` | `(*WorkflowStep).IsUsesStep` | `func (*WorkflowStep).IsUsesStep() bool` | IsUsesStep returns true if this step uses an action (has a "uses" field) |
| `step_types.go` | `(*WorkflowStep).ToMap` | `func (*WorkflowStep).ToMap() map[string]any` | ToMap converts a WorkflowStep to a map[string]any for YAML generation This is used when generating the final workflow YAML output |
| `stop_after.go` | `ExtractStopTimeFromLockFile` | `func ExtractStopTimeFromLockFile(lockFilePath string) string` | ExtractStopTimeFromLockFile extracts the STOP_TIME value from a compiled workflow lock file |
| `strings.go` | `SanitizeArtifactIdentifier` | `func SanitizeArtifactIdentifier(name string) string` | SanitizeArtifactIdentifier sanitizes a workflow name to create a safe identifier suitable for use as a user agent string or similar context. |
| `templatables.go` | `(*TemplatableBool).MarshalJSON` | `func (*TemplatableBool).MarshalJSON() ([]byte, error)` | MarshalJSON emits a JSON boolean for literal values and a JSON string for GitHub Actions expressions. |
| `templatables.go` | `(*TemplatableBool).UnmarshalJSON` | `func (*TemplatableBool).UnmarshalJSON(data []byte) error` | UnmarshalJSON allows TemplatableBool to accept both JSON booleans and JSON strings that are GitHub Actions expressions. |
| `templatables.go` | `(*TemplatableBool).UnmarshalYAML` | `func (*TemplatableBool).UnmarshalYAML(node *yaml.Node) error` | UnmarshalYAML allows TemplatableBool to accept both YAML booleans and GitHub Actions expression strings. |
| `templatables.go` | `(*TemplatableBoolOrInt).IsExpression` | `func (*TemplatableBoolOrInt).IsExpression() bool` | IsExpression returns true if the value is a GitHub Actions expression. |
| `templatables.go` | `(*TemplatableBoolOrInt).MarshalJSON` | `func (*TemplatableBoolOrInt).MarshalJSON() ([]byte, error)` | MarshalJSON emits a JSON boolean for "true"/"false", a JSON integer for numeric strings, and a JSON string for GitHub Actions expressions. |
| `templatables.go` | `(*TemplatableBoolOrInt).ToValue` | `func (*TemplatableBoolOrInt).ToValue() any` | ToValue returns the native Go value for use in map literals and JSON output: - true/false for boolean literals - an int for numeric literals - a string for GitHub Actions expressions |
| `templatables.go` | `(*TemplatableBoolOrInt).UnmarshalJSON` | `func (*TemplatableBoolOrInt).UnmarshalJSON(data []byte) error` | UnmarshalJSON allows TemplatableBoolOrInt to accept JSON booleans, JSON numbers, and JSON strings that are GitHub Actions expressions. |
| `templatables.go` | `(*TemplatableBoolOrInt).UnmarshalYAML` | `func (*TemplatableBoolOrInt).UnmarshalYAML(node *yaml.Node) error` | UnmarshalYAML allows TemplatableBoolOrInt to accept YAML booleans, YAML integers, and GitHub Actions expression strings. |
| `templatables.go` | `(*TemplatableInt32).IntValue` | `func (*TemplatableInt32).IntValue() int` | IntValue returns the integer value for numeric literals. |
| `templatables.go` | `(*TemplatableInt32).IsExpression` | `func (*TemplatableInt32).IsExpression() bool` | IsExpression returns true if the value is a GitHub Actions expression (i. |
| `templatables.go` | `(*TemplatableInt32).MarshalJSON` | `func (*TemplatableInt32).MarshalJSON() ([]byte, error)` | MarshalJSON emits a JSON number for numeric literals and a JSON string for GitHub Actions expressions. |
| `templatables.go` | `(*TemplatableInt32).Ptr` | `func (*TemplatableInt32).Ptr() *TemplatableInt32` | Ptr returns a pointer to a copy of t, convenient for constructing *TemplatableInt32 values inline. |
| `templatables.go` | `(*TemplatableInt32).ToValue` | `func (*TemplatableInt32).ToValue() any` | ToValue returns the native Go value for use in map literals and JSON output: - an int for numeric literals (e. |
| `templatables.go` | `(*TemplatableInt32).UnmarshalJSON` | `func (*TemplatableInt32).UnmarshalJSON(data []byte) error` | UnmarshalJSON allows TemplatableInt32 to accept both JSON numbers (integer literals) and JSON strings that are GitHub Actions expressions. |
| `threat_detection_config.go` | `(*ThreatDetectionConfig).HasRunnableDetection` | `func (*ThreatDetectionConfig).HasRunnableDetection() bool` | HasRunnableDetection reports whether this config will produce a detection job that actually executes. |
| `threat_detection_config.go` | `(*ThreatDetectionConfig).IsConditional` | `func (*ThreatDetectionConfig).IsConditional() bool` | IsConditional reports whether detection is expression-controlled (enabled/disabled at runtime). |
| `threat_detection_config.go` | `(*ThreatDetectionConfig).IsContinueOnError` | `func (*ThreatDetectionConfig).IsContinueOnError() bool` | IsContinueOnError reports whether detection failures should produce warnings instead of errors. |
| `threat_detection_helpers.go` | `IsConditionalDetection` | `func IsConditionalDetection(so *SafeOutputsConfig) bool` | IsConditionalDetection reports whether the safe-outputs configuration uses an expression to control threat detection at runtime. |
| `tools_types.go` | `(*PlaywrightToolConfig).IsCLIMode` | `func (*PlaywrightToolConfig).IsCLIMode() bool` | IsCLIMode returns true when the playwright tool is configured in CLI mode (mode: cli). |
| `tools_types.go` | `(*Tools).GetToolNames` | `func (*Tools).GetToolNames() []string` | GetToolNames returns a list of all tool names configured |
| `tools_types.go` | `(*Tools).HasTool` | `func (*Tools).HasTool(name string) bool` | HasTool checks if a tool is present in the configuration |
| `tools_types.go` | `(*ToolsConfig).ToMap` | `func (*ToolsConfig).ToMap() map[string]any` | ToMap converts the ToolsConfig back to a map[string]any for backward compatibility. |
| `tools_types.go` | `(GitHubAllowedTools).ToStringSlice` | `func (GitHubAllowedTools).ToStringSlice() []string` | ToStringSlice converts GitHubAllowedTools to []string |
| `tools_types.go` | `(GitHubToolsets).ToStringSlice` | `func (GitHubToolsets).ToStringSlice() []string` | ToStringSlice converts GitHubToolsets to []string |
| `trigger_parser.go` | `(*TriggerIR).ToYAMLMap` | `func (*TriggerIR).ToYAMLMap() map[string]any` | ToYAMLMap converts a TriggerIR to a map structure suitable for YAML generation |
| `universal_llm_consumer_engine.go` | `(*UniversalLLMConsumerEngine).ApplyUniversalProviderEnv` | `func (*UniversalLLMConsumerEngine).ApplyUniversalProviderEnv(env map[string]string, workflowData *WorkflowData, firewallEnabled bool)` | Exported function or method declared in `universal_llm_consumer_engine.go`. |
| `universal_llm_consumer_engine.go` | `(*UniversalLLMConsumerEngine).BuildCLIEngineExecutionSteps` | `func (*UniversalLLMConsumerEngine).BuildCLIEngineExecutionSteps( workflowData *WorkflowData, logFile string, cfg UniversalCLIEngineExecutionConfig, ) []GitHubActionStep` | BuildCLIEngineExecutionSteps generates the GitHub Actions execution steps for a universal CLI engine (e. |
| `universal_llm_consumer_engine.go` | `(*UniversalLLMConsumerEngine).GetUniversalRequiredSecretNames` | `func (*UniversalLLMConsumerEngine).GetUniversalRequiredSecretNames(workflowData *WorkflowData) []string` | Exported function or method declared in `universal_llm_consumer_engine.go`. |
| `universal_llm_consumer_engine.go` | `(*UniversalLLMConsumerEngine).GetUniversalSecretValidationStep` | `func (*UniversalLLMConsumerEngine).GetUniversalSecretValidationStep(workflowData *WorkflowData, engineName, docsURL string) GitHubActionStep` | Exported function or method declared in `universal_llm_consumer_engine.go`. |
| `utc_offset.go` | `NormalizeUTCOffset` | `func NormalizeUTCOffset(raw string) (string, error)` | NormalizeUTCOffset validates and normalizes a numeric UTC offset. |
| `utc_offset.go` | `ParseUTCOffsetLocation` | `func ParseUTCOffsetLocation(raw string) (*time.Location, error)` | ParseUTCOffsetLocation converts a numeric UTC offset to a fixed time. |
| `workflow_data.go` | `(*WorkflowData).PinContext` | `func (*WorkflowData).PinContext() *actionpins.PinContext` | PinContext returns an actionpins. |
| `workflow_errors.go` | `(*ErrorCollector).Add` | `func (*ErrorCollector).Add(err error) error` | Add adds an error to the collector If failFast is enabled, returns the error immediately Otherwise, adds it to the collection and returns nil |
| `workflow_errors.go` | `(*ErrorCollector).Count` | `func (*ErrorCollector).Count() int` | Count returns the number of errors collected |
| `workflow_errors.go` | `(*ErrorCollector).FormattedError` | `func (*ErrorCollector).FormattedError(category string) error` | FormattedError returns the aggregated error with a formatted header showing the count Returns nil if no errors were collected This method is preferred over Error() + FormatAggregatedError for better accuracy |
| `workflow_errors.go` | `(*OperationError).Unwrap` | `func (*OperationError).Unwrap() error` | Unwrap returns the underlying error |
| `workflow_errors.go` | `(*SharedWorkflowError).IsSharedWorkflow` | `func (*SharedWorkflowError).IsSharedWorkflow() bool` | IsSharedWorkflow returns true, indicating this is a shared workflow |
| `yaml.go` | `UnquoteYAMLTopLevelKey` | `func UnquoteYAMLTopLevelKey(yamlStr string, key string) string` | UnquoteYAMLTopLevelKey removes quotes from a YAML key only when it appears at the very start of the YAML content. |

<!-- END SOURCE-VERIFIED EXPORT COVERAGE -->

## Source Synchronization

Reviewed against recent source updates on 2026-07-17; no additional public-contract deltas were identified beyond the sections above.

---

*This specification is automatically maintained by the [spec-extractor](../../.github/workflows/spec-extractor.md) workflow.*
