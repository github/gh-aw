// This file provides strict mode security validation for agentic workflows.
//
// # Strict Mode Validation
//
// This file contains strict mode validation functions that enforce security
// and safety constraints when workflows are compiled with the --strict flag.
//
// Strict mode is designed for production workflows that require enhanced security
// guarantees. It enforces constraints on:
//   - Write permissions on sensitive scopes
//   - Network access configuration
//   - Top-level network configuration required for container-based MCP servers
//   - Bash wildcard tool usage
//
// # Validation Functions
//
// The strict mode validator performs progressive validation:
//  1. validateStrictMode() - Main orchestrator that coordinates all strict mode checks
//  2. validateStrictPermissions() - Refuses write permissions on sensitive scopes
//  3. validateStrictNetwork() - Requires explicit network configuration
//  4. validateStrictMCPNetwork() - Requires top-level network config for container-based MCP servers
//
// # Integration with Security Scanners
//
// Strict mode also affects the zizmor security scanner behavior (see pkg/cli/zizmor.go).
// When zizmor is enabled with --zizmor flag, strict mode treats any security findings
// as compilation errors rather than warnings.
//
// # When to Add Validation Here
//
// Add validation to this file when:
//   - It enforces a strict mode security policy
//   - It restricts permissions or access in production workflows
//   - It validates network access controls
//   - It enforces tool usage restrictions for security
//
// For general validation, see validation.go.
// For detailed documentation, see scratchpad/validation-architecture.md

package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var strictModeValidationLog = logger.New("workflow:strict_mode_validation")

// validateStrictPermissions refuses write permissions in strict mode
func (c *Compiler) validateStrictPermissions(frontmatter map[string]any) error {
	permissionsValue, exists := frontmatter["permissions"]
	if !exists {
		// No permissions specified is fine
		strictModeValidationLog.Printf("No permissions specified, validation passed")
		return nil
	}

	// Parse permissions using the PermissionsParser
	perms := NewPermissionsParserFromValue(permissionsValue)

	// Check for write permissions on sensitive scopes
	writePermissions := []string{"contents", "issues", "pull-requests"}
	for _, scope := range writePermissions {
		if perms.IsAllowed(scope, "write") {
			strictModeValidationLog.Printf("Write permission validation failed: scope=%s", scope)
			return fmt.Errorf("strict mode: write permission '%s: write' is not allowed for security reasons. Use 'safe-outputs.create-issue', 'safe-outputs.create-pull-request', 'safe-outputs.add-comment', or 'safe-outputs.update-issue' to perform write operations safely. See: https://github.github.com/gh-aw/reference/safe-outputs/", scope)
		}
	}

	strictModeValidationLog.Printf("Permissions validation passed")
	return nil
}

// validateStrictNetwork validates network configuration in strict mode and refuses "*" wildcard
// Note: networkPermissions should never be nil at this point because the compiler orchestrator
// applies defaults (Allowed: ["defaults"]) when no network configuration is specified in frontmatter.
// This automatic default application means users don't need to explicitly declare network in strict mode.
func (c *Compiler) validateStrictNetwork(networkPermissions *NetworkPermissions) error {
	// This check should never trigger in production since the compiler orchestrator
	// always applies defaults before calling validation. However, we keep it for defensive programming
	// and to handle direct unit test calls.
	if networkPermissions == nil {
		strictModeValidationLog.Printf("Network configuration unexpectedly nil (defaults should have been applied)")
		return fmt.Errorf("internal error: network permissions not initialized (this should not happen in normal operation)")
	}

	// If allowed list contains "defaults", that's acceptable (this is the automatic default)
	for _, domain := range networkPermissions.Allowed {
		if domain == "defaults" {
			strictModeValidationLog.Printf("Network validation passed: allowed list contains 'defaults'")
			return nil
		}
	}

	// Check for wildcard "*" in allowed domains
	for _, domain := range networkPermissions.Allowed {
		if domain == "*" {
			strictModeValidationLog.Printf("Network validation failed: wildcard detected")
			return fmt.Errorf("strict mode: wildcard '*' is not allowed in network.allowed domains to prevent unrestricted internet access. Specify explicit domains or use ecosystem identifiers like 'python', 'node', 'containers'. See: https://github.github.com/gh-aw/reference/network/#available-ecosystem-identifiers")
		}
	}

	strictModeValidationLog.Printf("Network validation passed: allowed_count=%d", len(networkPermissions.Allowed))
	return nil
}

