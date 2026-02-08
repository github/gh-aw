// This file provides sandbox validation functions for agentic workflow compilation.
//
// This file contains domain-specific validation functions for sandbox configuration:
//   - validateMountsSyntax() - Validates container mount syntax
//   - validateSandboxConfig() - Validates complete sandbox configuration
//
// These validation functions are organized in a dedicated file following the validation
// architecture pattern where domain-specific validation belongs in domain validation files.
// See validation.go for the complete validation architecture documentation.

package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var sandboxValidationLog = logger.New("workflow:sandbox_validation")

// validateMountsSyntax validates that mount strings follow the correct syntax
// Expected format: "source:destination:mode" where mode is either "ro" or "rw"
func validateMountsSyntax(mounts []string) error {
	for i, mount := range mounts {
		// Split the mount string by colons
		parts := strings.Split(mount, ":")

		// Must have exactly 3 parts: source, destination, mode
		if len(parts) != 3 {
			return NewValidationError(
				fmt.Sprintf("sandbox.mounts[%d]", i),
				mount,
				"mount syntax must follow 'source:destination:mode' format with exactly 3 colon-separated parts",
				fmt.Sprintf("Use the format 'source:destination:mode'.\n\nExample:\nsandbox:\n  mounts:\n    - \"/host/path:/container/path:ro\"\n\nSee: %s", constants.DocsSandboxURL),
			)
		}

		source := parts[0]
		dest := parts[1]
		mode := parts[2]

		// Validate that source and destination are not empty
		if source == "" {
			return NewValidationError(
				fmt.Sprintf("sandbox.mounts[%d].source", i),
				mount,
				"source path cannot be empty",
				fmt.Sprintf("Provide a valid source path.\n\nExample:\nsandbox:\n  mounts:\n    - \"/host/path:/container/path:ro\"\n\nSee: %s", constants.DocsSandboxURL),
			)
		}
		if dest == "" {
			return NewValidationError(
				fmt.Sprintf("sandbox.mounts[%d].destination", i),
				mount,
				"destination path cannot be empty",
				fmt.Sprintf("Provide a valid destination path.\n\nExample:\nsandbox:\n  mounts:\n    - \"/host/path:/container/path:ro\"\n\nSee: %s", constants.DocsSandboxURL),
			)
		}

		// Validate mode is either "ro" or "rw"
		if mode != "ro" && mode != "rw" {
			return NewValidationError(
				fmt.Sprintf("sandbox.mounts[%d].mode", i),
				mode,
				"mount mode must be 'ro' (read-only) or 'rw' (read-write)",
				fmt.Sprintf("Change the mount mode to either 'ro' or 'rw'.\n\nExample:\nsandbox:\n  mounts:\n    - \"/host/path:/container/path:ro\"  # read-only\n    - \"/host/path:/container/path:rw\"  # read-write\n\nSee: %s", constants.DocsSandboxURL),
			)
		}

		sandboxValidationLog.Printf("Validated mount %d: source=%s, dest=%s, mode=%s", i, source, dest, mode)
	}

	return nil
}

