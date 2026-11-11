package parser

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
)

var schemaErrorsLog = logger.New("parser:schema_errors")

// atPathPattern matches "- at '/path': " or "at '/path': " prefixes in error messages
var atPathPattern = regexp.MustCompile(`^-?\s*at '([^']*)': (.+)$`)

// minConstraintPattern matches "minimum: got X, want Y" messages from the jsonschema library
var minConstraintPattern = regexp.MustCompile(`^minimum: got (-?\d+(?:\.\d+)?), want (-?\d+(?:\.\d+)?)$`)

// maxConstraintPattern matches "maximum: got X, want Y" messages from the jsonschema library
var maxConstraintPattern = regexp.MustCompile(`^maximum: got (-?\d+(?:\.\d+)?), want (-?\d+(?:\.\d+)?)$`)

// translateSchemaConstraintMessage rewrites jsonschema range-constraint messages into plain English.
//
// Examples:
//   - "minimum: got -45, want 1" → "must be at least 1 (got -45)"
//   - "maximum: got 120, want 60" → "must be at most 60 (got 120)"
func translateSchemaConstraintMessage(message string) string {
	if m := minConstraintPattern.FindStringSubmatch(message); len(m) == 3 {
		schemaErrorsLog.Printf("Translating minimum constraint message: got=%s want=%s", m[1], m[2])
		return fmt.Sprintf("must be at least %s (got %s)", m[2], m[1])
	}
	if m := maxConstraintPattern.FindStringSubmatch(message); len(m) == 3 {
		schemaErrorsLog.Printf("Translating maximum constraint message: got=%s want=%s", m[1], m[2])
		return fmt.Sprintf("must be at most %s (got %s)", m[2], m[1])
	}
	return message
}

// cleanJSONSchemaErrorMessage removes unhelpful prefixes from jsonschema validation errors
func cleanJSONSchemaErrorMessage(errorMsg string) string {
	schemaErrorsLog.Printf("Cleaning JSON schema error message (%d chars)", len(errorMsg))
	// Split the error message into lines
	lines := strings.Split(errorMsg, "\n")

	var cleanedLines []string
	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Skip the "jsonschema validation failed" line entirely
		if strings.HasPrefix(line, "jsonschema validation failed") {
			continue
		}

		// Remove the unhelpful "- at '': " prefix from error descriptions
		line = strings.TrimPrefix(line, "- at '': ")

		// Keep non-empty lines that have actual content
		if line != "" {
			cleanedLines = append(cleanedLines, line)
		}
	}

	// Join the cleaned lines back together
	result := strings.Join(cleanedLines, "\n")

	// If we have no meaningful content left, return a generic message
	if strings.TrimSpace(result) == "" {
		return "schema validation failed"
	}

	// Apply oneOf cleanup to the full cleaned message
	return cleanOneOfMessage(result)
}

// cleanOneOfMessage simplifies 'oneOf failed, none matched' error messages by:
// 1. Removing "got X, want Y" type-mismatch lines (from the wrong branch of a oneOf)
// 2. Removing the "oneOf failed, none matched" wrapper line
// 3. Extracting the most meaningful sub-error (e.g., enum constraint violations)
//
// This converts confusing schema jargon like:
//
//	"'oneOf' failed, none matched\n- at '/engine': value must be one of...\n- at '/engine': got string, want object"
//
// into plain language:
//
//	"value must be one of 'claude', 'codex', 'copilot', 'gemini'"
func cleanOneOfMessage(message string) string {
	if !strings.Contains(message, "'oneOf' failed") {
		return message
	}

	schemaErrorsLog.Printf("Simplifying oneOf error message (%d lines)", strings.Count(message, "\n")+1)
	lines := strings.Split(message, "\n")
	var meaningful []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Skip the "oneOf failed" wrapper line — it's schema jargon, not user guidance
		if strings.Contains(trimmed, "'oneOf' failed, none matched") {
			continue
		}
		// Skip "got X, want Y" type-mismatch lines from the wrong oneOf branch
		if isTypeConflictLine(trimmed) {
			continue
		}
		meaningful = append(meaningful, trimmed)
	}

	if len(meaningful) == 0 {
		// All sub-errors were type conflicts — synthesize a plain-English message
		// instead of returning raw JSON Schema jargon.
		return synthesizeOneOfTypeConflictMessage(lines)
	}

	// Strip "- at '/path':" prefixes and format each remaining constraint
	var cleaned []string
	for _, line := range meaningful {
		cleaned = append(cleaned, stripAtPathPrefix(line))
	}

	return strings.Join(cleaned, "; ")
}

