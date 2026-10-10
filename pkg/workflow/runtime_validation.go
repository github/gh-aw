// This file provides runtime validation for packages, containers, and expressions.
//
// # Runtime Validation
//
// This file validates runtime dependencies and configuration for agentic workflows.
// It ensures that:
//   - Container images exist and are accessible
//   - Runtime packages (npm, pip, uv) are available
//   - Expression sizes don't exceed GitHub Actions limits
//
// # Validation Functions
//
//   - validateExpressionSizes() - Validates expression size limits (21KB max)
//   - validateContainerImages() - Validates Docker images exist
//   - validateRuntimePackages() - Validates npm, pip, uv packages
//   - collectPackagesFromWorkflow() - Generic package collection helper
//
// # Validation Patterns
//
// This file uses several patterns:
//   - External resource validation: Docker images, npm/pip packages
//   - Size limit validation: Expression sizes, file sizes
//   - Collection and deduplication: Package extraction
//
// # Size Limits
//
// GitHub Actions has a 21KB limit for expression values including environment variables.
// This validation prevents compilation of workflows that will fail at runtime.
//
// # When to Add Validation Here
//
// Add validation to this file when:
//   - It validates runtime dependencies (packages, containers)
//   - It checks expression or content size limits
//   - It requires external resource checking
//
// For general validation, see validation.go.
// For detailed documentation, see scratchpad/validation-architecture.md

package workflow

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/goccy/go-yaml"
)

var runtimeValidationLog = logger.New("workflow:runtime_validation")

// validateExpressionSizes validates that no expression values in the generated YAML exceed GitHub Actions limits.
//
// GitHub Actions enforces a 21,000-character limit on YAML string values that contain
// template expressions (${{ }}). This check covers two cases:
//
//  1. Single-line values: each YAML line is checked individually.
//
//  2. Multi-line block scalars: YAML literal-block (|) and folded-block (>) scalars span
//     many lines but are parsed by GitHub Actions as a single string value.  When such a
//     block contains at least one ${{ }} expression AND its total length exceeds 21,000
//     characters, GitHub Actions rejects the workflow with "Exceeded max expression length".
func (c *Compiler) validateExpressionSizes(yamlContent string) error {
	lines := strings.Split(yamlContent, "\n")
	runtimeValidationLog.Printf("Validating expression sizes: yaml_lines=%d, max_size=%d", len(lines), MaxExpressionSize)
	maxSize := MaxExpressionSize

	for lineNum, line := range lines {
		// Check the line length (actual content that will be in the YAML)
		if len(line) > maxSize {
			// Extract the key/value for better error message
			trimmed := strings.TrimSpace(line)
			key := ""
			if colonIdx := strings.Index(trimmed, ":"); colonIdx > 0 {
				key = strings.TrimSpace(trimmed[:colonIdx])
			}

			// Format sizes for display
			actualSize := console.FormatFileSize(int64(len(line)))
			maxSizeFormatted := console.FormatFileSize(int64(maxSize))

			var errorMsg string
			if key != "" {
				errorMsg = fmt.Sprintf("expression value for %q (%s) exceeds maximum allowed size (%s) at line %d. GitHub Actions has a 21KB limit for expression values including environment variables. Consider chunking the content or using artifacts instead.",
					key, actualSize, maxSizeFormatted, lineNum+1)
			} else {
				errorMsg = fmt.Sprintf("line %d (%s) exceeds maximum allowed expression size (%s). GitHub Actions has a 21KB limit for expression values.",
					lineNum+1, actualSize, maxSizeFormatted)
			}

			return errors.New(errorMsg)
		}
	}

	// Check multi-line YAML block scalars that contain template expressions.
	// A run: | or any other block-scalar value is a single string from GitHub Actions'
	// perspective; if it contains ${{ }} AND is longer than 21,000 characters the
	// runner rejects it with "Exceeded max expression length".
	if err := validateBlockScalarExpressionSizes(lines, maxSize); err != nil {
		return err
	}

	return nil
}

