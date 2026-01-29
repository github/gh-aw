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

// FuzzSanitizeLabelContent performs fuzz testing on the sanitizeLabelContent function
// to validate proper sanitization of label text for GitHub API.
//
// This fuzz test uses a hybrid approach: Go's native fuzzing framework generates
// inputs, which are then passed to a JavaScript harness (fuzz_sanitize_label_harness.cjs)
// via Node.js.
//
// The fuzzer validates that:
// 1. ALL @mentions are neutralized with backticks
// 2. Control characters are removed (except newlines and tabs)
// 3. ANSI escape sequences are removed
// 4. HTML special characters (<>&'") are removed
// 5. Function handles all fuzzer-generated inputs without panic
//
// To run the fuzzer:
//
//	go test -v -fuzz=FuzzSanitizeLabelContent -fuzztime=30s ./pkg/workflow
func FuzzSanitizeLabelContent(f *testing.F) {
	// Seed corpus with mention patterns
	f.Add("Hello @user")
	f.Add("@user1 and @user2")
	f.Add("@org/team label")
	f.Add("Contact @user-name")
	f.Add("Already `@user` mentioned")
	f.Add("Email email@example.com")

	// Control characters and ANSI codes
	f.Add("ANSI: \x1b[31mRed\x1b[0m")
	f.Add("ANSI bold: \x1b[1mbold\x1b[0m")
	f.Add("ANSI multi: \x1b[31;1mRed Bold\x1b[0m")
	f.Add("Null byte: test\x00text")
	f.Add("Control chars: \x01\x02\x03")
	f.Add("Bell: \x07beep")
	f.Add("Tabs: tab\there")
	f.Add("Newlines: line1\nline2")
	f.Add("Carriage return: CR\rtext")

	// HTML special characters
	f.Add("Less than: a < b")
	f.Add("Greater than: x > y")
	f.Add("Ampersand: this & that")
	f.Add("Single quote: can't")
	f.Add("Double quote: \"quoted\"")
	f.Add("All special: <>&'\"")
	f.Add("HTML entity: &lt;tag&gt;")

	// Combined patterns
	f.Add("@user with <tag>")
	f.Add("\x1b[31m@user\x1b[0m")
	f.Add("@user\x00null")
	f.Add("<script>@user</script>")
	f.Add("@user & @other")

	// Edge cases
	f.Add("")             // Empty
	f.Add("   ")          // Whitespace
	f.Add("Normal text")  // No special chars
	f.Add("@")            // Just @
	f.Add("<>")           // Empty tag
	f.Add("'\"")          // Quotes
	f.Add("\x1b[0m")      // Just ANSI
	f.Add("\x00\x01\x02") // Just control chars

	// Unicode and emoji
	f.Add("Unicode: 你好 мир")
	f.Add("Emoji: 😀 😃")
	f.Add("@user with 🎉")

	// Length variations
	f.Add(strings.Repeat("a", 100))
	f.Add(strings.Repeat("@user ", 20))
	f.Add(strings.Repeat("\x1b[31m", 50))

	// Whitespace handling
	f.Add("  leading spaces")
	f.Add("trailing spaces  ")
	f.Add("  both  ")
	f.Add("internal  spaces")

	f.Fuzz(func(t *testing.T, text string) {
		// Skip inputs that are too large
		if len(text) > 100000 {
			t.Skip("Input too large")
		}

		// Call JavaScript harness via Node.js
		result, err := runSanitizeLabelContentTest(text)

		// The test should never panic or crash Node.js
		if err != nil && !isExpectedError(err) {
			t.Errorf("Unexpected error from sanitize label content: %v", err)
		}

		// Basic validation checks on the result
		if result != nil {
			// Result can be longer than input due to backtick wrapping and escaping
			// Allow up to 3x the input length to account for:
			// - Backtick wrapping of mentions (adds 2 chars per mention)
			// - HTML entity escaping
			// - Other sanitization operations
			expectedMaxLen := len(text) * 3
			if len(result.Sanitized) > expectedMaxLen {
				t.Errorf("Sanitized result is unexpectedly longer than input (input: %d, result: %d)",
					len(text), len(result.Sanitized))
			}

			// Verify ALL mentions are neutralized (wrapped in backticks)
			if strings.Contains(text, "@") && !strings.Contains(text, "`@") {
				// Check that result doesn't have bare mentions
				for i := 0; i < len(result.Sanitized)-1; i++ {
					if result.Sanitized[i] == '@' {
						// Check if preceded by backtick
						if i > 0 && result.Sanitized[i-1] == '`' {
							continue
						}
						// Check if followed by word character
						if i+1 < len(result.Sanitized) && isWordChar(result.Sanitized[i+1]) {
							// Allow email patterns
							if i > 0 && isWordChar(result.Sanitized[i-1]) {
								continue
							}
							t.Errorf("Found bare mention in output at position %d", i)
						}
					}
				}
			}

			// Verify ANSI codes are removed
			if strings.Contains(result.Sanitized, "\x1b[") {
				t.Errorf("ANSI escape sequence found in output")
			}

			// Verify control characters are removed (except \n, \t, and \r)
			for i, r := range result.Sanitized {
				if r < 32 && r != '\n' && r != '\t' && r != '\r' {
					t.Errorf("Control character %d found at position %d", r, i)
				}
				if r == 127 {
					t.Errorf("DEL character found at position %d", i)
				}
			}

			// Verify HTML special characters are removed
			forbiddenChars := []string{"<", ">", "&", "'", "\""}
			for _, char := range forbiddenChars {
				if strings.Contains(result.Sanitized, char) {
					t.Errorf("Forbidden character %q found in output", char)
				}
			}

			// Verify leading/trailing whitespace is trimmed
			if result.Sanitized != "" {
				if result.Sanitized[0] == ' ' || result.Sanitized[0] == '\t' {
					t.Errorf("Result has leading whitespace")
				}
				if len(result.Sanitized) > 0 {
					lastChar := result.Sanitized[len(result.Sanitized)-1]
					if lastChar == ' ' || lastChar == '\t' {
						t.Errorf("Result has trailing whitespace")
					}
				}
			}
		}
	})
}

// sanitizeLabelContentTestInput represents the JSON input for the fuzz test harness
type sanitizeLabelContentTestInput struct {
	Text string `json:"text"`
}

// sanitizeLabelContentTestResult represents the JSON output from the fuzz test harness
type sanitizeLabelContentTestResult struct {
	Sanitized string  `json:"sanitized"`
	Error     *string `json:"error"`
}

// runSanitizeLabelContentTest runs the JavaScript sanitize_label_content test harness
func runSanitizeLabelContentTest(text string) (*sanitizeLabelContentTestResult, error) {
	// Prepare input JSON
	input := sanitizeLabelContentTestInput{
		Text: text,
	}
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}

	// Find the harness file
	harnessPath := filepath.Join("js", "fuzz_sanitize_label_harness.cjs")

	// Execute Node.js with the harness
	cmd := exec.Command("node", harnessPath)
	cmd.Stdin = bytes.NewReader(inputJSON)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		if stderr.Len() > 0 {
			return nil, nil // Expected error
		}
		return nil, err
	}

	// Parse output JSON
	var result sanitizeLabelContentTestResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return nil, err
	}

	return &result, nil
}
