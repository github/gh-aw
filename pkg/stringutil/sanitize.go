package stringutil

import (
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var sanitizeLog = logger.New("stringutil:sanitize")

// Regex patterns for detecting potential secret key names
var (
	// Match uppercase snake_case identifiers that look like secret names (e.g., MY_SECRET_KEY, GITHUB_TOKEN, API_KEY)
	// Excludes common workflow-related keywords
	secretNamePattern = regexp.MustCompile(`\b([A-Z][A-Z0-9]*_[A-Z0-9_]+)\b`)

	// Match PascalCase identifiers ending with security-related suffixes (e.g., GitHubToken, ApiKey, DeploySecret)
	pascalCaseSecretPattern = regexp.MustCompile(`\b([A-Z][a-z0-9]*(?:[A-Z][a-z0-9]*)*(?:Token|Key|Secret|Password|Credential|Auth))\b`)

	// Common non-sensitive workflow keywords to exclude from redaction
	commonWorkflowKeywords = map[string]bool{
		"GITHUB":            true,
		"ACTIONS":           true,
		"WORKFLOW":          true,
		"RUNNER":            true,
		"JOB":               true,
		"STEP":              true,
		"MATRIX":            true,
		"ENV":               true,
		"PATH":              true,
		"HOME":              true,
		"SHELL":             true,
		"INPUTS":            true,
		"OUTPUTS":           true,
		"NEEDS":             true,
		"STRATEGY":          true,
		"CONCURRENCY":       true,
		"IF":                true,
		"WITH":              true,
		"USES":              true,
		"RUN":               true,
		"WORKING_DIRECTORY": true,
		"CONTINUE_ON_ERROR": true,
		"TIMEOUT_MINUTES":   true,
	}
)

// SanitizeErrorMessage removes potential secret key names from error messages to prevent
// information disclosure via logs. This prevents exposing details about an organization's
// security infrastructure by redacting secret key names that might appear in error messages.
func SanitizeErrorMessage(message string) string {
	if message == "" {
		return message
	}

	sanitizeLog.Printf("Sanitizing error message: length=%d", len(message))

	// Redact uppercase snake_case patterns (e.g., MY_SECRET_KEY, API_TOKEN)
	sanitized := secretNamePattern.ReplaceAllStringFunc(message, func(match string) string {
		// Don't redact common workflow keywords
		if commonWorkflowKeywords[match] {
			return match
		}
		sanitizeLog.Printf("Redacted snake_case secret pattern: %s", match)
		return "[REDACTED]"
	})

	// Redact PascalCase patterns ending with security suffixes (e.g., GitHubToken, ApiKey)
	sanitized = pascalCaseSecretPattern.ReplaceAllString(sanitized, "[REDACTED]")

	if sanitized != message {
		sanitizeLog.Print("Error message sanitization applied redactions")
	}

	return sanitized
}

// SanitizeParameterName converts a parameter name to a safe JavaScript identifier
// by replacing non-alphanumeric characters with underscores.
//
// This function ensures that parameter names from workflows can be used safely
// in JavaScript code by:
// 1. Replacing any non-alphanumeric characters (except $ and _) with underscores
// 2. Prepending an underscore if the name starts with a number
//
// Valid characters: a-z, A-Z, 0-9 (not at start), _, $
//
// Examples:
//
//	SanitizeParameterName("my-param")        // returns "my_param"
//	SanitizeParameterName("my.param")        // returns "my_param"
//	SanitizeParameterName("123param")        // returns "_123param"
//	SanitizeParameterName("valid_name")      // returns "valid_name"
//	SanitizeParameterName("$special")        // returns "$special"
func SanitizeParameterName(name string) string {
	// Replace dashes and other non-alphanumeric chars with underscores
	result := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '$' {
			return r
		}
		return '_'
	}, name)

	// Ensure it doesn't start with a number
	if len(result) > 0 && result[0] >= '0' && result[0] <= '9' {
		result = "_" + result
	}

	return result
}

// SanitizePythonVariableName converts a parameter name to a valid Python identifier
// by replacing non-alphanumeric characters with underscores.
//
// This function ensures that parameter names from workflows can be used safely
// in Python code by:
// 1. Replacing any non-alphanumeric characters (except _) with underscores
// 2. Prepending an underscore if the name starts with a number
//
// Valid characters: a-z, A-Z, 0-9 (not at start), _
// Note: Python does not allow $ in identifiers (unlike JavaScript)
//
// Examples:
//
//	SanitizePythonVariableName("my-param")        // returns "my_param"
//	SanitizePythonVariableName("my.param")        // returns "my_param"
//	SanitizePythonVariableName("123param")        // returns "_123param"
//	SanitizePythonVariableName("valid_name")      // returns "valid_name"
func SanitizePythonVariableName(name string) string {
	// Replace dashes and other non-alphanumeric chars with underscores
	result := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			return r
		}
		return '_'
	}, name)

	// Ensure it doesn't start with a number
	if len(result) > 0 && result[0] >= '0' && result[0] <= '9' {
		result = "_" + result
	}

	return result
}

// SanitizeToolID removes common MCP prefixes and suffixes from tool IDs.
// This cleans up tool identifiers by removing redundant MCP-related naming patterns.
//
// The function:
// 1. Removes "mcp-" prefix
// 2. Removes "-mcp" suffix
// 3. Returns the original ID if the result would be empty
//
// Examples:
//
//	SanitizeToolID("notion-mcp")        // returns "notion"
//	SanitizeToolID("mcp-notion")        // returns "notion"
//	SanitizeToolID("some-mcp-server")   // returns "some-server"
//	SanitizeToolID("github")            // returns "github" (unchanged)
//	SanitizeToolID("mcp")               // returns "mcp" (prevents empty result)
func SanitizeToolID(toolID string) string {
	cleaned := toolID

	// Remove "mcp-" prefix
	cleaned = strings.TrimPrefix(cleaned, "mcp-")

	// Remove "-mcp" suffix
	cleaned = strings.TrimSuffix(cleaned, "-mcp")

	// If the result is empty, use the original
	if cleaned == "" {
		return toolID
	}

	return cleaned
}
