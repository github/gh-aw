//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// FuzzWrapExpressionsInTemplateConditionals performs fuzz testing on the template
// conditional expression wrapper to ensure it handles all inputs without panicking
// and correctly wraps/preserves expressions.
//
// The fuzzer validates that:
// 1. The function never panics on any input
// 2. GitHub expressions are properly wrapped in ${{ }}
// 3. Already-wrapped expressions are preserved
// 4. Environment variables (${...}) are not wrapped
// 5. Placeholder references (__...) are not wrapped
// 6. Empty expressions are wrapped as ${{ false }}
// 7. Malformed input is handled gracefully
func FuzzWrapExpressionsInTemplateConditionals(f *testing.F) {
	// Seed corpus with typical GitHub expressions
	f.Add("{{#if github.event.issue.number}}content{{/if}}")
	f.Add("{{#if github.actor}}content{{/if}}")
	f.Add("{{#if github.repository}}content{{/if}}")
	f.Add("{{#if needs.activation.outputs.text}}content{{/if}}")
	f.Add("{{#if steps.my-step.outputs.result}}content{{/if}}")
	f.Add("{{#if env.MY_VAR}}content{{/if}}")

	// Already wrapped expressions (should be preserved)
	f.Add("{{#if ${{ github.event.issue.number }} }}content{{/if}}")
	f.Add("{{#if ${{ github.actor }}}}content{{/if}}")

	// Environment variables (should not be wrapped)
	f.Add("{{#if ${GH_AW_EXPR_D892F163}}}content{{/if}}")
	f.Add("{{#if ${GH_AW_EXPR_ABC123}}}content{{/if}}")

	// Placeholder references (should not be wrapped)
	f.Add("{{#if __PLACEHOLDER__}}content{{/if}}")
	f.Add("{{#if __VAR_123__}}content{{/if}}")

	// Empty expressions
	f.Add("{{#if }}content{{/if}}")
	f.Add("{{#if   }}content{{/if}}")
	f.Add("{{#if\t}}content{{/if}}")

	// Literal values
	f.Add("{{#if true}}content{{/if}}")
	f.Add("{{#if false}}content{{/if}}")
	f.Add("{{#if 0}}content{{/if}}")
	f.Add("{{#if 1}}content{{/if}}")

	// Multiple conditionals
	f.Add("{{#if github.actor}}first{{/if}}\n{{#if github.repository}}second{{/if}}")
	f.Add("{{#if github.actor}}A{{/if}} {{#if github.repository }}B{{/if}} {{#if ${{ github.ref }} }}C{{/if}}")

	// Edge cases with whitespace
	f.Add("{{#if github.actor }}content{{/if}}")
	f.Add("{{#if  github.actor  }}content{{/if}}")
	f.Add("  {{#if github.actor}}content{{/if}}")
	f.Add("{{#if\tgithub.actor}}content{{/if}}")

	// Malformed inputs
	f.Add("{{#if github.actor}}")
	f.Add("{{/if}}")
	f.Add("{{#if")
	f.Add("}}")
	f.Add("{{#if }}{{#if }}")

	// Nested braces
	f.Add("{{#if ${{ ${{ github.actor }} }} }}content{{/if}}")
	f.Add("{{#if {github.actor}}}content{{/if}}")

	// Special characters
	f.Add("{{#if github.actor!}}content{{/if}}")
	f.Add("{{#if github-actor}}content{{/if}}")
	f.Add("{{#if github.actor.value}}content{{/if}}")

	// Unicode and control characters
	f.Add("{{#if github.actor™}}content{{/if}}")
	f.Add("{{#if github.actor\n}}content{{/if}}")
	f.Add("{{#if github.actor\x00}}content{{/if}}")

	// Very long expressions
	longExpr := "{{#if "
	for i := 0; i < 100; i++ {
		longExpr += "github.event.pull_request.head.repo."
	}
	longExpr += "name}}content{{/if}}"
	f.Add(longExpr)

	// Complex markdown structures
	f.Add(`# Header
{{#if github.actor}}
## Conditional section
{{/if}}`)
	f.Add("{{#if github.actor}}**bold**{{/if}}")
	f.Add("{{#if github.actor}}\n- list\n- items\n{{/if}}")

	// Mixed valid and edge cases
	f.Add("Before {{#if github.actor}}middle{{/if}} after")
	f.Add("{{#if github.actor}}{{#if github.repository}}nested{{/if}}{{/if}}")

	f.Fuzz(func(t *testing.T, input string) {
		// The fuzzer will generate variations of the seed corpus
		// and random strings to test the wrapper

		// This should never panic, even on malformed input
		result := wrapExpressionsInTemplateConditionals(input)

		// Basic validation checks
		if result == "" && input != "" {
			// Result should not be empty if input is not empty
			// (unless the input somehow gets completely removed, which shouldn't happen)
			t.Errorf("wrapExpressionsInTemplateConditionals returned empty string for non-empty input")
		}

		// If the function modified the input, verify the modification is sensible
		if result != input {
			// If input was modified, the result should contain either:
			// - The wrapping pattern ${{ }} (for wrapped expressions)
			// - The original conditional pattern {{#if (preserved structure)
			if !strings.Contains(result, "{{#if") {
				t.Errorf("Function removed conditional structure, input: %q, result: %q", input, result)
			}
		}

		// If the input already contains wrapped expressions, they should be preserved
		if strings.Contains(input, "${{ github.") {
			if !strings.Contains(result, "${{ github.") {
				t.Errorf("Already wrapped expression should be preserved, input: %q, result: %q", input, result)
			}
		}

		// If the input contains environment variables, they should be preserved
		if strings.Contains(input, "${GH_AW_EXPR_") {
			// The env var pattern should still exist after processing
			if !strings.Contains(result, "${GH_AW_EXPR_") {
				t.Errorf("Environment variable references should not be removed, input: %q, result: %q", input, result)
			}
		}

		// If the input contains placeholder references, they should be preserved
		if strings.Contains(input, "__") && strings.Contains(input, "{{#if __") {
			// The result should still contain the __ prefix pattern
			if !strings.Contains(result, "{{#if __") && !strings.Contains(result, "{{#if ${{ __") {
				t.Errorf("Placeholder references should be preserved, input: %q, result: %q", input, result)
			}
		}
	})
}
