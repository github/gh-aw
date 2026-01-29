//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateNoTemplateInjection(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		shouldError bool
		errorString string
	}{
		{
			name: "safe pattern - expression in env variable",
			yaml: `jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Safe usage
        env:
          ISSUE_TITLE: ${{ github.event.issue.title }}
        run: |
          echo "Title: $ISSUE_TITLE"`,
			shouldError: false,
		},
		{
			name: "safe pattern - no expressions in run block",
			yaml: `jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Safe command
        run: |
          echo "Hello world"
          bash script.sh`,
			shouldError: false,
		},
		{
			name: "safe pattern - safe context expressions",
			yaml: `jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Safe contexts
        run: |
          echo "Actor: ${{ github.actor }}"
          echo "Repository: ${{ github.repository }}"
          echo "SHA: ${{ github.sha }}"`,
			shouldError: false,
		},
		{
			name: "unsafe pattern - github.event in run block",
			yaml: `jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Unsafe usage
        run: |
          echo "Issue: ${{ github.event.issue.title }}"`,
			shouldError: true,
			errorString: "template injection",
		},
		{
			name: "unsafe pattern - steps.outputs in run block",
			yaml: `jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Unsafe usage
        run: |
          bash /opt/gh-aw/actions/stop_mcp_gateway.sh ${{ steps.start-mcp-gateway.outputs.gateway-pid }}`,
			shouldError: true,
			errorString: "steps.*.outputs",
		},
		{
			name: "unsafe pattern - inputs in run block",
			yaml: `jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Unsafe usage
        run: |
          echo "Input: ${{ inputs.user_data }}"`,
			shouldError: true,
			errorString: "workflow inputs",
		},
		{
			name: "unsafe pattern - multiple violations",
			yaml: `jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Multiple unsafe patterns
        run: |
          echo "Title: ${{ github.event.issue.title }}"
          echo "Body: ${{ github.event.issue.body }}"
          bash script.sh ${{ steps.foo.outputs.bar }}`,
			shouldError: true,
			errorString: "template injection",
		},
		{
			name: "unsafe pattern - single line run command",
			yaml: `jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Single line unsafe
        run: 'echo "PR title: ${{ github.event.pull_request.title }}"'`,
			shouldError: true,
			errorString: "github.event",
		},
		{
			name: "safe pattern - expression in condition",
			yaml: `jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Conditional step
        if: github.event.issue.title == 'test'
        run: |
          echo "Running conditional step"`,
			shouldError: false,
		},
		{
			name: "unsafe pattern - github.event.comment",
			yaml: `jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Process comment
        run: |
          comment="${{ github.event.comment.body }}"
          echo "$comment"`,
			shouldError: true,
			errorString: "github.event",
		},
		{
			name: "unsafe pattern - github.event.pull_request",
			yaml: `jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Process PR
        run: |
          title="${{ github.event.pull_request.title }}"
          body="${{ github.event.pull_request.body }}"`,
			shouldError: true,
			errorString: "github.event",
		},
		{
			name: "safe pattern - mixed safe and env usage",
			yaml: `jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Mixed safe usage
        env:
          TITLE: ${{ github.event.issue.title }}
          ACTOR: ${{ github.actor }}
        run: |
          echo "Title: $TITLE"
          echo "Actor: $ACTOR"
          echo "SHA: ${{ github.sha }}"`,
			shouldError: false,
		},
		{
			name: "unsafe pattern - github.head_ref in run",
			yaml: `jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Branch name
        run: |
          echo "Branch: ${{ github.head_ref }}"`,
			shouldError: false, // head_ref is not in our unsafe list (it's in env vars already in real workflows)
		},
		{
			name: "complex unsafe pattern - nested in script",
			yaml: `jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Complex unsafe
        run: |
          if [ -n "${{ github.event.issue.number }}" ]; then
            curl -X POST "https://api.github.com/repos/owner/repo/issues/${{ github.event.issue.number }}/comments"
          fi`,
			shouldError: true,
			errorString: "github.event",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateNoTemplateInjection(tt.yaml)

			if tt.shouldError {
				require.Error(t, err, "Expected validation to fail but it passed")
				if tt.errorString != "" {
					assert.Contains(t, err.Error(), tt.errorString,
						"Error message should contain expected string")
				}
				// Verify error message quality
				assert.Contains(t, err.Error(), "template injection",
					"Error should mention template injection")
				assert.Contains(t, err.Error(), "Safe Pattern",
					"Error should provide safe pattern example")
			} else {
				assert.NoError(t, err, "Expected validation to pass but got error: %v", err)
			}
		})
	}
}