// validateBlockScalarExpressionSizes scans the YAML lines for multi-line block scalars
// (literal | or folded >) and returns an error when any such block both contains a
// GitHub Actions template expression (${{ }}) and exceeds maxSize bytes in total.
func validateBlockScalarExpressionSizes(lines []string, maxSize int) error {
	// Track whether we are inside a block scalar and its metadata.
	inBlock := false
	blockKey := ""
	blockStartLine := 0
	blockIndent := -1
	blockSize := 0
	blockHasExpression := false

	checkBlock := func() error {
		if inBlock && blockHasExpression && blockSize > maxSize {
			actualSize := console.FormatFileSize(int64(blockSize))
			maxSizeFormatted := console.FormatFileSize(int64(maxSize))
			return fmt.Errorf("expression value for %q (%s) exceeds maximum allowed size (%s) starting at line %d. "+
				"GitHub Actions has a 21KB limit for YAML values that contain template expressions (${{ }}). "+
				"Split the step into separate run: blocks so that no single block containing ${{ }} expressions exceeds the limit",
				blockKey, actualSize, maxSizeFormatted, blockStartLine+1)
		}
		return nil
	}

	for i, line := range lines {
		// Count leading spaces to determine indentation level.
		trimmed := strings.TrimLeft(line, " \t")
		indent := len(line) - len(trimmed)

		if inBlock {
			// An empty line is part of the block (blank lines are allowed inside block scalars).
			if strings.TrimSpace(line) == "" {
				blockSize += len(line) + 1 // +1 for the newline
				continue
			}
			// A line whose indentation is greater than the block header's indentation
			// is still inside the block scalar.
			if indent > blockIndent {
				blockSize += len(line) + 1
				if strings.Contains(line, "${{") {
					blockHasExpression = true
				}
				continue
			}
			// Indentation dropped back – the block has ended.
			if err := checkBlock(); err != nil {
				return err
			}
			inBlock = false
			blockKey = ""
			blockSize = 0
			blockHasExpression = false
			blockIndent = -1
		}

		// Detect the start of a block scalar: a YAML key whose value is | or >
		// e.g. "        run: |" or "    script: >"
		if colonIdx := strings.Index(trimmed, ":"); colonIdx > 0 {
			afterColon := strings.TrimSpace(trimmed[colonIdx+1:])
			if afterColon == "|" || afterColon == ">" || strings.HasPrefix(afterColon, "|-") || strings.HasPrefix(afterColon, ">-") {
				inBlock = true
				blockKey = strings.TrimSpace(trimmed[:colonIdx])
				blockStartLine = i
				blockIndent = indent
				blockSize = 0
				blockHasExpression = false
			}
		}
	}

	// Check any block that was still open when the file ended.
	return checkBlock()
}

// validateContainerImages validates that container images specified in MCP configs exist and are accessible
func (c *Compiler) validateContainerImages(workflowData *WorkflowData) error {
	if workflowData.Tools == nil {
		runtimeValidationLog.Print("No tools configured, skipping container validation")
		return nil
	}

	runtimeValidationLog.Printf("Validating container images for %d tools", len(workflowData.Tools))

	// Snapshot daemon availability before iterating so we can detect a
	// mid-loop transition (available → unavailable) and emit exactly one
	// warning for it, correctly counted in the compiler's warning total.
	daemonWasAvailable := isDockerDaemonRunning()

	var errors []string
	for toolName, toolConfig := range workflowData.Tools {
		if config, ok := toolConfig.(map[string]any); ok {
			// Get the MCP configuration to extract container info
			mcpConfig, err := getMCPConfig(config, toolName)
			if err != nil {
				// If we can't parse the MCP config, skip validation (will be caught elsewhere)
				continue
			}

			// Check if this tool originally had a container field (before transformation)
			if containerName, hasContainer := config["container"]; hasContainer && mcpConfig.Type == "stdio" {
				// Build the full container image name with version
				containerStr, ok := containerName.(string)
				if !ok {
					continue
				}

				containerImage := containerStr
				if version, hasVersion := config["version"]; hasVersion {
					if versionStr, ok := version.(string); ok && versionStr != "" {
						containerImage = containerImage + ":" + versionStr
					}
				}

				// Validate the container image exists using docker
				if err := validateDockerImage(containerImage, c.verbose, c.requireDocker); err != nil {
					errors = append(errors, fmt.Sprintf("tool '%s': %v", toolName, err))
				}
			}
		}
	}

	// If the daemon appeared available at the start of the loop but became
	// unreachable during a pull (requireDocker=false path only), emit a single
	// visible warning here where we can also increment the warning count.
	// For requireDocker=true, the per-image errors are already returned below
	// and surfaced as a warning by the caller — no extra warning is needed.
	if daemonWasAvailable && !isDockerDaemonRunning() && !c.requireDocker {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr("Docker daemon is not running — skipping container image validation"))
		c.IncrementWarningCount()
	}

	if len(errors) > 0 {
		return NewValidationError(
			"container.images",
			fmt.Sprintf("%d images failed validation", len(errors)),
			"container image validation failed",
			fmt.Sprintf("Fix the following container image issues:\n\n%s\n\nEnsure:\n1. Container images exist and are accessible\n2. Registry URLs are correct\n3. Image tags are specified\n4. You have pull permissions for private images", strings.Join(errors, "\n")),
		)
	}

	runtimeValidationLog.Print("Container image validation passed")
	return nil
}

