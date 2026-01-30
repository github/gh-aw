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
// including template expressions that reference env. or vars. from the markdown body
func ComputeFrontmatterHashFromFile(filePath string, cache *ImportCache) (string, error) {
	frontmatterHashLog.Printf("Computing hash for file: %s", filePath)

	// Read file content
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	// Extract frontmatter
	result, err := ExtractFrontmatterFromContent(string(content))
	if err != nil {
		return "", fmt.Errorf("failed to extract frontmatter: %w", err)
	}

	// Get base directory for resolving imports
	baseDir := filepath.Dir(filePath)

	// Extract relevant template expressions from markdown body
	relevantExpressions := extractRelevantTemplateExpressions(result.Markdown)

	// Compute hash including template expressions
	return ComputeFrontmatterHashWithExpressions(result.Frontmatter, baseDir, cache, relevantExpressions)
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
			expressions = append(expressions, match[0])
		}
	}

	return expressions
}
