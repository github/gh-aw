package workflow

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/types"
)

var mcpCustomLog = logger.New("workflow:mcp-config-custom")

// renderCustomMCPConfigWrapper generates custom MCP server configuration wrapper
// This is a shared function used by both Claude and Custom engines
func renderCustomMCPConfigWrapper(yaml *strings.Builder, toolName string, toolConfig map[string]any, isLast bool) error {
	mcpCustomLog.Printf("Rendering custom MCP config wrapper for tool: %s", toolName)
	fmt.Fprintf(yaml, "              \"%s\": {\n", toolName)

	// Use the shared MCP config renderer with JSON format
	renderer := MCPConfigRenderer{
		IndentLevel: "                ",
		Format:      "json",
	}

	err := renderSharedMCPConfig(yaml, toolName, toolConfig, renderer)
	if err != nil {
		return err
	}

	if isLast {
		yaml.WriteString("              }\n")
	} else {
		yaml.WriteString("              },\n")
	}

	return nil
}

// renderCustomMCPConfigWrapperWithContext generates custom MCP server configuration wrapper with workflow context
// This version includes workflowData to determine if localhost URLs should be rewritten
func renderCustomMCPConfigWrapperWithContext(yaml *strings.Builder, toolName string, toolConfig map[string]any, isLast bool, workflowData *WorkflowData) error {
	mcpCustomLog.Printf("Rendering custom MCP config wrapper with context for tool: %s", toolName)
	fmt.Fprintf(yaml, "              \"%s\": {\n", toolName)

	// Determine if localhost URLs should be rewritten to host.docker.internal
	// This is needed when firewall is enabled (agent is not disabled)
	rewriteLocalhost := workflowData != nil && (workflowData.SandboxConfig == nil ||
		workflowData.SandboxConfig.Agent == nil ||
		!workflowData.SandboxConfig.Agent.Disabled)

	// Use the shared MCP config renderer with JSON format
	renderer := MCPConfigRenderer{
		IndentLevel:              "                ",
		Format:                   "json",
		RewriteLocalhostToDocker: rewriteLocalhost,
	}

	err := renderSharedMCPConfig(yaml, toolName, toolConfig, renderer)
	if err != nil {
		return err
	}

	if isLast {
		yaml.WriteString("              }\n")
	} else {
		yaml.WriteString("              },\n")
	}

	return nil
}

