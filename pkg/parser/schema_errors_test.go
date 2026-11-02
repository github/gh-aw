//go:build !integration

package parser

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCleanOneOfMessage tests that oneOf error messages are simplified to plain English
func TestCleanOneOfMessage(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantNot []string // substrings that must NOT appear in output
		wantAny []string // at least one of these must appear in output
	}{
		{
			name: "engine typo removes got-string-want-object branch",
			input: "at '/engine': 'oneOf' failed, none matched\n" +
				"- at '/engine': value must be one of 'claude', 'codex', 'copilot', 'gemini'\n" +
				"- at '/engine': got string, want object",
			wantNot: []string{"oneOf", "got string, want object"},
			wantAny: []string{"value must be one of 'claude', 'codex', 'copilot', 'gemini'"},
		},
		{
			name: "permissions typo removes got-object-want-string branch",
			input: "at '/permissions': 'oneOf' failed, none matched\n" +
				"- at '/permissions': got object, want string\n" +
				"- at '/permissions/deployments': value must be one of 'read', 'write', 'none'",
			wantNot: []string{"oneOf", "got object, want string"},
			wantAny: []string{"value must be one of 'read', 'write', 'none'"},
		},
		{
			name:    "non-oneOf message is returned unchanged",
			input:   "value must be one of 'a', 'b', 'c'",
			wantNot: []string{"oneOf"},
			wantAny: []string{"value must be one of 'a', 'b', 'c'"},
		},
		{
			name: "nested path context preserved for sub-field errors",
			input: "at '/permissions': 'oneOf' failed, none matched\n" +
				"- at '/permissions': got object, want string\n" +
				"- at '/permissions/deployments': value must be one of 'read', 'write', 'none'",
			wantNot: []string{},
			wantAny: []string{"deployments"},
		},
		{
			name: "all type conflicts synthesizes plain-English message",
			input: "at '/x': 'oneOf' failed, none matched\n" +
				"- at '/x': got string, want object\n" +
				"- at '/x': got string, want array",
			// When all sub-errors are type conflicts, synthesize a plain-English message
			wantNot: []string{"oneOf", "got string, want object"},
			wantAny: []string{"expected object or array, got string"},
		},
		{
			name: "engine type conflict produces actionable message",
			input: "at '/engine': 'oneOf' failed, none matched\n" +
				"- at '/engine': got number, want string\n" +
				"- at '/engine': got number, want object",
			wantNot: []string{"oneOf", "got number, want string"},
			wantAny: []string{"expected string or object, got number", "Valid engine names", "copilot"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cleanOneOfMessage(tt.input)

			for _, unwanted := range tt.wantNot {
				assert.NotContains(t, result, unwanted,
					"Result should not contain %q\nResult: %s", unwanted, result)
			}

			if len(tt.wantAny) > 0 {
				found := false
				for _, wanted := range tt.wantAny {
					if strings.Contains(result, wanted) {
						found = true
						break
					}
				}
				assert.True(t, found,
					"Result should contain at least one of %v\nResult: %s", tt.wantAny, result)
			}
		})
	}
}

// TestSynthesizeOneOfTypeConflictMessage tests the fallback message synthesis for oneOf
// errors where all sub-errors are type conflicts.
func TestSynthesizeOneOfTypeConflictMessage(t *testing.T) {
	tests := []struct {
		name    string
		lines   []string
		want    string
		wantAny []string // at least one of these must appear in output
	}{
		{
			name: "engine field with number type produces actionable message",
			lines: []string{
				"at '/engine': 'oneOf' failed, none matched",
				"- at '/engine': got number, want string",
				"- at '/engine': got number, want object",
			},
			wantAny: []string{"expected string or object, got number", "Valid engine names", "copilot"},
		},
		{
			name: "engine hint includes object form example",
			lines: []string{
				"at '/engine': 'oneOf' failed, none matched",
				"- at '/engine': got number, want string",
				"- at '/engine': got number, want object",
			},
			wantAny: []string{"id: copilot", "max-turns"},
		},
		{
			name: "toolsets field with number type produces actionable message",
			lines: []string{
				"at '/tools/github/toolsets': 'oneOf' failed, none matched",
				"- at '/tools/github/toolsets': got number, want string",
				"- at '/tools/github/toolsets': got number, want array",
			},
			wantAny: []string{"expected string or array, got number", "Valid toolsets", "default", "toolsets: default"},
		},
		{
			name: "unknown field produces generic message without hints",
			lines: []string{
				"at '/foo': 'oneOf' failed, none matched",
				"- at '/foo': got boolean, want string",
				"- at '/foo': got boolean, want array",
			},
			want: "expected string or array, got boolean",
		},
		{
			name: "bare got-want without path",
			lines: []string{
				"'oneOf' failed, none matched",
				"got integer, want string",
				"got integer, want object",
			},
			want: "expected string or object, got integer",
		},
		{
			name: "no type conflicts returns fallback",
			lines: []string{
				"'oneOf' failed, none matched",
			},
			want: "schema validation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := synthesizeOneOfTypeConflictMessage(tt.lines)

			if tt.want != "" {
				assert.Equal(t, tt.want, result,
					"synthesizeOneOfTypeConflictMessage should produce expected message")
			}

			for _, wanted := range tt.wantAny {
				assert.Contains(t, result, wanted,
					"Result should contain %q\nResult: %s", wanted, result)
			}
		})
	}
}

