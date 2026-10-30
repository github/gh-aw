//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// FuzzExtractTerminalSubExpressions fuzz-tests extractTerminalSubExpressions against
// arbitrary inputs. It validates that the function:
//  1. Never panics, regardless of input.
//  2. Returns only non-empty strings.
//  3. Every returned token matches simpleIdentifierRegex and runtimeEvalEnvVarPrefixRegex.
//  4. Returns no duplicate tokens.
//  5. Handles combinations of ||, &&, and parentheses without incorrect results.
func FuzzExtractTerminalSubExpressions(f *testing.F) {
	// Simple cases
	f.Add("steps.sanitized.outputs.text || inputs.command")
	f.Add("needs.build.outputs.version && inputs.override")
	f.Add("inputs.repo")
	f.Add("github.event.issue.number || inputs.item_number")
	f.Add("steps.pick-experiment.outputs.name || inputs.fallback")

	// Parenthesised combinations
	f.Add("(steps.a.outputs.x || inputs.y)")
	f.Add("(steps.a.outputs.x || inputs.y) && inputs.z")
	f.Add("steps.a.outputs.x || (inputs.y && inputs.z)")
	f.Add("(steps.a.outputs.x || inputs.y) && (steps.b.outputs.z || inputs.w)")
	f.Add("(steps.a.outputs.x || (inputs.y && inputs.z)) && needs.pre.outputs.ok")

	// Mixed github.* (excluded) and steps/inputs (included)
	f.Add("github.event.issue.number || inputs.item_number")
	f.Add("(github.event.issue.number || inputs.item_number) && steps.sanitized.outputs.text")

	// String literals (excluded)
	f.Add("steps.sanitized.outputs.text || 'fallback'")
	f.Add("inputs.repo || 'default/repo'")

	// Malformed / edge-case inputs
	f.Add("")
	f.Add("(")
	f.Add(")")
	f.Add("()")
	f.Add("((")
	f.Add("))")
	f.Add("(((steps.a || inputs.b)))")
	f.Add("||")
	f.Add("&&")
	f.Add("|| &&")
	f.Add("steps.a.outputs.x ||")
	f.Add("|| inputs.y")
	f.Add("((steps.a.outputs.x || inputs.y) && (steps.b.outputs.z")

	// Function calls (should produce no output due to simpleIdentifierRegex)
	f.Add("fromJSON(steps.a.outputs.json).field || inputs.fallback")
	f.Add("contains(inputs.labels, 'bug')")

	// Hyphenated identifiers (should be excluded)
	f.Add("steps.pick-experiment.outputs.name")
	f.Add("steps.pick-experiment.outputs.name || inputs.fallback")

	f.Fuzz(func(t *testing.T, expr string) {
		// Must never panic.
		result := extractTerminalSubExpressions(expr)

		// All returned tokens must be non-empty.
		for _, tok := range result {
			if tok == "" {
				t.Errorf("extractTerminalSubExpressions(%q) returned empty token", expr)
			}
		}

		// All returned tokens must satisfy both matchers.
		for _, tok := range result {
			if !simpleIdentifierRegex.MatchString(tok) {
				t.Errorf("extractTerminalSubExpressions(%q) returned non-simple-identifier token %q", expr, tok)
			}
			if !runtimeEvalEnvVarPrefixRegex.MatchString(tok) {
				t.Errorf("extractTerminalSubExpressions(%q) returned token with unexpected prefix %q", expr, tok)
			}
		}

		// No duplicate tokens.
		seen := make(map[string]struct{}, len(result))
		for _, tok := range result {
			if _, ok := seen[tok]; ok {
				t.Errorf("extractTerminalSubExpressions(%q) returned duplicate token %q", expr, tok)
			}
			seen[tok] = struct{}{}
		}
	})
}

