package workflow

import (
	"encoding/json"
	"sort"
)

// Frontmatter extraction helpers for workflow builder.

func extractEnclavesConfig(frontmatter map[string]any) EnclavesConfig {
	raw, ok := frontmatter["enclaves"]
	if !ok {
		return nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return EnclavesConfig{nil}
	}
	var enclaves EnclavesConfig
	if err := json.Unmarshal(data, &enclaves); err != nil {
		return EnclavesConfig{nil}
	}
	return enclaves
}

func extractLSPConfig(parsedFrontmatter *FrontmatterConfig, frontmatter map[string]any) map[string]LSPServerConfig {
	if parsedFrontmatter != nil && len(parsedFrontmatter.LSP) > 0 {
		return parsedFrontmatter.LSP
	}

	rawLSP, ok := frontmatter["lsp"]
	if !ok {
		return nil
	}

	jsonBytes, err := json.Marshal(rawLSP)
	if err != nil {
		workflowBuilderLog.Printf("Failed to marshal lsp frontmatter config: %v", err)
		return nil
	}

	var lsp map[string]LSPServerConfig
	if err := json.Unmarshal(jsonBytes, &lsp); err != nil {
		workflowBuilderLog.Printf("Failed to unmarshal lsp frontmatter config: %v", err)
		return nil
	}

	if len(lsp) == 0 {
		return nil
	}
	return lsp
}

func extractFrontmatterSkills(parsedFrontmatter *FrontmatterConfig, frontmatter map[string]any) []string {
	refs := extractFrontmatterSkillReferences(parsedFrontmatter, frontmatter)
	if len(refs) == 0 {
		return nil
	}

	skills := make([]string, 0, len(refs))
	for _, ref := range refs {
		if ref.Skill == "" {
			continue
		}
		skills = append(skills, ref.Skill)
	}
	if len(skills) == 0 {
		return nil
	}
	return skills
}

func mergeFrontmatterPlugins(parsedFrontmatter *FrontmatterConfig, frontmatter map[string]any, importedPlugins []string, importedPluginObjects []map[string]any) []string {
	return pluginRefsToStrings(mergeFrontmatterPluginReferences(parsedFrontmatter, frontmatter, importedPlugins, importedPluginObjects))
}

func extractFrontmatterPluginReferences(parsedFrontmatter *FrontmatterConfig, frontmatter map[string]any) []PluginReference {
	if parsedFrontmatter != nil && len(parsedFrontmatter.PluginReferences) > 0 {
		return append([]PluginReference(nil), parsedFrontmatter.PluginReferences...)
	}

	// Fall back to raw frontmatter when ParseFrontmatterConfig failed for non-plugins reasons
	// (e.g. unrecognized tool shapes). Safe because validateFrontmatterPlugins already ran
	// and succeeded on this frontmatter before we reach this point.
	rawPlugins, ok := frontmatter["plugins"].([]any)
	if !ok || len(rawPlugins) == 0 {
		return nil
	}

	return parseRawPluginReferences(rawPlugins)
}

// mergeFrontmatterPluginReferences merges the main workflow's structured plugin references
// with plugin specs imported from shared workflows.
func mergeFrontmatterPluginReferences(parsedFrontmatter *FrontmatterConfig, frontmatter map[string]any, importedPlugins []string, importedPluginObjects []map[string]any) []PluginReference {
	refs := extractFrontmatterPluginReferences(parsedFrontmatter, frontmatter)
	for _, imported := range importedPlugins {
		refs = append(refs, PluginReference{Plugin: imported})
	}
	for _, imported := range importedPluginObjects {
		refs = append(refs, parseRawPluginReferences([]any{imported})...)
	}
	return refs
}

func extractFrontmatterSkillReferences(parsedFrontmatter *FrontmatterConfig, frontmatter map[string]any) []SkillReference {
	if parsedFrontmatter != nil && len(parsedFrontmatter.SkillReferences) > 0 {
		return append([]SkillReference(nil), parsedFrontmatter.SkillReferences...)
	}

	// Fall back to raw frontmatter when ParseFrontmatterConfig failed for non-skills reasons
	// (e.g. unrecognized tool shapes). Safe because validateFrontmatterSkills already ran
	// and succeeded on this frontmatter before we reach this point.
	rawSkills, ok := frontmatter["skills"].([]any)
	if !ok || len(rawSkills) == 0 {
		return nil
	}

	return parseRawSkillReferences(rawSkills)
}

func resolveInlinedImports(rawFrontmatter map[string]any) bool {
	return ParseBoolFromConfig(rawFrontmatter, "inlined-imports", nil)
}

// mergeExcludedEnvVarNames unions the imported and main excluded-env name lists,
// deduplicates entries across both sources, and returns a sorted slice for
// deterministic output.
func mergeExcludedEnvVarNames(fromImports, fromMain []string) []string {
	if len(fromImports) == 0 && len(fromMain) == 0 {
		return nil
	}
	// Use max() for capacity hints: overflow-safe (no addition) and a tighter
	// lower-bound than either length alone.
	hint := max(len(fromImports), len(fromMain))
	seen := make(map[string]struct{}, hint)
	merged := make([]string, 0, hint)
	for _, name := range fromImports {
		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
			merged = append(merged, name)
		}
	}
	for _, name := range fromMain {
		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
			merged = append(merged, name)
		}
	}
	sort.Strings(merged)
	return merged
}

