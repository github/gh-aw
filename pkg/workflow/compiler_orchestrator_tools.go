package workflow

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/githubnext/gh-aw/pkg/console"
	"github.com/githubnext/gh-aw/pkg/logger"
	"github.com/githubnext/gh-aw/pkg/parser"
)

var orchestratorToolsLog = logger.New("workflow:compiler_orchestrator_tools")

// toolsProcessingResult holds the results of tools and markdown processing
type toolsProcessingResult struct {
	tools                map[string]any
	runtimes             map[string]any
	toolsTimeout         int
	toolsStartupTimeout  int
	markdownContent      string
	importedMarkdown     string // imported markdown from frontmatter imports (separate from main body)
	mainWorkflowMarkdown string // main workflow markdown without imports (for runtime-import)
	allIncludedFiles     []string
	workflowName         string
	frontmatterName      string
	needsTextOutput      bool
	trackerID            string
	safeOutputs          *SafeOutputsConfig
	secretMasking        *SecretMaskingConfig
	parsedFrontmatter    *FrontmatterConfig
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
	log.Print("Processing tools and includes...")

	// Extract SafeOutputs configuration early so we can use it when applying default tools
	safeOutputs := c.extractSafeOutputsConfig(result.Frontmatter)

	// Check for project field and auto-configure safe-outputs for project tracking
	safeOutputs = c.applyProjectSafeOutputs(result.Frontmatter, safeOutputs)

	// Extract SecretMasking configuration
	secretMasking := c.extractSecretMaskingConfig(result.Frontmatter)

	// Merge secret-masking from imports with top-level secret-masking
	if importsResult.MergedSecretMasking != "" {
		orchestratorToolsLog.Printf("Merging secret-masking from imports")
		var err error
		secretMasking, err = c.MergeSecretMasking(secretMasking, importsResult.MergedSecretMasking)
		if err != nil {
			orchestratorToolsLog.Printf("Secret-masking merge failed: %v", err)
			return nil, fmt.Errorf("failed to merge secret-masking: %w", err)
		}
	}

	var tools map[string]any

	// Extract tools from the main file
	topTools := extractToolsFromFrontmatter(result.Frontmatter)

	// Extract mcp-servers from the main file and merge them into tools
	mcpServers := extractMCPServersFromFrontmatter(result.Frontmatter)

	// Process @include directives to extract additional tools
	orchestratorToolsLog.Printf("Expanding includes for tools")
	includedTools, includedToolFiles, err := parser.ExpandIncludesWithManifest(result.Markdown, markdownDir, true)
	if err != nil {
		orchestratorToolsLog.Printf("Failed to expand includes for tools: %v", err)
		return nil, fmt.Errorf("failed to expand includes for tools: %w", err)
	}

	// Combine imported tools with included tools
	var allIncludedTools string
	if importsResult.MergedTools != "" && includedTools != "" {
		allIncludedTools = importsResult.MergedTools + "\n" + includedTools
	} else if importsResult.MergedTools != "" {
		allIncludedTools = importsResult.MergedTools
	} else {
		allIncludedTools = includedTools
	}

	// Combine imported mcp-servers with top-level mcp-servers
	// Imported mcp-servers are in JSON format (newline-separated), need to merge them
	allMCPServers := mcpServers
	if importsResult.MergedMCPServers != "" {
		orchestratorToolsLog.Printf("Merging imported mcp-servers")
		// Parse and merge imported MCP servers
		mergedMCPServers, err := c.MergeMCPServers(mcpServers, importsResult.MergedMCPServers)
		if err != nil {
			orchestratorToolsLog.Printf("MCP servers merge failed: %v", err)
			return nil, fmt.Errorf("failed to merge imported mcp-servers: %w", err)
		}
		allMCPServers = mergedMCPServers
	}

	// Merge tools including mcp-servers
	orchestratorToolsLog.Printf("Merging tools and MCP servers")
	tools, err = c.mergeToolsAndMCPServers(topTools, allMCPServers, allIncludedTools)
	if err != nil {
		orchestratorToolsLog.Printf("Tools merge failed: %v", err)
		return nil, fmt.Errorf("failed to merge tools: %w", err)
	}

	// Extract and validate tools timeout settings
	toolsTimeout, err := c.extractToolsTimeout(tools)
	if err != nil {
		return nil, fmt.Errorf("invalid tools timeout configuration: %w", err)
	}

	toolsStartupTimeout, err := c.extractToolsStartupTimeout(tools)
	if err != nil {
		return nil, fmt.Errorf("invalid tools startup timeout configuration: %w", err)
	}

	// Remove meta fields (timeout, startup-timeout) from merged tools map
	// These are configuration fields, not actual tools
	delete(tools, "timeout")
	delete(tools, "startup-timeout")

	// Extract and merge runtimes from frontmatter and imports
	topRuntimes := extractRuntimesFromFrontmatter(result.Frontmatter)
	orchestratorToolsLog.Printf("Merging runtimes")
	runtimes, err := mergeRuntimes(topRuntimes, importsResult.MergedRuntimes)
	if err != nil {
		orchestratorToolsLog.Printf("Runtimes merge failed: %v", err)
		return nil, fmt.Errorf("failed to merge runtimes: %w", err)
	}

	// Add MCP fetch server if needed (when web-fetch is requested but engine doesn't support it)
	tools, _ = AddMCPFetchServerIfNeeded(tools, agenticEngine)

	// Validate MCP configurations
	orchestratorToolsLog.Printf("Validating MCP configurations")
	if err := ValidateMCPConfigs(tools); err != nil {
		orchestratorToolsLog.Printf("MCP configuration validation failed: %v", err)
		return nil, err
	}

	// Validate HTTP transport support for the current engine
	if err := c.validateHTTPTransportSupport(tools, agenticEngine); err != nil {
		orchestratorToolsLog.Printf("HTTP transport validation failed: %v", err)
		return nil, err
	}

	if !agenticEngine.SupportsToolsAllowlist() {
		// For engines that don't support tool allowlists (like custom engine), ignore tools section and provide warnings
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Using experimental %s support (engine: %s)", agenticEngine.GetDisplayName(), agenticEngine.GetID())))
		c.IncrementWarningCount()
		if _, hasTools := result.Frontmatter["tools"]; hasTools {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("'tools' section ignored when using engine: %s (%s doesn't support MCP tool allow-listing)", agenticEngine.GetID(), agenticEngine.GetDisplayName())))
			c.IncrementWarningCount()
		}
		tools = map[string]any{}
		// For now, we'll add a basic github tool (always uses docker MCP)
		githubConfig := map[string]any{}
		tools["github"] = githubConfig
	}

	// Validate max-turns support for the current engine
	if err := c.validateMaxTurnsSupport(result.Frontmatter, agenticEngine); err != nil {
		return nil, err
	}

	// Validate web-search support for the current engine (warning only)
	c.validateWebSearchSupport(tools, agenticEngine)

	// Process @include directives in markdown content
	markdownContent, includedMarkdownFiles, err := parser.ExpandIncludesWithManifest(result.Markdown, markdownDir, false)
	if err != nil {
		return nil, fmt.Errorf("failed to expand includes in markdown: %w", err)
	}

	// Store the main workflow markdown (before prepending imports)
	mainWorkflowMarkdown := markdownContent
	orchestratorToolsLog.Printf("Main workflow markdown: %d bytes", len(mainWorkflowMarkdown))

	// Prepend imported markdown from frontmatter imports field
	var importedMarkdown string
	if importsResult.MergedMarkdown != "" {
		importedMarkdown = importsResult.MergedMarkdown
		markdownContent = importsResult.MergedMarkdown + markdownContent
		orchestratorToolsLog.Printf("Stored imported markdown: %d bytes, combined markdown: %d bytes", len(importedMarkdown), len(markdownContent))
	} else {
		orchestratorToolsLog.Print("No imported markdown")
	}

	log.Print("Expanded includes in markdown content")

	// Combine all included files (from tools and markdown)
	// Use a map to deduplicate files
	allIncludedFilesMap := make(map[string]bool)
	for _, file := range includedToolFiles {
		allIncludedFilesMap[file] = true
	}
	for _, file := range includedMarkdownFiles {
		allIncludedFilesMap[file] = true
	}
	var allIncludedFiles []string
	for file := range allIncludedFilesMap {
		allIncludedFiles = append(allIncludedFiles, file)
	}
	// Sort files alphabetically to ensure consistent ordering in lock files
	sort.Strings(allIncludedFiles)

	// Extract workflow name
	workflowName, err := parser.ExtractWorkflowNameFromMarkdown(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("failed to extract workflow name: %w", err)
	}

	// Check if frontmatter specifies a custom name and use it instead
	frontmatterName := extractStringFromMap(result.Frontmatter, "name", nil)
	if frontmatterName != "" {
		workflowName = frontmatterName
	}

	log.Printf("Extracted workflow name: '%s'", workflowName)

	// Check if the markdown content uses the text output
	needsTextOutput := c.detectTextOutputUsage(markdownContent)

	// Extract and validate tracker-id
	trackerID, err := c.extractTrackerID(result.Frontmatter)
	if err != nil {
		return nil, err
	}

	// Parse frontmatter config once for performance optimization
	parsedFrontmatter, err := ParseFrontmatterConfig(result.Frontmatter)
	if err != nil {
		orchestratorToolsLog.Printf("Failed to parse frontmatter config: %v", err)
		// Non-fatal error - continue with nil ParsedFrontmatter
		parsedFrontmatter = nil
	}

	return &toolsProcessingResult{
		tools:                tools,
		runtimes:             runtimes,
		toolsTimeout:         toolsTimeout,
		toolsStartupTimeout:  toolsStartupTimeout,
		markdownContent:      markdownContent,
		importedMarkdown:     importedMarkdown,
		mainWorkflowMarkdown: mainWorkflowMarkdown,
		allIncludedFiles:     allIncludedFiles,
		workflowName:         workflowName,
		frontmatterName:      frontmatterName,
		needsTextOutput:      needsTextOutput,
		trackerID:            trackerID,
		safeOutputs:          safeOutputs,
		secretMasking:        secretMasking,
		parsedFrontmatter:    parsedFrontmatter,
	}, nil
}

// detectTextOutputUsage checks if the markdown content uses ${{ needs.activation.outputs.text }}
func (c *Compiler) detectTextOutputUsage(markdownContent string) bool {
	// Check for the specific GitHub Actions expression
	hasUsage := strings.Contains(markdownContent, "${{ needs.activation.outputs.text }}")
	detectionLog.Printf("Detected usage of activation.outputs.text: %v", hasUsage)
	return hasUsage
}
