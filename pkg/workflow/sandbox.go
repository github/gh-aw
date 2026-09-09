// This file provides sandbox configuration for agentic workflows.
//
// This file handles:
//   - Sandbox type definitions (AWF, SRT)
//   - Sandbox configuration structures and parsing
//   - Sandbox runtime config generation
//
// # Validation Functions
//
// Domain-specific validation functions for sandbox configuration are located in
// sandbox_validation.go following the validation architecture pattern.
// See validation.go for the validation architecture documentation.

package workflow

import (
	"slices"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
)

var sandboxLog = logger.New("workflow:sandbox")

// SandboxType represents the type of sandbox to use
type SandboxType string

const (
	SandboxTypeAWF     SandboxType = "awf"     // Uses AWF (Agent Workflow Firewall)
	SandboxTypeDefault SandboxType = "default" // Alias for AWF (backward compat)
)

const (
	defaultAgentWorkspaceWritePath = "/tmp/gh-aw/agent"
	defaultAgentLogsWritePath      = "/tmp/gh-aw/sandbox/agent/logs"
)

// SandboxConfig represents the top-level sandbox configuration from front matter
// New format: { agent: "awf"|"srt"|{type, config}, mcp: {port, command, ...} }
// Legacy format: "default"|"sandbox-runtime" or { type, config }
type SandboxConfig struct {
	// New fields
	Agent *AgentSandboxConfig      `yaml:"agent,omitempty"` // Agent sandbox configuration
	MCP   *MCPGatewayRuntimeConfig `yaml:"mcp,omitempty"`   // MCP gateway configuration

	// Legacy fields (for backward compatibility)
	Type   SandboxType           `yaml:"type,omitempty"`   // Sandbox type: "default" or "sandbox-runtime"
	Config *SandboxRuntimeConfig `yaml:"config,omitempty"` // Custom SRT config (optional)
}

// AgentRuntime represents the container runtime to use for the agent container.
type AgentRuntime string

const (
	// AgentRuntimeDocker is the default runtime: Docker with a rootless AWF and
	// network isolation. It is also the profile used when runtime is omitted.
	AgentRuntimeDocker AgentRuntime = "docker"

	// AgentRuntimeDockerSudoIptables runs Docker with a privileged AWF, legacy
	// iptables networking, and host/service access. It is the only profile where
	// sandbox.agent.allow-host-ports and GitHub Actions services: connectivity apply.
	AgentRuntimeDockerSudoIptables AgentRuntime = "docker-sudo-iptables"

	// AgentRuntimeGVisor runs the agent container under gVisor's runsc runtime for
	// additional kernel-level isolation. The compiler emits the privileged
	// host-level installation steps that runsc requires.
	AgentRuntimeGVisor AgentRuntime = "gvisor"

	// AgentRuntimeDockerSbx runs the agent inside a Docker sbx microVM with
	// hypervisor-level isolation (KVM). Infrastructure containers (Squid proxy,
	// api-proxy, MCP gateway) remain on the host in Docker Compose.
	// The compiler emits the required privileged setup steps; the runner must be
	// KVM-capable and provide DOCKER_PAT / DOCKER_USERNAME secrets.
	AgentRuntimeDockerSbx AgentRuntime = "docker-sbx"

	// AgentRuntimeCloudHypervisor runs the agent inside a Cloud Hypervisor microVM
	// using AWF's preview cloud-hypervisor runtime mode.
	AgentRuntimeCloudHypervisor AgentRuntime = "cloud-hypervisor"
)

