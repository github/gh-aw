package parser

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ExpandIncludes recursively expands @include and @import directives until no more remain
// This matches the bash expand_includes function behavior
func ExpandIncludes(content, baseDir string, extractTools bool) (string, error) {
	expandedContent, _, err := ExpandIncludesWithManifest(content, baseDir, extractTools)
	return expandedContent, err
}

// ExpandIncludesWithManifest recursively expands @include and @import directives and returns list of included files
func ExpandIncludesWithManifest(content, baseDir string, extractTools bool) (string, []string, error) {
	log.Printf("Expanding includes: baseDir=%s, extractTools=%t, content_size=%d", baseDir, extractTools, len(content))
	const maxDepth = 10
	currentContent := content
	visited := make(map[string]bool)

	for depth := 0; depth < maxDepth; depth++ {
		log.Printf("Include expansion depth: %d", depth)
		// Process includes in current content
		processedContent, err := processIncludesWithVisited(currentContent, baseDir, extractTools, visited)
		if err != nil {
			return "", nil, err
		}

		// For tools mode, check if we still have @include or @import directives
		if extractTools {
			if !strings.Contains(processedContent, "@include") && !strings.Contains(processedContent, "@import") {
				// No more includes to process for tools mode
				currentContent = processedContent
				break
			}
		} else {
			// For content mode, check if content changed
			if processedContent == currentContent {
				// No more includes to process
				break
			}
		}

		currentContent = processedContent
	}

	// Convert visited map to slice of file paths (make them relative to baseDir if possible)
	var includedFiles []string
	for filePath := range visited {
		// Try to make path relative to baseDir for cleaner output
		relPath, err := filepath.Rel(baseDir, filePath)
		if err == nil && !strings.HasPrefix(relPath, "..") {
			// Normalize to Unix paths (forward slashes) for cross-platform compatibility
			relPath = filepath.ToSlash(relPath)
			includedFiles = append(includedFiles, relPath)
		} else {
			// Normalize to Unix paths (forward slashes) for cross-platform compatibility
			filePath = filepath.ToSlash(filePath)
			includedFiles = append(includedFiles, filePath)
		}
	}

	log.Printf("Include expansion complete: visited_files=%d", len(includedFiles))
	if extractTools {
		// For tools mode, merge all extracted JSON objects
		mergedTools, err := mergeToolsFromJSON(currentContent)
		return mergedTools, includedFiles, err
	}

	return currentContent, includedFiles, nil
}

// ExpandIncludesForEngines recursively expands @include and @import directives to extract engine configurations
func ExpandIncludesForEngines(content, baseDir string) ([]string, error) {
	log.Printf("Expanding includes for engines: baseDir=%s", baseDir)
	return expandIncludesForField(content, baseDir, extractEngineFromContent, "")
}

// ExpandIncludesForSafeOutputs recursively expands @include and @import directives to extract safe-outputs configurations
func ExpandIncludesForSafeOutputs(content, baseDir string) ([]string, error) {
	log.Printf("Expanding includes for safe-outputs: baseDir=%s", baseDir)
	return expandIncludesForField(content, baseDir, extractSafeOutputsFromContent, "{}")
}

// expandIncludesForField recursively expands includes to extract a specific frontmatter field
func expandIncludesForField(content, baseDir string, extractFunc func(string) (string, error), emptyValue string) ([]string, error) {
	const maxDepth = 10
	var results []string
	currentContent := content

	for depth := 0; depth < maxDepth; depth++ {
		// Process includes in current content to extract the field
		processedResults, processedContent, err := processIncludesForField(currentContent, baseDir, extractFunc, emptyValue)
		if err != nil {
			return nil, err
		}

		// Add found results to the list
		results = append(results, processedResults...)

		// Check if content changed
		if processedContent == currentContent {
			// No more includes to process
			break
		}

		currentContent = processedContent
	}

	log.Printf("Field expansion complete: results=%d", len(results))
	return results, nil
}

// ProcessIncludesForEngines processes import directives to extract engine configurations
func ProcessIncludesForEngines(content, baseDir string) ([]string, string, error) {
	return processIncludesForField(content, baseDir, extractEngineFromContent, "")
}

// ProcessIncludesForSafeOutputs processes import directives to extract safe-outputs configurations
func ProcessIncludesForSafeOutputs(content, baseDir string) ([]string, string, error) {
	return processIncludesForField(content, baseDir, extractSafeOutputsFromContent, "{}")
}

// processIncludesForField processes import directives to extract a specific frontmatter field
func processIncludesForField(content, baseDir string, extractFunc func(string) (string, error), emptyValue string) ([]string, string, error) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	var result bytes.Buffer
	var results []string

	for scanner.Scan() {
		line := scanner.Text()

		// Parse import directive
		directive := ParseImportDirective(line)
		if directive != nil {
			isOptional := directive.IsOptional
			includePath := directive.Path

			// Handle section references (file.md#Section) - for frontmatter fields, we ignore sections
			var filePath string
			if strings.Contains(includePath, "#") {
				parts := strings.SplitN(includePath, "#", 2)
				filePath = parts[0]
				// Note: section references are ignored for frontmatter field extraction
			} else {
				filePath = includePath
			}

			// Resolve file path
			fullPath, err := ResolveIncludePath(filePath, baseDir, nil)
			if err != nil {
				if isOptional {
					// For optional includes, skip extraction
					continue
				}
				// For required includes, fail compilation with an error
				return nil, "", fmt.Errorf("failed to resolve required include '%s': %w", filePath, err)
			}

			// Read the included file
			fileContent, err := os.ReadFile(fullPath)
			if err != nil {
				// For any processing errors, fail compilation
				return nil, "", fmt.Errorf("failed to read included file '%s': %w", fullPath, err)
			}

			// Extract the field using the provided extraction function
			fieldJSON, err := extractFunc(string(fileContent))
			if err != nil {
				return nil, "", fmt.Errorf("failed to extract field from '%s': %w", fullPath, err)
			}

			if fieldJSON != "" && fieldJSON != emptyValue {
				results = append(results, fieldJSON)
			}
		} else {
			// Regular line, just pass through
			result.WriteString(line + "\n")
		}
	}

	return results, result.String(), nil
}