// FuzzRenderExpressions fuzz-tests the combined extract-and-render pipeline against
// arbitrary markdown strings. It validates that the function:
//  1. Never panics; malformed Actions expressions may return errors.
//  2. After rendering, every ${{ ... }} expression that was extracted is replaced
//     in the rendered output by the exact __<EnvVar>__ placeholder for that mapping.
//  3. After rendering, no extracted mapping's Original expression literal remains
//     verbatim in the rendered output (i.e. the replacement was applied).
//  4. Every __GH_AW_…__ placeholder present in the rendered output corresponds to
//     an env var in the extracted mappings.
//  5. Re-substituting placeholder values into the rendered output (simulating what
//     the GitHub Actions runner does at runtime) restores the mapping Content values
//     into the text so that no __GH_AW_…__ placeholder remains.
func FuzzRenderExpressions(f *testing.F) {
	// Plain markdown (no expressions)
	f.Add("This is plain text")
	f.Add("")
	f.Add("# Heading\n\nSome content.")

	// Single simple expressions
	f.Add("Repo: ${{ github.repository }}")
	f.Add("Step output: ${{ steps.sanitized.outputs.text }}")
	f.Add("Input: ${{ inputs.command }}")

	// Compound expressions
	f.Add("Data: ${{ steps.sanitized.outputs.text || inputs.command }}")
	f.Add("Data: ${{ needs.build.outputs.version && inputs.override }}")

	// Parenthesised compound expressions
	f.Add("Data: ${{ (steps.a.outputs.x || inputs.y) && inputs.z }}")
	f.Add("Data: ${{ (steps.a.outputs.x || inputs.y) && (steps.b.outputs.z || inputs.w) }}")

	// Multiple expressions in one markdown
	f.Add("Repo: ${{ github.repository }}, Actor: ${{ github.actor }}")
	f.Add("${{ steps.a.outputs.x || inputs.y }}, ${{ inputs.z }}")

	// Deprecated activation output syntax (kept for backwards-compatibility transformer coverage)
	f.Add("Content: ${{ needs.activation.outputs.text }}")
	f.Add("Fallback: ${{ needs.activation.outputs.text || 'default' }}")

	// Malformed expressions
	f.Add("Bad: ${{ }}")
	f.Add("Unterminated: ${{ steps.a.outputs.x")

	// Expressions with string literals
	f.Add("${{ inputs.repo || 'owner/repo' }}")

	f.Fuzz(func(t *testing.T, markdown string) {
		extractor := NewExpressionExtractor()

		// Invalid source must not produce successful mappings.
		mappings, err := extractor.ExtractExpressions(markdown)
		if err != nil {
			if mappings != nil {
				t.Error("invalid expression returned mappings")
			}
			return
		}

		rendered := extractor.ReplaceExpressionsWithEnvVars(markdown)

		// Build a lookup: Original -> mapping.
		byOriginal := make(map[string]*ExpressionMapping, len(mappings))
		byEnvVar := make(map[string]*ExpressionMapping, len(mappings))
		for _, m := range mappings {
			byOriginal[m.Original] = m
			byEnvVar[m.EnvVar] = m
		}

		// Each mapping's Original must no longer appear verbatim in the rendered output
		// (it was replaced by the __EnvVar__ placeholder).
		for _, m := range mappings {
			if strings.Contains(rendered, m.Original) {
				t.Errorf("ReplaceExpressionsWithEnvVars(%q): Original %q still present in rendered output",
					markdown, m.Original)
			}
		}

		// Each mapping's placeholder must appear in the rendered output if and only if
		// the Original appeared in the markdown.
		for _, m := range mappings {
			placeholder := "__" + m.EnvVar + "__"
			if strings.Contains(markdown, m.Original) && !strings.Contains(rendered, placeholder) {
				t.Errorf("ReplaceExpressionsWithEnvVars(%q): placeholder %q missing from rendered output for Original %q",
					markdown, placeholder, m.Original)
			}
		}

		// Every __GH_AW_…__ token in the rendered output must correspond to a known env var.
		// We scan for all __GH_AW_*__ tokens to catch any spurious placeholders.
		remaining := rendered
		for {
			start := strings.Index(remaining, "__GH_AW_")
			if start == -1 {
				break
			}
			end := strings.Index(remaining[start+2:], "__")
			if end == -1 {
				break
			}
			// The placeholder is remaining[start : start+2+end+2]
			envVar := remaining[start+2 : start+2+end]
			if _, ok := byEnvVar[envVar]; !ok {
				t.Errorf("ReplaceExpressionsWithEnvVars(%q): rendered output contains unknown placeholder __%s__",
					markdown, envVar)
			}
			remaining = remaining[start+2+end+2:]
		}

		// Simulate the runtime: substitute each __EnvVar__ with the mapping's Content.
		// After substitution no __GH_AW_…__ placeholder should remain.
		substituted := rendered
		for _, m := range mappings {
			placeholder := "__" + m.EnvVar + "__"
			substituted = strings.ReplaceAll(substituted, placeholder, m.Content)
		}
		if strings.Contains(substituted, "__GH_AW_") {
			t.Errorf("ReplaceExpressionsWithEnvVars(%q): residual __GH_AW_…__ placeholder after substitution; rendered=%q",
				markdown, rendered)
		}
	})
}