// AgentSandboxConfig represents the agent sandbox configuration
type AgentSandboxConfig struct {
	ID             string                                `yaml:"id,omitempty"`              // Agent ID: "awf" or "srt" (replaces Type in new object format)
	Type           SandboxType                           `yaml:"type,omitempty"`            // Sandbox type: "awf" or "srt" (legacy, use ID instead)
	Version        string                                `yaml:"version,omitempty"`         // AWF version override used to install and run the matching firewall version
	Platform       string                                `yaml:"platform,omitempty"`        // AWF platform.type override (github.com, ghes, ghec, ghec-self-hosted)
	Runtime        AgentRuntime                          `yaml:"runtime,omitempty"`         // Sandbox runtime profile for the agent container (see sandbox_runtime_profile.go)
	AllowHostPorts []int                                 `yaml:"-"`                         // Additional host TCP ports the agent may connect to (docker-sudo-iptables only).
	Disabled       bool                                  `yaml:"-"`                         // True when agent is explicitly set to false (disables firewall). This is a runtime flag, not serialized to YAML.
	Config         *SandboxRuntimeConfig                 `yaml:"config,omitempty"`          // Custom SRT config (optional)
	Command        string                                `yaml:"command,omitempty"`         // Custom command to replace AWF or SRT installation
	Args           []string                              `yaml:"args,omitempty"`            // Additional arguments to append to the command
	Env            map[string]string                     `yaml:"env,omitempty"`             // Environment variables to set on the step
	Mounts         []string                              `yaml:"mounts,omitempty"`          // Container mounts to add for AWF (format: "source:dest:mode")
	Memory         string                                `yaml:"memory,omitempty"`          // Memory limit for the AWF container (e.g., "4g", "8g")
	ModelFallback  *TemplatableBool                      `yaml:"model-fallback,omitempty"`  // AWF API proxy model fallback enable/disable flag (optional)
	TokenSteering  *bool                                 `yaml:"token-steering,omitempty"`  // AWF API proxy token steering enable/disable flag (optional)
	Targets        map[string]*AgentAPIProxyTargetConfig `yaml:"targets,omitempty"`         // Per-provider API proxy target overrides keyed by provider name (e.g. "openai", "anthropic")
	RuntimeInstall *bool                                 `yaml:"runtime-install,omitempty"` // Controls generation of runtime installation steps (gVisor/docker-sbx). Default: true. Noop when runtime is not set.
	Images         map[string]string                     `yaml:"images,omitempty"`          // Digest-pinned AWF infrastructure images keyed by AWF image role (see sandbox_agent_images.go)
	CACert         string                                `yaml:"ca-cert,omitempty"`         // Host path to an additional CA certificate for API proxy upstream TLS verification (maps to apiProxy.caCert, AWF v0.28.10+)
}

// AiCreditsPricingConfig holds per-token pricing rates ($/1M tokens) used as a fallback
// for models not in the AWF built-in pricing table. Maps to apiProxy.defaultAiCreditsPricing
// in the AWF config file. Required when maxAiCredits is active and the model is unrecognized.
type AiCreditsPricingConfig struct {
	// Input is the input token price per 1M tokens in dollars.
	Input float64 `yaml:"input" json:"input"`
	// Output is the output token price per 1M tokens in dollars.
	Output float64 `yaml:"output" json:"output"`
	// CachedInput is the cached-read token price per 1M tokens in dollars.
	CachedInput *float64 `yaml:"cache_read,omitempty" json:"cachedInput,omitempty"`
	// CacheWrite is the cache-write token price per 1M tokens in dollars.
	CacheWrite *float64 `yaml:"cache_write,omitempty" json:"cacheWrite,omitempty"`
}

// AgentAPIProxyTargetConfig configures a single LLM provider's API proxy target.
type AgentAPIProxyTargetConfig struct {
	// AuthHeader is the custom authentication header name sent with API requests.
	// When set, the raw API key is sent as "<authHeader>: <key>" instead of the
	// provider default ("Authorization" for OpenAI, "x-api-key" for Anthropic).
	// Example: "api-key" for Azure OpenAI gateways.
	AuthHeader string `yaml:"authHeader,omitempty"`

	// ExtraHeaders holds additional non-sensitive headers to include on Copilot BYOK
	// upstream requests. Applies only to the "copilot" provider target.
	// Maps to apiProxy.targets.copilot.extraHeaders in the AWF config (AWF_BYOK_EXTRA_HEADERS).
	// Example:
	//   sandbox:
	//     agent:
	//       targets:
	//         copilot:
	//           extraHeaders:
	//             x-openrouter-title: my-workflow
	//             http-referer: https://github.com/org/repo
	ExtraHeaders map[string]string `yaml:"extraHeaders,omitempty"`

	// ExtraBodyFields holds additional non-sensitive JSON body fields to include on Copilot
	// BYOK upstream requests. Applies only to the "copilot" provider target.
	// Maps to apiProxy.targets.copilot.extraBodyFields in the AWF config (AWF_BYOK_EXTRA_BODY_FIELDS).
	ExtraBodyFields map[string]string `yaml:"extraBodyFields,omitempty"`

	// SessionId is an opt-in session identifier injected as the x-session-id request header
	// and session_id body field on Copilot BYOK upstream requests. Applies only to the
	// "copilot" provider target. Strict OpenAI-compatible servers (e.g. Azure OpenAI) reject
	// the unknown body field with HTTP 400, so this value must be set explicitly.
	// Maps to apiProxy.targets.copilot.sessionId in the AWF config (AWF_PROVIDER_SESSION_ID).
	SessionId string `yaml:"sessionId,omitempty"`
}

