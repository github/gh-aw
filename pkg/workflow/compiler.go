package workflow

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/constants"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/goccy/go-yaml"
)

var workflowLog = logger.New("workflow:compiler")

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

	// missingPermissionsDefaultToolsetWarning explains why strict mode was downgraded to warning.
	missingPermissionsDefaultToolsetWarning = "Some of the GitHub tools will not be available until the missing permissions are granted."
)

//go:embed schemas/github-workflow.json
var githubWorkflowSchema string

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
	workflowLog.Printf("Parsing workflow file")
	workflowData, err := c.ParseWorkflowFile(markdownPath)
	if err != nil {
		// ParseWorkflowFile already returns formatted compiler errors; pass them through.
		if isFormattedCompilerError(err) {
			return err
		}

		// Fallback for any unformatted error that slipped through.
		return formatCompilerError(markdownPath, "error", err.Error(), err)
	}

	return c.CompileWorkflowData(workflowData, markdownPath)
}

func (c *Compiler) configureGHESCompatibility() {
	if c.ghesCompatConfigured {
		return
	}
	c.ghesCompatConfigured = true
	c.ghesArtifactCompat = c.ghesCompatFromCLI
	if !c.ghesArtifactCompat {
		if repoConfig, err := c.loadRepoConfig(); err == nil && repoConfig != nil {
			c.ghesArtifactCompat = repoConfig.GHES
		}
	}
}

// validateWorkflowData orchestrates all validation of workflow configuration by
// delegating to four focused validators. Each validator is independently testable
// and covers a distinct concern:
//
//   - validateExpressions: expression safety and runtime-import file checks
//   - validateFeatureConfig: feature flags and action-mode override
//   - validatePermissions: permissions parsing, MCP tool constraints, workflow_run security
//   - validateToolConfiguration: safe-outputs, GitHub tools, dispatches, and resources
func (c *Compiler) validateWorkflowData(workflowData *WorkflowData, markdownPath string) error {
	if err := c.prepareOperationalValueGrader(workflowData, markdownPath); err != nil {
		return formatCompilerError(markdownPath, "error", err.Error(), err)
	}

	if err := validateRunnerConfig(workflowData.RunnerConfig); err != nil {
		return formatCompilerError(markdownPath, "error", err.Error(), err)
	}

	if err := validateArcDindRootless(workflowData); err != nil {
		return formatCompilerError(markdownPath, "error", err.Error(), err)
	}

	if err := c.validateExpressions(workflowData, markdownPath); err != nil {
		return err
	}

	if err := c.validateFeatureConfig(workflowData, markdownPath); err != nil {
		return err
	}

	workflowPermissions, err := c.validatePermissions(workflowData, markdownPath)
	if err != nil {
		return err
	}

	return c.validateToolConfiguration(workflowData, markdownPath, workflowPermissions)
}

// shouldDowngradeDefaultToolsetPermissionError returns true when strict-mode
// permission errors should be downgraded because the GitHub tool uses only the
// default toolset, either explicitly ([default]) or implicitly (no toolsets configured).
func shouldDowngradeDefaultToolsetPermissionError(githubTool *GitHubToolConfig) bool {
	if githubTool == nil {
		return false
	}

	if len(githubTool.Toolset) == 0 {
		return true
	}

	return len(githubTool.Toolset) == 1 && githubTool.Toolset[0] == GitHubToolset("default")
}

