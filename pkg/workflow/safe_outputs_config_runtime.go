package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/sliceutil"
)

// SafeOutputStepConfig holds configuration for building a single safe output step
// within the consolidated safe-outputs job
type SafeOutputStepConfig struct {
	StepName                   string            // Human-readable step name (e.g., "Create Issue")
	StepID                     string            // Step ID for referencing outputs (e.g., "create_issue")
	Script                     string            // JavaScript script to execute (for inline mode)
	ScriptName                 string            // Name of the script in the registry (for file mode)
	CustomEnvVars              []string          // Environment variables specific to this step
	Condition                  ConditionNode     // Step-level condition (if clause)
	Token                      string            // GitHub token for this step
	UseCopilotRequestsToken    bool              // Whether to use Copilot requests token preference chain
	UseCopilotCodingAgentToken bool              // Whether to use Copilot coding agent token preference chain
	PreSteps                   []string          // Optional steps to run before the script step
	PostSteps                  []string          // Optional steps to run after the script step
	Outputs                    map[string]string // Outputs from this step
	ContinueOnError            bool              // Whether to continue the job even if this step fails (continue-on-error: true)
}

func (c *Compiler) addHandlerManagerConfigEnvVar(steps *[]string, data *WorkflowData) {
	if data.SafeOutputs == nil {
		safeOutputsConfigLog.Print("No safe-outputs configuration, skipping handler manager config")
		return
	}

	safeOutputsConfigLog.Print("Building handler manager configuration for safe-outputs")
	// config holds both per-handler configs (keyed by handler name, e.g. "add_comment") and
	// global runtime knobs (e.g. "mentions") that safe_output_handler_manager.cjs forwards to
	// specific handlers at startup. Handler names are the reserved keys defined in handlerRegistry;
	// non-handler keys ("mentions") are documented in safe_outputs_config_generation.go.
	config := make(map[string]any)

	// Collect engine-specific manifest files and path prefixes (AgentFileProvider interface).
	// These are merged with the global runtime-derived lists so that engine-specific
	// instruction files (e.g. CLAUDE.md, .claude/, AGENTS.md) are automatically protected.
	extraManifestFiles, extraPathPrefixes := c.getEngineAgentFileInfo(data)
	fullManifestFiles := getAllManifestFiles(extraManifestFiles...)
	fullPathPrefixes := getProtectedPathPrefixes(extraPathPrefixes...)

	// For workflow_call relay workflows, inject the resolved platform repo and ref into the
	// dispatch_workflow handler config so dispatch targets the host repo, not the caller's.
	safeOutputs := data.SafeOutputs
	if hasWorkflowCallTrigger(data.On) && safeOutputs.DispatchWorkflow != nil {
		if safeOutputs.DispatchWorkflow.TargetRepoSlug == "" {
			safeOutputs = safeOutputsWithDispatchTargetRepo(safeOutputs, "${{ needs.activation.outputs.target_repo }}")
			safeOutputsConfigLog.Print("Injecting target_repo into dispatch_workflow config for workflow_call relay")
		}
		if safeOutputs.DispatchWorkflow.TargetRef == "" {
			safeOutputs = safeOutputsWithDispatchTargetRef(safeOutputs, "${{ needs.activation.outputs.target_ref }}")
			safeOutputsConfigLog.Print("Injecting target_ref into dispatch_workflow config for workflow_call relay")
		}
	}

	// Build configuration for each handler using the registry
	for handlerName, builder := range handlerRegistry {
		handlerConfig := builder(safeOutputs)
		// Include handler if:
		// 1. It returns a non-nil config (explicitly enabled, even if empty)
		// 2. For auto-enabled handlers, include even with empty config
		if handlerConfig != nil {
			injectCurrentCheckoutPatchWorkspacePath(handlerName, handlerConfig, data)
			injectCheckoutMapping(handlerName, handlerConfig, data)
			// Augment protected-files protection with engine-specific files for handlers that use it.
			if _, hasProtected := handlerConfig["protected_files"]; hasProtected {
				// Extract per-handler exclusions set by the handler builder (sentinel key).
				// These are compile-time overrides and must not be forwarded to the runtime.
				excludeFiles := ParseStringArrayFromConfig(handlerConfig, "_protected_files_exclude", nil)
				delete(handlerConfig, "_protected_files_exclude")

				handlerConfig["protected_files"] = sliceutil.Exclude(fullManifestFiles, excludeFiles...)
				filteredPrefixes := sliceutil.Exclude(fullPathPrefixes, excludeFiles...)
				if len(filteredPrefixes) > 0 {
					handlerConfig["protected_path_prefixes"] = filteredPrefixes
				} else {
					delete(handlerConfig, "protected_path_prefixes")
				}
				// Compute which top-level dot-folder prefixes are excluded so the runtime
				// dot-folder check can skip them.
				if dotFolderExcludes := getDotFolderExcludes(excludeFiles); len(dotFolderExcludes) > 0 {
					handlerConfig["protected_dot_folder_excludes"] = dotFolderExcludes
				}
			}
			safeOutputsConfigLog.Printf("Adding %s handler configuration", handlerName)
			config[handlerName] = handlerConfig
		}
	}
	if handlerConfig := buildCommentMemoryHandlerConfig(data.CommentMemoryConfig, safeOutputs.Footer); handlerConfig != nil {
		config[commentMemoryHandlerKey] = handlerConfig
	}

	// Include top-level mentions configuration so the handler manager can pass it to
	// markdown-producing handlers that call sanitizeContent with allowed aliases.
	if safeOutputs.Mentions != nil {
		mentionsCfg := buildMentionsHandlerConfig(safeOutputs.Mentions)
		if len(mentionsCfg) > 0 {
			config["mentions"] = mentionsCfg
		}
	}

	// Only add the env var if there are handlers to configure
	if len(config) > 0 {
		safeOutputsConfigLog.Printf("Marshaling handler config with %d handlers", len(config))
		configJSON, err := marshalSafeOutputsConfig(config)
		if err != nil {
			safeOutputsConfigLog.Printf("Failed to marshal handler config: %v", err)
			return
		}
		// Escape the JSON for YAML (handle quotes and special chars)
		configStr := string(configJSON)
		*steps = append(*steps, fmt.Sprintf("          GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: %q\n", configStr))
		safeOutputsConfigLog.Printf("Added handler config env var: size=%d bytes", len(configStr))
	} else {
		safeOutputsConfigLog.Print("No handlers configured, skipping config env var")
	}
}