// SandboxRuntimeConfig represents the Anthropic Sandbox Runtime configuration
// This matches the TypeScript SandboxRuntimeConfig interface
// Note: Network configuration is controlled by the top-level 'network' field, not this struct
type SandboxRuntimeConfig struct {
	// Network is only used internally for generating SRT settings JSON output.
	// It is NOT user-configurable from sandbox.agent.config (yaml:"-" prevents parsing).
	// The json tag is needed for output serialization to .srt-settings.json.
	Network                   *SRTNetworkConfig    `yaml:"-" json:"network,omitempty"`
	Filesystem                *SRTFilesystemConfig `yaml:"filesystem,omitempty" json:"filesystem,omitempty"`
	IgnoreViolations          map[string][]string  `yaml:"ignoreViolations,omitempty" json:"ignoreViolations,omitempty"`
	EnableWeakerNestedSandbox bool                 `yaml:"enableWeakerNestedSandbox" json:"enableWeakerNestedSandbox"`
}

// SRTNetworkConfig represents network configuration for SRT
type SRTNetworkConfig struct {
	AllowedDomains      []string `yaml:"allowedDomains,omitempty" json:"allowedDomains,omitempty"`
	BlockedDomains      []string `yaml:"blockedDomains,omitempty" json:"blockedDomains"`
	AllowUnixSockets    []string `yaml:"allowUnixSockets,omitempty" json:"allowUnixSockets,omitempty"`
	AllowLocalBinding   bool     `yaml:"allowLocalBinding" json:"allowLocalBinding"`
	AllowAllUnixSockets bool     `yaml:"allowAllUnixSockets" json:"allowAllUnixSockets"`
}

// SRTFilesystemConfig represents filesystem configuration for SRT
type SRTFilesystemConfig struct {
	DenyRead   []string `yaml:"denyRead" json:"denyRead"`
	AllowWrite []string `yaml:"allowWrite,omitempty" json:"allowWrite,omitempty"`
	DenyWrite  []string `yaml:"denyWrite" json:"denyWrite"`
}

// getAgentType returns the effective agent type from AgentSandboxConfig
// Prefers ID field (new format) over Type field (legacy)
func getAgentType(agent *AgentSandboxConfig) SandboxType {
	if agent == nil {
		return ""
	}
	// New format: use ID field if set
	if agent.ID != "" {
		return SandboxType(agent.ID)
	}
	// Legacy format: use Type field
	return agent.Type
}

// isSupportedSandboxType checks if a sandbox type is valid/supported
func isSupportedSandboxType(sandboxType SandboxType) bool {
	return sandboxType == SandboxTypeAWF ||
		sandboxType == SandboxTypeDefault
}

// migrateSRTToAWF converts any SRT sandbox configuration to AWF
// This is a codemod that automatically migrates workflows from the deprecated SRT to AWF
func migrateSRTToAWF(sandboxConfig *SandboxConfig) *SandboxConfig {
	if sandboxConfig == nil {
		return nil
	}

	// Migrate legacy Type field from SRT/sandbox-runtime to AWF/default
	if sandboxConfig.Type == "srt" || sandboxConfig.Type == "sandbox-runtime" {
		sandboxLog.Printf("Migrating legacy sandbox type from %s to awf", sandboxConfig.Type)
		sandboxConfig.Type = SandboxTypeAWF
	}

	// Migrate Agent.Type field from SRT to AWF
	if sandboxConfig.Agent != nil {
		if sandboxConfig.Agent.Type == "srt" || sandboxConfig.Agent.Type == "sandbox-runtime" {
			sandboxLog.Printf("Migrating agent type from %s to awf", sandboxConfig.Agent.Type)
			sandboxConfig.Agent.Type = SandboxTypeAWF
		}
		// Migrate Agent.ID field from SRT to AWF
		if sandboxConfig.Agent.ID == "srt" || sandboxConfig.Agent.ID == "sandbox-runtime" {
			sandboxLog.Printf("Migrating agent ID from %s to awf", sandboxConfig.Agent.ID)
			sandboxConfig.Agent.ID = "awf"
		}
	}

	return sandboxConfig
}

