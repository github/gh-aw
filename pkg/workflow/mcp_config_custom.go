package workflow

import (
	"fmt"
	"maps"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/types"
)

var mcpCustomLog = logger.New("workflow:mcp-config-custom")

// renderCustomMCPConfigWrapperWithContext generates custom MCP server configuration wrapper with workflow context
// This version includes workflowData to determine if localhost URLs should be rewritten
func renderCustomMCPConfigWrapperWithContext(yaml *strings.Builder, toolName string, toolConfig map[string]any, isLast bool, workflowData *WorkflowData) error {
	mcpCustomLog.Printf("Rendering custom MCP config wrapper with context for tool: %s", toolName)
	fmt.Fprintf(yaml, "              \"%s\": {\n", toolName)

	// Determine if localhost URLs should be rewritten to host.docker.internal
	// This is needed when firewall is enabled (agent is not disabled)
	rewriteLocalhost := shouldRewriteLocalhostToDocker(workflowData)

	// Use the shared MCP config renderer with JSON format
	renderer := MCPConfigRenderer{
		IndentLevel:              "                ",
		Format:                   "json",
		RewriteLocalhostToDocker: rewriteLocalhost,
		GuardPolicies:            deriveWriteSinkGuardPolicyFromWorkflow(workflowData),
		ContainerPinMappings:     workflowData.getContainerPinMappings(),
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

// renderCustomMCPEnvVars normalizes custom MCP env values for the target output
// format before serialization.
//
// For TOML output, GitHub Actions template expressions are rewritten to direct
// ${VAR} references because Codex config expects shell-style environment
// expansion. For JSON output, both Copilot and non-Copilot engines use the
// escaped \${VAR} passthrough syntax. The MCP gateway JSON config is written
// inside an unquoted heredoc, so any unescaped ${VAR} reference would be
// expanded by bash before the gateway sees it — splicing the raw secret bytes
// into JSON text. A secret value containing a '"' or '\' character would
// corrupt the JSON. Backslash-escaping (\${VAR}) prevents the heredoc from
// expanding the variable; bash only strips the leading backslash, leaving the
// literal ${VAR} string in the JSON, which the gateway then resolves safely
// from its own environment (RGS-008 compliance).
func renderCustomMCPEnvVars(env map[string]string, tomlFormat bool) map[string]string {
	renderedEnv := make(map[string]string, len(env))
	for envKey, envValue := range env {
		if tomlFormat {
			secrets := ExtractSecretsFromValue(envValue)
			envValue = replaceEnvExpressionsWithPrefixedEnvVars(envValue, "${")
			envValue = ReplaceSecretsWithShellEnvVars(envValue, secrets)
			envValue = strings.ReplaceAll(envValue, "${{ github.workspace }}", "${GITHUB_WORKSPACE}")
		} else {
			// For both Copilot and non-Copilot JSON engines, replace all template
			// expressions with \${VAR} passthrough syntax. This keeps raw secret values
			// out of the heredoc (RGS-008) and produces valid JSON regardless of the
			// characters contained in the secret at runtime.
			envValue = ReplaceTemplateExpressionsWithEnvVars(envValue)
		}
		renderedEnv[envKey] = envValue
	}

	return renderedEnv
}

// renderSharedMCPConfig generates MCP server configuration for a single tool using shared logic
// This function handles the common logic for rendering MCP configurations across different engines
func renderSharedMCPConfig(yaml *strings.Builder, toolName string, toolConfig map[string]any, renderer MCPConfigRenderer) error {
	mcpCustomLog.Printf("Rendering MCP config for tool: %s, format: %s", toolName, renderer.Format)
	mcpConfig, headerSecrets, err := loadSharedMCPConfig(toolConfig, toolName)
	if err != nil {
		return err
	}
	propertyOrder, ok := determineMCPPropertyOrder(toolName, mcpConfig, renderer, headerSecrets)
	if !ok {
		return nil
	}
	existingProperties := collectExistingMCPProperties(propertyOrder, mcpConfig, renderer, headerSecrets)
	if len(existingProperties) == 0 {
		return nil
	}
	renderMCPProperties(yaml, existingProperties, mcpConfig, renderer, headerSecrets)
	renderTrailingGuardPolicies(yaml, toolName, renderer)
	return nil
}

func loadSharedMCPConfig(toolConfig map[string]any, toolName string) (*parser.RegistryMCPServerConfig, map[string]string, error) {
	mcpConfig, err := getMCPConfig(toolConfig, toolName)
	if err != nil {
		mcpCustomLog.Printf("Failed to parse MCP config for tool %s: %v", toolName, err)
		return nil, nil, fmt.Errorf("failed to parse MCP config for tool '%s': %w", toolName, err)
	}
	if err := validateSharedMCPConfig(toolName, mcpConfig); err != nil {
		return nil, nil, err
	}
	headerSecrets := map[string]string(nil)
	if mcpConfig.Type == "http" {
		headerSecrets = ExtractSecretsFromMap(mcpConfig.Headers)
	}
	return mcpConfig, headerSecrets, nil
}

func validateSharedMCPConfig(toolName string, mcpConfig *parser.RegistryMCPServerConfig) error {
	if mcpConfig.Type != "stdio" || mcpConfig.Command == "" || mcpConfig.Command == "docker" {
		return nil
	}
	return fmt.Errorf(
		"tool '%s' stdio MCP server uses command %q which is not supported by MCP Gateway. "+
			"Stdio servers must be containerized (use 'container' with 'entrypoint'), "+
			"or switch to HTTP transport for servers that run directly on the runner.\n\n"+
			"Example (container):\ntools:\n  %s:\n    container: \"my-registry/my-tool:latest\"\n    entrypoint: \"my-tool\"\n    args: [\"--verbose\"]\n\n"+
			"Example (HTTP — for Python/Node servers installed on the runner):\ntools:\n  %s:\n    type: http\n    url: \"http://localhost:8765/mcp\"",
		toolName, mcpConfig.Command, toolName, toolName,
	)
}

func determineMCPPropertyOrder(toolName string, mcpConfig *parser.RegistryMCPServerConfig, renderer MCPConfigRenderer, headerSecrets map[string]string) ([]string, bool) {
	switch mcpConfig.Type {
	case "stdio":
		if renderer.Format == "toml" {
			return []string{"container", "entrypoint", "entrypointArgs", "mounts", "command", "args", "env", "proxy-args", "registry"}, true
		}
		return []string{"type", "container", "entrypoint", "entrypointArgs", "mounts", "command", "args", "tools", "env", "proxy-args", "registry", "required"}, true
	case "http":
		if renderer.Format == "toml" {
			return []string{"url", "http_headers"}, true
		}
		if len(headerSecrets) > 0 {
			return []string{"type", "url", "headers", "auth", "tools", "env", "required"}, true
		}
		return []string{"type", "url", "headers", "auth", "tools", "required"}, true
	default:
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Custom MCP server '%s' has unsupported type '%s'. Supported types: stdio, http", toolName, mcpConfig.Type)))
		return nil, false
	}
}

func collectExistingMCPProperties(propertyOrder []string, mcpConfig *parser.RegistryMCPServerConfig, renderer MCPConfigRenderer, headerSecrets map[string]string) []string {
	existingProperties := make([]string, 0, len(propertyOrder))
	for _, prop := range propertyOrder {
		if shouldRenderMCPProperty(prop, mcpConfig, renderer, headerSecrets) {
			existingProperties = append(existingProperties, prop)
		}
	}
	return existingProperties
}

func shouldRenderMCPProperty(prop string, mcpConfig *parser.RegistryMCPServerConfig, renderer MCPConfigRenderer, headerSecrets map[string]string) bool {
	switch prop {
	case "type":
		return true
	case "tools":
		return renderer.RequiresCopilotFields || len(mcpConfig.Allowed) > 0
	case "container":
		return mcpConfig.Container != ""
	case "entrypoint":
		return mcpConfig.Entrypoint != ""
	case "entrypointArgs":
		return len(mcpConfig.EntrypointArgs) > 0
	case "mounts":
		return len(mcpConfig.Mounts) > 0
	case "command":
		return mcpConfig.Command != ""
	case "args":
		return len(mcpConfig.Args) > 0
	case "env":
		return len(mcpConfig.Env) > 0 || len(headerSecrets) > 0
	case "url":
		return mcpConfig.URL != ""
	case "headers", "http_headers":
		return len(mcpConfig.Headers) > 0
	case "auth":
		return mcpConfig.Auth != nil
	case "proxy-args":
		return len(mcpConfig.ProxyArgs) > 0
	case "registry":
		return mcpConfig.Registry != ""
	case "required":
		return mcpConfig.Required != nil && !*mcpConfig.Required
	default:
		return false
	}
}

func renderMCPProperties(yaml *strings.Builder, properties []string, mcpConfig *parser.RegistryMCPServerConfig, renderer MCPConfigRenderer, headerSecrets map[string]string) {
	hasTrailingGuardPolicies := renderer.Format == "json" && len(renderer.GuardPolicies) > 0
	for propIndex, property := range properties {
		isLast := propIndex == len(properties)-1 && !hasTrailingGuardPolicies
		renderMCPProperty(yaml, property, isLast, mcpConfig, renderer, headerSecrets)
	}
}

func renderMCPProperty(yaml *strings.Builder, property string, isLast bool, mcpConfig *parser.RegistryMCPServerConfig, renderer MCPConfigRenderer, headerSecrets map[string]string) {
	switch property {
	case "type", "container", "entrypoint", "command", "url", "registry", "required":
		renderMCPScalarProperty(yaml, property, isLast, mcpConfig, renderer)
	case "tools", "entrypointArgs", "mounts", "args", "proxy-args":
		renderMCPArrayProperty(yaml, property, isLast, mcpConfig, renderer)
	case "env", "http_headers", "headers":
		renderMCPMapProperty(yaml, property, isLast, mcpConfig, renderer, headerSecrets)
	case "auth":
		renderMCPAuthProperty(yaml, isLast, mcpConfig, renderer)
	}
}

func renderTrailingGuardPolicies(yaml *strings.Builder, toolName string, renderer MCPConfigRenderer) {
	if renderer.Format == "json" && len(renderer.GuardPolicies) > 0 {
		renderGuardPoliciesJSON(yaml, renderer.GuardPolicies, renderer.IndentLevel)
	} else if renderer.Format == "toml" && len(renderer.GuardPolicies) > 0 {
		renderGuardPoliciesToml(yaml, renderer.GuardPolicies, toolName)
	}
}

func renderMCPScalarProperty(yaml *strings.Builder, property string, isLast bool, mcpConfig *parser.RegistryMCPServerConfig, renderer MCPConfigRenderer) {
	switch property {
	case "type":
		renderMCPJSONScalar(yaml, renderer, "type", mcpConfig.Type, isLast)
	case "container":
		container := resolveGatewayContainerFromMappings(mcpConfig.Container, renderer.ContainerPinMappings)
		renderMCPStringScalar(yaml, renderer, "container", container, isLast)
	case "entrypoint":
		renderMCPStringScalar(yaml, renderer, "entrypoint", mcpConfig.Entrypoint, isLast)
	case "command":
		renderMCPStringScalar(yaml, renderer, "command", mcpConfig.Command, isLast)
	case "url":
		urlValue := mcpConfig.URL
		if renderer.RewriteLocalhostToDocker {
			urlValue = rewriteLocalhostToDockerHost(urlValue)
		}
		renderMCPStringScalar(yaml, renderer, "url", urlValue, isLast)
	case "registry":
		renderMCPStringScalar(yaml, renderer, "registry", mcpConfig.Registry, isLast)
	case "required":
		if renderer.Format == "json" && mcpConfig.Required != nil && !*mcpConfig.Required {
			fmt.Fprintf(yaml, "%s\"required\": false%s\n", renderer.IndentLevel, renderMCPComma(isLast))
		}
	}
}

func renderMCPStringScalar(yaml *strings.Builder, renderer MCPConfigRenderer, key, value string, isLast bool) {
	if renderer.Format == "toml" {
		fmt.Fprintf(yaml, "%s%s = \"%s\"\n", renderer.IndentLevel, key, value)
		return
	}
	renderMCPJSONScalar(yaml, renderer, key, value, isLast)
}

func renderMCPJSONScalar(yaml *strings.Builder, renderer MCPConfigRenderer, key, value string, isLast bool) {
	fmt.Fprintf(yaml, "%s\"%s\": \"%s\"%s\n", renderer.IndentLevel, key, value, renderMCPComma(isLast))
}

func renderMCPArrayProperty(yaml *strings.Builder, property string, isLast bool, mcpConfig *parser.RegistryMCPServerConfig, renderer MCPConfigRenderer) {
	switch property {
	case "tools":
		values := mcpConfig.Allowed
		if len(values) == 0 {
			values = []string{"*"}
		}
		renderMCPArray(yaml, renderer, "tools", values, isLast, false)
	case "entrypointArgs":
		renderMCPArray(yaml, renderer, "entrypointArgs", mcpConfig.EntrypointArgs, isLast, renderer.RequiresCopilotFields)
	case "mounts":
		renderMCPArray(yaml, renderer, "mounts", mcpConfig.Mounts, isLast, renderer.RequiresCopilotFields)
	case "args":
		renderMCPArray(yaml, renderer, "args", mcpConfig.Args, isLast, false)
	case "proxy-args":
		renderMCPArray(yaml, renderer, "proxy-args", mcpConfig.ProxyArgs, isLast, false)
	}
}

func renderMCPArray(yaml *strings.Builder, renderer MCPConfigRenderer, key string, values []string, isLast bool, replaceTemplates bool) {
	if renderer.Format == "toml" {
		renderMCPTOMLArray(yaml, renderer, key, values)
		return
	}
	jsonKey := key
	if key == "proxy-args" {
		jsonKey = "proxy-args"
	}
	fmt.Fprintf(yaml, "%s\"%s\": [\n", renderer.IndentLevel, jsonKey)
	for i, value := range values {
		if replaceTemplates {
			value = ReplaceTemplateExpressionsWithEnvVars(value)
		}
		fmt.Fprintf(yaml, "%s  \"%s\"%s\n", renderer.IndentLevel, value, renderMCPComma(i == len(values)-1))
	}
	fmt.Fprintf(yaml, "%s]%s\n", renderer.IndentLevel, renderMCPComma(isLast))
}

func renderMCPTOMLArray(yaml *strings.Builder, renderer MCPConfigRenderer, key string, values []string) {
	tomlKey := strings.ReplaceAll(key, "-", "_")
	if key == "entrypointArgs" || key == "mounts" {
		fmt.Fprintf(yaml, "%s%s = [", renderer.IndentLevel, tomlKey)
		for i, value := range values {
			if i > 0 {
				yaml.WriteString(", ")
			}
			fmt.Fprintf(yaml, "\"%s\"", value)
		}
		yaml.WriteString("]\n")
		return
	}
	fmt.Fprintf(yaml, "%s%s = [\n", renderer.IndentLevel, tomlKey)
	for _, value := range values {
		fmt.Fprintf(yaml, "%s  \"%s\",\n", renderer.IndentLevel, value)
	}
	fmt.Fprintf(yaml, "%s]\n", renderer.IndentLevel)
}

func renderMCPMapProperty(yaml *strings.Builder, property string, isLast bool, mcpConfig *parser.RegistryMCPServerConfig, renderer MCPConfigRenderer, headerSecrets map[string]string) {
	switch property {
	case "env":
		renderMCPEnvMap(yaml, isLast, mcpConfig, renderer, headerSecrets)
	case "http_headers":
		writeTOMLInlineStringMapSection(yaml, renderer.IndentLevel, "http_headers", renderCustomMCPHeadersTOML(mcpConfig.Headers, headerSecrets))
	case "headers":
		renderMCPHeadersMap(yaml, isLast, mcpConfig, renderer, headerSecrets)
	}
}

// renderCustomMCPHeadersTOML normalizes HTTP MCP header values for TOML output.
//
// The TOML config is written inside an expanding heredoc in a `run:` block, so any
// `${{ secrets.* }}` expression left in a header value would be interpolated into the
// shell script source before execution, making the raw secret bytes part of the script
// text (RGS-008). Rewriting the expression as a ${VAR} shell reference keeps the secret
// out of the script: it is resolved by the shell from the step's `env:` mapping, which
// already carries every secret referenced by HTTP MCP headers.
func renderCustomMCPHeadersTOML(headers map[string]string, headerSecrets map[string]string) map[string]string {
	renderedHeaders := make(map[string]string, len(headers))
	for headerKey, headerValue := range headers {
		headerValue = ReplaceSecretsWithShellEnvVars(headerValue, headerSecrets)
		renderedHeaders[headerKey] = replaceEnvExpressionsWithPrefixedEnvVars(headerValue, "${")
	}
	return renderedHeaders
}

func renderMCPEnvMap(yaml *strings.Builder, isLast bool, mcpConfig *parser.RegistryMCPServerConfig, renderer MCPConfigRenderer, headerSecrets map[string]string) {
	renderedEnv := renderCustomMCPEnvVars(mcpConfig.Env, renderer.Format == "toml")
	if renderer.Format == "toml" {
		writeTOMLInlineStringMapSection(yaml, renderer.IndentLevel, "env", renderedEnv)
		return
	}
	for varName := range headerSecrets {
		if _, exists := renderedEnv[varName]; !exists {
			renderedEnv[varName] = "\\${" + varName + "}"
		}
	}
	writeJSONStringMapSectionRaw(yaml, renderer.IndentLevel, "env", renderedEnv, !isLast)
}

func renderMCPHeadersMap(yaml *strings.Builder, isLast bool, mcpConfig *parser.RegistryMCPServerConfig, renderer MCPConfigRenderer, headerSecrets map[string]string) {
	renderedHeaders := make(map[string]string, len(mcpConfig.Headers))
	for headerKey, headerValue := range mcpConfig.Headers {
		renderedHeaders[headerKey] = ReplaceTemplateExpressionsWithEnvVars(headerValue)
	}
	writeJSONStringMapSectionRaw(yaml, renderer.IndentLevel, "headers", renderedHeaders, !isLast)
}

func renderMCPAuthProperty(yaml *strings.Builder, isLast bool, mcpConfig *parser.RegistryMCPServerConfig, renderer MCPConfigRenderer) {
	if mcpConfig.Auth == nil {
		return
	}
	fmt.Fprintf(yaml, "%s\"auth\": {\n", renderer.IndentLevel)
	if mcpConfig.Auth.Audience != "" {
		fmt.Fprintf(yaml, "%s  \"type\": \"%s\",\n", renderer.IndentLevel, mcpConfig.Auth.Type)
		fmt.Fprintf(yaml, "%s  \"audience\": \"%s\"\n", renderer.IndentLevel, mcpConfig.Auth.Audience)
	} else {
		fmt.Fprintf(yaml, "%s  \"type\": \"%s\"\n", renderer.IndentLevel, mcpConfig.Auth.Type)
	}
	fmt.Fprintf(yaml, "%s}%s\n", renderer.IndentLevel, renderMCPComma(isLast))
}

func renderMCPComma(isLast bool) string {
	if isLast {
		return ""
	}
	return ","
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
					maps.Copy(allSecrets, secrets)
				}
			}
		}
	}

	return allSecrets
}

