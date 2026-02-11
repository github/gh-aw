package constants

import (
	"fmt"
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
// Benefits of semantic type aliases:
//   - Self-documenting: The type name explains the purpose
//   - Type safety: Prevents mixing different concepts with the same underlying type
//   - Clear intent: Signals to readers what the value represents
//   - Easy refactoring: Can change implementation without affecting API
//
// See scratchpad/go-type-patterns.md for detailed guidance on type patterns.

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

// String returns the string representation of the line length
func (l LineLength) String() string {
	return fmt.Sprintf("%d", l)
}

// IsValid returns true if the line length is positive
func (l LineLength) IsValid() bool {
	return l > 0
}

// Version represents a software version string.
// This semantic type distinguishes version strings from arbitrary strings,
// enabling future validation logic (e.g., semver parsing) and making
// version requirements explicit in function signatures.
//
// Example usage:
//
//	const DefaultCopilotVersion Version = "0.0.369"
//	func InstallTool(name string, version Version) error { ... }
type Version string

// String returns the string representation of the version
func (v Version) String() string {
	return string(v)
}

// IsValid returns true if the version is non-empty
func (v Version) IsValid() bool {
	return len(v) > 0
}

// FeatureFlag represents a feature flag identifier.
// This semantic type distinguishes feature flag names from arbitrary strings,
// making feature flag operations explicit and type-safe.
//
// Example usage:
//
//	const MCPGatewayFeatureFlag FeatureFlag = "mcp-gateway"
//	func IsFeatureEnabled(flag FeatureFlag) bool { ... }
type FeatureFlag string

// String returns the string representation of the feature flag
func (f FeatureFlag) String() string {
	return string(f)
}

// IsValid returns true if the feature flag is non-empty
func (f FeatureFlag) IsValid() bool {
	return len(f) > 0
}

// URL represents a URL string.
// This semantic type distinguishes URLs from arbitrary strings,
// making URL parameters explicit and enabling future validation logic.
//
// Example usage:
//
//	const DefaultMCPRegistryURL URL = "https://api.mcp.github.com/v0.1"
//	func FetchFromRegistry(url URL) error { ... }
type URL string

// String returns the string representation of the URL
func (u URL) String() string {
	return string(u)
}

// IsValid returns true if the URL is non-empty
func (u URL) IsValid() bool {
	return len(u) > 0
}

// ModelName represents an AI model name identifier.
// This semantic type distinguishes model names from arbitrary strings,
// making model selection explicit in function signatures.
//
// Example usage:
//
//	const DefaultCopilotDetectionModel ModelName = "gpt-5-mini"
//	func ExecuteWithModel(model ModelName) error { ... }
type ModelName string

// String returns the string representation of the model name
func (m ModelName) String() string {
	return string(m)
}

// IsValid returns true if the model name is non-empty
func (m ModelName) IsValid() bool {
	return len(m) > 0
}

// JobName represents a GitHub Actions job identifier.
// This semantic type distinguishes job names from arbitrary strings,
// preventing mixing of job identifiers with other string types.
//
// Example usage:
//
//	const AgentJobName JobName = "agent"
//	func GetJob(name JobName) (*Job, error) { ... }
type JobName string

// String returns the string representation of the job name
func (j JobName) String() string {
	return string(j)
}

// IsValid returns true if the job name is non-empty
func (j JobName) IsValid() bool {
	return len(j) > 0
}

// StepID represents a GitHub Actions step identifier.
// This semantic type distinguishes step IDs from arbitrary strings,
// preventing mixing of step identifiers with job names or other strings.
//
// Example usage:
//
//	const CheckMembershipStepID StepID = "check_membership"
//	func GetStep(id StepID) (*Step, error) { ... }
type StepID string

// String returns the string representation of the step ID
func (s StepID) String() string {
	return string(s)
}

// IsValid returns true if the step ID is non-empty
func (s StepID) IsValid() bool {
	return len(s) > 0
}

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
	return len(c) > 0
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
	return len(w) > 0
}