// renderSharedMCPConfig generates MCP server configuration for a single tool using shared logic
// This function handles the common logic for rendering MCP configurations across different engines
func renderSharedMCPConfig(yaml *strings.Builder, toolName string, toolConfig map[string]any, renderer MCPConfigRenderer) error {
	mcpCustomLog.Printf("Rendering MCP config for tool: %s, format: %s", toolName, renderer.Format)

	// Get MCP configuration in the new format
	mcpConfig, err := getMCPConfig(toolConfig, toolName)
	if err != nil {
		mcpCustomLog.Printf("Failed to parse MCP config for tool %s: %v", toolName, err)
		return fmt.Errorf("failed to parse MCP config for tool '%s': %w", toolName, err)
	}

	// Extract secrets from headers for HTTP MCP tools (copilot engine only)
	var headerSecrets map[string]string
	if mcpConfig.Type == "http" && renderer.RequiresCopilotFields {
		headerSecrets = ExtractSecretsFromMap(mcpConfig.Headers)
	}

	// Determine properties based on type
	var propertyOrder []string
	mcpType := mcpConfig.Type

	switch mcpType {
	case "stdio":
		if renderer.Format == "toml" {
			propertyOrder = []string{"command", "args", "env", "proxy-args", "registry"}
		} else {
			// JSON format - use MCP Gateway schema format (container-based) OR legacy command-based
			// Per MCP Gateway Specification v1.0.0 section 3.2.1, stdio servers SHOULD be containerized
			// But we also support legacy command-based tools for backwards compatibility
			propertyOrder = []string{"type", "container", "entrypoint", "entrypointArgs", "mounts", "command", "args", "tools", "env", "proxy-args", "registry"}
		}
	case "http":
		if renderer.Format == "toml" {
			// TOML format for HTTP MCP servers uses url and http_headers
			propertyOrder = []string{"url", "http_headers"}
		} else {
			// JSON format - include copilot fields if required
			if renderer.RequiresCopilotFields {
				// For HTTP MCP with secrets in headers, env passthrough is needed
				if len(headerSecrets) > 0 {
					propertyOrder = []string{"type", "url", "headers", "tools", "env"}
				} else {
					propertyOrder = []string{"type", "url", "headers", "tools"}
				}
			} else {
				propertyOrder = []string{"type", "url", "headers"}
			}
		}
	default:
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Custom MCP server '%s' has unsupported type '%s'. Supported types: stdio, http", toolName, mcpType)))
		return nil
	}

	// Find which properties actually exist in this config
	var existingProperties []string
	for _, prop := range propertyOrder {
		switch prop {
		case "type":
			// Include type field only for engines that require copilot fields
			existingProperties = append(existingProperties, prop)
		case "tools":
			// Include tools field only for engines that require copilot fields
			if renderer.RequiresCopilotFields {
				existingProperties = append(existingProperties, prop)
			}
		case "container":
			if mcpConfig.Container != "" {
				existingProperties = append(existingProperties, prop)
			}
		case "entrypoint":
			if mcpConfig.Entrypoint != "" {
				existingProperties = append(existingProperties, prop)
			}
		case "entrypointArgs":
			if len(mcpConfig.EntrypointArgs) > 0 {
				existingProperties = append(existingProperties, prop)
			}
		case "mounts":
			if len(mcpConfig.Mounts) > 0 {
				existingProperties = append(existingProperties, prop)
			}
		case "command":
			if mcpConfig.Command != "" {
				existingProperties = append(existingProperties, prop)
			}
		case "args":
			if len(mcpConfig.Args) > 0 {
				existingProperties = append(existingProperties, prop)
			}
		case "env":
			// Include env if there are existing env vars OR if there are header secrets to passthrough
			if len(mcpConfig.Env) > 0 || len(headerSecrets) > 0 {
				existingProperties = append(existingProperties, prop)
			}
		case "url":
			if mcpConfig.URL != "" {
				existingProperties = append(existingProperties, prop)
			}
		case "headers":
			if len(mcpConfig.Headers) > 0 {
				existingProperties = append(existingProperties, prop)
			}
		case "http_headers":
			if len(mcpConfig.Headers) > 0 {
				existingProperties = append(existingProperties, prop)
			}
		case "proxy-args":
			if len(mcpConfig.ProxyArgs) > 0 {
				existingProperties = append(existingProperties, prop)
			}
		case "registry":
			if mcpConfig.Registry != "" {
				existingProperties = append(existingProperties, prop)
			}
		}
	}

	// If no valid properties exist, skip rendering
	if len(existingProperties) == 0 {
		return nil
	}

	// Render properties based on format
	for propIndex, property := range existingProperties {
		isLast := propIndex == len(existingProperties)-1

		switch property {
		case "type":
			// Render type field for JSON format (copilot engine)
			comma := ","
			if isLast {
				comma = ""
			}
			// Type field - per MCP Gateway Specification v1.0.0
			// Use "stdio" for containerized servers, "http" for HTTP servers
			typeValue := mcpConfig.Type
			fmt.Fprintf(yaml, "%s\"type\": \"%s\"%s\n", renderer.IndentLevel, typeValue, comma)
		case "tools":
			// Render tools field for JSON format (copilot engine) - default to all tools
			comma := ","
			if isLast {
				comma = ""
			}
			// Check if allowed tools are specified, otherwise default to "*"
			if len(mcpConfig.Allowed) > 0 {
				fmt.Fprintf(yaml, "%s\"tools\": [\n", renderer.IndentLevel)
				for toolIndex, tool := range mcpConfig.Allowed {
					toolComma := ","
					if toolIndex == len(mcpConfig.Allowed)-1 {
						toolComma = ""
					}
					fmt.Fprintf(yaml, "%s  \"%s\"%s\n", renderer.IndentLevel, tool, toolComma)
				}
				fmt.Fprintf(yaml, "%s]%s\n", renderer.IndentLevel, comma)
			} else {
				fmt.Fprintf(yaml, "%s\"tools\": [\n", renderer.IndentLevel)
				fmt.Fprintf(yaml, "%s  \"*\"\n", renderer.IndentLevel)
				fmt.Fprintf(yaml, "%s]%s\n", renderer.IndentLevel, comma)
			}
		case "container":
			comma := ","
			if isLast {
				comma = ""
			}
			// Container field - per MCP Gateway Specification v1.0.0 section 4.1.2
			// Required for stdio servers (containerized servers)
			fmt.Fprintf(yaml, "%s\"container\": \"%s\"%s\n", renderer.IndentLevel, mcpConfig.Container, comma)
		case "entrypoint":
			comma := ","
			if isLast {
				comma = ""
			}
			// Entrypoint field - per MCP Gateway Specification v1.0.0
			// Optional entrypoint override for container
			fmt.Fprintf(yaml, "%s\"entrypoint\": \"%s\"%s\n", renderer.IndentLevel, mcpConfig.Entrypoint, comma)
		case "entrypointArgs":
			comma := ","
			if isLast {
				comma = ""
			}
			// EntrypointArgs field - per MCP Gateway Specification v1.0.0
			// Arguments passed to the container entrypoint
			fmt.Fprintf(yaml, "%s\"entrypointArgs\": [\n", renderer.IndentLevel)
			for argIndex, arg := range mcpConfig.EntrypointArgs {
				argComma := ","
				if argIndex == len(mcpConfig.EntrypointArgs)-1 {
					argComma = ""
				}
				fmt.Fprintf(yaml, "%s  \"%s\"%s\n", renderer.IndentLevel, arg, argComma)
			}
			fmt.Fprintf(yaml, "%s]%s\n", renderer.IndentLevel, comma)
		case "mounts":
			comma := ","
			if isLast {
				comma = ""
			}
			// Mounts field - per MCP Gateway Specification v1.0.0
			// Volume mounts for the container
			fmt.Fprintf(yaml, "%s\"mounts\": [\n", renderer.IndentLevel)
			for mountIndex, mount := range mcpConfig.Mounts {
				mountComma := ","
				if mountIndex == len(mcpConfig.Mounts)-1 {
					mountComma = ""
				}
				fmt.Fprintf(yaml, "%s  \"%s\"%s\n", renderer.IndentLevel, mount, mountComma)
			}
			fmt.Fprintf(yaml, "%s]%s\n", renderer.IndentLevel, comma)
		case "command":
			if renderer.Format == "toml" {
				fmt.Fprintf(yaml, "%scommand = \"%s\"\n", renderer.IndentLevel, mcpConfig.Command)
			} else {
				comma := ","
				if isLast {
					comma = ""
				}
				fmt.Fprintf(yaml, "%s\"command\": \"%s\"%s\n", renderer.IndentLevel, mcpConfig.Command, comma)
			}
		case "args":
			if renderer.Format == "toml" {
				fmt.Fprintf(yaml, "%sargs = [\n", renderer.IndentLevel)
				for _, arg := range mcpConfig.Args {
					fmt.Fprintf(yaml, "%s  \"%s\",\n", renderer.IndentLevel, arg)
				}
				fmt.Fprintf(yaml, "%s]\n", renderer.IndentLevel)
			} else {
				comma := ","
				if isLast {
					comma = ""
				}
				fmt.Fprintf(yaml, "%s\"args\": [\n", renderer.IndentLevel)
				for argIndex, arg := range mcpConfig.Args {
					argComma := ","
					if argIndex == len(mcpConfig.Args)-1 {
						argComma = ""
					}
					fmt.Fprintf(yaml, "%s  \"%s\"%s\n", renderer.IndentLevel, arg, argComma)
				}
				fmt.Fprintf(yaml, "%s]%s\n", renderer.IndentLevel, comma)
			}
		case "env":
			if renderer.Format == "toml" {
				fmt.Fprintf(yaml, "%senv = { ", renderer.IndentLevel)
				// Using functional helper to extract map keys
				envKeys := sliceutil.MapToSlice(mcpConfig.Env)
				sort.Strings(envKeys)
				for i, envKey := range envKeys {
					if i > 0 {
						yaml.WriteString(", ")
					}
					fmt.Fprintf(yaml, "\"%s\" = \"%s\"", envKey, mcpConfig.Env[envKey])
				}
				yaml.WriteString(" }\n")
			} else {
				comma := ","
				if isLast {
					comma = ""
				}
				fmt.Fprintf(yaml, "%s\"env\": {\n", renderer.IndentLevel)

				// CWE-190: Allocation Size Overflow Prevention
				// Instead of pre-calculating capacity (len(mcpConfig.Env)+len(headerSecrets)),
				// which could overflow if the maps are extremely large, we let Go's append
				// handle capacity growth automatically. This is safe and efficient for
				// environment variable maps which are typically small in practice.
				var envKeys []string
				for key := range mcpConfig.Env {
					envKeys = append(envKeys, key)
				}
				// Add header secrets for passthrough (copilot only)
				for varName := range headerSecrets {
					// Only add if not already in env
					if _, exists := mcpConfig.Env[varName]; !exists {
						envKeys = append(envKeys, varName)
					}
				}
				sort.Strings(envKeys)

				for envIndex, envKey := range envKeys {
					envComma := ","
					if envIndex == len(envKeys)-1 {
						envComma = ""
					}

					// Check if this is a header secret (needs passthrough)
					if _, isHeaderSecret := headerSecrets[envKey]; isHeaderSecret && renderer.RequiresCopilotFields {
						// Use passthrough syntax: "VAR_NAME": "\\${VAR_NAME}"
						fmt.Fprintf(yaml, "%s  \"%s\": \"\\${%s}\"%s\n", renderer.IndentLevel, envKey, envKey, envComma)
					} else {
						// Use existing env value
						fmt.Fprintf(yaml, "%s  \"%s\": \"%s\"%s\n", renderer.IndentLevel, envKey, mcpConfig.Env[envKey], envComma)
					}
				}
				fmt.Fprintf(yaml, "%s}%s\n", renderer.IndentLevel, comma)
			}
		case "url":
			// Rewrite localhost URLs to host.docker.internal when running inside firewall container
			// This allows MCP servers running on the host to be accessed from the container
			urlValue := mcpConfig.URL
			if renderer.RewriteLocalhostToDocker {
				urlValue = rewriteLocalhostToDockerHost(urlValue)
			}
			if renderer.Format == "toml" {
				fmt.Fprintf(yaml, "%surl = \"%s\"\n", renderer.IndentLevel, urlValue)
			} else {
				comma := ","
				if isLast {
					comma = ""
				}
				fmt.Fprintf(yaml, "%s\"url\": \"%s\"%s\n", renderer.IndentLevel, urlValue, comma)
			}
		case "http_headers":
			// TOML format for HTTP headers (Codex style)
			if len(mcpConfig.Headers) > 0 {
				fmt.Fprintf(yaml, "%shttp_headers = { ", renderer.IndentLevel)
				// Using functional helper to extract map keys
				headerKeys := sliceutil.MapToSlice(mcpConfig.Headers)
				sort.Strings(headerKeys)
				for i, headerKey := range headerKeys {
					if i > 0 {
						yaml.WriteString(", ")
					}
					fmt.Fprintf(yaml, "\"%s\" = \"%s\"", headerKey, mcpConfig.Headers[headerKey])
				}
				yaml.WriteString(" }\n")
			}
		case "headers":
			comma := ","
			if isLast {
				comma = ""
			}
			fmt.Fprintf(yaml, "%s\"headers\": {\n", renderer.IndentLevel)
			// Using functional helper to extract map keys
			headerKeys := sliceutil.MapToSlice(mcpConfig.Headers)
			sort.Strings(headerKeys)
			for headerIndex, headerKey := range headerKeys {
				headerComma := ","
				if headerIndex == len(headerKeys)-1 {
					headerComma = ""
				}

				// Replace secret expressions with env var references for copilot
				headerValue := mcpConfig.Headers[headerKey]
				if renderer.RequiresCopilotFields && len(headerSecrets) > 0 {
					headerValue = ReplaceSecretsWithEnvVars(headerValue, headerSecrets)
				}

				fmt.Fprintf(yaml, "%s  \"%s\": \"%s\"%s\n", renderer.IndentLevel, headerKey, headerValue, headerComma)
			}
			fmt.Fprintf(yaml, "%s}%s\n", renderer.IndentLevel, comma)
		case "proxy-args":
			if renderer.Format == "toml" {
				fmt.Fprintf(yaml, "%sproxy_args = [\n", renderer.IndentLevel)
				for _, arg := range mcpConfig.ProxyArgs {
					fmt.Fprintf(yaml, "%s  \"%s\",\n", renderer.IndentLevel, arg)
				}
				fmt.Fprintf(yaml, "%s]\n", renderer.IndentLevel)
			} else {
				comma := ","
				if isLast {
					comma = ""
				}
				fmt.Fprintf(yaml, "%s\"proxy-args\": [\n", renderer.IndentLevel)
				for argIndex, arg := range mcpConfig.ProxyArgs {
					argComma := ","
					if argIndex == len(mcpConfig.ProxyArgs)-1 {
						argComma = ""
					}
					fmt.Fprintf(yaml, "%s  \"%s\"%s\n", renderer.IndentLevel, arg, argComma)
				}
				fmt.Fprintf(yaml, "%s]%s\n", renderer.IndentLevel, comma)
			}
		case "registry":
			if renderer.Format == "toml" {
				fmt.Fprintf(yaml, "%sregistry = \"%s\"\n", renderer.IndentLevel, mcpConfig.Registry)
			} else {
				comma := ","
				if isLast {
					comma = ""
				}
				fmt.Fprintf(yaml, "%s\"registry\": \"%s\"%s\n", renderer.IndentLevel, mcpConfig.Registry, comma)
			}
		}
	}

	return nil
}

