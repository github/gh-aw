package workflow

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var dependencyTrackerLog = logger.New("workflow:dependency_tracker")

// FindJavaScriptDependencies analyzes a JavaScript file and recursively finds all its dependencies
// without actually bundling the code. Returns a map of file paths that are required.
//
// Parameters:
//   - mainContent: The JavaScript content to analyze
//   - sources: Map of file paths to their content
//   - basePath: Base directory path for resolving relative imports (e.g., "js")
//
// Returns:
//   - Map of file paths (relative to basePath) that are dependencies
//   - Error if a required file is not found in sources
func FindJavaScriptDependencies(mainContent string, sources map[string]string, basePath string) (map[string]bool, error) {
	dependencyTrackerLog.Printf("Finding JavaScript dependencies: source_count=%d, base_path=%s", len(sources), basePath)

	// Track discovered dependencies
	dependencies := make(map[string]bool)

	// Track files we've already processed to avoid circular dependencies
	processed := make(map[string]bool)

	// Recursively find dependencies starting from the main content
	if err := findDependenciesRecursive(mainContent, basePath, sources, dependencies, processed); err != nil {
		dependencyTrackerLog.Printf("Dependency tracking failed: %v", err)
		return nil, err
	}

	dependencyTrackerLog.Printf("Dependency tracking completed: found %d dependencies", len(dependencies))
	return dependencies, nil
}

// findDependenciesRecursive processes content and recursively tracks its dependencies
func findDependenciesRecursive(content string, currentPath string, sources map[string]string, dependencies map[string]bool, processed map[string]bool) error {
	// Regular expression to match require('./...') or require("./...")
	// This matches both single-line and multi-line destructuring:
	// const { x } = require("./file.cjs");
	// const {
	//   x,
	//   y
	// } = require("./file.cjs");
	// Captures the require path where it starts with ./ or ../
	requireRegex := regexp.MustCompile(`(?s)(?:const|let|var)\s+(?:\{[^}]*\}|\w+)\s*=\s*require\(['"](\.\.?/[^'"]+)['"]\);?`)

	// Find all requires
	matches := requireRegex.FindAllStringSubmatch(content, -1)

	if len(matches) == 0 {
		// No requires found, nothing to track
		return nil
	}

	dependencyTrackerLog.Printf("Found %d require statements in current file", len(matches))

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		// Extract the require path
		requirePath := match[1]

		// Resolve the full path relative to current path
		var fullPath string
		if currentPath == "" {
			fullPath = requirePath
		} else {
			fullPath = filepath.Join(currentPath, requirePath)
		}

		// Ensure .cjs extension
		if !strings.HasSuffix(fullPath, ".cjs") && !strings.HasSuffix(fullPath, ".js") {
			fullPath += ".cjs"
		}

		// Normalize the path (clean up ./ and ../)
		fullPath = filepath.Clean(fullPath)

		// Convert Windows path separators to forward slashes for consistency
		fullPath = filepath.ToSlash(fullPath)

		// Check if we've already processed this file
		if processed[fullPath] {
			dependencyTrackerLog.Printf("Skipping already processed dependency: %s", fullPath)
			continue
		}

		// Mark as processed
		processed[fullPath] = true

		// Add to dependencies
		dependencies[fullPath] = true
		dependencyTrackerLog.Printf("Added dependency: %s", fullPath)

		// Look up the required file in sources
		requiredContent, ok := sources[fullPath]
		if !ok {
			dependencyTrackerLog.Printf("Required file not found in sources: %s", fullPath)
			return fmt.Errorf("required file not found in sources: %s", fullPath)
		}

		// Recursively find dependencies of this file
		requiredDir := filepath.Dir(fullPath)
		if err := findDependenciesRecursive(requiredContent, requiredDir, sources, dependencies, processed); err != nil {
			return err
		}
	}

	return nil
}
