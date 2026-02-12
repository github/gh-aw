package workflow

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stringutil"
)

var log = logger.New("workflow:compiler")

const (
	// MaxLockFileSize is the maximum allowed size for generated lock workflow files (500KB)
	MaxLockFileSize = 512000 // 500KB in bytes

	// MaxExpressionSize is the maximum allowed size for GitHub Actions expression values (21KB)
	// This includes environment variable values, if conditions, and other expression contexts
	// See: https://docs.github.com/en/actions/learn-github-actions/usage-limits-billing-and-administration
	MaxExpressionSize = 21000 // 21KB in bytes

	// MaxPromptChunkSize is the maximum size for each chunk when splitting prompt text (20KB)
	// This limit ensures each heredoc block stays under GitHub Actions step size limits (21KB)
	MaxPromptChunkSize = 20000 // 20KB limit for each chunk

	// MaxPromptChunks is the maximum number of chunks allowed when splitting prompt text
	// This prevents excessive step generation for extremely large prompt texts
	MaxPromptChunks = 5 // Maximum number of chunks
)

//go:embed schemas/github-workflow.json
var githubWorkflowSchema string

// formatCompilerError creates a formatted compiler error message with optional error wrapping
// filePath: the file path to include in the error (typically markdownPath or lockFile)
// errType: the error type ("error" or "warning")
// message: the error message text
// cause: optional underlying error to wrap (use nil for validation errors)
func formatCompilerError(filePath string, errType string, message string, cause error) error {
	formattedErr := console.FormatError(console.CompilerError{
		Position: console.ErrorPosition{
			File:   filePath,
			Line:   1,
			Column: 1,
		},
		Type:    errType,
		Message: message,
	})

	// Wrap the underlying error if provided (preserves error chain)
	if cause != nil {
		return fmt.Errorf("%s: %w", formattedErr, cause)
	}

	// Create new error for validation errors (no underlying cause)
	return errors.New(formattedErr)
}

// formatCompilerMessage creates a formatted compiler message string (for warnings printed to stderr)
// filePath: the file path to include in the message (typically markdownPath or lockFile)
// msgType: the message type ("error" or "warning")
// message: the message text
func formatCompilerMessage(filePath string, msgType string, message string) string {
	return console.FormatError(console.CompilerError{
		Position: console.ErrorPosition{
			File:   filePath,
			Line:   1,
			Column: 1,
		},
		Type:    msgType,
		Message: message,
	})
}

// CompileWorkflow compiles a workflow markdown file into a GitHub Actions YAML file.
// It reads the file from disk, parses frontmatter and markdown sections, and generates
// the corresponding workflow YAML. Returns the compiled workflow data or an error.
//
// The compilation process includes:
//   - Reading and parsing the markdown file
//   - Extracting frontmatter configuration
//   - Validating workflow configuration
//   - Generating GitHub Actions YAML
//   - Writing the compiled workflow to a .lock.yml file
//
// This is the main entry point for compiling workflows from disk. For compiling
// pre-parsed workflow data, use CompileWorkflowData instead.
func (c *Compiler) CompileWorkflow(markdownPath string) error {
	// Store markdownPath for use in dynamic tool generation
	c.markdownPath = markdownPath

	// Parse the markdown file
	log.Printf("Parsing workflow file")
	workflowData, err := c.ParseWorkflowFile(markdownPath)
	if err != nil {
		// Check if this is already a formatted console error
		if strings.Contains(err.Error(), ":") && (strings.Contains(err.Error(), "error:") || strings.Contains(err.Error(), "warning:")) {
			// Already formatted, return as-is
			return err
		}
		// Otherwise, create a basic formatted error with wrapping
		return formatCompilerError(markdownPath, "error", err.Error(), err)
	}

	return c.CompileWorkflowData(workflowData, markdownPath)
}

