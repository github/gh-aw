package workflow

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/stringutil"
)

var compilerYamlLog = logger.New("workflow:compiler_yaml")

// buildJobsAndValidate builds all workflow jobs and validates their dependencies.
// It resets the job manager, builds jobs from the workflow data, and performs
// dependency and duplicate step validation.
func (c *Compiler) buildJobsAndValidate(data *WorkflowData, markdownPath string) error {
	compilerYamlLog.Printf("Building and validating jobs for workflow: %s", data.Name)

	// Reset job manager for this compilation
	c.jobManager = NewJobManager()

	// Build all jobs
	if err := c.buildJobs(data, markdownPath); err != nil {
		compilerYamlLog.Printf("Failed to build jobs: %v", err)
		return fmt.Errorf("failed to build jobs: %w", err)
	}

	compilerYamlLog.Printf("Built %d jobs successfully", len(c.jobManager.GetAllJobs()))

	// Validate job dependencies
	if err := c.jobManager.ValidateDependencies(); err != nil {
		return fmt.Errorf("job dependency validation failed: %w", err)
	}

	// Validate no duplicate steps within jobs (compiler bug detection)
	if err := c.jobManager.ValidateDuplicateSteps(); err != nil {
		return fmt.Errorf("duplicate step validation failed: %w", err)
	}

	return nil
}

// generateWorkflowHeader generates the YAML header section including comments
// for description, source, imports/includes, frontmatter-hash, stop-time, and manual-approval.
// All ANSI escape codes are stripped from the output.
func (c *Compiler) generateWorkflowHeader(yaml *strings.Builder, data *WorkflowData, frontmatterHash string) {
	// Add workflow header with logo and instructions
	sourceFile := "the corresponding .md file"
	if data.Source != "" {
		sourceFile = data.Source
	}
	header := GenerateWorkflowHeader(sourceFile, "gh-aw", "")
	yaml.WriteString(header)

	// Add description comment if provided
	if data.Description != "" {
		cleanDescription := stringutil.StripANSIEscapeCodes(data.Description)
		// Split description into lines and prefix each with "# "
		descriptionLines := strings.Split(strings.TrimSpace(cleanDescription), "\n")
		for _, line := range descriptionLines {
			fmt.Fprintf(yaml, "# %s\n", strings.TrimSpace(line))
		}
	}

	// Add source comment if provided
	if data.Source != "" {
		yaml.WriteString("#\n")
		cleanSource := stringutil.StripANSIEscapeCodes(data.Source)
		// Normalize to Unix paths (forward slashes) for cross-platform compatibility
		cleanSource = filepath.ToSlash(cleanSource)
		fmt.Fprintf(yaml, "# Source: %s\n", cleanSource)
	}

	// Add manifest of imported/included files if any exist
	if len(data.ImportedFiles) > 0 || len(data.IncludedFiles) > 0 {
		yaml.WriteString("#\n")
		yaml.WriteString("# Resolved workflow manifest:\n")

		if len(data.ImportedFiles) > 0 {
			yaml.WriteString("#   Imports:\n")
			for _, file := range data.ImportedFiles {
				cleanFile := stringutil.StripANSIEscapeCodes(file)
				// Normalize to Unix paths (forward slashes) for cross-platform compatibility
				cleanFile = filepath.ToSlash(cleanFile)
				fmt.Fprintf(yaml, "#     - %s\n", cleanFile)
			}
		}

		if len(data.IncludedFiles) > 0 {
			yaml.WriteString("#   Includes:\n")
			for _, file := range data.IncludedFiles {
				cleanFile := stringutil.StripANSIEscapeCodes(file)
				// Normalize to Unix paths (forward slashes) for cross-platform compatibility
				cleanFile = filepath.ToSlash(cleanFile)
				fmt.Fprintf(yaml, "#     - %s\n", cleanFile)
			}
		}
	}

	// Add frontmatter hash if computed
	// Format on a single line to minimize merge conflicts
	if frontmatterHash != "" {
		yaml.WriteString("#\n")
		fmt.Fprintf(yaml, "# frontmatter-hash: %s\n", frontmatterHash)
	}

	// Add stop-time comment if configured
	if data.StopTime != "" {
		yaml.WriteString("#\n")
		cleanStopTime := stringutil.StripANSIEscapeCodes(data.StopTime)
		fmt.Fprintf(yaml, "# Effective stop-time: %s\n", cleanStopTime)
	}

	// Add manual-approval comment if configured
	if data.ManualApproval != "" {
		yaml.WriteString("#\n")
		cleanManualApproval := stringutil.StripANSIEscapeCodes(data.ManualApproval)
		fmt.Fprintf(yaml, "# Manual approval required: environment '%s'\n", cleanManualApproval)
	}

	yaml.WriteString("\n")
}