// FuzzExtractExpressions fuzz-tests the full ExtractExpressions pipeline against
// arbitrary markdown strings. It validates that the function:
//  1. Never panics or returns an error for arbitrary input.
//  2. Returns only ExpressionMapping values with non-empty Original, EnvVar, and Content.
//  3. Every EnvVar has the "GH_AW_" prefix and is uppercase.
//  4. No two mappings share the same EnvVar.
//  5. Mappings for compound expressions (containing ||/&&) are accompanied by
//     sub-expression mappings for steps.*/inputs.*/needs.* leaf tokens.
func FuzzExtractExpressions(f *testing.F) {
	// Plain markdown (no expressions)
	f.Add("This is plain text")
	f.Add("")
	f.Add("# Heading\n\nSome content.")

	// Single simple expressions
	f.Add("Repo: ${{ github.repository }}")
	f.Add("Actor: ${{ github.actor }}")
	f.Add("Step output: ${{ steps.sanitized.outputs.text }}")
	f.Add("Input: ${{ inputs.command }}")

	// Compound expressions
	f.Add("Data: ${{ steps.sanitized.outputs.text || inputs.command }}")
	f.Add("Data: ${{ needs.build.outputs.version && inputs.override }}")

	// Parenthesised compound expressions
	f.Add("Data: ${{ (steps.a.outputs.x || inputs.y) && inputs.z }}")
	f.Add("Data: ${{ steps.a.outputs.x || (inputs.y && inputs.z) }}")
	f.Add("Data: ${{ (steps.a.outputs.x || inputs.y) && (steps.b.outputs.z || inputs.w) }}")

	// Multiple expressions in one markdown
	f.Add("Repo: ${{ github.repository }}, Actor: ${{ github.actor }}")
	f.Add("${{ steps.a.outputs.x || inputs.y }}, ${{ inputs.z }}")

	// Deprecated activation output syntax (kept for backwards-compatibility transformer coverage)
	f.Add("Content: ${{ needs.activation.outputs.text }}")
	f.Add("Fallback: ${{ needs.activation.outputs.text || 'default' }}")

	// Malformed expressions
	f.Add("Bad: ${{ }}")
	f.Add("Unterminated: ${{ steps.a.outputs.x")
	f.Add("No open: steps.a.outputs.x }}")

	// Expressions with special characters
	f.Add("${{ github.event.inputs.name || 'default-name' }}")
	f.Add("${{ inputs.repo || 'owner/repo' }}")

	f.Fuzz(func(t *testing.T, markdown string) {
		extractor := NewExpressionExtractor()

		// Invalid source must not produce successful mappings.
		mappings, err := extractor.ExtractExpressions(markdown)
		if err != nil {
			if mappings != nil {
				t.Error("invalid expression returned mappings")
			}
			return
		}

		// Every mapping must have non-empty fields and a valid env var.
		envVarSeen := make(map[string]struct{}, len(mappings))
		for _, m := range mappings {
			if m.Original == "" {
				t.Errorf("ExtractExpressions(%q) returned mapping with empty Original", markdown)
			}
			if m.EnvVar == "" {
				t.Errorf("ExtractExpressions(%q) returned mapping with empty EnvVar", markdown)
			}
			if m.Content == "" {
				t.Errorf("ExtractExpressions(%q) returned mapping with empty Content", markdown)
			}
			if !strings.HasPrefix(m.EnvVar, "GH_AW_") {
				t.Errorf("ExtractExpressions(%q): EnvVar %q missing GH_AW_ prefix", markdown, m.EnvVar)
			}
			if m.EnvVar != strings.ToUpper(m.EnvVar) {
				t.Errorf("ExtractExpressions(%q): EnvVar %q is not uppercase", markdown, m.EnvVar)
			}
			if _, ok := envVarSeen[m.EnvVar]; ok {
				t.Errorf("ExtractExpressions(%q): duplicate EnvVar %q", markdown, m.EnvVar)
			}
			envVarSeen[m.EnvVar] = struct{}{}
		}

		// For each compound mapping, every qualifying leaf sub-expression must have
		// its own deterministic mapping present in the result.
		contentToMapping := make(map[string]*ExpressionMapping, len(mappings))
		for _, m := range mappings {
			contentToMapping[m.Content] = m
		}
		for _, m := range mappings {
			for _, sub := range extractTerminalSubExpressions(m.Content) {
				if _, ok := contentToMapping[sub]; !ok {
					t.Errorf("ExtractExpressions(%q): compound %q missing sub-expression mapping for %q",
						markdown, m.Content, sub)
				}
			}
		}
	})
}
