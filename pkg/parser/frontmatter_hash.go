package parser

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/githubnext/gh-aw/pkg/constants"
	"github.com/githubnext/gh-aw/pkg/logger"
)

var frontmatterHashLog = logger.New("parser:frontmatter_hash")

// FileReader is a function type that reads file content
// This abstraction allows for different file reading strategies (disk, GitHub API, in-memory, etc.)
type FileReader func(filePath string) ([]byte, error)

// DefaultFileReader reads files from disk using os.ReadFile
var DefaultFileReader FileReader = os.ReadFile

// compilerVersion holds the gh-aw version for hash computation
var compilerVersion = "dev"

// isReleaseVersion indicates whether the current version is a release
var isReleaseVersion = false

// SetCompilerVersion sets the compiler version for hash computation
func SetCompilerVersion(version string) {
	compilerVersion = version
}

// SetIsRelease sets whether the current version is a release build
func SetIsRelease(isRelease bool) {
	isReleaseVersion = isRelease
}

// ComputeFrontmatterHash computes a deterministic SHA-256 hash of frontmatter
// including contributions from all imported workflows.
//
// The hash is computed over a canonical JSON representation that includes:
// - Main workflow frontmatter
// - All imported workflow frontmatter (in BFS processing order)
// - Normalized and sorted for deterministic output
//
// This function follows the Frontmatter Hash Specification (v1.0).
func ComputeFrontmatterHash(frontmatter map[string]any, baseDir string, cache *ImportCache) (string, error) {
	frontmatterHashLog.Print("Computing frontmatter hash")

	// Process imports to get merged frontmatter
	result, err := ProcessImportsFromFrontmatterWithManifest(frontmatter, baseDir, cache)
	if err != nil {
		return "", fmt.Errorf("failed to process imports: %w", err)
	}

	// Build the canonical frontmatter map
	canonical := buildCanonicalFrontmatter(frontmatter, result)

	// Serialize to canonical JSON
	canonicalJSON, err := marshalCanonicalJSON(canonical)
	if err != nil {
		return "", fmt.Errorf("failed to marshal canonical JSON: %w", err)
	}

	frontmatterHashLog.Printf("Canonical JSON length: %d bytes", len(canonicalJSON))

	// Compute SHA-256 hash
	hash := sha256.Sum256([]byte(canonicalJSON))
	hashHex := hex.EncodeToString(hash[:])

	frontmatterHashLog.Printf("Computed hash: %s", hashHex)
	return hashHex, nil
}

// buildCanonicalFrontmatter builds a canonical representation of frontmatter
// including all fields that should be included in the hash computation.
func buildCanonicalFrontmatter(frontmatter map[string]any, result *ImportsResult) map[string]any {
	canonical := make(map[string]any)

	// Helper to safely add field from frontmatter
	addField := func(key string) {
		if value, exists := frontmatter[key]; exists {
			canonical[key] = value
		}
	}

	// Helper to safely add non-empty string
	addString := func(key, value string) {
		if value != "" {
			canonical[key] = value
		}
	}

	// Helper to safely add non-empty slice
	addSlice := func(key string, value []string) {
		if len(value) > 0 {
			canonical[key] = value
		}
	}

	// Core configuration fields
	addField("engine")
	addField("on")
	addField("permissions")
	addField("tracker-id")

	// Tool and integration fields
	addField("tools")
	addField("mcp-servers")
	addField("network")
	addField("safe-outputs")
	addField("safe-inputs")

	// Runtime configuration fields
	addField("runtimes")
	addField("services")
	addField("cache")

	// Workflow structure fields
	addField("steps")
	addField("post-steps")
	addField("jobs")

	// Metadata fields
	addField("description")
	addField("labels")
	addField("bots")
	addField("timeout-minutes")
	addField("secret-masking")

	// Input parameter definitions
	addField("inputs")

	// Add merged content from imports
	addString("merged-tools", result.MergedTools)
	addString("merged-mcp-servers", result.MergedMCPServers)
	addSlice("merged-engines", result.MergedEngines)
	addSlice("merged-safe-outputs", result.MergedSafeOutputs)
	addSlice("merged-safe-inputs", result.MergedSafeInputs)
	addString("merged-steps", result.MergedSteps)
	addString("merged-runtimes", result.MergedRuntimes)
	addString("merged-services", result.MergedServices)
	addString("merged-network", result.MergedNetwork)
	addString("merged-permissions", result.MergedPermissions)
	addString("merged-secret-masking", result.MergedSecretMasking)
	addSlice("merged-bots", result.MergedBots)
	addString("merged-post-steps", result.MergedPostSteps)
	addSlice("merged-labels", result.MergedLabels)
	addSlice("merged-caches", result.MergedCaches)

	// Add list of imported files for traceability (sorted for determinism)
	if len(result.ImportedFiles) > 0 {
		// Sort imports for deterministic ordering
		sortedImports := make([]string, len(result.ImportedFiles))
		copy(sortedImports, result.ImportedFiles)
		sort.Strings(sortedImports)
		canonical["imports"] = sortedImports
	}

	// Add agent file if present
	if result.AgentFile != "" {
		canonical["agent-file"] = result.AgentFile
	}

	// Add import inputs if present
	if len(result.ImportInputs) > 0 {
		canonical["import-inputs"] = result.ImportInputs
	}

	return canonical
}

