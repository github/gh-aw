// This file provides runtime validation for packages, containers, and expressions.
//
// # Runtime Validation
//
// This file validates runtime dependencies and configuration for agentic workflows.
// It ensures that:
//   - Container images exist and are accessible
//   - Runtime packages (npm, pip, uv) are available
//   - Expression sizes don't exceed GitHub Actions limits
//   - Secret references are valid
//   - Cache IDs are unique
//
// # Validation Functions
//
//   - validateExpressionSizes() - Validates expression size limits (21KB max)
//   - validateContainerImages() - Validates Docker images exist
//   - validateRuntimePackages() - Validates npm, pip, uv packages
//   - collectPackagesFromWorkflow() - Generic package collection helper
//   - validateNoDuplicateCacheIDs() - Validates unique cache-memory IDs
//   - validateSecretReferences() - Validates secret name format
//
// # Validation Patterns
//
// This file uses several patterns:
//   - External resource validation: Docker images, npm/pip packages
//   - Size limit validation: Expression sizes, file sizes
//   - Format validation: Secret names, cache IDs
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
//   - It validates configuration format (secrets, cache IDs)
//   - It requires external resource checking
//
// For general validation, see validation.go.
// For detailed documentation, see scratchpad/validation-architecture.md

package workflow

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

var runtimeValidationLog = logger.New("workflow:runtime_validation")

// validateExpressionSizes validates that no expression values in the generated YAML exceed GitHub Actions limits
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

	return nil
}

