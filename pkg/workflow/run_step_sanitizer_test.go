//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSanitizeRunStepExpressions(t *testing.T) {
	tests := []struct {
		name            string
		step            map[string]any
		expectChanged   bool
		expectRunHas    []string
		expectRunNotHas []string
		expectEnvKeys   []string
		expectEnvVals   map[string]string
		expectWarnings  int
	}{
		{
			name: "no expressions - not changed",
			step: map[string]any{
				"name": "Safe step",
				"run":  "echo hello",
			},
			expectChanged: false,
		},
		{
			name: "single expression extracted",
			step: map[string]any{
				"name": "Print title",
				"run":  `echo "${{ github.event.issue.title }}"`,
			},
			expectChanged:   true,
			expectRunHas:    []string{"$GH_AW_GITHUB_EVENT_ISSUE_TITLE"},
			expectRunNotHas: []string{"${{"},
			expectEnvKeys:   []string{"GH_AW_GITHUB_EVENT_ISSUE_TITLE"},
			expectEnvVals: map[string]string{
				"GH_AW_GITHUB_EVENT_ISSUE_TITLE": "${{ github.event.issue.title }}",
			},
			expectWarnings: 1,
		},
		{
			name: "multiple distinct expressions extracted",
			step: map[string]any{
				"name": "Multi expr",
				"run":  "echo \"${{ github.event.issue.title }}\" && echo \"${{ github.event.issue.number }}\"",
			},
			expectChanged:   true,
			expectRunHas:    []string{"$GH_AW_GITHUB_EVENT_ISSUE_TITLE", "$GH_AW_GITHUB_EVENT_ISSUE_NUMBER"},
			expectRunNotHas: []string{"${{"},
			expectEnvKeys:   []string{"GH_AW_GITHUB_EVENT_ISSUE_TITLE", "GH_AW_GITHUB_EVENT_ISSUE_NUMBER"},
			expectWarnings:  2,
		},
		{
			name: "duplicate expression appears only once in env",
			step: map[string]any{
				"run": `echo "${{ github.event.issue.title }}" && echo "${{ github.event.issue.title }}"`,
			},
			expectChanged:   true,
			expectRunNotHas: []string{"${{"},
			expectEnvKeys:   []string{"GH_AW_GITHUB_EVENT_ISSUE_TITLE"},
			expectWarnings:  1,
		},
		{
			name: "existing env keys preserved",
			step: map[string]any{
				"name": "With env",
				"run":  `echo "${{ github.event.issue.title }}"`,
				"env": map[string]any{
					"EXISTING": "value",
				},
			},
			expectChanged:   true,
			expectRunNotHas: []string{"${{"},
			expectEnvKeys:   []string{"EXISTING", "GH_AW_GITHUB_EVENT_ISSUE_TITLE"},
			expectEnvVals: map[string]string{
				"EXISTING":                       "value",
				"GH_AW_GITHUB_EVENT_ISSUE_TITLE": "${{ github.event.issue.title }}",
			},
			expectWarnings: 1,
		},
		{
			name: "expression in heredoc not extracted",
			step: map[string]any{
				"name": "Heredoc step",
				"run": `cat > /tmp/out.txt << 'EOF'
${{ github.event.issue.title }}
EOF`,
			},
			// The run: script only has ${{ }} inside a heredoc; removeHeredocContent
			// strips that section before scanning, so no expressions are found and
			// nothing is extracted.
			expectChanged: false,
		},
		{
			name: "expression in bash comment not extracted",
			step: map[string]any{
				"run": strings.Join([]string{
					`set -euo pipefail`,
					`# docs: ${{ secrets.* }}`,
					`echo "ok"`,
				}, "\n"),
			},
			expectChanged: false,
		},
		{
			name: "only non-comment expression is extracted",
			step: map[string]any{
				"run": strings.Join([]string{
					`# docs: ${{ secrets.* }}`,
					`echo "${{ github.event.issue.title }}"`,
				}, "\n"),
			},
			expectChanged:   true,
			expectRunHas:    []string{"# docs: ${{ secrets.* }}", "$GH_AW_GITHUB_EVENT_ISSUE_TITLE"},
			expectRunNotHas: []string{"${{ github.event.issue.title }}"},
			expectEnvKeys:   []string{"GH_AW_GITHUB_EVENT_ISSUE_TITLE"},
			expectWarnings:  1,
		},
		{
			name: "same expression in comment remains unchanged",
			step: map[string]any{
				"run": strings.Join([]string{
					`# docs: ${{ github.event.issue.title }}`,
					`echo "${{ github.event.issue.title }}"`,
				}, "\n"),
			},
			expectChanged:  true,
			expectRunHas:   []string{"# docs: ${{ github.event.issue.title }}", `echo "$GH_AW_GITHUB_EVENT_ISSUE_TITLE"`},
			expectEnvKeys:  []string{"GH_AW_GITHUB_EVENT_ISSUE_TITLE"},
			expectWarnings: 1,
		},
		{
			name: "steps outputs expression extracted",
			step: map[string]any{
				"run": `bash script.sh "${{ steps.build.outputs.artifact }}"`,
			},
			expectChanged:   true,
			expectRunNotHas: []string{"${{"},
			expectEnvKeys:   []string{"GH_AW_STEPS_BUILD_OUTPUTS_ARTIFACT"},
			expectWarnings:  1,
		},
		{
			name: "inputs expression extracted",
			step: map[string]any{
				"run": `echo "${{ inputs.my_param }}"`,
			},
			expectChanged:   true,
			expectRunNotHas: []string{"${{"},
			expectEnvKeys:   []string{"GH_AW_INPUTS_MY_PARAM"},
			expectWarnings:  1,
		},
		{
			name: "safe context expressions (non-user-controlled) are still extracted",
			step: map[string]any{
				"run": `echo "${{ github.sha }}"`,
			},
			// github.sha is safe but we extract all expressions from run: for
			// consistency and defence-in-depth.
			expectChanged:   true,
			expectRunNotHas: []string{"${{"},
			expectEnvKeys:   []string{"GH_AW_GITHUB_SHA"},
			expectWarnings:  1,
		},
		{
			name: "no run field - not changed",
			step: map[string]any{
				"uses": "actions/checkout@v4",
			},
			expectChanged: false,
		},
		{
			name: "non-string run field - not changed",
			step: map[string]any{
				"run": 42,
			},
			expectChanged: false,
		},
		{
			name: "warning includes step name",
			step: map[string]any{
				"name": "My Step",
				"run":  `echo "${{ github.event.issue.title }}"`,
			},
			expectChanged:  true,
			expectWarnings: 1,
		},
		{
			name: "warning omits name when step has no name",
			step: map[string]any{
				"run": `echo "${{ github.event.issue.title }}"`,
			},
			expectChanged:  true,
			expectWarnings: 1,
		},
		{
			name: "pull request body expression extracted",
			step: map[string]any{
				"name": "PR body",
				"run":  `echo "${{ github.event.pull_request.body }}"`,
			},
			expectChanged:   true,
			expectRunNotHas: []string{"${{"},
			expectEnvKeys:   []string{"GH_AW_GITHUB_EVENT_PULL_REQUEST_BODY"},
			expectEnvVals: map[string]string{
				"GH_AW_GITHUB_EVENT_PULL_REQUEST_BODY": "${{ github.event.pull_request.body }}",
			},
			expectWarnings: 1,
		},
		{
			name: "comment body expression extracted",
			step: map[string]any{
				"run": `echo "${{ github.event.comment.body }}"`,
			},
			expectChanged:   true,
			expectRunNotHas: []string{"${{"},
			expectEnvKeys:   []string{"GH_AW_GITHUB_EVENT_COMMENT_BODY"},
			expectWarnings:  1,
		},
		{
			name: "expression with extra whitespace in expression",
			step: map[string]any{
				"run": `echo "${{   github.event.issue.title   }}"`,
			},
			expectChanged:   true,
			expectRunNotHas: []string{"${{"},
			expectWarnings:  1,
		},
		{
			name: "complex expression uses hash-based env var name",
			step: map[string]any{
				"run": `echo "${{ github.event.issue.title || 'default' }}"`,
			},
			// Complex expressions (with operators) fall back to SHA256-hash-based names.
			expectChanged:   true,
			expectRunNotHas: []string{"${{"},
			expectWarnings:  1,
		},
		{
			name: "mixed heredoc and non-heredoc expressions",
			step: map[string]any{
				"run": strings.Join([]string{
					// Non-heredoc: should be extracted
					`echo "${{ github.event.issue.title }}"`,
					// Heredoc: should NOT be extracted
					`cat > /tmp/cfg.json << 'EOF'`,
					`{"body": "${{ github.event.issue.body }}"}`,
					`EOF`,
					// Another non-heredoc after the heredoc: should be extracted
					`echo "${{ github.event.issue.number }}"`,
				}, "\n"),
			},
			expectChanged:   true,
			expectRunNotHas: []string{"${{ github.event.issue.title }}", "${{ github.event.issue.number }}"},
			// Body appears only inside heredoc so is NOT extracted.
			// Title and number appear outside heredoc so ARE extracted.
			expectEnvKeys:  []string{"GH_AW_GITHUB_EVENT_ISSUE_TITLE", "GH_AW_GITHUB_EVENT_ISSUE_NUMBER"},
			expectWarnings: 2,
		},
		{
			name: "injection attempt pattern is still extracted safely",
			step: map[string]any{
				// Attacker-controlled title that could break out of quotes
				"name": "Process",
				"run":  `echo "Processing: ${{ github.event.issue.title }}"`,
			},
			// The expression is extracted to env, so the attacker value never
			// reaches the shell interpreter directly.
			expectChanged:   true,
			expectRunNotHas: []string{"${{"},
			expectEnvKeys:   []string{"GH_AW_GITHUB_EVENT_ISSUE_TITLE"},
			expectWarnings:  1,
		},
		{
			name: "empty run string - not changed",
			step: map[string]any{
				"run": "",
			},
			expectChanged: false,
		},
		{
			name: "run string with no expression marker - not changed",
			step: map[string]any{
				"run": "echo ${MY_VAR}",
			},
			expectChanged: false,
		},
		{
			name: "three expressions in single command",
			step: map[string]any{
				"run": `curl -d '{"title":"${{ github.event.issue.title }}","body":"${{ github.event.issue.body }}","num":${{ github.event.issue.number }}}' https://example.com`,
			},
			expectChanged:   true,
			expectRunNotHas: []string{"${{"},
			expectEnvKeys: []string{
				"GH_AW_GITHUB_EVENT_ISSUE_TITLE",
				"GH_AW_GITHUB_EVENT_ISSUE_BODY",
				"GH_AW_GITHUB_EVENT_ISSUE_NUMBER",
			},
			expectWarnings: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, warnings, changed := sanitizeRunStepExpressions(tt.step)

			assert.Equal(t, tt.expectChanged, changed, "changed flag mismatch")

			if !tt.expectChanged {
				// When unchanged the original map should be returned as-is.
				assert.Equal(t, tt.step, result, "unchanged step should equal input")
				assert.Empty(t, warnings, "no warnings expected for unchanged step")
				return
			}

			// Verify run: field changes.
			runVal, ok := result["run"].(string)
			require.True(t, ok, "run field should be a string")

			for _, want := range tt.expectRunHas {
				assert.Contains(t, runVal, want, "run field should contain %q", want)
			}
			for _, notWant := range tt.expectRunNotHas {
				assert.NotContains(t, runVal, notWant, "run field should not contain %q", notWant)
			}

			// Verify env: block.
			if len(tt.expectEnvKeys) > 0 || len(tt.expectEnvVals) > 0 {
				envMap, ok := result["env"].(map[string]any)
				require.True(t, ok, "env field should be a map")

				for _, key := range tt.expectEnvKeys {
					assert.Contains(t, envMap, key, "env should contain key %q", key)
				}
				for key, val := range tt.expectEnvVals {
					assert.Equal(t, val, envMap[key], "env[%q] value mismatch", key)
				}
			}

			// Verify warning count.
			assert.Len(t, warnings, tt.expectWarnings, "warning count mismatch")

			// Verify that warnings contain the injection-related text.
			for _, w := range warnings {
				assert.Contains(t, w, "shell injection", "warning should mention shell injection")
			}

			// Verify that the step name appears in warnings when present.
			if name, hasName := tt.step["name"].(string); hasName && len(warnings) > 0 {
				assert.Contains(t, warnings[0], name, "warning should mention step name")
			}
		})
	}
}