// marshalCanonicalJSON marshals a map to canonical JSON with sorted keys
func marshalCanonicalJSON(data map[string]any) (string, error) {
	// Use a custom encoder to ensure sorted keys
	return marshalSorted(data), nil
}

// marshalSorted recursively marshals data with sorted keys
func marshalSorted(data any) string {
	switch v := data.(type) {
	case map[string]any:
		if len(v) == 0 {
			return "{}"
		}

		// Sort keys
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		// Build JSON string with sorted keys
		var result strings.Builder
		result.WriteString("{")
		for i, key := range keys {
			if i > 0 {
				result.WriteString(",")
			}
			// Marshal the key
			keyJSON, err := json.Marshal(key)
			if err != nil {
				frontmatterHashLog.Printf("Warning: failed to marshal key %s: %v", key, err)
				continue
			}
			result.Write(keyJSON)
			result.WriteString(":")
			// Marshal the value recursively
			result.WriteString(marshalSorted(v[key]))
		}
		result.WriteString("}")
		return result.String()

	case []any:
		if len(v) == 0 {
			return "[]"
		}

		var result strings.Builder
		result.WriteString("[")
		for i, elem := range v {
			if i > 0 {
				result.WriteString(",")
			}
			result.WriteString(marshalSorted(elem))
		}
		result.WriteString("]")
		return result.String()

	case string, int, int64, float64, bool, nil:
		// Use standard JSON marshaling for primitives
		jsonBytes, err := json.Marshal(v)
		if err != nil {
			// This should rarely happen for primitives, but log it for debugging
			frontmatterHashLog.Printf("Warning: failed to marshal primitive value: %v", err)
			return "null"
		}
		return string(jsonBytes)

	default:
		// Fallback to standard JSON marshaling
		jsonBytes, err := json.Marshal(v)
		if err != nil {
			frontmatterHashLog.Printf("Warning: failed to marshal value of type %T: %v", v, err)
			return "null"
		}
		return string(jsonBytes)
	}
}

// ComputeFrontmatterHashFromFile computes the frontmatter hash for a workflow file
// using text-based approach (no YAML parsing) to match JavaScript implementation
func ComputeFrontmatterHashFromFile(filePath string, cache *ImportCache) (string, error) {
	return ComputeFrontmatterHashFromFileWithReader(filePath, cache, DefaultFileReader)
}

