package workflow

import (
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/sliceutil"
)

var orchestratorToolsLog = logger.New("workflow:compiler_orchestrator_tools")

// toolsProcessingResult holds the results of tools and markdown processing
type toolsProcessingResult struct {
	tools                 map[string]any
	resolvedMCPServers    map[string]any // fully merged mcp-servers from main workflow and all imports
	runtimes              map[string]any
	runInstallScripts     bool // true when runtimes.node.run-install-scripts: true is set (from main + imports)
	toolsTimeout          string
	toolsStartupTimeout   string
	markdownContent       string
	importedMarkdown      string   // Only imports WITH inputs (for compile-time substitution)
	importPaths           []string // Import paths for runtime-import macro generation (imports without inputs)
	promptImports         []parser.PromptImportEntry
	mainWorkflowMarkdown  string // main workflow markdown without imports (for runtime-import)
	rawMainMarkdown       string // raw main markdown before include expansion, without inline sub-agent sections
	allIncludedFiles      []string
	workflowName          string
	frontmatterName       string
	frontmatterEmoji      string
	needsTextOutput       bool
	trackerID             string
	safeOutputs           *SafeOutputsConfig
	secretMasking         *SecretMaskingConfig
	parsedFrontmatter     *FrontmatterConfig
	hasExplicitGitHubTool bool // true if tools.github was explicitly configured in frontmatter
}

// toolsAndConfigData holds the pre-markdown-expansion configuration resolved
// from frontmatter: safe outputs, secret masking, tools/mcp-servers, and
// runtimes. It is an intermediate result used by processToolsAndMarkdown.
type toolsAndConfigData struct {
	effectiveMarkdown string
	safeOutputs       *SafeOutputsConfig
	secretMasking     *SecretMaskingConfig
	toolsData         *mergedToolsData
	runtimes          map[string]any
	runInstallScripts bool
}

// resolveToolsAndConfig resolves safe outputs, secret masking, tools/mcp-servers,
// and runtimes configuration ahead of markdown include expansion.
func (c *Compiler) resolveToolsAndConfig(result *parser.FrontmatterResult, markdownDir string,
	agenticEngine CodingAgentEngine, engineSetting string, importsResult *parser.ImportsResult) (*toolsAndConfigData, error) {
	effectiveMarkdown, err := c.extractEffectiveMarkdown(importsResult, result.Markdown)
	if err != nil {
		return nil, err
	}
	c.warnDeprecatedFrontmatterFields(result.Frontmatter)
	safeOutputs := c.extractSafeOutputsConfig(result.Frontmatter)
	secretMasking, err := c.resolveSecretMasking(result.Frontmatter, importsResult)
	if err != nil {
		return nil, err
	}
	toolsData, err := c.resolveToolsConfiguration(result, effectiveMarkdown, markdownDir, importsResult, agenticEngine, engineSetting)
	if err != nil {
		return nil, err
	}
	runtimes, runInstallScripts, err := c.resolveRuntimes(result.Frontmatter, importsResult)
	if err != nil {
		return nil, err
	}
	return &toolsAndConfigData{
		effectiveMarkdown: effectiveMarkdown,
		safeOutputs:       safeOutputs,
		secretMasking:     secretMasking,
		toolsData:         toolsData,
		runtimes:          runtimes,
		runInstallScripts: runInstallScripts,
	}, nil
}