// validateWorkflowData performs comprehensive validation of workflow configuration
// including expressions, features, permissions, and configurations.
func (c *Compiler) validateWorkflowData(workflowData *WorkflowData, markdownPath string) error {
	// Validate expression safety - check that all GitHub Actions expressions are in the allowed list
	log.Printf("Validating expression safety")
	if err := validateExpressionSafety(workflowData.MarkdownContent); err != nil {
		return formatCompilerError(markdownPath, "error", err.Error(), err)
	}

	// Validate expressions in runtime-import files at compile time
	log.Printf("Validating runtime-import files")
	// Go up from .github/workflows/file.md to repo root
	workflowDir := filepath.Dir(markdownPath) // .github/workflows
	githubDir := filepath.Dir(workflowDir)    // .github
	workspaceDir := filepath.Dir(githubDir)   // repo root
	if err := validateRuntimeImportFiles(workflowData.MarkdownContent, workspaceDir); err != nil {
		return formatCompilerError(markdownPath, "error", err.Error(), err)
	}

	// Validate feature flags
	log.Printf("Validating feature flags")
	if err := validateFeatures(workflowData); err != nil {
		return formatCompilerError(markdownPath, "error", err.Error(), err)
	}

	// Check for action-mode feature flag override
	if workflowData.Features != nil {
		if actionModeVal, exists := workflowData.Features["action-mode"]; exists {
			if actionModeStr, ok := actionModeVal.(string); ok && actionModeStr != "" {
				mode := ActionMode(actionModeStr)
				if !mode.IsValid() {
					return formatCompilerError(markdownPath, "error", fmt.Sprintf("invalid action-mode feature flag '%s'. Must be 'dev', 'release', or 'script'", actionModeStr), nil)
				}
				log.Printf("Overriding action mode from feature flag: %s", mode)
				c.SetActionMode(mode)
			}
		}
	}

	// Validate dangerous permissions
	log.Printf("Validating dangerous permissions")
	if err := validateDangerousPermissions(workflowData); err != nil {
		return formatCompilerError(markdownPath, "error", err.Error(), err)
	}

	// Validate agent file exists if specified in engine config
	log.Printf("Validating agent file if specified")
	if err := c.validateAgentFile(workflowData, markdownPath); err != nil {
		return err
	}

	// Validate sandbox configuration
	log.Printf("Validating sandbox configuration")
	if err := validateSandboxConfig(workflowData); err != nil {
		return formatCompilerError(markdownPath, "error", err.Error(), err)
	}

	// Validate safe-outputs target configuration
	log.Printf("Validating safe-outputs target fields")
	if err := validateSafeOutputsTarget(workflowData.SafeOutputs); err != nil {
		return formatCompilerError(markdownPath, "error", err.Error(), err)
	}

	// Validate safe-outputs allowed-domains configuration
	log.Printf("Validating safe-outputs allowed-domains")
	if err := c.validateSafeOutputsAllowedDomains(workflowData.SafeOutputs); err != nil {
		return formatCompilerError(markdownPath, "error", err.Error(), err)
	}

	// Validate network allowed domains configuration
	log.Printf("Validating network allowed domains")
	if err := c.validateNetworkAllowedDomains(workflowData.NetworkPermissions); err != nil {
		return formatCompilerError(markdownPath, "error", err.Error(), err)
	}

	// Validate network firewall configuration
	log.Printf("Validating network firewall configuration")
	if err := validateNetworkFirewallConfig(workflowData.NetworkPermissions); err != nil {
		return formatCompilerError(markdownPath, "error", err.Error(), err)
	}

	// Validate labels configuration
	log.Printf("Validating labels")
	if err := validateLabels(workflowData); err != nil {
		return formatCompilerError(markdownPath, "error", err.Error(), err)
	}

	// Validate workflow-level concurrency group expression
	log.Printf("Validating workflow-level concurrency configuration")
	if workflowData.Concurrency != "" {
		// Extract the group expression from the concurrency YAML
		// The Concurrency field contains the full YAML (e.g., "concurrency:\n  group: \"...\"")
		// We need to extract just the group value
		groupExpr := extractConcurrencyGroupFromYAML(workflowData.Concurrency)
		if groupExpr != "" {
			if err := validateConcurrencyGroupExpression(groupExpr); err != nil {
				return formatCompilerError(markdownPath, "error", fmt.Sprintf("workflow-level concurrency validation failed: %s", err.Error()), err)
			}
		}
	}

	// Validate engine-level concurrency group expression
	log.Printf("Validating engine-level concurrency configuration")
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Concurrency != "" {
		// Extract the group expression from the engine concurrency YAML
		groupExpr := extractConcurrencyGroupFromYAML(workflowData.EngineConfig.Concurrency)
		if groupExpr != "" {
			if err := validateConcurrencyGroupExpression(groupExpr); err != nil {
				return formatCompilerError(markdownPath, "error", fmt.Sprintf("engine.concurrency validation failed: %s", err.Error()), err)
			}
		}
	}

	// Emit experimental warning for sandbox-runtime feature
	if isSRTEnabled(workflowData) {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Using experimental feature: sandbox-runtime firewall"))
		c.IncrementWarningCount()
	}

	// Emit warning for sandbox.agent: false (disables agent sandbox firewall)
	if isAgentSandboxDisabled(workflowData) {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("⚠️  WARNING: Agent sandbox disabled (sandbox.agent: false). This removes firewall protection. The AI agent will have direct network access without firewall filtering. The MCP gateway remains enabled. Only use this for testing or in controlled environments where you trust the AI agent completely."))
		c.IncrementWarningCount()
	}

	// Emit experimental warning for safe-inputs feature
	if IsSafeInputsEnabled(workflowData.SafeInputs, workflowData) {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Using experimental feature: safe-inputs"))
		c.IncrementWarningCount()
	}

	// Emit experimental warning for plugins feature
	if workflowData.PluginInfo != nil && len(workflowData.PluginInfo.Plugins) > 0 {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Using experimental feature: plugins"))
		c.IncrementWarningCount()
	}

	// Emit experimental warning for rate-limit feature
	if workflowData.RateLimit != nil {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Using experimental feature: rate-limit"))
		c.IncrementWarningCount()
	}

	// Validate workflow_run triggers have branch restrictions
	log.Printf("Validating workflow_run triggers for branch restrictions")
	if err := c.validateWorkflowRunBranches(workflowData, markdownPath); err != nil {
		return err
	}

	// Validate permissions against GitHub MCP toolsets
	log.Printf("Validating permissions for GitHub MCP toolsets")
	if workflowData.ParsedTools != nil && workflowData.ParsedTools.GitHub != nil {
		// Parse permissions from the workflow data
		// WorkflowData.Permissions contains the raw YAML string (including "permissions:" prefix)
		permissions := NewPermissionsParser(workflowData.Permissions).ToPermissions()

		// Validate permissions using the typed GitHub tool configuration
		validationResult := ValidatePermissions(permissions, workflowData.ParsedTools.GitHub)

		if validationResult.HasValidationIssues {
			// Format the validation message
			message := FormatValidationMessage(validationResult, c.strictMode)

			if len(validationResult.MissingPermissions) > 0 {
				if c.strictMode {
					// In strict mode, missing permissions are errors
					return formatCompilerError(markdownPath, "error", message, nil)
				} else {
					// In non-strict mode, missing permissions are warnings
					fmt.Fprintln(os.Stderr, formatCompilerMessage(markdownPath, "warning", message))
					c.IncrementWarningCount()
				}
			}
		}
	}

	// Emit warning if id-token: write permission is detected
	log.Printf("Checking for id-token: write permission")
	if workflowData.Permissions != "" {
		permissions := NewPermissionsParser(workflowData.Permissions).ToPermissions()
		if permissions != nil {
			level, exists := permissions.Get(PermissionIdToken)
			if exists && level == PermissionWrite {
				warningMsg := `This workflow grants id-token: write permission
OIDC tokens can authenticate to cloud providers (AWS, Azure, GCP).
Ensure proper audience validation and trust policies are configured.`
				fmt.Fprintln(os.Stderr, formatCompilerMessage(markdownPath, "warning", warningMsg))
				c.IncrementWarningCount()
			}
		}
	}

	// Validate GitHub tools against enabled toolsets
	log.Printf("Validating GitHub tools against enabled toolsets")
	if workflowData.ParsedTools != nil && workflowData.ParsedTools.GitHub != nil {
		// Extract allowed tools and enabled toolsets from ParsedTools
		allowedTools := workflowData.ParsedTools.GitHub.Allowed.ToStringSlice()
		enabledToolsets := ParseGitHubToolsets(strings.Join(workflowData.ParsedTools.GitHub.Toolset.ToStringSlice(), ","))

		// Validate that all allowed tools have their toolsets enabled
		if err := ValidateGitHubToolsAgainstToolsets(allowedTools, enabledToolsets); err != nil {
			return formatCompilerError(markdownPath, "error", err.Error(), err)
		}

		// Print informational message if "projects" toolset is explicitly specified
		// (not when implied by "all", as users unlikely intend to use projects with "all")
		originalToolsets := workflowData.ParsedTools.GitHub.Toolset.ToStringSlice()
		for _, toolset := range originalToolsets {
			if toolset == "projects" {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage("The 'projects' toolset requires a GitHub token with organization Projects permissions."))
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage("See: https://github.github.com/gh-aw/reference/tokens/#gh_aw_project_github_token-github-projects-v2"))
				break
			}
		}
	}

	// Validate permissions for agentic-workflows tool
	log.Printf("Validating permissions for agentic-workflows tool")
	if _, hasAgenticWorkflows := workflowData.Tools["agentic-workflows"]; hasAgenticWorkflows {
		// Parse permissions from the workflow data
		permissions := NewPermissionsParser(workflowData.Permissions).ToPermissions()

		// Check if actions: read permission exists
		actionsLevel, hasActions := permissions.Get(PermissionActions)
		if !hasActions || actionsLevel == PermissionNone {
			// Missing actions: read permission
			message := "ERROR: Missing required permission for agentic-workflows tool:\n"
			message += "  - actions: read\n\n"
			message += "The agentic-workflows tool requires actions: read permission to access GitHub Actions data.\n\n"
			message += "Suggested fix: Add the following to your workflow frontmatter:\n"
			message += "permissions:\n"
			message += "  actions: read"

			return formatCompilerError(markdownPath, "error", message, nil)
		}
	}

	// Validate dispatch-workflow configuration (independent of agentic-workflows tool)
	log.Print("Validating dispatch-workflow configuration")
	if err := c.validateDispatchWorkflow(workflowData, markdownPath); err != nil {
		return formatCompilerError(markdownPath, "error", fmt.Sprintf("dispatch-workflow validation failed: %v", err), err)
	}

	return nil
}

