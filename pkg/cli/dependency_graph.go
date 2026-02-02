package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/workflow"
)

var depGraphLog = logger.New("cli:dependency_graph")

// WorkflowNode represents a workflow file in the dependency graph
type WorkflowNode struct {
	Path       string   // Absolute path to the workflow file
	IsTopLevel bool     // True if this is a top-level workflow (not in subdirectory)
	Imports    []string // List of imported file paths (absolute)
}

// DependencyGraph tracks workflow dependencies for efficient recompilation
type DependencyGraph struct {
	nodes            map[string]*WorkflowNode // Map of absolute path -> WorkflowNode
	reverseImports   map[string][]string      // Map of imported file -> list of files that import it
	workflowsDir     string                   // Base workflows directory
	sharedDirPattern string                   // Pattern to identify shared workflows (e.g., "shared/")
}

// NewDependencyGraph creates a new dependency graph
func NewDependencyGraph(workflowsDir string) *DependencyGraph {
	depGraphLog.Printf("Creating dependency graph for directory: %s", workflowsDir)
	return &DependencyGraph{
		nodes:            make(map[string]*WorkflowNode),
		reverseImports:   make(map[string][]string),
		workflowsDir:     workflowsDir,
		sharedDirPattern: "shared/",
	}
}

// isTopLevelWorkflow determines if a workflow is a top-level workflow (dominator)
// Top-level workflows are those directly in the workflows directory, not in subdirectories
func (g *DependencyGraph) isTopLevelWorkflow(absPath string) bool {
	// Get relative path from workflows directory
	relPath, err := filepath.Rel(g.workflowsDir, absPath)
	if err != nil {
		depGraphLog.Printf("Failed to get relative path for %s: %v", absPath, err)
		return false
	}

	// Check if the file is directly in the workflows directory (no subdirectory)
	// If there's a path separator in the relative path, it's in a subdirectory
	isTopLevel := !strings.Contains(relPath, string(filepath.Separator))
	depGraphLog.Printf("Checking if %s is top-level: %v (relPath: %s)", absPath, isTopLevel, relPath)
	return isTopLevel
}

