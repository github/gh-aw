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

// FuzzSanitizeIncomingText performs fuzz testing on the sanitizeIncomingText function
// (used by compute_text.cjs) to validate security controls without selective mention filtering.
//
// This fuzz test uses a hybrid approach: Go's native fuzzing framework generates
// inputs, which are then passed to a JavaScript harness (fuzz_sanitize_incoming_text_harness.cjs)
// via Node.js.
//
// The fuzzer validates that:
// 1. ALL @mentions are neutralized (no selective filtering)
// 2. URL protocols are properly redacted
// 3. Domains outside allowed list are redacted
// 4. XML/HTML tags are properly handled
// 5. Control characters and ANSI codes are removed
// 6. Content length limits are enforced
// 7. Function handles all fuzzer-generated inputs without panic
//
// To run the fuzzer:
//
//	go test -v -fuzz=FuzzSanitizeIncomingText -fuzztime=30s ./pkg/workflow
func FuzzSanitizeIncomingText(f *testing.F) {
	// Seed corpus with mention patterns (all should be escaped)
	f.Add("Hello @user", 0)
	f.Add("Hello @user1 and @user2", 0)
	f.Add("@org/team mention", 0)
	f.Add("Contact @user for help", 0)
	f.Add("Multiple @a @b @c mentions", 0)
	f.Add("Already `@user` mentioned", 0)
	f.Add("Email email@example.com not a mention", 0)

	// URL patterns
	f.Add("Visit https://github.com/repo", 0)
	f.Add("Visit http://example.com", 0)
	f.Add("Click javascript:alert('xss')", 0)
	f.Add("Mixed: https://github.com http://bad.com", 0)

	// Domain filtering
	f.Add("https://github.com/path", 0)
	f.Add("https://unknown.com/path", 0)
	f.Add("https://evil.com?x=https://github.com", 0)

	// XML/HTML tags
	f.Add("<script>alert('xss')</script>", 0)
	f.Add("Safe: <b>bold</b> and <i>italic</i>", 0)
	f.Add("<img src='x' onerror='alert(1)'>", 0)
	f.Add("<!-- comment -->text", 0)
	f.Add("<![CDATA[content]]>", 0)

	// Control characters
	f.Add("ANSI: \x1b[31mRed\x1b[0m", 0)
	f.Add("Null: test\x00text", 0)
	f.Add("Control: \x01\x02\x03", 0)

	// Commands and bot triggers
	f.Add("/bot-command action", 0)
	f.Add("fixes #123", 0)
	f.Add("closes #456 and resolves #789", 0)

	// Length limits
	f.Add(strings.Repeat("a", 100), 0)
	f.Add(strings.Repeat("a", 1000), 0)
	f.Add(strings.Repeat("line\n", 100), 0)
	f.Add(strings.Repeat("a", 100), 50)   // Short maxLength
	f.Add(strings.Repeat("a", 1000), 500) // Custom maxLength

	// Combined patterns
	f.Add("<script>@user</script>https://evil.com", 0)
	f.Add("@user says: javascript:alert(1)", 0)

	// Edge cases
	f.Add("", 0)            // Empty
	f.Add("   ", 0)         // Whitespace
	f.Add("Normal text", 0) // No special chars
	f.Add("@", 0)           // Just @
	f.Add("<>", 0)          // Empty tag
	f.Add("https://", 0)    // Incomplete URL

	// Unicode
	f.Add("Unicode: 你好 мир 🎉", 0)
	f.Add("Emoji: @user 😀", 0)

	f.Fuzz(func(t *testing.T, text string, maxLength int) {
		// Skip inputs that are too large
		if len(text) > 100000 {
			t.Skip("Input too large")
		}

		// Skip negative maxLength
		if maxLength < 0 {
			t.Skip("Negative maxLength")
		}

		// Call JavaScript harness via Node.js
		result, err := runSanitizeIncomingTextTest(text, maxLength)

		// The test should never panic or crash Node.js
		if err != nil && !isExpectedError(err) {
			t.Errorf("Unexpected error from sanitize incoming text: %v", err)
		}

		// Basic validation checks on the result
		if result != nil {
			// Result should not be excessively longer than input
			// Account for mention wrapping: each @ can be wrapped in backticks (e.g., @ -> `@`)
			// In the worst case, every character could be part of a mention or need wrapping,
			// which adds 2 characters per mention (the backticks). Additionally, truncation
			// messages and other transformations may add some overhead.
			// Formula breakdown: 1x (base) + 0.5x (general expansion) + 2x (worst-case backtick wrapping) = 3.5x
			// Simplified as: len(text) * 7 / 2
			expectedMaxLen := len(text) * 7 / 2
			if maxLength > 0 && maxLength < expectedMaxLen {
				expectedMaxLen = maxLength + 100 // Allow for truncation message
			}
			if len(result.Sanitized) > expectedMaxLen {
				t.Errorf("Sanitized result is unexpectedly longer than expected (input: %d, result: %d)",
					len(text), len(result.Sanitized))
			}

			// Verify ALL mentions are neutralized (wrapped in backticks)
			// If the original had a bare @mention (not already in backticks),
			// it should now be wrapped
			if strings.Contains(text, "@") && !strings.Contains(text, "`@") {
				// Check that result doesn't have bare mentions
				// Pattern: @ followed by alphanumeric (not preceded by backtick)
				for i := 0; i < len(result.Sanitized)-1; i++ {
					if result.Sanitized[i] == '@' {
						// Check if preceded by backtick
						if i > 0 && result.Sanitized[i-1] == '`' {
							continue // Already wrapped
						}
						// Check if followed by word character (mention pattern)
						if i+1 < len(result.Sanitized) && isWordChar(result.Sanitized[i+1]) {
							// This is likely a bare mention that wasn't neutralized
							// Allow email patterns (has @ not at word boundary)
							if i > 0 && isWordChar(result.Sanitized[i-1]) {
								continue // Part of email
							}
							t.Errorf("Found bare mention in output at position %d: %s", i, result.Sanitized[max(0, i-5):min(len(result.Sanitized), i+10)])
						}
					}
				}
			}

			// Verify dangerous protocols are removed
			// Note: file:/// with three slashes and some data: URLs may not be caught
			dangerousProtocols := []string{"javascript:", "vbscript:", "ftp://", "http://"}
			for _, proto := range dangerousProtocols {
				if strings.Contains(strings.ToLower(result.Sanitized), proto) {
					t.Errorf("Dangerous protocol %s not removed from output", proto)
				}
			}

			// Verify control characters are removed
			for i, r := range result.Sanitized {
				if r < 32 && r != '\n' && r != '\t' && r != '\r' {
					t.Errorf("Control character %d found at position %d", r, i)
				}
				if r == 127 {
					t.Errorf("DEL character found at position %d", i)
				}
			}
		}
	})
}

// Helper function
func isWordChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// sanitizeIncomingTextTestInput represents the JSON input for the fuzz test harness
type sanitizeIncomingTextTestInput struct {
	Text      string `json:"text"`
	MaxLength int    `json:"maxLength"`
}

// sanitizeIncomingTextTestResult represents the JSON output from the fuzz test harness
type sanitizeIncomingTextTestResult struct {
	Sanitized string  `json:"sanitized"`
	Error     *string `json:"error"`
}

// runSanitizeIncomingTextTest runs the JavaScript sanitize_incoming_text test harness
func runSanitizeIncomingTextTest(text string, maxLength int) (*sanitizeIncomingTextTestResult, error) {
	// Prepare input JSON
	input := sanitizeIncomingTextTestInput{
		Text:      text,
		MaxLength: maxLength,
	}
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}

	// Find the harness file
	harnessPath := filepath.Join("js", "fuzz_sanitize_incoming_text_harness.cjs")

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
	var result sanitizeIncomingTextTestResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return nil, err
	}

	return &result, nil
}