// applySandboxDefaults applies default values to sandbox configuration
// If no sandbox config exists, creates one with awf as default agent
// If sandbox config exists but has no agent, sets agent to awf (unless agent is explicitly disabled)
// If sandbox.agent is an object with no id/type (e.g., version-only), defaults the type to awf
func applySandboxDefaults(sandboxConfig *SandboxConfig, engineConfig *EngineConfig) *SandboxConfig {
	// First, migrate any SRT references to AWF (codemod)
	sandboxConfig = migrateSRTToAWF(sandboxConfig)

	// If agent sandbox is explicitly disabled (sandbox.agent: false), preserve that setting
	if sandboxConfig != nil && sandboxConfig.Agent != nil && sandboxConfig.Agent.Disabled {
		sandboxLog.Print("Agent sandbox explicitly disabled with sandbox.agent: false, preserving disabled state")
		return sandboxConfig
	}

	// If no sandbox config exists, create one with awf as default
	if sandboxConfig == nil {
		sandboxLog.Print("No sandbox config found, creating default with agent: awf")
		sandboxConfig = &SandboxConfig{
			Agent: &AgentSandboxConfig{
				Type: SandboxTypeAWF,
			},
		}
		ensureDefaultAgentWritePath(sandboxConfig, engineConfig)
		return sandboxConfig
	}

	// If sandbox config exists with legacy Type field set, don't override with awf default
	// The legacy Type field indicates explicit sandbox configuration
	if sandboxConfig.Type != "" {
		sandboxLog.Printf("Sandbox config uses legacy Type field: %s, preserving it", sandboxConfig.Type)
		ensureDefaultAgentWritePath(sandboxConfig, engineConfig)
		return sandboxConfig
	}

	// If sandbox config exists but has no agent, set agent to awf
	if sandboxConfig.Agent == nil {
		sandboxLog.Print("Sandbox config exists without agent, setting default agent: awf")
		sandboxConfig.Agent = &AgentSandboxConfig{
			Type: SandboxTypeAWF,
		}
		ensureDefaultAgentWritePath(sandboxConfig, engineConfig)
		return sandboxConfig
	}

	// If sandbox.agent is configured but has no type/ID set (e.g., a version-only object
	// like { version: "v0.25.29" } that reached here without a prior `return`), default
	// the type to awf so the sandbox is always enabled.  This prevents a bare
	// sandbox.agent object from silently disabling the firewall by leaving the type empty.
	// Note: this block is only reached when Agent != nil and Disabled == false (the
	// Disabled case returned early above).
	if !isSupportedSandboxType(getAgentType(sandboxConfig.Agent)) {
		sandboxLog.Print("Sandbox agent has no type/ID configured, defaulting to awf")
		sandboxConfig.Agent.Type = SandboxTypeAWF
	}

	ensureDefaultAgentWritePath(sandboxConfig, engineConfig)
	return sandboxConfig
}

// Cloud Hypervisor requires explicit filesystem.allowWrite entries for compiler-managed
// output paths as well as the workspace.
//
// Under Cloud Hypervisor, /workspace and /tmp/gh-aw are separate virtiofs exports, and the
// AWF planner narrows each export independently based on the allowWrite entries that fall
// under it. Seeding only defaultAgentWorkspaceWritePath (/tmp/gh-aw/agent) leaves no allowed
// path under /workspace, so that export is narrowed to read-only: the repo checkout becomes
// unwritable, and HOME (which Cloud Hypervisor sets to /workspace/.awf-home) becomes
// unwritable too. See gh-aw-firewall#7669/#7672 and the "Blocker 1" analysis on the tracking
// issue for the empirical planner output that demonstrated this.
const cloudHypervisorWorkspaceWritePath = "/workspace"
const cloudHypervisorAwfHomeWritePath = "/workspace/.awf-home"