// TestSanitizeRunStepExpressionsOriginalNotMutated verifies that sanitizeRunStepExpressions
// does not modify the input step map.
func TestSanitizeRunStepExpressionsOriginalNotMutated(t *testing.T) {
	original := map[string]any{
		"name": "My step",
		"run":  `echo "${{ github.event.issue.title }}"`,
	}
	originalRun := original["run"].(string)

	_, _, changed := sanitizeRunStepExpressions(original)
	require.True(t, changed, "expected change")

	assert.Equal(t, originalRun, original["run"], "input run field must not be mutated")
	_, hasEnv := original["env"]
	assert.False(t, hasEnv, "input map must not gain an env field")
}

// TestSanitizeRunStepExpressionsExistingEnvNotMutated verifies that the caller's existing
// env: map is not modified when expressions are extracted.
func TestSanitizeRunStepExpressionsExistingEnvNotMutated(t *testing.T) {
	existingEnv := map[string]any{"MY_KEY": "my_value"}
	original := map[string]any{
		"run": `echo "${{ github.event.issue.title }}"`,
		"env": existingEnv,
	}

	_, _, changed := sanitizeRunStepExpressions(original)
	require.True(t, changed, "expected change")

	// The original env map must not have been modified.
	assert.Len(t, existingEnv, 1, "original env map must not gain extra keys")
	assert.Equal(t, "my_value", existingEnv["MY_KEY"], "original env map must not be modified")
}