// generateAndValidateYAML generates GitHub Actions YAML and validates
// the output size and format.
func (c *Compiler) generateAndValidateYAML(workflowData *WorkflowData, markdownPath string, lockFile string) (string, []string, []string, error) { //nolint:largefunc // Existing YAML generation and validation remains centralized.
	// Generate the YAML content along with the collected body secrets and action refs
	// (returned to avoid a second scan of the full YAML in the caller for safe update enforcement).
	yamlContent, bodySecrets, bodyActions, err := c.generateYAML(workflowData, markdownPath)
	if err != nil {
		return "", nil, nil, formatCompilerError(markdownPath, "error", fmt.Sprintf("failed to generate YAML: %v", err), err)
	}

	// Always validate expression sizes - this is a hard limit from GitHub Actions (21KB)
	// that cannot be bypassed, so we validate it unconditionally
	workflowLog.Print("Validating expression sizes")
	if err := c.validateExpressionSizes(yamlContent); err != nil {
		// Store error first so we can write invalid YAML before returning
		formattedErr := formatCompilerError(markdownPath, "error", fmt.Sprintf("expression size validation failed: %v", err), err)
		// Write the invalid YAML to a .invalid.yml file for inspection
		invalidFile := strings.TrimSuffix(lockFile, ".lock.yml") + ".invalid.yml"
		if writeErr := os.WriteFile(invalidFile, []byte(yamlContent), constants.FilePermPublic); writeErr == nil {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr("Invalid workflow YAML written to: "+console.ToRelativePath(invalidFile)))
		}
		return "", nil, nil, formattedErr
	}

	// Template injection validation and GitHub Actions schema validation both require a
	// parsed representation of the compiled YAML.  Parse it once here and share the
	// result between the two validators to avoid redundant yaml.Unmarshal calls.
	//
	// Performance note: when schema validation is enabled (needsSchemaCheck=true) the
	// YAML is parsed regardless.  The text scan in validateTemplateInjection is only
	// used when schema validation is disabled (skipValidation=true), where targeted
	// fast-path checks avoid an unnecessary yaml.Unmarshal.
	needsSchemaCheck := !c.skipValidation

	var parsedWorkflow map[string]any
	if needsSchemaCheck {
		// Schema validation requires parsed YAML; parse once and share with the
		// template injection validator below.
		workflowLog.Print("Parsing compiled YAML for validation")
		if parseErr := yaml.Unmarshal([]byte(yamlContent), &parsedWorkflow); parseErr != nil {
			// If parsing fails here the subsequent validators would also fail; keep going
			// so we surface the root error from the right validator.
			parsedWorkflow = nil
		}
	}

	// Validate for template injection vulnerabilities (unsafe expression usage in run: commands).
	//
	// parsedWorkflow != nil means the YAML was already parsed for schema validation;
	// validateTemplateInjection reuses the pre-parsed tree (inspects only run: block values)
	// rather than re-scanning the full YAML string.  When parsedWorkflow is nil (schema
	// validation disabled), the validator uses targeted text scans to avoid an unnecessary
	// yaml.Unmarshal when all run-block expressions are in the compiler-owned allowed list.
	if err := c.validateTemplateInjection(yamlContent, lockFile, markdownPath, parsedWorkflow); err != nil {
		var suppressions []ThreatDetectionSuppression
		if workflowData.ParsedFrontmatter != nil {
			suppressions = workflowData.ParsedFrontmatter.ThreatDetectionSuppressions
		}
		if !isThreatDetectionDiagnosticSuppressed(err, suppressions, time.Now()) {
			return "", nil, nil, err
		}
		templateInjectionValidationLog.Print("Skipping CTR-006 diagnostic due to active threat-detection-suppress entry")
	}

	// Validate against GitHub Actions schema (unless skipped)
	if needsSchemaCheck {
		workflowLog.Print("Validating workflow against GitHub Actions schema")
		var schemaErr error
		if parsedWorkflow != nil {
			schemaErr = c.validateGitHubActionsSchemaFromParsed(parsedWorkflow)
		} else {
			schemaErr = c.validateGitHubActionsSchema(yamlContent)
		}
		if schemaErr != nil {
			// Try to point at the exact line of the failing field in the source markdown.
			// extractSchemaErrorField unwraps the error chain to find the top-level field
			// name (e.g. "timeout-minutes"), which findFrontmatterFieldLine then locates in
			// the source frontmatter so the error is IDE-navigable.
			fieldLine := 1
			if fieldName := extractSchemaErrorField(schemaErr); fieldName != "" {
				frontmatterLines := strings.Split(workflowData.FrontmatterYAML, "\n")
				if line := findFrontmatterFieldLine(frontmatterLines, 2, fieldName); line > 0 {
					fieldLine = line
				}
			}
			// Store error first so we can write invalid YAML before returning
			formattedErr := formatCompilerErrorWithPosition(markdownPath, fieldLine, 1, "error",
				fmt.Sprintf("invalid workflow: %v", schemaErr), schemaErr)
			// Write the invalid YAML to a .invalid.yml file for inspection
			invalidFile := strings.TrimSuffix(lockFile, ".lock.yml") + ".invalid.yml"
			if writeErr := os.WriteFile(invalidFile, []byte(yamlContent), constants.FilePermPublic); writeErr == nil {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr("Invalid workflow YAML written to: "+console.ToRelativePath(invalidFile)))
			}
			return "", nil, nil, formattedErr
		}

		// Validate container images used in MCP configurations
		workflowLog.Print("Validating container images")
		if err := c.validateContainerImages(workflowData); err != nil {
			// Treat container image validation failures as warnings, not errors
			// This is because validation may fail due to auth issues locally (e.g., private registries)
			fmt.Fprintln(os.Stderr, formatCompilerMessage(markdownPath, "warning", fmt.Sprintf("container image validation failed: %v", err)))
			c.IncrementWarningCount()
		}

		// Validate runtime packages (npx, uv)
		workflowLog.Print("Validating runtime packages")
		if err := c.validateRuntimePackages(workflowData); err != nil {
			return "", nil, nil, formatCompilerError(markdownPath, "error", fmt.Sprintf("runtime package validation failed: %v", err), err)
		}

		// Validate firewall configuration (log-level enum)
		workflowLog.Print("Validating firewall configuration")
		if err := c.validateFirewallConfig(workflowData); err != nil {
			return "", nil, nil, formatCompilerError(markdownPath, "error", fmt.Sprintf("firewall configuration validation failed: %v", err), err)
		}

		// Validate repository features (discussions, issues)
		workflowLog.Print("Validating repository features")
		if err := c.validateRepositoryFeatures(workflowData); err != nil {
			return "", nil, nil, formatCompilerError(markdownPath, "error", fmt.Sprintf("repository feature validation failed: %v", err), err)
		}
	} else if c.verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr("Schema validation available but skipped (use SetSkipValidation(false) to enable)"))
		c.IncrementWarningCount()
	}

	return yamlContent, bodySecrets, bodyActions, nil
}