// generateWorkflowBody generates the main workflow structure including name, triggers,
// permissions, concurrency, run-name, environment variables, cache comments, and jobs.
func (c *Compiler) generateWorkflowBody(yaml *strings.Builder, data *WorkflowData) {
	// Write basic workflow structure
	fmt.Fprintf(yaml, "name: \"%s\"\n", data.Name)
	yaml.WriteString(data.On + "\n\n")

	// Note: GitHub Actions doesn't support workflow-level if conditions
	// The workflow_run safety check is added to individual jobs instead

	// Always write empty permissions at the top level
	// Agent permissions are applied only to the agent job
	yaml.WriteString("permissions: {}\n\n")

	yaml.WriteString(data.Concurrency + "\n\n")
	yaml.WriteString(data.RunName + "\n\n")

	// Add env section if present
	if data.Env != "" {
		yaml.WriteString(data.Env + "\n\n")
	}

	// Add cache comment if cache configuration was provided
	if data.Cache != "" {
		yaml.WriteString("# Cache configuration from frontmatter was processed and added to the main job steps\n\n")
	}

	// Generate jobs section using JobManager
	yaml.WriteString(c.jobManager.RenderToYAML())
}

func (c *Compiler) generateYAML(data *WorkflowData, markdownPath string) (string, error) {
	compilerYamlLog.Printf("Generating YAML for workflow: %s", data.Name)

	// Build all jobs and validate dependencies
	if err := c.buildJobsAndValidate(data, markdownPath); err != nil {
		return "", fmt.Errorf("failed to build and validate jobs: %w", err)
	}

	// Compute frontmatter hash before generating YAML
	var frontmatterHash string
	if markdownPath != "" {
		baseDir := filepath.Dir(markdownPath)
		cache := parser.NewImportCache(baseDir)
		hash, err := parser.ComputeFrontmatterHashFromFile(markdownPath, cache)
		if err != nil {
			compilerYamlLog.Printf("Warning: failed to compute frontmatter hash: %v", err)
			// Continue without hash - non-fatal error
		} else {
			frontmatterHash = hash
			compilerYamlLog.Printf("Computed frontmatter hash: %s", hash)
		}
	}

	// Pre-allocate builder capacity based on estimated workflow size
	// Average workflow generates ~200KB, allocate 256KB to minimize reallocations
	var yaml strings.Builder
	yaml.Grow(256 * 1024)

	// Generate workflow header comments (including hash)
	c.generateWorkflowHeader(&yaml, data, frontmatterHash)

	// Generate workflow body structure
	c.generateWorkflowBody(&yaml, data)

	yamlContent := yaml.String()

	// If we're in non-cloning trial mode and this workflow has issue triggers,
	// replace github.event.issue.number with inputs.issue_number
	if c.trialMode && c.hasIssueTrigger(data.On) {
		compilerYamlLog.Print("Trial mode enabled, replacing issue number references")
		yamlContent = c.replaceIssueNumberReferences(yamlContent)
	}

	compilerYamlLog.Printf("Successfully generated YAML for workflow: %s (%d bytes)", data.Name, len(yamlContent))
	return yamlContent, nil
}

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

