package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/sliceutil"
)

var compilerYamlPromptLog = logger.New("workflow:compiler_yaml:prompt")

func splitContentIntoChunks(content string) []string {
	const maxChunkSize = 20900        // 21000 - 100 character buffer
	const indentSpaces = "          " // 10 spaces added to each line

	lines := strings.Split(content, "\n")
	var chunks []string
	var currentChunk []string
	currentSize := 0

	for _, line := range lines {
		lineSize := len(indentSpaces) + len(line) + 1 // +1 for newline

		// If adding this line would exceed the limit, start a new chunk
		if currentSize+lineSize > maxChunkSize && len(currentChunk) > 0 {
			chunks = append(chunks, strings.Join(currentChunk, "\n"))
			currentChunk = []string{line}
			currentSize = lineSize
		} else {
			currentChunk = append(currentChunk, line)
			currentSize += lineSize
		}
	}

	// Add the last chunk if there's content
	if len(currentChunk) > 0 {
		chunks = append(chunks, strings.Join(currentChunk, "\n"))
	}

	return chunks
}

func (c *Compiler) generatePrompt(yaml *strings.Builder, data *WorkflowData, preActivationJobCreated bool, beforeActivationJobs []string) {
	compilerYamlPromptLog.Printf("Generating prompt for workflow: %s (markdown size: %d bytes)", data.Name, len(data.MarkdownContent))

	builtinSections := c.collectPromptSections(data)
	compilerYamlPromptLog.Printf("Collected %d built-in prompt sections", len(builtinSections))

	// Process imports and enrich with main-markdown expressions, activation filters,
	// and experiment mappings.
	userPromptChunks, expressionMappings := c.processPromptImportEntries(data)
	expressionMappings = c.enrichExpressionMappings(data, expressionMappings, beforeActivationJobs)

	// Build main workflow content chunks (inline embed or runtime-import macro) and
	// collect any additional expression mappings from inlined markdown.
	userPromptChunks, expressionMappings = c.buildMainWorkflowPromptChunks(data, userPromptChunks, expressionMappings)

	// Enhance entity number expressions with || inputs.item_number fallback when the
	// workflow has a workflow_dispatch trigger with item_number.
	applyWorkflowDispatchFallbacks(expressionMappings, data.HasDispatchItemNumber)

	allExpressionMappings := c.generateUnifiedPromptCreationStep(yaml, builtinSections, userPromptChunks, expressionMappings, data)

	// Merge all known needs.* expressions for the substitution step.
	knownNeedsExpressions := generateKnownNeedsExpressions(data, preActivationJobCreated)
	if len(knownNeedsExpressions) > 0 {
		compilerYamlPromptLog.Printf("Adding %d known needs.* expressions for substitution step only", len(knownNeedsExpressions))
		allExpressionMappings = mergeKnownNeedsExpressions(allExpressionMappings, knownNeedsExpressions)
	}

	hasRuntimeImports := sliceutil.Any(userPromptChunks, func(chunk string) bool {
		return strings.Contains(chunk, "{{#runtime-import ") || strings.Contains(chunk, "{{#runtime-import? ")
	})
	c.generateInterpolationAndTemplateStep(yaml, expressionMappings, data, hasRuntimeImports)

	if len(allExpressionMappings) > 0 {
		generatePlaceholderSubstitutionStep(yaml, allExpressionMappings, "      ", data)
	}

	writePromptBashStep(yaml, "Validate prompt placeholders", "validate_prompt_placeholders.sh")
	writePromptBashStep(yaml, "Print prompt", "print_prompt_summary.sh")
}

