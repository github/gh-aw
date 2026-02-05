// This file provides MCP (Model Context Protocol) configuration validation.
//
// # MCP Configuration Validation
//
// This file validates MCP server configurations in agentic workflows.
// It ensures that MCP configurations have required fields, correct types,
// and satisfy type-specific requirements (stdio vs http).
//
// # Validation Functions
//
//   - ValidateMCPConfigs() - Validates all MCP configurations in tools section
//   - validateStringProperty() - Validates that a property is a string type
//   - validateMCPRequirements() - Validates type-specific MCP requirements
//
// # Validation Pattern: Schema and Requirements Validation
//
// MCP validation uses multiple patterns:
//   - Type inference: Determines MCP type from fields if not explicit
//   - Required field validation: Ensures necessary fields exist
//   - Type-specific validation: Different requirements for stdio vs http
//   - Property type checking: Validates field types match expectations
//
// # MCP Types and Requirements
//
// ## stdio type
//   - Requires either 'command' or 'container' (but not both)
//   - Optional: version, args, entrypointArgs, env, proxy-args, registry
//
// ## http type
//   - Requires 'url' field
//   - Cannot use 'container' field
//   - Optional: headers, registry
//
// # When to Add Validation Here
//
// Add validation to this file when:
//   - It validates MCP server configuration
//   - It checks MCP-specific field requirements
//   - It validates MCP type compatibility
//   - It ensures MCP configuration correctness
//
// For general validation, see validation.go.
// For detailed documentation, see scratchpad/validation-architecture.md

package workflow