// validateMCPKnownProperties checks that all keys in toolConfig are in the known set.
func validateMCPKnownProperties(toolConfig map[string]any, toolName string) error {
	knownProperties := map[string]struct{}{
		"type":           {},
		"mode":           {},
		"command":        {},
		"container":      {},
		"version":        {},
		"args":           {},
		"entrypoint":     {},
		"entrypointArgs": {},
		"mounts":         {},
		"env":            {},
		"proxy-args":     {},
		"url":            {},
		"headers":        {},
		"auth":           {},
		"registry":       {},
		"allowed":        {},
		"toolsets":       {},
		"required":       {},
	}
	for key := range toolConfig {
		if !setutil.Contains(knownProperties, key) {
			mcpCustomLog.Printf("Unknown property '%s' in MCP config for tool '%s'", key, toolName)
			validProps := sliceutil.SortedKeys(knownProperties)
			return fmt.Errorf(
				"unknown property '%s' in MCP configuration for tool '%s'. Valid properties are: %s. "+
					"Example:\nmcp-servers:\n  %s:\n    command: \"npx @my/tool\"\n    args: [\"--port\", \"3000\"]",
				key, toolName, strings.Join(validProps, ", "), toolName)
		}
	}
	return nil
}

// resolveMCPType determines the MCP server type from explicit or inferred fields.
func resolveMCPType(config MapToolConfig, toolName string) (string, error) {
	if typeStr, hasType := config.GetString("type"); hasType {
		mcpCustomLog.Printf("MCP type explicitly set to: %s", typeStr)
		if typeStr == "local" {
			return "stdio", nil
		}
		return typeStr, nil
	}
	mcpCustomLog.Print("No explicit MCP type, inferring from fields")
	if _, hasURL := config.GetString("url"); hasURL {
		mcpCustomLog.Printf("Inferred MCP type as http (has url field)")
		return "http", nil
	}
	if _, hasCommand := config.GetString("command"); hasCommand {
		mcpCustomLog.Printf("Inferred MCP type as stdio (has command field)")
		return "stdio", nil
	}
	if _, hasContainer := config.GetString("container"); hasContainer {
		mcpCustomLog.Printf("Inferred MCP type as stdio (has container field)")
		return "stdio", nil
	}
	mcpCustomLog.Printf("Unable to determine MCP type for tool '%s': missing type, url, command, or container", toolName)
	return "", fmt.Errorf(
		"unable to determine MCP type for tool '%s': missing type, url, command, or container. "+
			"Must specify one of: 'type' (stdio/http), 'url' (for HTTP MCP), 'command' (for command-based), or 'container' (for Docker-based). "+
			"Example:\nmcp-servers:\n  %s:\n    command: \"npx @my/tool\"\n    args: [\"--port\", \"3000\"]",
		toolName, toolName)
}

