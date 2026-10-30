//go:build !integration

package workflow

import (
	"regexp"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
)

// Prefixes intentionally preserved by wrapExpressionsInTemplateConditionals:
// - ${{ ... }} already wrapped GitHub expressions
// - ${...} runtime environment variable references
// - __... internal placeholder references replaced later in rendering
var skippableElseifExprPrefixes = []string{"${{", "${", "__"}

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
	nonCanonicalElseifPatterns := []*regexp.Regexp{
		regexp.MustCompile(`\{\{#else-if\s+([^}]*)\}\}`),
		regexp.MustCompile(`\{\{#else_if\s+([^}]*)\}\}`),
		regexp.MustCompile(`\{\{elseif\s+([^}]*)\}\}`),
		regexp.MustCompile(`\{\{else-if\s+([^}]*)\}\}`),
		regexp.MustCompile(`\{\{else_if\s+([^}]*)\}\}`),
	}

	// Seed corpus with typical GitHub expressions
	f.Add("{{#if github.event.issue.number}}content{{/if}}")
	f.Add("{{#if github.actor}}content{{/if}}")
	f.Add("{{#if github.repository}}content{{/if}}")
	f.Add("{{#if steps.sanitized.outputs.text}}content{{/if}}")
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
	f.Add("{{elseif 0")
	// Malformed nested braces that triggered CGO fuzz failure.
	f.Add("{{#if{{elseif {}}")
	f.Add("{{#if 0000000000000$$${{elseif 00000000000000000}}000")

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
	var longExpr strings.Builder
	longExpr.WriteString("{{#if ")
	for range 100 {
		longExpr.WriteString("github.event.pull_request.head.repo.")
	}
	longExpr.WriteString("name}}content{{/if}}")
	f.Add(longExpr.String())

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

	// elseif variants — all 6 syntax forms
	f.Add("{{#if github.actor}}A{{#elseif github.repository}}B{{/if}}")
	f.Add("{{#if github.actor}}A{{#else-if github.repository}}B{{/if}}")
	f.Add("{{#if github.actor}}A{{#else_if github.repository}}B{{/if}}")
	f.Add("{{#if github.actor}}A{{elseif github.repository}}B{{/if}}")
	f.Add("{{#if github.actor}}A{{else-if github.repository}}B{{/if}}")
	f.Add("{{#if github.actor}}A{{else_if github.repository}}B{{/if}}")

	// elseif with already-wrapped expressions (should be preserved)
	f.Add("{{#if github.actor}}A{{#elseif ${{ github.repository }} }}B{{/if}}")

	// elseif with env var reference (should be preserved, not re-wrapped)
	f.Add("{{#if github.actor}}A{{#elseif ${GH_AW_EXPR_REPO}}}B{{/if}}")

	// elseif with placeholder reference (should be preserved)
	f.Add("{{#if github.actor}}A{{#elseif __PLACEHOLDER__}}B{{/if}}")

	// multiple elseif branches
	f.Add("{{#if a}}A{{#elseif b}}B{{#elseif c}}C{{/if}}")

	// elseif + else
	f.Add("{{#if a}}A{{#elseif b}}B{{#else}}C{{/if}}")
	f.Add("{{#if github.actor}}\nA\n{{#elseif github.repository}}\nB\n{{#else}}\nC\n{{/if}}")

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

		// If a full {{#if ...}} conditional is present in the input, preserve that structure.
		if result != input && strings.Contains(input, "{{#if") {
			if !strings.Contains(result, "{{#if") {
				t.Errorf("Function removed #if conditional structure, input: %q, result: %q", input, result)
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

		// All complete elseif tags must be normalized to canonical {{#elseif ...}} form.
		// Ignore partial/malformed fragments produced by fuzzing (e.g. "{{elseif 0").
		if strings.Contains(input, "{{#if") {
			expressions, _ := actionslanguageservice.ExtractExpressions(result)
		patternLoop:
			for _, pattern := range nonCanonicalElseifPatterns {
				matches := pattern.FindAllStringSubmatchIndex(result, -1)
			matchLoop:
				for _, match := range matches {
					// Text inside an Actions expression is not a template tag.
					for _, expression := range expressions {
						if match[0] >= expression.Start && match[1] <= expression.End {
							continue matchLoop
						}
					}
					expr := strings.TrimSpace(result[match[2]:match[3]])
					// Fuzz heuristic: treat nested or stray braces as malformed
					// fragments rather than canonicalization failures.
					if hasSkippableElseifExprPrefix(expr) ||
						strings.Contains(expr, "{{") ||
						strings.Contains(expr, "{") {
						continue
					}
					t.Errorf("Non-canonical elseif pattern %q still present in output, input: %q", pattern.String(), input)
					break patternLoop
				}
			}
		}
	})
}

func hasSkippableElseifExprPrefix(expr string) bool {
	for _, prefix := range skippableElseifExprPrefixes {
		if strings.HasPrefix(expr, prefix) {
			return true
		}
	}
	return false
}
