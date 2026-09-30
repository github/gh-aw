//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestExpressionExtractor_ExtractExpressions(t *testing.T) {
	tests := []struct {
		name            string
		markdown        string
		wantCount       int
		wantExpressions []string
	}{
		{
			name:      "no expressions",
			markdown:  "This is plain text without any expressions",
			wantCount: 0,
		},
		{
			name:            "single simple expression",
			markdown:        "Repository: ${{ github.repository }}",
			wantCount:       1,
			wantExpressions: []string{"github.repository"},
		},
		{
			name:            "multiple expressions",
			markdown:        "Repo: ${{ github.repository }}, Actor: ${{ github.actor }}, Run: ${{ github.run_id }}",
			wantCount:       3,
			wantExpressions: []string{"github.repository", "github.actor", "github.run_id"},
		},
		{
			name:            "duplicate expressions",
			markdown:        "First: ${{ github.repository }}, Second: ${{ github.repository }}",
			wantCount:       1,
			wantExpressions: []string{"github.repository"},
		},
		{
			name:            "expression with operators",
			markdown:        "Issue: ${{ github.event.issue.number || github.event.pull_request.number }}",
			wantCount:       1,
			wantExpressions: []string{"github.event.issue.number || github.event.pull_request.number"},
		},
		{
			name:     "compound || with steps.* and inputs.* emits sub-expression env vars",
			markdown: `Instructions: ${{ steps.sanitized.outputs.text || inputs.command }}`,
			// compound mapping + two deterministic sub-expression mappings
			wantCount: 3,
			wantExpressions: []string{
				"steps.sanitized.outputs.text || inputs.command",
				"steps.sanitized.outputs.text",
				"inputs.command",
			},
		},
		{
			name:     "compound || with only github.* sub-expressions emits no extra mappings",
			markdown: `Issue: ${{ github.event.issue.number || github.event.pull_request.number }}`,
			// github.* sub-expressions are resolved via context, not env vars, so no extras
			wantCount:       1,
			wantExpressions: []string{"github.event.issue.number || github.event.pull_request.number"},
		},
		{
			name:     "compound || with needs.* and string literal emits sub-expression env var",
			markdown: `Data: ${{ needs.activation.outputs.text || 'fallback' }}`,
			// needs.activation.outputs.text transforms to steps.sanitized.outputs.text
			// compound mapping + one deterministic sub-expression mapping
			wantCount: 2,
			wantExpressions: []string{
				"steps.sanitized.outputs.text || 'fallback'",
				"steps.sanitized.outputs.text",
			},
		},
		{
			name:     "standalone sub-expression present in markdown takes precedence over synthesized mapping",
			markdown: `A: ${{ steps.sanitized.outputs.text || inputs.command }}, B: ${{ steps.sanitized.outputs.text }}`,
			// compound + inputs.command sub (steps.sanitized.outputs.text already registered from standalone)
			wantCount: 3,
			wantExpressions: []string{
				"steps.sanitized.outputs.text || inputs.command",
				"steps.sanitized.outputs.text",
				"inputs.command",
			},
		},
		{
			name:            "expression in URL",
			markdown:        "Link: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}",
			wantCount:       3,
			wantExpressions: []string{"github.server_url", "github.repository", "github.run_id"},
		},
		{
			name:            "needs.activation.outputs.text gets transformed",
			markdown:        "Content: ${{ needs.activation.outputs.text }}",
			wantCount:       1,
			wantExpressions: []string{"steps.sanitized.outputs.text"},
		},
		{
			name:            "experiments.name gets transformed to step output",
			markdown:        "Value: ${{ experiments.caveman }}",
			wantCount:       1,
			wantExpressions: []string{"steps.pick-experiment.outputs.caveman"},
		},
		{
			name:            "experiments.name == value comparison form gets transformed to step output",
			markdown:        `{{#if ${{ experiments.prompt_style == "concise" }} }}foo{{/if}}`,
			wantCount:       1,
			wantExpressions: []string{`steps.pick-experiment.outputs.prompt_style == 'concise'`},
		},
		{
			name:            "experiments.name === value strict-equality form gets transformed to step output",
			markdown:        `{{#if ${{ experiments.prompt_style === "concise" }} }}foo{{/if}}`,
			wantCount:       1,
			wantExpressions: []string{`steps.pick-experiment.outputs.prompt_style === 'concise'`},
		},
		{
			name:            "experiments.name != value inequality form gets transformed to step output",
			markdown:        `{{#if ${{ experiments.reasoning_depth != "multi_candidate" }} }}foo{{/if}}`,
			wantCount:       1,
			wantExpressions: []string{`steps.pick-experiment.outputs.reasoning_depth != 'multi_candidate'`},
		},
		{
			name:            "experiments.name !== value strict-inequality form gets transformed to step output",
			markdown:        `{{#if ${{ experiments.reasoning_depth !== "multi_candidate" }} }}foo{{/if}}`,
			wantCount:       1,
			wantExpressions: []string{`steps.pick-experiment.outputs.reasoning_depth !== 'multi_candidate'`},
		},
		{
			name:            "experiments.name == value with single quotes gets transformed to step output",
			markdown:        `{{#if ${{ experiments.prompt_style == 'concise' }} }}foo{{/if}}`,
			wantCount:       1,
			wantExpressions: []string{`steps.pick-experiment.outputs.prompt_style == 'concise'`},
		},
		{
			name:            "expression with whitespace",
			markdown:        "Value: ${{  github.actor  }}",
			wantCount:       1,
			wantExpressions: []string{"github.actor"},
		},
		{
			name:            "aw context syntax sugar gets transformed",
			markdown:        "Issue: ${{ github.event.issue.number || (github.aw.context.item_type == 'issue' && github.aw.context.item_number) }}",
			wantCount:       1,
			wantExpressions: []string{"github.event.issue.number || (fromJSON(steps.generate_aw_info.outputs.aw_context || '{}').item_type == 'issue' && fromJSON(steps.generate_aw_info.outputs.aw_context || '{}').item_number)"},
		},
		{
			name:            "aw context syntax sugar with hyphenated field does not transform",
			markdown:        "Issue: ${{ github.aw.context.item-number }}",
			wantCount:       1,
			wantExpressions: []string{"github.aw.context.item-number"},
		},
		// Parenthesised compound expressions
		{
			name:     "paren-wrapped compound || emits sub-expression env vars",
			markdown: `Instructions: ${{ (steps.sanitized.outputs.text || inputs.command) }}`,
			// outer parens are part of the compound expression content; sub-expressions are still extracted
			wantCount: 3,
			wantExpressions: []string{
				"(steps.sanitized.outputs.text || inputs.command)",
				"steps.sanitized.outputs.text",
				"inputs.command",
			},
		},
		{
			name:     "AND of two paren groups emits all sub-expression env vars",
			markdown: `Data: ${{ (steps.a.outputs.x || inputs.y) && (steps.b.outputs.z || inputs.w) }}`,
			// compound + four sub-expressions
			wantCount: 5,
			wantExpressions: []string{
				"(steps.a.outputs.x || inputs.y) && (steps.b.outputs.z || inputs.w)",
				"steps.a.outputs.x",
				"inputs.y",
				"steps.b.outputs.z",
				"inputs.w",
			},
		},
		{
			name:     "paren group on right of OR emits nested sub-expression env vars",
			markdown: `Data: ${{ steps.a.outputs.x || (inputs.y && inputs.z) }}`,
			// compound + three sub-expressions
			wantCount: 4,
			wantExpressions: []string{
				"steps.a.outputs.x || (inputs.y && inputs.z)",
				"steps.a.outputs.x",
				"inputs.y",
				"inputs.z",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extractor := NewExpressionExtractor()
			mappings, err := extractor.ExtractExpressions(tt.markdown)

			if err != nil {
				t.Errorf("ExtractExpressions() error = %v", err)
				return
			}

			if len(mappings) != tt.wantCount {
				t.Errorf("ExtractExpressions() got %d mappings, want %d", len(mappings), tt.wantCount)
			}

			// Verify expected expressions are present
			for _, wantExpr := range tt.wantExpressions {
				found := false
				for _, mapping := range mappings {
					if mapping.Content == wantExpr {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("ExtractExpressions() missing expected expression: %s", wantExpr)
				}
			}
		})
	}
}

