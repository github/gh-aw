//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHasNewlineInStringLiteral verifies that the function correctly detects newlines
// inside single-quoted GitHub Actions expression string literals.
func TestHasNewlineInStringLiteral(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "empty string",
			input:    "",
			expected: false,
		},
		{
			name:     "no newlines at all",
			input:    "startsWith(github.event.comment.body, '/command ')",
			expected: false,
		},
		{
			name:     "newline outside any string literal",
			input:    "startsWith(body, '/command ')\n|| body == '/command'",
			expected: false,
		},
		{
			name:     "newline inside single-quoted string literal",
			input:    "startsWith(github.event.comment.body, '/command\n')",
			expected: true,
		},
		{
			name:     "multiple string literals, newline in one",
			input:    "startsWith(body, '/command ') || startsWith(body, '/command\n') || body == '/command'",
			expected: true,
		},
		{
			name:     "escaped single quote before newline",
			input:    "startsWith(body, '/it''s\n')",
			expected: true,
		},
		{
			name:     "escaped single quote, no newline inside",
			input:    "startsWith(body, '/it''s a test')",
			expected: false,
		},
		{
			name:     "newline right before closing quote",
			input:    "body == '/command\n'",
			expected: true,
		},
		{
			name:     "multi-condition with event_name and no newline in literal",
			input:    "(github.event_name == 'issue_comment') && (startsWith(body, '/command '))",
			expected: false,
		},
		{
			name:     "bot comment newline detection in realistic condition",
			input:    "((startsWith(github.event.comment.body, '/deploy ')) || (startsWith(github.event.comment.body, '/deploy\n')))",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasNewlineInStringLiteral(tt.input)
			assert.Equal(t, tt.expected, result, "hasNewlineInStringLiteral(%q)", tt.input)
		})
	}
}

// TestEscapeForYAMLDoubleQuoted verifies that the function correctly escapes strings
// for use inside YAML double-quoted scalars.
func TestEscapeForYAMLDoubleQuoted(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "no special characters",
			input:    "startsWith(body, '/command')",
			expected: "startsWith(body, '/command')",
		},
		{
			name:     "newline character escaped",
			input:    "startsWith(body, '/command\n')",
			expected: `startsWith(body, '/command\n')`,
		},
		{
			name:     "double quote escaped",
			input:    `startsWith(body, "value")`,
			expected: `startsWith(body, \"value\")`,
		},
		{
			name:     "backslash escaped",
			input:    `path\to\file`,
			expected: `path\\to\\file`,
		},
		{
			name:     "carriage return escaped",
			input:    "value\rend",
			expected: `value\rend`,
		},
		{
			name:     "tab escaped",
			input:    "value\tend",
			expected: `value\tend`,
		},
		{
			name:     "combination of special characters",
			input:    "startsWith(body, '/command\n') || body == '/command'",
			expected: `startsWith(body, '/command\n') || body == '/command'`,
		},
		{
			name:     "realistic bot-comment condition",
			input:    "(startsWith(github.event.comment.body, '/deploy ')) || (startsWith(github.event.comment.body, '/deploy\n'))",
			expected: `(startsWith(github.event.comment.body, '/deploy ')) || (startsWith(github.event.comment.body, '/deploy\n'))`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := escapeForYAMLDoubleQuoted(tt.input)
			assert.Equal(t, tt.expected, result, "escapeForYAMLDoubleQuoted(%q)", tt.input)
		})
	}
}