func (c *Compiler) generatePrompt(yaml *strings.Builder, data *WorkflowData) {
	compilerYamlLog.Printf("Generating prompt for workflow: %s (markdown size: %d bytes)", data.Name, len(data.MarkdownContent))

	// Collect built-in prompt sections (these should be prepended to user prompt)
	builtinSections := c.collectPromptSections(data)
	compilerYamlLog.Printf("Collected %d built-in prompt sections", len(builtinSections))

	// NEW APPROACH: Use runtime-import macros for imports without inputs
	// - Imported markdown without inputs uses runtime-import macros (loaded at runtime)
	// - Imported markdown with inputs is still inlined (compile-time substitution required)
	// - Main workflow markdown body uses runtime-import to allow editing without recompilation
	// This ensures consistency for most imports while maintaining import inputs functionality

	var userPromptChunks []string
	var expressionMappings []*ExpressionMapping

	// Step 1a: Process and inline imported markdown with inputs (if any)
	// Imports with inputs MUST be inlined because substitution happens at compile time
	if data.ImportedMarkdown != "" {
		compilerYamlLog.Printf("Processing imported markdown (%d bytes)", len(data.ImportedMarkdown))

		// Clean and process imported markdown
		cleanedImportedMarkdown := removeXMLComments(data.ImportedMarkdown)

		// Substitute import inputs in imported content
		if len(data.ImportInputs) > 0 {
			compilerYamlLog.Printf("Substituting %d import input values", len(data.ImportInputs))
			cleanedImportedMarkdown = SubstituteImportInputs(cleanedImportedMarkdown, data.ImportInputs)
		}

		// Wrap GitHub expressions in template conditionals
		cleanedImportedMarkdown = wrapExpressionsInTemplateConditionals(cleanedImportedMarkdown)

		// Extract expressions from imported content
		extractor := NewExpressionExtractor()
		importedExprMappings, err := extractor.ExtractExpressions(cleanedImportedMarkdown)
		if err == nil && len(importedExprMappings) > 0 {
			cleanedImportedMarkdown = extractor.ReplaceExpressionsWithEnvVars(cleanedImportedMarkdown)
			expressionMappings = importedExprMappings
		}

		// Split imported content into chunks and add to user prompt
		importedChunks := splitContentIntoChunks(cleanedImportedMarkdown)
		userPromptChunks = append(userPromptChunks, importedChunks...)
		compilerYamlLog.Printf("Inlined imported markdown with inputs in %d chunks", len(importedChunks))
	}

	// Step 1b: Generate runtime-import macros for imported markdown without inputs
	// These imports don't need compile-time substitution, so they can be loaded at runtime
	if len(data.ImportPaths) > 0 {
		compilerYamlLog.Printf("Generating runtime-import macros for %d imports without inputs", len(data.ImportPaths))
		for _, importPath := range data.ImportPaths {
			// Normalize to Unix paths (forward slashes) for cross-platform compatibility
			importPath = filepath.ToSlash(importPath)
			runtimeImportMacro := fmt.Sprintf("{{#runtime-import %s}}", importPath)
			userPromptChunks = append(userPromptChunks, runtimeImportMacro)
			compilerYamlLog.Printf("Added runtime-import macro for: %s", importPath)
		}
	}

	// Step 1.5: Extract expressions from main workflow markdown (not imported content)
	// This is needed for needs.* expressions and other compile-time expressions
	// The main workflow markdown uses runtime-import, but expressions like needs.* must be
	// available at compile time for the substitute placeholders step
	// Use MainWorkflowMarkdown (not MarkdownContent) to avoid extracting from imported content
	if data.MainWorkflowMarkdown != "" {
		compilerYamlLog.Printf("Extracting expressions from main workflow markdown (%d bytes)", len(data.MainWorkflowMarkdown))

		// Create a new extractor for main workflow markdown
		mainExtractor := NewExpressionExtractor()
		mainExprMappings, err := mainExtractor.ExtractExpressions(data.MainWorkflowMarkdown)
		if err == nil && len(mainExprMappings) > 0 {
			compilerYamlLog.Printf("Extracted %d expressions from main workflow markdown", len(mainExprMappings))
			// Merge with imported expressions (append to existing mappings)
			expressionMappings = append(expressionMappings, mainExprMappings...)
		}
	}

	// Step 2: Add runtime-import for main workflow markdown
	// This allows users to edit the main workflow file without recompilation
	workflowBasename := filepath.Base(c.markdownPath)

	// Determine the directory path relative to workspace root
	// For a workflow at ".github/workflows/test.md", the runtime-import path should be ".github/workflows/test.md"
	// This makes the path explicit and matches the actual file location in the repository
	var workflowFilePath string
	if strings.Contains(c.markdownPath, ".github") {
		// Extract everything from ".github/" onwards (inclusive)
		githubIndex := strings.Index(c.markdownPath, ".github")
		if githubIndex != -1 {
			workflowFilePath = c.markdownPath[githubIndex:]
		} else {
			// Fallback
			workflowFilePath = workflowBasename
		}
	} else {
		// For non-standard paths (like /tmp/test.md), just use the basename
		workflowFilePath = workflowBasename
	}

	// Normalize to Unix paths (forward slashes) for cross-platform compatibility
	workflowFilePath = filepath.ToSlash(workflowFilePath)

	// Create a runtime-import macro for the main workflow markdown
	// The runtime_import.cjs helper will extract and process the markdown body at runtime
	// The path uses .github/ prefix for clarity (e.g., .github/workflows/test.md)
	runtimeImportMacro := fmt.Sprintf("{{#runtime-import %s}}", workflowFilePath)
	compilerYamlLog.Printf("Using runtime-import for main workflow markdown: %s", workflowFilePath)

	// Append runtime-import macro after imported chunks
	userPromptChunks = append(userPromptChunks, runtimeImportMacro)

	// Generate a single unified prompt creation step
	c.generateUnifiedPromptCreationStep(yaml, builtinSections, userPromptChunks, expressionMappings, data)

	// Add combined interpolation and template rendering step
	c.generateInterpolationAndTemplateStep(yaml, expressionMappings, data)

	// Validate that all placeholders have been substituted
	yaml.WriteString("      - name: Validate prompt placeholders\n")
	yaml.WriteString("        env:\n")
	yaml.WriteString("          GH_AW_PROMPT: /tmp/gh-aw/aw-prompts/prompt.txt\n")
	yaml.WriteString("        run: bash /opt/gh-aw/actions/validate_prompt_placeholders.sh\n")

	// Print prompt (merged into prompt generation)
	yaml.WriteString("      - name: Print prompt\n")
	yaml.WriteString("        env:\n")
	yaml.WriteString("          GH_AW_PROMPT: /tmp/gh-aw/aw-prompts/prompt.txt\n")
	yaml.WriteString("        run: bash /opt/gh-aw/actions/print_prompt_summary.sh\n")
}
func (c *Compiler) generatePostSteps(yaml *strings.Builder, data *WorkflowData) {
	if data.PostSteps != "" {
		// Remove "post-steps:" line and adjust indentation, similar to CustomSteps processing
		lines := strings.Split(data.PostSteps, "\n")
		if len(lines) > 1 {
			for _, line := range lines[1:] {
				// Trim trailing whitespace
				trimmed := strings.TrimRight(line, " ")
				// Skip empty lines
				if strings.TrimSpace(trimmed) == "" {
					yaml.WriteString("\n")
					continue
				}
				// Steps need 6-space indentation (      - name:)
				// Nested properties need 8-space indentation (        run:)
				if strings.HasPrefix(line, "  ") {
					yaml.WriteString("        " + line[2:] + "\n")
				} else {
					yaml.WriteString("      " + line + "\n")
				}
			}
		}
	}
}

