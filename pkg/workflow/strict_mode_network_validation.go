// This file contains strict mode network validation functions.
//
// It validates network configuration, MCP network requirements, and tools
// configuration for workflows compiled with the --strict flag.

package workflow

import (
	"errors"
	"fmt"
	"slices"
)

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
		return errors.New("internal error: network permissions not initialized (this should not happen in normal operation)")
	}

	// If allowed list contains "defaults", that's acceptable (this is the automatic default)
	if slices.Contains(networkPermissions.Allowed, "defaults") {
		strictModeValidationLog.Printf("Network validation passed: allowed list contains 'defaults'")
		return nil
	}

	// Check for wildcard "*" in allowed domains
	if slices.Contains(networkPermissions.Allowed, "*") {
		strictModeValidationLog.Printf("Network validation failed: wildcard detected")
		return NewValidationError(
			"network.allowed",
			"*",
			"strict mode: wildcard '*' is not allowed in network.allowed domains to prevent unrestricted internet access; expected explicit domains or ecosystem identifiers",
			"Use specific domains or supported ecosystem identifiers:\n\nnetwork:\n  allowed:\n    - github.com\n    - api.github.com\n    - python",
		)
	}

	strictModeValidationLog.Printf("Network validation passed: allowed_count=%d", len(networkPermissions.Allowed))
	return nil
}

// validateStrictMCPNetwork requires top-level network configuration when custom MCP servers use containers
func (c *Compiler) validateStrictMCPNetwork(frontmatter map[string]any, networkPermissions *NetworkPermissions) error {
	// Check mcp-servers section (new format)
	mcpServersValue, exists := frontmatter["mcp-servers"]
	if !exists {
		strictModeValidationLog.Print("No mcp-servers section, skipping MCP network validation")
		return nil
	}

	mcpServersMap, ok := mcpServersValue.(map[string]any)
	if !ok {
		strictModeValidationLog.Print("mcp-servers is not a map, skipping MCP network validation")
		return nil
	}

	// Check if top-level network configuration exists
	hasTopLevelNetwork := networkPermissions != nil && len(networkPermissions.Allowed) > 0
	strictModeValidationLog.Printf("Checking %d MCP servers for container network requirements: hasTopLevelNetwork=%t", len(mcpServersMap), hasTopLevelNetwork)

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
					return NewValidationError(
						"network.allowed",
						serverName,
						fmt.Sprintf("strict mode: custom MCP server '%s' with container must have top-level network configuration for security; expected top-level network restrictions for container-based MCP servers", serverName),
						fmt.Sprintf("Add explicit top-level network permissions for mcp-servers.%s:\n\nnetwork:\n  allowed:\n    - github.com\n    - api.github.com\n\nmcp-servers:\n  %s:\n    type: stdio\n    container: ghcr.io/example/mcp-server:latest", serverName, serverName),
					)
				}
			}
		}
	}

	return nil
}

// validateStrictTools validates tools configuration in strict mode
func (c *Compiler) validateStrictTools(frontmatter map[string]any) error {
	toolsValue, exists := frontmatter["tools"]
	if !exists {
		strictModeValidationLog.Print("No tools section, skipping strict tools validation")
		return nil
	}

	toolsMap, ok := toolsValue.(map[string]any)
	if !ok {
		strictModeValidationLog.Print("tools is not a map, skipping strict tools validation")
		return nil
	}

	if githubValue, hasGitHub := toolsMap["github"]; hasGitHub {
		if githubMap, ok := githubValue.(map[string]any); ok {
			if value, exists := githubMap["private-to-public-flows"]; exists {
				return NewValidationError(
					"tools.github.private-to-public-flows",
					fmt.Sprintf("%v", value),
					"strict mode: tools.github.private-to-public-flows is not allowed because private-to-public flows can expose private data through public action logs or public destinations used by the agent",
					"Remove private-to-public-flows:\n\ntools:\n  github:\n    mode: local",
				)
			}
		}
	}

	if err := validateStrictMinIntegrityTools(toolsMap); err != nil {
		return err
	}
	if err := validateStrictCLIProxyTools(toolsMap); err != nil {
		return err
	}
	return validateStrictCacheMemoryTools(toolsMap)
}

// validateStrictMinIntegrityTools ensures shell access is intentional when
// min-integrity is none.
func validateStrictMinIntegrityTools(toolsMap map[string]any) error {
	if githubValue, hasGitHub := toolsMap["github"]; hasGitHub {
		if githubMap, ok := githubValue.(map[string]any); ok {
			if minIntegrity, exists := githubMap["min-integrity"]; exists {
				if minIntegrityStr, ok := minIntegrity.(string); ok && minIntegrityStr == "none" {
					_, hasBash := toolsMap["bash"]
					if !hasBash {
						strictModeValidationLog.Printf("min-integrity: none without explicit bash setting rejected in strict mode")
						return NewValidationError(
							"tools.bash",
							"not specified",
							"strict mode: when 'tools.github.min-integrity' is set to 'none', 'tools.bash' must be explicitly specified so that shell access is intentional",
							"Add an explicit bash setting to the tools section:\n\ntools:\n  bash: [\"cat\", \"ls\", \"find\", \"grep\", \"head\", \"tail\", \"wc\"]\n  github:\n    min-integrity: none",
						)
					}
				}
			}
		}
	}
	return nil
}

