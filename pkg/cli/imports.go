package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/workflow"
)

var importsLog = logger.New("cli:imports")

// resolveImportPath resolves a relative import path to its full repository path
// based on the workflow file's location
func resolveImportPath(importPath string, workflowPath string) string {
	// If the import path is already a workflowspec format (contains owner/repo), return as-is
	if isWorkflowSpecFormat(importPath) {
		return importPath
	}

	// If the import path is absolute (starts with /), use it as-is (relative to repo root)
	if strings.HasPrefix(importPath, "/") {
		return strings.TrimPrefix(importPath, "/")
	}

	// Otherwise, resolve relative to the workflow file's directory
	workflowDir := filepath.Dir(workflowPath)

	// Clean the path to normalize it (removes .., ., etc.)
	fullPath := filepath.Clean(filepath.Join(workflowDir, importPath))

	// Convert back to forward slashes (filepath.Clean uses OS path separator)
	fullPath = filepath.ToSlash(fullPath)

	return fullPath
}

// processImportsWithWorkflowSpec processes imports field in frontmatter and replaces local file references
// with workflowspec format (owner/repo/path@sha) for all imports found
func processImportsWithWorkflowSpec(content string, workflow *WorkflowSpec, commitSHA string, verbose bool) (string, error) {
	importsLog.Printf("Processing imports with workflowspec: repo=%s, sha=%s", workflow.RepoSlug, commitSHA)
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Processing imports field to replace with workflowspec"))
	}

	// Extract frontmatter from content
	result, err := parser.ExtractFrontmatterFromContent(content)
	if err != nil {
		importsLog.Printf("No frontmatter found, skipping imports processing")
		return content, nil // Return original content if no frontmatter
	}

	// Check if imports field exists
	importsField, exists := result.Frontmatter["imports"]
	if !exists {
		importsLog.Print("No imports field in frontmatter")
		return content, nil // No imports field, return original content
	}

	// Convert imports to array of strings
	var imports []string
	switch v := importsField.(type) {
	case []any:
		for _, item := range v {
			if str, ok := item.(string); ok {
				imports = append(imports, str)
			}
		}
	case []string:
		imports = v
	default:
		importsLog.Print("Invalid imports field type, skipping")
		return content, nil // Invalid imports field, skip processing
	}

	importsLog.Printf("Found %d imports to process", len(imports))

	// Process each import and replace with workflowspec format
	processedImports := make([]string, 0, len(imports))
	for _, importPath := range imports {
		// Skip if already a workflowspec
		if isWorkflowSpecFormat(importPath) {
			importsLog.Printf("Import already in workflowspec format: %s", importPath)
			processedImports = append(processedImports, importPath)
			continue
		}

		// Resolve the import path relative to the workflow file's directory
		resolvedPath := resolveImportPath(importPath, workflow.WorkflowPath)
		importsLog.Printf("Resolved import path: %s -> %s (workflow: %s)", importPath, resolvedPath, workflow.WorkflowPath)

		// Build workflowspec for this import
		// Format: owner/repo/path@sha
		workflowSpec := workflow.RepoSlug + "/" + resolvedPath
		if commitSHA != "" {
			workflowSpec += "@" + commitSHA
		} else if workflow.Version != "" {
			workflowSpec += "@" + workflow.Version
		}

		importsLog.Printf("Converted import: %s -> %s", importPath, workflowSpec)
		processedImports = append(processedImports, workflowSpec)
	}

	// Update frontmatter with processed imports
	result.Frontmatter["imports"] = processedImports

	// Use helper function to reconstruct workflow file with proper field ordering
	return reconstructWorkflowFileFromMap(result.Frontmatter, result.Markdown)
}