// enrichExpressionMappings extracts expressions from the main workflow markdown, filters them
// for activation, and appends experiment expression mappings.
func (c *Compiler) enrichExpressionMappings(data *WorkflowData, expressionMappings []*ExpressionMapping, beforeActivationJobs []string) []*ExpressionMapping {
	if !c.inlinePrompt && !data.InlinedImports && data.MainWorkflowMarkdown != "" {
		compilerYamlPromptLog.Printf("Extracting expressions from main workflow markdown (%d bytes)", len(data.MainWorkflowMarkdown))
		mainExtractor := NewExpressionExtractor()
		mainExprMappings, err := mainExtractor.ExtractExpressions(data.MainWorkflowMarkdown)
		if err == nil && len(mainExprMappings) > 0 {
			compilerYamlPromptLog.Printf("Extracted %d expressions from main workflow markdown", len(mainExprMappings))
			expressionMappings = append(expressionMappings, mainExprMappings...)
		}
	}
	if runtimeImportMarkdown := c.collectRuntimeImportMarkdownForCompilerAnalysis(data); runtimeImportMarkdown != "" {
		runtimeImportExtractor := NewExpressionExtractor()
		runtimeImportExprMappings, err := runtimeImportExtractor.ExtractExpressions(runtimeImportMarkdown)
		if err == nil && len(runtimeImportExprMappings) > 0 {
			compilerYamlPromptLog.Printf("Extracted %d expressions from runtime-import markdown", len(runtimeImportExprMappings))
			expressionMappings = append(expressionMappings, runtimeImportExprMappings...)
		}
	}
	expressionMappings = filterExpressionsForActivation(expressionMappings, data.Jobs, beforeActivationJobs)
	if len(data.Experiments) > 0 {
		experimentMappings := ExperimentExpressionMappings(data.Experiments)
		compilerYamlPromptLog.Printf("Adding %d experiment expression mapping(s)", len(experimentMappings))
		expressionMappings = append(expressionMappings, experimentMappings...)
	}
	return expressionMappings
}

// buildMainWorkflowPromptChunks appends the main workflow content to userPromptChunks.
// In inline mode it embeds the markdown directly; otherwise it emits a runtime-import macro.
// Any expression mappings extracted from inline markdown are appended to expressionMappings.
func (c *Compiler) buildMainWorkflowPromptChunks(data *WorkflowData, userPromptChunks []string, expressionMappings []*ExpressionMapping) ([]string, []*ExpressionMapping) {
	if c.inlinePrompt || data.InlinedImports {
		if data.MainWorkflowMarkdown != "" {
			compilerYamlPromptLog.Printf("Inlining main workflow markdown (%d bytes)", len(data.MainWorkflowMarkdown))
			inlinedMarkdown := removeXMLComments(data.MainWorkflowMarkdown)
			inlinedMarkdown = wrapExpressionsInTemplateConditionals(inlinedMarkdown)
			inlineExtractor := NewExpressionExtractor()
			inlineExprMappings, err := inlineExtractor.ExtractExpressions(inlinedMarkdown)
			if err == nil && len(inlineExprMappings) > 0 {
				inlinedMarkdown = inlineExtractor.ReplaceExpressionsWithEnvVars(inlinedMarkdown)
				expressionMappings = append(expressionMappings, inlineExprMappings...)
			}
			inlinedChunks := splitContentIntoChunks(inlinedMarkdown)
			userPromptChunks = append(userPromptChunks, inlinedChunks...)
			compilerYamlPromptLog.Printf("Inlined main workflow markdown in %d chunks", len(inlinedChunks))
		}
		return userPromptChunks, expressionMappings
	}
	// Normal mode: use runtime-import macro so users can edit without recompilation.
	normalizedPath := filepath.ToSlash(c.markdownPath)
	var workflowFilePath string
	githubDirPattern := "/.github/"
	githubIndex := strings.LastIndex(normalizedPath, githubDirPattern)
	if githubIndex != -1 {
		workflowFilePath = normalizedPath[githubIndex+1:]
	} else if strings.HasPrefix(normalizedPath, constants.GithubDir) {
		workflowFilePath = normalizedPath
	} else {
		workflowFilePath = filepath.Base(c.markdownPath)
	}
	runtimeImportMacro := fmt.Sprintf("{{#runtime-import %s}}", workflowFilePath)
	compilerYamlPromptLog.Printf("Using runtime-import for main workflow markdown: %s", workflowFilePath)
	return append(userPromptChunks, runtimeImportMacro), expressionMappings
}