// extractMCPStdioFields populates stdio-specific fields on result.
func extractMCPStdioFields(config MapToolConfig, result *parser.RegistryMCPServerConfig) {
	if command, ok := config.GetString("command"); ok {
		result.Command = command
	}
	if container, ok := config.GetString("container"); ok {
		result.Container = container
	}
	if version, ok := config.GetString("version"); ok {
		result.Version = version
	}
	if args, ok := config.GetStringArray("args"); ok {
		result.Args = args
	}
	if entrypoint, ok := config.GetString("entrypoint"); ok {
		result.Entrypoint = entrypoint
	}
	if entrypointArgs, ok := config.GetStringArray("entrypointArgs"); ok {
		result.EntrypointArgs = entrypointArgs
	}
	if mounts, ok := config.GetStringArray("mounts"); ok {
		result.Mounts = mounts
	}
	if env, ok := config.GetStringMap("env"); ok {
		result.Env = env
	}
	if proxyArgs, ok := config.GetStringArray("proxy-args"); ok {
		result.ProxyArgs = proxyArgs
	}
}

// extractMCPHTTPFields populates HTTP-specific fields on result.
func extractMCPHTTPFields(config MapToolConfig, toolName string, result *parser.RegistryMCPServerConfig) error {
	if url, hasURL := config.GetString("url"); hasURL {
		result.URL = url
	} else {
		mcpCustomLog.Printf("HTTP MCP tool '%s' missing required 'url' field", toolName)
		return fmt.Errorf(
			"http MCP tool '%s' missing required 'url' field. HTTP MCP servers must specify a URL endpoint. "+
				"Example:\nmcp-servers:\n  %s:\n    type: http\n    url: \"https://api.example.com/mcp\"\n    headers:\n      Authorization: \"****** secrets.API_KEY }}\"",
			toolName, toolName)
	}
	if headers, ok := config.GetStringMap("headers"); ok {
		result.Headers = headers
	}
	if authVal, hasAuth := config.GetAny("auth"); hasAuth {
		if authMap, ok := authVal.(map[string]any); ok {
			authConfig := &types.MCPAuthConfig{}
			if authType, ok := authMap["type"].(string); ok {
				authConfig.Type = authType
			}
			if audience, ok := authMap["audience"].(string); ok {
				authConfig.Audience = audience
			}
			if authConfig.Type != "" {
				result.Auth = authConfig
			}
		} else if authCfg, ok := authVal.(*types.MCPAuthConfig); ok {
			result.Auth = authCfg
		}
	}
	return nil
}

