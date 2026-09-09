package workflow

import (
	"fmt"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/stringutil"
)

// ========================================
// Safe Output Configuration Generation
// ========================================
//
// This file generates the GH_AW_SAFE_OUTPUTS_CONFIG_PATH (config.json) consumed
// by the safe-outputs MCP server at startup and by the output ingestion step.
//
// Standard handler configuration is derived from the handlerRegistry defined in
// compiler_safe_outputs_config.go (the single source of truth for handler keys and
// field contracts). Non-handler global configuration (mentions, max_bot_mentions,
// safe_jobs, safe_scripts, push_repo_memory) is generated here because it is
// specific to config.json and not part of the handler registry.

// generateSafeOutputsConfig generates the JSON configuration for the safe-outputs
// MCP server. Standard handler configs are sourced from handlerRegistry to ensure
// they stay in sync with GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG.
func generateSafeOutputsConfig(data *WorkflowData) (string, error) {
	if data.SafeOutputs == nil {
		if data.CommentMemoryConfig == nil {
			safeOutputsConfigLog.Print("No safe outputs configuration found, returning empty config")
			return "", nil
		}
		data.SafeOutputs = &SafeOutputsConfig{}
	}
	safeOutputsConfigLog.Print("Generating safe outputs configuration for workflow")

	safeOutputsConfig := make(map[string]any)
	addStandardHandlerConfigs(safeOutputsConfig, data)
	if handlerConfig := buildCommentMemoryHandlerConfig(data.CommentMemoryConfig, data.SafeOutputs.Footer); handlerConfig != nil {
		safeOutputsConfig[commentMemoryHandlerKey] = handlerConfig
	}

	addSafeJobsConfig(safeOutputsConfig, data.SafeOutputs.Jobs)
	addSafeScriptsConfig(safeOutputsConfig, data.SafeOutputs.Scripts)
	if err := addSafeActionsConfig(safeOutputsConfig, data.SafeOutputs.Actions); err != nil {
		return "", err
	}
	addMentionsConfig(safeOutputsConfig, data.SafeOutputs)
	addPushRepoMemoryConfig(safeOutputsConfig, data.RepoMemoryConfig)

	if len(safeOutputsConfig) == 0 {
		return "", nil
	}
	// The agent job never depends on the custom jobs listed in safe-outputs.needs (those
	// dependencies are only wired onto the later safe_outputs handler job by
	// buildSafeOutputsJobNeeds). Any templated expression referencing needs.<job> for one of
	// those jobs would therefore be unresolvable inside the agent job and trip an actionlint
	// "undefined property" error. Neutralize such expressions here; the handler job's own copy
	// of the config (GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG) is unaffected and keeps the real value.
	sanitizeAgentSafeOutputsConfig(safeOutputsConfig, data.SafeOutputs.Needs)
	configJSON, err := marshalSafeOutputsConfig(safeOutputsConfig)
	if err != nil {
		return "", fmt.Errorf("marshaling safe-outputs config: %w", err)
	}
	safeOutputsConfigLog.Printf("Safe outputs config generation complete: %d tool types configured", len(safeOutputsConfig))
	return string(configJSON), nil
}

