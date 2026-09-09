package parser

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/types"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var mcpLog = logger.New("parser:mcp")
var simpleSecretExpressionPattern = regexp.MustCompile(`^\$\{\{\s*secrets\.[A-Z_][A-Z0-9_]*\s*\}\}$`)

// IsSimpleSecretExpression reports whether value is a direct GitHub Actions
// secrets reference without additional expression operators.
func IsSimpleSecretExpression(value string) bool {
	return simpleSecretExpressionPattern.MatchString(value)
}

// ValidMCPTypes defines all supported MCP server types.
// "local" is an alias for "stdio" and gets normalized during parsing.
var ValidMCPTypes = []string{"stdio", "http", "local"}

// IsMCPType checks if a type string is a valid MCP server type.
// Returns true for "stdio", "http", and "local" (which is an alias for "stdio").
func IsMCPType(typeStr string) bool {
	switch typeStr {
	case "stdio", "http", "local":
		return true
	default:
		return false
	}
}

// RegistryMCPServerConfig represents a parser-layer MCP server configuration.
// It is intentionally distinct from workflow.MCPServerConfig, which models
// workflow-facing YAML tool configuration.
// It embeds BaseMCPServerConfig for common fields and adds parser-specific fields.
type RegistryMCPServerConfig struct {
	types.BaseMCPServerConfig

	// Parser-specific fields
	Name      string   `json:"name"`       // Server name/identifier
	Registry  string   `json:"registry"`   // URI to installation location from registry
	ProxyArgs []string `json:"proxy-args"` // custom proxy arguments for container-based tools
	Allowed   []string `json:"allowed"`    // allowed tools

	// Gateway startup behavior: when Required is explicitly false the server is optional
	// and startup failures degrade to warnings. nil means the default (required).
	Required *bool `json:"required,omitempty"`
}

// MCPServerInfo contains the inspection results for an MCP server
type MCPServerInfo struct {
	Config    RegistryMCPServerConfig
	Connected bool
	Error     error
	Tools     []*mcp.Tool
	Resources []*mcp.Resource
	Prompts   []*mcp.Prompt
	Roots     []*MCPRootInfo
}

// MCPRootInfo contains inspection-only root display data inferred from resources.
type MCPRootInfo struct {
	URI  string
	Name string
}

// ExtractMCPConfigurations extracts MCP server configurations from workflow frontmatter
func ExtractMCPConfigurations(frontmatter map[string]any, serverFilter string) ([]RegistryMCPServerConfig, error) {
	mcpLog.Printf("Extracting MCP configurations with filter: %s", serverFilter)
	var configs []RegistryMCPServerConfig

	addSafeOutputsConfig(frontmatter, serverFilter, &configs)
	addSafeJobsConfig(frontmatter, serverFilter, &configs)
	addMCPScriptsConfig(frontmatter, serverFilter, &configs)

	// Get mcp-servers section from frontmatter
	mcpServersSection, hasMCPServers := frontmatter["mcp-servers"]
	if !hasMCPServers {
		mcpLog.Print("No mcp-servers section found, checking for built-in tools")
		// Process built-in MCP tools from tools section.
		if err := extractBuiltinMCPTools(frontmatter, serverFilter, &configs); err != nil {
			return nil, err
		}
		mcpLog.Printf("Extracted %d MCP configurations total", len(configs))
		return configs, nil // No mcp-servers configured, but we might have safe-outputs and built-in tools
	}

	mcpServers, ok := mcpServersSection.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("mcp-servers section must be a map, got %T. Example:\nmcp-servers:\n  my-server:\n    command: \"npx @my/tool\"\n    args: [\"--port\", \"3000\"]", mcpServersSection)
	}

	// Process built-in MCP tools from tools section.
	if err := extractBuiltinMCPTools(frontmatter, serverFilter, &configs); err != nil {
		return nil, err
	}

	// Process custom MCP servers from mcp-servers section
	mcpLog.Printf("Processing %d custom MCP servers", len(mcpServers))
	customConfigs, err := parseCustomMCPServerConfigs(mcpServers, serverFilter)
	if err != nil {
		return nil, err
	}
	configs = append(configs, customConfigs...)

	mcpLog.Printf("Extracted %d MCP configurations total", len(configs))
	return configs, nil
}

