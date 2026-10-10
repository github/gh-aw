package parser

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
)

var includeLog = logger.New("parser:include_processor")

// processIncludesWithVisited processes import directives with cycle detection
func processIncludesWithVisited(content, baseDir string, extractTools bool, visited map[string]struct {
}) (string, error) {
	if fastResult, fastPath := fastPathForNoIncludes(content, extractTools); fastPath {
		return fastResult, nil
	}

	scanner := bufio.NewScanner(strings.NewReader(content))
	var result bytes.Buffer

	for scanner.Scan() {
		line := scanner.Text()

		// Parse import directive
		directive := ParseImportDirective(line)
		if directive != nil {
			includedContent, shouldSkip, err := processIncludeDirectiveWithVisited(directive, baseDir, extractTools, visited)
			if err != nil {
				return "", err
			}
			if shouldSkip {
				continue
			}
			if extractTools {
				result.WriteString(includedContent + "\n")
			} else {
				result.WriteString(includedContent)
			}
		} else {
			// Regular line, just pass through (unless extracting tools)
			if !extractTools {
				result.WriteString(line + "\n")
			}
		}
	}

	return result.String(), nil
}

func fastPathForNoIncludes(content string, extractTools bool) (string, bool) {
	// Fast path: skip scanner allocation when no include/import directives are present.
	// ParseImportDirective only matches lines starting with '@' or '{{#import'.
	// For content mode, preserve the scanner's trailing-newline normalization behavior.
	if !hasIncludeDirectives(content) {
		if extractTools {
			return "", true
		}
		if !strings.HasSuffix(content, "\n") {
			return content + "\n", true
		}
		return content, true
	}
	return "", false
}

type includeDirectiveResolution struct {
	filePath    string
	sectionName string
	fullPath    string
}

func processIncludeDirectiveWithVisited(
	directive *ImportDirectiveMatch,
	baseDir string,
	extractTools bool,
	visited map[string]struct {
	}) (string, bool, error) {
	emitIncludeDirectiveDeprecationWarning(directive)
	resolution, shouldSkip, err := resolveDirectiveWithVisited(directive, baseDir, extractTools, visited)
	if err != nil || shouldSkip {
		return "", shouldSkip, err
	}

	includeLog.Printf("Processing include file: %s", resolution.fullPath)
	visited[resolution.fullPath] = struct {
	}{}

	includedContent, err := processIncludedFileWithVisited(resolution.fullPath, resolution.sectionName, extractTools, visited)
	if err != nil {
		return "", false, fmt.Errorf("failed to process included file '%s': %w", resolution.fullPath, err)
	}
	return includedContent, false, nil
}

func emitIncludeDirectiveDeprecationWarning(directive *ImportDirectiveMatch) {
	if !directive.IsLegacy {
		return
	}

	optionalMarker := ""
	if directive.IsOptional {
		optionalMarker = "?"
	}

	var suggestion string
	if strings.HasPrefix(strings.TrimSpace(directive.Original), "{{") {
		suggestion = fmt.Sprintf("Use {{#runtime-import%s %s}} for content injection or the 'imports:' frontmatter field for configuration merging.",
			optionalMarker,
			directive.Path)
	} else {
		suggestion = fmt.Sprintf("Use {{#runtime-import%s %s}} instead.",
			optionalMarker,
			directive.Path)
	}
	fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Deprecated syntax: %q. %s",
		directive.Original,
		suggestion)))
}

func resolveDirectiveWithVisited(
	directive *ImportDirectiveMatch,
	baseDir string,
	extractTools bool,
	visited map[string]struct {
	}) (includeDirectiveResolution, bool, error) {
	filePath, sectionName := splitPathAndSection(directive.Path)
	fullPath, err := ResolveIncludePath(filePath, baseDir, nil)
	if err != nil {
		includeLog.Printf("Failed to resolve include path '%s': %v", filePath, err)
		if directive.IsOptional {
			if !extractTools {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Optional include file not found: %s. You can create this file to configure the workflow.", filePath)))
			}
			return includeDirectiveResolution{}, true, nil
		}
		return includeDirectiveResolution{}, false, fmt.Errorf("failed to resolve required include '%s': %w", filePath, err)
	}

	if setutil.Contains(visited, fullPath) {
		includeLog.Printf("Skipping already included file: %s", fullPath)
		if !extractTools {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Already included: %s, skipping", filePath)))
		}
		return includeDirectiveResolution{}, true, nil
	}

	return includeDirectiveResolution{
		filePath:    filePath,
		sectionName: sectionName,
		fullPath:    fullPath,
	}, false, nil
}

