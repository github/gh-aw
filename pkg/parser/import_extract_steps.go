package parser

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

func (acc *importAccumulator) extractCheckoutField(fm map[string]any, fullPath string) {
	checkoutJSON, checkoutErr := extractFieldJSONFromMap(fm, "checkout", "")
	if checkoutErr != nil || checkoutJSON == "" || checkoutJSON == "null" || checkoutJSON == "false" {
		return
	}
	acc.checkouts = append(acc.checkouts, checkoutJSON)
	parserLog.Printf("Extracted checkout from import: %s", fullPath)
}

// extractStepAndJobFields extracts step and job configuration fields from the frontmatter
// map. Environment variable conflict detection is performed: if the same env var is
// defined in two different imports, an error is returned.
//
// Side effects: acc.preStepsBuilder, acc.preAgentStepsBuilder, acc.postStepsBuilder,
// acc.jobsBuilder, acc.envBuilder, acc.envSources.
func (acc *importAccumulator) extractStepAndJobFields(fm map[string]any, importPath string) error {
	// Extract pre-steps (prepend in order).
	if preStepsContent, err := extractYAMLFieldFromMap(fm, "pre-steps"); err == nil && preStepsContent != "" {
		acc.preStepsBuilder.WriteString(preStepsContent + "\n")
	}

	// Extract jobs (append in order; merged into custom jobs map).
	if jobsContent, err := extractFieldJSONFromMap(fm, "jobs", "{}"); err == nil && jobsContent != "" && jobsContent != "{}" {
		acc.jobsBuilder.WriteString(jobsContent + "\n")
	}

	// Extract env (append in order; main workflow env takes precedence).
	// Conflicts between two imports are disallowed — only the main workflow may override imported vars.
	envContent, err := extractFieldJSONFromMap(fm, "env", "{}")
	if err == nil && envContent != "" && envContent != "{}" {
		var envMap map[string]any
		if jsonErr := json.Unmarshal([]byte(envContent), &envMap); jsonErr == nil {
			for key := range envMap {
				if existingSource, exists := acc.envSources[key]; exists {
					return fmt.Errorf("env variable %q is defined in multiple imports: %q and %q; remove the duplicate definition from one of the imports, or move it to the main workflow to override imported values", key, existingSource, importPath)
				}
				acc.envSources[key] = importPath
			}
			acc.envBuilder.WriteString(envContent + "\n")
		}
	}

	return nil
}
func (acc *importAccumulator) extractOrderedStepFields(fm map[string]any) {
	acc.appendYAMLBuilderField(fm, "steps", &acc.stepsBuilder)
	if preAgentStepsContent, err := extractYAMLFieldFromMap(fm, "pre-agent-steps"); err == nil && preAgentStepsContent != "" {
		acc.preAgentStepsBuilder.WriteString(preAgentStepsContent + "\n")
	}
	if postStepsContent, err := extractYAMLFieldFromMap(fm, "post-steps"); err == nil && postStepsContent != "" {
		acc.postStepsBuilder.WriteString(postStepsContent + "\n")
	}
}

// extractFeatureAndObservabilityFields extracts labels, cache, feature flags, model
// aliases, the run-install-scripts flag, observability configuration, and excluded-env
// from the frontmatter map.
//
// Side effects: acc.labels, acc.labelsSet, acc.caches, acc.features, acc.models,
// acc.runInstallScripts, acc.observabilityConfigs, acc.excludedEnv, acc.excludedEnvSet.
func (acc *importAccumulator) extractFeatureAndObservabilityFields(fm map[string]any, fullPath string) {
	acc.mergeLabels(fm)
	acc.appendCacheField(fm)
	acc.appendFeaturesField(fm)
	acc.appendModelsField(fm, fullPath)
	acc.extractRunInstallScripts(fm, fullPath)
	acc.appendObservabilityField(fm, fullPath)
	acc.mergeExcludedEnv(fm)
}
func (acc *importAccumulator) mergeExcludedEnv(fm map[string]any) {
	mergeJSONStringListField(fm, "excluded-env", "[]", acc.excludedEnvSet, &acc.excludedEnv, func(m map[string]any, field string) (string, error) {
		return extractFieldJSONFromMap(m, field, "[]")
	})
}
func (acc *importAccumulator) mergeLabels(fm map[string]any) {
	mergeJSONStringListField(fm, "labels", "[]", acc.labelsSet, &acc.labels, func(m map[string]any, field string) (string, error) {
		return extractFieldJSONFromMap(m, field, "[]")
	})
}
func (acc *importAccumulator) appendCacheField(fm map[string]any) {
	if cacheContent, err := extractFieldJSONFromMap(fm, "cache", "{}"); err == nil && cacheContent != "" && cacheContent != "{}" {
		acc.caches = append(acc.caches, cacheContent)
	}
}
func (acc *importAccumulator) appendFeaturesField(fm map[string]any) {
	featuresContent, err := extractFieldJSONFromMap(fm, "features", "{}")
	if err != nil || featuresContent == "" || featuresContent == "{}" {
		return
	}
	var featuresMap map[string]any
	if jsonErr := json.Unmarshal([]byte(featuresContent), &featuresMap); jsonErr == nil {
		acc.features = append(acc.features, featuresMap)
		parserLog.Printf("Extracted features from import: %d entries", len(featuresMap))
	}
}
func (acc *importAccumulator) extractPlugins(fm map[string]any) {
	values, ok := fm["plugins"].([]any)
	if !ok {
		return
	}
	for _, value := range values {
		switch typed := value.(type) {
		case string:
			if typed != "" {
				acc.plugins = append(acc.plugins, typed)
			}
		case map[string]any:
			acc.pluginObjects = append(acc.pluginObjects, typed)
		}
	}
}
func (acc *importAccumulator) extractRunInstallScripts(fm map[string]any, fullPath string) {
	if acc.runInstallScripts {
		return
	}
	if hasNodeRuntimeRunInstallScripts(fm) {
		acc.runInstallScripts = true
		parserLog.Printf("Extracted runtimes.node.run-install-scripts: true from import: %s", fullPath)
	}
}
func hasNodeRuntimeRunInstallScripts(fm map[string]any) bool {
	runtimesAny, hasRuntimes := fm["runtimes"]
	if !hasRuntimes {
		return false
	}
	runtimesMap, ok := runtimesAny.(map[string]any)
	if !ok {
		return false
	}
	nodeAny, hasNode := runtimesMap["node"]
	if !hasNode {
		return false
	}
	nodeMap, ok := nodeAny.(map[string]any)
	if !ok {
		return false
	}
	rsAny, hasRS := nodeMap["run-install-scripts"]
	if !hasRS {
		return false
	}
	rsBool, ok := rsAny.(bool)
	return ok && rsBool
}
func (acc *importAccumulator) appendObservabilityField(fm map[string]any, fullPath string) {
	obsContent, obsErr := extractFieldJSONFromMap(fm, "observability", "{}")
	if obsErr != nil || obsContent == "" || obsContent == "{}" {
		return
	}
	acc.observabilityConfigs = append(acc.observabilityConfigs, obsContent)
	parserLog.Printf("Extracted observability from import: %s", fullPath)
}
func computeImportRelPath(fullPath, importPath string) string {
	normalizedFullPath := filepath.ToSlash(fullPath)
	if idx := strings.LastIndex(normalizedFullPath, "/.github/"); idx >= 0 {
		return normalizedFullPath[idx+1:] // +1 to skip the leading slash
	}
	if strings.HasPrefix(normalizedFullPath, constants.GithubDir) {
		return normalizedFullPath
	}
	return importPath
}