func addSafeOutputsConfig(frontmatter map[string]any, serverFilter string, configs *[]RegistryMCPServerConfig) {
	safeOutputsSection, hasSafeOutputs := frontmatter["safe-outputs"]
	if !hasSafeOutputs {
		return
	}
	mcpLog.Print("Found safe-outputs configuration")
	if !serverFilterMatches(constants.SafeOutputsMCPServerID.String(), serverFilter) {
		return
	}

	config := RegistryMCPServerConfig{
		BaseMCPServerConfig: types.BaseMCPServerConfig{
			Type:    "stdio",
			Command: "node",
			Env:     make(map[string]string),
		},
		Name: constants.SafeOutputsMCPServerID.String(),
	}
	if safeOutputsMap, ok := safeOutputsSection.(map[string]any); ok {
		for toolType := range safeOutputsMap {
			if mappedTool, ok := safeOutputToolName(toolType); ok {
				config.Allowed = append(config.Allowed, mappedTool)
			}
		}
	}
	*configs = append(*configs, config)
}

func addSafeJobsConfig(frontmatter map[string]any, serverFilter string, configs *[]RegistryMCPServerConfig) {
	safeJobsSection, hasSafeJobs := frontmatter["safe-jobs"]
	if !hasSafeJobs {
		return
	}
	mcpLog.Print("Found safe-jobs configuration")
	if !serverFilterMatches(constants.SafeOutputsMCPServerID.String(), serverFilter) {
		return
	}

	config := findOrCreateSafeOutputsConfig(configs)
	if safeJobsMap, ok := safeJobsSection.(map[string]any); ok {
		for jobName := range safeJobsMap {
			config.Allowed = append(config.Allowed, jobName)
		}
	}
}

func addMCPScriptsConfig(frontmatter map[string]any, serverFilter string, configs *[]RegistryMCPServerConfig) {
	mcpScriptsSection, hasMCPScripts := frontmatter["mcp-scripts"]
	if !hasMCPScripts {
		return
	}
	mcpLog.Print("Found mcp-scripts configuration")
	if !serverFilterMatches(constants.MCPScriptsMCPServerID.String(), serverFilter) {
		return
	}

	config := RegistryMCPServerConfig{
		BaseMCPServerConfig: types.BaseMCPServerConfig{
			Type:    "http",
			Command: "",
			Env:     make(map[string]string),
		},
		Name: constants.MCPScriptsMCPServerID.String(),
	}
	if mcpScriptsMap, ok := mcpScriptsSection.(map[string]any); ok {
		for toolName := range mcpScriptsMap {
			if toolName != "mode" {
				config.Allowed = append(config.Allowed, toolName)
			}
		}
	}
	*configs = append(*configs, config)
}

func serverFilterMatches(serverName, serverFilter string) bool {
	return serverFilter == "" || strings.Contains(strings.ToLower(serverName), strings.ToLower(serverFilter))
}

func safeOutputToolName(toolType string) (string, bool) {
	switch toolType {
	case "create-issue", "create-discussion", "add-comment", "create-pull-request",
		"create-pull-request-review-comment", "create-code-scanning-alert", "add-labels",
		"update-issue", "push-to-pull-request-branch", "missing-tool":
		return toolType, true
	default:
		return "", false
	}
}

func findOrCreateSafeOutputsConfig(configs *[]RegistryMCPServerConfig) *RegistryMCPServerConfig {
	for i := range *configs {
		if (*configs)[i].Name == constants.SafeOutputsMCPServerID.String() {
			return &(*configs)[i]
		}
	}
	newConfig := RegistryMCPServerConfig{
		BaseMCPServerConfig: types.BaseMCPServerConfig{
			Type:    "stdio",
			Command: "node",
			Env:     make(map[string]string),
		},
		Name: constants.SafeOutputsMCPServerID.String(),
	}
	*configs = append(*configs, newConfig)
	return &(*configs)[len(*configs)-1]
}

