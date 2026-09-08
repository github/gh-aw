package workflow

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
)

var mcpRendererSectionHelpersLog = logger.New("workflow:mcp_renderer_section_helpers")

func writeJSONStringMapEntries(yaml *strings.Builder, values map[string]string, indent string) {
	for i, key := range sliceutil.SortedKeys(values) {
		comma := ","
		if i == len(values)-1 {
			comma = ""
		}
		fmt.Fprintf(yaml, "%s%s: %s%s\n", indent, mustMarshalJSONString(key), mustMarshalJSONString(values[key]), comma)
	}
}

// writeJSONStringMapEntriesRaw writes JSON object entries for unquoted heredoc MCP
// config sections where values may contain pre-escaped shell placeholders such as
// \${VAR}.  Keys are JSON-encoded normally.  Values are JSON-encoded first (so that
// plain chars like `"`, `\`, or newlines remain safe), then the one double-escape
// that json.Marshal introduces for shell placeholders is undone: the sequence `\\${`
// (json.Marshal encoding of the single-backslash placeholder prefix `\${`) is
// restored to `\${` so that bash processes the unquoted heredoc correctly — turning
// `\$` into `$` — and delivers `${VAR}` to the MCP gateway for env-var expansion.
func writeJSONStringMapEntriesRaw(yaml *strings.Builder, values map[string]string, indent string) {
	keys := sliceutil.SortedKeys(values)
	for i, key := range keys {
		comma := ","
		if i == len(keys)-1 {
			comma = ""
		}
		// JSON-encode to handle special characters (quotes, newlines, etc.),
		// then undo json.Marshal's double-escaping of pre-formed shell placeholders.
		// json.Marshal turns \${VAR} into \\${VAR}; restore to the single-backslash
		// form so the unquoted heredoc produces ${VAR} for the MCP gateway.
		encoded := mustMarshalJSONString(values[key])
		encoded = strings.ReplaceAll(encoded, `\\${`, `\${`)
		fmt.Fprintf(yaml, "%s%s: %s%s\n", indent, mustMarshalJSONString(key), encoded, comma)
	}
}

// writeJSONStringMapSectionWithEntries writes a JSON object section, delegating
// per-entry serialisation to writeEntries.  writeEntries receives the builder,
// the values map, and the child indent string (indent+"  ").
func writeJSONStringMapSectionWithEntries(yaml *strings.Builder, indent, name string, values map[string]string, trailingComma bool, writeEntries func(*strings.Builder, map[string]string, string)) {
	fmt.Fprintf(yaml, "%s\"%s\": {\n", indent, name)
	writeEntries(yaml, values, indent+"  ")
	if trailingComma {
		fmt.Fprintf(yaml, "%s},\n", indent)
		return
	}
	fmt.Fprintf(yaml, "%s}\n", indent)
}

// writeJSONStringMapSectionRaw writes a JSON object section using writeJSONStringMapEntriesRaw.
// Use this for env and headers sections in unquoted heredoc MCP configs.  Values are
// JSON-encoded (so special chars remain safe) with the double-escape of shell
// placeholders (\${VAR} → \\${VAR}) undone so bash can process them correctly.
func writeJSONStringMapSectionRaw(yaml *strings.Builder, indent, name string, values map[string]string, trailingComma bool) {
	writeJSONStringMapSectionWithEntries(yaml, indent, name, values, trailingComma, writeJSONStringMapEntriesRaw)
}

func mustMarshalJSONString(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "\"\""
	}
	return string(encoded)
}

func writeJSONStringMapSection(yaml *strings.Builder, indent, name string, values map[string]string, trailingComma bool) {
	writeJSONStringMapSectionWithEntries(yaml, indent, name, values, trailingComma, writeJSONStringMapEntries)
}

func writeTOMLInlineStringMapSection(yaml *strings.Builder, indent, name string, values map[string]string) {
	fmt.Fprintf(yaml, "%s%s = { ", indent, name)
	for i, key := range sliceutil.SortedKeys(values) {
		if i > 0 {
			yaml.WriteString(", ")
		}
		fmt.Fprintf(yaml, "\"%s\" = \"%s\"", key, values[key])
	}
	yaml.WriteString(" }\n")
}