// addStandardHandlerConfigs adds config for every registered standard safe-output
// handler (same source as GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG) to safeOutputsConfig.
func addStandardHandlerConfigs(safeOutputsConfig map[string]any, data *WorkflowData) {
	engineManifestFiles, engineManifestPathPrefixes := getEngineAgentFileInfoFromWorkflowData(data)

	for handlerName, builder := range handlerRegistry {
		handlerCfg := builder(data.SafeOutputs)
		if handlerCfg == nil {
			continue
		}
		injectCurrentCheckoutPatchWorkspacePath(handlerName, handlerCfg, data)
		injectCheckoutMapping(handlerName, handlerCfg, data)
		excludeFiles := ParseStringArrayFromConfig(handlerCfg, "_protected_files_exclude", nil)
		// Strip the internal sentinel key used by the handler manager for compile-time
		// exclusion processing — it must not be forwarded to the runtime config.json.
		delete(handlerCfg, "_protected_files_exclude")
		if _, hasProtectedFiles := handlerCfg["protected_files"]; hasProtectedFiles {
			fullManifestFiles := getAllManifestFiles(engineManifestFiles...)
			fullPathPrefixes := getProtectedPathPrefixes(engineManifestPathPrefixes...)
			handlerCfg["protected_files"] = sliceutil.Exclude(fullManifestFiles, excludeFiles...)
			filteredPrefixes := sliceutil.Exclude(fullPathPrefixes, excludeFiles...)
			if len(filteredPrefixes) > 0 {
				handlerCfg["protected_path_prefixes"] = filteredPrefixes
			} else {
				delete(handlerCfg, "protected_path_prefixes")
			}
			// Compute which top-level dot-folder prefixes are excluded so the runtime
			// dot-folder check can skip them.
			if dotFolderExcludes := getDotFolderExcludes(excludeFiles); len(dotFolderExcludes) > 0 {
				handlerCfg["protected_dot_folder_excludes"] = dotFolderExcludes
			}
		}
		if data.SafeOutputs != nil && data.SafeOutputs.DataEnabled && isDataSchemaEnabledType(handlerName) {
			handlerCfg["data_enabled"] = true
			if data.SafeOutputs.NormalizedDataSchema != nil {
				handlerCfg["data_schema"] = data.SafeOutputs.NormalizedDataSchema
			} else if strings.TrimSpace(data.SafeOutputs.DataSchemaExpression) != "" {
				handlerCfg["data_schema"] = data.SafeOutputs.DataSchemaExpression
			}
		}
		safeOutputsConfig[handlerName] = handlerCfg
	}
}

// addMentionsConfig adds mentions and max-bot-mentions configuration (consumed by the
// ingestion step, not by standard handlers) to safeOutputsConfig.
func addMentionsConfig(safeOutputsConfig map[string]any, safeOutputs *SafeOutputsConfig) {
	// Mentions configuration: controls which @mentions are allowed in AI output.
	if safeOutputs.Mentions != nil {
		mentionsConfig := buildMentionsHandlerConfig(safeOutputs.Mentions)
		if len(mentionsConfig) > 0 {
			safeOutputsConfig["mentions"] = mentionsConfig
		}
	}

	// Max bot mentions: limits bot trigger references (e.g. "fixes #123") in AI output.
	// Store as integer when possible (matching original behavior), or as expression string.
	if safeOutputs.MaxBotMentions != nil {
		v := *safeOutputs.MaxBotMentions
		if n := templatableIntValue(safeOutputs.MaxBotMentions); n > 0 {
			safeOutputsConfig["max_bot_mentions"] = n
		} else if strings.HasPrefix(v, "${{") {
			safeOutputsConfig["max_bot_mentions"] = v
		}
	}
}

// addSafeJobsConfig adds safe-jobs configuration (custom output types that run as
// separate GitHub Actions jobs) to safeOutputsConfig. These are not standard handlers
// but must be in config.json so the ingestion step can validate and route those output types.
func addSafeJobsConfig(safeOutputsConfig map[string]any, jobs map[string]*SafeJobConfig) {
	if len(jobs) == 0 {
		return
	}
	safeOutputsConfigLog.Printf("Processing %d safe job configurations", len(jobs))
	for jobName, jobConfig := range jobs {
		safeOutputsConfigLog.Printf("Generating config for safe job: %s", jobName)
		safeJobConfig := map[string]any{}
		if jobConfig.Description != "" {
			safeJobConfig["description"] = jobConfig.Description
		}
		if jobConfig.Output != "" {
			safeJobConfig["output"] = jobConfig.Output
		}
		if jobConfig.Max > 0 {
			safeJobConfig["max"] = jobConfig.Max
		}
		if inputsConfig := buildSafeOutputInputsConfig(jobConfig.Inputs); inputsConfig != nil {
			safeJobConfig["inputs"] = inputsConfig
		}
		safeOutputsConfig[jobName] = safeJobConfig
	}
}