// reconstructWorkflowFileFromMap reconstructs a workflow file from frontmatter map and markdown
// using proper field ordering and YAML helpers
func reconstructWorkflowFileFromMap(frontmatter map[string]any, markdown string) (string, error) {
	// Convert frontmatter to YAML with proper field ordering
	// Use PriorityWorkflowFields to ensure consistent ordering of top-level fields
	updatedFrontmatter, err := workflow.MarshalWithFieldOrder(frontmatter, constants.PriorityWorkflowFields)
	if err != nil {
		return "", fmt.Errorf("failed to marshal frontmatter: %w", err)
	}

	// Clean up the YAML - remove trailing newline and unquote the "on" key
	frontmatterStr := strings.TrimSuffix(string(updatedFrontmatter), "\n")
	frontmatterStr = workflow.UnquoteYAMLKey(frontmatterStr, "on")

	// Reconstruct the file
	var lines []string
	lines = append(lines, "---")
	if frontmatterStr != "" {
		lines = append(lines, strings.Split(frontmatterStr, "\n")...)
	}
	lines = append(lines, "---")
	if markdown != "" {
		lines = append(lines, markdown)
	}

	return strings.Join(lines, "\n"), nil
}

// processIncludesWithWorkflowSpec processes @include directives in content and replaces local file references
// with workflowspec format (owner/repo/path@sha) for all includes found in the package
func processIncludesWithWorkflowSpec(content string, workflow *WorkflowSpec, commitSHA, packagePath string, verbose bool) (string, error) {
	importsLog.Printf("Processing @include directives: repo=%s, sha=%s, package=%s", workflow.RepoSlug, commitSHA, packagePath)
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Processing @include directives to replace with workflowspec"))
	}

	// Track visited includes to prevent cycles
	visited := make(map[string]bool)

	// Use a queue to process files iteratively instead of recursion
	type fileToProcess struct {
		path string
	}
	queue := []fileToProcess{}

	// Process the main content first
	scanner := bufio.NewScanner(strings.NewReader(content))
	var result strings.Builder

	for scanner.Scan() {
		line := scanner.Text()

		// Parse import directive using the helper function that handles both syntaxes
		directive := parser.ParseImportDirective(line)
		if directive != nil {
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

			// Skip if filePath is empty (e.g., section-only reference like "#Section")
			if filePath == "" {
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Skipping include with empty file path: %s", line)))
				}
				result.WriteString(line + "\n")
				continue
			}

			// Check for cycle detection
			if visited[filePath] {
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Cycle detected for include: %s, skipping", filePath)))
				}
				continue
			}

			// Mark as visited
			visited[filePath] = true

			// Build workflowspec for this include
			// Format: owner/repo/path@sha
			workflowSpec := workflow.RepoSlug + "/" + filePath
			if commitSHA != "" {
				workflowSpec += "@" + commitSHA
			} else if workflow.Version != "" {
				workflowSpec += "@" + workflow.Version
			}

			// Add section if present
			if sectionName != "" {
				workflowSpec += "#" + sectionName
			}

			// Write the updated @include directive
			if isOptional {
				result.WriteString("{{#import? " + workflowSpec + "}}\n")
			} else {
				result.WriteString("{{#import " + workflowSpec + "}}\n")
			}

			// Add file to queue for processing nested includes
			queue = append(queue, fileToProcess{path: filePath})
		} else {
			// Regular line, pass through
			result.WriteString(line + "\n")
		}
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	// Process queue of files to check for nested includes
	for len(queue) > 0 {
		// Dequeue the first file
		fileItem := queue[0]
		queue = queue[1:]

		fullSourcePath := filepath.Join(packagePath, fileItem.path)
		if _, err := os.Stat(fullSourcePath); err != nil {
			continue // File doesn't exist, skip
		}

		includedContent, err := os.ReadFile(fullSourcePath)
		if err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Could not read include file %s: %v", fullSourcePath, err)))
			}
			continue
		}

		// Extract markdown content from the included file
		markdownContent, err := parser.ExtractMarkdownContent(string(includedContent))
		if err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Could not extract markdown from %s: %v", fullSourcePath, err)))
			}
			continue
		}

		// Scan for nested includes
		nestedScanner := bufio.NewScanner(strings.NewReader(markdownContent))
		for nestedScanner.Scan() {
			line := nestedScanner.Text()

			directive := parser.ParseImportDirective(line)
			if directive != nil {
				includePath := directive.Path

				// Handle section references
				var nestedFilePath string
				if strings.Contains(includePath, "#") {
					parts := strings.SplitN(includePath, "#", 2)
					nestedFilePath = parts[0]
				} else {
					nestedFilePath = includePath
				}

				// Check for cycle detection
				if visited[nestedFilePath] {
					if verbose {
						fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Cycle detected for include: %s, skipping", nestedFilePath)))
					}
					continue
				}

				// Mark as visited and add to queue
				visited[nestedFilePath] = true
				queue = append(queue, fileToProcess{path: nestedFilePath})
			}
		}
	}

	return result.String(), nil
}