func (c *Compiler) generateCreateAwInfo(yaml *strings.Builder, data *WorkflowData, engine CodingAgentEngine) {
	yaml.WriteString("      - name: Generate agentic run info\n")
	yaml.WriteString("        id: generate_aw_info\n") // Add ID for outputs
	fmt.Fprintf(yaml, "        uses: %s\n", GetActionPin("actions/github-script"))
	yaml.WriteString("        with:\n")
	yaml.WriteString("          script: |\n")
	yaml.WriteString("            const fs = require('fs');\n")
	yaml.WriteString("            \n")
	yaml.WriteString("            const awInfo = {\n")

	// Engine ID (prefer EngineConfig.ID, fallback to AI field for backwards compatibility)
	engineID := engine.GetID()
	if data.EngineConfig != nil && data.EngineConfig.ID != "" {
		engineID = data.EngineConfig.ID
	} else if data.AI != "" {
		engineID = data.AI
	}
	fmt.Fprintf(yaml, "              engine_id: \"%s\",\n", engineID)

	// Engine display name
	fmt.Fprintf(yaml, "              engine_name: \"%s\",\n", engine.GetDisplayName())

	// Model information - resolve from explicit config or environment variable
	// If model is explicitly configured, use it directly
	// Otherwise, resolve from environment variable at runtime
	// Note: aw_info is always generated in the agent job, so use agent-specific env vars
	modelConfigured := data.EngineConfig != nil && data.EngineConfig.Model != ""
	if modelConfigured {
		// Explicit model - output as static string
		fmt.Fprintf(yaml, "              model: \"%s\",\n", data.EngineConfig.Model)
	} else {
		// Model from environment variable - resolve at runtime
		// Use agent-specific env var since aw_info is generated in agent job
		var modelEnvVar string

		switch engineID {
		case "copilot":
			modelEnvVar = constants.EnvVarModelAgentCopilot
		case "claude":
			modelEnvVar = constants.EnvVarModelAgentClaude
		case "codex":
			modelEnvVar = constants.EnvVarModelAgentCodex
		case "custom":
			modelEnvVar = constants.EnvVarModelAgentCustom
		default:
			// For unknown engines, use a generic environment variable pattern
			// This provides a fallback while maintaining consistency
			modelEnvVar = constants.EnvVarModelAgentCustom
		}

		// Generate JavaScript to resolve model from environment variable at runtime
		fmt.Fprintf(yaml, "              model: process.env.%s || \"\",\n", modelEnvVar)
	}

	// Version information (from engine config, kept for backwards compatibility)
	version := ""
	if data.EngineConfig != nil && data.EngineConfig.Version != "" {
		version = data.EngineConfig.Version
	}
	fmt.Fprintf(yaml, "              version: \"%s\",\n", version)

	// Agent version - use the actual installation version (includes defaults)
	// This matches what BuildStandardNpmEngineInstallSteps uses
	agentVersion := getInstallationVersion(data, engine)
	fmt.Fprintf(yaml, "              agent_version: \"%s\",\n", agentVersion)

	// CLI version - only include for released builds
	// Excludes development builds containing "dev", "dirty", or "test"
	if IsReleasedVersion(c.version) {
		fmt.Fprintf(yaml, "              cli_version: \"%s\",\n", c.version)
	}

	// Workflow information
	fmt.Fprintf(yaml, "              workflow_name: \"%s\",\n", data.Name)
	fmt.Fprintf(yaml, "              experimental: %t,\n", engine.IsExperimental())
	fmt.Fprintf(yaml, "              supports_tools_allowlist: %t,\n", engine.SupportsToolsAllowlist())
	fmt.Fprintf(yaml, "              supports_http_transport: %t,\n", engine.SupportsHTTPTransport())

	// Run metadata
	yaml.WriteString("              run_id: context.runId,\n")
	yaml.WriteString("              run_number: context.runNumber,\n")
	yaml.WriteString("              run_attempt: process.env.GITHUB_RUN_ATTEMPT,\n")
	yaml.WriteString("              repository: context.repo.owner + '/' + context.repo.repo,\n")
	yaml.WriteString("              ref: context.ref,\n")
	yaml.WriteString("              sha: context.sha,\n")
	yaml.WriteString("              actor: context.actor,\n")
	yaml.WriteString("              event_name: context.eventName,\n")

	// Add staged value from safe-outputs configuration
	stagedValue := "false"
	if data.SafeOutputs != nil && data.SafeOutputs.Staged {
		stagedValue = "true"
	}
	fmt.Fprintf(yaml, "              staged: %s,\n", stagedValue)

	// Network configuration
	var allowedDomains []string
	firewallEnabled := false
	firewallVersion := ""

	if data.NetworkPermissions != nil {
		allowedDomains = data.NetworkPermissions.Allowed
		if data.NetworkPermissions.Firewall != nil {
			firewallEnabled = data.NetworkPermissions.Firewall.Enabled
			firewallVersion = data.NetworkPermissions.Firewall.Version
			// Use default firewall version when enabled but not explicitly set
			if firewallEnabled && firewallVersion == "" {
				firewallVersion = string(constants.DefaultFirewallVersion)
			}
		}
	}

	// Add allowed domains as JSON array
	if len(allowedDomains) > 0 {
		domainsJSON, _ := json.Marshal(allowedDomains)
		fmt.Fprintf(yaml, "              allowed_domains: %s,\n", string(domainsJSON))
	} else {
		yaml.WriteString("              allowed_domains: [],\n")
	}

	fmt.Fprintf(yaml, "              firewall_enabled: %t,\n", firewallEnabled)
	fmt.Fprintf(yaml, "              awf_version: \"%s\",\n", firewallVersion)

	// MCP Gateway version
	mcpGatewayVersion := ""
	if data.SandboxConfig != nil && data.SandboxConfig.MCP != nil && data.SandboxConfig.MCP.Version != "" {
		mcpGatewayVersion = data.SandboxConfig.MCP.Version
	}
	fmt.Fprintf(yaml, "              awmg_version: \"%s\",\n", mcpGatewayVersion)

	// Add steps object with firewall information
	yaml.WriteString("              steps: {\n")

	// Determine firewall type
	firewallType := ""
	if isFirewallEnabled(data) {
		firewallType = "squid"
	}
	fmt.Fprintf(yaml, "                firewall: \"%s\"\n", firewallType)

	yaml.WriteString("              },\n")

	yaml.WriteString("              created_at: new Date().toISOString()\n")

	yaml.WriteString("            };\n")
	yaml.WriteString("            \n")
	yaml.WriteString("            // Write to /tmp/gh-aw directory to avoid inclusion in PR\n")
	yaml.WriteString("            const tmpPath = '/tmp/gh-aw/aw_info.json';\n")
	yaml.WriteString("            fs.writeFileSync(tmpPath, JSON.stringify(awInfo, null, 2));\n")
	yaml.WriteString("            console.log('Generated aw_info.json at:', tmpPath);\n")
	yaml.WriteString("            console.log(JSON.stringify(awInfo, null, 2));\n")
	yaml.WriteString("            \n")
	yaml.WriteString("            // Set model as output for reuse in other steps/jobs\n")
	yaml.WriteString("            core.setOutput('model', awInfo.model);\n")
}

