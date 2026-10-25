package cli

import (
	"bufio"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var importsLog = logger.New("cli:imports")

// buildWorkflowSpecRef builds a workflowspec reference string from components.
// Format: owner/repo/path@version (e.g., "github/gh-aw/shared/mcp/arxiv.md@abc123")
// If commitSHA is provided, it takes precedence over version.
// If neither is provided, returns the path without a version suffix.
func buildWorkflowSpecRef(repoSlug, path, commitSHA, version string) string {
	workflowSpec := repoSlug + "/" + path
	if commitSHA != "" {
		workflowSpec += "@" + commitSHA
	} else if version != "" {
		workflowSpec += "@" + version
	}
	return workflowSpec
}

// processImportsWithWorkflowSpec processes imports field in frontmatter and replaces local file references
// with workflowspec format (owner/repo/path@sha) for all imports found.
// Handles both array form and object form (with 'aw' subfield) of the imports field.
// If localWorkflowDir is non-empty, any import path whose file exists under that directory is
// left as a local relative path rather than being rewritten to a cross-repo reference.
func processImportsWithWorkflowSpec(content string, workflow *WorkflowSpec, commitSHA string, localWorkflowDir string, verbose bool) (string, error) {
	importsLog.Printf("Processing imports with workflowspec: repo=%s, sha=%s, localWorkflowDir=%s", workflow.RepoSlug, commitSHA, localWorkflowDir)
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

	resolveOneImportPath := func(importPath string) string {
		return resolveWorkflowSpecImportPath(importPath, workflow, commitSHA, localWorkflowDir)
	}

	switch v := importsField.(type) {
	case []any:
		processedItems := processWorkflowSpecImportItems(v, resolveOneImportPath)
		importsLog.Printf("Found %d imports (array form) to process", len(processedItems))
		result.Frontmatter["imports"] = processedItems
	case []string:
		importsLog.Printf("Found %d imports ([]string form) to process", len(v))
		result.Frontmatter["imports"] = processWorkflowSpecImportPaths(v, resolveOneImportPath)
	case map[string]any:
		// Object form: process the 'aw' subfield if present
		if awAny, hasAW := v["aw"]; hasAW {
			switch aw := awAny.(type) {
			case []any:
				processedItems := processWorkflowSpecImportItems(aw, resolveOneImportPath)
				importsLog.Printf("Found %d imports (object form, aw subfield) to process", len(processedItems))
				v["aw"] = processedItems
			case []string:
				importsLog.Printf("Found %d imports (object form, aw []string) to process", len(aw))
				v["aw"] = processWorkflowSpecImportPaths(aw, resolveOneImportPath)
			}
		}
	default:
		importsLog.Print("Invalid imports field type, skipping")
		return content, nil
	}

	// Use helper function to reconstruct workflow file with proper field ordering
	return reconstructWorkflowFileFromMap(result.Frontmatter, result.Markdown)
}

// resolveWorkflowSpecImportPath converts a single raw import path to workflowspec format.
// Paths that already use the workflowspec format (contain "@") are left unchanged.
// When localWorkflowDir is set, relative paths whose files exist locally are also
// preserved as-is so that consumers who have copied shared files into their own repo
// are not forced onto cross-repo references after every `gh aw update`.
func resolveWorkflowSpecImportPath(importPath string, workflow *WorkflowSpec, commitSHA, localWorkflowDir string) string {
	if isWorkflowSpecFormat(importPath) {
		importsLog.Printf("Import already in workflowspec format: %s", importPath)
		return importPath
	}
	// Preserve relative paths whose files exist in the local workflow directory.
	// Absolute paths (starting with "/") are not checked — they are always resolved
	// relative to the repo root and cannot be reliably tested here.
	if localWorkflowDir != "" && !strings.HasPrefix(importPath, "/") {
		if isLocalFileForUpdate(localWorkflowDir, importPath) {
			importsLog.Printf("Import path exists locally, preserving relative path: %s", importPath)
			return importPath
		}
	}
	resolvedPath := resolveImportPath(importPath, filepath.Dir(workflow.WorkflowPath), importPathImportsOpts)
	importsLog.Printf("Resolved import path: %s -> %s (workflow: %s)", importPath, resolvedPath, workflow.WorkflowPath)
	workflowSpec := buildWorkflowSpecRef(workflow.RepoSlug, resolvedPath, commitSHA, workflow.Version)
	importsLog.Printf("Converted import: %s -> %s", importPath, workflowSpec)
	return workflowSpec
}

// processImportPaths converts a list of raw string import paths to workflowspec format.
func processWorkflowSpecImportPaths(imports []string, resolveOneImportPath func(string) string) []string {
	processed := make([]string, 0, len(imports))
	for _, importPath := range imports {
		processed = append(processed, resolveOneImportPath(importPath))
	}
	return processed
}

// processImportItems converts a list of import entries to workflowspec format.
// Each entry may be a plain string path, or an object with a "path"/"uses" key
// (and optionally an "inputs"/"with" key). Object-form entries are preserved as
// objects — only their path/uses value is rewritten — so that any accompanying
// inputs/with data is not silently dropped from the imports collection.
func processWorkflowSpecImportItems(items []any, resolveOneImportPath func(string) string) []any {
	processed := make([]any, 0, len(items))
	for _, item := range items {
		switch v := item.(type) {
		case string:
			processed = append(processed, resolveOneImportPath(v))
		case map[string]any:
			updated := make(map[string]any, len(v))
			maps.Copy(updated, v)
			if pathVal, ok := updated["path"].(string); ok {
				updated["path"] = resolveOneImportPath(pathVal)
			} else if usesVal, ok := updated["uses"].(string); ok {
				updated["uses"] = resolveOneImportPath(usesVal)
			}
			processed = append(processed, updated)
		default:
			importsLog.Printf("Preserving import entry of unsupported type: %T", item)
			processed = append(processed, item)
		}
	}
	return processed
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

	return parser.ReconstructWorkflowFile(frontmatterStr, markdown)
}

// processIncludesWithWorkflowSpec processes @include directives in content and replaces local file references
// with workflowspec format (owner/repo/path@sha) for all includes found in the package.
// If localWorkflowDir is non-empty, any relative import path whose file exists under that directory is
// left as a local relative path rather than being rewritten to a cross-repo reference.
func processIncludesWithWorkflowSpec(content string, workflow *WorkflowSpec, commitSHA, packagePath, localWorkflowDir string, verbose bool) (string, error) {
	importsLog.Printf("Processing @include directives: repo=%s, sha=%s, package=%s, localWorkflowDir=%s", workflow.RepoSlug, commitSHA, packagePath, localWorkflowDir)
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Processing @include directives to replace with workflowspec"))
	}

	// Track visited includes to prevent cycles
	visited := make(map[string]struct {
	})

	// Use a queue to process files iteratively instead of recursion
	queue := []string{}

	// Process the main content first
	scanner := bufio.NewScanner(strings.NewReader(content))
	var result strings.Builder

	for scanner.Scan() {
		line := scanner.Text()
		filePath := writeWorkflowSpecIncludeLine(&result, line, workflow, commitSHA, localWorkflowDir, verbose)
		if filePath != "" && !setutil.Contains(visited, filePath) {
			visited[filePath] = struct{}{}
			queue = append(queue, filePath)
		}
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	scanNestedWorkflowIncludes(queue, visited, packagePath, verbose)
	return result.String(), nil
}

func scanNestedWorkflowIncludes(queue []string, visited map[string]struct{}, packagePath string, verbose bool) {
	// Process queue of files to check for nested includes
	for len(queue) > 0 {
		// Dequeue the first file
		filePath := queue[0]
		queue = queue[1:]

		fullSourcePath := filepath.Join(packagePath, filePath)
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
				nestedFilePath, _ := splitImportPath(includePath)

				// Check for cycle detection
				if setutil.Contains(visited, nestedFilePath) {
					if verbose {
						fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Cycle detected for include: %s, skipping", nestedFilePath)))
					}
					continue
				}

				// Mark as visited and add to queue
				visited[nestedFilePath] = struct {
				}{}
				queue = append(queue, nestedFilePath)
			}
		}
	}

}