func TestTemplateInjectionErrorMessageQuality(t *testing.T) {
	// Test that error messages are helpful and actionable
	yaml := `jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Test step
        run: echo "${{ github.event.issue.title }}"
      - name: Another step
        run: bash script.sh ${{ steps.foo.outputs.bar }}`

	err := validateNoTemplateInjection(yaml)
	require.Error(t, err, "Should detect template injection")

	errMsg := err.Error()

	// Check for key components of a good error message
	t.Run("mentions security risk", func(t *testing.T) {
		assert.Contains(t, errMsg, "Security Risk",
			"Error should explain the security implications")
	})

	t.Run("shows safe pattern", func(t *testing.T) {
		assert.Contains(t, errMsg, "Safe Pattern",
			"Error should show the correct way to do it")
		assert.Contains(t, errMsg, "env:",
			"Safe pattern should mention env variables")
	})

	t.Run("shows unsafe pattern", func(t *testing.T) {
		assert.Contains(t, errMsg, "Unsafe Pattern",
			"Error should show what NOT to do")
	})

	t.Run("provides references", func(t *testing.T) {
		assert.Contains(t, errMsg, "References",
			"Error should link to documentation")
		assert.Contains(t, errMsg, "security-hardening-for-github-actions",
			"Should link to GitHub security docs")
		assert.Contains(t, errMsg, "zizmor",
			"Should reference zizmor tool")
	})

	t.Run("groups by context", func(t *testing.T) {
		assert.Contains(t, errMsg, "github.event",
			"Should identify github.event context")
		assert.Contains(t, errMsg, "steps.*.outputs",
			"Should identify steps outputs context")
	})
}

func TestExtractRunSnippet(t *testing.T) {
	tests := []struct {
		name       string
		runContent string
		expression string
		want       string
	}{
		{
			name: "simple one-line",
			runContent: `  echo "Title: ${{ github.event.issue.title }}"
  echo "Done"`,
			expression: "${{ github.event.issue.title }}",
			want:       `echo "Title: ${{ github.event.issue.title }}"`,
		},
		{
			name: "multiline with indentation",
			runContent: `  if [ -n "${{ github.event.issue.number }}" ]; then
    echo "Processing"
  fi`,
			expression: "${{ github.event.issue.number }}",
			want:       `if [ -n "${{ github.event.issue.number }}" ]; then`,
		},
		{
			name:       "long line truncation",
			runContent: "  " + strings.Repeat("x", 120) + " ${{ github.event.issue.title }}",
			expression: "${{ github.event.issue.title }}",
			want:       strings.Repeat("x", 97) + "...",
		},
		{
			name:       "expression not found",
			runContent: `  echo "Hello"`,
			expression: "${{ github.event.issue.title }}",
			want:       "${{ github.event.issue.title }}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractRunSnippet(tt.runContent, tt.expression)
			assert.Equal(t, tt.want, got,
				"Snippet extraction should match expected output")
		})
	}
}

func TestDetectExpressionContext(t *testing.T) {
	tests := []struct {
		expression string
		want       string
	}{
		{
			expression: "${{ github.event.issue.title }}",
			want:       "github.event",
		},
		{
			expression: "${{ github.event.pull_request.body }}",
			want:       "github.event",
		},
		{
			expression: "${{ steps.foo.outputs.bar }}",
			want:       "steps.*.outputs",
		},
		{
			expression: "${{ steps.start-mcp-gateway.outputs.gateway-pid }}",
			want:       "steps.*.outputs",
		},
		{
			expression: "${{ inputs.user_data }}",
			want:       "workflow inputs",
		},
		{
			expression: "${{ github.actor }}",
			want:       "unknown context",
		},
	}

	for _, tt := range tests {
		t.Run(tt.expression, func(t *testing.T) {
			got := detectExpressionContext(tt.expression)
			assert.Equal(t, tt.want, got,
				"Context detection should correctly identify expression type")
		})
	}
}