// processToolsAndMarkdown processes tools configuration, runtimes, and markdown content.
// This function handles:
// - Safe outputs and secret masking configuration
// - Tools and MCP servers merging
// - Runtimes merging
// - MCP validations
// - Markdown content expansion
// - Workflow name extraction
func (c *Compiler) processToolsAndMarkdown(result *parser.FrontmatterResult, cleanPath string, markdownDir string,
	agenticEngine CodingAgentEngine, engineSetting string, importsResult *parser.ImportsResult) (*toolsProcessingResult, error) {
	orchestratorToolsLog.Printf("Processing tools and markdown")
	workflowLog.Print("Processing tools and includes...")

	config, err := c.resolveToolsAndConfig(result, markdownDir, agenticEngine, engineSetting, importsResult)
	if err != nil {
		return nil, err
	}

	markdownData, err := c.resolveMarkdownArtifacts(config.effectiveMarkdown, markdownDir, cleanPath, result, importsResult, config.toolsData.includedToolFiles)
	if err != nil {
		return nil, err
	}
	needsTextOutput := c.logAndDetectTextOutput(markdownData.markdownContent, result.Frontmatter)
	trackerID, err := c.extractTrackerID(result.Frontmatter)
	if err != nil {
		return nil, err
	}
	parsedFrontmatter := c.tryParseFrontmatterConfig(result.Frontmatter)

	return &toolsProcessingResult{
		tools:                 config.toolsData.tools,
		resolvedMCPServers:    config.toolsData.resolvedMCPServers,
		runtimes:              config.runtimes,
		runInstallScripts:     config.runInstallScripts,
		toolsTimeout:          config.toolsData.toolsTimeout,
		toolsStartupTimeout:   config.toolsData.toolsStartupTimeout,
		markdownContent:       markdownData.markdownContent,
		importedMarkdown:      markdownData.importedMarkdown,
		importPaths:           markdownData.importPaths,
		promptImports:         markdownData.promptImports,
		mainWorkflowMarkdown:  markdownData.mainWorkflowMarkdown,
		rawMainMarkdown:       config.effectiveMarkdown,
		allIncludedFiles:      markdownData.allIncludedFiles,
		workflowName:          markdownData.workflowName,
		frontmatterName:       markdownData.frontmatterName,
		frontmatterEmoji:      markdownData.frontmatterEmoji,
		needsTextOutput:       needsTextOutput,
		trackerID:             trackerID,
		safeOutputs:           config.safeOutputs,
		secretMasking:         config.secretMasking,
		parsedFrontmatter:     parsedFrontmatter,
		hasExplicitGitHubTool: config.toolsData.hasExplicitGitHubTool,
	}, nil
}

// mergedToolsData is the consolidated tool-resolution output produced before
// markdown include expansion. It carries the effective tools map, fully merged
// mcp-servers map, include metadata, and derived timeout/tool flags.
type mergedToolsData struct {
	tools                 map[string]any
	resolvedMCPServers    map[string]any
	includedToolFiles     []string
	toolsTimeout          string
	toolsStartupTimeout   string
	hasExplicitGitHubTool bool
}

// markdownArtifacts holds markdown-derived compilation artifacts after include
// expansion. markdownContent is the combined content used for prompt generation,
// importedMarkdown contains only frontmatter imports-with-inputs prepended content,
// and mainWorkflowMarkdown is the expanded main body before importedMarkdown prepending.
type markdownArtifacts struct {
	markdownContent      string
	importedMarkdown     string
	importPaths          []string
	promptImports        []parser.PromptImportEntry
	mainWorkflowMarkdown string
	allIncludedFiles     []string
	workflowName         string
	frontmatterName      string
	frontmatterEmoji     string
}

func (c *Compiler) extractEffectiveMarkdown(importsResult *parser.ImportsResult, markdown string) (string, error) {
	effectiveMarkdown, subAgents, err := parser.ExtractInlineSubAgents(markdown)
	if err != nil {
		return "", fmt.Errorf("failed to extract inline sub-agents: %w", err)
	}
	effectiveMarkdown, inlineSkills, err := parser.ExtractInlineSkills(effectiveMarkdown)
	if err != nil {
		return "", fmt.Errorf("failed to extract inline skills: %w", err)
	}
	orchestratorToolsLog.Printf("Effective markdown after stripping sub-agent and skill sections: %d bytes", len(effectiveMarkdown))
	orchestratorToolsLog.Printf("Extracted inline sub-agents: count=%d", len(subAgents))
	orchestratorToolsLog.Printf("Extracted inline skills: count=%d", len(inlineSkills))
	for _, w := range importsResult.Warnings {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(w))
		c.IncrementWarningCount()
	}
	return effectiveMarkdown, nil
}