// buildMentionsHandlerConfig converts a MentionsConfig into the map format used by
// GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG so safe_output_handler_manager.cjs can pass
// the top-level mentions policy through to mention-aware handlers.
func buildMentionsHandlerConfig(m *MentionsConfig) map[string]any {
	cfg := make(map[string]any)
	if m.Enabled != nil {
		cfg["enabled"] = *m.Enabled
	}
	if m.AllowedCollaborators != nil {
		cfg["allowedCollaborators"] = *m.AllowedCollaborators
	}
	if m.AllowContext != nil {
		cfg["allowContext"] = *m.AllowContext
	}
	if len(m.Allowed) > 0 {
		cfg["allowed"] = m.Allowed
	}
	if len(m.AllowedTeams) > 0 {
		cfg["allowedTeams"] = m.AllowedTeams
	}
	if m.Max != nil {
		if n := templatableIntValue(m.Max); n > 0 {
			cfg["max"] = n
		} else {
			cfg["max"] = *m.Max
		}
	}
	return cfg
}

// safeOutputsWithDispatchTargetRepo returns a shallow copy of cfg with the dispatch_workflow
// TargetRepoSlug overridden to targetRepo. Only DispatchWorkflow is deep-copied; all other
// pointer fields remain shared. This avoids mutating the original config.
func safeOutputsWithDispatchTargetRepo(cfg *SafeOutputsConfig, targetRepo string) *SafeOutputsConfig {
	dispatchCopy := *cfg.DispatchWorkflow
	dispatchCopy.TargetRepoSlug = targetRepo
	configCopy := *cfg
	configCopy.DispatchWorkflow = &dispatchCopy
	return &configCopy
}

// safeOutputsWithDispatchTargetRef returns a shallow copy of cfg with the dispatch_workflow
// TargetRef overridden to targetRef. Only DispatchWorkflow is deep-copied; all other
// pointer fields remain shared. This avoids mutating the original config.
func safeOutputsWithDispatchTargetRef(cfg *SafeOutputsConfig, targetRef string) *SafeOutputsConfig {
	dispatchCopy := *cfg.DispatchWorkflow
	dispatchCopy.TargetRef = targetRef
	configCopy := *cfg
	configCopy.DispatchWorkflow = &dispatchCopy
	return &configCopy
}

// getEngineAgentFileInfo returns the engine-specific manifest filenames and path prefixes
// by type-asserting the active engine to AgentFileProvider.  Returns empty slices when
// the engine is not set or does not implement the interface.
func (c *Compiler) getEngineAgentFileInfo(data *WorkflowData) (manifestFiles []string, pathPrefixes []string) {
	if data == nil || data.EngineConfig == nil {
		return nil, nil
	}
	engine, err := c.engineRegistry.GetEngine(data.EngineConfig.ID)
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
	safeOutputsConfigLog.Printf("Engine %s provides AgentFileProvider: files=%v, prefixes=%v",
		data.EngineConfig.ID, provider.GetAgentManifestFiles(), provider.GetAgentManifestPathPrefixes())
	return provider.GetAgentManifestFiles(), provider.GetAgentManifestPathPrefixes()
}