// mergeKnownNeedsExpressions merges knownNeedsExpressions into all, with all-entries taking
// precedence, and returns a deduplicated slice in sorted env-var order.
func mergeKnownNeedsExpressions(all, knownNeeds []*ExpressionMapping) []*ExpressionMapping {
	expressionMap := make(map[string]*ExpressionMapping)
	for _, mapping := range knownNeeds {
		expressionMap[mapping.EnvVar] = mapping
	}
	for _, mapping := range all {
		expressionMap[mapping.EnvVar] = mapping
	}
	result := make([]*ExpressionMapping, 0, len(expressionMap))
	for _, envVar := range sliceutil.SortedKeys(expressionMap) {
		result = append(result, expressionMap[envVar])
	}
	return result
}

// processPromptImportEntries handles the ordered PromptImports list when present, or falls back
// to the legacy grouped ImportedMarkdown / ImportPaths approach. It returns the collected prompt
// chunks and expression mappings extracted from all imported content.
//
// NEW APPROACH: Use runtime-import macros for imports without inputs
// - Imported markdown without inputs uses runtime-import macros (loaded at runtime)
// - Imported markdown with inputs is still inlined (compile-time substitution required)
// - Main workflow markdown body uses runtime-import to allow editing without recompilation
// This ensures consistency for most imports while maintaining import inputs functionality.
//
// NOTE: When an engine does not support native agent-file handling
// (GetCapabilities().NativeAgentFile == false), the agent file content is already present in the
// prompt via the standard mechanisms below — no special Step 0 is needed:
//   - Agent files WITHOUT inputs: path is in data.ImportPaths → included by Step 1b.
//   - Agent files WITH inputs: content is in data.ImportedMarkdown → included by Step 1a.
//   - inlined-imports mode: data.AgentFile is cleared; content is in data.ImportPaths.
//
// All current engines (Claude, Codex, Gemini, Copilot) use this mechanism: NativeAgentFile is false,
// and they read the fully-assembled prompt.txt in GetExecutionSteps.
func (c *Compiler) processPromptImportEntries(data *WorkflowData) (userPromptChunks []string, expressionMappings []*ExpressionMapping) {
	if len(data.PromptImports) > 0 {
		return c.processOrderedPromptImports(data)
	}
	return c.processLegacyPromptImports(data)
}

// processOrderedPromptImports handles the ordered data.PromptImports list, interleaving
// compile-time inlined markdown (imports with inputs) and runtime-import macros (imports without inputs).
func (c *Compiler) processOrderedPromptImports(data *WorkflowData) (userPromptChunks []string, expressionMappings []*ExpressionMapping) {
	compilerYamlPromptLog.Printf("Processing %d ordered prompt import entries", len(data.PromptImports))
	workspaceRoot := ""
	hasImportInputs := len(data.ImportInputs) > 0
	if data.InlinedImports && c.markdownPath != "" {
		workspaceRoot = resolveWorkspaceRoot(c.markdownPath)
	}
	for _, entry := range data.PromptImports {
		if entry.Markdown != "" {
			cleaned := removeXMLComments(entry.Markdown)
			if hasImportInputs {
				cleaned = SubstituteImportInputs(cleaned, data.ImportInputs)
			}
			chunks, exprMaps := extractPromptChunksFromMarkdown(cleaned)
			userPromptChunks = append(userPromptChunks, chunks...)
			expressionMappings = append(expressionMappings, exprMaps...)
			continue
		}
		if entry.ImportPath == "" {
			continue
		}
		importPath := filepath.ToSlash(entry.ImportPath)
		if workspaceRoot != "" {
			rawContent, err := os.ReadFile(filepath.Join(workspaceRoot, importPath))
			if err != nil {
				compilerYamlPromptLog.Printf("Warning: failed to read import file %s (%v), falling back to runtime-import", importPath, err)
				userPromptChunks = append(userPromptChunks, fmt.Sprintf("{{#runtime-import %s}}", importPath))
				continue
			}
			importedBody, extractErr := parser.ExtractMarkdownContent(string(rawContent))
			if extractErr != nil {
				importedBody = string(rawContent)
			}
			chunks, exprMaps := extractPromptChunksFromMarkdown(importedBody)
			userPromptChunks = append(userPromptChunks, chunks...)
			expressionMappings = append(expressionMappings, exprMaps...)
			continue
		}
		userPromptChunks = append(userPromptChunks, fmt.Sprintf("{{#runtime-import %s}}", importPath))
	}
	return userPromptChunks, expressionMappings
}

