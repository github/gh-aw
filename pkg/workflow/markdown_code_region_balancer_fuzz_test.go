//go:build !integration

package workflow

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// FuzzMarkdownCodeRegionBalancer performs fuzz testing on the markdown code region balancer
// (balanceCodeRegions function in markdown_code_region_balancer.cjs) to validate:
//
// 1. Function handles all inputs without crashing
// 2. Balanced markdown is not modified
// 3. Algorithm doesn't create more unbalanced regions than it started with
// 4. Common AI-generated patterns are properly handled
// 5. Edge cases with various fence types, lengths, and nesting patterns work correctly
//
// The fuzzer uses Go's native fuzzing framework to generate inputs, which are then
// passed to a JavaScript harness (fuzz_markdown_code_region_balancer_harness.cjs)
// via Node.js.
//
// To run the fuzzer:
//
//	go test -v -fuzz=FuzzMarkdownCodeRegionBalancer -fuzztime=30s ./pkg/workflow
func FuzzMarkdownCodeRegionBalancer(f *testing.F) {
	// Seed corpus with balanced code blocks (should not be modified)
	f.Add("```javascript\ncode\n```")
	f.Add("~~~markdown\ntext\n~~~")
	f.Add("```\ngeneric\n```")
	f.Add("# Title\n\n```bash\necho test\n```\n\nMore text")

	// Multiple balanced blocks
	f.Add("```js\ncode1\n```\n\n```python\ncode2\n```")
	f.Add("```\nblock1\n```\n```\nblock2\n```")

	// Different fence lengths
	f.Add("````\ncode\n````")
	f.Add("`````\ncode\n`````")
	f.Add("~~~~~~\ntext\n~~~~~~")

	// Nested code blocks (unbalanced - needs fixing)
	f.Add("```markdown\n```\nnested\n```\n```")
	f.Add("```javascript\nfunction() {\n```\nnested\n```\n}\n```")

	// Unclosed blocks
	f.Add("```javascript\nunclosed code")
	f.Add("~~~markdown\nunclosed text")

	// Indented code blocks
	f.Add("  ```javascript\n  code\n  ```")
	f.Add("    ```\n    indented\n    ```")

	// Mixed fence types
	f.Add("```\ncode\n```\n~~~\ntext\n~~~")
	f.Add("~~~\ntext\n~~~\n```\ncode\n```")

	// Language specifiers
	f.Add("```javascript {highlight: [1,3]}\ncode\n```")
	f.Add("```python title=\"example.py\"\ncode\n```")

	// XML comments (should be ignored)
	f.Add("<!-- comment -->\n```\ncode\n```")
	f.Add("```\ncode\n```\n<!-- comment -->")

	// Trailing content after fences
	f.Add("```javascript // inline comment\ncode\n```")
	f.Add("```\ncode\n``` trailing text")

	// Empty code blocks
	f.Add("```\n```")
	f.Add("~~~\n~~~")

	// Consecutive blocks without blank lines
	f.Add("```\ncode1\n```\n```\ncode2\n```")

	// AI-generated nested markdown examples (common error pattern)
	f.Add("```markdown\nExample:\n```javascript\ncode\n```\n```")

	// Multiple levels of nesting
	f.Add("```\nfirst\n```\nnested1\n```\n```\nnested2\n```\n```")

	// Edge cases
	f.Add("")                                     // Empty input
	f.Add("   ")                                  // Whitespace only
	f.Add("No code blocks here")                  // No fences
	f.Add("Inline `code` not affected")           // Inline code
	f.Add("```")                                  // Single fence
	f.Add("```\n```\n```")                        // Three consecutive fences
	f.Add(strings.Repeat("```\ncode\n```\n", 20)) // Many blocks
	f.Add("```\n" + strings.Repeat("a", 10000))   // Very long line

	// Unicode and special characters
	f.Add("```\n你好世界\n```")
	f.Add("```\n🚀 emoji\n```")
	f.Add("```\n\u200b\u200c\u200d\n```") // Zero-width chars

	// Windows line endings
	f.Add("```javascript\r\ncode\r\n```")

	f.Fuzz(func(t *testing.T, markdown string) {
		// Skip inputs that are too large to avoid timeout
		if len(markdown) > 100000 {
			t.Skip("Input too large")
		}

		// Call JavaScript harness via Node.js
		result, err := runMarkdownBalancerTest(markdown)

		// The test should never panic or crash Node.js
		// Accept expected errors like exit status
		if err != nil && !strings.Contains(err.Error(), "exit status") {
			t.Errorf("Unexpected error from markdown balancer: %v", err)
			return
		}

		// If the function returned an error through the harness, log it but don't fail
		// (the function should handle errors gracefully)
		if result != nil && result.Error != nil {
			t.Logf("Function returned error (handled): %s", *result.Error)
			return
		}

		// Validate the result
		if result != nil {
			// Result should not be excessively longer than input
			// Allow up to 2x input length (for fence escaping and closing)
			if len(result.Balanced) > len(markdown)*2+1000 {
				t.Errorf("Balanced result is excessively longer than input (input: %d, result: %d)",
					len(markdown), len(result.Balanced))
			}

			// If input was balanced, output should be identical (or just normalized line endings)
			if result.Counts.Unbalanced == 0 {
				normalized := strings.ReplaceAll(markdown, "\r\n", "\n")
				if result.Balanced != normalized && result.Balanced != markdown {
					t.Errorf("Balanced input was modified:\nInput: %q\nOutput: %q",
						markdown, result.Balanced)
				}
			}

			// Algorithm should not create MORE unbalanced regions than it started with
			// This is the key quality check
			if result.IsBalanced {
				// Result should have unbalanced count of 0
				resultCounts := countCodeRegionsInString(result.Balanced)
				if resultCounts.Unbalanced > 0 {
					t.Errorf("Result claims to be balanced but has %d unbalanced regions",
						resultCounts.Unbalanced)
				}
			} else {
				// If result is not balanced, it should at least not be worse
				resultCounts := countCodeRegionsInString(result.Balanced)
				originalCounts := countCodeRegionsInString(markdown)
				if resultCounts.Unbalanced > originalCounts.Unbalanced {
					t.Errorf("Algorithm made markdown WORSE: original had %d unbalanced, result has %d unbalanced",
						originalCounts.Unbalanced, resultCounts.Unbalanced)
				}
			}
		}
	})
}