// addSafeScriptsConfig adds safe-scripts configuration (script output types handled
// inline by the handler manager) to safeOutputsConfig.
func addSafeScriptsConfig(safeOutputsConfig map[string]any, scripts map[string]*SafeScriptConfig) {
	if len(scripts) == 0 {
		return
	}
	safeOutputsConfigLog.Printf("Processing %d safe script configurations", len(scripts))
	for scriptName, scriptConfig := range scripts {
		normalizedName := stringutil.NormalizeSafeOutputIdentifier(scriptName)
		safeOutputsConfigLog.Printf("Generating config for safe script: %s (normalized: %s)", scriptName, normalizedName)
		safeScriptConfigMap := map[string]any{}
		if scriptConfig.Description != "" {
			safeScriptConfigMap["description"] = scriptConfig.Description
		}
		if inputsConfig := buildSafeOutputInputsConfig(scriptConfig.Inputs); inputsConfig != nil {
			safeScriptConfigMap["inputs"] = inputsConfig
		}
		safeOutputsConfig[normalizedName] = safeScriptConfigMap
	}
}

// buildSafeOutputInputsConfig converts safe-job/safe-script input definitions into
// the map[string]any shape expected in config.json, or nil when there are none.
func buildSafeOutputInputsConfig(inputs map[string]*InputDefinition) map[string]any {
	if len(inputs) == 0 {
		return nil
	}
	inputsConfig := make(map[string]any)
	for inputName, inputDef := range inputs {
		inputConfig := map[string]any{
			"type":        inputDef.Type,
			"description": inputDef.Description,
			"required":    inputDef.Required,
		}
		if inputDef.Default != "" {
			inputConfig["default"] = inputDef.Default
		}
		if len(inputDef.Options) > 0 {
			inputConfig["options"] = inputDef.Options
		}
		inputsConfig[inputName] = inputConfig
	}
	return inputsConfig
}

// addSafeActionsConfig adds safe-actions configuration (custom GitHub Actions exposed
// as safe output tools) to safeOutputsConfig. The normalized action names are added as
// config keys so both MCP server implementations recognise them as enabled tools (the
// tool schema is already in tools.json via tools_meta.json; the MCP server just needs
// to see the name in config.json).
func addSafeActionsConfig(safeOutputsConfig map[string]any, actions map[string]*SafeOutputActionConfig) error {
	if len(actions) == 0 {
		return nil
	}
	safeOutputsConfigLog.Printf("Processing %d safe action configurations", len(actions))
	for actionName := range actions {
		normalizedName := stringutil.NormalizeSafeOutputIdentifier(actionName)
		if _, exists := safeOutputsConfig[normalizedName]; exists {
			return fmt.Errorf(
				"safe-outputs action %q has a normalized name %q that conflicts with an existing safe outputs config entry; rename the action to avoid the conflict",
				actionName,
				normalizedName,
			)
		}
		safeOutputsConfigLog.Printf("Adding safe action to config: %s (normalized: %s)", actionName, normalizedName)
		safeOutputsConfig[normalizedName] = true
	}
	return nil
}