// processIncludedFile processes a single included file, optionally extracting a section
// processIncludedFileWithVisited processes a single included file with cycle detection for nested includes
func processIncludedFileWithVisited(filePath, sectionName string, extractTools bool, visited map[string]struct {
}) (string, error) {
	includeLog.Printf("Reading included file: %s (extractTools=%t, section=%s)", filePath, extractTools, sectionName)
	content, err := readFileFunc(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read included file %s: %w", filePath, err)
	}
	includeLog.Printf("Read %d bytes from included file: %s", len(content), filePath)

	result, validationErr, isWorkflowFile, isAgentFile, err := parseAndValidateIncludedFrontmatter(filePath, content)
	if err != nil {
		return "", err
	}

	if extractTools {
		return extractToolsFromIncludedResult(result, validationErr, isWorkflowFile, isAgentFile)
	}

	// Extract markdown content
	return extractIncludedMarkdownContent(filePath, sectionName, content, visited, extractTools)
}

func parseAndValidateIncludedFrontmatter(filePath string, content []byte) (*FrontmatterResult, error, bool, bool, error) {
	result, err := extractIncludedFrontmatter(filePath, content)
	if err != nil {
		return nil, nil, false, false, fmt.Errorf("failed to extract frontmatter from included file %s: %w", filePath, err)
	}

	isWorkflowFile := isUnderWorkflowsDirectory(filePath)
	isAgentFile := isCustomAgentFile(filePath)
	validationErr := validateIncludedFrontmatterWithFallback(filePath, result.Frontmatter, isWorkflowFile, isAgentFile)
	if validationErr != nil && isWorkflowFile {
		includeLog.Printf("Validation failed for workflow file %s: %v", filePath, validationErr)
		return nil, nil, false, false, fmt.Errorf("invalid frontmatter in included file %s: %w", filePath, validationErr)
	}

	return result, validationErr, isWorkflowFile, isAgentFile, nil
}

func extractIncludedFrontmatter(filePath string, content []byte) (*FrontmatterResult, error) {
	if strings.HasPrefix(filePath, BuiltinPathPrefix) {
		return ExtractFrontmatterFromBuiltinFile(filePath, content)
	}
	return ExtractFrontmatterFromContent(string(content))
}

func validateIncludedFrontmatterWithFallback(filePath string, frontmatter map[string]any, isWorkflowFile, isAgentFile bool) error {
	if isAgentFile || strings.HasPrefix(filePath, BuiltinPathPrefix) {
		return nil
	}
	validationErr := ValidateIncludedFileFrontmatterWithSchemaAndLocation(frontmatter, filePath)
	if validationErr == nil || isWorkflowFile {
		return validationErr
	}

	includeLog.Printf("Validation failed for non-workflow file %s, applying relaxed validation", filePath)
	applyRelaxedIncludedFrontmatterValidation(filePath, frontmatter, isAgentFile)
	return validationErr
}

func applyRelaxedIncludedFrontmatterValidation(filePath string, frontmatter map[string]any, isAgentFile bool) {
	if len(frontmatter) == 0 {
		return
	}
	unexpectedFields := collectUnexpectedIncludedFrontmatterFields(frontmatter)
	if len(unexpectedFields) > 0 {
		fmt.Fprintf(os.Stderr, "%s\n", console.FormatWarningMessage(
			fmt.Sprintf("Ignoring unexpected frontmatter fields in %s: %s",
				filePath, strings.Join(unexpectedFields, ", "))))
	}

	filteredFrontmatter := filterIncludedFrontmatterForRelaxedValidation(frontmatter, isAgentFile)
	if len(filteredFrontmatter) > 0 {
		if err := ValidateIncludedFileFrontmatterWithSchemaAndLocation(filteredFrontmatter, filePath); err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", console.FormatWarningMessage(
				fmt.Sprintf("Invalid configuration in %s: %v", filePath, err)))
		}
	}
}