// validateStrictMCPNetwork requires top-level network configuration when custom MCP servers use containers
func (c *Compiler) validateStrictMCPNetwork(frontmatter map[string]any, networkPermissions *NetworkPermissions) error {
	// Check mcp-servers section (new format)
	mcpServersValue, exists := frontmatter["mcp-servers"]
	if !exists {
		return nil
	}

	mcpServersMap, ok := mcpServersValue.(map[string]any)
	if !ok {
		return nil
	}

	// Check if top-level network configuration exists
	hasTopLevelNetwork := networkPermissions != nil && len(networkPermissions.Allowed) > 0

	// Check each MCP server for containers
	for serverName, serverValue := range mcpServersMap {
		serverConfig, ok := serverValue.(map[string]any)
		if !ok {
			continue
		}

		// Use helper function to determine if this is an MCP config and its type
		hasMCP, mcpType := hasMCPConfig(serverConfig)
		if !hasMCP {
			continue
		}

		// Only stdio servers with containers need network configuration
		if mcpType == "stdio" {
			if _, hasContainer := serverConfig["container"]; hasContainer {
				// Require top-level network configuration
				if !hasTopLevelNetwork {
					return fmt.Errorf("strict mode: custom MCP server '%s' with container must have top-level network configuration for security. Add 'network: { allowed: [...] }' to the workflow to restrict network access. See: https://github.github.com/gh-aw/reference/network/", serverName)
				}
			}
		}
	}

	return nil
}

// validateStrictTools validates tools configuration in strict mode
func (c *Compiler) validateStrictTools(frontmatter map[string]any) error {
	// Check tools section
	toolsValue, exists := frontmatter["tools"]
	if !exists {
		return nil
	}

	toolsMap, ok := toolsValue.(map[string]any)
	if !ok {
		return nil
	}

	// Check if serena is configured with local mode
	serenaValue, hasSerena := toolsMap["serena"]
	if hasSerena {
		// Check if serena is a map (detailed configuration)
		if serenaConfig, ok := serenaValue.(map[string]any); ok {
			// Check if mode is set to "local"
			if mode, hasMode := serenaConfig["mode"]; hasMode {
				if modeStr, ok := mode.(string); ok && modeStr == "local" {
					strictModeValidationLog.Printf("Serena local mode validation failed")
					return fmt.Errorf("strict mode: serena tool with 'mode: local' is not allowed for security reasons. Local mode runs the MCP server directly on the host without containerization, bypassing security isolation. Use 'mode: docker' (default) instead, which runs Serena in a container. See: https://github.github.com/gh-aw/reference/tools/#serena")
				}
			}
		}
	}

	// Check if cache-memory is configured with scope: repo
	cacheMemoryValue, hasCacheMemory := toolsMap["cache-memory"]
	if hasCacheMemory {
		// Helper function to check scope in a cache entry
		checkScope := func(cacheMap map[string]any) error {
			if scope, hasScope := cacheMap["scope"]; hasScope {
				if scopeStr, ok := scope.(string); ok && scopeStr == "repo" {
					strictModeValidationLog.Printf("Cache-memory repo scope validation failed")
					return fmt.Errorf("strict mode: cache-memory with 'scope: repo' is not allowed for security reasons. Repo scope allows cache sharing across all workflows in the repository, which can enable cross-workflow cache poisoning attacks. Use 'scope: workflow' (default) instead, which isolates caches to individual workflows. See: https://github.github.com/gh-aw/reference/tools/#cache-memory")
				}
			}
			return nil
		}

		// Check if cache-memory is a map (object notation)
		if cacheMemoryConfig, ok := cacheMemoryValue.(map[string]any); ok {
			if err := checkScope(cacheMemoryConfig); err != nil {
				return err
			}
		}

		// Check if cache-memory is an array (array notation)
		if cacheMemoryArray, ok := cacheMemoryValue.([]any); ok {
			for _, item := range cacheMemoryArray {
				if cacheMap, ok := item.(map[string]any); ok {
					if err := checkScope(cacheMap); err != nil {
						return err
					}
				}
			}
		}
	}

	return nil
}

// validateStrictDeprecatedFields refuses deprecated fields in strict mode
func (c *Compiler) validateStrictDeprecatedFields(frontmatter map[string]any) error {
	// Get the list of deprecated fields from the schema
	deprecatedFields, err := parser.GetMainWorkflowDeprecatedFields()
	if err != nil {
		strictModeValidationLog.Printf("Failed to get deprecated fields: %v", err)
		// Don't fail compilation if we can't load deprecated fields list
		return nil
	}

	// Check if any deprecated fields are present in the frontmatter
	foundDeprecated := parser.FindDeprecatedFieldsInFrontmatter(frontmatter, deprecatedFields)

	if len(foundDeprecated) > 0 {
		// Build error message with all deprecated fields
		var errorMessages []string
		for _, field := range foundDeprecated {
			message := fmt.Sprintf("Field '%s' is deprecated", field.Name)
			if field.Replacement != "" {
				message += fmt.Sprintf(". Use '%s' instead", field.Replacement)
			}
			errorMessages = append(errorMessages, message)
		}

		strictModeValidationLog.Printf("Deprecated fields found: %v", errorMessages)
		return fmt.Errorf("strict mode: deprecated fields are not allowed. %s", strings.Join(errorMessages, ". "))
	}

	strictModeValidationLog.Printf("No deprecated fields found")
	return nil
}

