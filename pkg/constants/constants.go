package constants

import (
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// CLIExtensionPrefix is the prefix used in user-facing output to refer to the CLI extension.
const CLIExtensionPrefix CommandPrefix = "gh aw"

// Semantic types for measurements and identifiers
//
// These type aliases provide meaningful names for primitive types, improving code clarity
// and type safety. They follow the semantic type alias pattern where the type name
// indicates both what the value represents and how it should be used.
//
// # Intentional Method Duplication
//
// Several string-based types below define identical String() and IsValid() method bodies.
// This duplication is intentional: Go does not allow shared method sets for distinct named
// types, so each type must define its own methods. The bodies are deliberately simple and
// unlikely to diverge.

// LineLength represents a line length in characters for expression formatting.
// This semantic type distinguishes line lengths from arbitrary integers,
// making formatting code more readable and preventing accidental misuse.
//
// Example usage:
//
//	if len(expression) > int(constants.MaxExpressionLineLength) {
//	    // Break into multiple lines
//	}
type LineLength int

// CommandPrefix represents a CLI command prefix.
// This semantic type distinguishes command prefixes from arbitrary strings,
// making command-related operations explicit.
//
// Example usage:
//
//	const CLIExtensionPrefix CommandPrefix = "gh aw"
//	func FormatCommand(prefix CommandPrefix, cmd string) string { ... }
type CommandPrefix string

// String returns the string representation of the command prefix
func (c CommandPrefix) String() string {
	return string(c)
}

// IsValid returns true if the command prefix is non-empty
func (c CommandPrefix) IsValid() bool {
	return c != ""
}

// WorkflowID represents a workflow identifier (basename without .md extension).
// This semantic type distinguishes workflow identifiers from arbitrary strings,
// preventing mixing of workflow IDs with other string types like file paths.
//
// Example usage:
//
//	func GetWorkflow(id WorkflowID) (*Workflow, error) { ... }
//	func CompileWorkflow(id WorkflowID) error { ... }
type WorkflowID string

// String returns the string representation of the workflow ID
func (w WorkflowID) String() string {
	return string(w)
}

// IsValid returns true if the workflow ID is non-empty
func (w WorkflowID) IsValid() bool {
	return w != ""
}

// ArtifactName represents the name of a GitHub Actions artifact (as passed to
// actions/upload-artifact and actions/download-artifact).
// This semantic type distinguishes artifact names from arbitrary strings,
// preventing accidental mixing of artifact names with filenames or file paths.
//
// Example usage:
//
//	const AgentArtifactName ArtifactName = "agent"
//	func DownloadArtifact(name ArtifactName) error { ... }
type ArtifactName string

// String returns the string representation of the artifact name
func (a ArtifactName) String() string {
	return string(a)
}

// IsValid returns true if the artifact name is non-empty
func (a ArtifactName) IsValid() bool {
	return a != ""
}

// Filename represents the base name of a file (without a directory path).
// This semantic type distinguishes filenames from full file paths or artifact
// names, preventing accidental mixing of the two.
//
// Example usage:
//
//	const AgentOutputFilename Filename = "agent_output.json"
//	func WriteFile(name Filename) error { ... }
type Filename string

// String returns the string representation of the filename
func (f Filename) String() string {
	return string(f)
}

// IsValid returns true if the filename is non-empty
func (f Filename) IsValid() bool {
	return f != ""
}

// FilePath represents a filesystem (or GitHub Actions expression) path to a
// file or directory. This semantic type distinguishes file paths from bare
// filenames, preventing accidental mixing of the two.
//
// Example usage:
//
//	const AWFConfigFilePath FilePath = "/tmp/gh-aw/awf-config.json"
//	func ReadFile(path FilePath) ([]byte, error) { ... }
type FilePath string

// String returns the string representation of the file path
func (p FilePath) String() string {
	return string(p)
}

// IsValid returns true if the file path is non-empty
func (p FilePath) IsValid() bool {
	return p != ""
}

// MaxExpressionLineLength is the maximum length for a single line expression before breaking into multiline.
const MaxExpressionLineLength LineLength = 120

// ExpressionBreakThreshold is the threshold for breaking long lines at logical points.
const ExpressionBreakThreshold LineLength = 100

// File-permission policy for files and directories written by gh-aw.
const (
	// FilePermSensitive is owner-only read/write (0o600). Use for files that may
	// contain secrets, credentials, downloaded remote content, or audit/log output.
	FilePermSensitive fs.FileMode = 0o600

	// FilePermPublic is owner read/write + world read (0o644). Use for files that
	// are intentionally world-readable (e.g. generated files for inspection).
	FilePermPublic fs.FileMode = 0o644

	// FilePermExecutable is owner/group/world executable (0o755). Use for generated
	// scripts or binaries that must be executed.
	FilePermExecutable fs.FileMode = 0o755

	// DirPermSensitive is owner+group access (0o750). Use for directories that
	// contain sensitive files.
	DirPermSensitive fs.FileMode = 0o750

	// DirPermPublic is standard non-sensitive directory access (0o755).
	DirPermPublic fs.FileMode = 0o755
)

// Network port constants
//
// These constants define standard network port values used throughout the codebase
// for MCP servers, gateway services, and validation ranges.

const (
	// AWFAPIProxyContainerIP is the fixed api-proxy sidecar address inside the AWF sandbox network.
	AWFAPIProxyContainerIP = "172.30.0.30"

	// DefaultMCPGatewayPort is the default port for the MCP gateway HTTP service
	DefaultMCPGatewayPort = 8080

	// DefaultMCPServerPort is the default port for MCP servers (mcp-scripts server)
	DefaultMCPServerPort = 3000

	// DefaultMCPInspectorPort is the default port for the MCP inspector (safe-outputs server)
	DefaultMCPInspectorPort = 3001

	// DefaultCopilotSDKPort is the default localhost port for the Copilot CLI HTTP server
	// when running in headless SDK mode (copilot-sdk: true). The harness starts a
	// separate Copilot CLI sidecar with --headless --port <port>, and the SDK connects via
	// COPILOT_SDK_URI = "http://127.0.0.1:DefaultCopilotSDKPort".
	DefaultCopilotSDKPort = 3002

	// MinNetworkPort is the minimum valid network port number
	MinNetworkPort = 1

	// MaxNetworkPort is the maximum valid network port number
	MaxNetworkPort = 65535

	// CodexLLMGatewayPort is the port for the Codex/OpenAI LLM gateway.
	// AWF's api-proxy sidecar assigns 10000 to the OpenAI-compatible provider.
	CodexLLMGatewayPort = 10000

	// ClaudeLLMGatewayPort is the port for the Claude/Anthropic LLM gateway.
	// AWF's api-proxy sidecar assigns 10001 to the Anthropic-compatible provider.
	ClaudeLLMGatewayPort = 10001

	// CopilotLLMGatewayPort is the port for the Copilot LLM gateway
	CopilotLLMGatewayPort = 10002

	// GeminiLLMGatewayPort is the port for the Gemini LLM gateway
	GeminiLLMGatewayPort = 10003
)

// AWFNoProxyHosts is the value for the NO_PROXY and no_proxy environment variables
// in the AWF agent execution environment.  Both plain hostnames and explicit host:port
// forms are listed because some HTTP client runtimes (e.g. Bun)
// compare the full host:port string against the list rather than stripping the port
// before matching, causing them to route through Squid even when the hostname alone
// appears in the list.
// AWFAPIProxyContainerIP is also listed so that requests from custom providers
// (e.g. an awf-proxy provider) routed to the internal api-proxy sidecar
// bypass Squid instead of being forwarded through it.
const AWFNoProxyHosts = "localhost,127.0.0.1," +
	"host.docker.internal," +
	"host.docker.internal:10000," +
	"host.docker.internal:10001," +
	"host.docker.internal:10002," +
	"host.docker.internal:10003," +
	AWFAPIProxyContainerIP + "," +
	AWFAPIProxyContainerIP + ":10000," +
	AWFAPIProxyContainerIP + ":10001," +
	AWFAPIProxyContainerIP + ":10002," +
	AWFAPIProxyContainerIP + ":10003"

// DefaultGitHubLockdown is the default value for the GitHub MCP server lockdown setting.
// Lockdown mode restricts the GitHub MCP server to the triggering repository only.
// Defaults to false (lockdown disabled).
const DefaultGitHubLockdown = false

// OTELSentryEndpointSecretName is the well-known secret name used by shared OTLP
// workflow imports for Sentry endpoint configuration.
const OTELSentryEndpointSecretName = "GH_AW_OTEL_SENTRY_ENDPOINT"

// AWF (Agentic Workflow Firewall) constants

// AWFDefaultCommand is the default AWF command prefix.
// Strict security (no sudo) is the default since AWF v0.27.32.
const AWFDefaultCommand CommandPrefix = "awf"

// AWFCloudHypervisorCommand runs AWF with the host privileges required to
// create a Cloud Hypervisor VM while preserving the runner paths it consumes.
// sudo supplies SUDO_UID and SUDO_GID for AWF to recover the invoking identity.
const AWFCloudHypervisorCommand = "sudo --preserve-env awf"

// DefaultCloudHypervisorVCPUs and DefaultCloudHypervisorMemoryMiB are the
// minimum viable guest sizing defaults for the Cloud Hypervisor agent runtime.
const (
	DefaultCloudHypervisorVCPUs     = 2
	DefaultCloudHypervisorMemoryMiB = 4096
)

// AWFLegacySecurityCommand runs the trusted system AWF binary with the runner's
// PATH restored after sudo applies secure_path.
const AWFLegacySecurityCommand = `sudo -E /usr/bin/env PATH="$PATH" /usr/local/bin/awf`

// AWFProxyLogsDir is the default directory for AWF proxy logs
const AWFProxyLogsDir FilePath = "/tmp/gh-aw/sandbox/firewall/logs"

// AWFProxyLogsDirExpr is the host-side AWF proxy logs path resolved by Actions expression.
const AWFProxyLogsDirExpr = GhAwRootDir + "/sandbox/firewall/logs"

// AWFProxyLogsDirShell is the host-side AWF proxy logs path resolved by shell env expansion.
const AWFProxyLogsDirShell = GhAwRootDirShell + "/sandbox/firewall/logs"

// AWFAuditDir is the directory for AWF audit files (policy-manifest.json, squid.conf, docker-compose.redacted.yml).
// These files are written by AWF when --audit-dir is specified and provide structured policy/configuration data
// needed by the `awf logs audit` command for enriching log entries with policy rule matching.
const AWFAuditDir FilePath = "/tmp/gh-aw/sandbox/firewall/audit"

// AWFAuditDirExpr is the host-side AWF audit dir path resolved by Actions expression.
const AWFAuditDirExpr = GhAwRootDir + "/sandbox/firewall/audit"

// AWFAuditDirShell is the host-side AWF audit dir path resolved by shell env expansion.
const AWFAuditDirShell = GhAwRootDirShell + "/sandbox/firewall/audit"

// PreAgentAuditFilePath is the path where the pre-agent workspace audit report is saved.
// The audit step runs after all pre-agent preparation (skills, agents, MCP servers) is
// complete, capturing a file listing of agent-related directories before the AI engine
// starts. This file is included in the agent artifact for post-run inspection.
const PreAgentAuditFilePath FilePath = "/tmp/gh-aw/pre-agent-audit.txt"

// AWFConfigFilePath is the path inside the /tmp/gh-aw tree where the AWF config file
// is copied so it can be included in the unified agent artifact.
// AWF itself reads the config from ${RUNNER_TEMP}/gh-aw/awf-config.json (host-side),
// but that path is outside the /tmp/gh-aw/ root used by all other artifact paths.
// A copy at this path is created before artifact upload so the config is available
// for post-run analysis without mixing path roots in the artifact.
const AWFConfigFilePath FilePath = "/tmp/gh-aw/awf-config.json"

// AWFConfigFilePathExpr is the host-side AWF config path resolved by Actions expression.
const AWFConfigFilePathExpr = GhAwRootDir + "/awf-config.json"

// AWFReflectFilePath is the path where the AWF API proxy /reflect response is persisted
// by the agent harness before exiting. It is co-located with other firewall observability
// data under /tmp/gh-aw/sandbox/firewall/ so the existing chmod and artifact-upload steps
// pick it up automatically.
const AWFReflectFilePath FilePath = "/tmp/gh-aw/sandbox/firewall/awf-reflect.json"

// AWFReflectFilePathExpr is the host-side AWF /reflect output path resolved by Actions expression.
const AWFReflectFilePathExpr = GhAwRootDir + "/sandbox/firewall/awf-reflect.json"

// FirewallAuditArtifactName is the legacy artifact name that was previously used for dedicated
// firewall audit log uploads. Firewall audit/observability logs are now included in the unified
// agent artifact. This constant is retained for backward compatibility when downloading artifacts
// from older workflow runs.
const FirewallAuditArtifactName = "firewall-audit-logs"

// AWFDefaultLogLevel is the default log level for AWF
const AWFDefaultLogLevel = "info"

// DefaultMCPGatewayContainer is the default container image for the MCP Gateway
const DefaultMCPGatewayContainer = "ghcr.io/github/gh-aw-mcpg"

// DefaultMCPGatewayPayloadDir is the default directory for MCP gateway payload files
// This directory is shared between the agent container and MCP gateway for large payload exchange
const DefaultMCPGatewayPayloadDir = "/tmp/gh-aw/mcp-payloads"

// DefaultMCPGatewayPayloadSizeThreshold is the default size threshold (in bytes) for storing payloads to disk.
// Payloads larger than this threshold are stored to disk, smaller ones are returned inline.
// Default: 524288 bytes (512KB) - chosen to accommodate typical MCP tool responses including
// GitHub API queries (list_commits, list_issues, etc.) without triggering disk storage.
// This prevents agent looping issues when payloadPath is not accessible in agent containers.
const DefaultMCPGatewayPayloadSizeThreshold = 524288

// DefaultFirewallRegistry is the container image registry for AWF (gh-aw-firewall) Docker images
const DefaultFirewallRegistry = "ghcr.io/github/gh-aw-firewall"

// DefaultNodeAlpineLTSImage is the default Node.js Alpine LTS container image for MCP servers
// Using node:lts-alpine provides the latest LTS version with minimal footprint
const DefaultNodeAlpineLTSImage = "node:lts-alpine"

// DefaultGhAwNodeImage is the published gh-aw Node container image used for
// JavaScript-based MCP servers that need Node.js and git plus workspace-mounted
// repository scripts at runtime.
const DefaultGhAwNodeImage = "ghcr.io/github/gh-aw-node"

// DefaultPythonAlpineLTSImage is the default Python Alpine LTS container image for MCP servers
// Using python:alpine provides the latest stable version with minimal footprint
const DefaultPythonAlpineLTSImage = "python:alpine"

// DefaultAlpineImage is the default minimal Alpine container image for running Go binaries
// Used for MCP servers that run statically-linked Go binaries like gh-aw mcp-server
const DefaultAlpineImage = "alpine:latest"

// DevModeGhAwImage is the Docker image tag for locally built gh-aw container in dev mode
// This image is built during workflow execution and includes the gh-aw binary and dependencies
const DevModeGhAwImage = "localhost/gh-aw:dev"

// GhAwRootDir is the base directory for gh-aw files on the runner.
// Uses ${{ runner.temp }} for compatibility with self-hosted runners that may not
// have write access to /opt/gh-aw/. The expression is resolved by GitHub Actions
// at workflow runtime before any step execution.
// Use this in YAML `with:` fields, `env:` value declarations, and Docker mounts
// where GitHub Actions template expressions are needed.
const GhAwRootDir = "${{ runner.temp }}/gh-aw"

// GhAwRootDirShell is the same path as GhAwRootDir but using the shell environment
// variable $RUNNER_TEMP instead of the GitHub Actions expression ${{ runner.temp }}.
// Use this inside shell `run:` blocks where the env var is already available.
// This is shorter than the Actions expression and avoids expression-length issues.
const GhAwRootDirShell = "${RUNNER_TEMP}/gh-aw"

// DefaultGhAwMount is the mount path for the gh-aw directory in containerized MCP servers
// The gh-aw binary and supporting files are mounted read-only from the runner temp directory.
// Uses the shell env var form since mounts are resolved in a shell context.
const DefaultGhAwMount = GhAwRootDirShell + ":" + GhAwRootDirShell + ":ro"

// GhCLIPath is the path to the gh CLI binary in the GitHub Actions runner.
const GhCLIPath = "/usr/bin/gh"

// DefaultGhBinaryMount is the mount path for the gh CLI binary in containerized MCP servers
// The gh CLI is required for agentic-workflows MCP server to run gh commands
const DefaultGhBinaryMount = GhCLIPath + ":" + GhCLIPath + ":ro"

// DefaultTmpGhAwMount is the mount path for temporary gh-aw files in containerized MCP servers
// Used for logs, cache, and other runtime data that needs read-write access
const DefaultTmpGhAwMount = "/tmp/gh-aw:/tmp/gh-aw:rw"

// DefaultWorkspaceMount is the mount path for the GitHub workspace directory in containerized MCP servers
// Security: Uses GITHUB_WORKSPACE environment variable instead of template expansion to prevent template injection
// The GITHUB_WORKSPACE environment variable is automatically set by GitHub Actions and passed to the MCP gateway
const DefaultWorkspaceMount = "\\${GITHUB_WORKSPACE}:\\${GITHUB_WORKSPACE}:rw"

// DefaultSafeOutputsMount is the mount path for safe-outputs runtime files
// (config.json, tools.json, outputs.jsonl, upload-artifacts/) in the runner temp directory.
const DefaultSafeOutputsMount = GhAwRootDirShell + "/safeoutputs:" + GhAwRootDirShell + "/safeoutputs:rw"

// Timeout constants using time.Duration for type safety and clear units

// DefaultAgenticWorkflowTimeout is the default timeout for the agentic execution step.
const DefaultAgenticWorkflowTimeout = 20 * time.Minute

// DefaultAgentJobTimeout is the default timeout for the generated agent job.
// It bounds the whole job (setup, execution and teardown steps) and is
// independent from DefaultAgenticWorkflowTimeout, which only bounds the
// agentic execution step.
const DefaultAgentJobTimeout = time.Hour

// DefaultDetectionJobTimeout is the default timeout for the generated threat
// detection job and its agentic execution step.
const DefaultDetectionJobTimeout = 10 * time.Minute

// DefaultToolTimeout is the default timeout for tool/MCP server operations
const DefaultToolTimeout = 60 * time.Second

// DefaultMCPStartupTimeout is the default timeout for MCP server startup
const DefaultMCPStartupTimeout = 120 * time.Second

// DefaultHTTPClientTimeout is the default timeout for internal HTTP clients
const DefaultHTTPClientTimeout = 30 * time.Second

// DefaultMaxAICredits is the default AI Credits budget enforced by the AWF API proxy.
const DefaultMaxAICredits int64 = 1000

// DefaultDetectionMaxAICredits is the default AI Credits budget enforced by the
// AWF API proxy for threat-detection runs.
const DefaultDetectionMaxAICredits int64 = 400

// DefaultMaxDailyAICredits is the default per-workflow daily AI Credits guardrail.
const DefaultMaxDailyAICredits = "5000"

// DefaultMaxRuns is the default AWF invocation cap enforced by the AWF API proxy.
const DefaultMaxRuns = 500

// DefaultMaxTurnCacheMisses is the default AWF consecutive cache-miss guardrail.
const DefaultMaxTurnCacheMisses = 5

// MCPSessionTimeoutMin is the minimum allowed value for engine.mcp.session-timeout (5 minutes).
const MCPSessionTimeoutMin = 5 * time.Minute

// MCPToolTimeoutMin is the minimum allowed value for engine.mcp.tool-timeout (10 seconds).
const MCPToolTimeoutMin = 10 * time.Second

// MCPToolTimeoutMax is the maximum allowed value for engine.mcp.tool-timeout (600 seconds).
const MCPToolTimeoutMax = 600 * time.Second

// DefaultActivationJobRunnerImage is the default runner image for activation and pre-activation jobs
const DefaultActivationJobRunnerImage = "ubuntu-slim"

// DefaultAllowedDomains defines the default localhost domains with port variations
// that are always allowed for Playwright browser automation
var DefaultAllowedDomains = []string{"localhost", "localhost:*", "127.0.0.1", "127.0.0.1:*"}

// SafeWorkflowEvents defines events that are considered safe and don't require permission checks
// workflow_run is intentionally excluded because it has HIGH security risks:
// - Privilege escalation (inherits permissions from triggering workflow)
// - Branch protection bypass (can execute on protected branches via unprotected branches)
// - Secret exposure (secrets available even when triggered by untrusted code)
var SafeWorkflowEvents = []string{"workflow_dispatch", "schedule"}

// PriorityStepFields defines the conventional field order for GitHub Actions workflow steps
// Fields appear in this order first, followed by remaining fields alphabetically
var PriorityStepFields = []string{"name", "id", "if", "run", "uses", "script", "env", "with"}

// PriorityJobFields defines the conventional field order for GitHub Actions workflow jobs
// Fields appear in this order first, followed by remaining fields alphabetically
var PriorityJobFields = []string{"name", "runs-on", "needs", "if", "permissions", "environment", "concurrency", "outputs", "env", "steps"}

// PriorityWorkflowFields defines the conventional field order for top-level GitHub Actions workflow frontmatter
// Fields appear in this order first, followed by remaining fields alphabetically
var PriorityWorkflowFields = []string{"on", "permissions", "if", "network", "imports", "safe-outputs", "steps"}

// IgnoredFrontmatterFields are fields that should be silently ignored during frontmatter validation
var IgnoredFrontmatterFields = []string{}

// SharedWorkflowForbiddenFields lists fields that cannot be used in shared/included workflows.
// These fields are only allowed in main workflows (workflows with an 'on' trigger field).
//
// This list is maintained in constants.go to enable easy mining by agents and automated tools.
// The compiler enforces these restrictions at compile time with clear error messages.
//
// Forbidden fields fall into these categories:
//   - Workflow triggers: on (defines it as a main workflow)
//   - Workflow execution: run-name, runs-on, if, timeout-minutes
//   - Workflow metadata: name, tracker-id, strict
//   - Workflow features: container, environment
//   - Access control: github-token
//
// The concurrency field is partially import-safe: shared workflows may contribute
// import-safe concurrency.group values and concurrency.job-discriminator values,
// but unsupported concurrency keys (for example cancel-in-progress) are rejected.
//
// The features field is partially import-safe: shared workflows may contribute
// the import-safe features.samples and features.intentional-failure flags, but
// other feature keys are rejected.
//
// All other fields defined in main_workflow_schema.json can be used in shared workflows
// and will be properly imported and merged when the shared workflow is imported.
var SharedWorkflowForbiddenFields = []string{
	"on",              // Trigger field - only for main workflows
	"container",       // Container configuration
	"environment",     // Deployment environment
	"github-token",    // GitHub token configuration
	"if",              // Conditional execution
	"name",            // Workflow name
	"run-name",        // Run display name
	"runs-on",         // Runner specification
	"strict",          // Strict mode
	"timeout-minutes", // Timeout in minutes
	"tracker-id",      // Tracker ID
}

// Repository directory path constants
//
// These constants define the conventional repository-relative directory paths
// used by gh-aw for GitHub Actions workflows, agents, and related configuration.

// GithubDir is the root .github directory prefix (with trailing slash).
// Use this for path prefix comparisons against workspace-relative paths.
const GithubDir = ".github/"

// WorkflowsDir is the GitHub Actions workflow directory path (without trailing slash).
// This is the canonical location for workflow markdown and compiled lock YAML files.
const WorkflowsDir = ".github/workflows"

// WorkflowsDirSlash is WorkflowsDir with a trailing slash.
// Use this for path prefix matching (e.g. strings.HasPrefix or strings.Contains).
const WorkflowsDirSlash = WorkflowsDir + "/"

// AgentsDir is the custom GitHub Copilot agent definitions directory (with trailing slash).
const AgentsDir = ".github/agents/"

// WorkflowsLockYmlGlob is the glob pattern for compiled workflow lock YAML files.
const WorkflowsLockYmlGlob = WorkflowsDirSlash + "*.lock.yml"

// WorkflowsLockYmlGitAttributesEntry is the .gitattributes entry that marks lock YAML
// files as generated.
const WorkflowsLockYmlGitAttributesEntry = WorkflowsLockYmlGlob + " linguist-generated=true"

// WorkflowsLockYmlGitAttributesEntryLegacy is the previous .gitattributes entry format that
// included an ineffective "merge=ours" attribute. It is only used to detect and clean up
// entries that gh-aw itself previously wrote, so we do not overwrite repository-owned policy.
const WorkflowsLockYmlGitAttributesEntryLegacy = WorkflowsLockYmlGlob + " linguist-generated=true merge=ours"

// Temporary runtime directory constants (/tmp/gh-aw tree)
//
// These constants define the /tmp/gh-aw directory layout used by the agent
// and engine harnesses during workflow execution. Paths here are always
// in the /tmp/gh-aw tree regardless of whether the runner uses RUNNER_TEMP.
// See also GhAwRootDir / GhAwRootDirShell for the host-side RUNNER_TEMP paths.

// TmpGhAwDir is the root /tmp/gh-aw directory (without trailing slash).
const TmpGhAwDir = "/tmp/gh-aw"

// TmpGhAwDirSlash is TmpGhAwDir with a trailing slash.
// Use for path prefix comparisons (e.g. strings.HasPrefix).
const TmpGhAwDirSlash = TmpGhAwDir + "/"

// TmpGhAwAgentDir is the agent working directory in the /tmp/gh-aw tree.
const TmpGhAwAgentDir = TmpGhAwDir + "/agent/"

// TmpGhAwAssetsDir is the directory the upload_assets job downloads the
// safe-outputs assets artifact into. It is the single source of truth shared by
// the download step (path:) and the upload_assets.cjs consumer script, so the
// two never disagree on where staged assets live.
const TmpGhAwAssetsDir = TmpGhAwDir + "/safeoutputs/assets"

// TmpGhAwAssetsDirSlash is TmpGhAwAssetsDir with a trailing slash.
const TmpGhAwAssetsDirSlash = TmpGhAwAssetsDir + "/"

// AgentStdioLogPath is the path for capturing agent standard I/O log output.
const AgentStdioLogPath = TmpGhAwDir + "/agent-stdio.log"

// AwPromptsFile is the runtime prompt file path populated by the setup action.
// Engine harnesses read this file to pass the compiled prompt to the AI engine.
const AwPromptsFile = TmpGhAwDir + "/aw-prompts/prompt.txt"

// AwPromptsFileExpr is the host-side prompt path in GitHub Actions expression form.
const AwPromptsFileExpr = GhAwRootDir + "/aw-prompts/prompt.txt"

// AwPromptsFileShell is the runtime prompt file path in shell env-var form for host-side paths.
const AwPromptsFileShell = GhAwRootDirShell + "/aw-prompts/prompt.txt"

// TmpMcpConfigDir is the mcp-config directory in the /tmp/gh-aw tree.
// Engines that require a writable MCP config directory (e.g. Codex) use this path.
const TmpMcpConfigDir = TmpGhAwDir + "/mcp-config"

// TmpMcpServersJsonPath is the MCP servers JSON config file in the /tmp tree.
// Used by engines that resolve the config through the writable /tmp path.
const TmpMcpServersJsonPath = TmpMcpConfigDir + "/mcp-servers.json"

// TmpMcpConfigLogsDir is the MCP config server log directory.
const TmpMcpConfigLogsDir = TmpMcpConfigDir + "/logs/"

// TmpMcpLogsDir is the MCP server logs root directory (with trailing slash).
const TmpMcpLogsDir = TmpGhAwDir + "/mcp-logs/"

// TmpMcpLogsSafeOutputsDir is the safe-outputs MCP server log directory.
const TmpMcpLogsSafeOutputsDir = TmpGhAwDir + "/mcp-logs/safeoutputs"

// TmpMcpLogsMount is the Docker volume mount spec for the MCP logs directory.
const TmpMcpLogsMount = TmpGhAwDir + "/mcp-logs:" + TmpGhAwDir + "/mcp-logs"

// TmpMcpScriptsLogsDir is the mcp-scripts server log directory (with trailing slash).
const TmpMcpScriptsLogsDir = TmpGhAwDir + "/mcp-scripts/logs/"

// TmpRepoMemoryDir is the repo-memory data directory (with trailing slash).
const TmpRepoMemoryDir = TmpGhAwDir + "/repo-memory/"

// TmpCommentMemoryDir is the comment-memory data directory (with trailing slash).
const TmpCommentMemoryDir = TmpGhAwDir + "/comment-memory/"

// TmpAwBundleGlob is the glob pattern for bundle files produced by the agent.
const TmpAwBundleGlob = TmpGhAwDir + "/aw-*.bundle"

// TmpAwPatchGlob is the glob pattern for patch files produced by the agent.
const TmpAwPatchGlob = TmpGhAwDir + "/aw-*.patch"

// TmpGeminiClientErrorGlob is the glob for Gemini client error JSON diagnostic files.
const TmpGeminiClientErrorGlob = TmpGhAwDir + "/gemini-client-error-*.json"

// TmpPiAgentDir is the Pi engine agent working directory.
const TmpPiAgentDir = TmpGhAwDir + "/pi-agent-dir"

// ThreatDetectionLogPath is the threat detection engine log file path.
const ThreatDetectionLogPath = TmpGhAwDir + "/threat-detection/detection.log"

// ThreatDetectionDir is the threat detection working directory.
const ThreatDetectionDir = TmpGhAwDir + "/threat-detection"

// ThreatDetectionResultPath is the structured verdict output file written by the
// external threat-detect binary (enabled by default; set features.gh-aw-detection to false
// to use the inline path). The binary writes
// a four-field JSON verdict to this path via --output; threat-detect conclude reads it.
const ThreatDetectionResultPath = TmpGhAwDir + "/threat-detection/detection_result.json"

// ThreatDetectionStepSummaryPath is the path used as the step-summary target inside the
// AWF sandbox for the external threat-detect binary. The sandbox cannot write to the
// runner's real GITHUB_STEP_SUMMARY path (the _runner_file_commands directory is not
// mounted), so threat-detect is invoked with --step-summary pointing here instead.
// A post-execution host step then appends this file to the real $GITHUB_STEP_SUMMARY.
const ThreatDetectionStepSummaryPath = TmpGhAwDir + "/step-summary.md"

// TmpProxyLogsDir is the DIFC proxy logs directory (with trailing slash).
const TmpProxyLogsDir = TmpGhAwDir + "/proxy-logs/"

// TmpProxyTLSDir is the proxy TLS certificates sub-directory (with trailing slash).
const TmpProxyTLSDir = TmpGhAwDir + "/proxy-logs/proxy-tls/"

// TmpProxyTLSCACert is the proxy TLS CA certificate file path.
const TmpProxyTLSCACert = TmpGhAwDir + "/proxy-logs/proxy-tls/ca.crt"

// TmpDIFCProxyTLSCACert is the DIFC proxy TLS CA certificate file path.
const TmpDIFCProxyTLSCACert = TmpGhAwDir + "/difc-proxy-tls/ca.crt"

// TmpAwMcpLogsDir is the aw-mcp server logs directory.
const TmpAwMcpLogsDir = TmpGhAwDir + "/aw-mcp/logs"

// TmpSandboxAgentLogsDir is the sandbox agent logs directory (with trailing slash).
const TmpSandboxAgentLogsDir = TmpGhAwDir + "/sandbox/agent/logs/"

// Shell and Actions expression form path constants
//
// These complement GhAwRootDirShell and GhAwRootDir for sub-paths commonly
// referenced in both shell run: blocks and GitHub Actions expression contexts.

// GhAwRootDirShellSlash is GhAwRootDirShell with a trailing slash.
// Use for path prefix matching in shell expressions (e.g. ${RUNNER_TEMP}/gh-aw/).
const GhAwRootDirShellSlash = GhAwRootDirShell + "/"

// ShellMcpConfigDir is the mcp-config directory in shell environment variable form.
const ShellMcpConfigDir = GhAwRootDirShell + "/mcp-config"

// ShellMcpServersJsonPath is the MCP servers JSON config file path in shell form.
// Used by engines that resolve the config via the host RUNNER_TEMP path.
const ShellMcpServersJsonPath = GhAwRootDirShell + "/mcp-config/mcp-servers.json"

// GhAwRootDirSlash is GhAwRootDir with a trailing slash (Actions expression form).
const GhAwRootDirSlash = GhAwRootDir + "/"

// McpServersJsonPathExpr is the MCP servers JSON config path in Actions expression form.
const McpServersJsonPathExpr = GhAwRootDir + "/mcp-config/mcp-servers.json"

// CodexMcpConfigTomlPath is the Codex MCP config TOML file path in Actions expression form.
const CodexMcpConfigTomlPath = GhAwRootDir + "/mcp-config/config.toml"

// System path constants
//
// Well-known host system paths used by CLI tools and shell completion.

// CopilotBinaryPath is the path to the Copilot CLI binary inside AWF containers.
const CopilotBinaryPath = "/usr/local/bin/copilot"

// BashCompletionDir is the system-wide bash completion directory.
const BashCompletionDir = "/etc/bash_completion.d"

// BashCompletionGhAwPath is the gh-aw bash completion file path.
const BashCompletionGhAwPath = BashCompletionDir + "/gh-aw"

// HomebrewPrefix is the default Homebrew installation prefix on macOS.
const HomebrewPrefix = "/opt/homebrew"

// UsrLocalPrefix is the standard /usr/local installation prefix.
const UsrLocalPrefix = "/usr/local"

// GetWorkflowDir returns the workflows directory path.
// Always uses forward slashes, which are required for git/GitHub paths.
// GH_AW_WORKFLOWS_DIR overrides the default; any OS-specific separators are normalized.
func GetWorkflowDir() string {
	if dir := os.Getenv("GH_AW_WORKFLOWS_DIR"); dir != "" { //nolint:osgetenvlibrary
		return filepath.ToSlash(dir)
	}
	return WorkflowsDir
}

// MaxSymlinkDepth limits recursive symlink resolution when fetching remote files.
// The GitHub Contents API doesn't follow symlinks in path components, so gh-aw
// resolves them manually. This constant caps recursion to prevent infinite loops
// when symlinks chain to each other.
const MaxSymlinkDepth = 5

// DefaultAllowedMemoryExtensions is the default list of allowed file extensions for cache-memory and repo-memory storage.
// An empty slice means all file extensions are allowed. When this is empty, the validation step is not emitted.
var DefaultAllowedMemoryExtensions = []string{}