func TestExpressionExtractor_GenerateEnvVarName(t *testing.T) {
	extractor := NewExpressionExtractor()

	tests := []struct {
		name     string
		content  string
		wantName string // expected env var name for simple expressions
	}{
		{
			name:     "simple expression",
			content:  "github.repository",
			wantName: "GH_AW_GITHUB_REPOSITORY",
		},
		{
			name:     "expression with underscore",
			content:  "github.run_id",
			wantName: "GH_AW_GITHUB_RUN_ID",
		},
		{
			name:     "nested expression",
			content:  "github.event.issue.number",
			wantName: "GH_AW_GITHUB_EVENT_ISSUE_NUMBER",
		},
		{
			name:     "sanitized step outputs",
			content:  "steps.sanitized.outputs.text",
			wantName: "GH_AW_STEPS_SANITIZED_OUTPUTS_TEXT",
		},
		{
			name:    "complex expression with operators",
			content: "github.event.issue.number || github.event.pull_request.number",
			// Falls back to hash-based name for complex expressions
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envVar := extractor.generateEnvVarName(tt.content)

			// Check that env var has correct prefix
			if !strings.HasPrefix(envVar, "GH_AW_") {
				t.Errorf("generateEnvVarName() = %s, want prefix GH_AW_", envVar)
			}

			// Check that env var is uppercase
			if envVar != strings.ToUpper(envVar) {
				t.Errorf("generateEnvVarName() = %s, want uppercase", envVar)
			}

			// Check expected name for simple expressions
			if tt.wantName != "" && envVar != tt.wantName {
				t.Errorf("generateEnvVarName() = %s, want %s", envVar, tt.wantName)
			}

			// For complex expressions, check that it falls back to hash-based name
			if tt.wantName == "" && !strings.HasPrefix(envVar, "GH_AW_EXPR_") {
				t.Errorf("generateEnvVarName() = %s, want hash-based name with prefix GH_AW_EXPR_", envVar)
			}

			// Check that same content generates same env var (deterministic)
			envVar2 := extractor.generateEnvVarName(tt.content)
			if envVar != envVar2 {
				t.Errorf("generateEnvVarName() not deterministic: %s != %s", envVar, envVar2)
			}
		})
	}
}