// BuildGraph scans all workflow files and builds the dependency graph
func (g *DependencyGraph) BuildGraph(compiler *workflow.Compiler) error {
	depGraphLog.Printf("Building dependency graph by scanning %s", g.workflowsDir)

	// Find all markdown files in the workflows directory (including subdirectories)
	var allWorkflows []string
	err := filepath.Walk(g.workflowsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".md") && !strings.HasSuffix(path, ".lock.yml") {
			allWorkflows = append(allWorkflows, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to scan workflows directory: %w", err)
	}

	depGraphLog.Printf("Found %d workflow files to analyze", len(allWorkflows))

	// Parse each workflow to extract its imports
	for _, workflowPath := range allWorkflows {
		if err := g.addWorkflow(workflowPath, compiler); err != nil {
			depGraphLog.Printf("Warning: failed to add workflow %s to graph: %v", workflowPath, err)
			// Continue processing other workflows even if one fails
		}
	}

	depGraphLog.Printf("Dependency graph built: %d nodes, %d reverse import entries", len(g.nodes), len(g.reverseImports))
	return nil
}

// addWorkflow adds a workflow to the dependency graph by parsing its imports
func (g *DependencyGraph) addWorkflow(workflowPath string, compiler *workflow.Compiler) error {
	depGraphLog.Printf("Adding workflow to graph: %s", workflowPath)

	// Check if already in graph
	if _, exists := g.nodes[workflowPath]; exists {
		depGraphLog.Printf("Workflow already in graph: %s", workflowPath)
		return nil
	}

	// Extract imports directly from the file
	imports, err := g.extractImportsFromFile(workflowPath)
	if err != nil {
		// If extraction fails, still add the node but with no imports
		depGraphLog.Printf("Failed to extract imports from %s: %v", workflowPath, err)
		node := &WorkflowNode{
			Path:       workflowPath,
			IsTopLevel: g.isTopLevelWorkflow(workflowPath),
			Imports:    []string{},
		}
		g.nodes[workflowPath] = node
		return err
	}

	// Create node
	node := &WorkflowNode{
		Path:       workflowPath,
		IsTopLevel: g.isTopLevelWorkflow(workflowPath),
		Imports:    imports,
	}
	g.nodes[workflowPath] = node

	// Build reverse imports (for each imported file, track who imports it)
	for _, importPath := range imports {
		g.reverseImports[importPath] = append(g.reverseImports[importPath], workflowPath)
		depGraphLog.Printf("Tracking reverse import: %s <- %s", importPath, workflowPath)
	}

	depGraphLog.Printf("Added workflow to graph: %s (top-level: %v, imports: %d)", workflowPath, node.IsTopLevel, len(imports))
	return nil
}

// extractImportsFromFile extracts imports directly from a workflow file
func (g *DependencyGraph) extractImportsFromFile(workflowPath string) ([]string, error) {
	// Sanitize the path to prevent path traversal attacks
	cleanPath := filepath.Clean(workflowPath)
	depGraphLog.Printf("Extracting imports from file: %s", cleanPath)
	// Read the file
	content, err := os.ReadFile(cleanPath)
	if err != nil {
		depGraphLog.Printf("Failed to read file %s: %v", cleanPath, err)
		return nil, err
	}

	// Parse frontmatter
	result, err := parser.ExtractFrontmatterFromContent(string(content))
	if err != nil {
		depGraphLog.Printf("Failed to parse frontmatter from %s: %v", cleanPath, err)
		return nil, err
	}

	imports := g.extractImportsFromFrontmatter(workflowPath, result.Frontmatter)
	depGraphLog.Printf("Extracted %d imports from %s", len(imports), workflowPath)
	return imports, nil
}

// extractImportsFromFrontmatter extracts the list of imported file paths from frontmatter
func (g *DependencyGraph) extractImportsFromFrontmatter(workflowPath string, frontmatter map[string]any) []string {
	var imports []string

	// Get frontmatter to extract imports
	if frontmatter == nil {
		depGraphLog.Printf("No frontmatter found in %s", workflowPath)
		return imports
	}

	importsField, exists := frontmatter["imports"]
	if !exists {
		depGraphLog.Printf("No imports field in frontmatter for %s", workflowPath)
		return imports
	}

	depGraphLog.Printf("Processing imports field from %s", workflowPath)

	// Parse imports field - can be array of strings or objects with path
	workflowDir := filepath.Dir(workflowPath)
	switch v := importsField.(type) {
	case []any:
		for _, item := range v {
			switch importItem := item.(type) {
			case string:
				// Simple string import
				if resolvedPath := g.resolveImportPath(importItem, workflowDir); resolvedPath != "" {
					imports = append(imports, resolvedPath)
				}
			case map[string]any:
				// Object import with path field
				if pathValue, hasPath := importItem["path"]; hasPath {
					if pathStr, ok := pathValue.(string); ok {
						if resolvedPath := g.resolveImportPath(pathStr, workflowDir); resolvedPath != "" {
							imports = append(imports, resolvedPath)
						}
					}
				}
			}
		}
	case []string:
		for _, importPath := range v {
			if resolvedPath := g.resolveImportPath(importPath, workflowDir); resolvedPath != "" {
				imports = append(imports, resolvedPath)
			}
		}
	}

	return imports
}

// resolveImportPath resolves an import path to an absolute file path
func (g *DependencyGraph) resolveImportPath(importPath string, baseDir string) string {
	// Handle section references (file.md#Section) - strip the section part
	if strings.Contains(importPath, "#") {
		parts := strings.SplitN(importPath, "#", 2)
		importPath = parts[0]
	}

	// Try to resolve as relative path first
	if !filepath.IsAbs(importPath) {
		absPath := filepath.Join(baseDir, importPath)
		if _, err := os.Stat(absPath); err == nil {
			depGraphLog.Printf("Resolved import %s to %s", importPath, absPath)
			return absPath
		}
	}

	// If that fails, try resolving with parser's cache-aware resolution
	// Note: We create a minimal cache here just for resolution
	importCache := parser.NewImportCache(g.findGitRoot())
	fullPath, err := parser.ResolveIncludePath(importPath, baseDir, importCache)
	if err != nil {
		depGraphLog.Printf("Failed to resolve import path %s: %v", importPath, err)
		return ""
	}

	depGraphLog.Printf("Resolved import %s to %s", importPath, fullPath)
	return fullPath
}

// findGitRoot finds the git repository root
func (g *DependencyGraph) findGitRoot() string {
	depGraphLog.Printf("Finding git root starting from: %s", g.workflowsDir)
	// Start from workflows directory and walk up
	dir := g.workflowsDir
	for {
		gitDir := filepath.Join(dir, ".git")
		if _, err := os.Stat(gitDir); err == nil {
			depGraphLog.Printf("Found git root at: %s", dir)
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root
			depGraphLog.Printf("Reached filesystem root, no .git directory found")
			break
		}
		dir = parent
	}
	depGraphLog.Printf("Using fallback git root: %s", g.workflowsDir)
	return g.workflowsDir // Fallback to workflows dir
}

// GetAffectedWorkflows returns the list of workflows that need to be recompiled
// when the given file is modified
func (g *DependencyGraph) GetAffectedWorkflows(modifiedPath string) []string {
	depGraphLog.Printf("Finding affected workflows for modified file: %s", modifiedPath)

	node, exists := g.nodes[modifiedPath]
	if !exists {
		// File not in graph - it might be a new file
		// If it's a top-level workflow, just compile it
		if g.isTopLevelWorkflow(modifiedPath) {
			depGraphLog.Printf("Modified file is a new top-level workflow: %s", modifiedPath)
			return []string{modifiedPath}
		}
		// If it's a shared workflow, find all top-level workflows
		depGraphLog.Printf("Modified file is a new shared workflow, returning all top-level workflows")
		return g.getAllTopLevelWorkflows()
	}

	// If it's a top-level workflow, just recompile it
	if node.IsTopLevel {
		depGraphLog.Printf("Modified file is a top-level workflow: %s", modifiedPath)
		return []string{modifiedPath}
	}

	// If it's a shared workflow, find all workflows that import it (directly or indirectly)
	// and return only the top-level ones
	affected := g.findAffectedTopLevelWorkflows(modifiedPath)
	depGraphLog.Printf("Found %d affected top-level workflows for shared workflow %s", len(affected), modifiedPath)
	return affected
}

// findAffectedTopLevelWorkflows finds all top-level workflows that depend on the given file
func (g *DependencyGraph) findAffectedTopLevelWorkflows(filePath string) []string {
	visited := make(map[string]bool)
	var topLevelWorkflows []string

	// BFS to find all workflows that import this file
	queue := []string{filePath}
	visited[filePath] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		// Get all workflows that import this file
		importers := g.reverseImports[current]
		for _, importer := range importers {
			if visited[importer] {
				continue
			}
			visited[importer] = true

			node := g.nodes[importer]
			if node != nil && node.IsTopLevel {
				// Found a top-level workflow that depends on the modified file
				topLevelWorkflows = append(topLevelWorkflows, importer)
				depGraphLog.Printf("Found top-level workflow affected: %s", importer)
			} else {
				// This is an intermediate shared workflow, continue searching
				queue = append(queue, importer)
				depGraphLog.Printf("Found intermediate workflow: %s", importer)
			}
		}
	}

	return topLevelWorkflows
}

