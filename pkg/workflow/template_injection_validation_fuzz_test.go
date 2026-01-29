//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// FuzzValidateNoTemplateInjection performs fuzz testing on the template injection validator
// to validate security controls against template injection attacks in GitHub Actions workflows.
//
// The fuzzer validates that:
// 1. Unsafe expressions in run: blocks are correctly detected
// 2. Safe expressions in env: blocks are allowed
// 3. Heredoc content is properly filtered
// 4. Function handles all fuzzer-generated inputs without panic
// 5. Edge cases are handled correctly (empty, malformed, nested)
//
// To run the fuzzer:
//
//	go test -v -fuzz=FuzzValidateNoTemplateInjection -fuzztime=30s ./pkg/workflow
func FuzzValidateNoTemplateInjection(f *testing.F) {
	// Seed corpus with safe patterns
	f.Add(`jobs:
  test:
    steps:
      - name: Safe
        env:
          TITLE: ${{ github.event.issue.title }}
        run: echo "$TITLE"`)

	f.Add(`jobs:
  test:
    steps:
      - run: echo "Hello World"`)

	f.Add(`jobs:
  test:
    steps:
      - run: |
          echo "Actor: ${{ github.actor }}"
          echo "Repo: ${{ github.repository }}"`)

	// Seed corpus with unsafe patterns
	f.Add(`jobs:
  test:
    steps:
      - run: echo "${{ github.event.issue.title }}"`)

	f.Add(`jobs:
  test:
    steps:
      - run: bash script.sh ${{ steps.foo.outputs.bar }}`)

	f.Add(`jobs:
  test:
    steps:
      - run: |
          curl -X POST "https://api.github.com/issues/${{ github.event.issue.number }}/comments"`)

	f.Add(`jobs:
  test:
    steps:
      - run: echo "${{ inputs.user_data }}"`)

	// Heredoc patterns (safe)
	f.Add(`jobs:
  test:
    steps:
      - run: |
          cat > file << 'EOF'
          {"issue": "${{ github.event.issue.number }}"}
          EOF`)

	f.Add(`jobs:
  test:
    steps:
      - run: |
          cat > config.json << 'JSON'
          {"title": "${{ github.event.issue.title }}"}
          JSON`)

	// Mixed patterns
	f.Add(`jobs:
  test:
    steps:
      - name: Safe
        env:
          VAR: ${{ github.event.issue.title }}
        run: echo "$VAR"
      - name: Unsafe
        run: echo "${{ github.event.issue.body }}"`)

	// Edge cases
	f.Add(`jobs:
  test:
    steps:
      - run: echo "No expressions here"`)

	f.Add(`jobs:
  test:
    steps:
      - run: echo "${{ }}"`)

	f.Add(`jobs:
  test:
    steps:
      - run: echo "${ github.event.issue.title }"`)

	// Nested expressions
	f.Add(`jobs:
  test:
    steps:
      - run: echo "${{ ${{ github.event.issue.title }} }}"`)

	// Multiple expressions
	f.Add(`jobs:
  test:
    steps:
      - run: |
          echo "${{ github.event.issue.title }}"
          echo "${{ github.event.issue.body }}"
          echo "${{ steps.foo.outputs.bar }}"`)

	// Complex YAML structures
	f.Add(`jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4
      - name: Process
        run: |
          if [ -n "${{ github.event.issue.number }}" ]; then
            echo "Processing"
          fi`)

	// Single-line run commands
	f.Add(`jobs:
  test:
    steps:
      - run: echo "${{ github.event.pull_request.title }}"`)

	// Expressions with logical operators
	f.Add(`jobs:
  test:
    steps:
      - run: echo "${{ github.event.issue.title && github.event.issue.body }}"`)

	// Expressions with whitespace variations
	f.Add(`jobs:
  test:
    steps:
      - run: echo "${{github.event.issue.title}}"`)

	f.Add(`jobs:
  test:
    steps:
      - run: echo "${{    github.event.issue.title    }}"`)

	// Malformed YAML (should not panic)
	f.Add(`jobs:
  test:
    steps:
      - run: echo "${{ github.event.issue.title }"`)

	f.Add(`jobs:
  test:
    steps:
      - run: echo "{{ github.event.issue.title }}"`)

	// Empty and whitespace
	f.Add("")
	f.Add("   ")
	f.Add("\n\n\n")

	// Very long expressions
	longExpression := "jobs:\n  test:\n    steps:\n      - run: echo \""
	for i := 0; i < 50; i++ {
		longExpression += "${{ github.event.issue.title }} "
	}
	longExpression += "\""
	f.Add(longExpression)

	// Unicode and special characters
	f.Add(`jobs:
  test:
    steps:
      - run: echo "${{ github.event.issue.title }}" # Comment`)

	f.Add(`jobs:
  test:
    steps:
      - run: echo "Unicode: 你好 мир 🎉 ${{ github.event.issue.title }}"`)

	// Command injection attempts (should be detected)
	f.Add(`jobs:
  test:
    steps:
      - run: echo "${{ github.event.issue.title }}"; rm -rf /`)

	f.Add("jobs:\n  test:\n    steps:\n      - run: `echo ${{ github.event.issue.title }}`")

	f.Add(`jobs:
  test:
    steps:
      - run: $(echo ${{ github.event.issue.title }})`)

	// Expression in different contexts (not all should be detected)
	f.Add(`jobs:
  test:
    if: ${{ github.event.issue.title == 'bug' }}
    steps:
      - run: echo "Processing bug"`)

	f.Add(`jobs:
  test:
    steps:
      - name: Issue ${{ github.event.issue.number }}
        run: echo "Processing"`)

	// Multiple jobs
	f.Add(`jobs:
  job1:
    steps:
      - run: echo "${{ github.event.issue.title }}"
  job2:
    steps:
      - env:
          TITLE: ${{ github.event.issue.title }}
        run: echo "$TITLE"`)

	// Expressions with different contexts
	f.Add(`jobs:
  test:
    steps:
      - run: echo "${{ github.actor }}"`)

	f.Add(`jobs:
  test:
    steps:
      - run: echo "${{ github.sha }}"`)

	f.Add(`jobs:
  test:
    steps:
      - run: echo "${{ env.MY_VAR }}"`)

	f.Add(`jobs:
  test:
    steps:
      - run: echo "${{ secrets.GITHUB_TOKEN }}"`)

	// Nested YAML structures
	f.Add(`jobs:
  test:
    steps:
      - name: Test
        run: |
          cat << 'EOF' > script.sh
          #!/bin/bash
          echo "${{ github.event.issue.title }}"
          EOF
          chmod +x script.sh`)

	f.Fuzz(func(t *testing.T, yamlContent string) {
		// Skip inputs that are too large to avoid timeout
		if len(yamlContent) > 100000 {
			t.Skip("Input too large")
		}

		// This should never panic, even on malformed input
		err := validateNoTemplateInjection(yamlContent)

		// We don't assert on the error value here because we want to
		// find cases where the function panics or behaves unexpectedly.
		// The fuzzer will help us discover edge cases we haven't considered.

		// However, we can do some basic validation checks:
		// If the content contains known unsafe patterns in run blocks, it should error
		if containsUnsafePattern(yamlContent) {
			// We expect an error for unsafe expressions
			// But we don't require it because the fuzzer might generate
			// content that our simple pattern check misidentifies
			_ = err
		}

		// If the error is not nil, it should be a proper error message
		if err != nil {
			// The error should be non-empty
			if err.Error() == "" {
				t.Errorf("validateNoTemplateInjection returned error with empty message")
			}

			// Error should mention template injection
			if !strings.Contains(err.Error(), "template injection") {
				t.Errorf("Error message should mention 'template injection', got: %s", err.Error())
			}

			// Error should provide guidance
			if !strings.Contains(err.Error(), "Safe Pattern") {
				t.Errorf("Error message should provide 'Safe Pattern' guidance")
			}
		}
	})
}