// EngineName represents an AI engine name identifier (copilot, claude, codex, custom).
// This semantic type distinguishes engine names from arbitrary strings,
// making engine selection explicit and type-safe.
//
// Example usage:
//
//	const CopilotEngine EngineName = "copilot"
//	func SetEngine(engine EngineName) error { ... }
type EngineName string

// String returns the string representation of the engine name
func (e EngineName) String() string {
	return string(e)
}

// IsValid returns true if the engine name is non-empty
func (e EngineName) IsValid() bool {
	return len(e) > 0
}

// DocURL represents a documentation URL for error messages and help text.
// This semantic type distinguishes documentation URLs from arbitrary URLs,
// making documentation references explicit and centralized for easier maintenance.
//
// Example usage:
//
//	const DocsEnginesURL DocURL = "https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/engines.md"
//	func formatError(msg string, docURL DocURL) string { ... }
type DocURL string

// String returns the string representation of the documentation URL
func (d DocURL) String() string {
	return string(d)
}

// IsValid returns true if the documentation URL is non-empty
func (d DocURL) IsValid() bool {
	return len(d) > 0
}

// Documentation URLs for validation error messages.
// These URLs point to the relevant documentation pages that help users
// understand and resolve validation errors.
const (
	// DocsEnginesURL is the documentation URL for engine configuration
	DocsEnginesURL DocURL = "https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/engines.md"

	// DocsToolsURL is the documentation URL for tools and MCP server configuration
	DocsToolsURL DocURL = "https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/tools.md"

	// DocsGitHubToolsURL is the documentation URL for GitHub tools configuration
	DocsGitHubToolsURL DocURL = "https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/tools.md#github-tools-github"

	// DocsPermissionsURL is the documentation URL for GitHub permissions configuration
	DocsPermissionsURL DocURL = "https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/permissions.md"

	// DocsNetworkURL is the documentation URL for network configuration
	DocsNetworkURL DocURL = "https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/network.md"

	// DocsSandboxURL is the documentation URL for sandbox configuration
	DocsSandboxURL DocURL = "https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/sandbox.md"
)

// MaxExpressionLineLength is the maximum length for a single line expression before breaking into multiline.
const MaxExpressionLineLength LineLength = 120

// ExpressionBreakThreshold is the threshold for breaking long lines at logical points.
const ExpressionBreakThreshold LineLength = 100

// Network port constants
//
// These constants define standard network port values used throughout the codebase
// for MCP servers, gateway services, and validation ranges.

const (
	// DefaultMCPGatewayPort is the default port for the MCP gateway HTTP service
	DefaultMCPGatewayPort = 80

	// DefaultMCPServerPort is the default port for MCP servers (safe-inputs server)
	DefaultMCPServerPort = 3000

	// DefaultMCPInspectorPort is the default port for the MCP inspector (safe-outputs server)
	DefaultMCPInspectorPort = 3001

	// MinNetworkPort is the minimum valid network port number
	MinNetworkPort = 1

	// MaxNetworkPort is the maximum valid network port number
	MaxNetworkPort = 65535
)

// DefaultMCPRegistryURL is the default MCP registry URL.
const DefaultMCPRegistryURL URL = "https://api.mcp.github.com/v0.1"

// GitHubCopilotMCPDomain is the domain for the hosted GitHub MCP server.
// Used when github tool is configured with mode: remote.
const GitHubCopilotMCPDomain = "api.githubcopilot.com"

// DefaultClaudeCodeVersion is the default version of the Claude Code CLI.
const DefaultClaudeCodeVersion Version = "2.1.39"

// DefaultCopilotVersion is the default version of the GitHub Copilot CLI.
//
// WARNING: UPGRADING COPILOT CLI REQUIRES A FULL INTEGRATION TEST RUN TO ENSURE COMPATIBILITY.
const DefaultCopilotVersion Version = "0.0.406"

// DefaultCopilotDetectionModel is the default model for the Copilot engine when used in the detection job
// Updated to gpt-5.1-codex-mini after gpt-5-mini deprecation on 2026-01-17
const DefaultCopilotDetectionModel ModelName = "gpt-5.1-codex-mini"