func (c *Compiler) resolveSecretMasking(frontmatter map[string]any, importsResult *parser.ImportsResult) (*SecretMaskingConfig, error) {
	secretMasking := c.extractSecretMaskingConfig(frontmatter)
	if importsResult.MergedSecretMasking == "" {
		return secretMasking, nil
	}
	orchestratorToolsLog.Printf("Merging secret-masking from imports")
	merged, err := c.MergeSecretMasking(secretMasking, importsResult.MergedSecretMasking)
	if err != nil {
		orchestratorToolsLog.Printf("Secret-masking merge failed: %v", err)
		return nil, fmt.Errorf("failed to merge secret-masking: %w", err)
	}
	return merged, nil
}

func (c *Compiler) resolveToolsConfiguration(
	result *parser.FrontmatterResult,
	effectiveMarkdown string,
	markdownDir string,
	importsResult *parser.ImportsResult,
	agenticEngine CodingAgentEngine,
	engineSetting string,
) (*mergedToolsData, error) {
	topTools := extractToolsMapFromFrontmatter(result.Frontmatter)
	if err := ValidateToolsSection(topTools); err != nil {
		return nil, err
	}
	includedTools, includedToolFiles, err := parser.ExpandIncludesWithManifest(effectiveMarkdown, markdownDir, true)
	if err != nil {
		orchestratorToolsLog.Printf("Failed to expand includes for tools: %v", err)
		return nil, fmt.Errorf("failed to expand includes for tools: %w", err)
	}
	allIncludedTools := strings.Join(nonEmptyStrings(importsResult.MergedTools, includedTools), "\n")
	mcpServers := extractMCPServersMapFromFrontmatter(result.Frontmatter)
	resolvedMCPServers, err := c.mergeImportedMCPServers(mcpServers, importsResult.MergedMCPServers)
	if err != nil {
		return nil, err
	}
	orchestratorToolsLog.Printf("Merging tools and MCP servers")
	tools, err := c.mergeToolsAndMCPServers(topTools, resolvedMCPServers, allIncludedTools)
	if err != nil {
		orchestratorToolsLog.Printf("Tools merge failed: %v", err)
		return nil, fmt.Errorf("failed to merge tools: %w", err)
	}
	if err := expandLinearTool(tools); err != nil {
		return nil, err
	}
	githubToolExplicit := hasExplicitGitHubTool(tools, topTools)
	toolsTimeout, toolsStartupTimeout, err := c.extractToolTimeouts(tools)
	if err != nil {
		return nil, err
	}
	c.warnDeprecatedAPMImports(result.Frontmatter)
	if err := ValidateMCPConfigs(tools); err != nil {
		orchestratorToolsLog.Printf("MCP configuration validation failed: %v", err)
		return nil, err
	}
	tools, err = enforceMCPProxyTools(agenticEngine, tools)
	if err != nil {
		return nil, err
	}
	tools = c.adjustToolsForEngineCapabilities(result.Frontmatter, agenticEngine, tools)
	tools, err = enforceMCPProxyTools(agenticEngine, tools)
	if err != nil {
		return nil, err
	}
	if err := c.validateEngineToolRequirements(result.Frontmatter, agenticEngine, tools); err != nil {
		return nil, err
	}
	return &mergedToolsData{
		tools:                 tools,
		resolvedMCPServers:    resolvedMCPServers,
		includedToolFiles:     includedToolFiles,
		toolsTimeout:          toolsTimeout,
		toolsStartupTimeout:   toolsStartupTimeout,
		hasExplicitGitHubTool: githubToolExplicit,
	}, nil
}

