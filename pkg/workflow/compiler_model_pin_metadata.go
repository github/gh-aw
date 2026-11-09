package workflow

// projectModelPinMetadata is the document metadata contract consumed by the
// runtime model-pin auditor. Artifact records retain every authored field; this
// projection excludes workflow scripts and unrelated configuration from the
// per-repository model inventory embedded in each lock.
func projectModelPinMetadata(value any) any {
	root, ok := value.(map[string]any)
	if !ok {
		return value
	}
	result := metadataFields(root, "model", "models")
	if imports, exists := root["imports"]; exists {
		result["imports"] = modelPinImportsMetadata(imports)
	}
	if models, ok := result["models"].(map[string]any); ok {
		aliases := map[string]any{}
		for key, value := range models {
			if key != "providers" && key != "default-ai-credits-pricing" && key != "allowed" && key != "blocked" {
				aliases[key] = value
			}
		}
		result["models"] = aliases
	}
	if engine, exists := root["engine"]; exists {
		result["engine"] = modelPinEngineMetadata(engine)
	}
	if safe, ok := root["safe-outputs"].(map[string]any); ok {
		out := map[string]any{}
		if detection, exists := safe["threat-detection"]; exists {
			if config, ok := detection.(map[string]any); ok {
				target := metadataFields(config, "model", "enabled")
				detection = target
				for _, key := range []string{"engine", "engine-config"} {
					if engine, exists := config[key]; exists {
						target[key] = modelPinEngineMetadata(engine)
					}
				}
			}
			out["threat-detection"] = detection
		}
		if assignment, ok := safe["assign-to-agent"].(map[string]any); ok {
			out["assign-to-agent"] = metadataFields(assignment, "model")
		}
		result["safe-outputs"] = out
	}
	if evals, exists := root["evals"]; exists {
		if config, ok := evals.(map[string]any); ok {
			target := metadataFields(config, "model")
			if questions, exists := config["questions"]; exists {
				target["questions"] = modelPinArrayMetadata(questions, "model")
			}
			evals = target
		} else {
			evals = modelPinArrayMetadata(evals, "model")
		}
		result["evals"] = evals
	}
	if enclaves, ok := root["enclaves"].([]any); ok {
		result["enclaves"] = modelPinEnclavesMetadata(enclaves)
	}
	return result
}

func modelPinImportsMetadata(imports any) any {
	if config, ok := imports.(map[string]any); ok {
		target := metadataFields(config, "aw")
		if list, exists := target["aw"]; exists {
			target["aw"] = modelPinArrayMetadata(list, "path", "uses")
		}
		return target
	}
	return modelPinArrayMetadata(imports, "path", "uses")
}

func modelPinEnclavesMetadata(enclaves []any) []any {
	result := make([]any, 0, len(enclaves))
	for _, enclave := range enclaves {
		value := map[string]any{}
		if config, ok := enclave.(map[string]any); ok {
			if agent, ok := config["agent"].(map[string]any); ok {
				value = map[string]any{"agent": metadataFields(agent, "model", "engine")}
			}
		}
		result = append(result, value)
	}
	return result
}

func metadataFields(source map[string]any, keys ...string) map[string]any {
	result := map[string]any{}
	for _, key := range keys {
		if value, exists := source[key]; exists {
			result[key] = value
		}
	}
	return result
}

func modelPinEngineMetadata(value any) any {
	engine, ok := value.(map[string]any)
	if !ok {
		return value
	}
	result := metadataFields(engine, "id", "runtime", "model", "models", "fallback-models", "model-routing", "model-provider", "detection-engine")
	if runtime, ok := result["runtime"].(map[string]any); ok {
		result["runtime"] = metadataFields(runtime, "id")
	}
	if models, ok := result["models"].(map[string]any); ok {
		result["models"] = metadataFields(models, "default", "supported")
	}
	if routing, ok := result["model-routing"].(map[string]any); ok {
		result["model-routing"] = metadataFields(routing, "allowed-models")
	}
	if provider, exists := engine["provider"]; exists {
		if config, ok := provider.(map[string]any); ok {
			provider = metadataFields(config, "id", "name", "model")
		}
		result["provider"] = provider
	}
	if env, ok := engine["env"].(map[string]any); ok {
		result["env"] = metadataFields(env, "COPILOT_PROVIDER_BASE_URL")
	}
	if behaviors, ok := engine["behaviors"].(map[string]any); ok && behaviors != nil {
		result["behaviors"] = map[string]any{}
	}
	return result
}

func modelPinArrayMetadata(value any, keys ...string) any {
	items, ok := value.([]any)
	if !ok {
		return value
	}
	result := make([]any, 0, len(items))
	for _, item := range items {
		if config, ok := item.(map[string]any); ok {
			item = metadataFields(config, keys...)
		}
		result = append(result, item)
	}
	return result
}
