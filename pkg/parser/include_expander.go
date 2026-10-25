package parser

import (
	"bufio"
	"bytes"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var includeExpanderLog = logger.New("parser:include_expander")

// hasIncludeDirectives reports whether content contains any include/import directive that
// ParseImportDirective could match. Used as a fast pre-check to avoid scanner allocations.
// Matches @include, @import (legacy), and {{#import (legacy) forms.
func hasIncludeDirectives(content string) bool {
	return strings.Contains(content, "@include") ||
		strings.Contains(content, "@import") ||
		strings.Contains(content, "{{#import")
}

// ExpandIncludes recursively expands @include and @import directives until no more remain
// This matches the bash expand_includes function behavior

// ExpandIncludesWithManifest recursively expands @include and @import directives and returns list of included files
func ExpandIncludesWithManifest(content, baseDir string, extractTools bool, caches ...*ImportCache) (string, []string, error) {
	includeExpanderLog.Printf("Expanding includes: baseDir=%s, extractTools=%t, content_size=%d", baseDir, extractTools, len(content))
	if noIncludeContent, done := handleNoIncludeFastPath(content, extractTools); done {
		return noIncludeContent, nil, nil
	}
	currentContent, visited, err := expandIncludesIteratively(content, baseDir, extractTools, includeImportCache(caches))
	if err != nil {
		return "", nil, err
	}
	includedFiles := buildIncludedFilesManifest(baseDir, visited)
	includeExpanderLog.Printf("Include expansion complete: visited_files=%d", len(includedFiles))
	if extractTools {
		mergedTools, err := mergeToolsFromJSON(currentContent)
		return mergedTools, includedFiles, err
	}
	return currentContent, includedFiles, nil
}

func includeImportCache(caches []*ImportCache) *ImportCache {
	if len(caches) > 0 {
		return caches[0]
	}
	return nil
}

func handleNoIncludeFastPath(content string, extractTools bool) (string, bool) {
	if hasIncludeDirectives(content) {
		return "", false
	}
	includeExpanderLog.Print("Fast path: no include directives found")
	if extractTools {
		return "{}", true
	}
	if !strings.HasSuffix(content, "\n") {
		return content + "\n", true
	}
	return content, true
}

func expandIncludesIteratively(content, baseDir string, extractTools bool, cache *ImportCache) (string, map[string]struct {
}, error) {
	const maxDepth = 10
	currentContent := content
	visited := make(map[string]struct {
	})
	for depth := range maxDepth {
		includeExpanderLog.Printf("Include expansion depth: %d", depth)
		processedContent, err := processIncludesWithVisited(currentContent, baseDir, extractTools, visited, cache)
		if err != nil {
			return "", nil, err
		}
		if includeExpansionComplete(currentContent, processedContent, extractTools) {
			currentContent = processedContent
			break
		}
		currentContent = processedContent
	}
	return currentContent, visited, nil
}

func includeExpansionComplete(currentContent, processedContent string, extractTools bool) bool {
	if extractTools {
		return !strings.Contains(processedContent, "@include") && !strings.Contains(processedContent, "@import")
	}
	return processedContent == currentContent
}

func buildIncludedFilesManifest(baseDir string, visited map[string]struct {
}) []string {
	repoRoot := findGitHubRepoRoot(baseDir)
	includedFiles := make([]string, 0, len(visited))
	for filePath := range visited {
		includedFiles = append(includedFiles, relativizeIncludedFilePath(baseDir, repoRoot, filePath))
	}
	return includedFiles
}

func relativizeIncludedFilePath(baseDir, repoRoot, filePath string) string {
	relPath, err := filepath.Rel(baseDir, filePath)
	if err == nil && !strings.HasPrefix(relPath, "..") {
		return filepath.ToSlash(relPath)
	}
	if repoRoot != "" {
		repoRelPath, repoRelErr := filepath.Rel(repoRoot, filePath)
		if repoRelErr == nil && !strings.HasPrefix(repoRelPath, "..") {
			return filepath.ToSlash(repoRelPath)
		}
	}
	return filepath.ToSlash(filePath)
}

// findGitHubRepoRoot walks up the directory tree from dir to find the parent of the
// first ".github" directory encountered. It is used to compute repo-root-relative
// paths for files that live in sibling .github/ subdirectories (e.g. .github/shared/)
// so that the lock file Includes header shows ".github/shared/editorial.md" rather
// than an absolute system path.
//
// Returns the repo root directory (the parent of ".github"), or "" if no ".github"
// ancestor directory is found before reaching the filesystem root.
func findGitHubRepoRoot(dir string) string {
	current := filepath.Clean(dir)
	for {
		if filepath.Base(current) == ".github" {
			return filepath.Dir(current)
		}
		parent := filepath.Dir(current)
		if parent == current {
			// Reached filesystem root
			return ""
		}
		current = parent
	}
}

// BodyLevelImport represents a single {{#runtime-import}} or deprecated {{#import}} directive
// found in a markdown body, with the path resolved to be workspace-root-relative.
type BodyLevelImport struct {
	Path     string // workspace-root-relative path for the {{#runtime-import}} macro
	Optional bool   // true when the original directive used the ? form
}

// bodyLevelRuntimeImportRe matches {{#runtime-import}} and {{#runtime-import?}} directives
// in a single line of markdown (same pattern as runtime_import.cjs uses at runtime).
var bodyLevelRuntimeImportRe = regexp.MustCompile(`^\{\{#runtime-import(\?)?[ \t]+([^\}]+?)\}\}$`)

// ExtractBodyLevelImportPaths scans the markdown body (content is the body after frontmatter
// has been stripped) for {{#runtime-import}} directives and returns them as BodyLevelImport entries
// whose Path fields are ready to use in explicit {{#runtime-import}} macros in the compiled lock file.
//
// Relative paths (e.g. "shared/tools.md") are converted to workspace-root-relative form
// (e.g. ".github/workflows/shared/tools.md") using baseDir and the repo root.
// Paths that already start with ".github/" are kept as-is.
// Deprecated {{#import}} and legacy @include / @import directives are ignored;
// they are handled (with deprecation warnings) by include_processor.go.
func ExtractBodyLevelImportPaths(content, baseDir string) []BodyLevelImport {
	// Fast path: no {{#runtime-import}} directives present.
	if !strings.Contains(content, "{{#runtime-import") {
		return nil
	}

	repoRoot := findGitHubRepoRoot(baseDir)

	var results []BodyLevelImport
	for rawLine := range strings.SplitSeq(content, "\n") {
		line := strings.TrimSpace(rawLine)

		// Match {{#runtime-import}} directives only.
		m := bodyLevelRuntimeImportRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		optional := m[1] == "?"

		// Skip optional directives — they are handled with proper semantics at runtime
		// when runtime_import.cjs processes the workflow body. Promoting an optional
		// directive as a required macro would cause failures if the file is missing.
		if optional {
			continue
		}
		importPath := strings.TrimSpace(m[2])

		// Strip section reference (e.g. "file.md#Section" → "file.md")
		if idx := strings.Index(importPath, "#"); idx >= 0 {
			importPath = importPath[:idx]
		}
		importPath = strings.TrimSpace(importPath)

		// Skip URLs — these are fetched at runtime and don't need promotion.
		if strings.HasPrefix(importPath, "http://") || strings.HasPrefix(importPath, "https://") {
			continue
		}

		// Convert relative paths to workspace-root-relative.
		// Paths already starting with ".github/" are workspace-root-relative.
		// Absolute paths are used as-is.
		if !strings.HasPrefix(importPath, constants.GithubDir) && !filepath.IsAbs(importPath) {
			if repoRoot != "" {
				fullPath := filepath.Join(baseDir, importPath)
				if rel, err := filepath.Rel(repoRoot, fullPath); err == nil && !strings.HasPrefix(rel, "..") {
					importPath = rel
				}
			}
		}

		results = append(results, BodyLevelImport{
			Path:     filepath.ToSlash(importPath),
			Optional: false, // optional directives are skipped above; only required imports are promoted
		})
	}
	return results
}

func ExpandIncludesForEngines(content, baseDir string, caches ...*ImportCache) ([]string, error) {
	includeExpanderLog.Printf("Expanding includes for engines: baseDir=%s", baseDir)
	return expandIncludesForField(content, baseDir, func(c string) (string, error) {
		return extractFrontmatterField(c, "engine", "")
	}, "", includeImportCache(caches))
}

// ExpandIncludesForSafeOutputs recursively expands @include and @import directives to extract safe-outputs configurations
func ExpandIncludesForSafeOutputs(content, baseDir string, caches ...*ImportCache) ([]string, error) {
	includeExpanderLog.Printf("Expanding includes for safe-outputs: baseDir=%s", baseDir)
	return expandIncludesForField(content, baseDir, func(c string) (string, error) {
		return extractFrontmatterField(c, "safe-outputs", "{}")
	}, "{}", includeImportCache(caches))
}

// expandIncludesForField recursively expands includes to extract a specific frontmatter field
func expandIncludesForField(content, baseDir string, extractFunc func(string) (string, error), emptyValue string, cache *ImportCache) ([]string, error) {
	// Fast path: skip expansion entirely when no include/import directives are present.
	if !hasIncludeDirectives(content) {
		return nil, nil
	}

	const maxDepth = 10
	var results []string
	currentContent := content

	for range maxDepth {
		// Process includes in current content to extract the field
		processedResults, processedContent, err := processIncludesForField(currentContent, baseDir, extractFunc, emptyValue, cache)
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

	includeExpanderLog.Printf("Field expansion complete: results=%d", len(results))
	return results, nil
}

// processIncludesForField processes import directives to extract a specific frontmatter field
func processIncludesForField(content, baseDir string, extractFunc func(string) (string, error), emptyValue string, cache *ImportCache) ([]string, string, error) {
	// Fast path: skip scanner allocation when no include/import directives are present.
	if !hasIncludeDirectives(content) {
		return nil, content, nil
	}

	scanner := bufio.NewScanner(strings.NewReader(content))
	var result bytes.Buffer
	var results []string

	for scanner.Scan() {
		line := scanner.Text()

		// Parse import directive
		directive := ParseImportDirective(line)
		if directive != nil {
			fieldJSON, shouldSkip, err := extractFieldFromDirectiveForField(directive, baseDir, extractFunc, cache)
			if err != nil {
				return nil, "", err
			}
			if shouldSkip {
				continue
			}
			if fieldJSON != "" && fieldJSON != emptyValue {
				results = append(results, fieldJSON)
			}
		} else {
			// Regular line, just pass through
			result.WriteString(line + "\n")
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, "", fmt.Errorf("failed to scan includes for field: %w", err)
	}
	return results, result.String(), nil
}

func extractFieldFromDirectiveForField(
	directive *ImportDirectiveMatch,
	baseDir string,
	extractFunc func(string) (string, error),
	cache *ImportCache,
) (string, bool, error) {
	filePath := includeDirectiveFilePath(directive.Path)
	fullPath, err := ResolveIncludePath(filePath, baseDir, cache)
	if err != nil {
		if directive.IsOptional {
			return "", true, nil
		}
		return "", false, fmt.Errorf("failed to resolve required include '%s': %w", filePath, err)
	}

	fileContent, err := readFileFunc(fullPath)
	if err != nil {
		return "", false, fmt.Errorf("failed to read included file '%s': %w", fullPath, err)
	}

	fieldJSON, err := extractFunc(string(fileContent))
	if err != nil {
		return "", false, fmt.Errorf("failed to extract field from '%s': %w", fullPath, err)
	}

	return fieldJSON, false, nil
}

func includeDirectiveFilePath(includePath string) string {
	// Note: section references are ignored for frontmatter field extraction.
	if strings.Contains(includePath, "#") {
		parts := strings.SplitN(includePath, "#", 2)
		return parts[0]
	}
	return includePath
}
