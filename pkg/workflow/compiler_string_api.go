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
	"github.com/github/gh-aw/pkg/workflow/aw"
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

	// Store content so downstream code can use it instead of reading from disk.
	// Cleared in CompileToYAML after compilation completes.
	c.contentOverride = content

	// Enable inline prompt mode for string-based compilation (Wasm/browser)
	// since runtime-import macros cannot resolve without filesystem access
	c.inlinePrompt = true

	parseResult, err := c.parseStringFrontmatter(content, cleanPath)
	if err != nil {
		return nil, err
	}
	ctx := newWorkflowBuildContext(parseResult)
	if err := c.setupWorkflowBuildContext(ctx); err != nil {
		return nil, err
	}
	if err := c.validateStringWorkflowData(ctx.workflowData, cleanPath); err != nil {
		return nil, err
	}
	if err := c.populateWorkflowBuildContext(ctx); err != nil {
		return nil, err
	}
	if err := c.applyDefaults(ctx.workflowData, cleanPath); err != nil {
		return nil, err
	}
	return ctx.workflowData, nil
}

func (c *Compiler) parseStringFrontmatter(content, cleanPath string) (*frontmatterParseResult, error) {
	// Parse frontmatter directly from content string
	document, err := aw.Parse(content)
	if err != nil {
		return nil, c.createFrontmatterError(cleanPath, content, err, 2)
	}
	result := document.FrontmatterResult()

	if !hasMeaningfulFrontmatter(result) {
		return nil, errors.New("no frontmatter found")
	}

	compilerStringAPILog.Printf("ParseWorkflowString: extracted frontmatter with %d fields", len(result.Frontmatter))

	// Preprocess schedule fields
	if err := c.preprocessScheduleFields(result.Frontmatter, cleanPath, content); err != nil {
		return nil, err
	}
	if err := c.normalizeImportedConnectors(result.Frontmatter, cleanPath, content); err != nil {
		return nil, err
	}

	frontmatterForValidation := c.copyFrontmatterWithoutInternalMarkers(result.Frontmatter)

	// Check if "on" field is missing - distinguish redirect-only placeholders from shared workflows
	_, hasOnField := frontmatterForValidation["on"]
	if !hasOnField && !parser.HasConnectorTriggers(frontmatterForValidation) {
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

	if err := c.validateEngineBeforeSchema(cleanPath, []byte(content), result, frontmatterForValidation); err != nil {
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
	parsed := createFrontmatterParseResult(cleanPath, []byte(content), result, frontmatterForValidation)
	parsed.document = document
	return parsed, nil
}

func (c *Compiler) validateStringWorkflowData(workflowData *WorkflowData, cleanPath string) error {
	// Validate bash tool configuration
	if err := validateBashToolConfig(workflowData.ParsedTools, workflowData.Name); err != nil {
		return fmt.Errorf("%s: %w", cleanPath, err)
	}

	// Validate that cli-proxy is not enabled while shell execution is refused
	if err := validateCLIProxyBashCompatibility(workflowData.Tools, workflowData.Name); err != nil {
		return fmt.Errorf("%s: %w", cleanPath, err)
	}

	// Validate optional engine.mcp.session-timeout configuration.
	if err := c.validateEngineMCPSessionTimeout(workflowData); err != nil {
		return fmt.Errorf("%s: %w", cleanPath, err)
	}

	// Validate optional engine.mcp.tool-timeout configuration.
	if err := c.validateEngineMCPToolTimeout(workflowData); err != nil {
		return fmt.Errorf("%s: %w", cleanPath, err)
	}

	// Validate GitHub tool configuration
	if err := validateGitHubToolConfig(workflowData.ParsedTools, workflowData.Name); err != nil {
		return fmt.Errorf("%s: %w", cleanPath, err)
	}

	// Validate GitHub tool read-only configuration
	if err := validateGitHubReadOnly(workflowData.ParsedTools, workflowData.Name); err != nil {
		return fmt.Errorf("%s: %w", cleanPath, err)
	}

	// Validate GitHub guard policy configuration
	if err := validateGitHubGuardPolicy(workflowData.ParsedTools, workflowData.Name); err != nil {
		return fmt.Errorf("%s: %w", cleanPath, err)
	}
	emitGitHubLockdownGuardPolicyWarning(c, workflowData.ParsedTools, cleanPath)
	emitMinIntegrityNoneBashWarning(c, workflowData.ParsedTools, cleanPath)

	// Validate integrity-reactions feature configuration
	var gatewayConfig *MCPGatewayRuntimeConfig
	if workflowData.SandboxConfig != nil {
		gatewayConfig = workflowData.SandboxConfig.MCP
	}
	if err := validateIntegrityReactions(workflowData.ParsedTools, workflowData.Name, workflowData, gatewayConfig); err != nil {
		return fmt.Errorf("%s: %w", cleanPath, err)
	}
	return nil
}