func parseCustomMCPServerConfigs(mcpServers map[string]any, serverFilter string) ([]RegistryMCPServerConfig, error) {
	var configs []RegistryMCPServerConfig
	for serverName, serverValue := range mcpServers {
		if serverFilter != "" && !strings.Contains(strings.ToLower(serverName), strings.ToLower(serverFilter)) {
			continue
		}
		toolConfig, ok := serverValue.(map[string]any)
		if !ok {
			continue
		}
		config, err := ParseMCPConfig(serverName, toolConfig, toolConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to parse MCP config for %s: %w", serverName, err)
		}
		mcpLog.Printf("Parsed custom MCP server: %s (type=%s)", serverName, config.Type)
		configs = append(configs, config)
	}
	return configs, nil
}

// extractBuiltinMCPTools reads the tools section and appends GitHub configs to configs.
// It returns an error if a removed tool (serena) is present.
func extractBuiltinMCPTools(frontmatter map[string]any, serverFilter string, configs *[]RegistryMCPServerConfig) error {
	toolsSection, hasTools := frontmatter["tools"]
	if !hasTools {
		return nil
	}
	tools, ok := toolsSection.(map[string]any)
	if !ok {
		return nil
	}
	for toolName, toolValue := range tools {
		if toolName == "serena" {
			return errors.New("tools.serena is removed")
		}
		if toolName == "github" || toolName == "linear" {
			config, err := processBuiltinMCPTool(toolName, toolValue, serverFilter)
			if err != nil {
				return err
			}
			if config != nil {
				mcpLog.Printf("Added built-in MCP tool: %s", toolName)
				*configs = append(*configs, *config)
			}
		}
	}
	return nil
}

// processBuiltinMCPTool handles built-in MCP tools.
func processBuiltinMCPTool(toolName string, toolValue any, serverFilter string) (*RegistryMCPServerConfig, error) {
	if serverFilter != "" && !strings.Contains(strings.ToLower(toolName), strings.ToLower(serverFilter)) {
		return nil, nil
	}

	if toolName == "github" {
		config := buildGitHubBuiltinConfig(toolValue)
		return &config, nil
	}
	if toolName == "linear" {
		return buildLinearBuiltinConfig(toolValue)
	}
	return nil, nil
}

func buildLinearBuiltinConfig(toolValue any) (*RegistryMCPServerConfig, error) {
	if toolValue == nil {
		toolValue = map[string]any{}
	}
	toolConfig, ok := toolValue.(map[string]any)
	if !ok {
		return nil, errors.New("tools.linear must be an object")
	}
	if _, expanded := toolConfig["type"]; expanded {
		return buildExpandedLinearBuiltinConfig(toolConfig)
	}
	for field := range toolConfig {
		switch field {
		case "token", "toolsets", "allowed", "required":
		default:
			return nil, fmt.Errorf("unknown tools.linear property %q", field)
		}

	}

	token := constants.LinearMCPDefaultTokenExpr
	if value, exists := toolConfig["token"]; exists {
		var ok bool
		token, ok = value.(string)
		if !ok || strings.TrimSpace(token) == "" {
			return nil, errors.New("tools.linear.token must be a GitHub Actions secret reference")
		}
	}
	if !IsSimpleSecretExpression(token) {
		return nil, errors.New("tools.linear.token must be a GitHub Actions secret reference")
	}

	config := &RegistryMCPServerConfig{
		BaseMCPServerConfig: types.BaseMCPServerConfig{
			Type: "http",
			URL:  constants.LinearMCPReadOnlyURL,
			Headers: map[string]string{
				"Authorization": "Bearer " + token,
			},
		},
		Name: "linear",
	}
	allowed, err := buildLinearAllowedTools(toolConfig)
	if err != nil {
		return nil, err
	}
	config.Allowed = allowed
	if requiredValue, exists := toolConfig["required"]; exists {
		required, ok := requiredValue.(bool)
		if !ok {
			return nil, errors.New("tools.linear.required must be a boolean")
		}
		config.Required = &required
	}
	return config, nil
}

