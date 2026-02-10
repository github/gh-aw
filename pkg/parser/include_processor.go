package parser

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

var includeLog = logger.New("parser:include_processor")

// ProcessIncludes processes @include, @import (deprecated), and {{#import: directives in markdown content
// This matches the bash process_includes function behavior
func ProcessIncludes(content, baseDir string, extractTools bool) (string, error) {
	includeLog.Printf("Processing includes: baseDir=%s, extractTools=%t, content_size=%d", baseDir, extractTools, len(content))
	visited := make(map[string]bool)
	return processIncludesWithVisited(content, baseDir, extractTools, visited)
}

// processIncludesWithVisited processes import directives with cycle detection
func processIncludesWithVisited(content, baseDir string, extractTools bool, visited map[string]bool) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	var result bytes.Buffer

	for scanner.Scan() {
		line := scanner.Text()

		// Parse import directive
		directive := ParseImportDirective(line)
		if directive != nil {
			// Emit deprecation warning for legacy syntax
			if directive.IsLegacy {
				// Security: Escape strings to prevent quote injection in warning messages
				// Use %q format specifier to safely quote strings containing special characters
				optionalMarker := ""
				if directive.IsOptional {
					optionalMarker = "?"
				}
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Deprecated syntax: %q. Use {{#import%s %s}} instead.",
					directive.Original,
					optionalMarker,
					directive.Path)))
			}

			isOptional := directive.IsOptional
			includePath := directive.Path

			// Handle section references (file.md#Section)
			var filePath, sectionName string
			if strings.Contains(includePath, "#") {
				parts := strings.SplitN(includePath, "#", 2)
				filePath = parts[0]
				sectionName = parts[1]
			} else {
				filePath = includePath
			}

			// Resolve file path first to get the canonical path
			fullPath, err := ResolveIncludePath(filePath, baseDir, nil)
			if err != nil {
				includeLog.Printf("Failed to resolve include path '%s': %v", filePath, err)
				if isOptional {
					// For optional includes, show a friendly informational message to stdout
					if !extractTools {
						fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Optional include file not found: %s. You can create this file to configure the workflow.", filePath)))
					}
					continue
				}
				// For required includes, fail compilation with an error
				return "", fmt.Errorf("failed to resolve required include '%s': %w", filePath, err)
			}

			// Check for repeated imports using the resolved full path
			if visited[fullPath] {
				includeLog.Printf("Skipping already included file: %s", fullPath)
				if !extractTools {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Already included: %s, skipping", filePath)))
				}
				continue
			}

			// Mark as visited using the resolved full path
			includeLog.Printf("Processing include file: %s", fullPath)
			visited[fullPath] = true

			// Process the included file
			includedContent, err := processIncludedFileWithVisited(fullPath, sectionName, extractTools, visited)
			if err != nil {
				// For any processing errors, fail compilation
				return "", fmt.Errorf("failed to process included file '%s': %w", fullPath, err)
			}

			if extractTools {
				// For tools mode, add each JSON on a separate line
				result.WriteString(includedContent + "\n")
			} else {
				result.WriteString(includedContent)
			}
		} else {
			// Regular line, just pass through (unless extracting tools)
			if !extractTools {
				result.WriteString(line + "\n")
			}
		}
	}

	return result.String(), nil
}