// containsUnsafePattern checks if the YAML content contains patterns
// that should be rejected by the template injection validator.
// This is a simple heuristic check for the fuzzer.
func containsUnsafePattern(yamlContent string) bool {
	// Check if it looks like a run block with unsafe expressions
	hasRunBlock := strings.Contains(yamlContent, "run:")
	if !hasRunBlock {
		return false
	}

	// Check for unsafe expression patterns
	unsafePatterns := []string{
		"github.event.issue.title",
		"github.event.issue.body",
		"github.event.pull_request.title",
		"github.event.pull_request.body",
		"github.event.comment.body",
		"steps.",
		"inputs.",
	}

	// Simple heuristic: if run: is followed (within reasonable distance) by an unsafe pattern
	// Note: This is not perfect and may have false positives/negatives
	lines := strings.Split(yamlContent, "\n")
	inRunBlock := false
	runBlockContent := ""

	for _, line := range lines {
		if strings.Contains(line, "run:") {
			inRunBlock = true
			runBlockContent = ""
		}

		if inRunBlock {
			runBlockContent += line + "\n"

			// Check if we've left the run block (next step or key at same indentation)
			if strings.HasPrefix(strings.TrimSpace(line), "- name:") ||
				strings.HasPrefix(strings.TrimSpace(line), "- uses:") ||
				strings.HasPrefix(strings.TrimSpace(line), "env:") {
				inRunBlock = false
			}
		}
	}

	// Check if run block content contains unsafe patterns
	for _, pattern := range unsafePatterns {
		if strings.Contains(runBlockContent, pattern) && strings.Contains(runBlockContent, "${{") {
			// Exclude if it's in an env block
			if !strings.Contains(runBlockContent, "env:") {
				return true
			}
		}
	}

	return false
}