func TestTemplateInjectionRealWorldPatterns(t *testing.T) {
	// Test patterns found in real workflows from the problem statement
	t.Run("stop_mcp_gateway pattern", func(t *testing.T) {
		yaml := `jobs:
  agent:
    steps:
      - name: Stop MCP gateway
        if: always()
        continue-on-error: true
        env:
          MCP_GATEWAY_PORT: ${{ steps.start-mcp-gateway.outputs.gateway-port }}
          MCP_GATEWAY_API_KEY: ${{ steps.start-mcp-gateway.outputs.gateway-api-key }}
        run: |
          bash /opt/gh-aw/actions/stop_mcp_gateway.sh ${{ steps.start-mcp-gateway.outputs.gateway-pid }}`

		err := validateNoTemplateInjection(yaml)
		require.Error(t, err, "Should detect unsafe gateway-pid usage in run command")
		assert.Contains(t, err.Error(), "steps.*.outputs",
			"Should identify as steps.outputs context")
		assert.Contains(t, err.Error(), "gateway-pid",
			"Error should mention the specific expression")
	})

	t.Run("safe version of stop_mcp_gateway", func(t *testing.T) {
		yaml := `jobs:
  agent:
    steps:
      - name: Stop MCP gateway
        if: always()
        continue-on-error: true
        env:
          MCP_GATEWAY_PORT: ${{ steps.start-mcp-gateway.outputs.gateway-port }}
          MCP_GATEWAY_API_KEY: ${{ steps.start-mcp-gateway.outputs.gateway-api-key }}
          GATEWAY_PID: ${{ steps.start-mcp-gateway.outputs.gateway-pid }}
        run: |
          bash /opt/gh-aw/actions/stop_mcp_gateway.sh "$GATEWAY_PID"`

		err := validateNoTemplateInjection(yaml)
		assert.NoError(t, err, "Should pass with gateway-pid in env variable")
	})
}

func TestTemplateInjectionHeredocFiltering(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		shouldError bool
		description string
	}{
		{
			name: "safe - heredoc with EOF delimiter",
			yaml: `jobs:
  test:
    steps:
      - name: Write config
        run: |
          cat > config.json << 'EOF'
          {"issue": "${{ github.event.issue.number }}"}
          EOF`,
			shouldError: false,
			description: "Expressions in heredocs are safe - written to files, not executed",
		},
		{
			name: "safe - heredoc with JSON delimiter",
			yaml: `jobs:
  test:
    steps:
      - name: Write JSON
        run: |
          cat > data.json << 'JSON'
          {"title": "${{ github.event.issue.title }}"}
          JSON`,
			shouldError: false,
			description: "JSON heredoc delimiter should be recognized",
		},
		{
			name: "safe - heredoc with YAML delimiter",
			yaml: `jobs:
  test:
    steps:
      - name: Write YAML
        run: |
          cat > config.yaml << 'YAML'
          title: ${{ github.event.issue.title }}
          YAML`,
			shouldError: false,
			description: "YAML heredoc delimiter should be recognized",
		},
		{
			name: "unsafe - expression outside heredoc",
			yaml: `jobs:
  test:
    steps:
      - name: Mixed pattern
        run: |
          cat > config.json << 'EOF'
          {"safe": "${{ github.event.issue.number }}"}
          EOF
          echo "Unsafe: ${{ github.event.issue.title }}"`,
			shouldError: true,
			description: "Expressions outside heredoc should still be detected",
		},
		{
			name: "safe - multiple heredocs in same run block",
			yaml: `jobs:
  test:
    steps:
      - name: Multiple heredocs
        run: |
          cat > config1.json << 'EOF'
          {"value": "${{ github.event.issue.number }}"}
          EOF
          cat > config2.json << 'EOF'
          {"title": "${{ github.event.issue.title }}"}
          EOF`,
			shouldError: false,
			description: "Multiple heredocs should all be filtered",
		},
		{
			name: "safe - unquoted heredoc delimiter",
			yaml: `jobs:
  test:
    steps:
      - name: Unquoted delimiter
        run: |
          cat > config.json << EOF
          {"issue": "${{ github.event.issue.number }}"}
          EOF`,
			shouldError: false,
			description: "Unquoted heredoc delimiters should be recognized",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateNoTemplateInjection(tt.yaml)

			if tt.shouldError {
				require.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
			}
		})
	}
}

func TestTemplateInjectionEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		shouldError bool
		description string
	}{
		{
			name:        "empty yaml",
			yaml:        "",
			shouldError: false,
			description: "Empty YAML should not cause errors",
		},
		{
			name: "no run blocks",
			yaml: `jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4`,
			shouldError: false,
			description: "YAML without run blocks should pass",
		},
		{
			name: "run block with no expressions",
			yaml: `jobs:
  test:
    steps:
      - run: echo "Hello World"`,
			shouldError: false,
			description: "Simple run command without expressions should pass",
		},
		{
			name: "malformed expression syntax",
			yaml: `jobs:
  test:
    steps:
      - run: echo "Value: ${ github.event.issue.title }"`,
			shouldError: false,
			description: "Malformed expressions (single brace) should be ignored",
		},
		{
			name: "expression with extra whitespace",
			yaml: `jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Test
        run: 'echo "Issue: ${{    github.event.issue.title    }}"'`,
			shouldError: true,
			description: "Expressions with extra whitespace should still be detected",
		},
		{
			name: "multiple steps with mixed patterns",
			yaml: `jobs:
  test:
    steps:
      - name: Safe step
        env:
          TITLE: ${{ github.event.issue.title }}
        run: echo "$TITLE"
      - name: Unsafe step
        run: echo "${{ github.event.issue.body }}"
      - name: Another safe step
        run: echo "Hello"`,
			shouldError: true,
			description: "Mixed safe and unsafe steps should detect unsafe ones",
		},
		{
			name: "expression in step name (should be safe)",
			yaml: `jobs:
  test:
    steps:
      - name: Process issue ${{ github.event.issue.number }}
        run: echo "Processing"`,
			shouldError: false,
			description: "Expressions in step names are not in run blocks",
		},
		{
			name: "expression in if condition (should be safe)",
			yaml: `jobs:
  test:
    steps:
      - name: Conditional
        if: ${{ github.event.issue.title == 'bug' }}
        run: echo "Bug issue"`,
			shouldError: false,
			description: "Expressions in if conditions are not in run blocks",
		},
		{
			name: "very long run command",
			yaml: `jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Long command
        run: |
          ` + strings.Repeat("echo 'test'\n          ", 100) + `
          echo "${{ github.event.issue.title }}"`,
			shouldError: true,
			description: "Long run blocks should still be validated",
		},
		{
			name: "nested expressions (not real GitHub syntax but test defensively)",
			yaml: `jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Nested
        run: echo "${{ ${{ github.event.issue.title }} }}"`,
			shouldError: true,
			description: "Nested expressions should be detected",
		},
		{
			name: "expression with logical operators",
			yaml: `jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Logical operators
        run: |
          if [ "${{ github.event.issue.title && github.event.issue.body }}" ]; then
            echo "Has content"
          fi`,
			shouldError: true,
			description: "Expressions with logical operators should be detected",
		},
		{
			name: "expression with string interpolation",
			yaml: `jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: String interpolation
        run: curl -X POST "https://api.github.com/issues/${{ github.event.issue.number }}/comments"`,
			shouldError: true,
			description: "Expressions interpolated in URLs should be detected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateNoTemplateInjection(tt.yaml)

			if tt.shouldError {
				require.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
			}
		})
	}
}