// getAllTopLevelWorkflows returns all top-level workflows in the graph
func (g *DependencyGraph) getAllTopLevelWorkflows() []string {
	var topLevel []string
	for path, node := range g.nodes {
		if node.IsTopLevel {
			topLevel = append(topLevel, path)
		}
	}
	depGraphLog.Printf("Found %d top-level workflows in graph", len(topLevel))
	return topLevel
}

// UpdateWorkflow updates a workflow in the graph (e.g., after it's been modified)
func (g *DependencyGraph) UpdateWorkflow(workflowPath string, compiler *workflow.Compiler) error {
	depGraphLog.Printf("Updating workflow in graph: %s", workflowPath)

	// Remove old reverse imports for this workflow
	if oldNode, exists := g.nodes[workflowPath]; exists {
		for _, importPath := range oldNode.Imports {
			g.removeReverseImport(importPath, workflowPath)
		}
	}

	// Re-add the workflow with updated imports
	delete(g.nodes, workflowPath)
	return g.addWorkflow(workflowPath, compiler)
}

// removeReverseImport removes a reverse import entry
func (g *DependencyGraph) removeReverseImport(importPath string, importer string) {
	importers := g.reverseImports[importPath]
	for i, imp := range importers {
		if imp == importer {
			// Remove this entry
			g.reverseImports[importPath] = append(importers[:i], importers[i+1:]...)
			break
		}
	}
	// Clean up empty entries
	if len(g.reverseImports[importPath]) == 0 {
		delete(g.reverseImports, importPath)
	}
}

// RemoveWorkflow removes a workflow from the graph (e.g., when deleted)
func (g *DependencyGraph) RemoveWorkflow(workflowPath string) {
	depGraphLog.Printf("Removing workflow from graph: %s", workflowPath)

	node, exists := g.nodes[workflowPath]
	if !exists {
		return
	}

	// Remove reverse imports
	for _, importPath := range node.Imports {
		g.removeReverseImport(importPath, workflowPath)
	}

	// Remove the node
	delete(g.nodes, workflowPath)

	// Also remove from reverse imports if others import it
	delete(g.reverseImports, workflowPath)
}