// ComputeFrontmatterHashFromFileWithReader computes the frontmatter hash for a workflow file
// using a custom file reader function (e.g., for GitHub API, in-memory file system, etc.)
func ComputeFrontmatterHashFromFileWithReader(filePath string, cache *ImportCache, fileReader FileReader) (string, error) {
	frontmatterHashLog.Printf("Computing hash for file: %s", filePath)

	// Read file content using the provided file reader
	content, err := fileReader(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	// Extract frontmatter and markdown as text (no YAML parsing)
	frontmatterText, markdown, err := extractFrontmatterAndBodyText(string(content))
	if err != nil {
		return "", fmt.Errorf("failed to extract frontmatter: %w", err)
	}

	// Get base directory for resolving imports
	baseDir := filepath.Dir(filePath)

	// Extract relevant template expressions from markdown body
	relevantExpressions := extractRelevantTemplateExpressions(markdown)

	// Compute hash using text-based approach with custom file reader
	return computeFrontmatterHashTextBasedWithReader(frontmatterText, markdown, baseDir, cache, relevantExpressions, fileReader)
}

// ComputeFrontmatterHashWithExpressions computes the hash including template expressions
func ComputeFrontmatterHashWithExpressions(frontmatter map[string]any, baseDir string, cache *ImportCache, expressions []string) (string, error) {
	frontmatterHashLog.Print("Computing frontmatter hash with template expressions")

	// Process imports to get merged frontmatter
	result, err := ProcessImportsFromFrontmatterWithManifest(frontmatter, baseDir, cache)
	if err != nil {
		return "", fmt.Errorf("failed to process imports: %w", err)
	}

	// Build the canonical frontmatter map
	canonical := buildCanonicalFrontmatter(frontmatter, result)

	// Add template expressions if present
	if len(expressions) > 0 {
		// Sort expressions for deterministic output
		sortedExpressions := make([]string, len(expressions))
		copy(sortedExpressions, expressions)
		sort.Strings(sortedExpressions)
		canonical["template-expressions"] = sortedExpressions
	}

	// Add version information for reproducibility
	canonical["versions"] = buildVersionInfo()

	// Serialize to canonical JSON
	canonicalJSON, err := marshalCanonicalJSON(canonical)
	if err != nil {
		return "", fmt.Errorf("failed to marshal canonical JSON: %w", err)
	}

	frontmatterHashLog.Printf("Canonical JSON length: %d bytes", len(canonicalJSON))

	// Compute SHA-256 hash
	hash := sha256.Sum256([]byte(canonicalJSON))
	hashHex := hex.EncodeToString(hash[:])

	frontmatterHashLog.Printf("Computed hash: %s", hashHex)
	return hashHex, nil
}

// buildVersionInfo builds version information for hash computation
func buildVersionInfo() map[string]string {
	versions := make(map[string]string)

	// gh-aw version (compiler version) - only include for release builds
	// This prevents hash changes during development when version is "dev"
	if isReleaseVersion {
		versions["gh-aw"] = compilerVersion
	}

	// awf (firewall) version
	versions["awf"] = string(constants.DefaultFirewallVersion)

	// agents (MCP gateway) version - also aliased as "gateway" for clarity
	gatewayVersion := string(constants.DefaultMCPGatewayVersion)
	versions["agents"] = gatewayVersion
	versions["gateway"] = gatewayVersion

	return versions
}

// extractRelevantTemplateExpressions extracts template expressions from markdown
// that reference env. or vars. contexts
func extractRelevantTemplateExpressions(markdown string) []string {
	var expressions []string
	seen := make(map[string]bool)

	// Regex to match ${{ ... }} expressions
	expressionRegex := regexp.MustCompile(`\$\{\{(.*?)\}\}`)
	matches := expressionRegex.FindAllStringSubmatch(markdown, -1)

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		content := strings.TrimSpace(match[1])

		// Check if expression references env. or vars.
		if strings.Contains(content, "env.") || strings.Contains(content, "vars.") {
			// Store the full expression including ${{ }}
			expr := match[0]
			// Deduplicate expressions
			if !seen[expr] {
				expressions = append(expressions, expr)
				seen[expr] = true
			}
		}
	}

	// Sort for deterministic output
	sort.Strings(expressions)
	return expressions
}

// extractFrontmatterAndBodyText extracts frontmatter as raw text without parsing YAML
// Returns: frontmatterText, markdownBody, error
func extractFrontmatterAndBodyText(content string) (string, string, error) {
	lines := strings.Split(content, "\n")

	// Check if content starts with frontmatter delimiter
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		// No frontmatter
		return "", content, nil
	}

	// Find end of frontmatter
	endIndex := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			endIndex = i
			break
		}
	}

	if endIndex == -1 {
		return "", "", fmt.Errorf("frontmatter not properly closed")
	}

	// Extract frontmatter text (lines between --- delimiters)
	frontmatterText := strings.Join(lines[1:endIndex], "\n")

	// Extract markdown body (everything after closing ---)
	var markdown string
	if endIndex+1 < len(lines) {
		markdown = strings.Join(lines[endIndex+1:], "\n")
	}

	return frontmatterText, markdown, nil
}

// normalizeFrontmatterText normalizes frontmatter text for consistent hashing
// Removes leading/trailing whitespace and normalizes line endings
func normalizeFrontmatterText(text string) string {
	// Normalize Windows line endings to Unix
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	// Trim leading and trailing whitespace
	return strings.TrimSpace(normalized)
}

// extractImportsFromText extracts import paths from frontmatter text using simple text parsing
// Only extracts array items under "imports:" key
func extractImportsFromText(frontmatterText string) []string {
	var imports []string
	lines := strings.Split(frontmatterText, "\n")

	inImports := false
	baseIndent := 0

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// Skip empty lines and comments
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Check if this is the imports: key
		if strings.HasPrefix(trimmed, "imports:") {
			inImports = true
			// Find the base indentation (position of first non-whitespace character)
			baseIndent = len(line) - len(strings.TrimLeft(line, " \t"))
			continue
		}

		if inImports {
			// Calculate current line's indentation
			lineIndent := len(line) - len(strings.TrimLeft(line, " \t"))

			// If indentation decreased or same level, we're out of the imports array
			if lineIndent <= baseIndent && trimmed != "" && !strings.HasPrefix(trimmed, "#") {
				break
			}

			// Extract array item
			if strings.HasPrefix(trimmed, "-") {
				item := strings.TrimSpace(trimmed[1:])
				// Remove quotes if present
				item = strings.Trim(item, `"'`)
				if item != "" {
					imports = append(imports, item)
				}
			}
		}
	}

	return imports
}