// typeConflictGotWantPattern extracts "got X, want Y" components from type-conflict lines.
// Matches both bare "got X, want Y" and embedded "- at '/path': got X, want Y" forms.
var typeConflictGotWantPattern = regexp.MustCompile(`(?:^|: )got (\w+), want (\w+)$`)

// knownOneOfFieldHints provides field-specific guidance for oneOf type-conflict fallback
// messages. When all oneOf branches fail with type-mismatch errors (e.g., the user passes
// an integer where a string or object is expected), these hints are appended to the
// synthesized plain-English message to help the user fix the problem.
//
// The engine list mirrors the built-in engines in NewEngineCatalog.
// Update this list when built-in engines change.
var knownOneOfFieldHints = map[string]string{
	"/engine":                "Valid engine names: claude, codex, copilot, gemini, pi.\n\nExample:\nengine: copilot\n# or with options:\nengine:\n  id: copilot\nmax-turns: 15  # top-level field, not nested under engine",
	"/tools/github/toolsets": "Valid toolsets: all, default, action-friendly, context, repos, issues, pull_requests, actions, code_security, dependabot, discussions, experiments, gists, labels, notifications, orgs, projects, search, secret_protection, security_advisories, stargazers, users.\n\nExample:\ntools:\n  github:\n    toolsets: default\n    # or as an array:\n    toolsets: [default, repos]",
}

// synthesizeOneOfTypeConflictMessage produces a plain-English error message when every
// sub-error of a oneOf constraint is a type conflict (e.g., "got number, want string"
// and "got number, want object"). It extracts the actual and expected types from the
// conflict lines and, for well-known fields, appends guidance with valid values.
func synthesizeOneOfTypeConflictMessage(lines []string) string {
	var gotType string
	var wantTypes []string
	var path string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !isTypeConflictLine(trimmed) {
			continue
		}
		// Extract path from "- at '/path': got X, want Y"
		if match := atPathPattern.FindStringSubmatch(trimmed); match != nil {
			if path == "" {
				path = match[1]
			}
		}
		// Extract got/want types
		if match := typeConflictGotWantPattern.FindStringSubmatch(trimmed); match != nil {
			if gotType == "" {
				gotType = match[1]
			}
			wantTypes = append(wantTypes, match[2])
		}
	}

	if gotType == "" || len(wantTypes) == 0 {
		return "schema validation failed"
	}

	// Deduplicate expected types (e.g., multiple "object" branches in oneOf)
	seen := make(map[string]struct {
	})
	var uniqueWantTypes []string
	for _, t := range wantTypes {
		if !setutil.Contains(seen, t) {
			seen[t] = struct {
			}{}
			uniqueWantTypes = append(uniqueWantTypes, t)
		}
	}

	result := fmt.Sprintf("expected %s, got %s", strings.Join(uniqueWantTypes, " or "), gotType)

	// Add field-specific hints for known fields
	if hint, ok := knownOneOfFieldHints[path]; ok {
		result += ". " + hint
	}

	return result
}

// jsonTypeNames is the set of valid JSON Schema type names. Used to distinguish
// actual type conflicts ("got number, want string") from constraint violations
// ("minItems: got 0, want 1") in oneOf error messages.
var jsonTypeNames = map[string]bool{
	"string": true, "object": true, "array": true, "number": true,
	"integer": true, "boolean": true, "null": true,
}

// typeConflictPattern matches "got TYPE, want TYPE" where TYPE must be a JSON type name.
// This avoids false positives on constraint violations like "minItems: got 0, want 1".
var typeConflictPattern = regexp.MustCompile(`got (\w+), want (\w+)`)

// isTypeConflictLine returns true for "got X, want Y" lines that arise from the
// wrong branch of a oneOf constraint. These lines are generated when the user's value
// matches one branch's type but not the other, and they are confusing to display.
// Handles both bare "got X, want Y" and embedded "- at '/path': got X, want Y" forms.
//
// Only matches when both X and Y are JSON Schema type names (string, object, array,
// number, integer, boolean, null), to avoid misidentifying constraint violations
// (e.g., "minItems: got 0, want 1") as type conflicts.
func isTypeConflictLine(line string) bool {
	// Fast-path: skip regex for lines that clearly aren't type conflicts
	if !strings.Contains(line, "got ") || !strings.Contains(line, ", want ") {
		return false
	}
	match := typeConflictPattern.FindStringSubmatch(line)
	if match == nil {
		return false
	}
	return jsonTypeNames[match[1]] && jsonTypeNames[match[2]]
}