func collectUnexpectedIncludedFrontmatterFields(frontmatter map[string]any) []string {
	validFields := map[string]struct {
	}{
		"tools":           {},
		"engine":          {},
		"env":             {},
		"network":         {},
		"mcp-servers":     {},
		"imports":         {},
		"name":            {},
		"description":     {},
		"steps":           {},
		"pre-steps":       {},
		"pre-agent-steps": {},
		"post-steps":      {},
		"jobs":            {},
		"safe-outputs":    {},
		"mcp-scripts":     {},
		"services":        {},
		"runtimes":        {},
		"permissions":     {},
		"secret-masking":  {},
		"inputs":          {},
		"import-schema":   {},
		"features":        {},
	}
	var unexpectedFields []string
	for key := range frontmatter {
		if !setutil.Contains(validFields, key) {
			unexpectedFields = append(unexpectedFields, key)
		}
	}
	return unexpectedFields
}

func filterIncludedFrontmatterForRelaxedValidation(frontmatter map[string]any, isAgentFile bool) map[string]any {
	hasExpressions := frontmatterContainsExpressions(frontmatter)
	filteredFrontmatter := map[string]any{}
	if !isAgentFile && !hasExpressions {
		if tools, hasTools := frontmatter["tools"]; hasTools {
			filteredFrontmatter["tools"] = tools
		}
	}
	if engine, hasEngine := frontmatter["engine"]; hasEngine {
		filteredFrontmatter["engine"] = engine
	}
	if network, hasNetwork := frontmatter["network"]; hasNetwork {
		filteredFrontmatter["network"] = network
	}
	if !hasExpressions {
		if mcpServers, hasMCPServers := frontmatter["mcp-servers"]; hasMCPServers {
			filteredFrontmatter["mcp-servers"] = mcpServers
		}
	}
	return filteredFrontmatter
}

func extractToolsFromIncludedResult(result *FrontmatterResult, validationErr error, isWorkflowFile, isAgentFile bool) (string, error) {
	if isAgentFile {
		return "{}", nil
	}
	if validationErr == nil || isWorkflowFile {
		return extractToolsFromFrontmatter(result.Frontmatter)
	}
	if tools, hasTools := result.Frontmatter["tools"]; hasTools {
		toolsJSON, err := json.Marshal(tools)
		if err != nil {
			return "{}", nil
		}
		return strings.TrimSpace(string(toolsJSON)), nil
	}
	return "{}", nil
}

func extractIncludedMarkdownContent(filePath, sectionName string, content []byte, visited map[string]struct {
}, extractTools bool) (string, error) {
	markdownContent, err := ExtractMarkdownContent(string(content))
	if err != nil {
		return "", fmt.Errorf("failed to extract markdown from %s: %w", filePath, err)
	}

	includedDir := filepath.Dir(filePath)
	markdownContent, err = processIncludesWithVisited(markdownContent, includedDir, extractTools, visited)
	if err != nil {
		return "", fmt.Errorf("failed to process nested includes in %s: %w", filePath, err)
	}
	if sectionName != "" {
		sectionContent, sectionErr := ExtractMarkdownSection(markdownContent, sectionName)
		if sectionErr != nil {
			return "", fmt.Errorf("failed to extract section '%s' from %s: %w", sectionName, filePath, sectionErr)
		}
		return strings.Trim(sectionContent, "\n") + "\n", nil
	}
	return strings.Trim(markdownContent, "\n") + "\n", nil
}

// frontmatterContainsExpressions reports whether any string value in the frontmatter map
// (recursively) contains an unsubstituted ${{ }} expression. Shared workflows that use
// import-schema parameterisation may have ${{ github.aw.import-inputs.* }} expressions in
// their frontmatter fields (e.g. tools.serena) that are only resolved at import time.
// Validation of such files is deferred to avoid false-positive schema warnings.
func frontmatterContainsExpressions(m map[string]any) bool {
	for _, v := range m {
		if frontmatterValueContainsExpression(v) {
			return true
		}
	}
	return false
}

func frontmatterValueContainsExpression(v any) bool {
	switch val := v.(type) {
	case string:
		return strings.Contains(val, "${{")
	case map[string]any:
		return frontmatterContainsExpressions(val)
	case []any:
		return slices.ContainsFunc(val, frontmatterValueContainsExpression)
	}
	return false
}