func TestRemoveHeredocContent(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		want     string
		hasExpr  bool
		describe string
	}{
		{
			name: "simple EOF heredoc",
			content: `cat > file << 'EOF'
{"value": "${{ github.event.issue.number }}"}
EOF
echo "done"`,
			want:     "cat > file # heredoc removed\necho \"done\"",
			hasExpr:  false,
			describe: "EOF heredoc should be removed",
		},
		{
			name: "unquoted EOF heredoc",
			content: `cat > file << EOF
{"value": "${{ github.event.issue.number }}"}
EOF`,
			want:     "cat > file # heredoc removed",
			hasExpr:  false,
			describe: "Unquoted EOF heredoc should be removed",
		},
		{
			name: "JSON delimiter",
			content: `cat > file.json << 'JSON'
{"title": "${{ github.event.issue.title }}"}
JSON`,
			want:     "cat > file.json # heredoc removed",
			hasExpr:  false,
			describe: "JSON delimiter heredoc should be removed",
		},
		{
			name: "expression outside heredoc",
			content: `cat > file << 'EOF'
{"safe": "value"}
EOF
echo "${{ github.event.issue.title }}"`,
			want:     "cat > file # heredoc removed\necho \"${{ github.event.issue.title }}\"",
			hasExpr:  true,
			describe: "Expressions outside heredoc should remain",
		},
		{
			name: "multiple heredocs",
			content: `cat > file1 << 'EOF'
{"a": "${{ github.event.issue.number }}"}
EOF
cat > file2 << 'EOF'
{"b": "${{ github.event.issue.title }}"}
EOF`,
			want:     "cat > file1 # heredoc removed\ncat > file2 # heredoc removed",
			hasExpr:  false,
			describe: "Multiple heredocs should all be removed",
		},
		{
			name:     "no heredoc",
			content:  `echo "${{ github.event.issue.title }}"`,
			want:     `echo "${{ github.event.issue.title }}"`,
			hasExpr:  true,
			describe: "Content without heredoc should be unchanged",
		},
		{
			name: "heredoc with indentation",
			content: `          cat > file << 'EOF'
          {"value": "${{ github.event.issue.number }}"}
          EOF`,
			want:     "          cat > file # heredoc removed",
			hasExpr:  false,
			describe: "Indented heredoc should be handled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := removeHeredocContent(tt.content)

			// Check if expression is present
			hasExpr := strings.Contains(got, "${{")

			assert.Equal(t, tt.hasExpr, hasExpr,
				"Expression presence mismatch: %s", tt.describe)

			if !tt.hasExpr {
				assert.NotContains(t, got, "${{",
					"Should not contain expressions after heredoc removal: %s", tt.describe)
			}
		})
	}
}

// TestTemplateInjectionYAMLKeyOrdering tests that validation correctly handles
// different YAML key orderings, particularly when env: appears after run:
func TestTemplateInjectionYAMLKeyOrdering(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		shouldError bool
		description string
	}{
		{
			name: "safe - env after run with pipe keep indicator",
			yaml: `jobs:
  test:
    steps:
      - name: Use value safely
        run: |
          echo "Line 1"
          echo "Value: $MY_VALUE"
        env:
          MY_VALUE: ${{ steps.get_value.outputs.value }}`,
			shouldError: false,
			description: "Expression in env block should be safe even when env comes after run",
		},
		{
			name: "safe - env after run with pipe strip indicator",
			yaml: `jobs:
  test:
    steps:
      - name: Use value safely
        run: |-
          echo "Line 1"
          echo "Value: $MY_VALUE"
        env:
          MY_VALUE: ${{ steps.get_value.outputs.value }}`,
			shouldError: false,
			description: "Expression in env block should be safe with run: |- (strip indicator)",
		},
		{
			name: "safe - env before run (original ordering)",
			yaml: `jobs:
  test:
    steps:
      - name: Use value safely
        env:
          MY_VALUE: ${{ steps.get_value.outputs.value }}
        run: |
          echo "Value: $MY_VALUE"`,
			shouldError: false,
			description: "Expression in env block should be safe when env comes before run",
		},
		{
			name: "safe - multiple YAML keys after run",
			yaml: `jobs:
  test:
    steps:
      - name: Complex step
        run: |
          echo "Value: $MY_VALUE"
        env:
          MY_VALUE: ${{ steps.get_value.outputs.value }}
        if: always()
        continue-on-error: true`,
			shouldError: false,
			description: "Multiple YAML keys after run should not be captured",
		},
		{
			name: "safe - with block after run",
			yaml: `jobs:
  test:
    steps:
      - name: Use action
        run: echo "Running"
        with:
          value: ${{ steps.get_value.outputs.value }}`,
			shouldError: false,
			description: "with block after run should not be captured as run content",
		},
		{
			name: "unsafe - expression directly in run block",
			yaml: `jobs:
  test:
    steps:
      - name: Unsafe usage
        run: |
          echo "Value: ${{ steps.get_value.outputs.value }}"
        env:
          OTHER: "safe"`,
			shouldError: true,
			description: "Expression directly in run block should still be detected",
		},
		{
			name: "safe - env after run in custom job step",
			yaml: `jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - id: get_value
        run: echo "value=test" >> $GITHUB_OUTPUT
      - name: Sign image
        run: |
          echo "Signing image with digest: $DIGEST"
        env:
          DIGEST: ${{ steps.build.outputs.digest }}`,
			shouldError: false,
			description: "Custom job step with env after run should be safe (reproduces issue)",
		},
		{
			name: "safe - if condition after run",
			yaml: `jobs:
  test:
    steps:
      - name: Conditional run
        run: echo "test"
        if: ${{ steps.get_value.outputs.value == 'test' }}`,
			shouldError: false,
			description: "if condition after run should not be captured",
		},
		{
			name: "safe - timeout-minutes after run",
			yaml: `jobs:
  test:
    steps:
      - name: Timed run
        run: |
          echo "Running with timeout"
        timeout-minutes: 5
        env:
          VALUE: ${{ steps.get_value.outputs.value }}`,
			shouldError: false,
			description: "timeout-minutes and env after run should not be captured",
		},
		{
			name: "safe - continue-on-error after run",
			yaml: `jobs:
  test:
    steps:
      - name: Allowed failure
        run: echo "test"
        continue-on-error: true
        env:
          VALUE: ${{ steps.get_value.outputs.value }}`,
			shouldError: false,
			description: "continue-on-error and env after run should not be captured",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateNoTemplateInjection(tt.yaml)

			if tt.shouldError {
				require.Error(t, err, tt.description)
				assert.Contains(t, err.Error(), "template injection",
					"Error should mention template injection")
			} else {
				assert.NoError(t, err, tt.description)
			}
		})
	}
}

