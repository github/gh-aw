package workflow

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/stringutil"
)

var compilerStringAPILog = logger.New("workflow:compiler_string_api")

// CompileToYAML compiles workflow data and returns the YAML as a string
// without writing to disk. This is useful for Wasm builds and programmatic usage.
func (c *Compiler) CompileToYAML(workflowData *WorkflowData, markdownPath string) (string, error) {
	compilerStringAPILog.Printf("CompileToYAML: markdownPath=%s", markdownPath)
	c.markdownPath = markdownPath
	c.skipHeader = true
	c.configureGHESCompatibility()
	if workflowData != nil {
		workflowData.GHES = c.ghesArtifactCompat
	}
	// Clear contentOverride after compilation (set by ParseWorkflowString)
	defer func() { c.contentOverride = "" }()

	startTime := time.Now()
	defer func() {
		workflowLog.Printf("CompileToYAML completed in %v", time.Since(startTime))
	}()

	c.stepOrderTracker = NewStepOrderTracker()
	c.scheduleFriendlyFormats = nil

	if c.artifactManager == nil {
		c.artifactManager = NewArtifactManager()
	} else {
		c.artifactManager.Reset()
	}

	lockFile := stringutil.MarkdownToLockFile(markdownPath)

	if err := c.validateWorkflowData(workflowData, markdownPath); err != nil {
		return "", err
	}

	yamlContent, _, _, err := c.generateAndValidateYAML(workflowData, markdownPath, lockFile)
	if err != nil {
		return "", err
	}

	return yamlContent, nil
}