// collectHTTPMCPHeaderSecrets collects all secrets from HTTP MCP tool headers
// Returns a map of environment variable names to their secret expressions
func collectHTTPMCPHeaderSecrets(tools map[string]any) map[string]string {
	allSecrets := make(map[string]string)

	for toolName, toolValue := range tools {
		// Check if this is an MCP tool configuration
		if toolConfig, ok := toolValue.(map[string]any); ok {
			if hasMcp, mcpType := hasMCPConfig(toolConfig); hasMcp && mcpType == "http" {
				// Extract MCP config to get headers
				if mcpConfig, err := getMCPConfig(toolConfig, toolName); err == nil {
					secrets := ExtractSecretsFromMap(mcpConfig.Headers)
					for varName, expr := range secrets {
						allSecrets[varName] = expr
					}
				}
			}
		}
	}

	return allSecrets
}

// getMCPConfig extracts MCP configuration from a tool config and returns a structured MCPServerConfig
func getMCPConfig(toolConfig map[string]any, toolName string) (*parser.MCPServerConfig, error) {
	mcpCustomLog.Printf("Extracting MCP config for tool: %s", toolName)

	config := MapToolConfig(toolConfig)
	result := &parser.MCPServerConfig{
		BaseMCPServerConfig: types.BaseMCPServerConfig{
			Env:     make(map[string]string),
			Headers: make(map[string]string),
		},
		Name: toolName,
	}

	// Validate known properties - fail if unknown properties are found
	knownProperties := map[string]bool{
		"type":           true,
		"mode":           true, // Added for MCPServerConfig struct
		"command":        true,
		"container":      true,
		"version":        true,
		"args":           true,
		"entrypoint":     true,
		"entrypointArgs": true,
		"mounts":         true,
		"env":            true,
		"proxy-args":     true,
		"url":            true,
		"headers":        true,
		"registry":       true,
		"allowed":        true,
		"toolsets":       true, // Added for MCPServerConfig struct
	}

	for key := range toolConfig {
		if !knownProperties[key] {
			mcpCustomLog.Printf("Unknown property '%s' in MCP config for tool '%s'", key, toolName)
			// Build list of valid properties
			validProps := []string{}
			for prop := range knownProperties {
				validProps = append(validProps, prop)
			}
			sort.Strings(validProps)
			return nil, fmt.Errorf(
				"unknown property '%s' in MCP configuration for tool '%s'. Valid properties are: %s. "+
					"Example:\n"+
					"mcp-servers:\n"+
					"  %s:\n"+
					"    command: \"npx @my/tool\"\n"+
					"    args: [\"--port\", \"3000\"]",
				key, toolName, strings.Join(validProps, ", "), toolName)
		}
	}

	// Infer type from fields if not explicitly provided
	if typeStr, hasType := config.GetString("type"); hasType {
		mcpCustomLog.Printf("MCP type explicitly set to: %s", typeStr)
		// Normalize "local" to "stdio"
		if typeStr == "local" {
			result.Type = "stdio"
		} else {
			result.Type = typeStr
		}
	} else {
		mcpCustomLog.Print("No explicit MCP type, inferring from fields")
		// Infer type from presence of fields
		if _, hasURL := config.GetString("url"); hasURL {
			result.Type = "http"
			mcpCustomLog.Printf("Inferred MCP type as http (has url field)")
		} else if _, hasCommand := config.GetString("command"); hasCommand {
			result.Type = "stdio"
			mcpCustomLog.Printf("Inferred MCP type as stdio (has command field)")
		} else if _, hasContainer := config.GetString("container"); hasContainer {
			result.Type = "stdio"
			mcpCustomLog.Printf("Inferred MCP type as stdio (has container field)")
		} else {
			mcpCustomLog.Printf("Unable to determine MCP type for tool '%s': missing type, url, command, or container", toolName)
			return nil, fmt.Errorf(
				"unable to determine MCP type for tool '%s': missing type, url, command, or container. "+
					"Must specify one of: 'type' (stdio/http), 'url' (for HTTP MCP), 'command' (for command-based), or 'container' (for Docker-based). "+
					"Example:\n"+
					"mcp-servers:\n"+
					"  %s:\n"+
					"    command: \"npx @my/tool\"\n"+
					"    args: [\"--port\", \"3000\"]",
				toolName, toolName,
			)
		}
	}

	// Extract common fields (available for both stdio and http)
	if registry, hasRegistry := config.GetString("registry"); hasRegistry {
		result.Registry = registry
	}

	// Extract fields based on type
	mcpCustomLog.Printf("Extracting fields for MCP type: %s", result.Type)
	switch result.Type {
	case "stdio":
		if command, hasCommand := config.GetString("command"); hasCommand {
			result.Command = command
		}
		if container, hasContainer := config.GetString("container"); hasContainer {
			result.Container = container
		}
		if version, hasVersion := config.GetString("version"); hasVersion {
			result.Version = version
		}
		if args, hasArgs := config.GetStringArray("args"); hasArgs {
			result.Args = args
		}
		if entrypoint, hasEntrypoint := config.GetString("entrypoint"); hasEntrypoint {
			result.Entrypoint = entrypoint
		}
		if entrypointArgs, hasEntrypointArgs := config.GetStringArray("entrypointArgs"); hasEntrypointArgs {
			result.EntrypointArgs = entrypointArgs
		}
		if mounts, hasMounts := config.GetStringArray("mounts"); hasMounts {
			result.Mounts = mounts
		}
		if env, hasEnv := config.GetStringMap("env"); hasEnv {
			result.Env = env
		}
		if proxyArgs, hasProxyArgs := config.GetStringArray("proxy-args"); hasProxyArgs {
			result.ProxyArgs = proxyArgs
		}
	case "http":
		if url, hasURL := config.GetString("url"); hasURL {
			result.URL = url
		} else {
			mcpCustomLog.Printf("HTTP MCP tool '%s' missing required 'url' field", toolName)
			return nil, fmt.Errorf(
				"http MCP tool '%s' missing required 'url' field. HTTP MCP servers must specify a URL endpoint. "+
					"Example:\n"+
					"mcp-servers:\n"+
					"  %s:\n"+
					"    type: http\n"+
					"    url: \"https://api.example.com/mcp\"\n"+
					"    headers:\n"+
					"      Authorization: \"Bearer ${{ secrets.API_KEY }}\"",
				toolName, toolName,
			)
		}
		if headers, hasHeaders := config.GetStringMap("headers"); hasHeaders {
			result.Headers = headers
		}
	default:
		mcpCustomLog.Printf("Unsupported MCP type '%s' for tool '%s'", result.Type, toolName)
		return nil, fmt.Errorf(
			"unsupported MCP type '%s' for tool '%s'. Valid types are: stdio, http. "+
				"Example:\n"+
				"mcp-servers:\n"+
				"  %s:\n"+
				"    type: stdio\n"+
				"    command: \"npx @my/tool\"\n"+
				"    args: [\"--port\", \"3000\"]",
			result.Type, toolName, toolName)
	}

	// Extract allowed tools
	if allowed, hasAllowed := config.GetStringArray("allowed"); hasAllowed {
		result.Allowed = allowed
	}

	// Automatically assign well-known containers for stdio MCP servers based on command
	// This ensures all stdio servers work with the MCP Gateway which requires containerization
	if result.Type == "stdio" && result.Container == "" && result.Command != "" {
		containerConfig := getWellKnownContainer(result.Command)
		if containerConfig != nil {
			mcpCustomLog.Printf("Auto-assigning container for command '%s': %s", result.Command, containerConfig.Image)
			result.Container = containerConfig.Image
			result.Entrypoint = containerConfig.Entrypoint
			// Move command to entrypointArgs and preserve existing args after it
			if result.Command != "" {
				result.EntrypointArgs = append([]string{result.Command}, result.Args...)
				result.Args = nil // Clear args since they're now in entrypointArgs
			}
			result.Command = "" // Clear command since it's now the entrypoint
		}
	}

	// Combine container and version fields into a single container image string
	// Per MCP Gateway Specification, the container field should include the full image reference
	// including the tag (e.g., "mcp/ast-grep:latest" instead of separate container + version fields)
	if result.Type == "stdio" && result.Container != "" && result.Version != "" {
		result.Container = result.Container + ":" + result.Version
		result.Version = "" // Clear version since it's now part of container
	}

	return result, nil
}

// hasMCPConfig checks if a tool configuration has MCP configuration
func hasMCPConfig(toolConfig map[string]any) (bool, string) {
	// Check for direct type field
	if mcpType, hasType := toolConfig["type"]; hasType {
		if typeStr, ok := mcpType.(string); ok && parser.IsMCPType(typeStr) {
			// Normalize "local" to "stdio" for consistency
			if typeStr == "local" {
				return true, "stdio"
			}
			return true, typeStr
		}
	}

	// Infer type from presence of fields (same logic as getMCPConfig)
	if _, hasURL := toolConfig["url"]; hasURL {
		return true, "http"
	} else if _, hasCommand := toolConfig["command"]; hasCommand {
		return true, "stdio"
	} else if _, hasContainer := toolConfig["container"]; hasContainer {
		return true, "stdio"
	}

	return false, ""
}