func TestExpressionExtractor_CompleteWorkflow(t *testing.T) {
	markdown := `# Test Workflow

Repository: ${{ github.repository }}
Actor: ${{ github.actor }}
Run ID: ${{ github.run_id }}

Link: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}`

	extractor := NewExpressionExtractor()

	// Extract expressions
	mappings, err := extractor.ExtractExpressions(markdown)
	if err != nil {
		t.Fatalf("ExtractExpressions() error = %v", err)
	}

	// Should have 4 unique expressions
	expectedCount := 4
	if len(mappings) != expectedCount {
		t.Errorf("Expected %d mappings, got %d", expectedCount, len(mappings))
	}

	// Replace expressions
	result := extractor.ReplaceExpressionsWithEnvVars(markdown)

	// Verify no original expressions remain
	if strings.Contains(result, "${{") {
		t.Errorf("Result still contains ${{ expressions: %s", result)
	}

	// Verify all env vars are referenced
	for _, mapping := range mappings {
		envVarRef := "__" + mapping.EnvVar + "__"
		if !strings.Contains(result, envVarRef) {
			t.Errorf("Result missing env var placeholder reference %s: %s", envVarRef, result)
		}
	}

	// Verify the structure is intact (just with different placeholders)
	if !strings.Contains(result, "Repository:") {
		t.Errorf("Result missing 'Repository:' text")
	}
	if !strings.Contains(result, "Actor:") {
		t.Errorf("Result missing 'Actor:' text")
	}
	if !strings.Contains(result, "Link:") {
		t.Errorf("Result missing 'Link:' text")
	}
}

func TestExpressionExtractor_NoCollisions(t *testing.T) {
	// Test that different expressions get different env vars
	expressions := []string{
		"github.repository",
		"github.actor",
		"github.run_id",
		"github.event.issue.number",
		"steps.sanitized.outputs.text",
	}

	extractor := NewExpressionExtractor()
	envVars := make(map[string]bool)

	for _, expr := range expressions {
		envVar := extractor.generateEnvVarName(expr)
		if envVars[envVar] {
			t.Errorf("Collision detected: %s generated duplicate env var %s", expr, envVar)
		}
		envVars[envVar] = true
	}

	// Verify we have as many unique env vars as expressions
	if len(envVars) != len(expressions) {
		t.Errorf("Expected %d unique env vars, got %d", len(expressions), len(envVars))
	}
}

