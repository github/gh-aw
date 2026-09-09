package constants

// FeatureFlag represents a feature flag identifier.
// This semantic type distinguishes feature flag names from arbitrary strings,
// making feature flag operations explicit and type-safe.
//
// Example usage:
//
//	const MCPGatewayFeatureFlag FeatureFlag = "mcp-gateway"
//	func IsFeatureEnabled(flag FeatureFlag) bool { ... }
type FeatureFlag string

// Feature flag identifiers
const (
	// MCPScriptsFeatureFlag is the name of the feature flag for mcp-scripts
	MCPScriptsFeatureFlag FeatureFlag = "mcp-scripts"
	// MCPGatewayFeatureFlag is the feature flag name for enabling MCP gateway
	MCPGatewayFeatureFlag FeatureFlag = "mcp-gateway"
	// DisableXPIAPromptFeatureFlag is the feature flag name for disabling XPIA prompt
	DisableXPIAPromptFeatureFlag FeatureFlag = "disable-xpia-prompt"
	// DIFCProxyFeatureFlag is the deprecated feature flag name for the DIFC proxy.
	// Deprecated: Use tools.github.integrity-proxy instead. The proxy is now enabled
	// by default when guard policies are configured. Set tools.github.integrity-proxy: false
	// to disable it. The codemod "features-difc-proxy-to-tools-github" migrates this flag.
	DIFCProxyFeatureFlag FeatureFlag = "difc-proxy"
	// CliProxyFeatureFlag enables the AWF CLI proxy sidecar.
	// When enabled, the compiler starts a difc-proxy on the host before AWF and
	// injects --difc-proxy-host and --difc-proxy-ca-cert into the AWF command,
	// giving the agent secure gh CLI access without exposing GITHUB_TOKEN.
	// The token is held in an mcpg DIFC proxy on the host, enforcing
	// guard policies and audit logging.
	//
	// Workflow frontmatter usage:
	//
	//	features:
	//	  cli-proxy: true
	CliProxyFeatureFlag FeatureFlag = "cli-proxy"
	// AwfDiagnosticLogsFeatureFlag enables AWF operational Docker diagnostics
	// collection on failure. When enabled, AWF collects capped container logs,
	// container exit codes, mount metadata, and sanitized compose config into
	// the diagnostics subdirectory of the firewall audit artifact.
	//
	// Workflow frontmatter usage:
	//
	//	features:
	//	  awf-diagnostic-logs: true
	AwfDiagnosticLogsFeatureFlag FeatureFlag = "awf-diagnostic-logs"
	// ByokCopilotFeatureFlag is a deprecated legacy feature flag for Copilot BYOK mode.
	// Deprecated: Copilot now enables BYOK behavior by default, so this flag has no effect.
	//
	// The compiler always:
	//   - injects a dummy COPILOT_API_KEY into the agent env to trigger AWF BYOK runtime behavior
	//   - enables cli-proxy behavior for copilot workflows (unless tools.github.mode overrides)
	//   - installs the latest Copilot CLI version (un-pinned)
	//
	// Workflow frontmatter usage:
	//
	//	features:
	//	  byok-copilot: true
	ByokCopilotFeatureFlag FeatureFlag = "byok-copilot"
	// IntegrityReactionsFeatureFlag enables reaction-based integrity promotion/demotion
	// in the MCPG allow-only policy. When enabled, the compiler injects
	// endorsement-reactions and disapproval-reactions fields into the allow-only policy.
	// Requires MCPG >= v0.2.18.
	//
	// Workflow frontmatter usage:
	//
	//	features:
	//	  integrity-reactions: true
	IntegrityReactionsFeatureFlag FeatureFlag = "integrity-reactions"
	// GroupConcurrencyQueueFeatureFlag controls whether compiler-generated group
	// concurrency blocks include queue: max.
	//
	// Workflow frontmatter usage:
	//
	//	features:
	//	  group-concurrency-queue: false
	GroupConcurrencyQueueFeatureFlag FeatureFlag = "group-concurrency-queue"
	// DangerouslyDisableSandboxAgentFeatureFlag is required to allow sandbox.agent: false.
	// Without this flag, setting sandbox.agent to false raises a validation error.
	// This flag is intentionally named with "dangerously" to make the security
	// implications explicit and visible in the workflow frontmatter.
	//
	// Workflow frontmatter usage:
	//
	//	features:
	//	  dangerously-disable-sandbox-agent: true
	DangerouslyDisableSandboxAgentFeatureFlag FeatureFlag = "dangerously-disable-sandbox-agent"
	// GHAWDetectionFeatureFlag controls the external threat-detect binary detection path.
	// The external detector is enabled by default. Set this flag to false to use
	// the legacy inline engine execution path.
	// The binary version is resolved at runtime via DefaultThreatDetectVersion in version_constants.go.
	//
	// Workflow frontmatter usage:
	//
	//	features:
	//	  gh-aw-detection: true
	GHAWDetectionFeatureFlag FeatureFlag = "gh-aw-detection"
)
