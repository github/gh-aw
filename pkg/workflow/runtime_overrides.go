package workflow

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
)

func cloneRuntimeWithActionOverrides(base *Runtime, actionRepo, actionVersion string) *Runtime {
	if base == nil {
		return nil
	}

	customRuntime := *base
	customRuntime.Commands = slices.Clone(base.Commands)
	customRuntime.ManifestFiles = slices.Clone(base.ManifestFiles)
	customRuntime.ExtraWithFields = maps.Clone(base.ExtraWithFields)

	if actionRepo != "" {
		customRuntime.ActionRepo = actionRepo
	}
	if actionVersion != "" {
		customRuntime.ActionVersion = actionVersion
	}

	return &customRuntime
}

func runtimeVersionToString(version any) (string, bool) {
	switch v := version.(type) {
	case string:
		return v, true
	case int:
		return strconv.Itoa(v), true
	case float64:
		if v == float64(int(v)) {
			return strconv.Itoa(int(v)), true
		}
		return fmt.Sprintf("%g", v), true
	default:
		return "", false
	}
}

// applyRuntimeOverrides applies runtime version overrides from frontmatter
func applyRuntimeOverrides(runtimes map[string]any, requirements map[string]*RuntimeRequirement) {
	runtimeSetupLog.Printf("Applying runtime overrides for %d configured runtimes", len(runtimes))
	for runtimeID, configAny := range runtimes {
		// Parse runtime configuration
		configMap, ok := configAny.(map[string]any)
		if !ok {
			continue
		}

		// Extract version from config
		versionAny, hasVersion := configMap["version"]
		var version string
		if hasVersion {
			var supported bool
			version, supported = runtimeVersionToString(versionAny)
			if !supported {
				continue
			}
		}

		// Extract action-repo and action-version from config
		actionRepo, _ := configMap["action-repo"].(string)
		actionVersion, _ := configMap["action-version"].(string)

		// Extract if condition from config
		ifCondition, _ := configMap["if"].(string)

		// Extract cooldown flag from config (optional)
		cooldown, hasCooldown := configMap["cooldown"].(bool)

		// Find or create runtime requirement
		if existing, exists := requirements[runtimeID]; exists {
			// Override version for existing requirement
			if hasVersion {
				runtimeSetupLog.Printf("Overriding version for runtime %s: %s", runtimeID, version)
				existing.Version = version
			}

			// Override if condition if specified
			if ifCondition != "" {
				runtimeSetupLog.Printf("Setting if condition for runtime %s: %s", runtimeID, ifCondition)
				existing.IfCondition = ifCondition
			}

			// Override cooldown setting if specified
			if hasCooldown {
				runtimeSetupLog.Printf("Setting cooldown for runtime %s: %v", runtimeID, cooldown)
				existing.Cooldown = cooldown
			}

			// If action-repo or action-version is specified, create a custom Runtime
			if actionRepo != "" || actionVersion != "" {
				runtimeSetupLog.Printf("Applying custom action config for runtime %s: repo=%s, version=%s", runtimeID, actionRepo, actionVersion)
				existing.Runtime = cloneRuntimeWithActionOverrides(existing.Runtime, actionRepo, actionVersion)
			}
		} else {
			// Check if this is a known runtime
			runtimeSetupLog.Printf("Runtime %s not in requirements, checking known runtimes", runtimeID)
			var runtime *Runtime
			for _, knownRuntime := range knownRuntimes {
				if knownRuntime.ID == runtimeID {
					// Clone the known runtime if we need to customize it
					if actionRepo != "" || actionVersion != "" {
						runtimeSetupLog.Printf("Cloning known runtime %s with custom action config: repo=%s, version=%s", runtimeID, actionRepo, actionVersion)
						runtime = cloneRuntimeWithActionOverrides(knownRuntime, actionRepo, actionVersion)
					} else {
						runtimeSetupLog.Printf("Using known runtime %s as-is", runtimeID)
						runtime = knownRuntime
					}
					break
				}
			}

			// If runtime is known or we have custom action configuration, create a new requirement
			if runtime != nil {
				runtimeSetupLog.Printf("Adding new requirement for runtime %s: version=%s", runtimeID, version)
				requirements[runtimeID] = &RuntimeRequirement{
					Runtime:     runtime,
					Version:     version,
					IfCondition: ifCondition,
					Cooldown:    true,
				}
				if hasCooldown {
					requirements[runtimeID].Cooldown = cooldown
				}
			} else {
				// If runtime is unknown and no action-repo specified, skip it (user might have typo)
				runtimeSetupLog.Printf("Skipping unknown runtime %s: not in known runtimes and no action-repo specified", runtimeID)
			}
		}
	}
}