// generateWorkflowOverviewStep generates a step that writes an agentic workflow run overview to the GitHub step summary.
// This runs after aw_info.json is created and reads from it for consistent data display.
// Uses HTML details/summary tags for collapsible output.
func (c *Compiler) generateWorkflowOverviewStep(yaml *strings.Builder, data *WorkflowData, engine CodingAgentEngine) {
	yaml.WriteString("      - name: Generate workflow overview\n")
	fmt.Fprintf(yaml, "        uses: %s\n", GetActionPin("actions/github-script"))
	yaml.WriteString("        with:\n")
	yaml.WriteString("          script: |\n")
	yaml.WriteString("            const { generateWorkflowOverview } = require('/opt/gh-aw/actions/generate_workflow_overview.cjs');\n")
	yaml.WriteString("            await generateWorkflowOverview(core);\n")
}

func (c *Compiler) generateOutputCollectionStep(yaml *strings.Builder, data *WorkflowData) {
	// Record artifact upload for validation
	c.stepOrderTracker.RecordArtifactUpload("Upload Safe Outputs", []string{"${{ env.GH_AW_SAFE_OUTPUTS }}"})

	yaml.WriteString("      - name: Upload Safe Outputs\n")
	yaml.WriteString("        if: always()\n")
	fmt.Fprintf(yaml, "        uses: %s\n", GetActionPin("actions/upload-artifact"))
	yaml.WriteString("        with:\n")
	fmt.Fprintf(yaml, "          name: %s\n", constants.SafeOutputArtifactName)
	yaml.WriteString("          path: ${{ env.GH_AW_SAFE_OUTPUTS }}\n")
	yaml.WriteString("          if-no-files-found: warn\n")

	yaml.WriteString("      - name: Ingest agent output\n")
	yaml.WriteString("        id: collect_output\n")
	fmt.Fprintf(yaml, "        uses: %s\n", GetActionPin("actions/github-script"))

	// Add environment variables for JSONL validation
	yaml.WriteString("        env:\n")
	yaml.WriteString("          GH_AW_SAFE_OUTPUTS: ${{ env.GH_AW_SAFE_OUTPUTS }}\n")

	// Config is written to file, not passed as env var

	// Add allowed domains configuration for sanitization
	// Use manually configured domains if available, otherwise compute from network configuration
	var domainsStr string
	if data.SafeOutputs != nil && len(data.SafeOutputs.AllowedDomains) > 0 {
		// Use manually configured allowed domains
		domainsStr = strings.Join(data.SafeOutputs.AllowedDomains, ",")
	} else {
		// Fall back to computing from network configuration (same as firewall)
		domainsStr = c.computeAllowedDomainsForSanitization(data)
	}
	if domainsStr != "" {
		fmt.Fprintf(yaml, "          GH_AW_ALLOWED_DOMAINS: %q\n", domainsStr)
	}

	// Add allowed GitHub references configuration for reference escaping
	if data.SafeOutputs != nil && data.SafeOutputs.AllowGitHubReferences != nil {
		refsStr := strings.Join(data.SafeOutputs.AllowGitHubReferences, ",")
		fmt.Fprintf(yaml, "          GH_AW_ALLOWED_GITHUB_REFS: %q\n", refsStr)
	}

	// Add GitHub server URL and API URL for dynamic domain extraction
	// This allows the sanitization code to permit GitHub domains that vary by deployment
	yaml.WriteString("          GITHUB_SERVER_URL: ${{ github.server_url }}\n")
	yaml.WriteString("          GITHUB_API_URL: ${{ github.api_url }}\n")

	// Add command name for command trigger prevention in safe outputs
	if len(data.Command) > 0 {
		// Pass first command for backward compatibility
		fmt.Fprintf(yaml, "          GH_AW_COMMAND: %s\n", data.Command[0])
	}

	yaml.WriteString("        with:\n")
	yaml.WriteString("          script: |\n")

	// Load script from external file using require()
	yaml.WriteString("            const { setupGlobals } = require('/opt/gh-aw/actions/setup_globals.cjs');\n")
	yaml.WriteString("            setupGlobals(core, github, context, exec, io);\n")
	yaml.WriteString("            const { main } = require('/opt/gh-aw/actions/collect_ndjson_output.cjs');\n")
	yaml.WriteString("            await main();\n")

	// Record artifact upload for validation
	c.stepOrderTracker.RecordArtifactUpload("Upload sanitized agent output", []string{"${{ env.GH_AW_AGENT_OUTPUT }}"})

	yaml.WriteString("      - name: Upload sanitized agent output\n")
	yaml.WriteString("        if: always() && env.GH_AW_AGENT_OUTPUT\n")
	fmt.Fprintf(yaml, "        uses: %s\n", GetActionPin("actions/upload-artifact"))
	yaml.WriteString("        with:\n")
	fmt.Fprintf(yaml, "          name: %s\n", constants.AgentOutputArtifactName)
	yaml.WriteString("          path: ${{ env.GH_AW_AGENT_OUTPUT }}\n")
	yaml.WriteString("          if-no-files-found: warn\n")

}