// ParseWorkflowString parses workflow markdown content from a string rather than a file.
// This is the primary entry point for Wasm/browser usage where filesystem access is unavailable.
// The virtualPath is used for error messages and lock file naming (e.g., "workflow.md").
func (c *Compiler) ParseWorkflowString(content string, virtualPath string) (*WorkflowData, error) {
	workflowLog.Printf("ParseWorkflowString: parsing %d bytes with virtual path %s", len(content), virtualPath)

	c.configureGHESCompatibility()

	cleanPath := filepath.Clean(virtualPath)
	contentBytes := []byte(content)

	// Store content so downstream code can use it instead of reading from disk.
	// Cleared in CompileToYAML after compilation completes.
	c.contentOverride = content

	// Enable inline prompt mode for string-based compilation (Wasm/browser)
	// since runtime-import macros cannot resolve without filesystem access
	c.inlinePrompt = true

	// Parse frontmatter directly from content string
	result, err := parser.ExtractFrontmatterFromContent(content)
	if err != nil {
		frontmatterStart := 2
		if result != nil && result.FrontmatterStart > 0 {
			frontmatterStart = result.FrontmatterStart
		}
		return nil, c.createFrontmatterError(cleanPath, content, err, frontmatterStart)
	}

	if !hasMeaningfulFrontmatter(result) {
		return nil, errors.New("no frontmatter found")
	}

	compilerStringAPILog.Printf("ParseWorkflowString: extracted frontmatter with %d fields", len(result.Frontmatter))

	// Preprocess schedule fields
	if err := c.preprocessScheduleFields(result.Frontmatter, cleanPath, content); err != nil {
		return nil, err
	}

	frontmatterForValidation := c.copyFrontmatterWithoutInternalMarkers(result.Frontmatter)

	// Check if "on" field is missing - distinguish redirect-only placeholders from shared workflows
	_, hasOnField := frontmatterForValidation["on"]
	if !hasOnField {
		// Check if this is a redirect-only placeholder (has redirect field but no 'on' trigger).
		// Redirect-only files are distinct from regular shared workflows: they are placeholders
		// pointing to a workflow's new canonical location and should not be treated as importable components.
		if redirectVal, hasRedirect := frontmatterForValidation["redirect"]; hasRedirect {
			if redirectStr, ok := redirectVal.(string); ok {
				if redirectTarget := strings.TrimSpace(redirectStr); redirectTarget != "" {
					compilerStringAPILog.Printf("ParseWorkflowString: redirect-only workflow detected: redirect=%s", redirectTarget)
					return nil, &RedirectOnlyWorkflowError{Path: cleanPath, Target: redirectTarget}
				}
			}
		}
		compilerStringAPILog.Printf("ParseWorkflowString: no 'on' field, treating as shared workflow: %s", cleanPath)
		return nil, &SharedWorkflowError{Path: cleanPath}
	}

	if err := c.validateEngineBeforeSchema(cleanPath, contentBytes, result, frontmatterForValidation); err != nil {
		compilerStringAPILog.Printf("ParseWorkflowString: string engine pre-validation failed for %s", cleanPath)
		return nil, err
	}

	// Validate frontmatter against schema
	if err := parser.ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatterForValidation, cleanPath); err != nil {
		compilerStringAPILog.Printf("ParseWorkflowString: schema validation failed for %s", cleanPath)
		return nil, err
	}

	compilerStringAPILog.Printf("ParseWorkflowString: frontmatter validated, frontmatter_fields=%d", len(frontmatterForValidation))

	// Build parse result to reuse the rest of the orchestrator pipeline
	parseResult := &frontmatterParseResult{
		cleanPath:                cleanPath,
		content:                  contentBytes,
		frontmatterResult:        result,
		frontmatterForValidation: frontmatterForValidation,
		markdownDir:              filepath.Dir(cleanPath),
		isSharedWorkflow:         false,
	}

	// Setup engine and process imports
	engineSetup, err := c.setupEngineAndImports(parseResult.frontmatterResult, parseResult.cleanPath, parseResult.content, parseResult.markdownDir)
	if err != nil {
		return nil, err
	}

	// Process tools and markdown
	toolsResult, err := c.processToolsAndMarkdown(parseResult.frontmatterResult, parseResult.cleanPath, parseResult.markdownDir, engineSetup.agenticEngine, engineSetup.engineSetting, engineSetup.importsResult)
	if err != nil {
		return nil, err
	}

	// Build initial workflow data structure
	workflowData := c.buildInitialWorkflowData(parseResult.frontmatterResult, toolsResult, engineSetup, engineSetup.importsResult)
	workflowData.WorkflowID = GetWorkflowIDFromPath(cleanPath)
	resolveMaxDailyAICBackendIntoWorkflowData(workflowData, parseResult.frontmatterResult.Frontmatter, engineSetup.importsResult.MergedMaxDailyAICredits)

	// Validate bash tool configuration
	if err := validateBashToolConfig(workflowData.ParsedTools, workflowData.Name); err != nil {
		return nil, fmt.Errorf("%s: %w", cleanPath, err)
	}

	// Validate that cli-proxy is not enabled while shell execution is refused
	if err := validateCLIProxyBashCompatibility(workflowData.Tools, workflowData.Name); err != nil {
		return nil, fmt.Errorf("%s: %w", cleanPath, err)
	}

	// Validate optional engine.mcp.session-timeout configuration.
	if err := c.validateEngineMCPSessionTimeout(workflowData); err != nil {
		return nil, fmt.Errorf("%s: %w", cleanPath, err)
	}

	// Validate optional engine.mcp.tool-timeout configuration.
	if err := c.validateEngineMCPToolTimeout(workflowData); err != nil {
		return nil, fmt.Errorf("%s: %w", cleanPath, err)
	}

	// Validate GitHub tool configuration
	if err := validateGitHubToolConfig(workflowData.ParsedTools, workflowData.Name); err != nil {
		return nil, fmt.Errorf("%s: %w", cleanPath, err)
	}

	// Validate GitHub tool read-only configuration
	if err := validateGitHubReadOnly(workflowData.ParsedTools, workflowData.Name); err != nil {
		return nil, fmt.Errorf("%s: %w", cleanPath, err)
	}

	// Validate GitHub guard policy configuration
	if err := validateGitHubGuardPolicy(workflowData.ParsedTools, workflowData.Name); err != nil {
		return nil, fmt.Errorf("%s: %w", cleanPath, err)
	}
	emitGitHubLockdownGuardPolicyWarning(c, workflowData.ParsedTools, cleanPath)
	emitMinIntegrityNoneBashWarning(c, workflowData.ParsedTools, cleanPath)

	// Validate integrity-reactions feature configuration
	var gatewayConfig *MCPGatewayRuntimeConfig
	if workflowData.SandboxConfig != nil {
		gatewayConfig = workflowData.SandboxConfig.MCP
	}
	if err := validateIntegrityReactions(workflowData.ParsedTools, workflowData.Name, workflowData, gatewayConfig); err != nil {
		return nil, fmt.Errorf("%s: %w", cleanPath, err)
	}

	// Setup action cache and resolver
	actionCache, actionResolver := c.ensureSharedActionCacheAndResolver()
	workflowData.Ctx = c.ctx
	workflowData.ActionCache = actionCache
	workflowData.ActionResolver = actionResolver
	workflowData.ActionPinWarnings = c.actionPinWarnings
	workflowData.ActionPinMappings = c.getActionPinMappings()
	workflowData.ContainerPinMappings = c.getContainerPinMappings()

	// Extract YAML configuration sections
	if err := c.extractYAMLSections(parseResult.frontmatterResult.Frontmatter, workflowData); err != nil {
		return nil, fmt.Errorf("failed to extract YAML sections: %w", err)
	}

	// Merge features from imports
	if len(engineSetup.importsResult.MergedFeatures) > 0 {
		compilerStringAPILog.Printf("ParseWorkflowString: merging %d features from imports", len(engineSetup.importsResult.MergedFeatures))
		mergedFeatures, err := c.MergeFeatures(workflowData.Features, engineSetup.importsResult.MergedFeatures)
		if err != nil {
			return nil, fmt.Errorf("failed to merge features from imports: %w", err)
		}
		workflowData.Features = mergedFeatures
	}

	// Process and merge custom steps
	if err := c.processAndMergeSteps(parseResult.frontmatterResult.Frontmatter, workflowData, engineSetup.importsResult); err != nil {
		return nil, err
	}

	// Apply defaults
	if err := c.applyDefaults(workflowData, cleanPath); err != nil {
		return nil, err
	}

	return workflowData, nil
}
