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
	"encoding/json"
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
)

var sandboxLog = logger.New("workflow:sandbox")

// SandboxType represents the type of sandbox to use
type SandboxType string

const (
	SandboxTypeAWF     SandboxType = "awf"             // Uses AWF (Agent Workflow Firewall)
	SandboxTypeSRT     SandboxType = "srt"             // Uses Anthropic Sandbox Runtime
	SandboxTypeDefault SandboxType = "default"         // Alias for AWF (backward compat)
	SandboxTypeRuntime SandboxType = "sandbox-runtime" // Alias for SRT (backward compat)
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

// AgentSandboxConfig represents the agent sandbox configuration
type AgentSandboxConfig struct {
	ID       string                `yaml:"id,omitempty"`      // Agent ID: "awf" or "srt" (replaces Type in new object format)
	Type     SandboxType           `yaml:"type,omitempty"`    // Sandbox type: "awf" or "srt" (legacy, use ID instead)
	Disabled bool                  `yaml:"-"`                 // True when agent is explicitly set to false (disables firewall). This is a runtime flag, not serialized to YAML.
	Config   *SandboxRuntimeConfig `yaml:"config,omitempty"`  // Custom SRT config (optional)
	Command  string                `yaml:"command,omitempty"` // Custom command to replace AWF or SRT installation
	Args     []string              `yaml:"args,omitempty"`    // Additional arguments to append to the command
	Env      map[string]string     `yaml:"env,omitempty"`     // Environment variables to set on the step
	Mounts   []string              `yaml:"mounts,omitempty"`  // Container mounts to add for AWF (format: "source:dest:mode")
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
		sandboxType == SandboxTypeSRT ||
		sandboxType == SandboxTypeDefault ||
		sandboxType == SandboxTypeRuntime
}

// isSRTEnabled checks if Sandbox Runtime is enabled for the workflow
func isSRTEnabled(workflowData *WorkflowData) bool {
	if workflowData == nil || workflowData.SandboxConfig == nil {
		sandboxLog.Print("No sandbox config, SRT disabled")
		return false
	}

	config := workflowData.SandboxConfig

	// Check new format: sandbox.agent
	if config.Agent != nil {
		// Get effective type from ID or Type field
		agentType := getAgentType(config.Agent)
		enabled := agentType == SandboxTypeSRT || agentType == SandboxTypeRuntime
		sandboxLog.Printf("SRT enabled check (new format): %v (type=%s)", enabled, agentType)
		return enabled
	}

	// Check legacy format: sandbox.type
	enabled := config.Type == SandboxTypeRuntime || config.Type == SandboxTypeSRT
	sandboxLog.Printf("SRT enabled check (legacy format): %v (type=%s)", enabled, config.Type)
	return enabled
}

// generateSRTConfigJSON generates the .srt-settings.json content
// Network configuration is always derived from the top-level 'network' field.
// User-provided sandbox config can override filesystem, ignoreViolations, and enableWeakerNestedSandbox.
func generateSRTConfigJSON(workflowData *WorkflowData) (string, error) {
	if workflowData == nil {
		return "", fmt.Errorf("workflowData is nil")
	}

	sandboxConfig := workflowData.SandboxConfig
	if sandboxConfig == nil {
		return "", fmt.Errorf("sandbox config is nil")
	}

	// Start with base SRT config
	sandboxLog.Print("Generating SRT config from network permissions")

	// Generate network config from top-level network field (always)
	// Network config is NOT user-configurable from sandbox.agent.config
	domainMap := make(map[string]bool)

	// Add Copilot default domains
	for _, domain := range CopilotDefaultDomains {
		domainMap[domain] = true
	}

	// Add NetworkPermissions domains (if specified)
	if workflowData.NetworkPermissions != nil && len(workflowData.NetworkPermissions.Allowed) > 0 {
		// Expand ecosystem identifiers and add individual domains
		expandedDomains := GetAllowedDomains(workflowData.NetworkPermissions)
		for _, domain := range expandedDomains {
			domainMap[domain] = true
		}
	}

	// Convert map keys to slice - using functional helper
	allowedDomains := sliceutil.MapToSlice(domainMap)
	SortStrings(allowedDomains)

	srtConfig := &SandboxRuntimeConfig{
		Network: &SRTNetworkConfig{
			AllowedDomains:      allowedDomains,
			BlockedDomains:      []string{},
			AllowUnixSockets:    []string{"/var/run/docker.sock"},
			AllowLocalBinding:   false,
			AllowAllUnixSockets: true,
		},
		Filesystem: &SRTFilesystemConfig{
			DenyRead:   []string{},
			AllowWrite: []string{".", "/home/runner/.copilot", "/home/runner/.cache", "/tmp"},
			DenyWrite:  []string{},
		},
		IgnoreViolations:          map[string][]string{},
		EnableWeakerNestedSandbox: true,
	}

	// Apply user-provided non-network config (filesystem, ignoreViolations, enableWeakerNestedSandbox)
	var userConfig *SandboxRuntimeConfig
	if sandboxConfig.Agent != nil && sandboxConfig.Agent.Config != nil {
		userConfig = sandboxConfig.Agent.Config
	} else if sandboxConfig.Config != nil {
		userConfig = sandboxConfig.Config
	}

	if userConfig != nil {
		sandboxLog.Print("Applying user-provided SRT config (filesystem, ignoreViolations, enableWeakerNestedSandbox)")

		// Apply filesystem config if provided
		if userConfig.Filesystem != nil {
			srtConfig.Filesystem = userConfig.Filesystem
			// Normalize nil slices
			if srtConfig.Filesystem.DenyRead == nil {
				srtConfig.Filesystem.DenyRead = []string{}
			}
			if srtConfig.Filesystem.AllowWrite == nil {
				srtConfig.Filesystem.AllowWrite = []string{}
			}
			if srtConfig.Filesystem.DenyWrite == nil {
				srtConfig.Filesystem.DenyWrite = []string{}
			}
		}

		// Apply ignoreViolations if provided
		if userConfig.IgnoreViolations != nil {
			srtConfig.IgnoreViolations = userConfig.IgnoreViolations
		}

		// Note: EnableWeakerNestedSandbox defaults to true in srtConfig above.
		// We only override it with the user's value if they provided a config.
		// Since Go's bool zero value is false, if user doesn't specify this field,
		// it will be false in userConfig. This means users must explicitly set it
		// to true if they want it enabled when providing custom config.
		// This is intentional: providing custom config opts into full control.
		srtConfig.EnableWeakerNestedSandbox = userConfig.EnableWeakerNestedSandbox
	}

	// Marshal to JSON with indentation
	jsonBytes, err := json.MarshalIndent(srtConfig, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal SRT config to JSON: %w", err)
	}

	sandboxLog.Printf("Generated SRT config: %s", string(jsonBytes))
	return string(jsonBytes), nil
}

// applySandboxDefaults applies default values to sandbox configuration
// If no sandbox config exists, creates one with awf as default agent
// If sandbox config exists but has no agent, sets agent to awf (unless using legacy Type field or sandbox: false)
func applySandboxDefaults(sandboxConfig *SandboxConfig, engineConfig *EngineConfig) *SandboxConfig {
	// If sandbox is explicitly disabled (sandbox: false), preserve that setting
	if sandboxConfig != nil && sandboxConfig.Agent != nil && sandboxConfig.Agent.Disabled {
		sandboxLog.Print("Sandbox explicitly disabled with sandbox: false, preserving disabled state")
		return sandboxConfig
	}

	// If no sandbox config exists, create one with awf as default
	if sandboxConfig == nil {
		sandboxLog.Print("No sandbox config found, creating default with agent: awf")
		return &SandboxConfig{
			Agent: &AgentSandboxConfig{
				Type: SandboxTypeAWF,
			},
		}
	}

	// If sandbox config exists with legacy Type field set, don't override with awf default
	// The legacy Type field indicates explicit sandbox configuration
	if sandboxConfig.Type != "" {
		sandboxLog.Printf("Sandbox config uses legacy Type field: %s, preserving it", sandboxConfig.Type)
		return sandboxConfig
	}

	// If sandbox config exists but has no agent, set agent to awf
	if sandboxConfig.Agent == nil {
		sandboxLog.Print("Sandbox config exists without agent, setting default agent: awf")
		sandboxConfig.Agent = &AgentSandboxConfig{
			Type: SandboxTypeAWF,
		}
	}

	return sandboxConfig
}
