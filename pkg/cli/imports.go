package cli

import (
	"bufio"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/workflow"
	"go.yaml.in/yaml/v3"
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

	importChanged := false
	// resolveOneImportPath converts a single raw import path to workflowspec format.
	// Paths that already use the workflowspec format (contain "@") are left unchanged.
	// When localWorkflowDir is set, relative paths whose files exist locally are also
	// preserved as-is so that consumers who have copied shared files into their own repo
	// are not forced onto cross-repo references after every `gh aw update`.
	replacements := make(map[string]string)
	resolveOneImportPath := func(importPath string) string {
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
		if workflowSpec != importPath {
			replacements[importPath] = workflowSpec
			importChanged = true
		}
		return workflowSpec
	}

	// processImportPaths converts a list of raw string import paths to workflowspec format.
	processImportPaths := func(imports []string) []string {
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
	processImportItems := func(items []any) []any {
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

	switch v := importsField.(type) {
	case []any:
		processedItems := processImportItems(v)
		importsLog.Printf("Found %d imports (array form) to process", len(processedItems))
		result.Frontmatter["imports"] = processedItems
	case []string:
		importsLog.Printf("Found %d imports ([]string form) to process", len(v))
		result.Frontmatter["imports"] = processImportPaths(v)
	case map[string]any:
		// Object form: process the 'aw' subfield if present
		if awAny, hasAW := v["aw"]; hasAW {
			switch aw := awAny.(type) {
			case []any:
				processedItems := processImportItems(aw)
				importsLog.Printf("Found %d imports (object form, aw subfield) to process", len(processedItems))
				v["aw"] = processedItems
			case []string:
				importsLog.Printf("Found %d imports (object form, aw []string) to process", len(aw))
				v["aw"] = processImportPaths(aw)
			}
		}
	default:
		importsLog.Print("Invalid imports field type, skipping")
		return content, nil
	}

	if !importChanged {
		return content, nil
	}
	if updated, ok := rewriteImportPathScalars(content, result, replacements); ok {
		if parsed, parseErr := parser.ExtractFrontmatterFromContent(updated); parseErr == nil &&
			reflect.DeepEqual(parsed.Frontmatter, result.Frontmatter) { //nolint:reflectdeepequalusage // YAML fields contain nested, heterogeneous maps and slices.
			return updated, nil
		}
	}

	// Fall back to replacing only the imports field for YAML shapes that cannot be edited in place.
	return replaceImportsFieldInContent(content, result, result.Frontmatter["imports"])
}

// rewriteImportPathScalars edits only single-line import values, leaving comments,
// quoting, key order, and all content outside those values untouched.
func rewriteImportPathScalars(content string, result *parser.FrontmatterResult, replacements map[string]string) (string, bool) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(strings.Join(result.FrontmatterLines, "\n")), &doc); err != nil || len(doc.Content) != 1 {
		return "", false
	}
	var imports *yaml.Node
	for _, root := range doc.Content {
		imports = importMappingValue(root, "imports")
	}
	if imports != nil && imports.Kind == yaml.MappingNode {
		imports = importMappingValue(imports, "aw")
	}
	if imports == nil || imports.Kind != yaml.SequenceNode {
		return "", false
	}

	lines := strings.Split(content, "\n")
	// Work backwards so multiple values on the same flow-style line keep their columns.
	for _, node := range slices.Backward(imports.Content) {
		if node.Kind == yaml.MappingNode {
			path := importMappingValue(node, "path")
			if path == nil || path.Tag != "!!str" {
				path = importMappingValue(node, "uses")
			}
			if path == nil {
				continue
			}
			node = path
		}
		if node.Kind == yaml.AliasNode {
			return "", false
		}
		replacement, changed := replacements[node.Value]
		if !changed {
			continue
		}
		oldText, newText, ok := importPathScalarText(node, replacement)
		if !ok {
			return "", false
		}
		lineIndex := node.Line + result.FrontmatterStart - 2
		if lineIndex < 0 || lineIndex >= len(lines) {
			return "", false
		}
		runes := []rune(lines[lineIndex])
		if node.Column < 1 || node.Column > len(runes)+1 {
			return "", false
		}
		prefix, suffix := string(runes[:node.Column-1]), string(runes[node.Column-1:])
		if !strings.HasPrefix(suffix, oldText) {
			return "", false
		}
		lines[lineIndex] = prefix + newText + strings.TrimPrefix(suffix, oldText)
	}
	return strings.Join(lines, "\n"), true
}

func importMappingValue(node *yaml.Node, key string) *yaml.Node {
	if node.Kind != yaml.MappingNode {
		return nil
	}
	var name string
	for i, child := range node.Content {
		if i%2 == 0 {
			name = child.Value
		} else if name == key {
			return child
		}
	}
	return nil
}