// postProcessMCPConfig applies auto-container assignment and version merging for stdio configs.
func postProcessMCPConfig(result *parser.RegistryMCPServerConfig) {
	if result.Type == "stdio" && result.Container == "" && result.Command != "" {
		if containerConfig := getWellKnownContainer(result.Command); containerConfig != nil {
			mcpCustomLog.Printf("Auto-assigning container for command '%s': %s", result.Command, containerConfig.Image)
			result.Container = containerConfig.Image
			result.Entrypoint = containerConfig.Entrypoint
			// The command becomes the container entrypoint; original args become entrypointArgs.
			// Do NOT prepend the command to entrypointArgs — the entrypoint field already carries it.
			result.EntrypointArgs = result.Args
			result.Args = nil
			result.Command = ""
		}
	}
	// Combine container and version into a single image reference.
	if result.Type == "stdio" && result.Container != "" && result.Version != "" {
		result.Container = result.Container + ":" + result.Version
		result.Version = ""
	}
}

// getMCPConfig extracts MCP configuration from a tool config and returns a structured MCPServerConfig
func getMCPConfig(toolConfig map[string]any, toolName string) (*parser.RegistryMCPServerConfig, error) {
	mcpCustomLog.Printf("Extracting MCP config for tool: %s", toolName)

	// Jira's first-class auth block is compiler input rather than MCP Gateway
	// configuration. Remove it after expandJiraToolConfig has generated the
	// appropriate Authorization header.
	if toolName == "jira" {
		toolConfig = maps.Clone(toolConfig)
		delete(toolConfig, "auth")
	}

	config := MapToolConfig(toolConfig)
	result := &parser.RegistryMCPServerConfig{
		BaseMCPServerConfig: types.BaseMCPServerConfig{
			Env:     make(map[string]string),
			Headers: make(map[string]string),
		},
		Name: toolName,
	}

	if err := validateMCPKnownProperties(toolConfig, toolName); err != nil {
		return nil, err
	}

	mcpType, err := resolveMCPType(config, toolName)
	if err != nil {
		return nil, err
	}
	result.Type = mcpType

	if registry, ok := config.GetString("registry"); ok {
		result.Registry = registry
	}

	mcpCustomLog.Printf("Extracting fields for MCP type: %s", result.Type)
	switch result.Type {
	case "stdio":
		extractMCPStdioFields(config, result)
	case "http":
		if err := extractMCPHTTPFields(config, toolName, result); err != nil {
			return nil, err
		}
	default:
		mcpCustomLog.Printf("Unsupported MCP type '%s' for tool '%s'", result.Type, toolName)
		return nil, fmt.Errorf(
			"unsupported MCP type '%s' for tool '%s'. Valid types are: stdio, http. "+
				"Example:\nmcp-servers:\n  %s:\n    type: stdio\n    command: \"npx @my/tool\"\n    args: [\"--port\", \"3000\"]",
			result.Type, toolName, toolName)
	}

	if allowed, ok := config.GetStringArray("allowed"); ok {
		result.Allowed = allowed
	}
	if requiredVal, ok := config.GetAny("required"); ok {
		if requiredBool, ok := requiredVal.(bool); ok {
			result.Required = &requiredBool
		}
	}

	postProcessMCPConfig(result)
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