// Environment variable names for model configuration
const (
	// EnvVarModelAgentCopilot configures the default Copilot model for agent execution
	EnvVarModelAgentCopilot = "GH_AW_MODEL_AGENT_COPILOT"
	// EnvVarModelAgentClaude configures the default Claude model for agent execution
	EnvVarModelAgentClaude = "GH_AW_MODEL_AGENT_CLAUDE"
	// EnvVarModelAgentCodex configures the default Codex model for agent execution
	EnvVarModelAgentCodex = "GH_AW_MODEL_AGENT_CODEX"
	// EnvVarModelAgentCustom configures the default Custom model for agent execution
	EnvVarModelAgentCustom = "GH_AW_MODEL_AGENT_CUSTOM"
	// EnvVarModelDetectionCopilot configures the default Copilot model for detection
	EnvVarModelDetectionCopilot = "GH_AW_MODEL_DETECTION_COPILOT"
	// EnvVarModelDetectionClaude configures the default Claude model for detection
	EnvVarModelDetectionClaude = "GH_AW_MODEL_DETECTION_CLAUDE"
	// EnvVarModelDetectionCodex configures the default Codex model for detection
	EnvVarModelDetectionCodex = "GH_AW_MODEL_DETECTION_CODEX"
)

// DefaultCodexVersion is the default version of the OpenAI Codex CLI
const DefaultCodexVersion Version = "0.98.0"

// DefaultGitHubMCPServerVersion is the default version of the GitHub MCP server Docker image
const DefaultGitHubMCPServerVersion Version = "v0.30.3"

// DefaultFirewallVersion is the default version of the gh-aw-firewall (AWF) binary
const DefaultFirewallVersion Version = "v0.14.0"

// DefaultMCPGatewayVersion is the default version of the MCP Gateway (gh-aw-mcpg) Docker image
const DefaultMCPGatewayVersion Version = "v0.1.0"

// DefaultMCPGatewayContainer is the default container image for the MCP Gateway
const DefaultMCPGatewayContainer = "ghcr.io/github/gh-aw-mcpg"

// DefaultMCPGatewayPayloadDir is the default directory for MCP gateway payload files
// This directory is shared between the agent container and MCP gateway for large payload exchange
const DefaultMCPGatewayPayloadDir = "/tmp/gh-aw/mcp-payloads"

// DefaultFirewallRegistry is the container image registry for AWF (gh-aw-firewall) Docker images
const DefaultFirewallRegistry = "ghcr.io/github/gh-aw-firewall"

// DefaultSerenaMCPServerContainer is the default container image for the Serena MCP server
const DefaultSerenaMCPServerContainer = "ghcr.io/github/serena-mcp-server"

// OraiosSerenaContainer is the Oraios Serena MCP server container image (legacy)
const OraiosSerenaContainer = "ghcr.io/oraios/serena"

// SerenaLanguageSupport defines the supported languages for each Serena container image
var SerenaLanguageSupport = map[string][]string{
	DefaultSerenaMCPServerContainer: {
		"go", "typescript", "javascript", "python", "java", "rust", "csharp",
		"cpp", "c", "ruby", "php", "bash", "swift", "kotlin", "scala",
		"haskell", "elixir", "erlang", "clojure", "lua", "perl", "r",
		"dart", "julia", "fortran", "nix", "rego", "terraform", "yaml",
		"markdown", "zig", "elm",
	},
	OraiosSerenaContainer: {
		"go", "typescript", "javascript", "python", "java", "rust", "csharp",
		"cpp", "c", "ruby", "php", "bash", "swift", "kotlin", "scala",
		"haskell", "elixir", "erlang", "clojure", "lua", "perl", "r",
		"dart", "julia", "fortran", "nix", "rego", "terraform", "yaml",
		"markdown", "zig", "elm",
	},
}

// DefaultSandboxRuntimeVersion is the default version of the @anthropic-ai/sandbox-runtime package (SRT)
const DefaultSandboxRuntimeVersion Version = "0.0.37"

// DefaultPlaywrightMCPVersion is the default version of the @playwright/mcp package
const DefaultPlaywrightMCPVersion Version = "0.0.64"