// ensureDefaultAgentWritePath seeds the implicit filesystem.allowWrite entries for the
// Cloud Hypervisor runtime only.
//
// The compose runtimes (Docker, gVisor) deliberately get no implicit allowWrite entries:
// AWF enforces the policy there by narrowing its own writable bind mounts to read-only,
// which turns the container rootfs read-only outside the allowlist. AWF's init-signal
// bind mount at /tmp/awf-init then cannot have its mountpoint created and the agent
// container fails to start ("make mountpoint \"/tmp/awf-init\": read-only file system").
// Seeding a default there would therefore break every compose-runtime workflow, so
// filesystem.allowWrite stays opt-in for those runtimes.
func ensureDefaultAgentWritePath(sandboxConfig *SandboxConfig, engineConfig *EngineConfig) {
	if sandboxConfig == nil || sandboxConfig.Agent == nil {
		return
	}
	if sandboxConfig.Agent.Runtime != AgentRuntimeCloudHypervisor {
		return
	}
	if sandboxConfig.Agent.Config == nil {
		sandboxConfig.Agent.Config = &SandboxRuntimeConfig{}
	}
	if sandboxConfig.Agent.Config.Filesystem == nil {
		sandboxConfig.Agent.Config.Filesystem = &SRTFilesystemConfig{}
	}
	addAllowWritePathIfMissing(sandboxConfig.Agent.Config.Filesystem, defaultAgentWorkspaceWritePath)
	if engineConfig != nil && engineConfig.ID == string(constants.CopilotEngine) {
		addAllowWritePathIfMissing(sandboxConfig.Agent.Config.Filesystem, defaultAgentLogsWritePath)
	}
	if engineConfig != nil && engineConfig.ID == string(constants.CodexEngine) {
		// Codex writes runtime state under CODEX_HOME.
		addAllowWritePathIfMissing(sandboxConfig.Agent.Config.Filesystem, constants.TmpMcpConfigDir)
	}
	addAllowWritePathIfMissing(sandboxConfig.Agent.Config.Filesystem, cloudHypervisorWorkspaceWritePath)
	addAllowWritePathIfMissing(sandboxConfig.Agent.Config.Filesystem, cloudHypervisorAwfHomeWritePath)
}

// ensureCacheMemoryWritePaths adds compiler-provisioned cache directories to the
// Cloud Hypervisor write policy. cacheMemoryDirFor is also used to grant engine
// write tools, keeping both permission surfaces aligned.
func ensureCacheMemoryWritePaths(sandboxConfig *SandboxConfig, cacheMemoryConfig *CacheMemoryConfig) {
	if cacheMemoryConfig == nil || sandboxConfig == nil || sandboxConfig.Agent == nil ||
		sandboxConfig.Agent.Runtime != AgentRuntimeCloudHypervisor {
		return
	}
	if sandboxConfig.Agent.Config == nil {
		sandboxConfig.Agent.Config = &SandboxRuntimeConfig{}
	}
	if sandboxConfig.Agent.Config.Filesystem == nil {
		sandboxConfig.Agent.Config.Filesystem = &SRTFilesystemConfig{}
	}
	for _, cache := range cacheMemoryConfig.Caches {
		addAllowWritePathIfMissing(sandboxConfig.Agent.Config.Filesystem, cacheMemoryDirFor(cache.ID))
	}
}