// validateSandboxConfig validates the sandbox configuration
// Returns an error if the configuration is invalid
func validateSandboxConfig(workflowData *WorkflowData) error {
	if workflowData == nil {
		return nil
	}

	if workflowData.SandboxConfig == nil {
		return nil // No sandbox config is valid
	}

	sandboxConfig := workflowData.SandboxConfig

	// Check if sandbox: false or sandbox.agent: false was specified
	// In non-strict mode, this is allowed (with a warning shown at compile time)
	// The strict mode check happens in validateStrictFirewall()
	if sandboxConfig.Agent != nil && sandboxConfig.Agent.Disabled {
		// sandbox: false is allowed in non-strict mode, so we don't error here
		// The warning is emitted in compiler.go
		sandboxValidationLog.Print("sandbox: false detected, will be validated by strict mode check")
	}

	// Validate mounts syntax if specified in agent config
	agentConfig := getAgentConfig(workflowData)
	if agentConfig != nil && len(agentConfig.Mounts) > 0 {
		if err := validateMountsSyntax(agentConfig.Mounts); err != nil {
			return err
		}
	}

	// Validate that SRT is only used with Copilot engine
	if isSRTEnabled(workflowData) {
		// Check if the sandbox-runtime feature flag is enabled
		if !isFeatureEnabled(constants.SandboxRuntimeFeatureFlag, workflowData) {
			return NewConfigurationError(
				"features.sandbox-runtime",
				"not enabled",
				"sandbox-runtime feature is experimental and requires the feature flag to be enabled",
				"Enable the sandbox-runtime feature flag in your workflow:\nfeatures:\n  sandbox-runtime: true\n\nOr set the environment variable: GH_AW_FEATURES=sandbox-runtime",
			)
		}

		if workflowData.EngineConfig == nil || workflowData.EngineConfig.ID != "copilot" {
			engineID := "none"
			if workflowData.EngineConfig != nil {
				engineID = workflowData.EngineConfig.ID
			}
			return NewConfigurationError(
				"engine",
				engineID,
				"sandbox-runtime is only supported with Copilot engine",
				"Change your workflow to use the Copilot engine:\nengine: copilot\nsandbox: sandbox-runtime",
			)
		}

		// Check for mutual exclusivity with AWF
		if workflowData.NetworkPermissions != nil && workflowData.NetworkPermissions.Firewall != nil && workflowData.NetworkPermissions.Firewall.Enabled {
			return NewConfigurationError(
				"sandbox",
				"sandbox-runtime with network.firewall",
				"sandbox-runtime and AWF firewall cannot be used together",
				fmt.Sprintf("Choose one sandbox approach:\n\nOption 1 (sandbox-runtime):\nsandbox: sandbox-runtime\n\nOption 2 (AWF firewall):\nnetwork:\n  firewall: true\n\nSee: %s", constants.DocsSandboxURL),
			)
		}
	}

	// Validate config structure if provided
	if sandboxConfig.Config != nil {
		if sandboxConfig.Type != SandboxTypeRuntime {
			return NewConfigurationError(
				"sandbox.config",
				string(sandboxConfig.Type),
				"custom sandbox config can only be provided when type is 'sandbox-runtime'",
				"Set sandbox type to 'sandbox-runtime' to use custom config:\nsandbox:\n  type: sandbox-runtime\n  config:\n    # your custom config here",
			)
		}
	}

	// Validate MCP gateway port if configured
	if sandboxConfig.MCP != nil && sandboxConfig.MCP.Port != 0 {
		if err := validateIntRange(sandboxConfig.MCP.Port, constants.MinNetworkPort, constants.MaxNetworkPort, "sandbox.mcp.port"); err != nil {
			return err
		}
		sandboxValidationLog.Printf("Validated MCP gateway port: %d", sandboxConfig.MCP.Port)
	}

	// Validate that if agent sandbox is enabled, MCP gateway must be enabled
	// The MCP gateway is enabled when MCP servers are configured (tools that use MCP)
	// Only validate this when sandbox is explicitly configured (not nil)
	// If SandboxConfig is nil, defaults will be applied later and MCP check doesn't apply yet
	if !isSandboxDisabled(workflowData) {
		// Sandbox is enabled - check if MCP gateway is enabled
		// Only enforce this if sandbox was explicitly configured (has agent or type set)
		// This prevents false positives for workflows where sandbox defaults haven't been applied yet
		hasExplicitSandboxConfig := (sandboxConfig.Agent != nil && !sandboxConfig.Agent.Disabled) ||
			sandboxConfig.Type != ""

		if hasExplicitSandboxConfig && !HasMCPServers(workflowData) {
			return NewConfigurationError(
				"sandbox",
				"enabled without MCP servers",
				"agent sandbox requires MCP servers to be configured",
				"Add MCP tools to your workflow:\n\ntools:\n  github:\n    mode: remote\n  playwright:\n    allowed_domains: [\"example.com\"]\n\nOr disable the sandbox:\nsandbox: false",
			)
		}
		if hasExplicitSandboxConfig {
			sandboxValidationLog.Print("Sandbox enabled with MCP gateway - validation passed")
		}
	}

	return nil
}