// generateAndValidateYAML generates GitHub Actions YAML and validates
// the output size and format.
func (c *Compiler) generateAndValidateYAML(workflowData *WorkflowData, markdownPath string, lockFile string) (string, error) {
	// Generate the YAML content
	yamlContent, err := c.generateYAML(workflowData, markdownPath)
	if err != nil {
		return "", formatCompilerError(markdownPath, "error", fmt.Sprintf("failed to generate YAML: %v", err), err)
	}

	// Always validate expression sizes - this is a hard limit from GitHub Actions (21KB)
	// that cannot be bypassed, so we validate it unconditionally
	log.Print("Validating expression sizes")
	if err := c.validateExpressionSizes(yamlContent); err != nil {
		// Store error first so we can write invalid YAML before returning
		formattedErr := formatCompilerError(markdownPath, "error", fmt.Sprintf("expression size validation failed: %v", err), err)
		// Write the invalid YAML to a .invalid.yml file for inspection
		invalidFile := strings.TrimSuffix(lockFile, ".lock.yml") + ".invalid.yml"
		if writeErr := os.WriteFile(invalidFile, []byte(yamlContent), 0644); writeErr == nil {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Invalid workflow YAML written to: %s", console.ToRelativePath(invalidFile))))
		}
		return "", formattedErr
	}

	// Validate for template injection vulnerabilities - detect unsafe expression usage in run: commands
	log.Print("Validating for template injection vulnerabilities")
	if err := validateNoTemplateInjection(yamlContent); err != nil {
		// Store error first so we can write invalid YAML before returning
		formattedErr := formatCompilerError(markdownPath, "error", err.Error(), err)
		// Write the invalid YAML to a .invalid.yml file for inspection
		invalidFile := strings.TrimSuffix(lockFile, ".lock.yml") + ".invalid.yml"
		if writeErr := os.WriteFile(invalidFile, []byte(yamlContent), 0644); writeErr == nil {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Workflow with template injection risks written to: %s", console.ToRelativePath(invalidFile))))
		}
		return "", formattedErr
	}

	// Validate against GitHub Actions schema (unless skipped)
	if !c.skipValidation {
		log.Print("Validating workflow against GitHub Actions schema")
		if err := c.validateGitHubActionsSchema(yamlContent); err != nil {
			// Store error first so we can write invalid YAML before returning
			formattedErr := formatCompilerError(markdownPath, "error", fmt.Sprintf("workflow schema validation failed: %v", err), err)
			// Write the invalid YAML to a .invalid.yml file for inspection
			invalidFile := strings.TrimSuffix(lockFile, ".lock.yml") + ".invalid.yml"
			if writeErr := os.WriteFile(invalidFile, []byte(yamlContent), 0644); writeErr == nil {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Invalid workflow YAML written to: %s", console.ToRelativePath(invalidFile))))
			}
			return "", formattedErr
		}

		// Validate container images used in MCP configurations
		log.Print("Validating container images")
		if err := c.validateContainerImages(workflowData); err != nil {
			// Treat container image validation failures as warnings, not errors
			// This is because validation may fail due to auth issues locally (e.g., private registries)
			fmt.Fprintln(os.Stderr, formatCompilerMessage(markdownPath, "warning", fmt.Sprintf("container image validation failed: %v", err)))
			c.IncrementWarningCount()
		}

		// Validate runtime packages (npx, uv)
		log.Print("Validating runtime packages")
		if err := c.validateRuntimePackages(workflowData); err != nil {
			return "", formatCompilerError(markdownPath, "error", fmt.Sprintf("runtime package validation failed: %v", err), err)
		}

		// Validate firewall configuration (log-level enum)
		log.Print("Validating firewall configuration")
		if err := c.validateFirewallConfig(workflowData); err != nil {
			return "", formatCompilerError(markdownPath, "error", fmt.Sprintf("firewall configuration validation failed: %v", err), err)
		}

		// Validate repository features (discussions, issues)
		log.Print("Validating repository features")
		if err := c.validateRepositoryFeatures(workflowData); err != nil {
			return "", formatCompilerError(markdownPath, "error", fmt.Sprintf("repository feature validation failed: %v", err), err)
		}
	} else if c.verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Schema validation available but skipped (use SetSkipValidation(false) to enable)"))
		c.IncrementWarningCount()
	}

	return yamlContent, nil
}