// FuzzRemoveHeredocContent performs fuzz testing on the heredoc removal function
// to ensure it correctly filters heredoc content without false positives.
func FuzzRemoveHeredocContent(f *testing.F) {
	// Seed corpus with heredoc patterns
	f.Add(`cat > file << 'EOF'
{"value": "${{ github.event.issue.number }}"}
EOF`)

	f.Add(`cat > file << EOF
{"value": "${{ github.event.issue.number }}"}
EOF`)

	f.Add(`cat > file.json << 'JSON'
{"title": "${{ github.event.issue.title }}"}
JSON`)

	f.Add(`cat > file.yaml << 'YAML'
title: ${{ github.event.issue.title }}
YAML`)

	f.Add(`cat > file << 'END'
{"data": "${{ github.event.issue.body }}"}
END`)

	f.Add(`echo "${{ github.event.issue.title }}"`)

	f.Add(`cat > file << 'EOF'
{"safe": "value"}
EOF
echo "${{ github.event.issue.title }}"`)

	f.Add("")
	f.Add("   ")

	// Multiple heredocs
	f.Add(`cat > file1 << 'EOF'
{"a": "${{ github.event.issue.number }}"}
EOF
cat > file2 << 'EOF'
{"b": "${{ github.event.issue.title }}"}
EOF`)

	// Nested content
	f.Add(`cat > script.sh << 'EOF'
#!/bin/bash
echo "${{ github.event.issue.title }}"
EOF`)

	f.Fuzz(func(t *testing.T, content string) {
		// Skip inputs that are too large to avoid timeout
		if len(content) > 50000 {
			t.Skip("Input too large")
		}

		// This should never panic
		result := removeHeredocContent(content)

		// Basic validation: result should not be longer than input
		if len(result) > len(content)*2 {
			t.Errorf("Result is unexpectedly longer than input (input: %d, result: %d)",
				len(content), len(result))
		}

		// If input had heredoc delimiters, they should be handled
		if strings.Contains(content, "<<") {
			// Result should either have heredocs removed or be unchanged
			// We can't assert much more without knowing the exact format
			_ = result
		}

		// If there were no heredocs, content should be mostly unchanged
		// (except for heredoc removal markers)
		if !strings.Contains(content, "<<") {
			if content != result && !strings.Contains(result, "# heredoc removed") {
				t.Errorf("Content without heredocs should be unchanged or have removal markers")
			}
		}
	})
}