// validateStrictMode performs strict mode validations on the workflow
//
// This is the main orchestrator that calls individual validation functions.
// It performs progressive validation:
//  1. validateStrictPermissions() - Refuses write permissions on sensitive scopes
//  2. validateStrictNetwork() - Requires explicit network configuration
//  3. validateStrictMCPNetwork() - Requires top-level network config for container-based MCP servers
//  4. validateStrictTools() - Validates tools configuration (e.g., serena local mode)
//  5. validateStrictDeprecatedFields() - Refuses deprecated fields
//
// Note: Strict mode also affects zizmor security scanner behavior (see pkg/cli/zizmor.go)
// When zizmor is enabled with --zizmor flag, strict mode will treat any security
// findings as compilation errors rather than warnings.
func (c *Compiler) validateStrictMode(frontmatter map[string]any, networkPermissions *NetworkPermissions) error {
	if !c.strictMode {
		strictModeValidationLog.Printf("Strict mode disabled, skipping validation")
		return nil
	}

	strictModeValidationLog.Printf("Starting strict mode validation")

	// Collect all strict mode validation errors
	collector := NewErrorCollector(c.failFast)

	// 1. Refuse write permissions
	if err := c.validateStrictPermissions(frontmatter); err != nil {
		if returnErr := collector.Add(err); returnErr != nil {
			return returnErr // Fail-fast mode
		}
	}

	// 2. Require network configuration and refuse "*" wildcard
	if err := c.validateStrictNetwork(networkPermissions); err != nil {
		if returnErr := collector.Add(err); returnErr != nil {
			return returnErr // Fail-fast mode
		}
	}

	// 3. Require network configuration on custom MCP servers
	if err := c.validateStrictMCPNetwork(frontmatter, networkPermissions); err != nil {
		if returnErr := collector.Add(err); returnErr != nil {
			return returnErr // Fail-fast mode
		}
	}

	// 4. Validate tools configuration
	if err := c.validateStrictTools(frontmatter); err != nil {
		if returnErr := collector.Add(err); returnErr != nil {
			return returnErr // Fail-fast mode
		}
	}

	// 5. Refuse deprecated fields
	if err := c.validateStrictDeprecatedFields(frontmatter); err != nil {
		if returnErr := collector.Add(err); returnErr != nil {
			return returnErr // Fail-fast mode
		}
	}

	strictModeValidationLog.Printf("Strict mode validation completed: error_count=%d", collector.Count())

	return collector.FormattedError("strict mode")
}

