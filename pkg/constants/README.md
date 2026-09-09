# constants Package

> Shared semantic types, runtime defaults, and named constants used throughout `gh-aw`.

## Overview

The `constants` package centralizes stable identifiers and configuration defaults that are referenced across workflow compilation, runtime setup, engine integration, sandboxing, artifact handling, and validation. It defines semantic string and integer wrapper types such as `EngineName`, `FeatureFlag`, `JobName`, `StepID`, `Version`, and `DocURL` so call sites can express intent more clearly than with untyped primitives alone.

The package also acts as the authoritative source for the generated workflow contract: built-in job names, artifact names, environment variable names, file-system layout, AWF paths, MCP defaults, supported engines, default tool allowlists, and pinned dependency versions. Most values are plain exported constants or variables because they are consumed from many packages and are expected to remain compile-time visible.

## Public API

### Types

| Type | Kind | Description |
|------|------|-------------|
| `ArtifactName` | alias | Semantic string type for GitHub Actions artifact names (`actions/upload-artifact`/`download-artifact`), distinct from filenames or file paths. |
| `CommandPrefix` | alias | Semantic string type for user-facing CLI command prefixes. |
| `DocURL` | alias | Semantic string type for documentation URLs used in validation and help messages. |
| `EngineName` | alias | Semantic string type for AI engine identifiers such as `copilot` and `claude`. |
| `EngineOption` | struct | Display and secret metadata for a selectable engine. |
| `FeatureFlag` | alias | Semantic string type for workflow feature flag identifiers. |
| `Filename` | alias | Semantic string type for a file's base name (without a directory path), distinct from `FilePath` and `ArtifactName`. |
| `FilePath` | alias | Semantic string type for a filesystem (or GitHub Actions expression) path to a file or directory, distinct from `Filename`. |
| `JobName` | alias | Semantic string type for built-in GitHub Actions job names. |
| `LineLength` | alias | Semantic integer type for formatting thresholds. |
| `MCPServerID` | alias | Semantic string type for built-in MCP server identifiers. |
| `ModelName` | alias | Semantic string type for AI model identifiers. |
| `StepID` | alias | Semantic string type for GitHub Actions step identifiers. |
| `SystemSecretSpec` | struct | Metadata describing a non-engine-specific secret. |
| `URL` | alias | Semantic string type for general URLs. |
| `Version` | alias | Semantic string type for version pins and minimum-version gates. |
| `WorkflowID` | alias | Semantic string type for workflow basenames without the `.md` suffix. |

### Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `GetAllEngineSecretNames` | `func GetAllEngineSecretNames() []string` | Returns the deduplicated union of engine primary secrets, alternative secrets, and system secrets. |
| `GetEngineOption` | `func GetEngineOption(engineValue string) *EngineOption` | Returns the configured engine option for an engine value, or `nil` when none exists. |
| `GetWorkflowDir` | `func GetWorkflowDir() string` | Returns the workflow directory, honoring `GH_AW_WORKFLOWS_DIR` and normalizing path separators to `/`. |

### Constants