// processLegacyPromptImports handles the legacy grouped ImportedMarkdown / ImportPaths fields
// when data.PromptImports is empty.
func (c *Compiler) processLegacyPromptImports(data *WorkflowData) (userPromptChunks []string, expressionMappings []*ExpressionMapping) {
	// Step 1a: Process and inline imported markdown with inputs (if any).
	// Imports with inputs MUST be inlined because substitution happens at compile time.
	if data.ImportedMarkdown != "" {
		compilerYamlPromptLog.Printf("Processing imported markdown (%d bytes)", len(data.ImportedMarkdown))
		cleaned := removeXMLComments(data.ImportedMarkdown)
		if len(data.ImportInputs) > 0 {
			compilerYamlPromptLog.Printf("Substituting %d import input values", len(data.ImportInputs))
			cleaned = SubstituteImportInputs(cleaned, data.ImportInputs)
		}
		chunks, exprMaps := extractPromptChunksFromMarkdown(cleaned)
		userPromptChunks = append(userPromptChunks, chunks...)
		expressionMappings = append(expressionMappings, exprMaps...)
		compilerYamlPromptLog.Printf("Inlined imported markdown with inputs in %d chunks", len(chunks))
	}

	// Step 1b: For imports without inputs:
	// - inlinedImports mode (inlined-imports: true frontmatter): read and inline content at compile time
	// - normal mode: generate runtime-import macros (loaded at runtime)
	if len(data.ImportPaths) == 0 {
		return userPromptChunks, expressionMappings
	}
	if data.InlinedImports && c.markdownPath != "" {
		compilerYamlPromptLog.Printf("Inlining %d imports without inputs at compile time", len(data.ImportPaths))
		workspaceRoot := resolveWorkspaceRoot(c.markdownPath)
		for _, importPath := range data.ImportPaths {
			importPath = filepath.ToSlash(importPath)
			rawContent, err := os.ReadFile(filepath.Join(workspaceRoot, importPath))
			if err != nil {
				// Fall back to runtime-import macro if file cannot be read
				compilerYamlPromptLog.Printf("Warning: failed to read import file %s (%v), falling back to runtime-import", importPath, err)
				userPromptChunks = append(userPromptChunks, fmt.Sprintf("{{#runtime-import %s}}", importPath))
				continue
			}
			importedBody, extractErr := parser.ExtractMarkdownContent(string(rawContent))
			if extractErr != nil {
				importedBody = string(rawContent)
			}
			chunks, exprMaps := extractPromptChunksFromMarkdown(importedBody)
			userPromptChunks = append(userPromptChunks, chunks...)
			expressionMappings = append(expressionMappings, exprMaps...)
			compilerYamlPromptLog.Printf("Inlined import without inputs: %s", importPath)
		}
	} else {
		// Normal mode: generate runtime-import macros (loaded at workflow runtime)
		compilerYamlPromptLog.Printf("Generating runtime-import macros for %d imports without inputs", len(data.ImportPaths))
		for _, importPath := range data.ImportPaths {
			importPath = filepath.ToSlash(importPath)
			userPromptChunks = append(userPromptChunks, fmt.Sprintf("{{#runtime-import %s}}", importPath))
			compilerYamlPromptLog.Printf("Added runtime-import macro for: %s", importPath)
		}
	}
	return userPromptChunks, expressionMappings
}