// writeWorkflowOutput writes the compiled workflow to the lock file
// and handles console output formatting.
func (c *Compiler) writeWorkflowOutput(lockFile, yamlContent string, markdownPath string) error {
	// Write to lock file (unless noEmit is enabled)
	if c.noEmit {
		workflowLog.Print("Validation completed - no lock file generated (--no-emit enabled)")
	} else {
		workflowLog.Printf("Writing output to: %s", lockFile)

		// Check if content has actually changed
		contentUnchanged := false
		if existingContent, err := os.ReadFile(lockFile); err == nil {
			if normalizeHeredocDelimiters(string(existingContent)) == normalizeHeredocDelimiters(yamlContent) {
				// Content is identical (modulo random heredoc tokens) - skip write to preserve timestamp
				contentUnchanged = true
				workflowLog.Print("Lock file content unchanged - skipping write to preserve timestamp")
			}
		}

		// Only write if content has changed
		if !contentUnchanged {
			if err := os.WriteFile(lockFile, []byte(yamlContent), constants.FilePermPublic); err != nil {
				return formatCompilerError(lockFile, "error", fmt.Sprintf("failed to write lock file: %v", err), err)
			}
			workflowLog.Print("Lock file written successfully")
		}

		// Validate file size after writing
		if lockFileInfo, err := os.Stat(lockFile); err == nil {
			if lockFileInfo.Size() > MaxLockFileSize {
				lockSize := console.FormatFileSize(lockFileInfo.Size())
				maxSize := console.FormatFileSize(MaxLockFileSize)
				warningMsg := fmt.Sprintf("Generated lock file size (%s) exceeds recommended maximum size (%s)", lockSize, maxSize)
				fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(warningMsg))
			}
		}
	}

	// Display success message with file size if we generated a lock file (unless quiet mode)
	if !c.quiet {
		if c.noEmit {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessageStderr(console.ToRelativePath(markdownPath)))
		} else {
			// Get the size of the generated lock file for display
			if lockFileInfo, err := os.Stat(lockFile); err == nil {
				lockSize := console.FormatFileSize(lockFileInfo.Size())
				fmt.Fprintln(os.Stderr, console.FormatSuccessMessageStderr(fmt.Sprintf("%s (%s)", console.ToRelativePath(markdownPath), lockSize)))
			} else {
				// Fallback to original display if we can't get file info
				fmt.Fprintln(os.Stderr, console.FormatSuccessMessageStderr(console.ToRelativePath(markdownPath)))
			}
		}
	}
	return nil
}