| Constant | Type | Value | Description |
|----------|------|-------|-------------|
| `CLIExtensionPrefix` | `CommandPrefix` | `"gh aw"` | Canonical user-facing CLI prefix. |
| `ExpressionBreakThreshold` | `LineLength` | `100` | Preferred break point for wrapping long expressions. |
| `MaxExpressionLineLength` | `LineLength` | `120` | Maximum single-line expression length before wrapping. |
| `DefaultEngine` | `EngineName` | `CopilotEngine` | Default engine when workflows do not specify one. |
| `CopilotEngine` | `EngineName` | `"copilot"` | GitHub Copilot engine identifier. |
| `ClaudeEngine` | `EngineName` | `"claude"` | Claude engine identifier. |
| `CodexEngine` | `EngineName` | `"codex"` | Codex engine identifier. |
| `GeminiEngine` | `EngineName` | `"gemini"` | Gemini engine identifier. |
| `PiEngine` | `EngineName` | `"pi"` | Pi engine identifier. |
| `MCPScriptsFeatureFlag` | `FeatureFlag` | `"mcp-scripts"` | Enables mcp-scripts workflow support. |
| `MCPGatewayFeatureFlag` | `FeatureFlag` | `"mcp-gateway"` | Enables MCP gateway workflow support. |
| `DisableXPIAPromptFeatureFlag` | `FeatureFlag` | `"disable-xpia-prompt"` | Disables the XPIA prompt path. |
| `DIFCProxyFeatureFlag` | `FeatureFlag` | `"difc-proxy"` | Deprecated integrity proxy flag kept for compatibility. |
| `CliProxyFeatureFlag` | `FeatureFlag` | `"cli-proxy"` | Enables the AWF CLI proxy sidecar path. |
| `AwfDiagnosticLogsFeatureFlag` | `FeatureFlag` | `"awf-diagnostic-logs"` | Enables AWF failure diagnostics capture. |
| `ByokCopilotFeatureFlag` | `FeatureFlag` | `"byok-copilot"` | Deprecated legacy Copilot BYOK flag. |
| `IntegrityReactionsFeatureFlag` | `FeatureFlag` | `"integrity-reactions"` | Enables reaction-based integrity promotion and demotion. |
| `GroupConcurrencyQueueFeatureFlag` | `FeatureFlag` | `"group-concurrency-queue"` | Controls whether generated group concurrency uses `queue: max`. |
| `DangerouslyDisableSandboxAgentFeatureFlag` | `FeatureFlag` | `"dangerously-disable-sandbox-agent"` | Required to allow `sandbox.agent: false`. |
| `GHAWDetectionFeatureFlag` | `FeatureFlag` | `"gh-aw-detection"` | Enables the external threat-detection binary path by default; set to `false` for the legacy inline path. |
| `AgentJobName` | `JobName` | `"agent"` | Built-in agent job identifier. |
| `ActivationJobName` | `JobName` | `"activation"` | Built-in activation job identifier. |
| `PreActivationJobName` | `JobName` | `"pre_activation"` | Built-in pre-activation job identifier. |
| `PreActivationHyphenJobName` | `JobName` | `"pre-activation"` | Hyphenated compatibility alias for the pre-activation job. |
| `DetectionJobName` | `JobName` | `"detection"` | Built-in detection job identifier. |
| `EvalsJobName` | `JobName` | `"evals"` | Built-in evals job identifier. |
| `SafeOutputsJobName` | `JobName` | `"safe_outputs"` | Built-in safe-outputs job identifier. |
| `SafeOutputsHyphenJobName` | `JobName` | `"safe-outputs"` | Hyphenated compatibility alias for the safe-outputs job. |
| `UploadAssetsJobName` | `JobName` | `"upload_assets"` | Built-in asset-upload job identifier. |
| `UploadCodeScanningJobName` | `JobName` | `"upload_code_scanning_sarif"` | Built-in SARIF upload job identifier. |
| `ConclusionJobName` | `JobName` | `"conclusion"` | Built-in conclusion job identifier. |
| `UnlockJobName` | `JobName` | `"unlock"` | Built-in unlock job identifier. |
| `GitHubMCPServerID` | `MCPServerID` | `"github"` | Built-in GitHub MCP server identifier. |
| `SafeOutputsMCPServerID` | `MCPServerID` | `"safeoutputs"` | Built-in safe-outputs MCP server identifier. |
| `MCPScriptsMCPServerID` | `MCPServerID` | `"mcpscripts"` | Built-in mcp-scripts MCP server identifier. |
| `AgenticWorkflowsMCPServerID` | `MCPServerID` | `"agenticworkflows"` | Built-in agentic-workflows MCP server identifier. |
| `MCPScriptsMCPVersion` | untyped `string` | `"1.0.0"` | Version identifier for the mcp-scripts MCP server. |
| `DefaultMCPRegistryURL` | `URL` | `"https://api.mcp.github.com/v0.1"` | Default MCP registry endpoint. |
| `PublicGitHubHost` | `URL` | `"https://github.com"` | Canonical public GitHub host. |
| `GitHubCopilotMCPDomain` | untyped `string` | `"api.githubcopilot.com"` | Hosted GitHub MCP server domain used by remote mode. |
| `DocsEnginesURL` | `DocURL` | `"https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/engines.md"` | Engines reference documentation URL. |
| `DocsToolsURL` | `DocURL` | `"https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/tools.md"` | Tools reference documentation URL. |
| `DocsGitHubToolsURL` | `DocURL` | `"https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/tools.md#github-tools-github"` | GitHub tools subsection URL. |
| `DocsPermissionsURL` | `DocURL` | `"https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/permissions.md"` | Permissions reference URL. |
| `DocsNetworkURL` | `DocURL` | `"https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/network.md"` | Network reference URL. |
| `DocsSandboxURL` | `DocURL` | `"https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/sandbox.md"` | Sandbox reference URL. |

## Usage Examples

```go
engine := constants.CopilotEngine
fmt.Println(string(engine))

opt := constants.GetEngineOption("copilot")
fmt.Println(opt.Label)
fmt.Println(opt.SecretName)
```

```go
dir := constants.GetWorkflowDir()
fmt.Println(dir) // ".github/workflows" unless GH_AW_WORKFLOWS_DIR is set
```

```go
fmt.Println(constants.AgentJobName.String())
fmt.Println(constants.CheckMembershipStepID.String())
fmt.Println(constants.DefaultCopilotVersion.String())
```

## Design Decisions

The package intentionally uses many named string and integer types instead of raw primitives. Types such as `CommandPrefix`, `JobName`, `StepID`, `Version`, and `DocURL` implement `String()` and, where appropriate, `IsValid()` so callers can keep signatures expressive without introducing complex wrappers.

Constants in this package SHOULD be treated as the single source of truth for generated workflow identifiers, pinned tool versions, filesystem paths, and engine environment variables. Functions such as `GetAllEngineSecretNames` MUST return deduplicated values because callers use the result for validation and secret wiring.

`GetWorkflowDir` reads `GH_AW_WORKFLOWS_DIR` at call time and normalizes it with `filepath.ToSlash`. Callers MAY override the default in tests or CI, but code that persists GitHub paths SHOULD expect forward slashes in the returned value.

The package preserves compatibility aliases where workflow history requires them. For example, `AgenticEngines` is deprecated in favor of the workflow engine catalog, and `DefaultGitHubTools` is a deprecated alias for `DefaultGitHubToolsLocal`, but both remain exported for older callers.

## Dependencies

Internal dependencies include `pkg/setutil` for deduplicating engine secret names. External dependencies are limited to the Go standard library, including `io/fs`, `os`, `path/filepath`, and `time`.

## Thread Safety

The package exports immutable constants plus package-level slices, maps, and structs that are initialized once and then read by callers. Read-only access is safe for concurrent use. Callers SHOULD treat exported variables such as `EngineOptions`, `SystemSecrets`, `AllowedExpressions`, `DefaultReadOnlyGitHubTools`, and `CopilotStemCommands` as shared configuration data and SHOULD NOT mutate them concurrently.

---

*This specification is automatically maintained by the [spec-extractor](../../.github/workflows/spec-extractor.md) workflow.*