// validateStrictFirewall requires firewall to be enabled in strict mode for copilot and codex engines
// when network domains are provided (non-wildcard).
// In strict mode, ALL engines (regardless of LLM gateway support) require that network domains
// must be defaults or from known ecosystems, and sandbox.agent must be enabled.
func (c *Compiler) validateStrictFirewall(engineID string, networkPermissions *NetworkPermissions, sandboxConfig *SandboxConfig) error {
	if !c.strictMode {
		strictModeValidationLog.Printf("Strict mode disabled, skipping firewall validation")
		return nil
	}

	// Get the engine instance to check LLM gateway support
	agenticEngine, err := c.engineRegistry.GetEngine(engineID)
	if err != nil {
		strictModeValidationLog.Printf("Failed to get engine: %v", err)
		return fmt.Errorf("internal error: failed to get engine '%s': %w", engineID, err)
	}

	// Check if engine supports LLM gateway
	llmGatewayPort := agenticEngine.SupportsLLMGateway()
	strictModeValidationLog.Printf("Engine '%s' LLM gateway port: %d", engineID, llmGatewayPort)

	// Check if sandbox.agent: false is set (explicitly disabled)
	sandboxAgentDisabled := sandboxConfig != nil && sandboxConfig.Agent != nil && sandboxConfig.Agent.Disabled

	// In strict mode, sandbox.agent: false is not allowed for any engine as it disables the agent sandbox firewall
	if sandboxAgentDisabled {
		strictModeValidationLog.Printf("sandbox.agent: false is set, refusing in strict mode")
		// For engines without LLM gateway support, provide more specific error message
		if llmGatewayPort < 0 {
			return fmt.Errorf("strict mode: engine '%s' does not support LLM gateway and requires 'sandbox.agent' to be enabled for security. Remove 'sandbox.agent: false' or set 'strict: false'. See: https://github.github.com/gh-aw/reference/sandbox/", engineID)
		}
		return fmt.Errorf("strict mode: 'sandbox.agent: false' is not allowed because it disables the agent sandbox firewall. This removes important security protections. Remove 'sandbox.agent: false' or set 'strict: false' to disable strict mode. See: https://github.github.com/gh-aw/reference/sandbox/")
	}

	// In strict mode, ALL engines must use network domains from known ecosystems (not custom domains)
	// This applies regardless of LLM gateway support
	if networkPermissions != nil && len(networkPermissions.Allowed) > 0 {
		strictModeValidationLog.Printf("Validating network domains in strict mode for all engines")

		// Check if allowed domains contain only known ecosystem identifiers
		// Track domains that are not ecosystem identifiers (both individual ecosystem domains and truly custom domains)
		type domainSuggestion struct {
			domain    string
			ecosystem string // empty if no ecosystem found, non-empty if domain belongs to known ecosystem
		}
		var invalidDomains []domainSuggestion

		for _, domain := range networkPermissions.Allowed {
			// Skip wildcards (handled below)
			if domain == "*" {
				continue
			}

			// Check if this is a known ecosystem identifier
			ecosystemDomains := getEcosystemDomains(domain)
			if len(ecosystemDomains) > 0 {
				// This is a known ecosystem identifier - allowed in strict mode
				strictModeValidationLog.Printf("Domain '%s' is a known ecosystem identifier", domain)
				continue
			}

			// Not an ecosystem identifier - check if it belongs to any ecosystem
			ecosystem := GetDomainEcosystem(domain)
			// Add to invalid domains (with or without ecosystem suggestion)
			strictModeValidationLog.Printf("Domain '%s' ecosystem: '%s'", domain, ecosystem)
			invalidDomains = append(invalidDomains, domainSuggestion{domain: domain, ecosystem: ecosystem})
		}

		if len(invalidDomains) > 0 {
			strictModeValidationLog.Printf("Engine '%s' has invalid domains in strict mode, failing validation", engineID)

			// Build error message with ecosystem suggestions
			errorMsg := "strict mode: network domains must be from known ecosystems (e.g., 'defaults', 'python', 'node') for all engines in strict mode. Custom domains are not allowed for security."

			// Add suggestions for domains that belong to known ecosystems
			var suggestions []string
			for _, ds := range invalidDomains {
				if ds.ecosystem != "" {
					suggestions = append(suggestions, fmt.Sprintf("'%s' belongs to ecosystem '%s'", ds.domain, ds.ecosystem))
				}
			}

			if len(suggestions) > 0 {
				errorMsg += " Did you mean: " + strings.Join(suggestions, ", ") + "?"
			}

			errorMsg += " Set 'strict: false' to use custom domains. See: https://github.github.com/gh-aw/reference/network/"

			return fmt.Errorf("%s", errorMsg)
		}
	}

	// Only apply firewall validation to copilot and codex engines
	if engineID != "copilot" && engineID != "codex" {
		strictModeValidationLog.Printf("Engine '%s' does not support firewall, skipping firewall validation", engineID)
		return nil
	}

	// Check if SRT is enabled (SRT and AWF are mutually exclusive)
	if sandboxConfig != nil {
		// Check legacy Type field
		if sandboxConfig.Type == SandboxTypeRuntime {
			strictModeValidationLog.Printf("SRT sandbox is enabled (via Type), skipping firewall validation")
			return nil
		}
		// Check new Agent field
		if sandboxConfig.Agent != nil {
			agentType := getAgentType(sandboxConfig.Agent)
			if agentType == SandboxTypeRuntime || agentType == SandboxTypeSRT {
				strictModeValidationLog.Printf("SRT sandbox is enabled (via Agent), skipping firewall validation")
				return nil
			}
		}
	}

	// If network permissions don't exist, that's fine (will default to "defaults")
	if networkPermissions == nil {
		strictModeValidationLog.Printf("No network permissions, skipping firewall validation")
		return nil
	}

	// Check if allowed contains "*" (unrestricted network access)
	// If it does, firewall is not required
	for _, domain := range networkPermissions.Allowed {
		if domain == "*" {
			strictModeValidationLog.Printf("Wildcard '*' in allowed domains, skipping firewall validation")
			return nil
		}
	}

	// At this point, we have network domains (or defaults) and copilot/codex engine
	// In strict mode, firewall MUST be enabled
	if networkPermissions.Firewall == nil || !networkPermissions.Firewall.Enabled {
		strictModeValidationLog.Printf("Firewall validation failed: firewall not enabled in strict mode")
		return fmt.Errorf("strict mode: firewall must be enabled for %s engine with network restrictions. The firewall should be enabled by default, but if you've explicitly disabled it with 'network.firewall: false' or 'sandbox.agent: false', this is not allowed in strict mode for security reasons. See: https://github.github.com/gh-aw/reference/network/", engineID)
	}

	strictModeValidationLog.Printf("Firewall validation passed")
	return nil
}
