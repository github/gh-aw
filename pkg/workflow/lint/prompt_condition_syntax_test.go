package lint

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPromptConditionSyntax(t *testing.T) {
	for _, tt := range []struct {
		name   string
		prompt string
		lines  []int
	}{
		{
			name:   "bare fallback delimiter",
			prompt: "{{#if condition}}\nSelected\n{{else}}\nFallback\n{{/if}}",
			lines:  []int{3},
		},
		{
			name:   "whitespace around bare fallback delimiter",
			prompt: "{{#if condition}}\nSelected\n{{ else }}\nFallback\n{{/if}}",
			lines:  []int{3},
		},
		{
			name:   "CRLF input",
			prompt: "{{#if condition}}\r\nSelected\r\n{{else}}\r\nFallback\r\n{{/if}}",
			lines:  []int{3},
		},
		{
			name:   "multiple bare fallback delimiters",
			prompt: "{{#if condition}}\n{{else}}\n{{#if other}}\n{{ else }}\n{{/if}}\n{{/if}}",
			lines:  []int{2, 4},
		},
		{
			name:   "bare fallback outside conditional",
			prompt: "{{else}}",
		},
		{
			name:   "supported conditional markers",
			prompt: "{{#if condition}}\nSelected\n{{#elseif other}}\nOther\n{{#else}}\nFallback\n{{/if}}",
		},
		{
			name:   "inline code",
			prompt: "`{{else}}`",
		},
		{
			name:   "fenced code",
			prompt: "```text\n{{else}}\n```\n~~~\n{{else}}\n~~~",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			input := policyTestInput(t, "")
			input.Document.Markdown = tt.prompt

			runner, err := Default().Select("prompt-condition-syntax")
			require.NoError(t, err)
			diagnostics, err := runner.Run(context.Background(), input)
			require.NoError(t, err)
			require.Len(t, diagnostics, len(tt.lines))
			var lines []int
			for _, diagnostic := range diagnostics {
				lines = append(lines, diagnostic.Line)
				assert.Equal(t, "prompt", diagnostic.Path)
				assert.Equal(t, Error, diagnostic.Severity)
				assert.Contains(t, diagnostic.Message, "{{#else}}")
			}
			assert.Equal(t, tt.lines, lines)
		})
	}
}