// TestSanitizeRunStepExpressionsEnvVarNameGeneration checks env var name generation for
// known expression patterns.
func TestSanitizeRunStepExpressionsEnvVarNameGeneration(t *testing.T) {
	cases := []struct {
		expression string
		wantVarRef string // expected $VAR in the run: script
		wantEnvKey string
	}{
		{"${{ github.event.issue.title }}", "$GH_AW_GITHUB_EVENT_ISSUE_TITLE", "GH_AW_GITHUB_EVENT_ISSUE_TITLE"},
		{"${{ github.event.pull_request.number }}", "$GH_AW_GITHUB_EVENT_PULL_REQUEST_NUMBER", "GH_AW_GITHUB_EVENT_PULL_REQUEST_NUMBER"},
		{"${{ inputs.my_value }}", "$GH_AW_INPUTS_MY_VALUE", "GH_AW_INPUTS_MY_VALUE"},
		{"${{ steps.setup.outputs.path }}", "$GH_AW_STEPS_SETUP_OUTPUTS_PATH", "GH_AW_STEPS_SETUP_OUTPUTS_PATH"},
	}

	for _, c := range cases {
		t.Run(c.expression, func(t *testing.T) {
			step := map[string]any{"run": `echo "` + c.expression + `"`}
			result, _, changed := sanitizeRunStepExpressions(step)

			require.True(t, changed, "should have changed")
			assert.Contains(t, result["run"].(string), c.wantVarRef, "run field should reference env var")

			envMap, ok := result["env"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, c.expression, envMap[c.wantEnvKey], "env var value should be original expression")
		})
	}
}