// TestIsTypeConflictLine tests detection of "got X, want Y" lines in oneOf errors
func TestIsTypeConflictLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want bool
	}{
		{
			name: "bare got-want format",
			line: "got string, want object",
			want: true,
		},
		{
			name: "embedded in at-path format",
			line: "- at '/engine': got string, want object",
			want: true,
		},
		{
			name: "embedded got-object-want-string",
			line: "- at '/permissions': got object, want string",
			want: true,
		},
		{
			name: "enum constraint is not a type conflict",
			line: "- at '/engine': value must be one of 'claude', 'codex'",
			want: false,
		},
		{
			name: "additional property error is not a type conflict",
			line: "additional property 'foo' not allowed",
			want: false,
		},
		{
			name: "empty line is not a type conflict",
			line: "",
			want: false,
		},
		{
			name: "minItems constraint is not a type conflict",
			line: "- at '/safe-outputs/add-labels/allowed': minItems: got 0, want 1",
			want: false,
		},
		{
			name: "minimum constraint is not a type conflict",
			line: "- at '/tools/timeout': minimum: got 0, want 1",
			want: false,
		},
		{
			name: "maximum constraint is not a type conflict",
			line: "maximum: got 120, want 60",
			want: false,
		},
		{
			name: "got number want integer is a type conflict",
			line: "- at '/timeout-minutes': got number, want integer",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTypeConflictLine(tt.line)
			assert.Equal(t, tt.want, got,
				"isTypeConflictLine(%q) = %v, want %v", tt.line, got, tt.want)
		})
	}
}

// TestStripAtPathPrefix tests removal of "at '/path':" prefixes from error lines
func TestStripAtPathPrefix(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{
			name: "top-level path stripped entirely",
			line: "- at '/engine': value must be one of 'claude', 'codex'",
			want: "value must be one of 'claude', 'codex'",
		},
		{
			name: "nested path keeps last component",
			line: "- at '/permissions/deployments': value must be one of 'read', 'write', 'none'",
			want: "'deployments': value must be one of 'read', 'write', 'none'",
		},
		{
			name: "line without at-path prefix is unchanged",
			line: "value must be one of 'a', 'b'",
			want: "value must be one of 'a', 'b'",
		},
		{
			name: "at-path without dash prefix",
			line: "at '/engine': some error",
			want: "some error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripAtPathPrefix(tt.line)
			assert.Equal(t, tt.want, got,
				"stripAtPathPrefix(%q) = %q, want %q", tt.line, got, tt.want)
		})
	}
}

// TestCleanJSONSchemaErrorMessage tests the full cleanup pipeline for jsonschema errors
func TestCleanJSONSchemaErrorMessage(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantNot []string
		wantAny []string
	}{
		{
			name: "removes jsonschema validation failed header",
			input: "jsonschema validation failed with 'http://contoso.com/schema.json#'\n" +
				"- at '/engine': 'oneOf' failed, none matched\n" +
				"- at '/engine': value must be one of 'claude', 'codex'\n" +
				"- at '/engine': got string, want object",
			wantNot: []string{"jsonschema validation failed", "contoso.com", "got string, want object", "oneOf"},
			wantAny: []string{"value must be one of 'claude', 'codex'"},
		},
		{
			name: "removes at-root prefix",
			input: "jsonschema validation failed with '...'\n" +
				"- at '': additional property 'foo' not allowed",
			wantNot: []string{"jsonschema validation failed", "at '': "},
			wantAny: []string{"additional property 'foo' not allowed"},
		},
		{
			name:    "empty result falls back to generic message",
			input:   "jsonschema validation failed with '...'",
			wantAny: []string{"schema validation failed"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cleanJSONSchemaErrorMessage(tt.input)

			for _, unwanted := range tt.wantNot {
				assert.NotContains(t, result, unwanted,
					"Result should not contain %q\nResult: %s", unwanted, result)
			}

			if len(tt.wantAny) > 0 {
				found := false
				for _, wanted := range tt.wantAny {
					if strings.Contains(result, wanted) {
						found = true
						break
					}
				}
				assert.True(t, found,
					"Result should contain at least one of %v\nResult: %s", tt.wantAny, result)
			}
		})
	}
}