// processIncludesInContent processes @include directives in workflow content for update command
// and also processes imports field in frontmatter.
// If localWorkflowDir is non-empty, any relative import/include path whose file exists under
// that directory is left as-is rather than being rewritten to a cross-repo reference.
func processIncludesInContent(content string, workflow *WorkflowSpec, commitSHA string, localWorkflowDir string, verbose bool) (string, error) {
	// First process imports field in frontmatter
	processedImportsContent, err := processImportsWithWorkflowSpec(content, workflow, commitSHA, localWorkflowDir, verbose)
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
		writeWorkflowSpecIncludeLine(&result, line, workflow, commitSHA, localWorkflowDir, verbose)
	}

	return result.String(), scanner.Err()
}

func writeWorkflowSpecIncludeLine(result *strings.Builder, line string, workflow *WorkflowSpec, commitSHA, localWorkflowDir string, verbose bool) string {
	// Parse import directive
	directive := parser.ParseImportDirective(line)
	if directive != nil {
		isOptional := directive.IsOptional
		includePath := directive.Path

		// Skip if it's already a workflowspec (owner/repo/path@sha format)
		if isWorkflowSpecFormat(includePath) {
			result.WriteString(line + "\n")
			return ""
		}

		// Handle section references (file.md#Section)
		filePath, sectionName := splitImportPath(includePath)

		// Skip if filePath is empty (e.g., section-only reference like "#Section")
		if filePath == "" {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Skipping include with empty file path: "+line))
			}
			result.WriteString(line + "\n")
			return ""
		}

		// Preserve relative {{#import}} paths whose files exist in the local workflow directory.
		if localWorkflowDir != "" && !strings.HasPrefix(filePath, "/") {
			if isLocalFileForUpdate(localWorkflowDir, filePath) {
				importsLog.Printf("Include path exists locally, preserving: %s", filePath)
				result.WriteString(line + "\n")
				return filePath
			}
		}

		// Resolve the file path relative to the workflow file's directory
		resolvedPath := resolveImportPath(filePath, filepath.Dir(workflow.WorkflowPath), importPathImportsOpts)

		// Build workflowspec for this include
		workflowSpec := buildWorkflowSpecRef(workflow.RepoSlug, resolvedPath, commitSHA, workflow.Version)

		// Add section if present
		if sectionName != "" {
			workflowSpec += "#" + sectionName
		}

		// Write the updated import directive
		writeImportDirective(result, workflowSpec, isOptional)
		return filePath
	} else {
		// Regular line, pass through
		result.WriteString(line + "\n")
	}
	return ""
}