// DefaultPlaywrightBrowserVersion is the default version of the Playwright browser Docker image
const DefaultPlaywrightBrowserVersion Version = "v1.58.2"

// DefaultMCPSDKVersion is the default version of the @modelcontextprotocol/sdk package
const DefaultMCPSDKVersion Version = "1.24.0"

// DefaultGitHubScriptVersion is the default version of the actions/github-script action
const DefaultGitHubScriptVersion Version = "v8"

// DefaultBunVersion is the default version of Bun for runtime setup
const DefaultBunVersion Version = "1.1"

// DefaultNodeVersion is the default version of Node.js for runtime setup
const DefaultNodeVersion Version = "24"

// DefaultNodeAlpineLTSImage is the default Node.js Alpine LTS container image for MCP servers
// Using node:lts-alpine provides the latest LTS version with minimal footprint
const DefaultNodeAlpineLTSImage = "node:lts-alpine"

// DefaultPythonAlpineLTSImage is the default Python Alpine LTS container image for MCP servers
// Using python:alpine provides the latest stable version with minimal footprint
const DefaultPythonAlpineLTSImage = "python:alpine"

// DefaultAlpineImage is the default minimal Alpine container image for running Go binaries
// Used for MCP servers that run statically-linked Go binaries like gh-aw mcp-server
const DefaultAlpineImage = "alpine:latest"

// DevModeGhAwImage is the Docker image tag for locally built gh-aw container in dev mode
// This image is built during workflow execution and includes the gh-aw binary and dependencies
const DevModeGhAwImage = "localhost/gh-aw:dev"

// DefaultGhAwMount is the mount path for the gh-aw directory in containerized MCP servers
// The gh-aw binary and supporting files are mounted read-only from /opt/gh-aw
const DefaultGhAwMount = "/opt/gh-aw:/opt/gh-aw:ro"

// DefaultGhBinaryMount is the mount path for the gh CLI binary in containerized MCP servers
// The gh CLI is required for agentic-workflows MCP server to run gh commands
const DefaultGhBinaryMount = "/usr/bin/gh:/usr/bin/gh:ro"

// DefaultTmpGhAwMount is the mount path for temporary gh-aw files in containerized MCP servers
// Used for logs, cache, and other runtime data that needs read-write access
const DefaultTmpGhAwMount = "/tmp/gh-aw:/tmp/gh-aw:rw"

// DefaultWorkspaceMount is the mount path for the GitHub workspace directory in containerized MCP servers
// Uses GitHub Actions expression syntax which expands to the actual workspace path at runtime
// This is required when MCP servers need read-write access to repository files
const DefaultWorkspaceMount = "${{ github.workspace }}:${{ github.workspace }}:rw"

// DefaultPythonVersion is the default version of Python for runtime setup
const DefaultPythonVersion Version = "3.12"

// DefaultRubyVersion is the default version of Ruby for runtime setup
const DefaultRubyVersion Version = "3.3"

// DefaultDotNetVersion is the default version of .NET for runtime setup
const DefaultDotNetVersion Version = "8.0"

// DefaultJavaVersion is the default version of Java for runtime setup
const DefaultJavaVersion Version = "21"

// DefaultElixirVersion is the default version of Elixir for runtime setup
const DefaultElixirVersion Version = "1.17"

// DefaultGoVersion is the default version of Go for runtime setup
const DefaultGoVersion Version = "1.25"

// DefaultHaskellVersion is the default version of GHC for runtime setup
const DefaultHaskellVersion Version = "9.10"

// DefaultDenoVersion is the default version of Deno for runtime setup
const DefaultDenoVersion Version = "2.x"

// Timeout constants using time.Duration for type safety and clear units

// DefaultAgenticWorkflowTimeout is the default timeout for agentic workflow execution
const DefaultAgenticWorkflowTimeout = 20 * time.Minute

// DefaultToolTimeout is the default timeout for tool/MCP server operations
const DefaultToolTimeout = 60 * time.Second

// DefaultMCPStartupTimeout is the default timeout for MCP server startup
const DefaultMCPStartupTimeout = 120 * time.Second

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