// enforceMCPProxyTools exposes MCP-backed tools through CLI proxies for engines
// that do not have an MCP client.
func enforceMCPProxyTools(engine MCPProxyEngine, tools map[string]any) (map[string]any, error) {
	if engine == nil || engine.GetCapabilities().MCP {
		return tools, nil
	}

	if githubValue, exists := tools["github"]; exists {
		switch github := githubValue.(type) {
		case bool:
			if !github {
				return nil, fmt.Errorf("engine '%s' does not support MCP; tools.github cannot be disabled because gh-proxy is required", engine.GetID())
			}
		case map[string]any:
			if modeValue, hasMode := github["mode"]; hasMode {
				mode, ok := modeValue.(string)
				if !ok || (mode != string(GitHubMCPModeGHProxy) && mode != string(GitHubMCPModeCLI)) {
					return nil, fmt.Errorf("engine '%s' does not support MCP; tools.github.mode must be gh-proxy", engine.GetID())
				}
			}
			github["mode"] = string(GitHubMCPModeGHProxy)
		case nil, string:
			tools["github"] = map[string]any{"mode": string(GitHubMCPModeGHProxy)}
		}
	}

	if _, exists := tools["github"]; !exists {
		tools["github"] = map[string]any{"mode": string(GitHubMCPModeGHProxy)}
	} else if enabled, ok := tools["github"].(bool); ok && enabled {
		tools["github"] = map[string]any{"mode": string(GitHubMCPModeGHProxy)}
	}

	if cliProxy, exists := tools["cli-proxy"]; exists {
		if enabled, ok := cliProxy.(bool); ok && !enabled {
			return nil, fmt.Errorf("engine '%s' does not support MCP; tools.cli-proxy cannot be disabled", engine.GetID())
		}
	}
	tools["cli-proxy"] = true
	return tools, nil
}

func nonEmptyStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func (c *Compiler) mergeImportedMCPServers(mcpServers map[string]any, mergedImportMCPServers string) (map[string]any, error) {
	if mergedImportMCPServers == "" {
		return mcpServers, nil
	}
	orchestratorToolsLog.Printf("Merging imported mcp-servers")
	mergedMCPServers, err := c.MergeMCPServers(mcpServers, mergedImportMCPServers)
	if err != nil {
		orchestratorToolsLog.Printf("MCP servers merge failed: %v", err)
		return nil, fmt.Errorf("failed to merge imported mcp-servers: %w", err)
	}
	return mergedMCPServers, nil
}

func hasExplicitGitHubTool(tools map[string]any, topTools map[string]any) bool {
	_, inMergedTools := tools["github"]
	_, inTopTools := topTools["github"]
	return inMergedTools && inTopTools
}

func (c *Compiler) extractToolTimeouts(tools map[string]any) (string, string, error) {
	toolsTimeout, err := c.extractToolsTimeout(tools)
	if err != nil {
		return "", "", fmt.Errorf("invalid tools timeout configuration: %w", err)
	}
	toolsStartupTimeout, err := c.extractToolsStartupTimeout(tools)
	if err != nil {
		return "", "", fmt.Errorf("invalid tools startup timeout configuration: %w", err)
	}
	delete(tools, "timeout")
	delete(tools, "startup-timeout")
	return toolsTimeout, toolsStartupTimeout, nil
}

func (c *Compiler) resolveRuntimes(frontmatter map[string]any, importsResult *parser.ImportsResult) (map[string]any, bool, error) {
	topRuntimes := extractRuntimesMapFromFrontmatter(frontmatter)
	orchestratorToolsLog.Printf("Merging runtimes")
	runtimes, err := mergeRuntimes(topRuntimes, importsResult.MergedRuntimes)
	if err != nil {
		orchestratorToolsLog.Printf("Runtimes merge failed: %v", err)
		return nil, false, fmt.Errorf("failed to merge runtimes: %w", err)
	}
	runInstallScripts := resolveRunInstallScripts(runtimes, importsResult.MergedRunInstallScripts)
	return runtimes, runInstallScripts, nil
}