// isLocalFileForUpdate returns true when importPath resolves to an existing file
// within localWorkflowDir. The resolved absolute path must stay inside localWorkflowDir
// to guard against path traversal (e.g. "../../etc/passwd" in import paths).
// importPath must be a relative path — callers must not pass absolute paths here.
func isLocalFileForUpdate(localWorkflowDir, importPath string) bool {
	if localWorkflowDir == "" || importPath == "" {
		return false
	}
	localPath := filepath.Join(localWorkflowDir, importPath)
	absDir, err1 := filepath.Abs(localWorkflowDir)
	absPath, err2 := filepath.Abs(localPath)
	if err1 != nil || err2 != nil {
		return false
	}
	// Reject traversal attempts: the resolved path must be a child of localWorkflowDir
	if !strings.HasPrefix(absPath, absDir+string(filepath.Separator)) {
		return false
	}
	_, statErr := os.Stat(localPath)
	return statErr == nil
}

// isWorkflowSpecFormat reports whether path is a workflowspec-style reference.
// It delegates to parser.IsWorkflowSpec to keep CLI and parser behavior consistent.
func isWorkflowSpecFormat(path string) bool {
	return parser.IsWorkflowSpec(path)
}

// splitImportPath splits "file.md#Section" into ("file.md", "Section").
// If no "#" is present, returns (includePath, "").
func splitImportPath(includePath string) (filePath, sectionName string) {
	if file, section, ok := strings.Cut(includePath, "#"); ok {
		return file, section
	}
	return includePath, ""
}

// writeImportDirective writes an {{#import}} or {{#import?}} directive for workflowSpec.
func writeImportDirective(w *strings.Builder, workflowSpec string, isOptional bool) {
	if isOptional {
		w.WriteString("{{#import? " + workflowSpec + "}}\n")
	} else {
		w.WriteString("{{#import " + workflowSpec + "}}\n")
	}
}
