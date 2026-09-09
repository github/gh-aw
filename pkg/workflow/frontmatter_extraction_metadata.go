package workflow

import (
	"fmt"
	"maps"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/typeutil"
)

var frontmatterMetadataLog = logger.New("workflow:frontmatter_extraction_metadata")

// extractFeatures extracts the features field from frontmatter
// Returns a map of feature flags and configuration options (supports boolean flags and string values)
func (c *Compiler) extractFeatures(frontmatter map[string]any) map[string]any {
	frontmatterMetadataLog.Print("Extracting features from frontmatter")
	value, exists := frontmatter["features"]
	if !exists {
		frontmatterMetadataLog.Print("No features field found in frontmatter")
		return nil
	}

	// Features should be an object with any values (boolean or string)
	if featuresMap, ok := value.(map[string]any); ok {
		result := make(map[string]any)
		// Accept any value type (boolean, string, etc.)
		maps.Copy(result, featuresMap)
		frontmatterMetadataLog.Printf("Extracted %d features", len(result))
		return result
	}

	frontmatterMetadataLog.Print("Features field is not a map")
	return nil
}

// extractDescription extracts the description field from frontmatter
func (c *Compiler) extractDescription(frontmatter map[string]any) string {
	value, exists := frontmatter["description"]
	if !exists {
		return ""
	}

	// Convert the value to string
	if strValue, ok := value.(string); ok {
		desc := strings.TrimSpace(strValue)
		frontmatterMetadataLog.Printf("Extracted description: %d characters", len(desc))
		return desc
	}

	frontmatterMetadataLog.Printf("Description field is not a string: type=%T", value)
	return ""
}

// extractIntent extracts the intent field from frontmatter.
// intent captures the durable outcome the workflow exists to achieve (why it
// exists), while description captures what the workflow does.
func (c *Compiler) extractIntent(frontmatter map[string]any) string {
	value, exists := frontmatter["intent"]
	if !exists {
		return ""
	}

	// Convert the value to string
	if strValue, ok := value.(string); ok {
		intent := strings.TrimSpace(strValue)
		frontmatterMetadataLog.Printf("Extracted intent: %d characters", len(intent))
		return intent
	}

	frontmatterMetadataLog.Printf("Intent field is not a string: type=%T", value)
	return ""
}

// extractMetadataDocs extracts metadata.docs from frontmatter.
func (c *Compiler) extractMetadataDocs(frontmatter map[string]any) string {
	metadata, ok := frontmatter["metadata"].(map[string]any)
	if !ok {
		return ""
	}

	if strValue, ok := metadata["docs"].(string); ok {
		return strings.TrimSpace(strValue)
	}

	return ""
}

// extractSource extracts the source field from frontmatter
func (c *Compiler) extractSource(frontmatter map[string]any) string {
	value, exists := frontmatter["source"]
	if !exists {
		return ""
	}

	// Convert the value to string
	if strValue, ok := value.(string); ok {
		return strings.TrimSpace(strValue)
	}

	return ""
}

// extractRedirect extracts the redirect field from frontmatter
func (c *Compiler) extractRedirect(frontmatter map[string]any) string {
	value, exists := frontmatter["redirect"]
	if !exists {
		return ""
	}

	// Convert the value to string
	if strValue, ok := value.(string); ok {
		return strings.TrimSpace(strValue)
	}

	return ""
}

// extractTrackerID extracts and validates the tracker-id field from frontmatter
func (c *Compiler) extractTrackerID(frontmatter map[string]any) (string, error) {
	value, exists := frontmatter["tracker-id"]
	if !exists {
		return "", nil
	}

	frontmatterMetadataLog.Print("Extracting and validating tracker-id")

	// Convert the value to string
	strValue, ok := value.(string)
	if !ok {
		frontmatterMetadataLog.Printf("Invalid tracker-id type: %T", value)
		return "", fmt.Errorf("tracker-id must be a string, got %T. Example: tracker-id: \"my-tracker-123\"", value)
	}

	trackerID := strings.TrimSpace(strValue)

	// Validate minimum length
	if len(trackerID) < 8 {
		frontmatterMetadataLog.Printf("tracker-id too short: %d characters", len(trackerID))
		return "", fmt.Errorf("tracker-id is %d characters long, expected at least 8 characters. Example: tracker-id: \"my-tracker-123\"", len(trackerID))
	}

	// Validate maximum length
	if len(trackerID) > 128 {
		frontmatterMetadataLog.Printf("tracker-id too long: %d characters", len(trackerID))
		return "", fmt.Errorf("tracker-id is %d characters long, expected at most 128 characters. Example: tracker-id: \"my-tracker-123\"", len(trackerID))
	}

	// Validate that it's a valid identifier (alphanumeric, hyphens, underscores)
	for i, char := range trackerID {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') &&
			(char < '0' || char > '9') && char != '-' && char != '_' {
			frontmatterMetadataLog.Printf("Invalid character in tracker-id at position %d", i+1)
			return "", fmt.Errorf("tracker-id has unsupported character '%c' at position %d, expected only alphanumeric characters, hyphens, and underscores. Example: tracker-id: \"my-tracker-123\"", char, i+1)
		}
	}

	frontmatterMetadataLog.Printf("Successfully validated tracker-id: %s", trackerID)
	return trackerID, nil
}