// validateTemplateInjection checks compiled YAML for template injection vulnerabilities
// (unsafe GitHub Actions expressions used directly in run: blocks).
//
// When parsedWorkflow is non-nil the YAML was already parsed for schema validation;
// this function reuses it directly by walking the run: block values in the pre-parsed
// tree, which is faster than re-scanning the full YAML string with a regex.
//
// When parsedWorkflow is nil (schema validation disabled via skipValidation), this
// function uses two targeted text scans instead of a broad any-expression check:
//
//  1. hasExpressionInRunContent(UnsafeContextPattern) — detects user-controlled
//     contexts (github.event.*, steps.*.outputs.*, inputs.*) that represent genuine
//     template-injection risks.
//
//  2. hasNonAllowedExpressionInRunContent — detects compiler-regression cases where
//     a ${{ ... }} expression that is NOT in the compiler-owned allow-list slipped
//     into a run: block.
//
// For well-formed compiled workflows (the common case) both scans return false, so
// no yaml.Unmarshal is needed at all.  A yaml.Unmarshal is only performed when at
// least one scan detects a potential issue.
func (c *Compiler) validateTemplateInjection(yamlContent, lockFile, markdownPath string, parsedWorkflow map[string]any) error {
	var templateErr error

	if parsedWorkflow != nil {
		// Path A: YAML was already parsed for schema validation; reuse it.
		// Walking the pre-parsed tree (run: block values only) is faster than
		// scanning the full YAML string.
		workflowLog.Print("Validating for template injection vulnerabilities")
		templateErr = validateNoTemplateInjectionFromParsed(parsedWorkflow)
		if templateErr == nil {
			templateErr = validateNoGitHubExpressionsInRunScriptsFromParsed(parsedWorkflow)
		}
	} else {
		// Path B: schema validation is disabled (parsedWorkflow is nil).
		//
		// Use two targeted text scans with a shared run-block walker so we can skip the
		// expensive yaml.Unmarshal when all run-block expressions are compiler-owned safe
		// references (e.g. ${{ runner.temp }}, ${{ env.FOO }}).
		//
		// needsUnsafeCheck:    true when user-controlled contexts appear in run: blocks
		//                      → triggers validateNoTemplateInjectionFromParsed
		// needsDisallowedCheck: true when any expression outside the compiler allow-list
		//                      appears in run: blocks
		//                      → triggers validateNoGitHubExpressionsInRunScriptsFromParsed
		scan := scanRunContentExpressions(yamlContent)
		needsUnsafeCheck := scan.hasUnsafe
		needsDisallowedCheck := scan.hasDisallowed

		if needsUnsafeCheck || needsDisallowedCheck {
			workflowLog.Print("Validating for template injection vulnerabilities")
			var reparsed map[string]any
			if err := yaml.Unmarshal([]byte(yamlContent), &reparsed); err != nil {
				// Malformed YAML: skip validation (compilation would have surfaced this elsewhere).
				templateInjectionValidationLog.Printf("Failed to parse YAML for template injection check: %v", err)
				reparsed = nil
			}
			if reparsed != nil {
				// validateNoTemplateInjectionFromParsed only catches user-controlled contexts,
				// so it is intentionally skipped when the UnsafeContextPattern scan found none.
				if needsUnsafeCheck {
					templateErr = validateNoTemplateInjectionFromParsed(reparsed)
				}
				if templateErr == nil && needsDisallowedCheck {
					templateErr = validateNoGitHubExpressionsInRunScriptsFromParsed(reparsed)
				}
			}
		}
	}

	if templateErr != nil {
		diagnosticErr := &threatDetectionDiagnosticError{Rule: "CTR-006", Err: templateErr}
		// Store error first so we can write invalid YAML before returning
		formattedErr := formatCompilerError(markdownPath, "error", diagnosticErr.Error(), diagnosticErr)
		// Write the invalid YAML to a .invalid.yml file for inspection
		invalidFile := strings.TrimSuffix(lockFile, ".lock.yml") + ".invalid.yml"
		if writeErr := os.WriteFile(invalidFile, []byte(yamlContent), constants.FilePermPublic); writeErr == nil {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr("Workflow with template injection risks written to: "+console.ToRelativePath(invalidFile)))
		}
		return formattedErr
	}
	return nil
}