// processIncludesInContent processes @include directives in workflow content for update command
// and also processes imports field in frontmatter
func processIncludesInContent(content string, workflow *WorkflowSpec, commitSHA string, verbose bool) (string, error) {
	// First process imports field in frontmatter
	processedImportsContent, err := processImportsWithWorkflowSpec(content, workflow, commitSHA, verbose)
	if err != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to process imports: %v", err)))
		}
		// Continue with original content on error
		processedImportsContent = content
	}

	// Then process @include directives in markdown
	scanner := bufio.NewScanner(strings.NewReader(processedImportsContent))
	var result strings.Builder

	for scanner.Scan() {
		line := scanner.Text()

		// Parse import directive
		directive := parser.ParseImportDirective(line)
		if directive != nil {
			isOptional := directive.IsOptional
			includePath := directive.Path

			// Skip if it's already a workflowspec (contains repo/path format)
			if isWorkflowSpecFormat(includePath) {
				result.WriteString(line + "\n")
				continue
			}

			// Handle section references (file.md#Section)
			var filePath, sectionName string
			if strings.Contains(includePath, "#") {
				parts := strings.SplitN(includePath, "#", 2)
				filePath = parts[0]
				sectionName = parts[1]
			} else {
				filePath = includePath
			}

			// Skip if filePath is empty (e.g., section-only reference like "#Section")
			if filePath == "" {
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Skipping include with empty file path: %s", line)))
				}
				result.WriteString(line + "\n")
				continue
			}

			// Resolve the file path relative to the workflow file's directory
			resolvedPath := resolveImportPath(filePath, workflow.WorkflowPath)

			// Build workflowspec for this include
			// Format: owner/repo/path@sha
			workflowSpec := workflow.RepoSlug + "/" + resolvedPath
			if commitSHA != "" {
				workflowSpec += "@" + commitSHA
			} else if workflow.Version != "" {
				workflowSpec += "@" + workflow.Version
			}

			// Add section if present
			if sectionName != "" {
				workflowSpec += "#" + sectionName
			}

			// Write the updated import directive
			if isOptional {
				result.WriteString("{{#import? " + workflowSpec + "}}\n")
			} else {
				result.WriteString("{{#import " + workflowSpec + "}}\n")
			}
		} else {
			// Regular line, pass through
			result.WriteString(line + "\n")
		}
	}

	return result.String(), scanner.Err()
}

// isWorkflowSpecFormat checks if a path already looks like a workflowspec
func isWorkflowSpecFormat(path string) bool {
	// Check if it contains @ (ref separator) or looks like owner/repo/path
	if strings.Contains(path, "@") {
		return true
	}

	// Remove section reference if present
	cleanPath := path
	if idx := strings.Index(path, "#"); idx != -1 {
		cleanPath = path[:idx]
	}

	// Check if it has at least 3 parts and doesn't start with . or /
	parts := strings.Split(cleanPath, "/")
	if len(parts) >= 3 && !strings.HasPrefix(cleanPath, ".") && !strings.HasPrefix(cleanPath, "/") {
		return true
	}

	return false
}