func (c *Compiler) warnDeprecatedAPMImports(frontmatter map[string]any) {
	importsVal, hasImports := frontmatter["imports"]
	if !hasImports {
		return
	}
	importsMap, ok := importsVal.(map[string]any)
	if !ok {
		return
	}
	if _, hasAPMPackages := importsMap["apm-packages"]; hasAPMPackages {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("The 'imports.apm-packages' field is deprecated and no longer supported. Migrate to 'imports: - uses: shared/apm.md' to configure APM packages."))
		c.IncrementWarningCount()
	}
}

func (c *Compiler) adjustToolsForEngineCapabilities(frontmatter map[string]any, agenticEngine CodingAgentEngine, tools map[string]any) map[string]any {
	if agenticEngine.GetCapabilities().ToolsAllowlist {
		return tools
	}
	fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(fmt.Sprintf("Using experimental %s support (engine: %s)", agenticEngine.GetDisplayName(), agenticEngine.GetID())))
	c.IncrementWarningCount()
	if _, hasTools := frontmatter["tools"]; hasTools {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(fmt.Sprintf("'tools' section ignored when using engine: %s (%s doesn't support MCP tool allow-listing)", agenticEngine.GetID(), agenticEngine.GetDisplayName())))
		c.IncrementWarningCount()
	}
	return map[string]any{"github": map[string]any{}}
}

func (c *Compiler) validateEngineToolRequirements(frontmatter map[string]any, agenticEngine CodingAgentEngine, tools map[string]any) error {
	validators := []func() error{
		func() error { return c.validateMaxTurnsSupport(frontmatter, agenticEngine) },
		func() error { return c.validateMaxContinuationsSupport(frontmatter, agenticEngine) },
		func() error { return c.validateMaxToolDenialsSupport(frontmatter, agenticEngine) },
		func() error { return c.validateUniversalLLMConsumerModel(frontmatter, agenticEngine) },
		func() error { return c.validatePiEngineRequirements(NewTools(tools), agenticEngine) },
		func() error { return c.validateBashCommandAllowlistSupport(tools, agenticEngine) },
	}
	for _, validator := range validators {
		if err := validator(); err != nil {
			return err
		}
	}
	c.validateWebSearchSupport(tools, agenticEngine)
	c.validateBareModeSupport(frontmatter, agenticEngine)
	return nil
}

func (c *Compiler) resolveMarkdownArtifacts(
	effectiveMarkdown string,
	markdownDir string,
	cleanPath string,
	result *parser.FrontmatterResult,
	importsResult *parser.ImportsResult,
	includedToolFiles []string,
) (*markdownArtifacts, error) {
	markdownContent, includedMarkdownFiles, err := parser.ExpandIncludesWithManifest(effectiveMarkdown, markdownDir, false)
	if err != nil {
		return nil, fmt.Errorf("failed to expand includes in markdown: %w", err)
	}
	mainWorkflowMarkdown := markdownContent
	orchestratorToolsLog.Printf("Main workflow markdown: %d bytes", len(mainWorkflowMarkdown))
	importPaths := append([]string{}, importsResult.ImportPaths...)
	promptImports := append([]parser.PromptImportEntry(nil), importsResult.PromptImports...)
	if len(importPaths) > 0 {
		orchestratorToolsLog.Printf("Found %d import paths for runtime-import macros", len(importPaths))
	}
	bodyImports := parser.ExtractBodyLevelImportPaths(effectiveMarkdown, markdownDir)
	if len(bodyImports) > 0 {
		orchestratorToolsLog.Printf("Found %d body-level {{#runtime-import}} directive(s) to promote to lock-file macros", len(bodyImports))
		for _, bodyImport := range bodyImports {
			importPaths = append(importPaths, bodyImport.Path)
			promptImports = append(promptImports, parser.PromptImportEntry{ImportPath: bodyImport.Path})
		}
	}
	importedMarkdown := ""
	if importsResult.MergedMarkdown != "" {
		importedMarkdown = importsResult.MergedMarkdown
		markdownContent = importedMarkdown + markdownContent
		orchestratorToolsLog.Printf("Stored imported markdown with inputs: %d bytes, combined markdown: %d bytes", len(importedMarkdown), len(markdownContent))
	} else {
		orchestratorToolsLog.Print("No imported markdown with inputs")
	}
	workflowLog.Print("Expanded includes in markdown content")
	workflowName, err := c.extractWorkflowName(cleanPath, effectiveMarkdown)
	if err != nil {
		return nil, err
	}
	frontmatterName := extractStringFromMap(result.Frontmatter, "name", nil)
	if frontmatterName != "" {
		workflowName = frontmatterName
	}
	frontmatterEmoji := extractStringFromMap(result.Frontmatter, "emoji", nil)
	workflowLog.Printf("Extracted workflow name: '%s'", workflowName)
	return &markdownArtifacts{
		markdownContent:      markdownContent,
		importedMarkdown:     importedMarkdown,
		importPaths:          importPaths,
		promptImports:        promptImports,
		mainWorkflowMarkdown: mainWorkflowMarkdown,
		allIncludedFiles:     mergeAndSortIncludedFiles(includedToolFiles, includedMarkdownFiles),
		workflowName:         workflowName,
		frontmatterName:      frontmatterName,
		frontmatterEmoji:     frontmatterEmoji,
	}, nil
}

