package parser

import (
	"encoding/json"
	"strings"
)

// extractEngineConfig extracts engine-related settings from the imported frontmatter map
// and accumulates them. Engine configs with only `mcp` sub-keys (no `id` or `runtime`)
// are not counted as engine specifications — they carry MCP gateway settings only.
//
// Side effects: acc.engines, acc.mergedEngineMCPToolTimeout,
// acc.mergedEngineMCPSessionTimeout, acc.mergedEngineModel.
func (acc *importAccumulator) extractEngineConfig(fm map[string]any, fullPath string) {
	if _, ok := fm["model"]; ok {
		acc.engineModelChoiceDeclared = true
	}
	if modelStr, ok := fm["model"].(string); ok && modelStr != "" && acc.mergedEngineModel == "" {
		acc.mergedEngineModel = modelStr
		parserLog.Printf("Extracted top-level model preference from import %s: %s", fullPath, modelStr)
	}

	engineVal, hasEngine := fm["engine"]
	if !hasEngine {
		return
	}
	parserLog.Printf("Found engine config in import: %s", fullPath)

	switch v := engineVal.(type) {
	case string:
		// String engine (e.g. "copilot") — always counts as an engine spec.
		if engineJSON, merr := json.Marshal(v); merr == nil {
			acc.engines = append(acc.engines, string(engineJSON))
		}
	case map[string]any:
		// Object engine — extract engine.mcp.* settings first, then decide
		// whether to add to engines based on whether an engine ID is present.
		if mcpVal, hasMCP := v["mcp"]; hasMCP {
			acc.extractEngineMCPSettings(mcpVal, fullPath)
		}
		// Only add to engines list if this config specifies an actual engine
		// (i.e. it carries an 'id' or 'runtime' field). Configs with only
		// 'model' or 'mcp' settings are preferences, not engine selections,
		// and must not trigger the "multiple engine fields" validation error.
		_, hasID := v["id"]
		_, hasRuntime := v["runtime"]
		if hasID || hasRuntime {
			if engineJSON, merr := json.Marshal(v); merr == nil {
				acc.engines = append(acc.engines, string(engineJSON))
			}
		} else {
			// No engine ID or runtime — this is a model/MCP-only preference.
			// Extract the model hint (first-wins) so it can be applied to the
			// resolved engine after all imports are processed.
			for _, key := range []string{"model", "model-routing", "fallback-models"} {
				if _, ok := v[key]; ok {
					acc.engineModelChoiceDeclared = true
				}
			}
			if modelStr, ok := v["model"].(string); ok && modelStr != "" {
				if acc.mergedEngineModel == "" {
					acc.mergedEngineModel = modelStr
					parserLog.Printf("Extracted engine.model preference from import %s: %s", fullPath, modelStr)
				}
			}
		}
	default:
		// Unexpected type — marshal and add to preserve existing behavior.
		if engineJSON, merr := json.Marshal(engineVal); merr == nil {
			acc.engines = append(acc.engines, string(engineJSON))
		}
	}
}

// extractEngineMCPSettings extracts engine.mcp.tool-timeout and engine.mcp.session-timeout
// from mcpVal (first-wins across all imports).
func (acc *importAccumulator) extractEngineMCPSettings(mcpVal any, fullPath string) {
	mcpMap, ok := mcpVal.(map[string]any)
	if !ok {
		return
	}
	if acc.mergedEngineMCPToolTimeout == "" {
		if ttStr, ok := mcpMap["tool-timeout"].(string); ok && ttStr != "" {
			acc.mergedEngineMCPToolTimeout = ttStr
			parserLog.Printf("Extracted engine.mcp.tool-timeout from import %s: %s", fullPath, ttStr)
		}
	}
	if acc.mergedEngineMCPSessionTimeout == "" {
		if stStr, ok := mcpMap["session-timeout"].(string); ok && stStr != "" {
			acc.mergedEngineMCPSessionTimeout = stStr
			parserLog.Printf("Extracted engine.mcp.session-timeout from import %s: %s", fullPath, stStr)
		}
	}
}