func TestTransformActivationOutputs(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "transform text output",
			input:    "needs.activation.outputs.text",
			expected: "steps.sanitized.outputs.text",
		},
		{
			name:     "transform title output",
			input:    "needs.activation.outputs.title",
			expected: "steps.sanitized.outputs.title",
		},
		{
			name:     "transform body output",
			input:    "needs.activation.outputs.body",
			expected: "steps.sanitized.outputs.body",
		},
		{
			name:     "no transformation for other outputs",
			input:    "needs.activation.outputs.comment_id",
			expected: "needs.activation.outputs.comment_id",
		},
		{
			name:     "no transformation for other jobs",
			input:    "needs.pre_activation.outputs.activated",
			expected: "needs.pre_activation.outputs.activated",
		},
		{
			name:     "expression with operators",
			input:    "needs.activation.outputs.text || 'default'",
			expected: "steps.sanitized.outputs.text || 'default'",
		},
		{
			name:     "multiple transformations in same expression",
			input:    "needs.activation.outputs.title && needs.activation.outputs.body",
			expected: "steps.sanitized.outputs.title && steps.sanitized.outputs.body",
		},
		{
			name:     "no transformation needed",
			input:    "github.repository",
			expected: "github.repository",
		},
		{
			name:     "no transformation for partial match",
			input:    "needs.activation.outputs.text_custom",
			expected: "needs.activation.outputs.text_custom",
		},
		{
			name:     "transform with trailing operator",
			input:    "needs.activation.outputs.text && true",
			expected: "steps.sanitized.outputs.text && true",
		},
		{
			name:     "transform with trailing parenthesis",
			input:    "func(needs.activation.outputs.text)",
			expected: "func(steps.sanitized.outputs.text)",
		},
		{
			name:     "partial match followed by valid match",
			input:    "needs.activation.outputs.text_custom || needs.activation.outputs.text",
			expected: "needs.activation.outputs.text_custom || steps.sanitized.outputs.text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := transformActivationOutputs(tt.input)
			if result != tt.expected {
				t.Errorf("transformActivationOutputs() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestExpressionExtractor_ActivationOutputTransformation(t *testing.T) {
	// Test that the extraction process applies the transformation
	markdown := `# Test

Triggering content: ${{ needs.activation.outputs.text }}
Title: ${{ needs.activation.outputs.title }}
Body: ${{ needs.activation.outputs.body }}
Other: ${{ needs.activation.outputs.comment_id }}
`

	extractor := NewExpressionExtractor()
	mappings, err := extractor.ExtractExpressions(markdown)

	if err != nil {
		t.Fatalf("ExtractExpressions() error = %v", err)
	}

	// Build a map for lookup
	contentMap := make(map[string]*ExpressionMapping)
	for _, mapping := range mappings {
		contentMap[mapping.Content] = mapping
	}

	// Verify transformations were applied
	tests := []struct {
		original    string
		transformed string
		shouldExist bool
	}{
		{
			original:    "needs.activation.outputs.text",
			transformed: "steps.sanitized.outputs.text",
			shouldExist: true,
		},
		{
			original:    "needs.activation.outputs.title",
			transformed: "steps.sanitized.outputs.title",
			shouldExist: true,
		},
		{
			original:    "needs.activation.outputs.body",
			transformed: "steps.sanitized.outputs.body",
			shouldExist: true,
		},
		{
			original:    "needs.activation.outputs.comment_id",
			transformed: "needs.activation.outputs.comment_id",
			shouldExist: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.transformed, func(t *testing.T) {
			// The original expression should NOT be in the content map
			if _, found := contentMap[tt.original]; found && tt.original != tt.transformed {
				t.Errorf("Found untransformed expression %q in mappings (should be %q)", tt.original, tt.transformed)
			}

			// The transformed expression should be in the content map
			if tt.shouldExist {
				if _, found := contentMap[tt.transformed]; !found {
					t.Errorf("Expected transformed expression %q in mappings", tt.transformed)
				}
			}
		})
	}
}

// TestExpressionExtractor_DeprecatedActivationOutputWarning verifies that extracting
// a deprecated needs.activation.outputs.* expression emits a deprecation warning to
// stderr while still producing the correct (transformed) mapping.
func TestExpressionExtractor_DeprecatedActivationOutputWarning(t *testing.T) {
	tests := []struct {
		name       string
		markdown   string
		deprecated string
		modern     string
	}{
		{
			name:       "text output emits warning",
			markdown:   "Content: ${{ needs.activation.outputs.text }}",
			deprecated: "needs.activation.outputs.text",
			modern:     "steps.sanitized.outputs.text",
		},
		{
			name:       "title output emits warning",
			markdown:   "Title: ${{ needs.activation.outputs.title }}",
			deprecated: "needs.activation.outputs.title",
			modern:     "steps.sanitized.outputs.title",
		},
		{
			name:       "body output emits warning",
			markdown:   "Body: ${{ needs.activation.outputs.body }}",
			deprecated: "needs.activation.outputs.body",
			modern:     "steps.sanitized.outputs.body",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extractor := NewExpressionExtractor()

			stderr := captureStderr(func() {
				mappings, err := extractor.ExtractExpressions(tt.markdown)
				if err != nil {
					t.Fatalf("ExtractExpressions() unexpected error: %v", err)
				}
				// Verify the mapping was produced with the modern expression.
				found := false
				for _, m := range mappings {
					if m.Content == tt.modern {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected mapping for %q but not found", tt.modern)
				}
			})

			if !strings.Contains(stderr, tt.deprecated) {
				t.Errorf("expected deprecation warning mentioning %q in stderr, got: %q", tt.deprecated, stderr)
			}
			if !strings.Contains(stderr, tt.modern) {
				t.Errorf("expected deprecation warning mentioning %q in stderr, got: %q", tt.modern, stderr)
			}
		})
	}
}

func TestApplyWorkflowDispatchFallbacks(t *testing.T) {
	tests := []struct {
		name          string
		hasItemNumber bool
		inputMappings []*ExpressionMapping
		wantContents  map[string]string // envVar -> expected Content after applying fallbacks
	}{
		{
			name:          "PR number expression gets fallback when hasItemNumber is true",
			hasItemNumber: true,
			inputMappings: []*ExpressionMapping{
				{Original: "${{ github.event.pull_request.number }}", EnvVar: "GH_AW_GITHUB_EVENT_PULL_REQUEST_NUMBER", Content: "github.event.pull_request.number"},
			},
			wantContents: map[string]string{
				"GH_AW_GITHUB_EVENT_PULL_REQUEST_NUMBER": "github.event.pull_request.number || inputs.item_number",
			},
		},
		{
			name:          "issue number expression gets fallback",
			hasItemNumber: true,
			inputMappings: []*ExpressionMapping{
				{Original: "${{ github.event.issue.number }}", EnvVar: "GH_AW_GITHUB_EVENT_ISSUE_NUMBER", Content: "github.event.issue.number"},
			},
			wantContents: map[string]string{
				"GH_AW_GITHUB_EVENT_ISSUE_NUMBER": "github.event.issue.number || inputs.item_number",
			},
		},
		{
			name:          "discussion number expression gets fallback",
			hasItemNumber: true,
			inputMappings: []*ExpressionMapping{
				{Original: "${{ github.event.discussion.number }}", EnvVar: "GH_AW_GITHUB_EVENT_DISCUSSION_NUMBER", Content: "github.event.discussion.number"},
			},
			wantContents: map[string]string{
				"GH_AW_GITHUB_EVENT_DISCUSSION_NUMBER": "github.event.discussion.number || inputs.item_number",
			},
		},
		{
			name:          "no fallback applied when hasItemNumber is false",
			hasItemNumber: false,
			inputMappings: []*ExpressionMapping{
				{Original: "${{ github.event.pull_request.number }}", EnvVar: "GH_AW_GITHUB_EVENT_PULL_REQUEST_NUMBER", Content: "github.event.pull_request.number"},
			},
			wantContents: map[string]string{
				"GH_AW_GITHUB_EVENT_PULL_REQUEST_NUMBER": "github.event.pull_request.number",
			},
		},
		{
			name:          "unrelated expressions are not modified",
			hasItemNumber: true,
			inputMappings: []*ExpressionMapping{
				{Original: "${{ github.repository }}", EnvVar: "GH_AW_GITHUB_REPOSITORY", Content: "github.repository"},
				{Original: "${{ github.event.pull_request.number }}", EnvVar: "GH_AW_GITHUB_EVENT_PULL_REQUEST_NUMBER", Content: "github.event.pull_request.number"},
			},
			wantContents: map[string]string{
				"GH_AW_GITHUB_REPOSITORY":                "github.repository",
				"GH_AW_GITHUB_EVENT_PULL_REQUEST_NUMBER": "github.event.pull_request.number || inputs.item_number",
			},
		},
		{
			name:          "EnvVar name is preserved after fallback is applied",
			hasItemNumber: true,
			inputMappings: []*ExpressionMapping{
				{Original: "${{ github.event.pull_request.number }}", EnvVar: "GH_AW_GITHUB_EVENT_PULL_REQUEST_NUMBER", Content: "github.event.pull_request.number"},
			},
			wantContents: map[string]string{
				"GH_AW_GITHUB_EVENT_PULL_REQUEST_NUMBER": "github.event.pull_request.number || inputs.item_number",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			applyWorkflowDispatchFallbacks(tt.inputMappings, tt.hasItemNumber)

			for _, mapping := range tt.inputMappings {
				wantContent, ok := tt.wantContents[mapping.EnvVar]
				if !ok {
					t.Errorf("unexpected mapping with EnvVar %q", mapping.EnvVar)
					continue
				}
				if mapping.Content != wantContent {
					t.Errorf("mapping %q Content = %q, want %q", mapping.EnvVar, mapping.Content, wantContent)
				}
				// Verify the EnvVar name itself was not changed by the fallback
				if !strings.HasPrefix(mapping.EnvVar, "GH_AW_") {
					t.Errorf("mapping EnvVar %q lost GH_AW_ prefix after fallback", mapping.EnvVar)
				}
			}
		})
	}
}

func TestExtractTerminalSubExpressions(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name:    "steps and inputs sub-expressions are returned",
			content: "steps.sanitized.outputs.text || inputs.command",
			want:    []string{"steps.sanitized.outputs.text", "inputs.command"},
		},
		{
			name:    "needs sub-expression is returned",
			content: "needs.build.outputs.version || inputs.override",
			want:    []string{"needs.build.outputs.version", "inputs.override"},
		},
		{
			name:    "github.* sub-expressions are excluded (resolved via context)",
			content: "github.event.issue.number || github.event.pull_request.number",
			want:    []string{},
		},
		{
			name:    "github.* left side excluded, inputs.* right side included",
			content: "github.event.issue.number || inputs.item_number",
			want:    []string{"inputs.item_number"},
		},
		{
			name:    "string literal on right side is excluded",
			content: "steps.sanitized.outputs.text || 'fallback'",
			want:    []string{"steps.sanitized.outputs.text"},
		},
		{
			name:    "hyphenated identifier is excluded (not a simpleIdentifier)",
			content: "steps.pick-experiment.outputs.name || inputs.command",
			want:    []string{"inputs.command"},
		},
		{
			name:    "three-way OR returns all matching sub-expressions",
			content: "steps.a.outputs.x || steps.b.outputs.y || inputs.z",
			want:    []string{"steps.a.outputs.x", "steps.b.outputs.y", "inputs.z"},
		},
		{
			name:    "deduplicates repeated sub-expressions",
			content: "steps.foo.outputs.bar || steps.foo.outputs.bar",
			want:    []string{"steps.foo.outputs.bar"},
		},
		{
			name: "simple expression (no operators) — single qualifying token is returned",
			// Note: ExtractExpressions guards the call with !simpleIdentifierRegex,
			// so this case never arises in production; but the function is correct regardless.
			content: "steps.sanitized.outputs.text",
			want:    []string{"steps.sanitized.outputs.text"},
		},
		{
			name:    "function call expression returns empty",
			content: "fromJSON(github.event.inputs.aw_context || '{}').item_number",
			want:    []string{},
		},
		{
			name:    "AND operator is also split",
			content: "needs.check.outputs.passed && inputs.override",
			want:    []string{"needs.check.outputs.passed", "inputs.override"},
		},
		// Parenthesised groups
		{
			name:    "outer parens wrapping the whole expression are stripped",
			content: "(steps.sanitized.outputs.text || inputs.command)",
			want:    []string{"steps.sanitized.outputs.text", "inputs.command"},
		},
		{
			name:    "AND of two paren groups",
			content: "(steps.a.outputs.x || inputs.y) && (steps.b.outputs.z || inputs.w)",
			want:    []string{"steps.a.outputs.x", "inputs.y", "steps.b.outputs.z", "inputs.w"},
		},
		{
			name:    "paren group on the right of OR",
			content: "steps.a.outputs.x || (inputs.y && inputs.z)",
			want:    []string{"steps.a.outputs.x", "inputs.y", "inputs.z"},
		},
		{
			name:    "paren group on the left of AND",
			content: "(steps.a.outputs.x || inputs.y) && inputs.z",
			want:    []string{"steps.a.outputs.x", "inputs.y", "inputs.z"},
		},
		{
			name:    "nested parens with github.* excluded",
			content: "(github.event.issue.number || inputs.item_number) && steps.a.outputs.x",
			want:    []string{"inputs.item_number", "steps.a.outputs.x"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTerminalSubExpressions(tt.content)
			if len(got) != len(tt.want) {
				t.Errorf("extractTerminalSubExpressions(%q) = %v, want %v", tt.content, got, tt.want)
				return
			}
			for i, want := range tt.want {
				if got[i] != want {
					t.Errorf("extractTerminalSubExpressions(%q)[%d] = %q, want %q", tt.content, i, got[i], want)
				}
			}
		})
	}
}