// addPushRepoMemoryConfig adds push_repo_memory configuration (enables the
// push_repo_memory MCP tool for early size/eligibility validation during the agent
// session) to safeOutputsConfig, when repo-memory is configured.
func addPushRepoMemoryConfig(safeOutputsConfig map[string]any, repoMemoryConfig *RepoMemoryConfig) {
	if repoMemoryConfig == nil || len(repoMemoryConfig.Memories) == 0 {
		return
	}
	var memories []map[string]any
	for _, memory := range repoMemoryConfig.Memories {
		memoryConfig := map[string]any{
			"id":             memory.ID,
			"dir":            constants.TmpRepoMemoryDir + memory.ID,
			"max_file_size":  memory.MaxFileSize,
			"max_patch_size": memory.MaxPatchSize,
			"max_file_count": memory.MaxFileCount,
		}
		if len(memory.AllowedExtensions) > 0 {
			memoryConfig["allowed_extensions"] = memory.AllowedExtensions
		}
		if len(memory.FileGlob) > 0 {
			memoryConfig["file_glob"] = strings.Join(memory.FileGlob, " ")
		}
		if memory.FormatJSON {
			memoryConfig["format_json"] = true
		}
		if memory.Validation != nil {
			memoryConfig["validation"] = map[string]any{
				"script":  memory.Validation.Script,
				"timeout": memoryValidationTimeoutSeconds(memory.Validation),
			}
		}
		memories = append(memories, memoryConfig)
	}
	safeOutputsConfig["push_repo_memory"] = map[string]any{
		"memories": memories,
	}
	safeOutputsConfigLog.Printf("Added push_repo_memory config with %d memory entries", len(repoMemoryConfig.Memories))
}

func getEngineAgentFileInfoFromWorkflowData(data *WorkflowData) (manifestFiles []string, pathPrefixes []string) {
	if data == nil || data.EngineConfig == nil {
		return nil, nil
	}

	engineRegistry := GetGlobalEngineRegistry()
	engine, err := engineRegistry.GetEngine(data.EngineConfig.ID)
	if err != nil {
		safeOutputsConfigLog.Printf("Engine lookup failed for %q: %v — skipping agent manifest file injection", data.EngineConfig.ID, err)
		return nil, nil
	}
	if engine == nil {
		return nil, nil
	}

	provider, ok := engine.(AgentFileProvider)
	if !ok {
		return nil, nil
	}

	return provider.GetAgentManifestFiles(), provider.GetAgentManifestPathPrefixes()
}

// populateCustomJobToolProperties fills properties (an MCP tool inputSchema.properties
// map) from the given safe-job input definitions, returning the names of required inputs.
func populateCustomJobToolProperties(properties map[string]any, inputs map[string]*InputDefinition) []string {
	var requiredFields []string
	for inputName, inputDef := range inputs {
		property := map[string]any{}

		if inputDef.Description != "" {
			property["description"] = inputDef.Description
		}

		switch inputDef.Type {
		case "choice":
			property["type"] = "string"
			if len(inputDef.Options) > 0 {
				property["enum"] = inputDef.Options
			}
		case "boolean":
			property["type"] = "boolean"
		case "number":
			property["type"] = "number"
		default:
			property["type"] = "string"
		}

		if inputDef.Default != nil {
			property["default"] = inputDef.Default
		}

		if inputDef.Required {
			requiredFields = append(requiredFields, inputName)
		}

		properties[inputName] = property
	}
	return requiredFields
}

// generateCustomJobToolDefinition creates an MCP tool definition for a custom safe-output job.
// Returns a map representing the tool definition in MCP format with name, description, and inputSchema.
func generateCustomJobToolDefinition(jobName string, jobConfig *SafeJobConfig) map[string]any {
	safeOutputsConfigLog.Printf("Generating tool definition for custom job: %s", jobName)

	description := jobConfig.Description
	if description == "" {
		description = fmt.Sprintf("Execute the %s custom job", jobName)
	}

	inputSchema := map[string]any{
		"type":                 "object",
		"properties":           make(map[string]any),
		"additionalProperties": false,
	}

	properties, ok := inputSchema["properties"].(map[string]any)
	if !ok {
		properties = make(map[string]any)
		inputSchema["properties"] = properties
	}
	requiredFields := populateCustomJobToolProperties(properties, jobConfig.Inputs)

	if len(requiredFields) > 0 {
		sort.Strings(requiredFields)
		inputSchema["required"] = requiredFields
	}

	safeOutputsConfigLog.Printf("Generated tool definition for %s with %d inputs, %d required",
		jobName, len(jobConfig.Inputs), len(requiredFields))

	return map[string]any{
		"name":        jobName,
		"description": description,
		"inputSchema": inputSchema,
	}
}