// processImportsTextBased processes imports from frontmatter using text-based parsing
// Returns: importedFiles (list of import paths), importedFrontmatterTexts (list of frontmatter texts)
func processImportsTextBased(frontmatterText, baseDir string, visited map[string]bool, fileReader FileReader) ([]string, []string, error) {
	var importedFiles []string
	var importedFrontmatterTexts []string

	// Extract imports from frontmatter text
	imports := extractImportsFromText(frontmatterText)

	if len(imports) == 0 {
		return importedFiles, importedFrontmatterTexts, nil
	}

	// Sort imports for deterministic processing
	sortedImports := make([]string, len(imports))
	copy(sortedImports, imports)
	sort.Strings(sortedImports)

	for _, importPath := range sortedImports {
		// Resolve import path relative to base directory
		fullPath := filepath.Join(baseDir, importPath)

		// Skip if already visited (cycle detection)
		if visited[fullPath] {
			continue
		}
		visited[fullPath] = true

		// Read imported file using the provided file reader
		content, err := fileReader(fullPath)
		if err != nil {
			// Skip missing imports silently (matches JavaScript behavior)
			continue
		}

		// Extract frontmatter text from imported file
		importFrontmatterText, _, err := extractFrontmatterAndBodyText(string(content))
		if err != nil {
			// Skip files with invalid frontmatter
			continue
		}

		// Add to imported files and texts
		importedFiles = append(importedFiles, importPath)
		importedFrontmatterTexts = append(importedFrontmatterTexts, importFrontmatterText)

		// Recursively process imports in the imported file
		importBaseDir := filepath.Dir(fullPath)
		nestedFiles, nestedTexts, err := processImportsTextBased(importFrontmatterText, importBaseDir, visited, fileReader)
		if err != nil {
			// Continue processing other imports even if one fails
			continue
		}

		// Add nested imports
		importedFiles = append(importedFiles, nestedFiles...)
		importedFrontmatterTexts = append(importedFrontmatterTexts, nestedTexts...)
	}

	return importedFiles, importedFrontmatterTexts, nil
}

// computeFrontmatterHashTextBasedWithReader computes the hash using text-based approach with custom file reader
func computeFrontmatterHashTextBasedWithReader(frontmatterText, markdown, baseDir string, cache *ImportCache, expressions []string, fileReader FileReader) (string, error) {
	frontmatterHashLog.Print("Computing frontmatter hash using text-based approach")

	// Process imports using text-based parsing with custom file reader
	visited := make(map[string]bool)
	importedFiles, importedFrontmatterTexts, err := processImportsTextBased(frontmatterText, baseDir, visited, fileReader)
	if err != nil {
		return "", fmt.Errorf("failed to process imports: %w", err)
	}

	// Build canonical representation from text
	canonical := make(map[string]any)

	// Add the main frontmatter text as-is (trimmed and normalized)
	canonical["frontmatter-text"] = normalizeFrontmatterText(frontmatterText)

	// Add sorted imported files list
	if len(importedFiles) > 0 {
		sortedImports := make([]string, len(importedFiles))
		copy(sortedImports, importedFiles)
		sort.Strings(sortedImports)
		canonical["imports"] = sortedImports
	}

	// Add sorted imported frontmatter texts (concatenated with delimiter)
	if len(importedFrontmatterTexts) > 0 {
		// Normalize and sort all imported texts
		normalizedTexts := make([]string, len(importedFrontmatterTexts))
		for i, text := range importedFrontmatterTexts {
			normalizedTexts[i] = normalizeFrontmatterText(text)
		}
		sort.Strings(normalizedTexts)
		canonical["imported-frontmatters"] = strings.Join(normalizedTexts, "\n---\n")
	}

	// Add template expressions if present
	if len(expressions) > 0 {
		canonical["template-expressions"] = expressions
	}

	// Add version information
	canonical["versions"] = buildVersionInfo()

	// Serialize to canonical JSON
	canonicalJSON, err := marshalCanonicalJSON(canonical)
	if err != nil {
		return "", fmt.Errorf("failed to marshal canonical JSON: %w", err)
	}

	frontmatterHashLog.Printf("Canonical JSON length: %d bytes", len(canonicalJSON))

	// Compute SHA-256 hash
	hash := sha256.Sum256([]byte(canonicalJSON))
	hashHex := hex.EncodeToString(hash[:])

	frontmatterHashLog.Printf("Computed hash: %s", hashHex)
	return hashHex, nil
}