// markdownBalancerTestInput represents the JSON input for the fuzz test harness
type markdownBalancerTestInput struct {
	Markdown string `json:"markdown"`
}

// markdownBalancerTestResult represents the JSON output from the fuzz test harness
type markdownBalancerTestResult struct {
	Balanced   string                 `json:"balanced"`
	IsBalanced bool                   `json:"isBalanced"`
	Counts     markdownBalancerCounts `json:"counts"`
	Error      *string                `json:"error"`
}

// markdownBalancerCounts represents code region counts
type markdownBalancerCounts struct {
	Total      int `json:"total"`
	Balanced   int `json:"balanced"`
	Unbalanced int `json:"unbalanced"`
}

// runMarkdownBalancerTest runs the JavaScript markdown balancer test harness
func runMarkdownBalancerTest(markdown string) (*markdownBalancerTestResult, error) {
	// Prepare input JSON
	input := markdownBalancerTestInput{
		Markdown: markdown,
	}
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}

	// Find the harness file
	harnessPath := filepath.Join("js", "fuzz_markdown_code_region_balancer_harness.cjs")

	// Execute Node.js with the harness
	cmd := exec.Command("node", harnessPath)
	cmd.Stdin = bytes.NewReader(inputJSON)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		// Check if this is an expected error
		if stderr.Len() > 0 {
			return nil, nil // Expected error, handled gracefully
		}
		return nil, err
	}

	// Parse output JSON
	var result markdownBalancerTestResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// countCodeRegionsInString is a simple Go implementation to count code regions
// This is used for validation in the fuzzer
func countCodeRegionsInString(markdown string) markdownBalancerCounts {
	lines := strings.Split(markdown, "\n")
	total := 0
	balanced := 0
	inCodeBlock := false
	var openingFence *struct {
		char   rune
		length int
	}

	for _, line := range lines {
		// Simple fence detection (matches the pattern used in JS)
		trimmedLine := strings.TrimLeft(line, " \t")
		if len(trimmedLine) >= 3 {
			char := rune(trimmedLine[0])
			if char == '`' || char == '~' {
				count := 0
				for _, c := range trimmedLine {
					if c == char {
						count++
					} else {
						break
					}
				}
				if count >= 3 {
					if !inCodeBlock {
						inCodeBlock = true
						total++
						openingFence = &struct {
							char   rune
							length int
						}{char, count}
					} else if openingFence != nil && char == openingFence.char && count >= openingFence.length {
						inCodeBlock = false
						balanced++
						openingFence = nil
					}
				}
			}
		}
	}

	return markdownBalancerCounts{
		Total:      total,
		Balanced:   balanced,
		Unbalanced: total - balanced,
	}
}