// writePromptBashStep writes a YAML step that runs a bash script from the gh-aw actions directory
// with the GH_AW_PROMPT env var set.
func writePromptBashStep(yaml *strings.Builder, name, script string) {
	fmt.Fprintf(yaml, "      - name: %s\n", name)
	yaml.WriteString("        env:\n")
	fmt.Fprintf(yaml, "          GH_AW_PROMPT: %s\n", constants.AwPromptsFileExpr)
	yaml.WriteString("        run: |\n")
	fmt.Fprintf(yaml, "          bash \"${RUNNER_TEMP}/gh-aw/actions/%s\"\n", script)
}

// extractPromptChunksFromMarkdown applies the standard post-processing pipeline to a markdown body:
// XML comment removal, expression wrapping, expression extraction/substitution, and chunking.
// It returns the prompt chunks and expression mappings extracted from the content.
func extractPromptChunksFromMarkdown(body string) ([]string, []*ExpressionMapping) {
	body = removeXMLComments(body)
	body = wrapExpressionsInTemplateConditionals(body)
	extractor := NewExpressionExtractor()
	exprMappings, err := extractor.ExtractExpressions(body)
	if err == nil && len(exprMappings) > 0 {
		body = extractor.ReplaceExpressionsWithEnvVars(body)
	} else {
		exprMappings = nil
	}
	return splitContentIntoChunks(body), exprMappings
}

// resolveWorkspaceRoot returns the workspace root directory given the path to a workflow markdown
// file. ImportPaths are relative to the workspace root (e.g. ".github/workflows/shared/foo.md"),
// so the workspace root is the directory that contains ".github/".
func resolveWorkspaceRoot(markdownPath string) string {
	normalized := filepath.ToSlash(markdownPath)
	if before, _, ok := strings.Cut(normalized, "/.github/"); ok {
		// Absolute or non-root-relative path: strip everything from "/.github/" onward.
		return filepath.FromSlash(before)
	}
	if strings.HasPrefix(normalized, constants.GithubDir) {
		// Path already starts at the workspace root.
		return "."
	}
	// Fallback: use the directory containing the workflow file.
	return filepath.Dir(markdownPath)
}

func (c *Compiler) collectRuntimeImportMarkdownForCompilerAnalysis(data *WorkflowData) string {
	if data == nil || c.markdownPath == "" {
		return ""
	}
	workspaceRoot := resolveWorkspaceRoot(c.markdownPath)
	var seed strings.Builder
	seed.WriteString(data.MarkdownContent)
	if data.MainWorkflowMarkdown != "" {
		seed.WriteByte('\n')
		seed.WriteString(data.MainWorkflowMarkdown)
	}
	for _, importPath := range data.ImportPaths {
		seed.WriteString("\n{{#runtime-import ")
		seed.WriteString(filepath.ToSlash(importPath))
		seed.WriteString("}}")
	}
	for _, entry := range data.PromptImports {
		if entry.ImportPath != "" {
			seed.WriteString("\n{{#runtime-import ")
			seed.WriteString(filepath.ToSlash(entry.ImportPath))
			seed.WriteString("}}")
		}
	}
	seedContent := seed.String()
	if !strings.Contains(seedContent, "{{#runtime-import") && !strings.Contains(seedContent, "{{#import") {
		return ""
	}
	return collectRuntimeImportMarkdownContent(seedContent, workspaceRoot, map[string]struct{}{})
}

func collectRuntimeImportMarkdownContent(markdownContent, workspaceRoot string, seen map[string]struct{}) string {
	refs := extractRuntimeImportReferences(markdownContent)
	if len(refs) == 0 {
		return ""
	}

	var builder strings.Builder
	for _, ref := range refs {
		content, ok, stop := readRuntimeImportMarkdownForAnalysis(ref, workspaceRoot, seen)
		if stop {
			return builder.String()
		}
		if !ok {
			continue
		}
		builder.WriteByte('\n')
		builder.WriteString(content)
		if nested := collectRuntimeImportMarkdownContent(content, workspaceRoot, seen); nested != "" {
			builder.WriteByte('\n')
			builder.WriteString(nested)
		}
	}
	return builder.String()
}