// writeWorkflowOutput writes the compiled workflow to the lock file
// and handles console output formatting.
func (c *Compiler) writeWorkflowOutput(lockFile, yamlContent string, markdownPath string) error {
	// Write to lock file (unless noEmit is enabled)
	if c.noEmit {
		log.Print("Validation completed - no lock file generated (--no-emit enabled)")
	} else {
		log.Printf("Writing output to: %s", lockFile)

		// Check if content has actually changed
		contentUnchanged := false
		if existingContent, err := os.ReadFile(lockFile); err == nil {
			if string(existingContent) == yamlContent {
				// Content is identical - skip write to preserve timestamp
				contentUnchanged = true
				log.Print("Lock file content unchanged - skipping write to preserve timestamp")
			}
		}

		// Only write if content has changed
		if !contentUnchanged {
			if err := os.WriteFile(lockFile, []byte(yamlContent), 0644); err != nil {
				return formatCompilerError(lockFile, "error", fmt.Sprintf("failed to write lock file: %v", err), err)
			}
			log.Print("Lock file written successfully")
		}

		// Validate file size after writing
		if lockFileInfo, err := os.Stat(lockFile); err == nil {
			if lockFileInfo.Size() > MaxLockFileSize {
				lockSize := console.FormatFileSize(lockFileInfo.Size())
				maxSize := console.FormatFileSize(MaxLockFileSize)
				warningMsg := fmt.Sprintf("Generated lock file size (%s) exceeds recommended maximum size (%s)", lockSize, maxSize)
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(warningMsg))
			}
		}
	}

	// Display success message with file size if we generated a lock file (unless quiet mode)
	if !c.quiet {
		if c.noEmit {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(console.ToRelativePath(markdownPath)))
		} else {
			// Get the size of the generated lock file for display
			if lockFileInfo, err := os.Stat(lockFile); err == nil {
				lockSize := console.FormatFileSize(lockFileInfo.Size())
				fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("%s (%s)", console.ToRelativePath(markdownPath), lockSize)))
			} else {
				// Fallback to original display if we can't get file info
				fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(console.ToRelativePath(markdownPath)))
			}
		}
	}
	return nil
}