import (
	"fmt"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var mcpValidationLog = logger.New("workflow:mcp_config_validation")

// ValidateMCPConfigs validates all MCP configurations in the tools section using JSON schema
func ValidateMCPConfigs(tools map[string]any) error {
	mcpValidationLog.Printf("Validating MCP configurations for %d tools", len(tools))

	// List of built-in tools that have their own validation logic
	// These tools should not be validated as custom MCP servers
	builtInTools := map[string]bool{
		"github":            true,
		"playwright":        true,
		"serena":            true,
		"agentic-workflows": true,
		"cache-memory":      true,
		"repo-memory":       true,
		"bash":              true,
		"edit":              true,
		"web-fetch":         true,
		"web-search":        true,
		"safety-prompt":     true,
		"timeout":           true,
		"startup-timeout":   true,
	}

	for toolName, toolConfig := range tools {
		// Skip built-in tools - they have their own schema validation
		if builtInTools[toolName] {
			mcpValidationLog.Printf("Skipping MCP validation for built-in tool: %s", toolName)
			continue
		}

		if config, ok := toolConfig.(map[string]any); ok {
			// Extract raw MCP configuration (without transformation)
			mcpConfig, err := getRawMCPConfig(config)
			if err != nil {
				mcpValidationLog.Printf("Invalid MCP configuration for tool %s: %v", toolName, err)
				return fmt.Errorf("tool '%s' has invalid MCP configuration: %w", toolName, err)
			}

			// Skip validation if no MCP configuration found
			if len(mcpConfig) == 0 {
				continue
			}

			mcpValidationLog.Printf("Validating MCP requirements for tool: %s", toolName)

			// Validate MCP configuration requirements (before transformation)
			if err := validateMCPRequirements(toolName, mcpConfig, config); err != nil {
				return err
			}
		}
	}

	mcpValidationLog.Print("MCP configuration validation completed successfully")
	return nil
}

// getRawMCPConfig extracts MCP configuration without any transformations for validation
func getRawMCPConfig(toolConfig map[string]any) (map[string]any, error) {
	result := make(map[string]any)

	// List of MCP fields that can be direct children of the tool config
	// Note: "args" is NOT included here because it's used for built-in tools (github, playwright)
	// to add custom arguments without triggering custom MCP tool processing logic. Including "args"
	// would incorrectly classify built-in tools as custom MCP tools, changing their processing behavior
	// and causing validation errors.
	mcpFields := []string{"type", "url", "command", "container", "env", "headers"}

	// List of all known tool config fields (not just MCP)
	knownToolFields := map[string]bool{
		"type":            true,
		"url":             true,
		"command":         true,
		"container":       true,
		"env":             true,
		"headers":         true,
		"version":         true,
		"args":            true,
		"entrypoint":      true,
		"entrypointArgs":  true,
		"mounts":          true,
		"proxy-args":      true,
		"registry":        true,
		"allowed":         true,
		"mode":            true, // for github tool
		"github-token":    true, // for github tool
		"read-only":       true, // for github tool
		"toolsets":        true, // for github tool
		"id":              true, // for cache-memory (array notation)
		"key":             true, // for cache-memory
		"description":     true, // for cache-memory
		"retention-days":  true, // for cache-memory
		"allowed_domains": true, // for playwright tool
		"allowed-domains": true, // for playwright tool (alternative notation)
	}

	// Check new format: direct fields in tool config
	for _, field := range mcpFields {
		if value, exists := toolConfig[field]; exists {
			result[field] = value
		}
	}

	// Check for unknown fields that might be typos or deprecated (like "network")
	for field := range toolConfig {
		if !knownToolFields[field] {
			// Build list of valid fields for the error message
			validFields := []string{}
			for k := range knownToolFields {
				validFields = append(validFields, k)
			}
			sort.Strings(validFields)
			maxFields := 10
			if len(validFields) < maxFields {
				maxFields = len(validFields)
			}
			return nil, fmt.Errorf("unknown property '%s' in tool configuration. Valid properties include: %s.\n\nExample:\ntools:\n  my-tool:\n    command: \"node server.js\"\n    args: [\"--verbose\"]\n\nSee: %s", field, strings.Join(validFields[:maxFields], ", "), constants.DocsToolsURL)
		}
	}

	return result, nil
}

// getTypeString returns a human-readable type name for error messages
func getTypeString(value any) string {
	if value == nil {
		return "null"
	}

	switch value.(type) {
	case int, int64, float64, float32:
		return "number"
	case bool:
		return "boolean"
	case map[string]any:
		return "object"
	case string:
		return "string"
	default:
		// Check if it's any kind of slice/array by examining the type string
		typeStr := fmt.Sprintf("%T", value)
		if strings.HasPrefix(typeStr, "[]") {
			return "array"
		}
		return "unknown"
	}
}

// validateStringProperty validates that a property is a string and returns appropriate error message
func validateStringProperty(toolName, propertyName string, value any, exists bool) error {
	if !exists {
		return fmt.Errorf("tool '%s' mcp configuration missing required property '%s'.\n\nExample:\ntools:\n  %s:\n    %s: \"value\"\n\nSee: %s", toolName, propertyName, toolName, propertyName, constants.DocsToolsURL)
	}
	if _, ok := value.(string); !ok {
		return fmt.Errorf("tool '%s' mcp configuration property '%s' must be a string, got %T.\n\nExample:\ntools:\n  %s:\n    %s: \"my-value\"\n\nSee: %s", toolName, propertyName, value, toolName, propertyName, constants.DocsToolsURL)
	}
	return nil
}

// validateMCPRequirements validates the specific requirements for MCP configuration
func validateMCPRequirements(toolName string, mcpConfig map[string]any, toolConfig map[string]any) error {
	// Validate 'type' property - allow inference from other fields
	mcpType, hasType := mcpConfig["type"]
	var typeStr string

	if hasType {
		// Explicit type provided - validate it's a string
		if _, ok := mcpType.(string); !ok {
			return fmt.Errorf("tool '%s' mcp configuration 'type' must be a string, got %T. Valid types per MCP Gateway Specification: stdio, http. Note: 'local' is accepted for backward compatibility and treated as 'stdio'.\n\nExample:\ntools:\n  %s:\n    type: \"stdio\"\n    command: \"node server.js\"\n\nSee: %s", toolName, mcpType, toolName, constants.DocsToolsURL)
		}
		typeStr = mcpType.(string)
	} else {
		// Infer type from presence of fields
		if _, hasURL := mcpConfig["url"]; hasURL {
			typeStr = "http"
		} else if _, hasCommand := mcpConfig["command"]; hasCommand {
			typeStr = "stdio"
		} else if _, hasContainer := mcpConfig["container"]; hasContainer {
			typeStr = "stdio"
		} else {
			return fmt.Errorf("tool '%s' unable to determine MCP type: missing type, url, command, or container.\n\nExample:\ntools:\n  %s:\n    command: \"node server.js\"\n    args: [\"--port\", \"3000\"]\n\nSee: %s", toolName, toolName, constants.DocsToolsURL)
		}
	}

	// Normalize "local" to "stdio" for validation
	if typeStr == "local" {
		typeStr = "stdio"
	}

	// Validate type is one of the supported types
	if !parser.IsMCPType(typeStr) {
		return fmt.Errorf("tool '%s' mcp configuration 'type' must be one of: stdio, http (per MCP Gateway Specification). Note: 'local' is accepted for backward compatibility and treated as 'stdio'. Got: %s.\n\nExample:\ntools:\n  %s:\n    type: \"stdio\"\n    command: \"node server.js\"\n\nSee: %s", toolName, typeStr, toolName, constants.DocsToolsURL)
	}

	// Validate type-specific requirements
	switch typeStr {
	case "http":
		// HTTP type requires 'url' property
		url, hasURL := mcpConfig["url"]

		// HTTP type cannot use container field
		if _, hasContainer := mcpConfig["container"]; hasContainer {
			return fmt.Errorf("tool '%s' mcp configuration with type 'http' cannot use 'container' field. HTTP MCP uses URL endpoints, not containers.\n\nExample:\ntools:\n  %s:\n    type: http\n    url: \"https://api.example.com/mcp\"\n    headers:\n      Authorization: \"Bearer ${{ secrets.API_KEY }}\"\n\nSee: %s", toolName, toolName, constants.DocsToolsURL)
		}

		return validateStringProperty(toolName, "url", url, hasURL)

	case "stdio":
		// stdio type requires either 'command' or 'container' property (but not both)
		command, hasCommand := mcpConfig["command"]
		container, hasContainer := mcpConfig["container"]

		if hasCommand && hasContainer {
			return fmt.Errorf("tool '%s' mcp configuration cannot specify both 'container' and 'command'. Choose one.\n\nExample (command):\ntools:\n  %s:\n    command: \"node server.js\"\n\nExample (container):\ntools:\n  %s:\n    container: \"my-registry/my-tool\"\n    version: \"latest\"\n\nSee: %s", toolName, toolName, toolName, constants.DocsToolsURL)
		}

		if hasCommand {
			if err := validateStringProperty(toolName, "command", command, true); err != nil {
				return err
			}
		} else if hasContainer {
			if err := validateStringProperty(toolName, "container", container, true); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("tool '%s' mcp configuration must specify either 'command' or 'container'.\n\nExample (command):\ntools:\n  %s:\n    command: \"node server.js\"\n    args: [\"--port\", \"3000\"]\n\nExample (container):\ntools:\n  %s:\n    container: \"my-registry/my-tool\"\n    version: \"latest\"\n\nSee: %s", toolName, toolName, toolName, constants.DocsToolsURL)
		}
	}

	return nil
}