// TestSanitizeRunStepExpressions_ComplexExpressionHashName verifies that complex
// expressions (containing operators or function calls) fall back to a hash-based
// GH_AW_EXPR_... variable name instead of a pretty dot-separated name.
func TestSanitizeRunStepExpressions_ComplexExpressionHashName(t *testing.T) {
	step := map[string]any{
		"run": `echo "${{ github.event.issue.title || 'no title' }}"`,
	}
	result, warnings, changed := sanitizeRunStepExpressions(step)

	require.True(t, changed)
	runVal := result["run"].(string)
	assert.NotContains(t, runVal, "${{", "run field should have no inline expressions")
	assert.Len(t, warnings, 1)

	envMap, ok := result["env"].(map[string]any)
	require.True(t, ok)

	// Hash-based names start with GH_AW_EXPR_
	foundHashVar := false
	for key := range envMap {
		if strings.HasPrefix(key, "GH_AW_EXPR_") {
			foundHashVar = true
			// The env var's value should be the original expression.
			assert.Equal(t, "${{ github.event.issue.title || 'no title' }}", envMap[key])
		}
	}
	assert.True(t, foundHashVar, "expected hash-based env var name for complex expression")
}

// TestSanitizeRunStepExpressions_MultilineRun verifies multiline run scripts are handled.
func TestSanitizeRunStepExpressions_MultilineRun(t *testing.T) {
	step := map[string]any{
		"name": "Multi-line",
		"run": strings.Join([]string{
			"echo \"Title: ${{ github.event.issue.title }}\"",
			"echo \"Body: ${{ github.event.issue.body }}\"",
			"echo done",
		}, "\n"),
	}

	result, warnings, changed := sanitizeRunStepExpressions(step)

	require.True(t, changed)
	runVal := result["run"].(string)
	assert.NotContains(t, runVal, "${{", "run field should have no inline expressions")
	assert.Contains(t, runVal, "$GH_AW_GITHUB_EVENT_ISSUE_TITLE")
	assert.Contains(t, runVal, "$GH_AW_GITHUB_EVENT_ISSUE_BODY")
	assert.Len(t, warnings, 2, "one warning per unique expression")

	envMap := result["env"].(map[string]any)
	assert.Equal(t, "${{ github.event.issue.title }}", envMap["GH_AW_GITHUB_EVENT_ISSUE_TITLE"])
	assert.Equal(t, "${{ github.event.issue.body }}", envMap["GH_AW_GITHUB_EVENT_ISSUE_BODY"])
}