// validateStrictCLIProxyTools requires cli-proxy to be explicitly disabled
// when bash is refused because CLI-mounted MCP servers require a shell.
func validateStrictCLIProxyTools(toolsMap map[string]any) error {
	if isBashExplicitlyRefused(toolsMap) {
		cliProxyValue, hasCLIProxy := toolsMap["cli-proxy"]
		enabled, isBool := cliProxyValue.(bool)
		if !hasCLIProxy || !isBool || enabled {
			strictModeValidationLog.Print("bash disabled without explicit cli-proxy: false rejected in strict mode")
			value := "not specified"
			if hasCLIProxy {
				value = fmt.Sprintf("%v", cliProxyValue)
			}
			return NewValidationError(
				"tools.cli-proxy",
				value,
				"strict mode: when 'tools.bash' is disabled, 'tools.cli-proxy: false' must be set explicitly because CLI-mounted MCP servers can only be invoked from a shell",
				"Add an explicit cli-proxy setting to the tools section:\n\ntools:\n  bash: false\n  cli-proxy: false\n\nRun 'gh aw fix' to apply this change automatically.",
			)
		}
		if mode, enabled := IsGitHubCLIProxyMode(toolsMap); enabled {
			strictModeValidationLog.Print("bash disabled with tools.github.mode: gh-proxy rejected in strict mode")
			return NewValidationError(
				"tools.github.mode",
				mode,
				"strict mode: when 'tools.bash' is disabled, 'tools.github.mode: gh-proxy' is not allowed because GitHub gh-proxy reads can only be invoked from a shell",
				"Use an MCP-backed GitHub mode instead:\n\ntools:\n  bash: false\n  cli-proxy: false\n  github:\n    mode: local\n\nRun 'gh aw fix' to apply this change automatically.",
			)
		}
	}
	return nil
}

// validateStrictCacheMemoryTools rejects repository-scoped cache memory.
func validateStrictCacheMemoryTools(toolsMap map[string]any) error {
	cacheMemoryValue, hasCacheMemory := toolsMap["cache-memory"]
	if !hasCacheMemory {
		return nil
	}
	strictModeValidationLog.Print("Checking cache-memory scope in strict mode")
	checkScope := func(cacheMap map[string]any) error {
		if scope, hasScope := cacheMap["scope"]; hasScope {
			if scopeStr, ok := scope.(string); ok && scopeStr == "repo" {
				strictModeValidationLog.Printf("Cache-memory repo scope validation failed")
				return NewValidationError(
					"tools.cache-memory.scope",
					scopeStr,
					"strict mode: cache-memory with 'scope: repo' is not allowed for security reasons; expected 'scope: workflow' to isolate cache data per workflow",
					"Use workflow-scoped cache entries:\n\ntools:\n  cache-memory:\n    key: my-cache\n    scope: workflow",
				)
			}
		}
		return nil
	}

	if cacheMemoryConfig, ok := cacheMemoryValue.(map[string]any); ok {
		return checkScope(cacheMemoryConfig)
	}
	if cacheMemoryArray, ok := cacheMemoryValue.([]any); ok {
		for _, item := range cacheMemoryArray {
			if cacheMap, ok := item.(map[string]any); ok {
				if err := checkScope(cacheMap); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// validatePrivateToPublicFlowsStringValue validates that the string form of
// private-to-public-flows uses the only supported value: "allow".
func validatePrivateToPublicFlowsStringValue(workflowData *WorkflowData) error {
	if workflowData == nil || workflowData.ParsedTools == nil || workflowData.ParsedTools.GitHub == nil {
		return nil
	}
	value, ok := workflowData.ParsedTools.GitHub.PrivateToPublicFlows.(string)
	if !ok || value == "" || value == "allow" {
		return nil
	}

	return NewValidationError(
		"tools.github.private-to-public-flows",
		value,
		fmt.Sprintf("invalid value %q; expected \"allow\" or a list of MCP server IDs", value),
		"Set tools.github.private-to-public-flows to \"allow\" for blanket opt-in, or use a list of MCP server IDs for targeted exemptions.",
	)
}

// validatePrivateToPublicFlowsServerIDs validates that every server ID in the list form of
// private-to-public-flows matches a declared MCP server in the workflow's tools list.
// Per MCP Gateway Specification Section 10.9.2, unknown IDs must be rejected at compile time.
func validatePrivateToPublicFlowsServerIDs(workflowData *WorkflowData) error {
	if workflowData == nil || workflowData.ParsedTools == nil || workflowData.ParsedTools.GitHub == nil {
		return nil
	}

	servers, ok := workflowData.ParsedTools.GitHub.PrivateToPublicFlows.([]string)
	if !ok || len(servers) == 0 {
		return nil
	}
	// Collect valid server IDs from the merged tools map.
	validIDs := make(map[string]struct{}, len(workflowData.Tools))
	for id := range workflowData.Tools {
		validIDs[id] = struct{}{}
	}
	var unknown []string
	for _, id := range servers {
		if _, ok := validIDs[id]; !ok {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	return NewValidationError(
		"tools.github.private-to-public-flows",
		fmt.Sprintf("%v", unknown),
		fmt.Sprintf("unknown MCP server ID(s) %v; every ID in private-to-public-flows must match a server declared in tools or mcp-servers", unknown),
		"Check that each server ID in private-to-public-flows matches an entry in tools or mcp-servers. "+
			"The built-in GitHub MCP server ID is \"github\". Custom servers use the key from mcp-servers.",
	)
}