// TestTranslateSchemaConstraintMessage tests that minimum/maximum messages are translated to plain English
func TestTranslateSchemaConstraintMessage(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "minimum violation negative value",
			input: "minimum: got -45, want 1",
			want:  "must be at least 1 (got -45)",
		},
		{
			name:  "minimum violation zero",
			input: "minimum: got 0, want 1",
			want:  "must be at least 1 (got 0)",
		},
		{
			name:  "minimum violation decimal",
			input: "minimum: got -1.5, want 0",
			want:  "must be at least 0 (got -1.5)",
		},
		{
			name:  "maximum violation",
			input: "maximum: got 120, want 60",
			want:  "must be at most 60 (got 120)",
		},
		{
			name:  "maximum violation decimal",
			input: "maximum: got 100.5, want 60",
			want:  "must be at most 60 (got 100.5)",
		},
		{
			name:  "unrelated message is unchanged",
			input: "value must be one of 'a', 'b'",
			want:  "value must be one of 'a', 'b'",
		},
		{
			name:  "already plain English message is unchanged",
			input: "must be at least 1",
			want:  "must be at least 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := translateSchemaConstraintMessage(tt.input)
			assert.Equal(t, tt.want, got,
				"translateSchemaConstraintMessage(%q) = %q, want %q", tt.input, got, tt.want)
		})
	}
}

func TestRewriteAdditionalPropertiesErrorOrdering(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "sorts multiple unknown properties",
			in:   "at '/safe-outputs': additional properties 'zeta', 'alpha', 'beta' not allowed",
			want: "Unknown properties: alpha, beta, zeta",
		},
		{
			name: "sorts and trims unquoted list",
			in:   "additional properties zeta, alpha, beta not allowed",
			want: "Unknown properties: alpha, beta, zeta",
		},
		{
			name: "single unknown property remains singular",
			in:   "additional property 'timeout_minutes' not allowed",
			want: "Unknown property: timeout_minutes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rewriteAdditionalPropertiesError(tt.in)
			assert.Equal(t, tt.want, got,
				"rewriteAdditionalPropertiesError(%q) = %q, want %q", tt.in, got, tt.want)
		})
	}
}

// TestAppendKnownFieldValidValuesHint tests that the hint function appends valid values,
// "Did you mean?" suggestions, and documentation links for well-known schema paths.
func TestAppendKnownFieldValidValuesHint(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		jsonPath string
		contains []string // substrings that must appear
		excludes []string // substrings that must NOT appear
	}{
		{
			name:     "no hint for non-unknown-property message",
			message:  "value must be one of 'read', 'write', 'none'",
			jsonPath: "/permissions",
			contains: []string{"value must be one of"},
			excludes: []string{"Valid permission scopes", "See:"},
		},
		{
			name:     "hint appended for permissions path",
			message:  "Unknown property: issuess",
			jsonPath: "/permissions",
			contains: []string{"Valid permission scopes", "See: https://docs.github.com"},
		},
		{
			name:     "did you mean suggestion for close typo",
			message:  "Unknown property: issuess",
			jsonPath: "/permissions",
			contains: []string{"Did you mean 'issues'?"},
		},
		{
			name:     "did you mean suggestion for another typo",
			message:  "Unknown property: contnets",
			jsonPath: "/permissions",
			contains: []string{"Did you mean 'contents'?"},
		},
		{
			name:     "no did you mean for unrelated word",
			message:  "Unknown property: completely-unknown-xyz",
			jsonPath: "/permissions",
			excludes: []string{"Did you mean"},
			contains: []string{"Valid permission scopes"},
		},
		{
			name:     "no hint for unknown path",
			message:  "Unknown property: foo",
			jsonPath: "/some-unknown-path",
			contains: []string{"Unknown property: foo"},
			excludes: []string{"Valid permission scopes", "See:"},
		},
		{
			name:     "hint works for nested permissions path",
			message:  "Unknown property: issuess",
			jsonPath: "/permissions/issuess",
			contains: []string{"Valid permission scopes", "See: https://docs.github.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, _ := appendKnownFieldValidValuesHint(tt.message, tt.jsonPath)
			for _, want := range tt.contains {
				assert.Contains(t, result, want,
					"appendKnownFieldValidValuesHint should contain %q\nResult: %s", want, result)
			}
			for _, exclude := range tt.excludes {
				assert.NotContains(t, result, exclude,
					"appendKnownFieldValidValuesHint should not contain %q\nResult: %s", exclude, result)
			}
		})
	}
}