// AllowedExpressions contains the GitHub Actions expressions that can be used in workflow markdown content
// see https://docs.github.com/en/actions/reference/workflows-and-actions/contexts#github-context
var AllowedExpressions = []string{
	"github.event.after",
	"github.event.before",
	"github.event.check_run.id",
	"github.event.check_suite.id",
	"github.event.comment.id",
	"github.event.deployment.id",
	"github.event.deployment_status.id",
	"github.event.head_commit.id",
	"github.event.installation.id",
	"github.event.issue.number",
	"github.event.discussion.number",
	"github.event.pull_request.number",
	"github.event.milestone.number",
	"github.event.check_run.number",
	"github.event.check_suite.number",
	"github.event.workflow_job.run_id",
	"github.event.workflow_run.number",
	"github.event.label.id",
	"github.event.milestone.id",
	"github.event.organization.id",
	"github.event.page.id",
	"github.event.project.id",
	"github.event.project_card.id",
	"github.event.project_column.id",
	"github.event.release.assets[0].id",
	"github.event.release.id",
	"github.event.release.tag_name",
	"github.event.repository.id",
	"github.event.repository.default_branch",
	"github.event.review.id",
	"github.event.review_comment.id",
	"github.event.sender.id",
	"github.event.workflow_run.id",
	"github.event.workflow_run.conclusion",
	"github.event.workflow_run.html_url",
	"github.event.workflow_run.head_sha",
	"github.event.workflow_run.run_number",
	"github.event.workflow_run.event",
	"github.event.workflow_run.status",
	"github.event.issue.state",
	"github.event.issue.title",
	"github.event.pull_request.state",
	"github.event.pull_request.title",
	"github.event.discussion.title",
	"github.event.discussion.category.name",
	"github.event.release.name",
	"github.event.workflow_job.id",
	"github.event.deployment.environment",
	"github.event.pull_request.head.sha",
	"github.event.pull_request.base.sha",
	"github.actor",
	"github.job",
	"github.owner",
	"github.repository",
	"github.repository_owner",
	"github.run_id",
	"github.run_number",
	"github.server_url",
	"github.workflow",
	"github.workspace",
} // needs., steps. already allowed

// DangerousPropertyNames contains JavaScript built-in property names that are blocked
// in GitHub Actions expressions to prevent prototype pollution and traversal attacks.
// This list matches the DANGEROUS_PROPS list in actions/setup/js/runtime_import.cjs
// See PR #14826 for context on these security measures.
var DangerousPropertyNames = []string{
	"constructor",
	"__proto__",
	"prototype",
	"__defineGetter__",
	"__defineSetter__",
	"__lookupGetter__",
	"__lookupSetter__",
	"hasOwnProperty",
	"isPrototypeOf",
	"propertyIsEnumerable",
	"toString",
	"valueOf",
	"toLocaleString",
}

const AgentJobName JobName = "agent"
const ActivationJobName JobName = "activation"
const PreActivationJobName JobName = "pre_activation"
const DetectionJobName JobName = "detection"
const SafeOutputArtifactName = "safe-output"
const AgentOutputArtifactName = "agent-output"

// AgentOutputFilename is the filename of the agent output JSON file
const AgentOutputFilename = "agent_output.json"

// SafeOutputsMCPServerID is the identifier for the safe-outputs MCP server
const SafeOutputsMCPServerID = "safeoutputs"

// SafeInputsMCPServerID is the identifier for the safe-inputs MCP server
const SafeInputsMCPServerID = "safeinputs"

// SafeInputsMCPVersion is the version of the safe-inputs MCP server
const SafeInputsMCPVersion = "1.0.0"

// AgenticWorkflowsMCPServerID is the identifier for the agentic-workflows MCP server
const AgenticWorkflowsMCPServerID = "agenticworkflows"