// buildSourceURL converts a source string (owner/repo/path@ref) to a GitHub URL
// For enterprise deployments, the URL will use the GitHub server URL from the workflow context
func buildSourceURL(source string) string {
	frontmatterMetadataLog.Printf("Building source URL from: %s", source)
	if source == "" {
		return ""
	}

	// Parse the source string: owner/repo/path@ref
	parts := strings.Split(source, "@")
	if len(parts) == 0 {
		return ""
	}

	pathPart := parts[0] // "owner/repo/path"
	refPart := "main"    // default ref
	if len(parts) > 1 {
		refPart = parts[1]
	}

	// Build GitHub URL using server URL from GitHub Actions context
	// The pathPart is "owner/repo/workflows/file.md", we need to convert it to
	// "${GITHUB_SERVER_URL}/owner/repo/blob/ref/workflows/file.md"
	// Using /blob/ renders the markdown file (rendered view) instead of /tree/ (directory listing)
	pathComponents := strings.SplitN(pathPart, "/", 3)
	if len(pathComponents) < 3 {
		frontmatterMetadataLog.Printf("Invalid source path format: %s (expected owner/repo/path)", pathPart)
		return ""
	}

	owner := pathComponents[0]
	repo := pathComponents[1]
	filePath := pathComponents[2]

	url := fmt.Sprintf("${{ github.server_url }}/%s/%s/blob/%s/%s", owner, repo, refPart, filePath)
	frontmatterMetadataLog.Printf("Built source URL: %s/%s blob %s", owner, repo, refPart)
	// Use github.server_url for enterprise GitHub deployments
	return url
}

// buildLocalWorkflowSourceURL builds a GitHub URL for a local workflow markdown file.
// It uses github.server_url, github.repository, and github.ref_name expressions so the URL
// is resolved correctly at runtime, including on GitHub Enterprise Server.
// Returns an empty string when the markdown path does not contain a ".github/" directory
// component (e.g. for temporary test files), since a valid repo-relative URL cannot be derived.
func buildLocalWorkflowSourceURL(markdownPath string) string {
	if markdownPath == "" {
		return ""
	}

	// Normalise path separators to forward slashes for consistent matching.
	normalised := filepath.ToSlash(markdownPath)

	// Extract the repo-relative path by finding the ".github/" directory component.
	// Use LastIndex so paths like "/username.github.io/.github/workflows/x.md" resolve
	// to ".github/workflows/x.md" rather than the outer username.github.io portion.
	const githubDirPattern = "/.github/"
	idx := strings.LastIndex(normalised, githubDirPattern)
	var relPath string
	if idx != -1 {
		// Skip the leading slash to get ".github/workflows/...".
		relPath = normalised[idx+1:]
	} else if strings.HasPrefix(normalised, constants.GithubDir) {
		// Already a relative path starting with ".github/".
		relPath = normalised
	} else {
		// Non-standard path (e.g. /tmp/test.md) — cannot derive a valid URL.
		return ""
	}

	url := "${{ github.server_url }}/${{ github.repository }}/blob/${{ github.ref_name }}/" + relPath
	frontmatterMetadataLog.Printf("Built local workflow source URL for %s", relPath)
	return url
}