func TestCompileWorkflowNameScalars(t *testing.T) {
	tests := []struct {
		name        string
		value       string
		fromHeading bool
		crlf        bool
	}{
		{name: "plain", value: "Example workflow"},
		{name: "quotes", value: `Example "quoted" workflow`},
		{name: "backslashes", value: `Example C:\new\test\workflow`},
		{name: "LF", value: "Example\nworkflow\n"},
		{name: "CRLF", value: "Example\r\nworkflow\r\n"},
		{name: "Unicode", value: "日本語 café 🚀"},
		{name: "punctuation", value: `Example: [a, b] {c} # 'quoted' & * ! | > % @`},
		{name: "mapping injection", value: "Example\"\nenv:\n  INJECTED: true\n#"},
		{name: "document boundary", value: "Example\n---\nname: injected"},
		{name: "tabs", value: "Example\tworkflow"},
		{name: "controls", value: "Example\x00\a\b\v\f\x1b\x7f workflow"},
		{name: "Unicode line breaks", value: "Example\u0085next\u2028line\u2029paragraph"},
		{name: "expression", value: `Example "quoted" ${{ github.actor }}`},
		{name: "heading", value: `Heading "quoted" C:\new\test: 日本語 🚀 # [a] {b}`, fromHeading: true},
		{name: "CRLF heading", value: `Heading "quoted" C:\new\test: 日本語 🚀`, fromHeading: true, crlf: true},
	}

	compile := func(t *testing.T, value string, fromHeading, crlf, explicitRunName bool) map[string]any {
		t.Helper()
		frontmatter := "on: workflow_dispatch\npermissions:\n  contents: read\nstrict: false\n"
		heading := "Example"
		if fromHeading {
			heading = value
		} else {
			frontmatter += "name: " + strconv.Quote(value) + "\n"
		}
		if explicitRunName {
			frontmatter += "run-name: 'Explicit \"quoted\" run: ${{ github.actor }}'\n"
		}
		content := "---\n" + frontmatter + "---\n\n# " + heading + "\n\nSummarize the repository.\n"
		if crlf {
			content = strings.ReplaceAll(content, "\n", "\r\n")
		}
		path := filepath.Join(t.TempDir(), "workflow.md")
		require.NoError(t, os.WriteFile(path, []byte(content), 0600))
		require.NoError(t, NewCompiler().CompileWorkflow(path))
		lock, err := os.ReadFile(strings.TrimSuffix(path, ".md") + ".lock.yml")
		require.NoError(t, err, "successful compilation must write the lock file")
		var decoded map[string]any
		require.NoError(t, yaml.Unmarshal(lock, &decoded))
		return decoded
	}

	baseline := compile(t, "Example workflow", false, false, false)
	keys := func(mapping map[string]any) []string {
		result := make([]string, 0, len(mapping))
		for key := range mapping {
			result = append(result, key)
		}
		return result
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, explicit := range []bool{false, true} {
				t.Run("explicit="+strconv.FormatBool(explicit), func(t *testing.T) {
					decoded := compile(t, tt.value, tt.fromHeading, tt.crlf, explicit)
					assert.Equal(t, tt.value, decoded["name"])
					expectedRunName := tt.value
					if explicit {
						expectedRunName = `Explicit "quoted" run: ${{ github.actor }}`
					}
					assert.Equal(t, expectedRunName, decoded["run-name"])
					assert.ElementsMatch(t, keys(baseline), keys(decoded), "name contents must not change the top-level mapping")

					jobs, ok := decoded["jobs"].(map[string]any)
					require.True(t, ok)
					foundMetadata := false
					for _, job := range jobs {
						jobMap, ok := job.(map[string]any)
						require.True(t, ok)
						steps, _ := jobMap["steps"].([]any)
						for _, step := range steps {
							stepMap, ok := step.(map[string]any)
							require.True(t, ok)
							env, _ := stepMap["env"].(map[string]any)
							if name, ok := env["GH_AW_INFO_WORKFLOW_NAME"]; ok {
								foundMetadata = true
								assert.Equal(t, tt.value, name)
							}
						}
					}
					assert.True(t, foundMetadata, "compiled workflow must include workflow-name metadata")
				})
			}
		})
	}
}

func TestCompileWorkflowRejectsNonScalarNames(t *testing.T) {
	for _, field := range []string{"name", "run-name"} {
		t.Run(field, func(t *testing.T) {
			for _, existingLock := range []bool{false, true} {
				t.Run("existing-lock="+strconv.FormatBool(existingLock), func(t *testing.T) {
					path := filepath.Join(t.TempDir(), "workflow.md")
					lockPath := strings.TrimSuffix(path, ".md") + ".lock.yml"
					content := "---\non: workflow_dispatch\nstrict: false\n" + field + ": [not, a, scalar]\n---\n\n# Example\n\nSummarize the repository.\n"
					require.NoError(t, os.WriteFile(path, []byte(content), 0600))
					const originalLock = "# Existing lock file must not be overwritten on error.\n"
					if existingLock {
						require.NoError(t, os.WriteFile(lockPath, []byte(originalLock), 0600))
					}

					err := NewCompiler().CompileWorkflow(path)
					require.ErrorContains(t, err, field)
					if existingLock {
						lock, err := os.ReadFile(lockPath)
						require.NoError(t, err)
						assert.Equal(t, originalLock, string(lock))
					} else {
						assert.NoFileExists(t, lockPath)
					}
				})
			}
		})
	}
}