func importPathScalarText(node *yaml.Node, replacement string) (string, string, bool) {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!str" || node.Anchor != "" {
		return "", "", false
	}
	switch node.Style {
	case 0:
		encoded, err := yaml.Marshal(replacement)
		if err != nil {
			return "", "", false
		}
		return node.Value, strings.TrimSuffix(string(encoded), "\n"), true
	case yaml.SingleQuotedStyle:
		return "'" + strings.ReplaceAll(node.Value, "'", "''") + "'",
			"'" + strings.ReplaceAll(replacement, "'", "''") + "'", true
	case yaml.DoubleQuotedStyle:
		return strconv.Quote(node.Value), strconv.Quote(replacement), true
	default:
		return "", "", false
	}
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

func replaceImportsFieldInContent(content string, parsed *parser.FrontmatterResult, imports any) (string, error) {
	importLine, exists := parsed.FieldLines["imports"]
	if !exists {
		return "", errors.New("imports field line not found")
	}
	start := importLine - parsed.FrontmatterStart
	end := len(parsed.FrontmatterLines)
	for _, line := range parsed.FieldLines {
		if line > importLine && line-parsed.FrontmatterStart < end {
			end = line - parsed.FrontmatterStart
		}
	}
	if start < 0 || start >= end || end > len(parsed.FrontmatterLines) {
		return "", errors.New("invalid imports field range")
	}

	serializedLines, err := serializeImportsField(imports)
	if err != nil {
		return "", err
	}
	lineEnding := "\n"
	if strings.Contains(content, "\r\n") {
		lineEnding = "\r\n"
	}
	fileLines := strings.SplitAfter(content, "\n")
	start++
	end++
	if end > len(fileLines) {
		return "", errors.New("imports field range exceeds content")
	}
	return strings.Join(fileLines[:start], "") +
		strings.Join(serializedLines, lineEnding) + lineEnding +
		strings.Join(fileLines[end:], ""), nil
}

func serializeImportsField(imports any) ([]string, error) {
	serialized, err := reconstructWorkflowFileFromMap(map[string]any{"imports": imports}, "")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(serialized, "\n")
	return lines[1 : len(lines)-1], nil
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

		// Parse import directive using the helper function that handles both syntaxes
		directive := parser.ParseImportDirective(line)
		if directive != nil {
			isOptional := directive.IsOptional
			includePath := directive.Path

			// Skip if it's already a workflowspec (owner/repo/path@sha format)
			if isWorkflowSpecFormat(includePath) {
				result.WriteString(line + "\n")
				continue
			}

			// Handle section references (file.md#Section)
			filePath, sectionName := splitImportPath(includePath)

			// Skip if filePath is empty (e.g., section-only reference like "#Section")
			if filePath == "" {
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Skipping include with empty file path: "+line))
				}
				result.WriteString(line + "\n")
				continue
			}

			// Preserve relative {{#import}} paths whose files exist in the local workflow directory.
			if localWorkflowDir != "" && !strings.HasPrefix(filePath, "/") {
				if isLocalFileForUpdate(localWorkflowDir, filePath) {
					importsLog.Printf("Include path exists locally, preserving: %s", filePath)
					result.WriteString(line + "\n")
					// Add file to queue for processing nested includes (first visit only)
					if !setutil.Contains(visited, filePath) {
						visited[filePath] = struct {
						}{}
						queue = append(queue, filePath)
					}
					continue
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

			// Write the updated @include directive (even for duplicate occurrences)
			writeImportDirective(&result, workflowSpec, isOptional)

			// Only enqueue for nested-include processing on the first visit to prevent cycles
			if !setutil.Contains(visited, filePath) {
				visited[filePath] = struct {
				}{}
				queue = append(queue, filePath)
			}
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

	return result.String(), nil
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

		// Parse import directive
		directive := parser.ParseImportDirective(line)
		if directive != nil {
			isOptional := directive.IsOptional
			includePath := directive.Path

			// Skip if it's already a workflowspec (owner/repo/path@sha format)
			if isWorkflowSpecFormat(includePath) {
				result.WriteString(line + "\n")
				continue
			}

			// Handle section references (file.md#Section)
			filePath, sectionName := splitImportPath(includePath)

			// Skip if filePath is empty (e.g., section-only reference like "#Section")
			if filePath == "" {
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Skipping include with empty file path: "+line))
				}
				result.WriteString(line + "\n")
				continue
			}

			// Preserve relative {{#import}} paths whose files exist in the local workflow directory.
			if localWorkflowDir != "" && !strings.HasPrefix(filePath, "/") {
				if isLocalFileForUpdate(localWorkflowDir, filePath) {
					importsLog.Printf("Include path exists locally, preserving: %s", filePath)
					result.WriteString(line + "\n")
					continue
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
			writeImportDirective(&result, workflowSpec, isOptional)
		} else {
			// Regular line, pass through
			result.WriteString(line + "\n")
		}
	}

	return result.String(), scanner.Err()
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