// processIncludedFile processes a single included file, optionally extracting a section
// processIncludedFileWithVisited processes a single included file with cycle detection for nested includes
func processIncludedFileWithVisited(filePath, sectionName string, extractTools bool, visited map[string]bool) (string, error) {
	includeLog.Printf("Reading included file: %s (extractTools=%t, section=%s)", filePath, extractTools, sectionName)
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read included file %s: %w", filePath, err)
	}
	includeLog.Printf("Read %d bytes from included file: %s", len(content), filePath)

	// Validate included file frontmatter based on file location
	result, err := ExtractFrontmatterFromContent(string(content))
	if err != nil {
		return "", fmt.Errorf("failed to extract frontmatter from included file %s: %w", filePath, err)
	}

	// Check if file is under .github/workflows/ for strict validation
	isWorkflowFile := isUnderWorkflowsDirectory(filePath)

	// Check if file is a custom agent file (.github/agents/*.md)
	// Custom agent files use GitHub Copilot's format where 'tools' is an array, not an object
	isAgentFile := isCustomAgentFile(filePath)

	// Always try strict validation first (but skip for agent files which have a different schema)
	var validationErr error
	if !isAgentFile {
		validationErr = ValidateIncludedFileFrontmatterWithSchemaAndLocation(result.Frontmatter, filePath)
	}

	if validationErr != nil {
		if isWorkflowFile {
			// For workflow files, strict validation must pass
			includeLog.Printf("Validation failed for workflow file %s: %v", filePath, validationErr)
			return "", fmt.Errorf("invalid frontmatter in included file %s: %w", filePath, validationErr)
		} else {
			includeLog.Printf("Validation failed for non-workflow file %s, applying relaxed validation", filePath)
			// For non-workflow files, fall back to relaxed validation with warnings
			if len(result.Frontmatter) > 0 {
				// Valid fields for non-workflow frontmatter (fields that are allowed in shared workflows)
				// This list matches the allowed fields in shared workflows (main_workflow_schema minus forbidden fields)
				validFields := map[string]bool{
					"tools":          true,
					"engine":         true,
					"network":        true,
					"mcp-servers":    true,
					"imports":        true,
					"name":           true,
					"description":    true,
					"steps":          true,
					"safe-outputs":   true,
					"safe-inputs":    true,
					"services":       true,
					"runtimes":       true,
					"permissions":    true,
					"secret-masking": true,
					"applyTo":        true,
					"inputs":         true,
					"infer":          true, // Custom agent format field (Copilot)
				}

				// Check for unexpected frontmatter fields
				var unexpectedFields []string
				for key := range result.Frontmatter {
					if !validFields[key] {
						unexpectedFields = append(unexpectedFields, key)
					}
				}

				if len(unexpectedFields) > 0 {
					// Show warning for unexpected frontmatter fields
					fmt.Fprintf(os.Stderr, "%s\n", console.FormatWarningMessage(
						fmt.Sprintf("Ignoring unexpected frontmatter fields in %s: %s",
							filePath, strings.Join(unexpectedFields, ", "))))
				}

				// Validate the tools, engine, network, and mcp-servers sections if present
				// Skip tools validation for custom agent files as they use a different format (array vs object)
				filteredFrontmatter := map[string]any{}
				if !isAgentFile {
					if tools, hasTools := result.Frontmatter["tools"]; hasTools {
						filteredFrontmatter["tools"] = tools
					}
				}
				if engine, hasEngine := result.Frontmatter["engine"]; hasEngine {
					filteredFrontmatter["engine"] = engine
				}
				if network, hasNetwork := result.Frontmatter["network"]; hasNetwork {
					filteredFrontmatter["network"] = network
				}
				if mcpServers, hasMCPServers := result.Frontmatter["mcp-servers"]; hasMCPServers {
					filteredFrontmatter["mcp-servers"] = mcpServers
				}
				// Note: we don't validate imports field as it's handled separately
				if len(filteredFrontmatter) > 0 {
					if err := ValidateIncludedFileFrontmatterWithSchemaAndLocation(filteredFrontmatter, filePath); err != nil {
						fmt.Fprintf(os.Stderr, "%s\n", console.FormatWarningMessage(
							fmt.Sprintf("Invalid configuration in %s: %v", filePath, err)))
					}
				}
			}
		}
	}

	if extractTools {
		// For custom agent files, skip tools extraction as they use a different format (array vs object)
		// Agent files are meant to be passed directly to the engine (e.g., via --agent flag)
		if isAgentFile {
			return "{}", nil
		}

		// Extract tools from frontmatter, using filtered frontmatter for non-workflow files with validation errors
		if validationErr == nil || isWorkflowFile {
			// If validation passed or it's a workflow file (which must have valid frontmatter), use original extraction
			return extractToolsFromContent(string(content))
		} else {
			// For non-workflow files with validation errors, only extract tools section
			if tools, hasTools := result.Frontmatter["tools"]; hasTools {
				toolsJSON, err := json.Marshal(tools)
				if err != nil {
					return "{}", nil
				}
				return strings.TrimSpace(string(toolsJSON)), nil
			}
			return "{}", nil
		}
	}

	// Extract markdown content
	markdownContent, err := ExtractMarkdownContent(string(content))
	if err != nil {
		return "", fmt.Errorf("failed to extract markdown from %s: %w", filePath, err)
	}

	// Process nested includes recursively
	includedDir := filepath.Dir(filePath)
	markdownContent, err = processIncludesWithVisited(markdownContent, includedDir, extractTools, visited)
	if err != nil {
		return "", fmt.Errorf("failed to process nested includes in %s: %w", filePath, err)
	}

	// If section specified, extract only that section
	if sectionName != "" {
		sectionContent, err := ExtractMarkdownSection(markdownContent, sectionName)
		if err != nil {
			return "", fmt.Errorf("failed to extract section '%s' from %s: %w", sectionName, filePath, err)
		}
		return strings.Trim(sectionContent, "\n") + "\n", nil
	}

	return strings.Trim(markdownContent, "\n") + "\n", nil
}
