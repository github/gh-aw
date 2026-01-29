//go:build !integration

package workflow

import (
	"testing"
)

func TestWrapExpressionsInTemplateConditionals(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple github.event expression",
			input:    "{{#if github.event.issue.number}}content{{/if}}",
			expected: "{{#if ${{ github.event.issue.number }} }}content{{/if}}",
		},
		{
			name:     "github.actor expression",
			input:    "{{#if github.actor}}content{{/if}}",
			expected: "{{#if ${{ github.actor }} }}content{{/if}}",
		},
		{
			name:     "github.repository expression",
			input:    "{{#if github.repository}}content{{/if}}",
			expected: "{{#if ${{ github.repository }} }}content{{/if}}",
		},
		{
			name:     "needs. expression",
			input:    "{{#if needs.activation.outputs.text}}content{{/if}}",
			expected: "{{#if ${{ needs.activation.outputs.text }} }}content{{/if}}",
		},
		{
			name:     "steps. expression",
			input:    "{{#if steps.my-step.outputs.result}}content{{/if}}",
			expected: "{{#if ${{ steps.my-step.outputs.result }} }}content{{/if}}",
		},
		{
			name:     "env. expression",
			input:    "{{#if env.MY_VAR}}content{{/if}}",
			expected: "{{#if ${{ env.MY_VAR }} }}content{{/if}}",
		},
		{
			name:     "already wrapped expression",
			input:    "{{#if ${{ github.event.issue.number }} }}content{{/if}}",
			expected: "{{#if ${{ github.event.issue.number }} }}content{{/if}}",
		},
		{
			name:     "literal true value (wrapped)",
			input:    "{{#if true}}content{{/if}}",
			expected: "{{#if ${{ true }} }}content{{/if}}",
		},
		{
			name:     "literal false value (wrapped)",
			input:    "{{#if false}}content{{/if}}",
			expected: "{{#if ${{ false }} }}content{{/if}}",
		},
		{
			name:     "literal string value (wrapped)",
			input:    "{{#if some_literal}}content{{/if}}",
			expected: "{{#if ${{ some_literal }} }}content{{/if}}",
		},
		{
			name:     "multiple conditionals",
			input:    "{{#if github.actor}}first{{/if}}\n{{#if github.repository}}second{{/if}}",
			expected: "{{#if ${{ github.actor }} }}first{{/if}}\n{{#if ${{ github.repository }} }}second{{/if}}",
		},
		{
			name:     "mixed wrapped and unwrapped",
			input:    "{{#if github.actor}}first{{/if}}\n{{#if ${{ github.repository }} }}second{{/if}}",
			expected: "{{#if ${{ github.actor }} }}first{{/if}}\n{{#if ${{ github.repository }} }}second{{/if}}",
		},
		{
			name:     "expression with extra whitespace",
			input:    "{{#if   github.event.issue.number  }}content{{/if}}",
			expected: "{{#if ${{ github.event.issue.number }} }}content{{/if}}",
		},
		{
			name: "multiline content with multiple conditionals",
			input: `# Test Template

{{#if github.event.issue.number}}
This should be shown if there's an issue number.
{{/if}}

{{#if github.actor}}
This should be shown if there's an actor.
{{/if}}

Normal content here.`,
			expected: `# Test Template

{{#if ${{ github.event.issue.number }} }}
This should be shown if there's an issue number.
{{/if}}

{{#if ${{ github.actor }} }}
This should be shown if there's an actor.
{{/if}}

Normal content here.`,
		},
		{
			name:     "complex github.event path",
			input:    "{{#if github.event.pull_request.number}}content{{/if}}",
			expected: "{{#if ${{ github.event.pull_request.number }} }}content{{/if}}",
		},
		{
			name:     "github.run_id expression",
			input:    "{{#if github.run_id}}content{{/if}}",
			expected: "{{#if ${{ github.run_id }} }}content{{/if}}",
		},
		{
			name:     "environment variable reference (should not be wrapped)",
			input:    "{{#if ${GH_AW_EXPR_D892F163}}}content{{/if}}",
			expected: "{{#if ${GH_AW_EXPR_D892F163}}}content{{/if}}",
		},
		{
			name:     "multiple environment variable references",
			input:    "{{#if ${GH_AW_EXPR_ABC123}}}first{{/if}}\n{{#if ${GH_AW_EXPR_DEF456}}}second{{/if}}",
			expected: "{{#if ${GH_AW_EXPR_ABC123}}}first{{/if}}\n{{#if ${GH_AW_EXPR_DEF456}}}second{{/if}}",
		},
		{
			name:     "mixed github expression and env var reference",
			input:    "{{#if github.actor}}first{{/if}}\n{{#if ${GH_AW_EXPR_ABC123}}}second{{/if}}",
			expected: "{{#if ${{ github.actor }} }}first{{/if}}\n{{#if ${GH_AW_EXPR_ABC123}}}second{{/if}}",
		},
		{
			name:     "two leading spaces before opening tag",
			input:    "  {{#if github.event.issue.number}}content{{/if}}",
			expected: "  {{#if ${{ github.event.issue.number }} }}content{{/if}}",
		},
		{
			name:     "four leading spaces before opening tag",
			input:    "    {{#if github.actor}}content{{/if}}",
			expected: "    {{#if ${{ github.actor }} }}content{{/if}}",
		},
		{
			name:     "tab before opening tag",
			input:    "\t{{#if github.repository}}content{{/if}}",
			expected: "\t{{#if ${{ github.repository }} }}content{{/if}}",
		},
		{
			name:     "leading spaces with multiline content",
			input:    "  {{#if github.event.issue.number}}\n  This is indented\n  {{/if}}",
			expected: "  {{#if ${{ github.event.issue.number }} }}\n  This is indented\n  {{/if}}",
		},
		{
			name:     "mixed indentation levels",
			input:    "{{#if github.actor}}first{{/if}}\n  {{#if github.repository}}second{{/if}}",
			expected: "{{#if ${{ github.actor }} }}first{{/if}}\n  {{#if ${{ github.repository }} }}second{{/if}}",
		},
		{
			name: "realistic markdown with indentation",
			input: `# Header

  {{#if github.event.issue.number}}
  ## Conditional section
  Content here
  {{/if}}

Regular content`,
			expected: `# Header

  {{#if ${{ github.event.issue.number }} }}
  ## Conditional section
  Content here
  {{/if}}

Regular content`,
		},
		{
			name:     "leading spaces with already wrapped expression",
			input:    "  {{#if ${{ github.event.issue.number }} }}content{{/if}}",
			expected: "  {{#if ${{ github.event.issue.number }} }}content{{/if}}",
		},
		{
			name:     "leading spaces with environment variable reference",
			input:    "  {{#if ${GH_AW_EXPR_ABC123}}}content{{/if}}",
			expected: "  {{#if ${GH_AW_EXPR_ABC123}}}content{{/if}}",
		},
		{
			name:     "expression with space before closing braces",
			input:    "{{#if github.actor }}content{{/if}}",
			expected: "{{#if ${{ github.actor }} }}content{{/if}}",
		},
		{
			name:     "expression with spaces around expression",
			input:    "{{#if  github.actor  }}content{{/if}}",
			expected: "{{#if ${{ github.actor }} }}content{{/if}}",
		},
		{
			name:     "already wrapped with space variations",
			input:    "{{#if ${{ github.actor }}}}content{{/if}}",
			expected: "{{#if ${{ github.actor }}}}content{{/if}}",
		},
		{
			name:     "already wrapped with extra spaces",
			input:    "{{#if ${{ github.actor }} }}content{{/if}}",
			expected: "{{#if ${{ github.actor }} }}content{{/if}}",
		},
		{
			name:     "already wrapped with space before ${{",
			input:    "{{#if  ${{ github.actor }} }}content{{/if}}",
			expected: "{{#if  ${{ github.actor }} }}content{{/if}}",
		},
		{
			name:     "expression with trailing space in conditional",
			input:    "{{#if github.event.issue.number }}content{{/if}}",
			expected: "{{#if ${{ github.event.issue.number }} }}content{{/if}}",
		},
		{
			name:     "expression with tab character",
			input:    "{{#if\tgithub.actor}}content{{/if}}",
			expected: "{{#if ${{ github.actor }} }}content{{/if}}",
		},
		{
			name:     "multiple expressions with varying spaces",
			input:    "{{#if github.actor}}A{{/if}} {{#if github.repository }}B{{/if}} {{#if ${{ github.ref }} }}C{{/if}}",
			expected: "{{#if ${{ github.actor }} }}A{{/if}} {{#if ${{ github.repository }} }}B{{/if}} {{#if ${{ github.ref }} }}C{{/if}}",
		},
		{
			name:     "complex expression with spaces",
			input:    "{{#if needs.setup.outputs.value }}content{{/if}}",
			expected: "{{#if ${{ needs.setup.outputs.value }} }}content{{/if}}",
		},
		{
			name:     "empty expression (treated as false)",
			input:    "{{#if }}content{{/if}}",
			expected: "{{#if ${{ false }} }}content{{/if}}",
		},
		{
			name:     "empty expression with only spaces (treated as false)",
			input:    "{{#if   }}content{{/if}}",
			expected: "{{#if ${{ false }} }}content{{/if}}",
		},
		{
			name:     "multiple conditionals including empty",
			input:    "{{#if github.actor}}A{{/if}} {{#if }}B{{/if}} {{#if true}}C{{/if}}",
			expected: "{{#if ${{ github.actor }} }}A{{/if}} {{#if ${{ false }} }}B{{/if}} {{#if ${{ true }} }}C{{/if}}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := wrapExpressionsInTemplateConditionals(tt.input)
			if result != tt.expected {
				t.Errorf("wrapExpressionsInTemplateConditionals() = %q, want %q", result, tt.expected)
			}
		})
	}
}