// validateContainerImages validates that container images specified in MCP configs exist and are accessible
func (c *Compiler) validateContainerImages(workflowData *WorkflowData) error {
	if workflowData.Tools == nil {
		runtimeValidationLog.Print("No tools configured, skipping container validation")
		return nil
	}

	runtimeValidationLog.Printf("Validating container images for %d tools", len(workflowData.Tools))
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
				if err := validateDockerImage(containerImage, c.verbose); err != nil {
					errors = append(errors, fmt.Sprintf("tool '%s': %v", toolName, err))
				} else if c.verbose {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("✓ Container image validated: %s", containerImage)))
				}
			}
		}
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
	requirements := DetectRuntimeRequirements(workflowData)
	runtimeValidationLog.Printf("Validating runtime packages: found %d runtime requirements", len(requirements))

	var errors []string
	for _, req := range requirements {
		switch req.Runtime.ID {
		case "node":
			// Validate npx packages used in the workflow
			runtimeValidationLog.Print("Validating npx packages")
			if err := c.validateNpxPackages(workflowData); err != nil {
				runtimeValidationLog.Printf("Npx package validation failed: %v", err)
				errors = append(errors, err.Error())
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

// collectPackagesFromWorkflow is a generic helper that collects packages from workflow data
// using the provided extractor function. It deduplicates packages and optionally extracts
// from MCP tool configurations when toolCommand is provided.
func collectPackagesFromWorkflow(
	workflowData *WorkflowData,
	extractor func(string) []string,
	toolCommand string,
) []string {
	runtimeValidationLog.Printf("Collecting packages from workflow: toolCommand=%s", toolCommand)
	var packages []string
	seen := make(map[string]bool)

	// Extract from custom steps
	if workflowData.CustomSteps != "" {
		pkgs := extractor(workflowData.CustomSteps)
		for _, pkg := range pkgs {
			if !seen[pkg] {
				packages = append(packages, pkg)
				seen[pkg] = true
			}
		}
	}

	// Extract from engine steps
	if workflowData.EngineConfig != nil && len(workflowData.EngineConfig.Steps) > 0 {
		for _, step := range workflowData.EngineConfig.Steps {
			if run, hasRun := step["run"]; hasRun {
				if runStr, ok := run.(string); ok {
					pkgs := extractor(runStr)
					for _, pkg := range pkgs {
						if !seen[pkg] {
							packages = append(packages, pkg)
							seen[pkg] = true
						}
					}
				}
			}
		}
	}

	// Extract from MCP server configurations (if toolCommand is provided)
	if toolCommand != "" && workflowData.Tools != nil {
		for _, toolConfig := range workflowData.Tools {
			// Handle structured MCP config with command and args fields
			if config, ok := toolConfig.(map[string]any); ok {
				if command, hasCommand := config["command"]; hasCommand {
					if cmdStr, ok := command.(string); ok && cmdStr == toolCommand {
						// Extract package from args, skipping flags
						if args, hasArgs := config["args"]; hasArgs {
							if argsSlice, ok := args.([]any); ok {
								for _, arg := range argsSlice {
									if pkgStr, ok := arg.(string); ok {
										// Skip flags (arguments starting with - or --)
										if !strings.HasPrefix(pkgStr, "-") && !seen[pkgStr] {
											packages = append(packages, pkgStr)
											seen[pkgStr] = true
											break // Only take the first non-flag argument
										}
									}
								}
							}
						}
					}
				}
			} else if cmdStr, ok := toolConfig.(string); ok {
				// Handle string-format MCP tool (e.g., "npx -y package")
				// Use the extractor function to parse the command string
				pkgs := extractor(cmdStr)
				for _, pkg := range pkgs {
					if !seen[pkg] {
						packages = append(packages, pkg)
						seen[pkg] = true
					}
				}
			}
		}
	}

	runtimeValidationLog.Printf("Collected %d unique packages", len(packages))
	return packages
}

// validateNoDuplicateCacheIDs checks for duplicate cache IDs and returns an error if found
func validateNoDuplicateCacheIDs(caches []CacheMemoryEntry) error {
	runtimeValidationLog.Printf("Validating cache IDs: checking %d caches for duplicates", len(caches))
	seen := make(map[string]bool)
	for _, cache := range caches {
		if seen[cache.ID] {
			runtimeValidationLog.Printf("Duplicate cache ID found: %s", cache.ID)
			return NewValidationError(
				"sandbox.cache-memory",
				cache.ID,
				"duplicate cache-memory ID found - each cache must have a unique ID",
				"Change the cache ID to a unique value. Example:\n\nsandbox:\n  cache-memory:\n    - id: cache-1\n      size: 100MB\n    - id: cache-2  # Use unique IDs\n      size: 50MB",
			)
		}
		seen[cache.ID] = true
	}
	runtimeValidationLog.Print("Cache ID validation passed: no duplicates found")
	return nil
}

// validateSecretReferences validates that secret references are valid
func validateSecretReferences(secrets []string) error {
	runtimeValidationLog.Printf("Validating secret references: checking %d secrets", len(secrets))
	// Secret names must be valid environment variable names
	secretNamePattern := regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

	for _, secret := range secrets {
		if !secretNamePattern.MatchString(secret) {
			runtimeValidationLog.Printf("Invalid secret name format: %s", secret)
			return NewValidationError(
				"secrets",
				secret,
				"invalid secret name format - must follow environment variable naming conventions",
				"Secret names must:\n- Start with an uppercase letter\n- Contain only uppercase letters, numbers, and underscores\n\nExamples:\n  MY_SECRET_KEY      ✓\n  API_TOKEN_123      ✓\n  mySecretKey        ✗ (lowercase)\n  123_SECRET         ✗ (starts with number)\n  MY-SECRET          ✗ (hyphens not allowed)",
			)
		}
	}

	return nil
}

// validateFirewallConfig validates firewall configuration including log-level
func (c *Compiler) validateFirewallConfig(workflowData *WorkflowData) error {
	if workflowData.NetworkPermissions == nil || workflowData.NetworkPermissions.Firewall == nil {
		return nil
	}

	config := workflowData.NetworkPermissions.Firewall
	runtimeValidationLog.Printf("Validating firewall config: enabled=%v, logLevel=%s", config.Enabled, config.LogLevel)
	if config.LogLevel != "" {
		if err := ValidateLogLevel(config.LogLevel); err != nil {
			runtimeValidationLog.Printf("Invalid firewall log level: %s", config.LogLevel)
			return err
		}
	}

	runtimeValidationLog.Print("Firewall config validation passed")
	return nil
}