// stripAtPathPrefix removes "- at '/path': " or "at '/path': " prefixes from schema error lines
// and formats nested path references to be more readable.
//
// Examples:
//   - "- at '/engine': value must be one of..." → "value must be one of..."
//   - "- at '/permissions/deployments': value must be..." → "'deployments': value must be..."
func stripAtPathPrefix(line string) string {
	match := atPathPattern.FindStringSubmatch(line)
	if match == nil {
		return line
	}
	path := match[1]
	msg := match[2]

	// For nested paths (e.g., /permissions/deployments), keep the last component
	// so users know which sub-field has the error
	if idx := strings.LastIndex(path, "/"); idx > 0 {
		subField := path[idx+1:]
		return fmt.Sprintf("'%s': %s", subField, msg)
	}

	// For top-level field errors, just return the constraint message
	return msg
}

// findFrontmatterBounds finds the start and end indices of frontmatter in file lines
// Returns: startIdx (-1 if not found), endIdx (-1 if not found), frontmatterContent
func findFrontmatterBounds(lines []string) (startIdx int, endIdx int, frontmatterContent string) {
	schemaErrorsLog.Printf("Finding frontmatter bounds in %d lines", len(lines))
	startIdx = -1
	endIdx = -1

	// Look for the opening "---"
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			startIdx = i
			break
		}
		// Skip empty lines and comments at the beginning
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			// Found non-empty, non-comment line before "---" - no frontmatter
			return -1, -1, ""
		}
	}

	if startIdx == -1 {
		schemaErrorsLog.Print("No frontmatter opening delimiter found")
		return -1, -1, ""
	}

	// Look for the closing "---"
	for i := startIdx + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "---" {
			endIdx = i
			break
		}
	}

	if endIdx == -1 {
		// No closing "---" found
		schemaErrorsLog.Print("No frontmatter closing delimiter found")
		return -1, -1, ""
	}
	schemaErrorsLog.Printf("Found frontmatter bounds: start=%d end=%d", startIdx, endIdx)

	// Extract frontmatter content between the markers
	frontmatterLines := lines[startIdx+1 : endIdx]
	frontmatterContent = strings.Join(frontmatterLines, "\n")

	return startIdx, endIdx, frontmatterContent
}

// knownFieldValidValues maps well-known JSON schema paths to a human-readable description
// of the valid values / children for that field. Used to append helpful hints when an
// additionalProperties error occurs on these fields so users quickly know what is allowed.
//
// Both /permissions and /on/permissions mirror #/$defs/github_actions_permissions in
// main_workflow_schema.json. Update this list when the schema changes.
var knownFieldValidValues = map[string]string{
	// Both entries mirror $defs/github_actions_permissions in main_workflow_schema.json.
	// Update both when the schema changes.
	"/permissions":    "Valid permission scopes: actions, all, attestations, checks, code-quality, copilot-requests, contents, deployments, discussions, id-token, issues, metadata, models, organization-custom-org-roles, organization-custom-repository-roles, organization-projects, packages, pages, pull-requests, repository-projects, security-events, statuses, vulnerability-alerts",
	"/on/permissions": "Valid permission scopes: actions, all, attestations, checks, code-quality, copilot-requests, contents, deployments, discussions, id-token, issues, metadata, models, organization-custom-org-roles, organization-custom-repository-roles, organization-projects, packages, pages, pull-requests, repository-projects, security-events, statuses, vulnerability-alerts",
}

// knownFieldScopes maps well-known JSON schema paths to a slice of valid scope names.
// This enables spell-check ("Did you mean?") suggestions for unknown-property errors.
//
// Both /permissions and /on/permissions mirror #/$defs/github_actions_permissions in
// main_workflow_schema.json. Update this list when the schema changes.
var knownFieldScopes = map[string][]string{
	"/permissions": {
		"actions", "all", "attestations", "checks", "code-quality", "copilot-requests", "contents", "deployments",
		"discussions", "id-token", "issues", "metadata", "models",
		"organization-custom-org-roles", "organization-custom-repository-roles",
		"organization-projects", "packages", "pages", "pull-requests",
		"repository-projects", "security-events", "statuses", "vulnerability-alerts",
	},
	"/on/permissions": {
		"actions", "all", "attestations", "checks", "code-quality", "copilot-requests", "contents", "deployments",
		"discussions", "id-token", "issues", "metadata", "models",
		"organization-custom-org-roles", "organization-custom-repository-roles",
		"organization-projects", "packages", "pages", "pull-requests",
		"repository-projects", "security-events", "statuses", "vulnerability-alerts",
	},
}

// knownFieldDocs maps well-known JSON schema paths to documentation URLs.
var knownFieldDocs = map[string]string{
	"/permissions":    "https://docs.github.com/en/actions/writing-workflows/choosing-what-your-workflow-does/controlling-permissions-for-github_token",
	"/on/permissions": "https://docs.github.com/en/actions/writing-workflows/choosing-what-your-workflow-does/controlling-permissions-for-github_token",
}

