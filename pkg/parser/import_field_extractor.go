// Package parser provides functions for parsing and processing workflow markdown files.
// import_field_extractor.go implements field extraction from imported workflow files.
// It defines the importAccumulator struct that centralizes all result-building state
// and provides the extractAllImportFields method for processing a single imported file.
package parser

import (
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

// importAccumulator centralizes the builder/slice/set variables used during
// BFS import traversal. It accumulates results from all imported files and provides
// a method to convert the accumulated state into the final ImportsResult.
type importAccumulator struct {
	toolsBuilder               strings.Builder
	mcpServersBuilder          strings.Builder
	markdownBuilder            strings.Builder // imports with substituted inputs or schema defaults (compile-time substitution)
	importPaths                []string        // Import paths for runtime-import macro generation
	promptImports              []PromptImportEntry
	stepsBuilder               strings.Builder
	copilotSetupStepsBuilder   strings.Builder // Steps from copilot-setup-steps.yml (inserted at start)
	preStepsBuilder            strings.Builder
	preAgentStepsBuilder       strings.Builder
	runtimesBuilder            strings.Builder
	servicesBuilder            strings.Builder
	networkBuilder             strings.Builder
	permissionsBuilder         strings.Builder
	secretMaskingBuilder       strings.Builder
	postStepsBuilder           strings.Builder
	jobsBuilder                strings.Builder   // Jobs from imported YAML workflows
	envBuilder                 strings.Builder   // env vars from imported workflows (JSON, one object per line)
	envSources                 map[string]string // env var name → source import path (for conflict detection and header listing)
	observabilityConfigs       []string          // observability config JSON blobs from all imports (merged into endpoint array)
	engines                    []string
	plugins                    []string
	pluginObjects              []map[string]any
	safeOutputs                []string
	gradersBuilder             strings.Builder
	mcpScripts                 []string
	bots                       []string
	botsSet                    map[string]bool
	labels                     []string
	labelsSet                  map[string]bool
	skipRoles                  []string
	skipRolesSet               map[string]bool
	skipBots                   []string
	skipBotsSet                map[string]bool
	skipIfMatch                string
	skipIfNoMatch              string
	ambientFolders             []string
	ambientFoldersSet          map[string]bool
	sandboxAgentMounts         []string
	sandboxAgentMountsSet      map[string]bool
	sandboxAgentRuntimeInstall *bool // false if any import sets sandbox.agent.runtime-install: false
	caches                     []string
	features                   []map[string]any
	metadataDocs               string
	models                     []map[string][]string // model alias maps from each imported file (appended in import order)
	modelPolicies              []map[string][]string // model policy sets from each imported file (appended in import order)
	modelCosts                 []map[string]any      // model pricing overlays from each imported file (appended in import order)
	defaultAiCreditsPricing    map[string]any        // first models.default-ai-credits-pricing object found in imports (first-wins)
	runInstallScripts          bool                  // true if any imported workflow sets runtimes.node.run-install-scripts: true
	agentFile                  string
	agentImportSpec            string
	repositoryImports          []string
	importInputs               map[string]any
	// First on.github-token / on.github-app found across all imported files (first-wins strategy)
	activationGitHubToken string
	activationGitHubApp   string // JSON-encoded GitHubAppConfig
	// First top-level github-app found across all imported files (first-wins strategy)
	topLevelGitHubApp string // JSON-encoded GitHubAppConfig
	// Checkout configs from all imported files (append in order; main workflow's checkouts take precedence)
	checkouts []string // JSON-encoded checkout values, one per import
	// First engine.mcp.tool-timeout / engine.mcp.session-timeout found across all imported files (first-wins strategy)
	mergedEngineMCPToolTimeout    string // Go duration string (e.g. "10m", "30s")
	mergedEngineMCPSessionTimeout string // Go duration string (e.g. "4h", "30m")
	// First engine.model found in imports that have no engine.id (first-wins strategy).
	// These express a model preference without selecting a specific engine.
	mergedEngineModel string
	// First top-level max-turns / max-runs / max-ai-credits /
	// max-daily-ai-credits found across imports (first-wins).
	// Values are stored as JSON-encoded raw values so numeric literals and strings
	// round-trip consistently through import processing.
	mergedMaxTurns           string
	mergedMaxToolDenials     string
	mergedMaxRuns            string
	mergedMaxTurnCacheMisses string
	mergedMaxAICredits       string
	mergedMaxDailyAICredits  string
	mergedConcurrency        string
	mergedJobDiscriminator   string
	// Union of excluded-env lists from all imported files (deduplicated).
	excludedEnv    []string
	excludedEnvSet map[string]bool
	// Best-effort sub-agent frontmatter warnings collected during BFS traversal.
	warnings []string
}

const (
	modelPolicyAllowedKey = "allowed"
	modelPolicyBlockedKey = "blocked"
)

// newImportAccumulator creates and initializes a new importAccumulator.
// Maps (botsSet, etc.) are explicitly initialized to prevent nil map panics
// during deduplication. Slices are left as nil, which is valid for append operations.
func newImportAccumulator() *importAccumulator {
	return &importAccumulator{
		botsSet:               make(map[string]bool),
		labelsSet:             make(map[string]bool),
		skipRolesSet:          make(map[string]bool),
		skipBotsSet:           make(map[string]bool),
		ambientFoldersSet:     make(map[string]bool),
		importInputs:          make(map[string]any),
		envSources:            make(map[string]string),
		sandboxAgentMountsSet: make(map[string]bool),
		excludedEnvSet:        make(map[string]bool),
	}
}

// extractAllImportFields extracts all frontmatter fields from a single imported file
// and accumulates the results. Handles tools, engines, mcp-servers, safe-outputs,
// mcp-scripts, steps, runtimes, services, network, permissions, secret-masking, bots,
// skip-roles, skip-bots, pre-steps, pre-agent-steps, post-steps, labels, cache, and features.
// The work is delegated to focused helper methods, each handling one logical phase.
func (acc *importAccumulator) extractAllImportFields(content []byte, item importQueueItem, visited map[string]struct{}) error {
	return acc.extractImportFields(content, item, visited, true)
}

func (acc *importAccumulator) extractImportFields(content []byte, item importQueueItem, visited map[string]struct{}, includeOrderedSteps bool) error {
	parserLog.Printf("Extracting all import fields: path=%s, section=%s, inputs=%d, content_size=%d bytes", item.fullPath, item.sectionName, len(item.inputs), len(content))

	// Phase 1: Parse, apply defaults, substitute inputs, extract tools and markdown.
	origFm, fm, err := acc.prepareFrontmatter(content, item, visited)
	if err != nil {
		return err
	}

	// Phase 2: Validate 'with'/'inputs' values against the imported workflow's 'import-schema'.
	// Always use the ORIGINAL (unsubstituted) frontmatter for schema lookup so the import-schema
	// declaration itself is not affected by expression substitution.
	if _, hasSchema := origFm["import-schema"]; hasSchema {
		if err := validateWithImportSchema(item.inputs, origFm, item.importPath); err != nil {
			return err
		}
	}

	// Phase 3: Extract engine configuration (id, runtime, mcp timeouts, model preference).
	acc.extractEngineConfig(fm, item.fullPath)

	// Phase 4: Extract scalar and builder-based configuration fields.
	acc.extractConfigFields(fm, item.fullPath)

	// Phase 5: Extract activation, authentication, and access-control fields.
	acc.extractActivationFields(fm, item)

	// Phase 6: Extract step, job, and environment fields.
	if err := acc.extractStepAndJobFields(fm, item.importPath); err != nil {
		return err
	}

	// Phase 7: Extract feature flags, model aliases, run-install-scripts, and observability.
	acc.extractFeatureAndObservabilityFields(fm, item.fullPath)

	if includeOrderedSteps {
		acc.extractOrderedStepFields(fm)
	}

	return nil
}

// prepareFrontmatter handles the parse → defaults → substitution → re-parse pipeline for
// a single imported file. It parses the original content, applies import-schema defaults,
// substitutes import-inputs expressions in the raw content, extracts tools and markdown
// (handling the substituted vs. unsubstituted cases), and re-parses the possibly-modified
// frontmatter for use in subsequent field extractions.
//
// Side effects: acc.toolsBuilder, acc.markdownBuilder, acc.importPaths, acc.warnings,
// acc.importInputs.
//
// Returns: origFm (parsed from unsubstituted content, used for schema validation),
// fm (parsed from possibly-substituted content, used for all field extraction), and
// any error that should abort processing for this import.
func (acc *importAccumulator) prepareFrontmatter(content []byte, item importQueueItem, visited map[string]struct{}) (origFm, fm map[string]any, err error) {
	origContent := string(content)
	origParsed, origParseErr := parseOriginalFrontmatter(content, item.fullPath, origContent)
	origFm = frontmatterMapOrEmpty(origParsed, origParseErr)
	rawContent, wasSubstituted := acc.applyImportDefaultsToContent(origContent, origFm, item.inputs)
	acc.collectInlineSubAgentWarnings(item.importPath, rawContent, wasSubstituted, origParsed, origParseErr)
	toolsContent, err := acc.extractToolsContent(rawContent, item, visited, wasSubstituted)
	if err != nil {
		return nil, nil, err
	}
	acc.toolsBuilder.WriteString(toolsContent + "\n")
	importRelPath := computeImportRelPath(item.fullPath, item.importPath)
	if err := acc.trackRuntimeOrInlineImport(item.fullPath, importRelPath, rawContent, wasSubstituted); err != nil {
		return nil, nil, err
	}

	fm = parseFrontmatterForExtraction(rawContent, wasSubstituted, origFm)
	return origFm, fm, nil
}

func parseOriginalFrontmatter(content []byte, fullPath, origContent string) (*FrontmatterResult, error) {
	if strings.HasPrefix(fullPath, BuiltinPathPrefix) {
		return ExtractFrontmatterFromBuiltinFile(fullPath, content)
	}
	return ExtractFrontmatterFromContent(origContent)
}

func frontmatterMapOrEmpty(result *FrontmatterResult, parseErr error) map[string]any {
	if parseErr != nil {
		return make(map[string]any)
	}
	return result.Frontmatter
}

func (acc *importAccumulator) applyImportDefaultsToContent(origContent string, origFm, inputs map[string]any) (string, bool) {
	inputsWithDefaults := applyImportSchemaDefaultsFromFrontmatter(origFm, inputs)
	if len(inputsWithDefaults) == 0 {
		return origContent, false
	}
	maps.Copy(acc.importInputs, inputsWithDefaults)
	rawContent := substituteImportInputsInContent(origContent, inputsWithDefaults)
	return rawContent, rawContent != origContent
}

func (acc *importAccumulator) collectInlineSubAgentWarnings(importPath, rawContent string, wasSubstituted bool, origParsed *FrontmatterResult, origParseErr error) {
	var bodyForValidation string
	if !wasSubstituted && origParseErr == nil {
		bodyForValidation = origParsed.Markdown
	}
	agentWarnings := validateSubAgentFrontmatterWarnings(bodyForValidation, rawContent)
	for _, w := range agentWarnings {
		msg := fmt.Sprintf("import '%s': %s", importPath, w)
		acc.warnings = append(acc.warnings, msg)
		parserLog.Printf("%s", msg)
	}
}

func validateSubAgentFrontmatterWarnings(bodyForValidation, rawContent string) []string {
	if bodyForValidation != "" {
		return ValidateInlineSubAgentsInBody(bodyForValidation)
	}
	return ValidateInlineSubAgentsFrontmatter(rawContent)
}

func (acc *importAccumulator) extractToolsContent(rawContent string, item importQueueItem, visited map[string]struct{}, wasSubstituted bool) (string, error) {
	if wasSubstituted {
		toolsContent, err := extractToolsFromContent(rawContent)
		if err != nil {
			return "", fmt.Errorf("tools content in '%s' is not recognized, expected a 'Tools:' section with a valid YAML block: %w", item.fullPath, err)
		}
		return toolsContent, nil
	}
	toolsContent, err := processIncludedFileWithVisited(item.fullPath, item.sectionName, true, visited)
	if err != nil {
		return "", fmt.Errorf("imported file '%s' could not be processed, expected a readable markdown file with valid frontmatter: %w", item.fullPath, err)
	}
	return toolsContent, nil
}

func (acc *importAccumulator) trackRuntimeOrInlineImport(fullPath, importRelPath, rawContent string, wasSubstituted bool) error {
	if !wasSubstituted && !strings.HasPrefix(importRelPath, BuiltinPathPrefix) {
		acc.importPaths = append(acc.importPaths, importRelPath)
		acc.promptImports = append(acc.promptImports, PromptImportEntry{ImportPath: importRelPath})
		parserLog.Printf("Added import path for runtime-import: %s", importRelPath)
		return nil
	}
	if !wasSubstituted {
		return nil
	}
	parserLog.Printf("Import %s has substituted inputs - will be inlined for compile-time substitution", importRelPath)
	markdownContent, err := ExtractMarkdownContent(rawContent)
	if err != nil {
		return fmt.Errorf("markdown content in imported file '%s' is not recognized, expected content after the frontmatter delimiters: %w", fullPath, err)
	}
	appendMarkdownWithSeparator(&acc.markdownBuilder, markdownContent)
	acc.promptImports = append(acc.promptImports, PromptImportEntry{Markdown: markdownContent})
	return nil
}

func appendMarkdownWithSeparator(builder *strings.Builder, markdownContent string) {
	if markdownContent == "" {
		return
	}
	builder.WriteString(markdownContent)
	if strings.HasSuffix(markdownContent, "\n\n") {
		return
	}
	if strings.HasSuffix(markdownContent, "\n") {
		builder.WriteString("\n")
		return
	}
	builder.WriteString("\n\n")
}

func parseFrontmatterForExtraction(rawContent string, wasSubstituted bool, origFm map[string]any) map[string]any {
	if !wasSubstituted {
		return origFm
	}
	reparsed, err := ExtractFrontmatterFromContent(rawContent)
	if err != nil {
		return make(map[string]any)
	}
	return reparsed.Frontmatter
}

// extractEngineConfig extracts engine-related settings from the imported frontmatter map
// and accumulates them. Engine configs with only `mcp` sub-keys (no `id` or `runtime`)
// are not counted as engine specifications — they carry MCP gateway settings only.
//
// Side effects: acc.engines, acc.mergedEngineMCPToolTimeout,
// acc.mergedEngineMCPSessionTimeout, acc.mergedEngineModel.
func (acc *importAccumulator) extractEngineConfig(fm map[string]any, fullPath string) {
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
// Side effects: acc.mergedMaxTurns, acc.mergedMaxToolDenials, acc.mergedMaxRuns, acc.mergedMaxAICredits,
// acc.mergedMaxDailyAICredits, acc.mcpServersBuilder,
// acc.safeOutputs, acc.mcpScripts, acc.stepsBuilder, acc.runtimesBuilder,
// acc.servicesBuilder, acc.networkBuilder, acc.permissionsBuilder,
// acc.secretMaskingBuilder.
func (acc *importAccumulator) extractConfigFields(fm map[string]any, fullPath string) {
	acc.extractFirstWinsJSONField(fm, fullPath, "max-turns", &acc.mergedMaxTurns)
	acc.extractFirstWinsJSONField(fm, fullPath, "max-tool-denials", &acc.mergedMaxToolDenials)
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
	acc.appendJSONSliceField(fm, "mcp-scripts", "{}", &acc.mcpScripts)
	acc.appendJSONBuilderField(fm, "runtimes", "{}", &acc.runtimesBuilder)
	acc.appendYAMLBuilderField(fm, "services", &acc.servicesBuilder)
	acc.appendJSONBuilderField(fm, "network", "{}", &acc.networkBuilder)
	acc.mergeSandboxAgentMounts(fm)
	acc.mergeSandboxAgentRuntimeInstall(fm)
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

// mergeSandboxAgentRuntimeInstall extracts sandbox.agent.runtime-install from an
// imported workflow's frontmatter. False wins: if any import sets runtime-install
// to false the accumulated value becomes false and stays false.
func (acc *importAccumulator) mergeSandboxAgentRuntimeInstall(fm map[string]any) {
	// Already locked to false — no need to inspect further imports.
	if acc.sandboxAgentRuntimeInstall != nil && !*acc.sandboxAgentRuntimeInstall {
		return
	}

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
	riVal, hasRI := agentMap["runtime-install"]
	if !hasRI {
		return
	}
	ri, ok := riVal.(bool)
	if !ok {
		return
	}
	if !ri {
		f := false
		acc.sandboxAgentRuntimeInstall = &f
	} else if acc.sandboxAgentRuntimeInstall == nil {
		t := true
		acc.sandboxAgentRuntimeInstall = &t
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

// extractActivationFields extracts activation and authentication-related fields from
// the frontmatter map: bots, skip-roles, skip-bots, skip-if-match, skip-if-no-match,
// top-level ambient-folders, on.github-token, on.github-app, top-level github-app, and checkout.
//
// Side effects: acc.bots, acc.botsSet, acc.skipRoles, acc.skipRolesSet, acc.skipBots,
// acc.skipBotsSet, acc.skipIfMatch, acc.skipIfNoMatch, acc.activationGitHubToken,
// acc.activationGitHubApp, acc.topLevelGitHubApp, acc.checkouts.
func (acc *importAccumulator) extractActivationFields(fm map[string]any, item importQueueItem) {
	acc.mergeBots(fm)
	acc.mergeSkipRoles(fm)
	acc.mergeSkipBots(fm)
	acc.mergeAmbientFolders(fm)
	acc.extractActivationSkipMatchFields(fm, item.fullPath)
	acc.extractActivationGitHubToken(fm, item.fullPath)
	acc.extractActivationGitHubAppFields(fm, item.fullPath)
	acc.extractCheckoutField(fm, item.fullPath)
}

func (acc *importAccumulator) mergeBots(fm map[string]any) {
	mergeJSONStringListField(fm, "bots", "[]", acc.botsSet, &acc.bots, func(m map[string]any, field string) (string, error) {
		return extractOnSectionFieldFromMap(m, field)
	})
}

func (acc *importAccumulator) mergeSkipRoles(fm map[string]any) {
	mergeJSONStringListField(fm, "skip-roles", "[]", acc.skipRolesSet, &acc.skipRoles, func(m map[string]any, field string) (string, error) {
		return extractOnSectionFieldFromMap(m, field)
	})
}

func (acc *importAccumulator) mergeSkipBots(fm map[string]any) {
	mergeJSONStringListField(fm, "skip-bots", "[]", acc.skipBotsSet, &acc.skipBots, func(m map[string]any, field string) (string, error) {
		return extractOnSectionFieldFromMap(m, field)
	})
}

func (acc *importAccumulator) mergeAmbientFolders(fm map[string]any) {
	mergeJSONStringListField(fm, "ambient-folders", "[]", acc.ambientFoldersSet, &acc.ambientFolders, func(m map[string]any, field string) (string, error) {
		return extractFieldJSONFromMap(m, field, "[]")
	})
}

func mergeJSONStringListField(
	fm map[string]any,
	field, emptyValue string,
	seen map[string]bool,
	merged *[]string,
	extractor func(map[string]any, string) (string, error),
) {
	content, err := extractor(fm, field)
	if err != nil || content == "" || content == emptyValue {
		return
	}
	var imported []string
	if jsonErr := json.Unmarshal([]byte(content), &imported); jsonErr != nil {
		return
	}
	for _, value := range imported {
		if !seen[value] {
			seen[value] = true
			*merged = append(*merged, value)
		}
	}
}

func (acc *importAccumulator) extractActivationSkipMatchFields(fm map[string]any, fullPath string) {
	if acc.skipIfMatch == "" {
		if skipJSON, skipErr := extractOnSectionAnyFieldFromMap(fm, "skip-if-match"); skipErr == nil && skipJSON != "" && skipJSON != "null" {
			acc.skipIfMatch = skipJSON
			parserLog.Printf("Extracted on.skip-if-match from import: %s", fullPath)
		}
	}
	if acc.skipIfNoMatch == "" {
		if skipJSON, skipErr := extractOnSectionAnyFieldFromMap(fm, "skip-if-no-match"); skipErr == nil && skipJSON != "" && skipJSON != "null" {
			acc.skipIfNoMatch = skipJSON
			parserLog.Printf("Extracted on.skip-if-no-match from import: %s", fullPath)
		}
	}
}

func (acc *importAccumulator) extractActivationGitHubToken(fm map[string]any, fullPath string) {
	if acc.activationGitHubToken != "" {
		return
	}
	tokenJSON, tokenErr := extractOnSectionAnyFieldFromMap(fm, "github-token")
	if tokenErr != nil || tokenJSON == "" || tokenJSON == "null" {
		return
	}
	var token string
	if jsonErr := json.Unmarshal([]byte(tokenJSON), &token); jsonErr == nil && token != "" {
		acc.activationGitHubToken = token
		parserLog.Printf("Extracted on.github-token from import: %s", fullPath)
	}
}

func (acc *importAccumulator) extractActivationGitHubAppFields(fm map[string]any, fullPath string) {
	if acc.activationGitHubApp == "" {
		if appJSON, appErr := extractOnSectionAnyFieldFromMap(fm, "github-app"); appErr == nil {
			if validated := validateGitHubAppJSON(appJSON); validated != "" {
				acc.activationGitHubApp = validated
				parserLog.Printf("Extracted on.github-app from import: %s", fullPath)
			}
		}
	}
	if acc.topLevelGitHubApp == "" {
		if appJSON, appErr := extractFieldJSONFromMap(fm, "github-app", ""); appErr == nil {
			if validated := validateGitHubAppJSON(appJSON); validated != "" {
				acc.topLevelGitHubApp = validated
				parserLog.Printf("Extracted top-level github-app from import: %s", fullPath)
			}
		}
	}
}

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

func (acc *importAccumulator) appendModelsField(fm map[string]any, importPath string) {
	modelsContent, err := extractFieldJSONFromMap(fm, "models", "{}")
	if err != nil || modelsContent == "" || modelsContent == "{}" {
		return
	}
	var rawModels map[string]any
	if jsonErr := json.Unmarshal([]byte(modelsContent), &rawModels); jsonErr != nil {
		acc.warnings = append(acc.warnings, fmt.Sprintf("import %q: models field is not a valid object; skipping invalid value", importPath))
		return
	}
	if modelPolicy := normalizeModelPolicies(rawModels, importPath, &acc.warnings); len(modelPolicy) > 0 {
		acc.modelPolicies = append(acc.modelPolicies, modelPolicy)
		parserLog.Printf("Extracted model policy from import: allowed=%d, blocked=%d", len(modelPolicy["allowed"]), len(modelPolicy["blocked"]))
	}
	if providers, hasProviders := rawModels["providers"]; hasProviders {
		if providerMap, ok := sanitizeModelProvidersForCosts(providers, importPath, &acc.warnings); ok {
			acc.modelCosts = append(acc.modelCosts, map[string]any{"providers": providerMap})
			parserLog.Printf("Extracted model costs from import: providers=%d", len(providerMap))
		}
	}
	if acc.defaultAiCreditsPricing == nil {
		if defaultPricing, hasDefaultPricing := rawModels["default-ai-credits-pricing"]; hasDefaultPricing {
			if pricingMap, ok := defaultPricing.(map[string]any); ok {
				acc.defaultAiCreditsPricing = maps.Clone(pricingMap)
				parserLog.Printf("Extracted default-ai-credits-pricing from import: %s", importPath)
			} else {
				acc.warnings = append(acc.warnings, fmt.Sprintf("import %q: models.default-ai-credits-pricing must be an object; skipping invalid value", importPath))
			}
		}
	}

	aliasModels := make(map[string]any, len(rawModels))
	for key, value := range rawModels {
		// providers is reserved for model-cost overlays and should not be treated
		// as an alias key, even when aliases and providers coexist.
		if key == "providers" || key == "default-ai-credits-pricing" || isModelPolicyKey(key) {
			continue
		}
		aliasModels[key] = value
	}
	if len(aliasModels) == 0 {
		return
	}
	modelsMap := normalizeModelAliases(aliasModels)
	if len(modelsMap) > 0 {
		acc.models = append(acc.models, modelsMap)
		parserLog.Printf("Extracted model aliases from import: %d entries", len(modelsMap))
	}
}

func normalizeModelPolicies(rawModels map[string]any, importPath string, warnings *[]string) map[string][]string {
	parse := func(key string) []string {
		value, exists := rawModels[key]
		if !exists {
			return nil
		}
		return parseModelPolicyField(value, key, importPath, warnings)
	}
	allowed := parse(modelPolicyAllowedKey)
	blocked := parse(modelPolicyBlockedKey)
	if len(allowed) == 0 && len(blocked) == 0 {
		return nil
	}
	return map[string][]string{
		modelPolicyAllowedKey: allowed,
		modelPolicyBlockedKey: blocked,
	}
}

func normalizeModelAliases(rawModels map[string]any) map[string][]string {
	modelsMap := make(map[string][]string, len(rawModels))
	for k, v := range rawModels {
		strs := parseStringSliceField(v, true)
		if len(strs) == 0 {
			continue
		}
		modelsMap[k] = strs
	}
	return modelsMap
}

// parseModelPolicyField parses one imported models policy field as a string list.
// Invalid field shapes or entries are ignored and appended to warnings.
func parseModelPolicyField(value any, fieldName, importPath string, warnings *[]string) []string {
	values, ok := value.([]any)
	if !ok {
		*warnings = append(*warnings, fmt.Sprintf("import %q: models.%s must be an array; skipping invalid value", importPath, fieldName))
		return nil
	}
	result := make([]string, 0, len(values))
	for _, v := range values {
		s, ok := v.(string)
		if !ok {
			*warnings = append(*warnings, fmt.Sprintf("import %q: models.%s contains a non-string entry; skipping invalid entry", importPath, fieldName))
			continue
		}
		if s == "" {
			*warnings = append(*warnings, fmt.Sprintf("import %q: models.%s contains an empty string entry; skipping invalid entry", importPath, fieldName))
			continue
		}
		result = append(result, s)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// sanitizeModelProvidersForCosts validates models.providers from an import.
// It returns the provider map and true when the input is a non-empty object; otherwise false.
func sanitizeModelProvidersForCosts(providers any, importPath string, warnings *[]string) (map[string]any, bool) {
	providerMap, ok := providers.(map[string]any)
	if !ok || len(providerMap) == 0 {
		*warnings = append(*warnings, fmt.Sprintf("import %q: models.providers must be a non-empty object; skipping invalid value", importPath))
		return nil, false
	}
	sanitizedProviders := make(map[string]any, len(providerMap))
	for providerName, providerValue := range providerMap {
		if isModelPolicyKey(providerName) || providerName == "blocked" {
			*warnings = append(*warnings, fmt.Sprintf("import %q: models.providers.%s is reserved for policy and ignored in cost data", importPath, providerName))
			continue
		}
		sanitizedProviders[providerName] = providerValue
	}
	if len(sanitizedProviders) == 0 {
		*warnings = append(*warnings, fmt.Sprintf("import %q: models.providers must contain at least one non-policy provider key", importPath))
		return nil, false
	}
	return sanitizedProviders, true
}

func parseStringSliceField(value any, keepEmpty bool) []string {
	values, ok := value.([]any)
	if !ok {
		return nil
	}

	result := make([]string, 0, len(values))
	for _, v := range values {
		if s, ok := v.(string); ok {
			if s == "" && !keepEmpty {
				continue
			}
			result = append(result, s)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
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

func isModelPolicyKey(key string) bool {
	return key == modelPolicyAllowedKey || key == modelPolicyBlockedKey
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

// toImportsResult converts the accumulated state to a final ImportsResult.
// topologicalOrder is the result from topologicalSortImports.
func (acc *importAccumulator) toImportsResult(topologicalOrder []string) *ImportsResult {
	parserLog.Printf("Building ImportsResult: importedFiles=%d, importPaths=%d, engines=%d, bots=%d, labels=%d",
		len(topologicalOrder), len(acc.importPaths), len(acc.engines), len(acc.bots), len(acc.labels))
	result := acc.buildImportsResult()
	result.ImportedFiles = topologicalOrder
	return result
}

// buildImportsResult constructs the ImportsResult from accumulated state, excluding
// ImportedFiles which is populated separately from the topological sort order.
func (acc *importAccumulator) buildImportsResult() *ImportsResult {
	result := &ImportsResult{
		MergedTools:                      acc.toolsBuilder.String(),
		MergedMCPServers:                 acc.mcpServersBuilder.String(),
		MergedEngines:                    acc.engines,
		MergedPlugins:                    acc.plugins,
		MergedPluginObjects:              acc.pluginObjects,
		MergedSafeOutputs:                acc.safeOutputs,
		MergedGraders:                    acc.gradersBuilder.String(),
		MergedMCPScripts:                 acc.mcpScripts,
		MergedMarkdown:                   acc.markdownBuilder.String(),
		ImportPaths:                      acc.importPaths,
		PromptImports:                    acc.promptImports,
		MergedSteps:                      acc.stepsBuilder.String(),
		CopilotSetupSteps:                acc.copilotSetupStepsBuilder.String(),
		MergedPreSteps:                   acc.preStepsBuilder.String(),
		MergedPreAgentSteps:              acc.preAgentStepsBuilder.String(),
		MergedRuntimes:                   acc.runtimesBuilder.String(),
		MergedRunInstallScripts:          acc.runInstallScripts,
		MergedServices:                   acc.servicesBuilder.String(),
		MergedNetwork:                    acc.networkBuilder.String(),
		MergedSandboxAgentMounts:         acc.sandboxAgentMounts,
		MergedSandboxAgentRuntimeInstall: acc.sandboxAgentRuntimeInstall,
		MergedPermissions:                acc.permissionsBuilder.String(),
		MergedSecretMasking:              acc.secretMaskingBuilder.String(),
		MergedBots:                       acc.bots,
		MergedSkipRoles:                  acc.skipRoles,
		MergedSkipBots:                   acc.skipBots,
		MergedSkipIfMatch:                acc.skipIfMatch,
		MergedSkipIfNoMatch:              acc.skipIfNoMatch,
		MergedAmbientFolders:             acc.ambientFolders,
		MergedPostSteps:                  acc.postStepsBuilder.String(),
		MergedLabels:                     acc.labels,
		MergedCaches:                     acc.caches,
		MergedJobs:                       acc.jobsBuilder.String(),
		MergedEnv:                        acc.envBuilder.String(),
		MergedEnvSources:                 acc.envSources,
		MergedFeatures:                   acc.features,
		MergedMetadataDocs:               acc.metadataDocs,
		MergedModels:                     acc.models,
		MergedModelPolicies:              acc.modelPolicies,
		MergedModelCosts:                 acc.modelCosts,
		MergedDefaultAiCreditsPricing:    acc.defaultAiCreditsPricing,
		MergedObservability:              mergeObservabilityConfigs(acc.observabilityConfigs),
		AgentFile:                        acc.agentFile,
		AgentImportSpec:                  acc.agentImportSpec,
		RepositoryImports:                acc.repositoryImports,
		ImportInputs:                     acc.importInputs,
		Warnings:                         acc.warnings,
	}
	acc.populateImportsResultScalars(result)
	return result
}

func (acc *importAccumulator) populateImportsResultScalars(result *ImportsResult) {
	result.MergedActivationGitHubToken = acc.activationGitHubToken
	result.MergedActivationGitHubApp = acc.activationGitHubApp
	result.MergedTopLevelGitHubApp = acc.topLevelGitHubApp
	result.MergedCheckout = strings.Join(acc.checkouts, "\n")
	result.MergedEngineMCPToolTimeout = acc.mergedEngineMCPToolTimeout
	result.MergedEngineMCPSessionTimeout = acc.mergedEngineMCPSessionTimeout
	result.MergedEngineModel = acc.mergedEngineModel
	result.MergedMaxTurns = acc.mergedMaxTurns
	result.MergedMaxToolDenials = acc.mergedMaxToolDenials
	result.MergedMaxRuns = acc.mergedMaxRuns
	result.MergedMaxTurnCacheMisses = acc.mergedMaxTurnCacheMisses
	result.MergedMaxAICredits = acc.mergedMaxAICredits
	result.MergedMaxDailyAICredits = acc.mergedMaxDailyAICredits
	result.MergedConcurrency = acc.mergedConcurrency
	result.MergedJobDiscriminator = acc.mergedJobDiscriminator
	result.MergedExcludedEnv = acc.excludedEnv
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

// validateGitHubAppJSON validates that a JSON-encoded GitHub App configuration has the required
// string fields ((client-id or app-id) and private-key). Returns the input JSON if valid, or "" otherwise.
func validateGitHubAppJSON(appJSON string) string {
	if appJSON == "" || appJSON == "null" {
		return ""
	}
	var appMap map[string]any
	if err := json.Unmarshal([]byte(appJSON), &appMap); err != nil {
		return ""
	}
	_, hasClientID := appMap["client-id"].(string)
	_, hasAppID := appMap["app-id"].(string)
	if !hasClientID && !hasAppID {
		return ""
	}
	if _, hasKey := appMap["private-key"].(string); !hasKey {
		return ""
	}
	return appJSON
}