func TestCompoundExpressionEnvVarNames(t *testing.T) {
	// Verify that the sub-expression mappings have the expected env var names
	extractor := NewExpressionExtractor()
	mappings, err := extractor.ExtractExpressions(
		`Instructions: ${{ steps.sanitized.outputs.text || inputs.command }}`,
	)
	if err != nil {
		t.Fatalf("ExtractExpressions() error = %v", err)
	}

	envVarsByContent := make(map[string]string)
	for _, m := range mappings {
		envVarsByContent[m.Content] = m.EnvVar
	}

	wantEnvVars := map[string]string{
		"steps.sanitized.outputs.text": "GH_AW_STEPS_SANITIZED_OUTPUTS_TEXT",
		"inputs.command":               "GH_AW_INPUTS_COMMAND",
	}
	for content, wantEnvVar := range wantEnvVars {
		if got, ok := envVarsByContent[content]; !ok {
			t.Errorf("missing mapping for sub-expression %q", content)
		} else if got != wantEnvVar {
			t.Errorf("sub-expression %q: EnvVar = %q, want %q", content, got, wantEnvVar)
		}
	}

	// The compound expression itself must still have a hash-based env var
	compoundContent := "steps.sanitized.outputs.text || inputs.command"
	if envVar, ok := envVarsByContent[compoundContent]; !ok {
		t.Errorf("missing mapping for compound expression %q", compoundContent)
	} else if !strings.HasPrefix(envVar, "GH_AW_EXPR_") {
		t.Errorf("compound expression %q: EnvVar = %q, want GH_AW_EXPR_* prefix", compoundContent, envVar)
	}
}