// ensureRepoMemoryWritePaths adds compiler-provisioned repo-memory directories to the
// Cloud Hypervisor write policy. Wiki memories are stored in the same RepoMemoryConfig
// and share the /tmp/gh-aw/repo-memory/<id> layout. Without these entries the
// /tmp/gh-aw export is narrowed to read-only outside the allowlist, so the agent cannot
// write to the cloned memory working tree (writes fail with EROFS) and the repo-memory
// push job has nothing to commit.
func ensureRepoMemoryWritePaths(sandboxConfig *SandboxConfig, repoMemoryConfig *RepoMemoryConfig) {
	if repoMemoryConfig == nil || sandboxConfig == nil || sandboxConfig.Agent == nil ||
		sandboxConfig.Agent.Runtime != AgentRuntimeCloudHypervisor {
		return
	}
	if sandboxConfig.Agent.Config == nil {
		sandboxConfig.Agent.Config = &SandboxRuntimeConfig{}
	}
	if sandboxConfig.Agent.Config.Filesystem == nil {
		sandboxConfig.Agent.Config.Filesystem = &SRTFilesystemConfig{}
	}
	for _, memory := range repoMemoryConfig.Memories {
		addAllowWritePathIfMissing(sandboxConfig.Agent.Config.Filesystem, constants.TmpRepoMemoryDir+memory.ID)
	}
}

// addAllowWritePathIfMissing appends path to filesystem.AllowWrite unless it is already present.
func addAllowWritePathIfMissing(filesystem *SRTFilesystemConfig, path string) {
	if slices.Contains(filesystem.AllowWrite, path) {
		return
	}
	filesystem.AllowWrite = append(filesystem.AllowWrite, path)
}

func mergeImportedSandboxAgentMounts(sandboxConfig *SandboxConfig, importedMounts []string) *SandboxConfig {
	if len(importedMounts) == 0 {
		return sandboxConfig
	}

	if sandboxConfig == nil {
		sandboxConfig = &SandboxConfig{}
	}

	if sandboxConfig.Agent != nil && sandboxConfig.Agent.Disabled {
		return sandboxConfig
	}

	if sandboxConfig.Agent == nil {
		sandboxConfig.Agent = &AgentSandboxConfig{}
	}

	sandboxConfig.Agent.Mounts = sliceutil.MergeUnique(importedMounts, sandboxConfig.Agent.Mounts...)
	return sandboxConfig
}

// mergeImportedSandboxAgentRuntimeInstall applies the runtime-install override
// from imported workflows. When any import sets runtime-install: false the main
// workflow's agent config inherits false (the restrictive value wins). A nil value
// (field not set in any import) leaves the main workflow's own setting intact.
func mergeImportedSandboxAgentRuntimeInstall(sandboxConfig *SandboxConfig, importedRuntimeInstall *bool) *SandboxConfig {
	if importedRuntimeInstall == nil {
		return sandboxConfig
	}
	if sandboxConfig == nil {
		sandboxConfig = &SandboxConfig{}
	}
	if sandboxConfig.Agent == nil {
		sandboxConfig.Agent = &AgentSandboxConfig{}
	}
	// Only apply when the imported value is false (restrictive wins) or when the
	// main workflow has not explicitly set the field.
	if !*importedRuntimeInstall || sandboxConfig.Agent.RuntimeInstall == nil {
		sandboxConfig.Agent.RuntimeInstall = importedRuntimeInstall
	}
	return sandboxConfig
}

// isSandboxEnabled checks if the sandbox is enabled (either explicitly or auto-enabled)
// Returns true when:
// - sandbox.agent is explicitly set to awf
// - Firewall is auto-enabled (networkPermissions.Firewall is set and enabled)
// Returns false when:
// - sandbox.agent is false (explicitly disabled)
// - No sandbox configuration and no auto-enabled firewall
func isSandboxEnabled(sandboxConfig *SandboxConfig, networkPermissions *NetworkPermissions) bool {
	// Check if sandbox.agent is explicitly disabled
	if sandboxConfig != nil && sandboxConfig.Agent != nil && sandboxConfig.Agent.Disabled {
		return false
	}

	// Check if sandbox.agent is explicitly configured with a type
	if sandboxConfig != nil && sandboxConfig.Agent != nil {
		agentType := getAgentType(sandboxConfig.Agent)
		if isSupportedSandboxType(agentType) {
			return true
		}
	}

	// Check legacy top-level Type field (deprecated but still supported)
	if sandboxConfig != nil && isSupportedSandboxType(sandboxConfig.Type) {
		return true
	}

	// Check if firewall is auto-enabled (AWF)
	if networkPermissions != nil && networkPermissions.Firewall != nil && networkPermissions.Firewall.Enabled {
		return true
	}

	return false
}