// TestTemplateInjectionInvalidYAML tests how the validator handles invalid YAML
func TestTemplateInjectionInvalidYAML(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		shouldError bool
		description string
	}{
		{
			name: "malformed YAML - missing closing bracket",
			yaml: `jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Test
        run: echo "${{ github.event.issue.title"`,
			shouldError: false,
			description: "Invalid YAML should skip validation gracefully (no error)",
		},
		{
			name: "malformed YAML - invalid indentation",
			yaml: `jobs:
  test:
runs-on: ubuntu-latest
    steps:
      - name: Test
        run: echo "${{ github.event.issue.title }}"`,
			shouldError: false,
			description: "Invalid indentation should skip validation gracefully",
		},
		{
			name: "malformed YAML - missing colon",
			yaml: `jobs
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Test
        run: echo "${{ github.event.issue.title }}"`,
			shouldError: false,
			description: "Missing colon should skip validation gracefully",
		},
		{
			name:        "malformed YAML - tabs instead of spaces",
			yaml:        "jobs:\n\ttest:\n\t\truns-on: ubuntu-latest\n\t\tsteps:\n\t\t\t- name: Test\n\t\t\t\trun: echo \"${{ github.event.issue.title }}\"",
			shouldError: false,
			description: "Tabs in YAML should be handled (may or may not parse)",
		},
		{
			name:        "empty YAML",
			yaml:        "",
			shouldError: false,
			description: "Empty YAML should not cause errors",
		},
		{
			name:        "whitespace only YAML",
			yaml:        "   \n\n   \n",
			shouldError: false,
			description: "Whitespace-only YAML should not cause errors",
		},
		{
			name: "malformed YAML - unquoted colon in value",
			yaml: `jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Test
        run: echo "Issue: ${{ github.event.issue.title }}"`,
			shouldError: false,
			description: "Unquoted colon in string value is invalid YAML, should skip gracefully",
		},
		{
			name: "valid YAML with complex nested structure",
			yaml: `jobs:
  test:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        version: [1, 2, 3]
    steps:
      - name: Test with unsafe expression
        run: 'echo "Version: ${{ github.event.issue.number }}"'`,
			shouldError: true,
			description: "Complex nested YAML with unsafe expression should be detected",
		},
		{
			name: "valid YAML with multiple jobs",
			yaml: `jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - name: Build
        env:
          TITLE: ${{ github.event.issue.title }}
        run: echo "$TITLE"
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Test unsafe
        run: 'echo "${{ github.event.issue.body }}"'`,
			shouldError: true,
			description: "Multiple jobs with one unsafe expression should be detected",
		},
		{
			name: "YAML with comments and unsafe expression",
			yaml: `# This is a workflow
jobs:
  test:
    runs-on: ubuntu-latest
    # These are steps
    steps:
      - name: Test
        # Unsafe usage
        run: 'echo "${{ github.event.issue.title }}"'`,
			shouldError: true,
			description: "YAML with comments should parse correctly and detect unsafe patterns",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateNoTemplateInjection(tt.yaml)

			if tt.shouldError {
				require.Error(t, err, tt.description)
				assert.Contains(t, err.Error(), "template injection",
					"Error should mention template injection")
			} else {
				assert.NoError(t, err, tt.description)
			}
		})
	}
}