// Feature flag identifiers
const (
	// SafeInputsFeatureFlag is the name of the feature flag for safe-inputs
	SafeInputsFeatureFlag FeatureFlag = "safe-inputs"
	// MCPGatewayFeatureFlag is the feature flag name for enabling MCP gateway
	MCPGatewayFeatureFlag FeatureFlag = "mcp-gateway"
	// SandboxRuntimeFeatureFlag is the feature flag name for sandbox runtime
	SandboxRuntimeFeatureFlag FeatureFlag = "sandbox-runtime"
	// DangerousPermissionsWriteFeatureFlag is the feature flag name for allowing write permissions
	DangerousPermissionsWriteFeatureFlag FeatureFlag = "dangerous-permissions-write"
)

// Step IDs for pre-activation job
const CheckMembershipStepID StepID = "check_membership"
const CheckStopTimeStepID StepID = "check_stop_time"
const CheckSkipIfMatchStepID StepID = "check_skip_if_match"
const CheckSkipIfNoMatchStepID StepID = "check_skip_if_no_match"
const CheckCommandPositionStepID StepID = "check_command_position"

// Output names for pre-activation job steps
const IsTeamMemberOutput = "is_team_member"
const StopTimeOkOutput = "stop_time_ok"
const SkipCheckOkOutput = "skip_check_ok"
const SkipNoMatchCheckOkOutput = "skip_no_match_check_ok"
const CommandPositionOkOutput = "command_position_ok"
const MatchedCommandOutput = "matched_command"
const ActivatedOutput = "activated"

// Agentic engine name constants using EngineName type for type safety
const (
	// CopilotEngine is the GitHub Copilot engine identifier
	CopilotEngine EngineName = "copilot"
	// ClaudeEngine is the Anthropic Claude engine identifier
	ClaudeEngine EngineName = "claude"
	// CodexEngine is the OpenAI Codex engine identifier
	CodexEngine EngineName = "codex"
	// CustomEngine is the custom engine identifier
	CustomEngine EngineName = "custom"
)

// AgenticEngines lists all supported agentic engine names
// Note: This remains a string slice for backward compatibility with existing code
var AgenticEngines = []string{string(ClaudeEngine), string(CodexEngine), string(CopilotEngine)}

// EngineOption represents a selectable AI engine with its display metadata and secret configuration
type EngineOption struct {
	Value       string
	Label       string
	Description string
	SecretName  string // The name of the secret required for this engine (e.g., "COPILOT_GITHUB_TOKEN")
	EnvVarName  string // Alternative environment variable name if different from SecretName (optional)
	KeyURL      string // URL where users can obtain their API key (empty for engines with special setup like Copilot)
}

// EngineOptions provides the list of available AI engines for user selection
var EngineOptions = []EngineOption{
	{string(CopilotEngine), "GitHub Copilot", "GitHub Copilot CLI with agent support", "COPILOT_GITHUB_TOKEN", "", ""},
	{string(ClaudeEngine), "Claude", "Anthropic Claude Code coding agent", "ANTHROPIC_API_KEY", "", "https://console.anthropic.com/settings/keys"},
	{string(CodexEngine), "Codex", "OpenAI Codex/GPT engine", "OPENAI_API_KEY", "", "https://platform.openai.com/api-keys"},
}

// GetEngineOption returns the EngineOption for the given engine value, or nil if not found
func GetEngineOption(engineValue string) *EngineOption {
	for i := range EngineOptions {
		if EngineOptions[i].Value == engineValue {
			return &EngineOptions[i]
		}
	}
	return nil
}

