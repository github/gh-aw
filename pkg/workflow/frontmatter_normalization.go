package workflow

import "maps"

// normalizeFrontmatterConfig adapts schema-valid shorthands for typed JSON
// unmarshaling without changing the raw frontmatter used by other compiler paths.
func normalizeFrontmatterConfig(frontmatter map[string]any) (map[string]any, error) {
	normalized := maps.Clone(frontmatter)
	normalizeTriggerShorthand(normalized)
	normalizeToolsShorthands(normalized)
	normalizePermissionsShorthand(normalized)
	normalizeStringObjectShorthands(normalized)
	normalizeSandboxShorthands(normalized)
	if err := normalizeSafeOutputShorthands(normalized); err != nil {
		return nil, err
	}
	return normalized, nil
}

func normalizeTriggerShorthand(frontmatter map[string]any) {
	if trigger, ok := frontmatter["on"].(string); ok {
		frontmatter["on"] = map[string]any{trigger: nil}
	}
}

func normalizeToolsShorthands(frontmatter map[string]any) {
	tools, ok := frontmatter["tools"].(map[string]any)
	if !ok {
		return
	}
	normalized := maps.Clone(tools)
	changed := false
	for name, value := range normalized {
		switch name {
		case "github":
			if enabled, ok := value.(bool); ok {
				if enabled {
					normalized[name] = map[string]any{}
				} else {
					normalized[name] = nil
				}
				changed = true
				continue
			}
			if mode, ok := value.(string); ok {
				normalized[name] = map[string]any{"mode": mode}
				changed = true
			}
		case "bash", "edit":
			if enabled, ok := value.(bool); ok {
				if enabled {
					normalized[name] = map[string]any{}
				} else {
					normalized[name] = nil
				}
				changed = true
			}
		}
	}
	if changed {
		frontmatter["tools"] = normalized
	}
}

func normalizePermissionsShorthand(frontmatter map[string]any) {
	permission, ok := frontmatter["permissions"].(string)
	if ok {
		frontmatter["permissions"] = map[string]any{permission: permission}
	}
}

func normalizeStringObjectShorthands(frontmatter map[string]any) {
	if network, ok := frontmatter["network"].(string); ok {
		frontmatter["network"] = map[string]any{"allowed": []any{network}}
	}
	if environment, ok := frontmatter["environment"].(string); ok {
		frontmatter["environment"] = map[string]any{"name": environment}
	}
	if concurrency, ok := frontmatter["concurrency"].(string); ok {
		frontmatter["concurrency"] = map[string]any{"group": concurrency}
	}
	if container, ok := frontmatter["container"].(string); ok {
		frontmatter["container"] = map[string]any{"image": container}
	}
}

func normalizeSandboxShorthands(frontmatter map[string]any) {
	if sandboxType, ok := frontmatter["sandbox"].(string); ok {
		frontmatter["sandbox"] = map[string]any{"type": sandboxType}
		return
	}
	sandbox, ok := frontmatter["sandbox"].(map[string]any)
	if !ok {
		return
	}
	agent := sandbox["agent"]
	switch value := agent.(type) {
	case bool:
		if !value {
			normalized := maps.Clone(sandbox)
			normalized["agent"] = map[string]any{"disabled": true}
			frontmatter["sandbox"] = normalized
		}
	case string:
		normalized := maps.Clone(sandbox)
		normalized["agent"] = map[string]any{"id": value}
		frontmatter["sandbox"] = normalized
	}
}

func normalizeSafeOutputShorthands(frontmatter map[string]any) error {
	outputs, ok := frontmatter["safe-outputs"].(map[string]any)
	if !ok {
		return nil
	}
	normalized := maps.Clone(outputs)
	changed := false
	for name, value := range normalized {
		switch name {
		case "noop":
			if enabled, ok := value.(bool); ok {
				if enabled {
					normalized[name] = map[string]any{}
				} else {
					normalized[name] = nil
				}
				changed = true
				continue
			}
		case "mentions":
			if enabled, ok := value.(bool); ok {
				normalized[name] = map[string]any{"enabled": enabled}
				changed = true
				continue
			}
		case "data":
			// data is an inline JSON schema, not a handler configuration.
			continue
		case "jobs":
			if jobs, ok := value.(map[string]any); ok {
				normalized[name] = normalizeSafeJobNeeds(jobs)
				changed = true
			}
			continue
		}
		handler, ok := value.(map[string]any)
		if !ok {
			continue
		}
		switch handler["max"].(type) {
		case int, int64, uint64, float64:
			handler = maps.Clone(handler)
			if err := preprocessIntFieldAsString(handler, "max", frontmatterTypesLog); err != nil {
				return err
			}
			normalized[name] = handler
			changed = true
		}
	}
	if changed {
		frontmatter["safe-outputs"] = normalized
	}
	return nil
}

// normalizeSafeJobNeeds adapts needs for typed JSON decoding; parseSafeJobsConfig
// separately accepts both shapes because it parses the unchanged raw map.
func normalizeSafeJobNeeds(jobs map[string]any) map[string]any {
	normalized := maps.Clone(jobs)
	for name, value := range jobs {
		job, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if needs, ok := job["needs"].(string); ok {
			job = maps.Clone(job)
			job["needs"] = []string{needs}
			normalized[name] = job
		}
	}
	return normalized
}