func buildExpandedLinearBuiltinConfig(toolConfig map[string]any) (*RegistryMCPServerConfig, error) {
	for field := range toolConfig {
		switch field {
		case "type", "url", "headers", "allowed", "required":
		default:
			return nil, fmt.Errorf("unknown expanded tools.linear property %q", field)
		}
	}
	typeName, ok := toolConfig["type"].(string)
	if !ok || typeName != "http" {
		return nil, errors.New("expanded tools.linear type must be http")
	}
	url, ok := toolConfig["url"].(string)
	if !ok || url == "" {
		return nil, errors.New("expanded tools.linear url must be a non-empty string")
	}
	config := &RegistryMCPServerConfig{
		BaseMCPServerConfig: types.BaseMCPServerConfig{Type: typeName, URL: url, Headers: map[string]string{}},
		Name:                "linear",
	}
	if headers, ok := toolConfig["headers"].(map[string]any); ok {
		for key, value := range headers {
			header, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("expanded tools.linear header %q must be a string", key)
			}
			config.Headers[key] = header
		}
	}
	allowed, err := buildLinearAllowedTools(toolConfig)
	if err != nil {
		return nil, err
	}
	config.Allowed = allowed
	if requiredValue, exists := toolConfig["required"]; exists {
		required, ok := requiredValue.(bool)
		if !ok {
			return nil, errors.New("tools.linear.required must be a boolean")
		}
		config.Required = &required
	}
	return config, nil
}