// CompileWorkflowData compiles pre-parsed workflow content into GitHub Actions YAML.
// Unlike CompileWorkflow, this accepts already-parsed frontmatter and markdown content
// rather than reading from disk. This is useful for testing and programmatic workflow generation.
//
// The compilation process includes:
//   - Validating workflow configuration and features
//   - Checking permissions and tool configurations
//   - Generating GitHub Actions YAML structure
//   - Writing the compiled workflow to a .lock.yml file
//
// This function avoids re-parsing when workflow data has already been extracted,
// making it efficient for scenarios where the same workflow is compiled multiple times
// or when workflow data comes from a non-file source.
func (c *Compiler) CompileWorkflowData(workflowData *WorkflowData, markdownPath string) error {
	// Store markdownPath for use in dynamic tool generation and prompt generation
	c.markdownPath = markdownPath

	// Track compilation time for performance monitoring
	startTime := time.Now()
	defer func() {
		if log.Enabled() {
			log.Printf("Compilation completed in %v", time.Since(startTime))
		}
	}()

	// Reset the step order tracker for this compilation
	c.stepOrderTracker = NewStepOrderTracker()

	// Reset schedule friendly formats for this compilation
	c.scheduleFriendlyFormats = nil

	// Reset the artifact manager for this compilation
	if c.artifactManager == nil {
		c.artifactManager = NewArtifactManager()
	} else {
		c.artifactManager.Reset()
	}

	// Generate lock file name
	lockFile := stringutil.MarkdownToLockFile(markdownPath)

	// Sanitize the lock file path to prevent path traversal attacks
	lockFile = filepath.Clean(lockFile)

	log.Printf("Starting compilation: %s -> %s", markdownPath, lockFile)

	// Validate workflow data
	if err := c.validateWorkflowData(workflowData, markdownPath); err != nil {
		return err
	}

	// Note: Markdown content size is now handled by splitting into multiple steps in generatePrompt
	log.Printf("Workflow: %s, Tools: %d", workflowData.Name, len(workflowData.Tools))

	// Note: compute-text functionality is now inlined directly in the task job
	// instead of using a shared action file

	// Generate and validate YAML
	yamlContent, err := c.generateAndValidateYAML(workflowData, markdownPath, lockFile)
	if err != nil {
		return err
	}

	// Write output
	return c.writeWorkflowOutput(lockFile, yamlContent, markdownPath)
}

