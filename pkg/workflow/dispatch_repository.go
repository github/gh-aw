package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stringutil"
)

var dispatchRepositoryLog = logger.New("workflow:dispatch_repository")

// DispatchRepositoryToolConfig defines a single repository dispatch tool within dispatch_repository
type DispatchRepositoryToolConfig struct {
	Description         string           `yaml:"description,omitempty"`          // Human-readable description
	Workflow            string           `yaml:"workflow"`                       // Target workflow name (for traceability and payload)
	EventType           string           `yaml:"event_type"`                     // repository_dispatch event_type
	Repository          string           `yaml:"repository,omitempty"`           // Single target repository (owner/repo)
	AllowedRepositories []string         `yaml:"allowed_repositories,omitempty"` // Multiple allowed target repositories
	Inputs              map[string]any   `yaml:"inputs,omitempty"`               // Input schema (similar to workflow_dispatch inputs)
	Max                 *string          `yaml:"max,omitempty"`                  // Max dispatch executions (templatable int)
	GitHubToken         string           `yaml:"github-token,omitempty"`         // Optional override token
	GitHubApp           *GitHubAppConfig `yaml:"github-app,omitempty"`           // Optional per-tool GitHub App override
	Staged              *TemplatableBool `yaml:"staged,omitempty"`               // Templatable preview-only mode
}

// DispatchRepositoryConfig holds configuration for dispatching repository_dispatch events
// Uses a map-of-tools pattern where each key defines a named dispatch tool
type DispatchRepositoryConfig struct {
	Tools map[string]*DispatchRepositoryToolConfig // Map of tool name to tool config
}

// parseDispatchRepositoryConfig parses dispatch-repository configuration from the safe-outputs map.
func (c *Compiler) parseDispatchRepositoryConfig(outputMap map[string]any) *DispatchRepositoryConfig {
	dispatchRepositoryLog.Print("Parsing dispatch-repository configuration")

	var configData any
	var exists bool

	// dispatch-repository is canonical; keep underscore form as a backward-compatible alias.
	if configData, exists = outputMap["dispatch-repository"]; !exists {
		if configData, exists = outputMap["dispatch_repository"]; !exists {
			return nil
		}
		dispatchRepositoryLog.Print("WARNING: safe-outputs.dispatch_repository is deprecated; rename to dispatch-repository or run `gh aw fix`")
	}

	configMap, ok := configData.(map[string]any)
	if !ok {
		dispatchRepositoryLog.Print("dispatch-repository value is not a map, skipping")
		return nil
	}

	dispatchRepositoryLog.Printf("Parsing dispatch-repository tools map with %d entries", len(configMap))

	dispatchRepoConfig := &DispatchRepositoryConfig{
		Tools: make(map[string]*DispatchRepositoryToolConfig),
	}

	for toolKey, toolValue := range configMap {
		toolMap, ok := toolValue.(map[string]any)
		if !ok {
			dispatchRepositoryLog.Printf("Skipping tool %q: value is not a map", toolKey)
			continue
		}

		tool := c.parseDispatchRepositoryTool(toolMap)

		// Cap max at 50
		if maxVal := templatableIntValue(tool.Max); maxVal > 50 {
			dispatchRepositoryLog.Printf("Tool %q: max value %d exceeds limit, capping at 50", toolKey, maxVal)
			tool.Max = defaultIntStr(50)
		}

		dispatchRepositoryLog.Printf("Parsed dispatch-repository tool %q: workflow=%s, event_type=%s, max=%v",
			toolKey, tool.Workflow, tool.EventType, tool.Max)

		dispatchRepoConfig.Tools[toolKey] = tool
	}

	if len(dispatchRepoConfig.Tools) == 0 {
		dispatchRepositoryLog.Print("No valid tools found in dispatch-repository config")
		return nil
	}

	return dispatchRepoConfig
}

func (c *Compiler) parseDispatchRepositoryTool(toolMap map[string]any) *DispatchRepositoryToolConfig {
	tool := &DispatchRepositoryToolConfig{}

	if desc, ok := toolMap["description"].(string); ok {
		tool.Description = desc
	}

	if workflow, ok := toolMap["workflow"].(string); ok {
		tool.Workflow = workflow
	}

	if eventType, ok := toolMap["event_type"].(string); ok {
		tool.EventType = eventType
	}

	if repo, ok := toolMap["repository"].(string); ok {
		tool.Repository = repo
	}

	// Parse allowed_repositories (list of repos)
	if allowedReposRaw, exists := toolMap["allowed_repositories"]; exists {
		if allowedReposList, ok := allowedReposRaw.([]any); ok {
			for _, r := range allowedReposList {
				if rStr, ok := r.(string); ok {
					tool.AllowedRepositories = append(tool.AllowedRepositories, rStr)
				}
			}
		}
	}

	// Parse inputs (map of input definitions)
	if inputsRaw, exists := toolMap["inputs"]; exists {
		if inputsMap, ok := inputsRaw.(map[string]any); ok {
			tool.Inputs = inputsMap
		}
	}

	// Parse max (templatable int, default 1)
	var baseCfg BaseSafeOutputConfig
	c.parseBaseSafeOutputConfig(toolMap, &baseCfg, 1)
	tool.Max = baseCfg.Max
	tool.GitHubToken = baseCfg.GitHubToken
	tool.GitHubApp = baseCfg.GitHubApp
	tool.Staged = baseCfg.Staged

	return tool
}

func dispatchRepositoryToolAppTokenStepID(toolKey string) string {
	return "dispatch-repository-" + stringutil.NormalizeSafeOutputIdentifier(toolKey) + "-app-token"
}

// generateDispatchRepositoryTool generates an MCP tool definition for a specific dispatch-repository tool.
// The tool will be named after the tool key (normalized to underscores) and accept
// the tool's declared inputs as parameters.
func generateDispatchRepositoryTool(toolKey string, toolConfig *DispatchRepositoryToolConfig) map[string]any {
	dispatchRepositoryLog.Printf("Generating dispatch-repository tool: key=%s", toolKey)

	// Normalize tool key to use underscores
	toolName := stringutil.NormalizeSafeOutputIdentifier(toolKey)

	description := toolConfig.Description
	if description == "" {
		description = "Dispatch a repository_dispatch event"
		if toolConfig.EventType != "" {
			description += " with event_type: " + toolConfig.EventType
		}
	}

	if toolConfig.Workflow != "" {
		description += " (targets workflow: " + toolConfig.Workflow + ")"
	}

	// Build input schema from the tool's inputs definition
	properties, required := buildInputSchema(toolConfig.Inputs, func(inputName string) string {
		return "Input parameter '" + inputName + "'"
	})

	inputSchema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}

	if len(required) > 0 {
		inputSchema["required"] = required
	}

	tool := map[string]any{
		"name":                      toolName,
		"description":               description,
		"_dispatch_repository_tool": toolKey, // Internal metadata for handler routing
		"inputSchema":               inputSchema,
	}

	dispatchRepositoryLog.Printf("Generated dispatch-repository tool: name=%s, properties=%d", toolName, len(properties))
	return tool
}