func buildLinearAllowedTools(toolConfig map[string]any) ([]string, error) {
	var toolsetTools []string
	if toolsetsValue, exists := toolConfig["toolsets"]; exists {
		var err error
		toolsetTools, err = ParseLinearToolsets(toolsetsValue)
		if err != nil {
			return nil, err
		}
	}
	allowedValue, exists := toolConfig["allowed"]
	if !exists {
		return toolsetTools, nil
	}
	allowed, ok := allowedValue.([]any)
	if !ok || len(allowed) == 0 {
		return nil, errors.New("tools.linear.allowed must be a non-empty array of tool names")
	}
	result := make([]string, 0, len(allowed))
	for _, item := range allowed {
		tool, ok := item.(string)
		if !ok || strings.TrimSpace(tool) == "" {
			return nil, errors.New("tools.linear.allowed must contain only non-empty tool names")
		}
		result = append(result, tool)
	}
	if len(toolsetTools) > 0 {
		if err := ValidateLinearAllowedForToolsets(result, toolsetTools); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func buildGitHubBuiltinConfig(toolValue any) RegistryMCPServerConfig {
	useRemote, customToken, toolConfig := githubBuiltinMode(toolValue)
	var config RegistryMCPServerConfig
	if useRemote {
		config = RegistryMCPServerConfig{
			BaseMCPServerConfig: types.BaseMCPServerConfig{
				Type:    "http",
				URL:     "https://api.githubcopilot.com/mcp/",
				Headers: make(map[string]string),
				Env:     make(map[string]string),
			},
			Name: "github",
		}
		if customToken != "" {
			config.Env["GITHUB_TOKEN"] = customToken
		}
		config.Headers["X-MCP-Readonly"] = "true"
	} else {
		config = RegistryMCPServerConfig{
			BaseMCPServerConfig: types.BaseMCPServerConfig{
				Type:    "docker",
				Command: "docker",
				Args: []string{
					"run", "-i", "--rm", "-e", "GITHUB_PERSONAL_ACCESS_TOKEN",
					"-e", "GITHUB_READ_ONLY=1",
					"ghcr.io/github/github-mcp-server:" + string(constants.DefaultGitHubMCPServerVersion),
				},
				Env: make(map[string]string),
			},
			Name: "github",
		}
		if githubToken, err := GetGitHubToken(); err == nil {
			config.Env["GITHUB_PERSONAL_ACCESS_TOKEN"] = githubToken
		} else {
			config.Env["GITHUB_PERSONAL_ACCESS_TOKEN"] = "${GITHUB_TOKEN_REQUIRED}"
		}
	}
	appendBuiltinAllowedTools(&config, toolConfig)
	if !useRemote {
		applyGitHubBuiltinOverrides(&config, toolConfig)
	}
	return config
}

func githubBuiltinMode(toolValue any) (bool, string, map[string]any) {
	toolConfig, ok := toolValue.(map[string]any)
	if !ok {
		return false, "", nil
	}
	useRemote := false
	customToken := ""
	if modeField, hasMode := toolConfig["mode"]; hasMode {
		if modeStr, ok := modeField.(string); ok && modeStr == "remote" {
			useRemote = true
		}
	}
	if token, hasToken := toolConfig["github-token"]; hasToken {
		if tokenStr, ok := token.(string); ok {
			customToken = tokenStr
		}
	}
	return useRemote, customToken, toolConfig
}

func appendBuiltinAllowedTools(config *RegistryMCPServerConfig, toolConfig map[string]any) {
	if toolConfig == nil {
		return
	}
	if allowed, hasAllowed := toolConfig["allowed"]; hasAllowed {
		if allowedSlice, ok := allowed.([]any); ok {
			for _, item := range allowedSlice {
				if str, ok := item.(string); ok {
					config.Allowed = append(config.Allowed, str)
				}
			}
		}
	}
}

func applyGitHubBuiltinOverrides(config *RegistryMCPServerConfig, toolConfig map[string]any) {
	if toolConfig == nil {
		return
	}
	if version, exists := toolConfig["version"]; exists {
		if versionStr := stringutil.ParseVersionValue(version); versionStr != "" {
			replaceBuiltinImage(config, "ghcr.io/github/github-mcp-server:", "ghcr.io/github/github-mcp-server:"+versionStr)
		}
	}
	appendBuiltinArgs(config, toolConfig["args"])
}

func replaceBuiltinImage(config *RegistryMCPServerConfig, prefix, replacement string) {
	for i, arg := range config.Args {
		if strings.HasPrefix(arg, prefix) {
			config.Args[i] = replacement
			break
		}
	}
}

func appendBuiltinArgs(config *RegistryMCPServerConfig, argsValue any) {
	if argsSlice, ok := argsValue.([]any); ok {
		for _, arg := range argsSlice {
			if argStr, ok := arg.(string); ok {
				config.Args = append(config.Args, argStr)
			}
		}
	}
	if argsSlice, ok := argsValue.([]string); ok {
		config.Args = append(config.Args, argsSlice...)
	}
}

// ParseMCPConfig parses MCP configuration from various formats (map or JSON string)
func ParseMCPConfig(toolName string, mcpSection any, toolConfig map[string]any) (RegistryMCPServerConfig, error) {
	mcpLog.Printf("Parsing MCP configuration for tool: %s", toolName)
	config := RegistryMCPServerConfig{
		BaseMCPServerConfig: types.BaseMCPServerConfig{
			Env:     make(map[string]string),
			Headers: make(map[string]string),
		},
		Name: toolName,
	}

	appendBuiltinAllowedTools(&config, toolConfig)
	mcpConfig, err := parseMCPSectionMap(mcpSection, toolName)
	if err != nil {
		return config, err
	}

	config.Type, err = inferOrReadMCPType(mcpConfig, toolName)
	if err != nil {
		return config, err
	}
	if err := parseMCPRegistryValue(mcpConfig, toolName, &config); err != nil {
		return config, err
	}

	mcpLog.Printf("Extracting %s configuration for tool: %s", config.Type, toolName)
	switch config.Type {
	case "stdio":
		if err := parseMCPStdioTypeConfig(mcpConfig, toolName, &config); err != nil {
			return config, err
		}
	case "http":
		if err := parseMCPHTTPTypeConfig(mcpConfig, toolName, &config); err != nil {
			return config, err
		}
	default:
		return config, fmt.Errorf("unsupported MCP type '%s' for tool '%s'. Valid types are: stdio, http. Example:\nmcp-servers:\n  %s:\n    type: stdio\n    command: \"npx @my/tool\"\n    args: [\"--port\", \"3000\"]", config.Type, toolName, toolName)
	}

	return config, nil
}

func parseMCPSectionMap(mcpSection any, toolName string) (map[string]any, error) {
	switch v := mcpSection.(type) {
	case map[string]any:
		return v, nil
	case string:
		var mcpConfig map[string]any
		if err := json.Unmarshal([]byte(v), &mcpConfig); err != nil {
			return nil, fmt.Errorf("invalid JSON in mcp configuration: %w", err)
		}
		return mcpConfig, nil
	default:
		return nil, fmt.Errorf("mcp configuration must be a map or JSON string, got %T. Example:\nmcp-servers:\n  %s:\n    command: \"npx @my/tool\"\n    args: [\"--port\", \"3000\"]", v, toolName)
	}
}

func inferOrReadMCPType(mcpConfig map[string]any, toolName string) (string, error) {
	if typeVal, hasType := mcpConfig["type"]; hasType {
		typeStr, ok := typeVal.(string)
		if !ok {
			return "", fmt.Errorf("type field must be a string, got %T. Valid types are: stdio, http. Example:\nmcp-servers:\n  %s:\n    type: stdio\n    command: \"npx @my/tool\"", typeVal, toolName)
		}
		if typeStr == "local" {
			return "stdio", nil
		}
		return typeStr, nil
	}
	if _, hasURL := mcpConfig["url"]; hasURL {
		mcpLog.Printf("Inferred MCP type 'http' for tool %s based on url field", toolName)
		return "http", nil
	}
	if _, hasCommand := mcpConfig["command"]; hasCommand {
		mcpLog.Printf("Inferred MCP type 'stdio' for tool %s based on command field", toolName)
		return "stdio", nil
	}
	if _, hasContainer := mcpConfig["container"]; hasContainer {
		mcpLog.Printf("Inferred MCP type 'stdio' for tool %s based on container field", toolName)
		return "stdio", nil
	}
	return "", fmt.Errorf("unable to determine MCP type for tool '%s': missing type, url, command, or container. Must specify one of: 'type' (stdio/http), 'url' (for HTTP MCP), 'command' (for command-based), or 'container' (for Docker-based). Example:\nmcp-servers:\n  %s:\n    command: \"npx @my/tool\"\n    args: [\"--port\", \"3000\"]", toolName, toolName)
}

func parseMCPRegistryValue(mcpConfig map[string]any, toolName string, config *RegistryMCPServerConfig) error {
	if registry, hasRegistry := mcpConfig["registry"]; hasRegistry {
		registryStr, ok := registry.(string)
		if !ok {
			return fmt.Errorf("registry field must be a string, got %T. Example:\nmcp-servers:\n  %s:\n    registry: \"https://registry.npmjs.org/@my/tool\"\n    command: \"npx @my/tool\"", registry, toolName)
		}
		config.Registry = registryStr
	}
	return nil
}

func parseMCPStdioTypeConfig(mcpConfig map[string]any, toolName string, config *RegistryMCPServerConfig) error {
	if container, hasContainer := mcpConfig["container"]; hasContainer {
		if containerStr, ok := container.(string); ok {
			configureMCPStdioContainer(toolName, containerStr, mcpConfig, config)
		}
	} else {
		if err := configureMCPStdioCommand(toolName, mcpConfig, config); err != nil {
			return err
		}
	}
	copyStringMapFromAny(mcpConfig["env"], config.Env)
	appendNetworkProxyArgs(mcpConfig["network"], &config.ProxyArgs)
	return nil
}

func configureMCPStdioContainer(toolName, containerStr string, mcpConfig map[string]any, config *RegistryMCPServerConfig) {
	mcpLog.Printf("Tool %s uses container: %s", toolName, containerStr)
	config.Container = containerStr
	config.Command = "docker"
	config.Args = []string{"run", "--rm", "-i"}

	appendContainerEnv(config, mcpConfig["env"])
	appendContainerMounts(config, mcpConfig["mounts"])
	if entrypointStr, ok := mcpConfig["entrypoint"].(string); ok {
		config.Entrypoint = entrypointStr
		config.Args = append(config.Args, "--entrypoint", entrypointStr)
	}
	config.Args = append(config.Args, containerStr)
	appendBuiltinArgs(config, mcpConfig["entrypointArgs"])
}

func configureMCPStdioCommand(toolName string, mcpConfig map[string]any, config *RegistryMCPServerConfig) error {
	command, hasCommand := mcpConfig["command"]
	if !hasCommand {
		return fmt.Errorf(
			"stdio MCP tool '%s' must specify either 'command' or 'container' field. Cannot specify both. "+
				"Example with command:\n"+
				"mcp-servers:\n"+
				"  %s:\n"+
				"    command: \"npx @my/tool\"\n"+
				"    args: [\"--port\", \"3000\"]\n\n"+
				"Example with container:\n"+
				"mcp-servers:\n"+
				"  %s:\n"+
				"    container: \"myorg/my-tool:latest\"\n"+
				"    env:\n"+
				"      API_KEY: \"${{ secrets.API_KEY }}\"",
			toolName, toolName, toolName,
		)
	}
	commandStr, ok := command.(string)
	if !ok {
		return fmt.Errorf("command field must be a string, got %T. Example:\nmcp-servers:\n  %s:\n    command: \"npx @my/tool\"\n    args: [\"--port\", \"3000\"]", command, toolName)
	}
	config.Command = commandStr
	appendBuiltinArgs(config, mcpConfig["args"])
	return nil
}

func appendContainerEnv(config *RegistryMCPServerConfig, envValue any) {
	envMap, ok := envValue.(map[string]any)
	if !ok {
		return
	}
	envKeys := sliceutil.SortedKeys(envMap)
	for _, key := range envKeys {
		if valueStr, ok := envMap[key].(string); ok {
			config.Args = append(config.Args, "-e", key)
			config.Env[key] = valueStr
		}
	}
}

func appendContainerMounts(config *RegistryMCPServerConfig, mountsValue any) {
	mountsSlice, ok := mountsValue.([]any)
	if !ok {
		return
	}
	var mountStrings []string
	for _, mount := range mountsSlice {
		if mountStr, ok := mount.(string); ok {
			mountStrings = append(mountStrings, mountStr)
			config.Mounts = append(config.Mounts, mountStr)
		}
	}
	sort.Strings(mountStrings)
	for _, mountStr := range mountStrings {
		config.Args = append(config.Args, "-v", mountStr)
	}
}

func appendNetworkProxyArgs(networkValue any, proxyArgs *[]string) {
	networkMap, ok := networkValue.(map[string]any)
	if !ok {
		return
	}
	proxyValue, hasProxyArgs := networkMap["proxy-args"]
	if !hasProxyArgs {
		return
	}
	if proxyArgsSlice, ok := proxyValue.([]any); ok {
		for _, arg := range proxyArgsSlice {
			if argStr, ok := arg.(string); ok {
				*proxyArgs = append(*proxyArgs, argStr)
			}
		}
	}
}

func copyStringMapFromAny(source any, target map[string]string) {
	sourceMap, ok := source.(map[string]any)
	if !ok {
		return
	}
	for key, value := range sourceMap {
		if valueStr, ok := value.(string); ok {
			target[key] = valueStr
		}
	}
}

func parseMCPHTTPTypeConfig(mcpConfig map[string]any, toolName string, config *RegistryMCPServerConfig) error {
	url, hasURL := mcpConfig["url"]
	if !hasURL {
		return fmt.Errorf(
			"http MCP tool '%s' missing required 'url' field. HTTP MCP servers must specify a URL endpoint. "+
				"Example:\n"+
				"mcp-servers:\n"+
				"  %s:\n"+
				"    type: http\n"+
				"    url: \"https://api.example.com/mcp\"\n"+
				"    headers:\n"+
				"      Authorization: \"${{ secrets.API_KEY }}\"",
			toolName, toolName,
		)
	}
	urlStr, ok := url.(string)
	if !ok {
		return fmt.Errorf(
			"url field must be a string, got %T. Example:\n"+
				"mcp-servers:\n"+
				"  %s:\n"+
				"    type: http\n"+
				"    url: \"https://api.example.com/mcp\"\n"+
				"    headers:\n"+
				"      Authorization: \"${{ secrets.API_KEY }}\"",
			url, toolName)
	}
	mcpLog.Printf("Tool %s uses HTTP transport with URL: %s", toolName, urlStr)
	config.URL = urlStr
	copyStringMapFromAny(mcpConfig["headers"], config.Headers)
	return nil
}
