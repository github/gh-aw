package cli

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/setutil"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var packagesLog = logger.New("cli:packages")

// Pre-compiled regexes for package processing (performance optimization)
var (
	includePattern = regexp.MustCompile(`^@include(\?)?\s+(.+)$`)
)

// collectLocalIncludeDependencies collects dependencies for package-based workflows
func collectLocalIncludeDependencies(content, packagePath string, verbose bool) ([]IncludeDependency, error) {
	packagesLog.Printf("Collecting include dependencies: packagePath=%s, content_size=%d", packagePath, len(content))
	var dependencies []IncludeDependency
	seen := make(map[string]struct {
	})

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Collecting package dependencies from: "+packagePath))
	}

	err := collectLocalIncludeDependenciesRecursive(content, packagePath, &dependencies, seen, verbose)
	packagesLog.Printf("Collected %d include dependencies from %s", len(dependencies), packagePath)
	return dependencies, err
}

// collectLocalIncludeDependenciesRecursive recursively processes @include directives in package content
func collectLocalIncludeDependenciesRecursive(content, baseDir string, dependencies *[]IncludeDependency, seen map[string]struct {
}, verbose bool) error {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		if matches := includePattern.FindStringSubmatch(line); matches != nil {
			isOptional := matches[1] == "?"
			includePath := strings.TrimSpace(matches[2])

			// Handle section references (file.md#Section)
			var filePath string
			if strings.Contains(includePath, "#") {
				parts := strings.SplitN(includePath, "#", 2)
				filePath = parts[0]
			} else {
				filePath = includePath
			}

			// Resolve the full source path relative to base directory
			fullSourcePath := filepath.Join(baseDir, filePath)

			// Skip if we've already processed this file
			if setutil.Contains(seen, fullSourcePath) {
				continue
			}
			seen[fullSourcePath] = struct {
			}{}

			// Add dependency
			dep := IncludeDependency{
				SourcePath: fullSourcePath,
				TargetPath: filePath, // Keep relative path for target
				IsOptional: isOptional,
			}
			*dependencies = append(*dependencies, dep)

			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found include dependency: %s -> %s", fullSourcePath, filePath)))
			}

			// Read the included file and process its includes recursively
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

			// Recursively process includes in the included file
			includedDir := filepath.Dir(fullSourcePath)
			if err := collectLocalIncludeDependenciesRecursive(markdownContent, includedDir, dependencies, seen, verbose); err != nil {
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Error processing includes in %s: %v", fullSourcePath, err)))
				}
			}
		}
	}

	return scanner.Err()
}

// copyIncludeDependenciesFromPackageWithForce copies include dependencies from package filesystem with force option
func copyIncludeDependenciesFromPackageWithForce(dependencies []IncludeDependency, githubWorkflowsDir string, verbose bool, force bool, tracker *FileTracker) error {
	packagesLog.Printf("Copying %d include dependencies to %s (force=%t)", len(dependencies), githubWorkflowsDir, force)
	for _, dep := range dependencies {
		// Create the target path in .github/workflows
		targetPath := filepath.Join(githubWorkflowsDir, dep.TargetPath)

		// Create target directory if it doesn't exist
		targetDir := filepath.Dir(targetPath)
		if err := os.MkdirAll(targetDir, constants.DirPermPublic); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", targetDir, err)
		}

		// Read source content from package
		sourceContent, err := os.ReadFile(dep.SourcePath)
		if err != nil {
			if dep.IsOptional {
				// For optional includes, just show an informational message and skip
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Optional include file not found: %s (you can create this file to configure the workflow)", dep.TargetPath)))
				}
				continue
			}
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to read include file %s: %v", dep.SourcePath, err)))
			continue
		}

		// Check if target file already exists
		fileExists := false
		if existingContent, err := os.ReadFile(targetPath); err == nil {
			fileExists = true
			// File exists, compare contents
			if bytes.Equal(existingContent, sourceContent) {
				// Contents are the same, skip
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Include file %s already exists with same content, skipping", dep.TargetPath)))
				}
				continue
			}

			// Contents are different
			if !force {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Include file %s already exists with different content, skipping (use --force to overwrite)", dep.TargetPath)))
				continue
			}

			// Force is enabled, overwrite
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Overwriting existing include file: "+dep.TargetPath))
		}

		// Track the file based on whether it existed before (if tracker is available)
		if tracker != nil {
			if fileExists {
				tracker.TrackModified(targetPath)
			} else {
				tracker.TrackCreated(targetPath)
			}
		}

		// Write to target
		if err := os.WriteFile(targetPath, sourceContent, constants.FilePermPublic); err != nil {
			return fmt.Errorf("failed to write include file %s: %w", targetPath, err)
		}

		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Copied include file: %s -> %s", dep.SourcePath, targetPath)))
		}
	}

	return nil
}

// IncludeDependency represents a file dependency from @include directives
type IncludeDependency struct {
	SourcePath string // Path in the source (local)
	TargetPath string // Relative path where it should be copied in .github/workflows
	IsOptional bool   // Whether this is an optional include (@include?)
}

// ExtractWorkflowDescription extracts the description field from workflow content string
func ExtractWorkflowDescription(content string) string {
	result, err := parser.ExtractFrontmatterFromContent(content)
	if err != nil {
		return ""
	}

	if desc, ok := result.Frontmatter["description"]; ok {
		if descStr, ok := desc.(string); ok {
			return descStr
		}
	}

	return ""
}

// ExtractWorkflowEngine extracts the engine field from workflow content string.
// Supports both string format (engine: copilot) and nested format (engine: { id: copilot }).
func ExtractWorkflowEngine(content string) string {
	result, err := parser.ExtractFrontmatterFromContent(content)
	if err != nil {
		return ""
	}

	if engine, ok := result.Frontmatter["engine"]; ok {
		// Handle string format: engine: copilot
		if engineStr, ok := engine.(string); ok {
			packagesLog.Printf("Extracted engine (string format): %s", engineStr)
			return engineStr
		}
		// Handle nested format: engine: { id: copilot }
		if engineMap, ok := engine.(map[string]any); ok {
			if id, ok := engineMap["id"]; ok {
				if idStr, ok := id.(string); ok {
					packagesLog.Printf("Extracted engine (nested format): %s", idStr)
					return idStr
				}
			}
		}
	}

	return ""
}

// ExtractWorkflowPrivateSetting extracts the private field from workflow content string.
// Returns the boolean value and whether the field was explicitly present.
func ExtractWorkflowPrivateSetting(content string) (bool, bool) {
	result, err := parser.ExtractFrontmatterFromContent(content)
	if err != nil {
		return false, false
	}

	if private, ok := result.Frontmatter["private"]; ok {
		if privateBool, ok := private.(bool); ok {
			return privateBool, true
		}
	}

	return false, false
}

// ExtractWorkflowPrivate extracts the private field from workflow content string.
// Returns true if the workflow has private: true in its frontmatter.
func ExtractWorkflowPrivate(content string) bool {
	privateBool, ok := ExtractWorkflowPrivateSetting(content)
	if ok {
		return privateBool
	}
	return false
}

// ExtractWorkflowDescriptionFromFile extracts the description field from a workflow file
func ExtractWorkflowDescriptionFromFile(filePath string) string {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}

	return ExtractWorkflowDescription(string(content))
}