// extractConfigFields extracts scalar and builder-based configuration fields from the
// frontmatter map and writes them into the appropriate accumulator builders and slices.
//
// Side effects: acc.mergedMaxTurns, acc.mergedMaxToolDenials, acc.mergedMaxToolCalls, acc.mergedMaxRuns, acc.mergedMaxAICredits,
// acc.mergedMaxDailyAICredits, acc.mcpServersBuilder,
// acc.safeOutputs, acc.mcpScripts, acc.stepsBuilder, acc.runtimesBuilder,
// acc.servicesBuilder, acc.networkBuilder, acc.permissionsBuilder,
// acc.secretMaskingBuilder.
func (acc *importAccumulator) extractConfigFields(fm map[string]any, fullPath string) {
	acc.extractFirstWinsJSONField(fm, fullPath, "max-turns", &acc.mergedMaxTurns)
	acc.extractFirstWinsJSONField(fm, fullPath, "max-tool-denials", &acc.mergedMaxToolDenials)
	acc.extractFirstWinsJSONField(fm, fullPath, "max-tool-calls", &acc.mergedMaxToolCalls)
	acc.extractFirstWinsJSONField(fm, fullPath, "max-runs", &acc.mergedMaxRuns)
	acc.extractFirstWinsJSONField(fm, fullPath, "max-turn-cache-misses", &acc.mergedMaxTurnCacheMisses)
	acc.extractFirstWinsJSONField(fm, fullPath, "max-ai-credits", &acc.mergedMaxAICredits)
	acc.extractFirstWinsJSONField(fm, fullPath, "max-daily-ai-credits", &acc.mergedMaxDailyAICredits)
	acc.extractConcurrencyInImport(fm, fullPath)
	acc.extractConcurrencyJobDiscriminator(fm, fullPath)
	if acc.metadataDocs == "" {
		if metadata, ok := fm["metadata"].(map[string]any); ok {
			if docs, ok := metadata["docs"].(string); ok {
				acc.metadataDocs = strings.TrimSpace(docs)
			}
		}
	}

	acc.appendJSONBuilderField(fm, "mcp-servers", "{}", &acc.mcpServersBuilder)
	acc.extractPlugins(fm)
	acc.appendJSONSliceField(fm, "safe-outputs", "{}", &acc.safeOutputs)
	acc.appendJSONBuilderField(fm, "graders", "{}", &acc.gradersBuilder)
	if graders, ok := fm["graders"].(map[string]any); ok {
		for id, value := range graders {
			if definition, ok := value.(map[string]any); ok {
				if _, hasRun := definition["run"]; hasRun {
					acc.graderRunSources[id] = fullPath
				}
			}
		}
	}
	acc.appendJSONSliceField(fm, "mcp-scripts", "{}", &acc.mcpScripts)
	acc.appendJSONBuilderField(fm, "runtimes", "{}", &acc.runtimesBuilder)
	acc.appendYAMLBuilderField(fm, "services", &acc.servicesBuilder)
	acc.appendJSONBuilderField(fm, "network", "{}", &acc.networkBuilder)
	acc.mergeSandboxAgentMounts(fm)
	acc.appendJSONBuilderField(fm, "permissions", "{}", &acc.permissionsBuilder)
	acc.appendJSONBuilderField(fm, "secret-masking", "{}", &acc.secretMaskingBuilder)
}
func (acc *importAccumulator) extractConcurrencyInImport(fm map[string]any, fullPath string) {
	if acc.mergedConcurrency != "" {
		return
	}
	concurrencyValue, ok := fm["concurrency"]
	if !ok {
		return
	}
	if group, ok := concurrencyValue.(string); ok && strings.TrimSpace(group) != "" {
		groupJSON := mustMarshalJSON(group)
		if len(groupJSON) == 0 {
			parserLog.Printf("Skipping invalid concurrency.group from import: %s", fullPath)
			return
		}
		acc.mergedConcurrency = string(groupJSON)
		parserLog.Printf("Extracted concurrency.group from import: %s", fullPath)
		return
	}
	concurrencyMap, ok := concurrencyValue.(map[string]any)
	if !ok {
		return
	}
	groupValue, ok := concurrencyMap["group"].(string)
	if !ok || strings.TrimSpace(groupValue) == "" {
		return
	}
	groupJSON := mustMarshalJSON(map[string]any{"group": groupValue})
	if len(groupJSON) == 0 {
		parserLog.Printf("Skipping invalid concurrency.group from import: %s", fullPath)
		return
	}
	acc.mergedConcurrency = string(groupJSON)
	parserLog.Printf("Extracted concurrency.group from import: %s", fullPath)
}
func (acc *importAccumulator) extractConcurrencyJobDiscriminator(fm map[string]any, fullPath string) {
	if acc.mergedJobDiscriminator != "" {
		return
	}
	concurrency, ok := fm["concurrency"].(map[string]any)
	if !ok {
		return
	}
	discriminator, ok := concurrency["job-discriminator"].(string)
	if !ok || discriminator == "" {
		return
	}
	acc.mergedJobDiscriminator = discriminator
	parserLog.Printf("Extracted concurrency.job-discriminator from import: %s", fullPath)
}
func mustMarshalJSON(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return data
}
func (acc *importAccumulator) mergeSandboxAgentMounts(fm map[string]any) {
	sandboxVal, hasSandbox := fm["sandbox"]
	if !hasSandbox {
		return
	}

	sandboxMap, ok := sandboxVal.(map[string]any)
	if !ok {
		return
	}

	agentVal, hasAgent := sandboxMap["agent"]
	if !hasAgent {
		return
	}

	agentMap, ok := agentVal.(map[string]any)
	if !ok {
		return
	}

	mountsVal, hasMounts := agentMap["mounts"]
	if !hasMounts {
		return
	}

	mounts, ok := mountsVal.([]any)
	if !ok {
		return
	}

	for _, mountVal := range mounts {
		mount, ok := mountVal.(string)
		if !ok || mount == "" {
			continue
		}
		if !acc.sandboxAgentMountsSet[mount] {
			acc.sandboxAgentMountsSet[mount] = true
			acc.sandboxAgentMounts = append(acc.sandboxAgentMounts, mount)
		}
	}
}
func (acc *importAccumulator) extractFirstWinsJSONField(fm map[string]any, fullPath, field string, target *string) {
	if *target != "" {
		return
	}
	fieldJSON, err := extractFieldJSONFromMap(fm, field, "")
	if err != nil || fieldJSON == "" || fieldJSON == "null" {
		return
	}
	*target = fieldJSON
	parserLog.Printf("Extracted %s from import: %s", field, fullPath)
}
func (acc *importAccumulator) appendJSONBuilderField(fm map[string]any, field, emptyValue string, builder *strings.Builder) {
	content, err := extractFieldJSONFromMap(fm, field, emptyValue)
	if err != nil || content == "" || content == emptyValue {
		return
	}
	builder.WriteString(content + "\n")
}
func (acc *importAccumulator) appendJSONSliceField(fm map[string]any, field, emptyValue string, target *[]string) {
	content, err := extractFieldJSONFromMap(fm, field, emptyValue)
	if err != nil || content == "" || content == emptyValue {
		return
	}
	*target = append(*target, content)
}
func (acc *importAccumulator) appendYAMLBuilderField(fm map[string]any, field string, builder *strings.Builder) {
	content, err := extractYAMLFieldFromMap(fm, field)
	if err != nil || content == "" {
		return
	}
	builder.WriteString(content + "\n")
}