// ParseWorkflowFile parses a markdown workflow file and extracts all necessary data

// extractTopLevelYAMLSection extracts a top-level YAML section from the frontmatter map
// This ensures we only extract keys at the root level, avoiding nested keys with the same name
// parseOnSection parses the "on" section from frontmatter to extract command triggers, reactions, and other events

// generateYAML generates the complete GitHub Actions YAML content

// isActivationJobNeeded determines if the activation job is required
// generateMainJobSteps generates the steps section for the main job

// The original JavaScript code will use the pattern as-is with "g" flags

// validateMarkdownSizeForGitHubActions is no longer used - content is now split into multiple steps
// to handle GitHub Actions script size limits automatically
// func (c *Compiler) validateMarkdownSizeForGitHubActions(content string) error { ... }

// splitContentIntoChunks splits markdown content into chunks that fit within GitHub Actions script size limits

// generatePostSteps generates the post-steps section that runs after AI execution

// convertStepToYAML converts a step map to YAML string with proper indentation

// generateEngineExecutionSteps uses the new GetExecutionSteps interface method

// generateAgentVersionCapture generates a step that captures the agent version if the engine supports it

// generateCreateAwInfo generates a step that creates aw_info.json with agentic run metadata

// generateOutputCollectionStep generates a step that reads the output file and sets it as a GitHub Actions output