// buildGitHubMCPEnvVars builds the common GitHub MCP environment map used by
// local, remote, and TOML renderers.
//
// hostValue should be a full URL (for example https://hostname, with no
// trailing slash) because github-mcp-server expects GITHUB_HOST in the same
// format that GitHub Actions exposes via GITHUB_SERVER_URL (for example
// https://github.com or https://myorg.ghe.com).
func buildGitHubMCPEnvVars(tokenValue, hostValue string, readOnly, lockdown bool, toolsets, features string) map[string]string {
	envVars := map[string]string{
		"GITHUB_PERSONAL_ACCESS_TOKEN": tokenValue,
		"GITHUB_HOST":                  hostValue,
	}

	if readOnly {
		envVars["GITHUB_READ_ONLY"] = "1"
	}

	if lockdown {
		envVars["GITHUB_LOCKDOWN_MODE"] = "1"
	}

	if toolsets != "" {
		envVars["GITHUB_TOOLSETS"] = toolsets
	}

	if features != "" {
		envVars["GITHUB_FEATURES"] = features
	}

	// Note: tokenValue is a secret and is intentionally not logged.
	mcpRendererSectionHelpersLog.Printf("Built GitHub MCP env vars: host=%s, readOnly=%v, lockdown=%v, hasToolsets=%v, hasFeatures=%v", hostValue, readOnly, lockdown, toolsets != "", features != "")

	return envVars
}

func buildGitHubMCPRemoteHeaders(authValue string, readOnly, lockdown bool, toolsets, features string) map[string]string {
	headers := map[string]string{
		"Authorization": authValue,
	}

	if readOnly {
		headers["X-MCP-Readonly"] = "true"
	}

	if lockdown {
		headers["X-MCP-Lockdown"] = "true"
	}

	if toolsets != "" {
		headers["X-MCP-Toolsets"] = toolsets
	}

	if features != "" {
		headers["X-MCP-Features"] = features
	}

	// Note: authValue is a secret and is intentionally not logged.
	mcpRendererSectionHelpersLog.Printf("Built GitHub MCP remote headers: readOnly=%v, lockdown=%v, hasToolsets=%v, hasFeatures=%v", readOnly, lockdown, toolsets != "", features != "")

	return headers
}

func hasGitHubMCPGuardPolicies(guardPolicies map[string]any, guardPoliciesFromStep bool) bool {
	return len(guardPolicies) > 0 || guardPoliciesFromStep
}

func renderGitHubMCPGuardPolicies(yaml *strings.Builder, guardPolicies map[string]any, guardPoliciesFromStep bool, indent string) {
	if guardPoliciesFromStep {
		renderGuardPoliciesJSON(yaml, map[string]any{
			"allow-only": map[string]any{
				"min-integrity": "$GITHUB_MCP_GUARD_MIN_INTEGRITY",
				"repos":         "$GITHUB_MCP_GUARD_REPOS",
			},
		}, indent)
		return
	}

	if len(guardPolicies) > 0 {
		renderGuardPoliciesJSON(yaml, guardPolicies, indent)
	}
}

// appendRequiredFalseField writes a "required": false field as the final field of the
// current JSON object. renderGitHubMCPGuardPolicies (and other section helpers) always
// render their field as the last field with no trailing comma, so when a field must
// follow it, the preceding trailing newline must be replaced with ",\n" in place rather
// than writing a bare "," on its own line: a line with no leading indentation would
// dedent out of the enclosing "run: |" block scalar and corrupt the surrounding YAML.
func appendRequiredFalseField(yaml *strings.Builder, precededByField bool, indent string) {
	if precededByField {
		content := yaml.String()
		if trimmed, ok := strings.CutSuffix(content, "\n"); ok {
			yaml.Reset()
			yaml.WriteString(trimmed)
			yaml.WriteString(",\n")
		}
	}
	fmt.Fprintf(yaml, "%s\"required\": false\n", indent)
}