func readRuntimeImportMarkdownForAnalysis(ref runtimeImportReference, workspaceRoot string, seen map[string]struct{}) (string, bool, bool) {
	for _, candidate := range runtimeImportAnalysisCandidatePaths(ref.importPath, workspaceRoot) {
		seenKey := fmt.Sprintf("%s:%d-%d", candidate, ref.startLine, ref.endLine)
		if _, alreadySeen := seen[seenKey]; alreadySeen {
			return "", false, false
		}
		if !runtimeImportAnalysisRealPathAllowed(candidate, workspaceRoot) {
			continue
		}
		rawContent, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}
		seen[seenKey] = struct{}{}
		content := string(rawContent)
		if ref.startLine > 0 || ref.endLine > 0 {
			content = applyRuntimeImportLineRangeForAnalysis(content, ref.startLine, ref.endLine)
		}
		importedBody, extractErr := parser.ExtractMarkdownContent(content)
		if extractErr != nil {
			importedBody = content
		}
		if err := validateExpressionSafety(importedBody); err != nil {
			return "", false, true
		}
		return importedBody, true, false
	}
	return "", false, false
}

func applyRuntimeImportLineRangeForAnalysis(content string, startLine, endLine int) string {
	lines := strings.Split(content, "\n")
	start := startLine
	if start <= 0 {
		start = 1
	}
	end := endLine
	if end <= 0 || end > len(lines) {
		end = len(lines)
	}
	if start > end || start > len(lines) {
		return ""
	}
	return strings.Join(lines[start-1:end], "\n")
}

func runtimeImportAnalysisRealPathAllowed(candidate, workspaceRoot string) bool {
	for _, base := range []string{
		filepath.Join(workspaceRoot, strings.TrimSuffix(constants.GithubDir, "/")),
		filepath.Join(workspaceRoot, ".agents"),
	} {
		if realPathWithinBase(candidate, base) {
			return true
		}
	}
	return false
}

func realPathWithinBase(pathToCheck, baseDir string) bool {
	realBase, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return false
	}
	realPath, err := filepath.EvalSymlinks(pathToCheck)
	if err != nil {
		return false
	}
	relativePath, err := filepath.Rel(realBase, realPath)
	return err == nil && relativePath != ".." && !strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) && !filepath.IsAbs(relativePath)
}

func runtimeImportAnalysisCandidatePaths(importPath, workspaceRoot string) []string {
	normalized := filepath.ToSlash(strings.TrimSpace(importPath))
	if before, _, ok := strings.Cut(normalized, ":"); ok {
		normalized = before
	}
	if strings.HasPrefix(normalized, "/") {
		normalized = strings.TrimLeft(normalized, "/")
		if !strings.HasPrefix(normalized, constants.GithubDir) && !strings.HasPrefix(normalized, ".agents/") {
			return nil
		}
	}
	normalized = strings.TrimPrefix(normalized, "./")
	if normalized == "" || strings.HasPrefix(normalized, "../") || strings.Contains(normalized, "/../") {
		return nil
	}

	githubDir := strings.TrimSuffix(constants.GithubDir, "/")
	if strings.HasPrefix(normalized, githubDir+"/") {
		return []string{filepath.Join(workspaceRoot, filepath.FromSlash(normalized))}
	}

	candidates := []string{
		filepath.Join(workspaceRoot, githubDir, filepath.FromSlash(normalized)),
		filepath.Join(workspaceRoot, githubDir, "workflows", filepath.FromSlash(normalized)),
	}
	if candidates[0] == candidates[1] {
		return candidates[:1]
	}
	return candidates
}