// extractToolsTimeout extracts the timeout setting from tools
// Returns "" if not set (engines will use their own defaults)
// Returns error if timeout is explicitly set but invalid (< 1 for literals, or non-expression string)
func (c *Compiler) extractToolsTimeout(tools map[string]any) (string, error) {
	if tools == nil {
		return "", nil // Use engine defaults
	}

	// Check if timeout is explicitly set in tools
	if timeoutValue, exists := tools["timeout"]; exists {
		frontmatterMetadataLog.Printf("Extracting tools.timeout value: type=%T", timeoutValue)
		// Handle GitHub Actions expression strings
		if strVal, ok := timeoutValue.(string); ok {
			if isExpression(strVal) {
				frontmatterMetadataLog.Printf("Extracted tools.timeout as expression: %s", strVal)
				return strVal, nil
			}
			frontmatterMetadataLog.Printf("Invalid tools.timeout string (not an expression): %s", strVal)
			return "", fmt.Errorf("tools.timeout must be an integer or a GitHub Actions expression (e.g. '${{ inputs.tool-timeout }}'), got string %q", strVal)
		}
		// Handle different numeric types with safe conversions to prevent overflow
		var timeout int
		switch v := timeoutValue.(type) {
		case int:
			timeout = v
		case int64:
			timeout = int(v)
		case uint:
			timeout = typeutil.SafeUintToInt(v) // Safe conversion to prevent overflow (alert #418)
		case uint64:
			timeout = typeutil.SafeUint64ToInt(v) // Safe conversion to prevent overflow (alert #416)
		case float64:
			timeout = int(v)
		default:
			frontmatterMetadataLog.Printf("Invalid tools.timeout type: %T", timeoutValue)
			return "", fmt.Errorf("tools.timeout has type %T, expected an integer or a GitHub Actions expression. Example:\ntools:\n  timeout: 60", timeoutValue)
		}

		// Validate minimum value per schema constraint
		if timeout < 1 {
			frontmatterMetadataLog.Printf("Invalid tools.timeout value: %d (must be >= 1)", timeout)
			return "", fmt.Errorf("tools.timeout must be at least 1 second, got %d. Example:\ntools:\n  timeout: 60", timeout)
		}

		frontmatterMetadataLog.Printf("Extracted tools.timeout: %d seconds", timeout)
		return strconv.Itoa(timeout), nil
	}

	// Default to "" (use engine defaults)
	return "", nil
}

// extractToolsStartupTimeout extracts the startup-timeout setting from tools
// Returns "" if not set (engines will use their own defaults)
// Returns error if startup-timeout is explicitly set but invalid (< 1 for literals, or non-expression string)
func (c *Compiler) extractToolsStartupTimeout(tools map[string]any) (string, error) {
	if tools == nil {
		return "", nil // Use engine defaults
	}

	// Check if startup-timeout is explicitly set in tools
	if timeoutValue, exists := tools["startup-timeout"]; exists {
		// Handle GitHub Actions expression strings
		if strVal, ok := timeoutValue.(string); ok {
			if isExpression(strVal) {
				return strVal, nil
			}
			return "", fmt.Errorf("tools.startup-timeout must be an integer or a GitHub Actions expression (e.g. '${{ inputs.startup-timeout }}'), got string %q", strVal)
		}
		var timeout int
		// Handle different numeric types with safe conversions to prevent overflow
		switch v := timeoutValue.(type) {
		case int:
			timeout = v
		case int64:
			timeout = int(v)
		case uint:
			timeout = typeutil.SafeUintToInt(v) // Safe conversion to prevent overflow (alert #417)
		case uint64:
			timeout = typeutil.SafeUint64ToInt(v) // Safe conversion to prevent overflow (alert #415)
		case float64:
			timeout = int(v)
		default:
			return "", fmt.Errorf("tools.startup-timeout has type %T, expected an integer or a GitHub Actions expression. Example:\ntools:\n  startup-timeout: 120", timeoutValue)
		}

		// Validate minimum value per schema constraint
		if timeout < 1 {
			return "", fmt.Errorf("tools.startup-timeout must be at least 1 second, got %d. Example:\ntools:\n  startup-timeout: 120", timeout)
		}

		return strconv.Itoa(timeout), nil
	}

	// Default to "" (use engine defaults)
	return "", nil
}

// extractToolsMapFromFrontmatter extracts tools section from frontmatter map
func extractToolsMapFromFrontmatter(frontmatter map[string]any) map[string]any {
	return ExtractMapField(frontmatter, "tools")
}

// extractMCPServersMapFromFrontmatter extracts mcp-servers section from frontmatter
func extractMCPServersMapFromFrontmatter(frontmatter map[string]any) map[string]any {
	return ExtractMapField(frontmatter, "mcp-servers")
}

// extractRuntimesMapFromFrontmatter extracts runtimes section from frontmatter map
func extractRuntimesMapFromFrontmatter(frontmatter map[string]any) map[string]any {
	return ExtractMapField(frontmatter, "runtimes")
}