// validateRuntimePackages validates that packages required by npx, pip, and uv are available
func (c *Compiler) validateRuntimePackages(workflowData *WorkflowData) error {
	// Detect runtime requirements
	requirements := detectRuntimeRequirementsCached(workflowData)
	runtimeValidationLog.Printf("Validating runtime packages: found %d runtime requirements", len(requirements))

	var errors []string
	if err := c.validateMCPScriptDependencies(workflowData); err != nil {
		errors = append(errors, err.Error())
	}

	for _, req := range requirements {
		switch req.Runtime.ID {
		case "node":
			// Validate npx packages used in the workflow
			runtimeValidationLog.Print("Validating npx packages")
			if err := c.validateNpxPackages(workflowData); err != nil {
				if isErrNpmNotAvailable(err) {
					// npm is not installed on this system — treat as a warning, not an error.
					// The workflow may still compile and run successfully in environments
					// that have npm (e.g., GitHub Actions).
					runtimeValidationLog.Print("npm not available, skipping npx package validation")
					fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr("npm not found, skipping npx package validation"))
					c.IncrementWarningCount()
				} else {
					runtimeValidationLog.Printf("Npx package validation failed: %v", err)
					errors = append(errors, err.Error())
				}
			}
		case "python":
			// Validate pip packages used in the workflow
			runtimeValidationLog.Print("Validating pip packages")
			if err := c.validatePipPackages(workflowData); err != nil {
				runtimeValidationLog.Printf("Pip package validation failed: %v", err)
				errors = append(errors, err.Error())
			}
		case "uv":
			// Validate uv packages used in the workflow
			runtimeValidationLog.Print("Validating uv packages")
			if err := c.validateUvPackages(workflowData); err != nil {
				runtimeValidationLog.Printf("Uv package validation failed: %v", err)
				errors = append(errors, err.Error())
			}
		}
	}

	if len(errors) > 0 {
		runtimeValidationLog.Printf("Runtime package validation completed with %d errors", len(errors))
		return NewValidationError(
			"runtime.packages",
			fmt.Sprintf("%d package validation errors", len(errors)),
			"runtime package validation failed",
			fmt.Sprintf("Fix the following package issues:\n\n%s\n\nEnsure:\n1. Package names are spelled correctly\n2. Packages exist in their respective registries (npm, PyPI)\n3. Package managers (npm, pip, uv) are installed\n4. Network access is available for registry checks", strings.Join(errors, "\n")),
		)
	}

	runtimeValidationLog.Print("Runtime package validation passed")
	return nil
}

func customSetupUVCacheWarnings(customSteps string) []string {
	var stepsWrapper map[string]any
	if err := yaml.Unmarshal([]byte(customSteps), &stepsWrapper); err != nil {
		return nil
	}

	steps, ok := stepsWrapper["steps"].([]any)
	if !ok {
		return nil
	}

	var warnings []string
	for index, stepValue := range steps {
		step, ok := stepValue.(map[string]any)
		if !ok {
			continue
		}
		uses, ok := step["uses"].(string)
		if !ok || !strings.EqualFold(extractActionRepo(uses), "astral-sh/setup-uv") {
			continue
		}

		withFields, hasWithFields := step["with"].(map[string]any)
		if value, hasCacheSetting := withFields["enable-cache"]; hasWithFields && hasCacheSetting && isExplicitlyFalse(value) {
			continue
		}

		stepDescription := fmt.Sprintf("step %d", index+1)
		if name, ok := step["name"].(string); ok && strings.TrimSpace(name) != "" {
			stepDescription = fmt.Sprintf("step %q", name)
		}
		warnings = append(warnings, fmt.Sprintf(
			"custom setup-uv %s does not explicitly disable Actions caching; set `with.enable-cache: false` to disable caching",
			stepDescription,
		))
	}
	return warnings
}

func isExplicitlyFalse(value any) bool {
	switch value := value.(type) {
	case bool:
		return !value
	case string:
		return strings.EqualFold(strings.TrimSpace(value), "false")
	default:
		return false
	}
}
