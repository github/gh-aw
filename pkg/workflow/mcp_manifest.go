package workflow

import (
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/stringutil"
)

func collectMCPServersForManifest(data *WorkflowData) []GHAWManifestMCPServer {
	if data == nil {
		return []GHAWManifestMCPServer{}
	}

	serversByName := make(map[string]GHAWManifestMCPServer)
	add := func(name string, tools []string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		serversByName[name] = GHAWManifestMCPServer{
			Name:  name,
			Tools: normalizeManifestToolNames(tools),
		}
	}

	// collectMCPTools intentionally excludes GitHub when gh-proxy is configured,
	// because that mode exposes GitHub access through the gh CLI rather than MCP.
	for _, toolName := range collectMCPTools(data) {
		switch toolName {
		case "cache-memory":
			// cache-memory is collected for setup sequencing but is backed by a file
			// share, not an MCP server entry in generated engine configs.
			continue
		case "github":
			add("github", collectGitHubMCPManifestTools(data.Tools["github"]))
		case "agentic-workflows":
			add(constants.AgenticWorkflowsMCPServerID.String(), []string{"*"})
		case "safe-outputs":
			add(constants.SafeOutputsMCPServerID.String(), collectSafeOutputsManifestTools(data.SafeOutputs))
		case "mcp-scripts":
			add(constants.MCPScriptsMCPServerID.String(), sliceutil.SortedKeys(data.MCPScripts.Tools))
		case enclaveMCPServerName:
			add(enclaveMCPServerName, []string{"*"})
		case "dispatch_workflow", "dispatch_repository", "call_workflow":
			// These descriptors configure dynamic safe-output tools. The actual
			// exposed tools are the normalized target names collected below.
			continue
		default:
			if toolConfig, ok := data.Tools[toolName].(map[string]any); ok {
				add(toolName, collectGenericMCPManifestTools(toolConfig))
			}
		}
	}

	if len(serversByName) == 0 {
		return []GHAWManifestMCPServer{}
	}
	names := sliceutil.SortedKeys(serversByName)
	servers := make([]GHAWManifestMCPServer, 0, len(names))
	for _, name := range names {
		servers = append(servers, serversByName[name])
	}
	return servers
}

func collectGitHubMCPManifestTools(toolValue any) []string {
	githubConfig := parseGitHubTool(toolValue)
	if githubConfig != nil && len(githubConfig.Allowed) > 0 {
		return githubConfig.Allowed.ToStringSlice()
	}

	githubTool, _ := toolValue.(map[string]any)
	defaultTools := constants.DefaultGitHubToolsLocal
	if getGitHubType(githubTool) == GitHubMCPModeRemote {
		defaultTools = constants.DefaultGitHubToolsRemote
	}
	enabledToolsets := ParseGitHubToolsets(getGitHubToolsets(githubTool))
	enabled := make(map[string]struct{}, len(enabledToolsets))
	for _, toolset := range enabledToolsets {
		enabled[toolset] = struct{}{}
	}
	toolToToolset, err := getGitHubToolToToolsetMap()
	if err != nil {
		return nil
	}

	tools := make([]string, 0, len(defaultTools))
	for _, tool := range defaultTools {
		if toolset, ok := toolToToolset[tool]; ok {
			if _, enabled := enabled[toolset]; enabled {
				tools = append(tools, tool)
			}
		}
	}
	return tools
}

func collectGenericMCPManifestTools(toolConfig map[string]any) []string {
	allowed, ok := toolConfig["allowed"]
	if !ok {
		return []string{"*"}
	}
	return stringsFromAnySlice(allowed)
}

func collectSafeOutputsManifestTools(safeOutputs *SafeOutputsConfig) []string {
	if safeOutputs == nil {
		return nil
	}

	var tools []string
	for fieldName, toolName := range safeOutputFieldMapping {
		if hasSafeOutputFieldSet(safeOutputs, fieldName) {
			tools = append(tools, toolName)
		}
	}
	tools = append(tools, normalizedMapKeys(safeOutputs.Jobs)...)
	tools = append(tools, normalizedMapKeys(safeOutputs.Scripts)...)
	tools = append(tools, normalizedMapKeys(safeOutputs.Actions)...)

	if safeOutputs.DispatchWorkflow != nil {
		tools = append(tools, normalizedStrings(safeOutputs.DispatchWorkflow.Workflows)...)
	}
	if safeOutputs.DispatchRepository != nil {
		tools = append(tools, normalizedMapKeys(safeOutputs.DispatchRepository.Tools)...)
	}
	if safeOutputs.CallWorkflow != nil {
		tools = append(tools, normalizedStrings(safeOutputs.CallWorkflow.Workflows)...)
	}

	return tools
}

func anySliceToStrings(values []any) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if str, ok := value.(string); ok {
			result = append(result, str)
		}
	}
	return result
}

func stringsFromAnySlice(value any) []string {
	switch items := value.(type) {
	case []any:
		return anySliceToStrings(items)
	case []string:
		return append([]string(nil), items...)
	case string:
		if items != "" {
			return []string{items}
		}
		return []string{"*"}
	default:
		return []string{"*"}
	}
}

func normalizedMapKeys[V any](value map[string]V) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, stringutil.NormalizeSafeOutputIdentifier(key))
	}
	return keys
}

func normalizedStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, stringutil.NormalizeSafeOutputIdentifier(value))
	}
	return result
}

func normalizeManifestToolNames(tools []string) []string {
	seen := make(map[string]struct{}, len(tools))
	normalized := make([]string, 0, len(tools))
	for _, tool := range tools {
		tool = strings.TrimSpace(tool)
		if tool == "" {
			continue
		}
		if _, ok := seen[tool]; ok {
			continue
		}
		seen[tool] = struct{}{}
		normalized = append(normalized, tool)
	}
	sort.Strings(normalized)
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}