func mergeAndSortIncludedFiles(files1 []string, files2 []string) []string {
	allIncludedFilesMap := make(map[string]struct {
	})
	for _, file := range files1 {
		allIncludedFilesMap[file] = struct {
		}{}
	}
	for _, file := range files2 {
		allIncludedFilesMap[file] = struct {
		}{}
	}
	allIncludedFiles := sliceutil.SortedKeys(allIncludedFilesMap)
	return allIncludedFiles
}

func (c *Compiler) extractWorkflowName(cleanPath string, effectiveMarkdown string) (string, error) {
	if c.contentOverride != "" {
		workflowName, err := parser.ExtractWorkflowNameFromContent(c.contentOverride, cleanPath)
		if err != nil {
			return "", fmt.Errorf("failed to extract workflow name: %w", err)
		}
		return workflowName, nil
	}
	workflowName, err := parser.ExtractWorkflowNameFromMarkdownBody(effectiveMarkdown, cleanPath)
	if err != nil {
		return "", fmt.Errorf("failed to extract workflow name: %w", err)
	}
	return workflowName, nil
}

func (c *Compiler) logAndDetectTextOutput(markdownContent string, frontmatter map[string]any) bool {
	explicitUsage := c.detectTextOutputUsage(markdownContent)
	hasContext := c.hasContentContext(frontmatter)
	needsTextOutput := explicitUsage || hasContext
	orchestratorToolsLog.Printf("Text output needed: explicit=%v, context=%v, final=%v", explicitUsage, hasContext, needsTextOutput)
	return needsTextOutput
}

func (c *Compiler) tryParseFrontmatterConfig(frontmatter map[string]any) *FrontmatterConfig {
	parsedFrontmatter, err := ParseFrontmatterConfig(frontmatter)
	if err != nil {
		orchestratorToolsLog.Printf("Failed to parse frontmatter config: %v", err)
		return nil
	}
	return parsedFrontmatter
}