// extractYAMLSections extracts YAML configuration sections from frontmatter
func (c *Compiler) extractYAMLSections(frontmatter map[string]any, workflowData *WorkflowData) error {
	workflowBuilderLog.Print("Extracting YAML sections from frontmatter")

	workflowData.On = c.extractTopLevelYAMLSection(frontmatter, "on")
	workflowData.HasDispatchItemNumber = extractDispatchItemNumber(frontmatter)
	workflowData.Permissions = c.extractPermissions(frontmatter)
	workflowData.Network = c.extractTopLevelYAMLSection(frontmatter, "network")
	workflowData.ConcurrencyJobDiscriminator = extractConcurrencyJobDiscriminator(frontmatter)
	workflowData.Concurrency = c.extractConcurrencySection(frontmatter)
	workflowData.RunName = c.extractTopLevelYAMLSection(frontmatter, "run-name")
	workflowData.Env = c.extractTopLevelYAMLSection(frontmatter, "env")
	workflowData.Features = c.extractFeatures(frontmatter)

	ifCondition, err := c.extractIfCondition(frontmatter)
	if err != nil {
		return err
	}
	workflowData.If = ifCondition

	// Extract timeout-minutes (canonical form)
	workflowData.TimeoutMinutes = c.extractTopLevelYAMLSection(frontmatter, "timeout-minutes")

	workflowData.RunsOn = c.extractTopLevelYAMLSection(frontmatter, "runs-on")
	if v, ok := frontmatter["runs-on-slim"]; ok && !isEmptyRunsOnValue(v) {
		workflowData.RunsOnSlim = c.extractTopLevelYAMLSection(map[string]any{"runs-on": v}, "runs-on")
	}
	workflowData.Environment = c.extractTopLevelYAMLSection(frontmatter, "environment")
	workflowData.Container = c.extractTopLevelYAMLSection(frontmatter, "container")
	workflowData.Cache = c.extractTopLevelYAMLSection(frontmatter, "cache")
	return nil
}

// extractConcurrencyJobDiscriminator reads the job-discriminator value from the
// frontmatter concurrency block without modifying the original map.
// Returns the discriminator expression string or empty string if not present.
func extractConcurrencyJobDiscriminator(frontmatter map[string]any) string {
	concurrencyRaw, ok := frontmatter["concurrency"]
	if !ok {
		return ""
	}
	concurrencyMap, ok := concurrencyRaw.(map[string]any)
	if !ok {
		return ""
	}
	discriminator, ok := concurrencyMap["job-discriminator"]
	if !ok {
		return ""
	}
	discriminatorStr, ok := discriminator.(string)
	if !ok {
		return ""
	}
	return discriminatorStr
}

// extractConcurrencySection extracts the workflow-level concurrency YAML section,
// stripping the gh-aw-specific job-discriminator field so it does not appear in
// the compiled lock file (which must be valid GitHub Actions YAML).
func (c *Compiler) extractConcurrencySection(frontmatter map[string]any) string {
	concurrencyRaw, ok := frontmatter["concurrency"]
	if !ok {
		return ""
	}
	concurrencyMap, ok := concurrencyRaw.(map[string]any)
	if !ok || len(concurrencyMap) == 0 {
		// String or empty format: serialize as-is (no job-discriminator possible)
		return c.extractTopLevelYAMLSection(frontmatter, "concurrency")
	}

	_, hasDiscriminator := concurrencyMap["job-discriminator"]
	if !hasDiscriminator {
		return c.extractTopLevelYAMLSection(frontmatter, "concurrency")
	}

	// Build a copy of the concurrency map without job-discriminator for serialization.
	// Use len(concurrencyMap) for capacity: at most one entry (job-discriminator) will be
	// omitted, so this is a slight over-allocation that avoids a subtle negative-capacity
	// edge case if job-discriminator were the only key.
	cleanMap := make(map[string]any, len(concurrencyMap))
	for k, v := range concurrencyMap {
		if k != "job-discriminator" {
			cleanMap[k] = v
		}
	}
	// When job-discriminator is the only field, there is no user-specified workflow-level
	// group to emit; return empty so the compiler can generate the default concurrency.
	if len(cleanMap) == 0 {
		return ""
	}
	// Use a minimal temporary frontmatter containing only the concurrency key to avoid
	// copying the entire (potentially large) frontmatter map.
	return c.extractTopLevelYAMLSection(map[string]any{"concurrency": cleanMap}, "concurrency")
}

// extractDispatchItemNumber reports whether the frontmatter's on.workflow_dispatch
// trigger exposes an item_number input. This is the signature produced by the label
// trigger shorthand (e.g. "on: pull_request labeled my-label"). Reading the
// structured map avoids re-parsing the rendered YAML string later.
func extractDispatchItemNumber(frontmatter map[string]any) bool {
	onVal, ok := frontmatter["on"]
	if !ok {
		return false
	}
	onMap, ok := onVal.(map[string]any)
	if !ok {
		return false
	}
	wdVal, ok := onMap["workflow_dispatch"]
	if !ok {
		return false
	}
	wdMap, ok := wdVal.(map[string]any)
	if !ok {
		return false
	}
	inputsVal, ok := wdMap["inputs"]
	if !ok {
		return false
	}
	inputsMap, ok := inputsVal.(map[string]any)
	if !ok {
		return false
	}
	_, ok = inputsMap["item_number"]
	return ok
}