// readLockFileFromHEAD reads a lock file from git HEAD using the compiler's cached
// git root directory, avoiding the overhead of spawning a subprocess to re-discover
// the repository root on every call.
func (c *Compiler) readLockFileFromHEAD(lockFile string) (string, error) {
	if c.gitRoot == "" {
		return "", errors.New("git root not available (not in a git repository or git not installed)")
	}
	return gitutil.ReadFileFromHEAD(lockFile, c.gitRoot)
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
func (c *Compiler) CompileWorkflowData(workflowData *WorkflowData, markdownPath string) error { //nolint:largefunc // Existing compilation lifecycle remains centralized.
	// Store markdownPath for use in dynamic tool generation and prompt generation
	c.markdownPath = markdownPath

	// Track compilation time for performance monitoring
	startTime := time.Now()
	defer func() {
		workflowLog.Printf("Compilation completed in %v", time.Since(startTime))
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

	// Enable GHES artifact compatibility from CLI flag or aw.json (CLI flag wins).
	c.configureGHESCompatibility()
	if c.ghesArtifactCompat {
		actionPinsLog.Print("GHES compatibility mode enabled: artifact actions will use v3-compatible pins")
	}
	workflowData.GHES = c.ghesArtifactCompat

	// Generate lock file name
	lockFile := stringutil.MarkdownToLockFile(markdownPath)

	// Sanitize the lock file path to prevent path traversal attacks
	lockFile = filepath.Clean(lockFile)

	workflowLog.Printf("Starting compilation: %s -> %s", markdownPath, lockFile)

	// CompileWorkflowData is also a public entry point, so enforce the
	// strict-mode sandbox restriction here rather than relying on the
	// orchestrator path to validate it.
	if err := c.withEffectiveStrictMode(workflowData.RawFrontmatter, func() error {
		return c.validateStrictFirewall("", nil, workflowData.SandboxConfig)
	}); err != nil {
		return formatCompilerError(markdownPath, "error", err.Error(), err)
	}

	// Resolve and cache the baseline manifest only when safe update mode is active.
	// This avoids unnecessary git/filesystem reads on compile paths that skip safe update
	// enforcement (e.g., --approve or strict: false).
	safeUpdateEnabled := c.effectiveSafeUpdate(workflowData)
	var oldManifest *GHAWManifest
	var oldHasPR bool
	var oldHasPRTarget bool
	if safeUpdateEnabled {
		// Read the existing lock file to extract the previous gh-aw-manifest for safe update
		// enforcement.
		//
		// Priority (highest to lowest):
		//  1. Pre-cached manifest supplied by the caller (e.g. MCP server collected at startup
		//     before any agent interaction, making it tamper-proof without requiring git access).
		//  2. Content from the last git commit (HEAD) – prevents a local agent from modifying
		//     the .lock.yml file on disk to forge an approved manifest.
		//  3. Filesystem read – fallback for first-time compilations or non-git environments.
		if cached, ok := c.priorManifests[lockFile]; ok {
			oldManifest = cached
			if committedContent, readErr := c.readLockFileFromHEAD(lockFile); readErr == nil {
				oldHasPR, oldHasPRTarget = extractPullRequestEventPresenceFromCompiledWorkflow(committedContent)
			}
			secretCount := 0
			if cached != nil {
				secretCount = len(cached.Secrets)
			}
			workflowLog.Printf("Using pre-cached gh-aw-manifest for %s: %d secret(s)", lockFile, secretCount)
		} else if committedContent, readErr := c.readLockFileFromHEAD(lockFile); readErr == nil {
			oldHasPR, oldHasPRTarget = extractPullRequestEventPresenceFromCompiledWorkflow(committedContent)
			if m, parseErr := ExtractGHAWManifestFromLockFile(committedContent); parseErr == nil {
				oldManifest = m
				if oldManifest != nil {
					workflowLog.Printf("Loaded committed gh-aw-manifest from HEAD: %d secret(s)", len(oldManifest.Secrets))
				}
			} else {
				workflowLog.Printf("Failed to parse committed gh-aw-manifest: %v. Safe update enforcement will proceed without baseline comparison (all secrets will be considered new).", parseErr)
			}
		} else {
			workflowLog.Printf("Lock file %s not found in HEAD commit (%v); falling back to filesystem read.", lockFile, readErr)
			if existingContent, fsErr := os.ReadFile(lockFile); fsErr == nil {
				oldHasPR, oldHasPRTarget = extractPullRequestEventPresenceFromCompiledWorkflow(string(existingContent))
				if m, parseErr := ExtractGHAWManifestFromLockFile(string(existingContent)); parseErr == nil {
					oldManifest = m
					if oldManifest != nil {
						workflowLog.Printf("Loaded gh-aw-manifest from filesystem: %d secret(s)", len(oldManifest.Secrets))
					}
				} else {
					workflowLog.Printf("Failed to parse filesystem gh-aw-manifest: %v. Safe update enforcement will treat as empty manifest.", parseErr)
				}
			} else {
				// No lock file anywhere — this is a brand-new workflow.  Use an empty
				// (non-nil) manifest so EnforceSafeUpdate applies enforcement and flags
				// any newly introduced secrets or actions for review.
				workflowLog.Printf("Lock file %s not found (new workflow). Safe update enforcement will use an empty baseline.", lockFile)
				oldManifest = &GHAWManifest{Version: currentGHAWManifestVersion}
			}
		}
		// Keep the first non-nil baseline seen by this compiler instance.
		// This intentionally does not overwrite an existing cache entry so repeated
		// compiles in the same process continue to compare against the same trusted
		// baseline rather than a just-generated local lock file.
		// Nil baselines (e.g., legacy lock files without gh-aw-manifest) are not
		// cached so future compiles can pick up a newly available manifest.
		if oldManifest != nil {
			if _, ok := c.priorManifests[lockFile]; !ok {
				c.priorManifests[lockFile] = oldManifest
			}
		}
	}

	// Validate workflow data
	if err := c.validateWorkflowData(workflowData, markdownPath); err != nil {
		// validateWorkflowData always returns formatCompilerError results; pass through directly.
		// If an unformatted error somehow slips through, wrap it with compiler context.
		if isFormattedCompilerError(err) {
			return err
		}
		return formatCompilerError(markdownPath, "error", "workflow validation: "+err.Error(), err)
	}

	// Note: Markdown content size is now handled by splitting into multiple steps in generatePrompt
	workflowLog.Printf("Workflow: %s, Tools: %d", workflowData.Name, len(workflowData.Tools))

	// Note: compute-text functionality is now inlined directly in the task job
	// instead of using a shared action file

	// Generate and validate YAML (also embeds the new gh-aw-manifest in the header).
	// Returns the collected body secrets and action refs to avoid duplicate scans for
	// safe update enforcement.
	yamlContent, bodySecrets, bodyActions, err := c.generateAndValidateYAML(workflowData, markdownPath, lockFile)
	if err != nil {
		// generateAndValidateYAML always returns formatCompilerError results; pass through directly.
		// If an unformatted error somehow slips through, wrap it with compiler context.
		if isFormattedCompilerError(err) {
			return err
		}
		return formatCompilerError(markdownPath, "error", "YAML generation: "+err.Error(), err)
	}

	// Enforce safe update mode: emit a warning prompt (not a hard error) when unapproved
	// secrets or action changes are detected.  body* vars contain data collected from the
	// workflow body only (not the header) to avoid matching the gh-aw-manifest JSON comment.
	//
	// Emitting a warning instead of failing allows compilation to succeed so that the lock
	// file is written and the agent receives the actionable guidance embedded in the warning.
	if safeUpdateEnabled {
		currentHasPR, currentHasPRTarget := extractPullRequestEventPresenceFromOnField(workflowData.RawFrontmatter["on"])
		prTransition := PullRequestEventTransition{
			OldHasPullRequest:           oldHasPR,
			OldHasPullRequestTarget:     oldHasPRTarget,
			CurrentHasPullRequest:       currentHasPR,
			CurrentHasPullRequestTarget: currentHasPRTarget,
		}
		if enforceErr := EnforceSafeUpdate(SafeUpdateOptions{
			Manifest:                oldManifest,
			SecretNames:             bodySecrets,
			ActionRefs:              bodyActions,
			CurrentRedirect:         workflowData.Redirect,
			PullRequestTransition:   prTransition,
			MemoryValidationScripts: collectMemoryValidationScripts(workflowData),
		}); enforceErr != nil {
			warningMsg := buildSafeUpdateWarningPrompt(enforceErr.Error())
			c.AddSafeUpdateWarning(warningMsg)
			fmt.Fprintln(os.Stderr, formatCompilerMessage(markdownPath, "warning", enforceErr.Error()))
			c.IncrementWarningCount()
		}
	}

	// Mark compiler-generated actions as used to prevent pruning.
	// This handles actions that are hardcoded in code generators (cache.go,
	// checkout_step_generator.go, etc.) rather than resolved from markdown workflows.
	if resolver := c.GetSharedActionResolver(); resolver != nil {
		resolver.MarkCompilerGeneratedActionsAsUsed()
	}

	// Write output
	if err := c.writeWorkflowOutput(lockFile, yamlContent, markdownPath); err != nil {
		return err
	}

	return nil
}