// TestSanitizeRunStepExpressions_SanitizedRunHasNoInlineExpressions is a broad
// property test: after sanitization the non-heredoc portion of the run: field must
// contain no ${{ }} markers.  (Expressions inside heredoc blocks are intentionally
// left in place because they are written to files, not executed by the shell.)
func TestSanitizeRunStepExpressions_SanitizedRunHasNoInlineExpressions(t *testing.T) {
	scripts := []string{
		`echo "${{ github.event.issue.title }}"`,
		`curl -d "${{ github.event.issue.body }}" https://example.com`,
		`gh api repos/${{ github.repository }}/issues/${{ github.event.issue.number }} -f title="${{ github.event.issue.title }}"`,
		`bash -c "echo ${{ steps.build.outputs.version }} && echo ${{ inputs.env }}"`,
	}

	for _, script := range scripts {
		t.Run(script[:min(40, len(script))], func(t *testing.T) {
			step := map[string]any{"run": script}
			result, _, changed := sanitizeRunStepExpressions(step)

			require.True(t, changed, "expected change for script containing expressions")
			// Only check the non-heredoc portion; heredoc content is intentionally left.
			sanitizedRun := result["run"].(string)
			assert.NotContains(t, removeHeredocContent(sanitizedRun), "${{",
				"non-heredoc portion of sanitized run field must not contain any ${{ }} markers")
		})
	}
}

// ---------------------------------------------------------------------------
// Tests for sanitizeCustomStepsYAML
// ---------------------------------------------------------------------------