// unknownPropertyPattern extracts the property name(s) from a rewritten "Unknown property(ies):" message.
var unknownPropertyPattern = regexp.MustCompile(`(?i)^Unknown propert(?:y|ies): (.+)$`)

// appendKnownFieldValidValuesHint appends a "Valid values: …" hint, "Did you mean?" suggestions,
// and a documentation link to message when the jsonPath matches a well-known field and the
// message is an unknown-property error.
// It returns the message unchanged for unknown paths or non-additional-properties messages.
// The second return value is true if a hint was actually appended to the message.
func appendKnownFieldValidValuesHint(message string, jsonPath string) (string, bool) {
	// Use truncated prefix "unknown propert" to match both singular ("Unknown property")
	// and plural ("Unknown properties") forms produced by rewriteAdditionalPropertiesError.
	if !strings.Contains(strings.ToLower(message), "unknown propert") {
		return message, false
	}
	schemaErrorsLog.Printf("Appending known field hint for path: %s", jsonPath)

	hint, scopes, docsURL, hintOK := knownFieldHintForPath(jsonPath)
	if !hintOK {
		return message, false
	}

	result := message + " (" + hint + ")"
	result = appendKnownFieldSuggestions(result, message, scopes)
	result = appendKnownFieldDocsURL(result, docsURL)
	return result, true
}

func knownFieldHintForPath(jsonPath string) (string, []string, string, bool) {
	if hint, ok := knownFieldValidValues[jsonPath]; ok {
		return hint, knownFieldScopes[jsonPath], knownFieldDocs[jsonPath], true
	}
	bestPath := findBestKnownFieldParentPath(jsonPath)
	if bestPath == "" {
		return "", nil, "", false
	}
	return knownFieldValidValues[bestPath], knownFieldScopes[bestPath], knownFieldDocs[bestPath], true
}

func findBestKnownFieldParentPath(jsonPath string) string {
	bestPath := ""
	bestLen := 0
	for path := range knownFieldValidValues {
		if strings.HasPrefix(jsonPath, path+"/") {
			if l := len(path); l > bestLen {
				bestLen = l
				bestPath = path
			}
		}
	}
	return bestPath
}

func appendKnownFieldSuggestions(result, message string, scopes []string) string {
	if len(scopes) == 0 {
		return result
	}
	m := unknownPropertyPattern.FindStringSubmatch(message)
	if len(m) != 2 {
		return result
	}

	unknownProps := strings.Split(m[1], ", ")
	unique := uniqueClosestScopeSuggestions(unknownProps, scopes)
	if len(unique) == 1 {
		return fmt.Sprintf("%s. Did you mean '%s'?", result, unique[0])
	}
	if len(unique) > 1 {
		return fmt.Sprintf("%s. Did you mean: %s?", result, strings.Join(unique, ", "))
	}
	return result
}

func uniqueClosestScopeSuggestions(unknownProps []string, scopes []string) []string {
	var allSuggestions []string
	for _, prop := range unknownProps {
		prop = strings.TrimSpace(prop)
		if prop == "" {
			continue
		}
		allSuggestions = append(allSuggestions, FindClosestMatches(prop, scopes, maxClosestMatches)...)
	}
	seen := make(map[string]struct {
	})
	var unique []string
	for _, s := range allSuggestions {
		if !setutil.Contains(seen, s) {
			seen[s] = struct {
			}{}
			unique = append(unique, s)
		}
	}
	return unique
}

func appendKnownFieldDocsURL(result, docsURL string) string {
	if docsURL == "" {
		return result
	}
	return fmt.Sprintf("%s See: %s", result, docsURL)
}

// rewriteAdditionalPropertiesError rewrites "additional properties not allowed" errors to be more user-friendly
func rewriteAdditionalPropertiesError(message string) string {
	// Check if this is an "additional properties not allowed" error
	if strings.Contains(strings.ToLower(message), "additional propert") && strings.Contains(strings.ToLower(message), "not allowed") {
		// Extract property names from the message using regex
		match := additionalPropertiesPattern.FindStringSubmatch(message)

		if len(match) >= 2 {
			properties := normalizeAdditionalPropertyList(match[1])
			schemaErrorsLog.Printf("Rewriting additional properties error: %s", properties)

			if strings.Contains(properties, ",") {
				return "Unknown properties: " + properties
			} else {
				return "Unknown property: " + properties
			}
		}
	}

	return message
}

// normalizeAdditionalPropertyList strips quotes, trims whitespace, and sorts the
// comma-separated property names so that diagnostics are deterministic regardless
// of the order in which the schema validator emits them.
func normalizeAdditionalPropertyList(raw string) string {
	raw = strings.ReplaceAll(raw, "'", "")
	parts := strings.Split(raw, ",")
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	sort.Strings(cleaned)
	return strings.Join(cleaned, ", ")
}