// DefaultReadOnlyGitHubTools defines the default read-only GitHub MCP tools.
// This list is shared by both local (Docker) and remote (hosted) modes.
// Currently, both modes use identical tool lists, but this may diverge in the future
// if different modes require different default tool sets.
var DefaultReadOnlyGitHubTools = []string{
	// actions
	"download_workflow_run_artifact",
	"get_job_logs",
	"get_workflow_run",
	"get_workflow_run_logs",
	"get_workflow_run_usage",
	"list_workflow_jobs",
	"list_workflow_run_artifacts",
	"list_workflow_runs",
	"list_workflows",
	// code security
	"get_code_scanning_alert",
	"list_code_scanning_alerts",
	// context
	"get_me",
	// dependabot
	"get_dependabot_alert",
	"list_dependabot_alerts",
	// discussions
	"get_discussion",
	"get_discussion_comments",
	"list_discussion_categories",
	"list_discussions",
	// issues
	"issue_read",
	"list_issues",
	"search_issues",
	// notifications
	"get_notification_details",
	"list_notifications",
	// organizations
	"search_orgs",
	// labels
	"get_label",
	"list_label",
	// prs
	"get_pull_request",
	"get_pull_request_comments",
	"get_pull_request_diff",
	"get_pull_request_files",
	"get_pull_request_reviews",
	"get_pull_request_status",
	"list_pull_requests",
	"pull_request_read",
	"search_pull_requests",
	// repos
	"get_commit",
	"get_file_contents",
	"get_tag",
	"list_branches",
	"list_commits",
	"list_tags",
	"search_code",
	"search_repositories",
	// secret protection
	"get_secret_scanning_alert",
	"list_secret_scanning_alerts",
	// users
	"search_users",
	// additional unique tools (previously duplicated block extras)
	"get_latest_release",
	"get_pull_request_review_comments",
	"get_release_by_tag",
	"list_issue_types",
	"list_releases",
	"list_starred_repositories",
}

// DefaultGitHubToolsLocal defines the default read-only GitHub MCP tools for local (Docker) mode.
// Currently identical to DefaultReadOnlyGitHubTools. Kept separate for backward compatibility
// and to allow future divergence if local mode requires different defaults.
var DefaultGitHubToolsLocal = DefaultReadOnlyGitHubTools

// DefaultGitHubToolsRemote defines the default read-only GitHub MCP tools for remote (hosted) mode.
// Currently identical to DefaultReadOnlyGitHubTools. Kept separate for backward compatibility
// and to allow future divergence if remote mode requires different defaults.
var DefaultGitHubToolsRemote = DefaultReadOnlyGitHubTools

// DefaultGitHubTools is deprecated. Use DefaultGitHubToolsLocal or DefaultGitHubToolsRemote instead.
// Kept for backward compatibility and defaults to local mode tools.
var DefaultGitHubTools = DefaultGitHubToolsLocal

// DefaultBashTools defines basic bash commands that should be available by default when bash is enabled
var DefaultBashTools = []string{
	"echo",
	"ls",
	"pwd",
	"cat",
	"head",
	"tail",
	"grep",
	"wc",
	"sort",
	"uniq",
	"date",
	"yq",
}

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
// NOTE: This is now empty as description and applyTo are properly validated by the schema
var IgnoredFrontmatterFields = []string{}

// SharedWorkflowForbiddenFields lists fields that cannot be used in shared/included workflows.
// These fields are only allowed in main workflows (workflows with an 'on' trigger field).
//
// This list is maintained in constants.go to enable easy mining by agents and automated tools.
// The compiler enforces these restrictions at compile time with clear error messages.
//
// Forbidden fields fall into these categories:
//   - Workflow triggers: on (defines it as a main workflow)
//   - Workflow execution: command, run-name, runs-on, concurrency, if, timeout-minutes, timeout_minutes
//   - Workflow metadata: name, tracker-id, strict
//   - Workflow features: container, env, environment, sandbox, features
//   - Access control: roles, github-token
//
// All other fields defined in main_workflow_schema.json can be used in shared workflows
// and will be properly imported and merged when the shared workflow is imported.
var SharedWorkflowForbiddenFields = []string{
	"on",              // Trigger field - only for main workflows
	"command",         // Command for workflow execution
	"concurrency",     // Concurrency control
	"container",       // Container configuration
	"env",             // Environment variables
	"environment",     // Deployment environment
	"features",        // Feature flags
	"github-token",    // GitHub token configuration
	"if",              // Conditional execution
	"name",            // Workflow name
	"roles",           // Role requirements
	"run-name",        // Run display name
	"runs-on",         // Runner specification
	"sandbox",         // Sandbox configuration
	"strict",          // Strict mode
	"timeout-minutes", // Timeout in minutes
	"timeout_minutes", // Timeout in minutes (underscore variant)
	"tracker-id",      // Tracker ID
}

func GetWorkflowDir() string {
	return filepath.Join(".github", "workflows")
}