func TestSanitizeCustomStepsYAML(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		expectChanged  bool
		expectWarnings int
		checkOutput    func(t *testing.T, out string)
		expectErrNil   bool
	}{
		{
			name: "no expressions - returned unchanged",
			input: `steps:
  - name: Safe
    run: echo hello`,
			expectChanged:  false,
			expectWarnings: 0,
			expectErrNil:   true,
		},
		{
			name: "single step with expression sanitized",
			input: `steps:
  - name: Print title
    run: echo "${{ github.event.issue.title }}"`,
			expectChanged:  true,
			expectWarnings: 1,
			expectErrNil:   true,
			checkOutput: func(t *testing.T, out string) {
				t.Helper()
				// The env var key should appear in the output YAML (in the env: block).
				assert.Contains(t, out, "GH_AW_GITHUB_EVENT_ISSUE_TITLE", "env var key should appear in output")
				// The run: line should reference the env var, not the raw expression.
				assert.Contains(t, out, "$GH_AW_GITHUB_EVENT_ISSUE_TITLE", "run field should reference env var")
			},
		},
		{
			name: "multiple steps - only unsafe ones are modified",
			input: `steps:
  - name: Safe
    run: echo hello
  - name: Unsafe
    run: echo "${{ github.event.issue.title }}"
  - name: Also safe
    uses: actions/checkout@v4`,
			expectChanged:  true,
			expectWarnings: 1,
			expectErrNil:   true,
			checkOutput: func(t *testing.T, out string) {
				t.Helper()
				assert.Contains(t, out, "GH_AW_GITHUB_EVENT_ISSUE_TITLE", "env var key should appear for unsafe step")
				assert.Contains(t, out, "$GH_AW_GITHUB_EVENT_ISSUE_TITLE", "run field should reference env var")
				assert.Contains(t, out, "echo hello", "safe step should be unchanged")
				assert.Contains(t, out, "actions/checkout", "uses step should be unchanged")
			},
		},
		{
			name: "multiple expressions across multiple steps",
			input: `steps:
  - name: Step A
    run: echo "${{ github.event.issue.title }}"
  - name: Step B
    run: bash script.sh "${{ github.event.pull_request.body }}"`,
			expectChanged:  true,
			expectWarnings: 2,
			expectErrNil:   true,
			checkOutput: func(t *testing.T, out string) {
				t.Helper()
				assert.Contains(t, out, "GH_AW_GITHUB_EVENT_ISSUE_TITLE", "issue title env var should appear")
				assert.Contains(t, out, "GH_AW_GITHUB_EVENT_PULL_REQUEST_BODY", "PR body env var should appear")
				assert.Contains(t, out, "$GH_AW_GITHUB_EVENT_ISSUE_TITLE", "run should reference issue title env var")
				assert.Contains(t, out, "$GH_AW_GITHUB_EVENT_PULL_REQUEST_BODY", "run should reference PR body env var")
			},
		},
		{
			name:           "empty string - returned unchanged",
			input:          "",
			expectChanged:  false,
			expectWarnings: 0,
			expectErrNil:   true,
		},
		{
			name:           "malformed YAML - returned unchanged without error",
			input:          "steps:\n  - run: ${{ unclosed",
			expectChanged:  false,
			expectWarnings: 0,
			expectErrNil:   true,
		},
		{
			name: "steps with no run field - not changed",
			input: `steps:
  - name: Checkout
    uses: actions/checkout@v4
  - name: Setup Node
    uses: actions/setup-node@v4
    with:
      node-version: '24'`,
			expectChanged:  false,
			expectWarnings: 0,
			expectErrNil:   true,
		},
		{
			name: "expression already in env not double-extracted",
			input: `steps:
  - name: Safe already
    env:
      TITLE: ${{ github.event.issue.title }}
    run: echo "$TITLE"`,
			// No ${{ }} in the run: field, so nothing to extract.
			expectChanged:  false,
			expectWarnings: 0,
			expectErrNil:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, warnings, err := sanitizeCustomStepsYAML(tt.input)

			if tt.expectErrNil {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}

			assert.Len(t, warnings, tt.expectWarnings, "warning count mismatch")

			if !tt.expectChanged {
				assert.Equal(t, tt.input, out, "output should equal input when no change expected")
			} else {
				assert.NotEqual(t, tt.input, out, "output should differ from input when changed")
			}

			if tt.checkOutput != nil && out != tt.input {
				tt.checkOutput(t, out)
			}
		})
	}
}

