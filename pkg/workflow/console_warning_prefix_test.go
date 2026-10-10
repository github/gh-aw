//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConsoleWarningPrefixes(t *testing.T) {
	tests := []struct {
		name     string
		validate func(*Compiler) error
		message  string
	}{
		{
			name: "serialized secrets",
			validate: func(c *Compiler) error {
				return c.validateSecretsSerializationExpressions(&WorkflowData{
					MarkdownContent: "${{ toJSON(secrets) }}",
					RawFrontmatter:  map[string]any{"strict": false},
				})
			},
			message: "secrets serialization expression(s) detected",
		},
		{
			name: "shell validation",
			validate: func(c *Compiler) error {
				return c.validateStepShellScripts(map[string]any{
					"steps": []any{map[string]any{"name": "List issues", "run": "gh issue list"}},
				})
			},
			message: "shell script validation failed",
		},
		{
			name: "top-level secrets",
			validate: func(c *Compiler) error {
				return c.validateEnvSecretsSection(map[string]any{
					"env": map[string]any{"TOKEN": "${{ secrets.MY_TOKEN }}"},
				}, "env", nil)
			},
			message: "secrets detected in 'env'",
		},
		{
			name: "engine secrets",
			validate: func(c *Compiler) error {
				return c.validateEnvSecretsSection(map[string]any{
					"env": map[string]any{"TOKEN": "${{ secrets.MY_TOKEN }}"},
				}, "engine.env", nil)
			},
			message: "secrets detected in 'engine.env'",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.SetStrictMode(false)
			var err error
			output := captureStderr(func() { err = tt.validate(compiler) })
			require.NoError(t, err)
			require.Contains(t, output, tt.message)
			require.Equal(t, 1, strings.Count(output, "⚠"))
			require.NotContains(t, output, "Warning:")
			require.Equal(t, 1, compiler.GetWarningCount())
		})
	}
}