// TestTemplateInjectionYAMLParsingEdgeCases tests edge cases in YAML parsing
func TestTemplateInjectionYAMLParsingEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		shouldError bool
		description string
	}{
		{
			name: "run field with null value",
			yaml: `jobs:
  test:
    steps:
      - name: Test
        run: null`,
			shouldError: false,
			description: "Null run value should not cause errors",
		},
		{
			name: "run field with numeric value (invalid but should handle)",
			yaml: `jobs:
  test:
    steps:
      - name: Test
        run: 123`,
			shouldError: false,
			description: "Non-string run value should be skipped",
		},
		{
			name: "run field with boolean value",
			yaml: `jobs:
  test:
    steps:
      - name: Test
        run: true`,
			shouldError: false,
			description: "Boolean run value should be skipped",
		},
		{
			name: "run field with array value (invalid)",
			yaml: `jobs:
  test:
    steps:
      - name: Test
        run:
          - echo "test"
          - echo "test2"`,
			shouldError: false,
			description: "Array run value should be skipped",
		},
		{
			name: "run field with map value (invalid)",
			yaml: `jobs:
  test:
    steps:
      - name: Test
        run:
          command: echo "test"`,
			shouldError: false,
			description: "Map run value should be skipped",
		},
		{
			name: "deeply nested jobs structure",
			yaml: `jobs:
  test:
    runs-on: ubuntu-latest
    container:
      image: ubuntu:latest
      options: --privileged
    services:
      postgres:
        image: postgres
    steps:
      - name: Test
        env:
          VALUE: ${{ steps.test.outputs.value }}
        run: echo "$VALUE"`,
			shouldError: false,
			description: "Deeply nested structure should parse correctly",
		},
		{
			name: "steps with uses instead of run",
			yaml: `jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4
      - name: Setup
        uses: actions/setup-node@v4
        with:
          node-version: 20`,
			shouldError: false,
			description: "Steps without run fields should not cause errors",
		},
		{
			name: "mix of uses and run steps",
			yaml: `jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Build
        run: npm run build
      - name: Test unsafe
        run: 'echo "${{ github.event.issue.title }}"'
      - uses: actions/upload-artifact@v4`,
			shouldError: true,
			description: "Mix of uses and run steps should detect unsafe run blocks",
		},
		{
			name: "empty steps array",
			yaml: `jobs:
  test:
    runs-on: ubuntu-latest
    steps: []`,
			shouldError: false,
			description: "Empty steps array should not cause errors",
		},
		{
			name: "missing steps field",
			yaml: `jobs:
  test:
    runs-on: ubuntu-latest`,
			shouldError: false,
			description: "Missing steps field should not cause errors",
		},
		{
			name: "run with multiline string using pipe",
			yaml: `jobs:
  test:
    steps:
      - name: Multiline
        run: |
          echo "Line 1"
          echo "${{ github.event.issue.title }}"
          echo "Line 3"`,
			shouldError: true,
			description: "Multiline run with unsafe expression should be detected",
		},
		{
			name: "run with multiline string using greater than",
			yaml: `jobs:
  test:
    steps:
      - name: Folded
        run: >
          echo "Line 1"
          echo "${{ github.event.issue.title }}"
          echo "Line 3"`,
			shouldError: true,
			description: "Folded multiline run with unsafe expression should be detected",
		},
		{
			name: "run with literal block chomping indicators",
			yaml: `jobs:
  test:
    steps:
      - name: Strip chomping
        run: |-
          echo "${{ github.event.issue.title }}"`,
			shouldError: true,
			description: "Run with strip chomping indicator should detect unsafe patterns",
		},
		{
			name: "run with keep chomping indicator",
			yaml: `jobs:
  test:
    steps:
      - name: Keep chomping
        run: |+
          echo "${{ github.event.issue.title }}"`,
			shouldError: true,
			description: "Run with keep chomping indicator should detect unsafe patterns",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateNoTemplateInjection(tt.yaml)

			if tt.shouldError {
				require.Error(t, err, tt.description)
				assert.Contains(t, err.Error(), "template injection",
					"Error should mention template injection")
			} else {
				assert.NoError(t, err, tt.description)
			}
		})
	}
}