// TestSanitizeCustomStepsYAML_WarningsDescribeExtraction checks that warning messages
// identify the expression and env var.
func TestSanitizeCustomStepsYAML_WarningsDescribeExtraction(t *testing.T) {
	input := `steps:
  - name: My Step
    run: echo "${{ github.event.issue.title }}"`

	_, warnings, err := sanitizeCustomStepsYAML(input)
	require.NoError(t, err)
	require.Len(t, warnings, 1)

	w := warnings[0]
	assert.Contains(t, w, "github.event.issue.title", "warning should name the expression")
	assert.Contains(t, w, "GH_AW_GITHUB_EVENT_ISSUE_TITLE", "warning should name the env var")
	assert.Contains(t, w, "shell injection", "warning should mention shell injection")
	assert.Contains(t, w, "My Step", "warning should include step name")
}

// ---------------------------------------------------------------------------
// Collision handling tests
// ---------------------------------------------------------------------------

// TestSanitizeRunStepExpressions_CollisionReusesSameValue verifies that when the
// existing env: block already binds the generated key to the same expression,
// the sanitizer reuses it without emitting a new env var entry.
func TestSanitizeRunStepExpressions_CollisionReusesSameValue(t *testing.T) {
	step := map[string]any{
		"run": `echo "${{ github.event.issue.title }}"`,
		"env": map[string]any{
			// Pre-populated with the same expression the sanitizer would generate.
			"GH_AW_GITHUB_EVENT_ISSUE_TITLE": "${{ github.event.issue.title }}",
		},
	}
	result, warnings, changed := sanitizeRunStepExpressions(step)

	require.True(t, changed, "expected change (run: still had the expression)")
	envMap, ok := result["env"].(map[string]any)
	require.True(t, ok)

	// Exactly one key — the pre-existing one — should be in the env block.
	assert.Len(t, envMap, 1, "no extra env key should be added when same value already present")
	assert.Equal(t, "${{ github.event.issue.title }}", envMap["GH_AW_GITHUB_EVENT_ISSUE_TITLE"])
	// One warning is still emitted because the run: script was modified.
	assert.Len(t, warnings, 1)
}

// TestSanitizeRunStepExpressions_CollisionDifferentValueGetsAlternateName verifies that
// when the existing env: block already binds the generated key name to a *different* value,
// the sanitizer picks an alternate name (GH_AW_..._2) rather than silently overwriting.
func TestSanitizeRunStepExpressions_CollisionDifferentValueGetsAlternateName(t *testing.T) {
	step := map[string]any{
		"run": `echo "${{ github.event.issue.title }}"`,
		"env": map[string]any{
			// Pre-populated with the SAME key but a completely different value.
			"GH_AW_GITHUB_EVENT_ISSUE_TITLE": "something-else",
		},
	}
	result, warnings, changed := sanitizeRunStepExpressions(step)

	require.True(t, changed, "expected change")
	envMap, ok := result["env"].(map[string]any)
	require.True(t, ok)

	// The original value must not be overwritten.
	assert.Equal(t, "something-else", envMap["GH_AW_GITHUB_EVENT_ISSUE_TITLE"],
		"existing env var value must not be overwritten")

	// An alternate name must have been allocated.
	assert.Equal(t, "${{ github.event.issue.title }}", envMap["GH_AW_GITHUB_EVENT_ISSUE_TITLE_2"],
		"collision must be resolved with _2 suffix")

	// The run: script must reference the alternate name.
	runVal := result["run"].(string)
	assert.Contains(t, runVal, "$GH_AW_GITHUB_EVENT_ISSUE_TITLE_2",
		"run field must reference the alternate env var name")
	assert.NotContains(t, runVal, "${{",
		"run field must not contain inline expressions after sanitization")

	assert.Len(t, warnings, 1)
}

// ---------------------------------------------------------------------------
// Quoted-heredoc replacement tests
// ---------------------------------------------------------------------------

