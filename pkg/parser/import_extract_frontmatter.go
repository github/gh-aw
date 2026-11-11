package parser

import (
	"fmt"
	"maps"
	"strings"
)

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
	if registrations, ok := fm["connectors"].(map[string]any); ok {
		if err := validateSharedConnectorFields(fm); err != nil {
			return fmt.Errorf("import %q: %w", item.importPath, err)
		}
		acc.connectors = append(acc.connectors, ConnectorImport{Source: item.importPath, Registrations: registrations})
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
	origContent := RemoveConnectorDocumentation(string(content))
	documentationRemoved := origContent != string(content)
	origParsed, origParseErr := parseOriginalFrontmatter([]byte(origContent), item.fullPath, origContent)
	if origParseErr == nil {
		acc.collectActionPinSourceVersions([]byte(strings.Join(origParsed.FrontmatterLines, "\n")))
	}
	origFm = frontmatterMapOrEmpty(origParsed, origParseErr)
	rawContent, wasSubstituted := acc.applyImportDefaultsToContent(origContent, origFm, item.inputs)
	if parsed, err := ExtractFrontmatterFromContent(rawContent); err == nil {
		acc.collectActionPinSourceVersions([]byte(strings.Join(parsed.FrontmatterLines, "\n")))
	}
	if err := acc.collectInlineSubAgentWarnings(item.importPath, item.sectionName, rawContent, wasSubstituted, origParsed, origParseErr); err != nil {
		return nil, nil, err
	}
	toolsContent, err := acc.extractToolsContent(rawContent, item, visited, wasSubstituted || documentationRemoved)
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
func (acc *importAccumulator) collectActionPinSourceVersions(content []byte) {
	acc.addActionPinSourceVersions(ActionPinSourceVersions(content))
}
func (acc *importAccumulator) addActionPinSourceVersions(sourceVersions map[string]string) {
	for reference, label := range sourceVersions {
		if _, exists := acc.actionPinSourceVersions[reference]; !exists {
			acc.actionPinSourceVersions[reference] = label
		}
	}
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
	maps.Copy(acc.importInputs, inputsWithDefaults)
	rawContent := substituteImportInputsInContent(origContent, inputsWithDefaults)
	return rawContent, rawContent != origContent
}
func (acc *importAccumulator) collectInlineSubAgentWarnings(importPath, sectionName, rawContent string, wasSubstituted bool, origParsed *FrontmatterResult, origParseErr error) error {
	var parsed *FrontmatterResult
	var parseErr error
	if !wasSubstituted && origParseErr == nil {
		parsed = origParsed
	} else {
		parsed, parseErr = ExtractFrontmatterFromContent(rawContent)
	}
	if parseErr != nil {
		for _, w := range ValidateInlineSubAgentsFrontmatter(rawContent) {
			msg := fmt.Sprintf("import '%s': %s", importPath, w)
			acc.warnings = append(acc.warnings, msg)
			parserLog.Printf("%s", msg)
		}
		return nil
	}
	bodyForValidation := parsed.Markdown
	if sectionName != "" {
		var err error
		bodyForValidation, err = ExtractMarkdownSection(bodyForValidation, sectionName)
		if err != nil {
			return fmt.Errorf("failed to extract section '%s' from imported file '%s': %w", sectionName, importPath, err)
		}
	}
	var agentWarnings []string
	if bodyForValidation == "" && sectionName == "" {
		agentWarnings = ValidateInlineSubAgentsFrontmatter(rawContent)
	} else {
		agentWarnings = ValidateInlineSubAgentsInBody(bodyForValidation)
	}
	acc.subAgentModels = append(acc.subAgentModels, ExtractSubAgentModels(bodyForValidation)...)
	if _, agents, err := ExtractInlineSubAgents(bodyForValidation); err == nil {
		acc.subAgents = append(acc.subAgents, agents...)
	}
	for _, w := range agentWarnings {
		msg := fmt.Sprintf("import '%s': %s", importPath, w)
		acc.warnings = append(acc.warnings, msg)
		parserLog.Printf("%s", msg)
	}
	return nil
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