// detectTextOutputUsage checks if the markdown content uses ${{ steps.sanitized.outputs.text }},
// ${{ steps.sanitized.outputs.title }}, or ${{ steps.sanitized.outputs.body }}.
// It also recognises the deprecated ${{ needs.activation.outputs.{text,title,body} }} forms so
// that workflows that have not yet been migrated still compile correctly.
func (c *Compiler) detectTextOutputUsage(markdownContent string) bool {
	// Check for any of the text-related output expressions (modern form)
	hasTextUsage := strings.Contains(markdownContent, "${{ steps.sanitized.outputs.text }}")
	hasTitleUsage := strings.Contains(markdownContent, "${{ steps.sanitized.outputs.title }}")
	hasBodyUsage := strings.Contains(markdownContent, "${{ steps.sanitized.outputs.body }}")

	// Also recognise the deprecated needs.activation.outputs.* forms so that workflows
	// using the old syntax still get the sanitized step included during compilation.
	if !hasTextUsage {
		hasTextUsage = strings.Contains(markdownContent, "${{ needs.activation.outputs.text }}")
	}
	if !hasTitleUsage {
		hasTitleUsage = strings.Contains(markdownContent, "${{ needs.activation.outputs.title }}")
	}
	if !hasBodyUsage {
		hasBodyUsage = strings.Contains(markdownContent, "${{ needs.activation.outputs.body }}")
	}

	hasUsage := hasTextUsage || hasTitleUsage || hasBodyUsage
	detectionLog.Printf("Detected usage of sanitized outputs - text: %v, title: %v, body: %v, any: %v",
		hasTextUsage, hasTitleUsage, hasBodyUsage, hasUsage)
	return hasUsage
}

// hasContentContext checks if the workflow is triggered by events that have text content
// (issues, discussions, pull requests, or comments). These events can provide sanitized
// text/title/body outputs via the sanitized step, even if not explicitly referenced.
func (c *Compiler) hasContentContext(frontmatter map[string]any) bool {
	// Check if "on" field exists
	onField, exists := frontmatter["on"]
	if !exists || onField == nil {
		return false
	}

	// Only the map form of the "on" field contains individually-keyed event triggers.
	// String ("on: issues") and array ("on: [issues]") forms are not inspected because
	// GitHub Actions treats them as default-activity-type triggers and the original
	// implementation only detected events that appeared as YAML map keys (i.e. "event:").
	onMap, ok := onField.(map[string]any)
	if !ok {
		orchestratorToolsLog.Printf("No content context detected: 'on' is not a map")
		return false
	}

	// Content-related event types that provide text/title/body outputs via the sanitized step.
	// These are the same events supported by compute_text.cjs.
	// Note: "issues", "pull_request", and "discussion" are included here, which also covers
	// workflows using "labeled"/"unlabeled" activity types on those events — any trigger that
	// declares one of these events as a map key is treated as having content context.
	contentEventKeys := map[string]struct {
	}{
		"issues":                      {},
		"pull_request":                {},
		"pull_request_target":         {},
		"issue_comment":               {},
		"pull_request_review_comment": {},
		"pull_request_review":         {},
		"discussion":                  {},
		"discussion_comment":          {},
		"slash_command":               {},
	}

	for eventName := range onMap {
		if setutil.Contains(contentEventKeys, eventName) {
			orchestratorToolsLog.Printf("Detected content context: workflow triggered by %s", eventName)
			return true
		}
	}

	orchestratorToolsLog.Printf("No content context detected in trigger events")
	return false
}

// warnDeprecatedFrontmatterFields emits a console warning for every deprecated
// field found in the frontmatter by walking the JSON schema hierarchy.
// The schema's x-deprecation-message (falling back to description) is used as
// the warning text so deprecations self-document without per-field plumbing.
func (c *Compiler) warnDeprecatedFrontmatterFields(frontmatter map[string]any) {
	deprecatedFields, err := parser.GetMainWorkflowDeprecatedFieldsDeep()
	if err != nil {
		orchestratorToolsLog.Printf("Failed to load deprecated fields from schema: %v", err)
		return
	}

	found := parser.FindDeprecatedFieldsInFrontmatterDeep(frontmatter, deprecatedFields)
	for _, f := range found {
		msg := f.DeprecationMessage
		if msg == "" {
			msg = f.Description
		}
		if msg == "" {
			msg = fmt.Sprintf("'%s' is deprecated", f.Path)
		}
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(msg))
		c.IncrementWarningCount()
	}
}