// TestSanitizeRunStepExpressions_ExpressionInQuotedHeredocNotReplaced verifies that
// when the same expression appears both outside a quoted heredoc (trigger extraction)
// and inside a quoted heredoc, the replacement inside the heredoc is skipped.
// The expression outside is replaced; the heredoc body is preserved verbatim.
func TestSanitizeRunStepExpressions_ExpressionInQuotedHeredocNotReplaced(t *testing.T) {
	// Title appears BOTH outside (echo line) AND inside the quoted heredoc.
	script := strings.Join([]string{
		`echo "${{ github.event.issue.title }}"`,
		`cat > /tmp/cfg.json << 'EOF'`,
		`{"title": "${{ github.event.issue.title }}"}`,
		`EOF`,
	}, "\n")

	step := map[string]any{"run": script}
	result, warnings, changed := sanitizeRunStepExpressions(step)

	require.True(t, changed, "expression outside heredoc should trigger extraction")
	runVal := result["run"].(string)

	// The non-heredoc echo line should use the env var reference.
	assert.Contains(t, runVal, "$GH_AW_GITHUB_EVENT_ISSUE_TITLE",
		"echo line should reference env var")

	// The quoted heredoc body should still contain the original expression
	// (the Actions runner expands it before the shell runs; quoted heredoc
	// prevents variable expansion so we must not replace it there).
	assert.Contains(t, runVal, `${{ github.event.issue.title }}`,
		"expression inside quoted heredoc must be preserved verbatim")

	assert.Len(t, warnings, 1)
}

// TestReplaceOutsideQuotedHeredocs_NoHeredoc verifies that without heredocs the
// function behaves identically to strings.ReplaceAll.
func TestReplaceOutsideQuotedHeredocs_NoHeredoc(t *testing.T) {
	s := `echo "${{ github.event.issue.title }}" && echo "${{ github.event.issue.title }}"`
	result := replaceOutsideQuotedHeredocs(s, "${{ github.event.issue.title }}", "$TITLE")
	assert.Equal(t, `echo "$TITLE" && echo "$TITLE"`, result)
}

// TestReplaceOutsideQuotedHeredocs_QuotedHeredocPreserved verifies that quoted
// heredoc content is left unchanged.
func TestReplaceOutsideQuotedHeredocs_QuotedHeredocPreserved(t *testing.T) {
	s := strings.Join([]string{
		`echo "${{ github.event.issue.title }}"`,
		`cat > /tmp/f << 'EOF'`,
		`{"t": "${{ github.event.issue.title }}"}`,
		`EOF`,
		`echo done`,
	}, "\n")
	result := replaceOutsideQuotedHeredocs(s, "${{ github.event.issue.title }}", "$TITLE")

	// Replacement must happen in echo lines.
	assert.Contains(t, result, `echo "$TITLE"`)
	// Replacement must NOT happen inside the quoted heredoc.
	assert.Contains(t, result, `${{ github.event.issue.title }}`)
	// Closing EOF line and trailing echo must be intact.
	assert.Contains(t, result, "echo done")
}

// TestReplaceOutsideQuotedHeredocs_UnquotedHeredocIsReplaced verifies that
// unquoted heredoc content (where the shell expands variables) is replaced.
func TestReplaceOutsideQuotedHeredocs_UnquotedHeredocIsReplaced(t *testing.T) {
	s := strings.Join([]string{
		`cat > /tmp/f << EOF`,
		`{"t": "${{ github.event.issue.title }}"}`,
		`EOF`,
	}, "\n")
	result := replaceOutsideQuotedHeredocs(s, "${{ github.event.issue.title }}", "$TITLE")

	// Unquoted heredoc — replacement is safe because the shell expands $TITLE.
	assert.NotContains(t, result, "${{ github.event.issue.title }}")
	assert.Contains(t, result, "$TITLE")
}

func TestReplaceOutsideQuotedHeredocs_DoesNotReplaceInComments(t *testing.T) {
	s := strings.Join([]string{
		`# docs: ${{ github.event.issue.title }}`,
		`echo "${{ github.event.issue.title }}"`,
	}, "\n")
	result := replaceOutsideQuotedHeredocs(s, "${{ github.event.issue.title }}", "$TITLE")

	assert.Contains(t, result, `# docs: ${{ github.event.issue.title }}`)
	assert.Contains(t, result, `echo "$TITLE"`)
}
