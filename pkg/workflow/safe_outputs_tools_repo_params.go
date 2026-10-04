package workflow

import (
	"fmt"
	"reflect"
	"sync"
)

type repoTargetConfig struct {
	allowedRepos   []string
	targetRepoSlug string
}

type repoTargetAccessor func(*SafeOutputsConfig) *repoTargetConfig

var (
	repoTargetAccessors     map[string]repoTargetAccessor
	repoTargetAccessorsOnce sync.Once
)

func getRepoTargetAccessors() map[string]repoTargetAccessor {
	repoTargetAccessorsOnce.Do(func() {
		repoTargetAccessors = buildRepoTargetAccessors() //nolint:packagelevelmutableslicemap // sync.Once guards initialization.
	})
	return repoTargetAccessors
}

func buildRepoTargetAccessors() map[string]repoTargetAccessor {
	accessors := make(map[string]repoTargetAccessor)
	for _, handler := range safeOutputHandlers {
		if !isRepoTargetHandler(handler) {
			continue
		}

		accessors[handler.ToolName] = newRepoTargetAccessor(handler.StructField)
	}
	return accessors
}

func isRepoTargetHandler(handler safeOutputHandlerDescriptor) bool {
	if handler.NewConfig == nil {
		return false
	}
	configType := reflect.TypeOf(handler.NewConfig())
	if configType == nil {
		return false
	}
	outputField, hasOutputField := reflect.TypeFor[SafeOutputsConfig]().FieldByName(handler.StructField)
	if !hasOutputField || outputField.Type != configType {
		return false
	}
	allowedRepos, hasAllowedRepos := configType.Elem().FieldByName("AllowedRepos")
	targetRepoSlug, hasTargetRepoSlug := configType.Elem().FieldByName("TargetRepoSlug")
	return hasAllowedRepos && hasTargetRepoSlug &&
		allowedRepos.Type == reflect.TypeFor[[]string]() &&
		targetRepoSlug.Type.Kind() == reflect.String
}

func newRepoTargetAccessor(structField string) repoTargetAccessor {
	return func(config *SafeOutputsConfig) *repoTargetConfig {
		output := reflect.ValueOf(config).Elem().FieldByName(structField)
		if !output.IsValid() || output.IsNil() {
			return nil
		}
		output = output.Elem()
		allowedRepos, ok := output.FieldByName("AllowedRepos").Interface().([]string)
		if !ok {
			return nil
		}
		return &repoTargetConfig{
			allowedRepos:   allowedRepos,
			targetRepoSlug: output.FieldByName("TargetRepoSlug").String(),
		}
	}
}

// addRepoParameterIfNeeded adds a "repo" parameter to the tool's inputSchema
// if the safe output configuration has allowed-repos entries or a wildcard "*" target-repo
func addRepoParameterIfNeeded(tool map[string]any, toolName string, safeOutputs *SafeOutputsConfig) {
	safeOutputsConfigLog.Printf("Checking if repo parameter needed for tool: %s", toolName)
	if safeOutputs == nil {
		return
	}

	accessor, ok := getRepoTargetAccessors()[toolName]
	if !ok {
		return
	}
	targetConfig := accessor(safeOutputs)
	if targetConfig == nil {
		return
	}

	// Only add repo parameter if allowed-repos has entries or target-repo is wildcard ("*")
	if len(targetConfig.allowedRepos) == 0 && targetConfig.targetRepoSlug != "*" {
		safeOutputsConfigLog.Printf("Skipping repo parameter for tool %s: no allowed-repos and target-repo is not wildcard", toolName)
		return
	}

	// Get the inputSchema
	inputSchema, ok := tool["inputSchema"].(map[string]any)
	if !ok {
		return
	}

	properties, ok := inputSchema["properties"].(map[string]any)
	if !ok {
		return
	}

	// Build repo parameter description
	var repoDescription string
	if targetConfig.targetRepoSlug == "*" {
		repoDescription = "Target repository for this operation in 'owner/repo' format. Any repository can be targeted."
	} else if targetConfig.targetRepoSlug != "" {
		repoDescription = fmt.Sprintf("Target repository for this operation in 'owner/repo' format. Default is %q. Must be the target-repo or in the allowed-repos list.", targetConfig.targetRepoSlug)
	} else {
		repoDescription = "Target repository for this operation in 'owner/repo' format. Must be the target-repo or in the allowed-repos list."
	}

	// Add repo parameter to properties
	properties["repo"] = map[string]any{
		"type":        "string",
		"description": repoDescription,
	}

	safeOutputsConfigLog.Printf("Added repo parameter to tool: %s (has allowed-repos or wildcard target-repo)", toolName)
}

// computeRepoParamForTool returns the "repo" input parameter definition that should
// be added to a tool's inputSchema, or nil if no repo parameter is needed.
// This mirrors the logic in addRepoParameterIfNeeded but returns the param instead
// of modifying a tool in place, making it usable for generateToolsMetaJSON.
func computeRepoParamForTool(toolName string, safeOutputs *SafeOutputsConfig) map[string]any {
	safeOutputsConfigLog.Printf("Computing repo parameter definition for tool: %s", toolName)
	// Reuse addRepoParameterIfNeeded by passing a scratch tool with an empty inputSchema.
	scratch := map[string]any{
		"name":        toolName,
		"inputSchema": map[string]any{"properties": map[string]any{}},
	}
	addRepoParameterIfNeeded(scratch, toolName, safeOutputs)

	inputSchema, ok := scratch["inputSchema"].(map[string]any)
	if !ok {
		return nil
	}
	properties, ok := inputSchema["properties"].(map[string]any)
	if !ok {
		return nil
	}
	repoProp, ok := properties["repo"].(map[string]any)
	if !ok {
		safeOutputsConfigLog.Printf("No repo parameter generated for tool: %s", toolName)
		return nil
	}
	safeOutputsConfigLog.Printf("Repo parameter computed for tool: %s", toolName)
	return repoProp
}