// TestMarshalImportInputValue tests the marshalImportInputValue helper for correct
// JSON serialization of array and map types, including typed slices produced by
// goccy/go-yaml (e.g. []string instead of []any).
func TestMarshalImportInputValue(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{
			name:  "string scalar",
			value: "hello",
			want:  "hello",
		},
		{
			name:  "int scalar",
			value: 42,
			want:  "42",
		},
		{
			name:  "bool scalar",
			value: true,
			want:  "true",
		},
		{
			name:  "[]any slice",
			value: []any{"a", "b", "c"},
			want:  `["a","b","c"]`,
		},
		{
			// goccy/go-yaml produces []string instead of []any for string arrays
			name:  "[]string typed slice (goccy/go-yaml output)",
			value: []string{"microsoft/apm#main", "github/awesome-copilot/skills/foo"},
			want:  `["microsoft/apm#main","github/awesome-copilot/skills/foo"]`,
		},
		{
			name:  "[]int typed slice",
			value: []int{1, 2, 3},
			want:  `[1,2,3]`,
		},
		{
			name:  "map[string]any",
			value: map[string]any{"key": "val"},
			want:  `{"key":"val"}`,
		},
		{
			name:  "empty []string",
			value: []string{},
			want:  `[]`,
		},
		{
			name:  "nil value",
			value: nil,
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := marshalImportInputValue(tt.value)
			if got != tt.want {
				t.Errorf("marshalImportInputValue(%v) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}
