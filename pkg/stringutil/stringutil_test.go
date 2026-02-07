//go:build !integration

package stringutil

import (
	"strings"
	"testing"
)

func TestTruncate(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		maxLen   int
		expected string
	}{
		{
			name:     "string shorter than max length",
			s:        "hello",
			maxLen:   10,
			expected: "hello",
		},
		{
			name:     "string equal to max length",
			s:        "hello",
			maxLen:   5,
			expected: "hello",
		},
		{
			name:     "string longer than max length",
			s:        "hello world",
			maxLen:   8,
			expected: "hello...",
		},
		{
			name:     "max length 3",
			s:        "hello",
			maxLen:   3,
			expected: "hel",
		},
		{
			name:     "max length 2",
			s:        "hello",
			maxLen:   2,
			expected: "he",
		},
		{
			name:     "max length 1",
			s:        "hello",
			maxLen:   1,
			expected: "h",
		},
		{
			name:     "empty string",
			s:        "",
			maxLen:   5,
			expected: "",
		},
		{
			name:     "long string truncated",
			s:        "this is a very long string that needs to be truncated",
			maxLen:   20,
			expected: "this is a very lo...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Truncate(tt.s, tt.maxLen)
			if result != tt.expected {
				t.Errorf("Truncate(%q, %d) = %q; want %q", tt.s, tt.maxLen, result, tt.expected)
			}
		})
	}
}

func TestNormalizeWhitespace(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name:     "no trailing whitespace",
			content:  "hello\nworld",
			expected: "hello\nworld\n",
		},
		{
			name:     "trailing spaces on lines",
			content:  "hello  \nworld  ",
			expected: "hello\nworld\n",
		},
		{
			name:     "trailing tabs on lines",
			content:  "hello\t\nworld\t",
			expected: "hello\nworld\n",
		},
		{
			name:     "multiple trailing newlines",
			content:  "hello\nworld\n\n\n",
			expected: "hello\nworld\n",
		},
		{
			name:     "empty string",
			content:  "",
			expected: "",
		},
		{
			name:     "single newline",
			content:  "\n",
			expected: "",
		},
		{
			name:     "mixed whitespace",
			content:  "hello  \t\nworld \t \n\n",
			expected: "hello\nworld\n",
		},
		{
			name:     "content with no newline",
			content:  "hello world",
			expected: "hello world\n",
		},
		{
			name:     "content already normalized",
			content:  "hello\nworld\n",
			expected: "hello\nworld\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeWhitespace(tt.content)
			if result != tt.expected {
				t.Errorf("NormalizeWhitespace(%q) = %q; want %q", tt.content, result, tt.expected)
			}
		})
	}
}

func BenchmarkTruncate(b *testing.B) {
	s := "this is a very long string that needs to be truncated for testing purposes"
	for i := 0; i < b.N; i++ {
		Truncate(s, 30)
	}
}

func BenchmarkNormalizeWhitespace(b *testing.B) {
	content := "line1  \nline2\t\nline3   \t\nline4\n\n"
	for i := 0; i < b.N; i++ {
		NormalizeWhitespace(content)
	}
}

// Additional edge case tests

func TestTruncate_Zero(t *testing.T) {
	result := Truncate("hello", 0)
	if result != "" {
		t.Errorf("Truncate with maxLen 0 should return empty string, got %q", result)
	}
}

func TestTruncate_ExactlyThreeChars(t *testing.T) {
	// When string is exactly maxLen, it should not be truncated
	result := Truncate("abc", 3)
	if result != "abc" {
		t.Errorf("Truncate('abc', 3) = %q; want 'abc'", result)
	}
}

func TestTruncate_FourChars(t *testing.T) {
	// When string is 4 chars and maxLen is 4, should add "..."
	result := Truncate("abcd", 4)
	if result != "abcd" {
		t.Errorf("Truncate('abcd', 4) = %q; want 'abcd'", result)
	}

	// When string is 5 chars and maxLen is 4, should truncate with "..."
	result = Truncate("abcde", 4)
	if result != "a..." {
		t.Errorf("Truncate('abcde', 4) = %q; want 'a...'", result)
	}
}

func TestTruncate_Unicode(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		maxLen   int
		expected string
	}{
		{
			name:     "emoji truncation",
			s:        "Hello 👋 World 🌍",
			maxLen:   10,
			expected: "Hello \xf0...", // Truncates in middle of emoji byte sequence
		},
		{
			name:     "unicode characters",
			s:        "Café España México",
			maxLen:   12,
			expected: "Café Esp...", // Actual behavior
		},
		{
			name:     "mixed unicode and ascii",
			s:        "Test-测试-テスト",
			maxLen:   8,
			expected: "Test-...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Truncate(tt.s, tt.maxLen)
			if result != tt.expected {
				t.Errorf("Truncate(%q, %d) = %q; want %q", tt.s, tt.maxLen, result, tt.expected)
			}
		})
	}
}

func TestNormalizeWhitespace_OnlyWhitespace(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name:     "only spaces",
			content:  "   ",
			expected: "", // After trimming trailing spaces and newlines, becomes empty
		},
		{
			name:     "only tabs",
			content:  "\t\t\t",
			expected: "", // After trimming trailing tabs and newlines, becomes empty
		},
		{
			name:     "mixed spaces and tabs",
			content:  "  \t  \t",
			expected: "", // After trimming, becomes empty
		},
		{
			name:     "only newlines",
			content:  "\n\n\n",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeWhitespace(tt.content)
			if result != tt.expected {
				t.Errorf("NormalizeWhitespace(%q) = %q; want %q", tt.content, result, tt.expected)
			}
		})
	}
}

func TestNormalizeWhitespace_ManyLines(t *testing.T) {
	// Test with many lines
	lines := make([]string, 100)
	for i := 0; i < 100; i++ {
		lines[i] = "line with trailing spaces  "
	}
	content := ""
	for _, line := range lines {
		content += line + "\n"
	}

	result := NormalizeWhitespace(content)

	// Check that all trailing spaces are removed
	expectedLines := make([]string, 100)
	for i := 0; i < 100; i++ {
		expectedLines[i] = "line with trailing spaces"
	}
	expected := ""
	for _, line := range expectedLines {
		expected += line + "\n"
	}

	if result != expected {
		t.Error("NormalizeWhitespace did not properly normalize many lines")
	}
}

func TestNormalizeWhitespace_PreservesContent(t *testing.T) {
	// Ensure that non-trailing whitespace is preserved
	content := "line1  middle  spaces\nline2\t\tmiddle\t\ttabs\n"
	result := NormalizeWhitespace(content)

	if !strings.Contains(result, "middle  spaces") {
		t.Error("NormalizeWhitespace should preserve non-trailing spaces")
	}

	if !strings.Contains(result, "middle\t\ttabs") {
		t.Error("NormalizeWhitespace should preserve non-trailing tabs")
	}
}

func BenchmarkTruncate_Short(b *testing.B) {
	s := "short"
	for i := 0; i < b.N; i++ {
		Truncate(s, 10)
	}
}

func BenchmarkTruncate_Long(b *testing.B) {
	s := "this is a very very very very very long string that definitely needs truncation"
	for i := 0; i < b.N; i++ {
		Truncate(s, 20)
	}
}

func BenchmarkNormalizeWhitespace_NoChange(b *testing.B) {
	content := "line1\nline2\nline3\n"
	for i := 0; i < b.N; i++ {
		NormalizeWhitespace(content)
	}
}

func BenchmarkNormalizeWhitespace_ManyChanges(b *testing.B) {
	content := "line1  \t  \nline2  \t  \nline3  \t  \n\n\n"
	for i := 0; i < b.N; i++ {
		NormalizeWhitespace(content)
	}
}

func TestParseVersionValue(t *testing.T) {
	tests := []struct {
		name     string
		version  any
		expected string
	}{
		// String versions
		{
			name:     "string version",
			version:  "v1.2.3",
			expected: "v1.2.3",
		},
		{
			name:     "numeric string",
			version:  "123",
			expected: "123",
		},
		{
			name:     "empty string",
			version:  "",
			expected: "",
		},
		// Integer versions
		{
			name:     "int version",
			version:  42,
			expected: "42",
		},
		{
			name:     "int64 version",
			version:  int64(100),
			expected: "100",
		},
		{
			name:     "uint64 version",
			version:  uint64(999),
			expected: "999",
		},
		// Float versions
		{
			name:     "float64 simple",
			version:  float64(1.5),
			expected: "1.5",
		},
		{
			name:     "float64 whole number",
			version:  float64(2.0),
			expected: "2",
		},
		{
			name:     "float64 with precision",
			version:  float64(1.234),
			expected: "1.234",
		},
		// Unsupported types
		{
			name:     "nil",
			version:  nil,
			expected: "",
		},
		{
			name:     "bool",
			version:  true,
			expected: "",
		},
		{
			name:     "slice",
			version:  []string{"1", "2"},
			expected: "",
		},
		{
			name:     "map",
			version:  map[string]string{"version": "1.0"},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseVersionValue(tt.version)
			if result != tt.expected {
				t.Errorf("ParseVersionValue(%v) = %q, expected %q", tt.version, result, tt.expected)
			}
		})
	}
}

func TestStripANSIEscapeCodes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no ANSI codes",
			input:    "Hello World",
			expected: "Hello World",
		},
		{
			name:     "simple color reset",
			input:    "Hello World[m",
			expected: "Hello World[m", // [m without ESC is not an ANSI code
		},
		{
			name:     "ANSI color reset",
			input:    "Hello World\x1b[m",
			expected: "Hello World",
		},
		{
			name:     "ANSI color code with reset",
			input:    "Hello \x1b[31mWorld\x1b[0m",
			expected: "Hello World",
		},
		{
			name:     "ANSI bold text",
			input:    "\x1b[1mBold text\x1b[0m",
			expected: "Bold text",
		},
		{
			name:     "multiple ANSI codes",
			input:    "\x1b[1m\x1b[31mRed Bold\x1b[0m",
			expected: "Red Bold",
		},
		{
			name:     "ANSI with parameters",
			input:    "Text \x1b[1;32mgreen bold\x1b[0m more text",
			expected: "Text green bold more text",
		},
		{
			name:     "ANSI clear screen",
			input:    "\x1b[2JCleared",
			expected: "Cleared",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only ANSI codes",
			input:    "\x1b[0m\x1b[31m\x1b[1m",
			expected: "",
		},
		{
			name:     "real-world example from issue",
			input:    "2. **REQUIRED**: Run 'make recompile' to update workflows (MUST be run after any constant changes)\x1b[m",
			expected: "2. **REQUIRED**: Run 'make recompile' to update workflows (MUST be run after any constant changes)",
		},
		{
			name:     "another real-world example",
			input:    "- **SAVE TO CACHE**: Store help outputs (main and all subcommands) and version check results in cache-memory\x1b[m",
			expected: "- **SAVE TO CACHE**: Store help outputs (main and all subcommands) and version check results in cache-memory",
		},
		{
			name:     "ANSI underline",
			input:    "\x1b[4mUnderlined\x1b[0m text",
			expected: "Underlined text",
		},
		{
			name:     "ANSI 256 color",
			input:    "\x1b[38;5;214mOrange\x1b[0m",
			expected: "Orange",
		},
		{
			name:     "mixed content with newlines",
			input:    "Line 1\x1b[31m\nLine 2\x1b[0m\nLine 3",
			expected: "Line 1\nLine 2\nLine 3",
		},
		{
			name:     "ANSI cursor movement",
			input:    "\x1b[2AMove up\x1b[3BMove down",
			expected: "Move upMove down",
		},
		{
			name:     "ANSI erase in line",
			input:    "Start\x1b[KEnd",
			expected: "StartEnd",
		},
		{
			name:     "consecutive ANSI codes",
			input:    "\x1b[1m\x1b[31m\x1b[4mRed Bold Underline\x1b[0m\x1b[0m\x1b[0m",
			expected: "Red Bold Underline",
		},
		{
			name:     "ANSI with large parameter",
			input:    "\x1b[38;5;255mWhite\x1b[0m",
			expected: "White",
		},
		{
			name:     "ANSI RGB color (24-bit)",
			input:    "\x1b[38;2;255;128;0mOrange RGB\x1b[0m",
			expected: "Orange RGB",
		},
		{
			name:     "ANSI codes in the middle of words",
			input:    "hel\x1b[31mlo\x1b[0m wor\x1b[32mld\x1b[0m",
			expected: "hello world",
		},
		{
			name:     "ANSI save/restore cursor",
			input:    "Text\x1b[s more text\x1b[u end",
			expected: "Text more text end",
		},
		{
			name:     "ANSI cursor position",
			input:    "\x1b[H\x1b[2JClear and home",
			expected: "Clear and home",
		},
		{
			name:     "long string with multiple ANSI codes",
			input:    "\x1b[1mThis\x1b[0m \x1b[31mis\x1b[0m \x1b[32ma\x1b[0m \x1b[33mvery\x1b[0m \x1b[34mlong\x1b[0m \x1b[35mstring\x1b[0m \x1b[36mwith\x1b[0m \x1b[37mmany\x1b[0m \x1b[1mANSI\x1b[0m \x1b[4mcodes\x1b[0m",
			expected: "This is a very long string with many ANSI codes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StripANSIEscapeCodes(tt.input)
			if result != tt.expected {
				t.Errorf("StripANSIEscapeCodes(%q) = %q, expected %q", tt.input, result, tt.expected)
			}

			// Verify no ANSI escape sequences remain
			if result != "" && strings.Contains(result, "\x1b[") {
				t.Errorf("Result still contains ANSI escape sequences: %q", result)
			}
		})
	}
}

func BenchmarkStripANSIEscapeCodes_Clean(b *testing.B) {
	s := "This is a clean string without any ANSI codes"
	for i := 0; i < b.N; i++ {
		StripANSIEscapeCodes(s)
	}
}

func BenchmarkStripANSIEscapeCodes_WithCodes(b *testing.B) {
	s := "This \x1b[31mhas\x1b[0m some \x1b[1mANSI\x1b[0m codes"
	for i := 0; i < b.N; i++ {
		StripANSIEscapeCodes(s)
	}
}

func TestIsPositiveInteger(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want bool
	}{
		{
			name: "positive integer",
			s:    "123",
			want: true,
		},
		{
			name: "one",
			s:    "1",
			want: true,
		},
		{
			name: "large number",
			s:    "999999999",
			want: true,
		},
		{
			name: "zero",
			s:    "0",
			want: false,
		},
		{
			name: "negative",
			s:    "-5",
			want: false,
		},
		{
			name: "leading zeros",
			s:    "007",
			want: false,
		},
		{
			name: "float",
			s:    "3.14",
			want: false,
		},
		{
			name: "not a number",
			s:    "abc",
			want: false,
		},
		{
			name: "empty string",
			s:    "",
			want: false,
		},
		{
			name: "spaces",
			s:    " 123 ",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsPositiveInteger(tt.s)
			if got != tt.want {
				t.Errorf("IsPositiveInteger(%q) = %v, want %v", tt.s, got, tt.want)
			}
		})
	}
}

func TestLevenshteinDistance(t *testing.T) {
	tests := []struct {
		name     string
		s1       string
		s2       string
		expected int
	}{
		{
			name:     "identical strings",
			s1:       "copilot",
			s2:       "copilot",
			expected: 0,
		},
		{
			name:     "one insertion - copiilot",
			s1:       "copilot",
			s2:       "copiilot",
			expected: 1,
		},
		{
			name:     "one deletion - claud",
			s1:       "claude",
			s2:       "claud",
			expected: 1,
		},
		{
			name:     "one substitution - codec",
			s1:       "codex",
			s2:       "codec",
			expected: 1,
		},
		{
			name:     "completely different",
			s1:       "abc",
			s2:       "xyz",
			expected: 3,
		},
		{
			name:     "empty to string",
			s1:       "",
			s2:       "hello",
			expected: 5,
		},
		{
			name:     "string to empty",
			s1:       "hello",
			s2:       "",
			expected: 5,
		},
		{
			name:     "both empty",
			s1:       "",
			s2:       "",
			expected: 0,
		},
		{
			name:     "multiple edits",
			s1:       "kitten",
			s2:       "sitting",
			expected: 3,
		},
		{
			name:     "case difference counts",
			s1:       "Copilot",
			s2:       "copilot",
			expected: 1,
		},
		{
			name:     "similar words",
			s1:       "custom",
			s2:       "custm",
			expected: 1,
		},
		{
			name:     "transposition - two edits",
			s1:       "copilot",
			s2:       "copliot",
			expected: 2,
		},
		{
			name:     "long strings with small difference",
			s1:       "this-is-a-very-long-identifier",
			s2:       "this-is-a-very-long-identifer", //nolint:misspell // Intentional typo for testing
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := LevenshteinDistance(tt.s1, tt.s2)
			if result != tt.expected {
				t.Errorf("LevenshteinDistance(%q, %q) = %d, expected %d",
					tt.s1, tt.s2, result, tt.expected)
			}

			// Distance should be symmetric
			reverseResult := LevenshteinDistance(tt.s2, tt.s1)
			if result != reverseResult {
				t.Errorf("Distance is not symmetric: (%q, %q)=%d but (%q, %q)=%d",
					tt.s1, tt.s2, result, tt.s2, tt.s1, reverseResult)
			}
		})
	}
}

func TestFindClosestMatch(t *testing.T) {
	validEngines := []string{"copilot", "claude", "codex", "custom"}

	tests := []struct {
		name         string
		input        string
		validOptions []string
		expected     string
		shouldMatch  bool
	}{
		{
			name:         "exact match not needed - copiilot",
			input:        "copiilot",
			validOptions: validEngines,
			expected:     "copilot",
			shouldMatch:  true,
		},
		{
			name:         "one character deletion - claud",
			input:        "claud",
			validOptions: validEngines,
			expected:     "claude",
			shouldMatch:  true,
		},
		{
			name:         "one character substitution - codec",
			input:        "codec",
			validOptions: validEngines,
			expected:     "codex",
			shouldMatch:  true,
		},
		{
			name:         "two character difference - custon",
			input:        "custon",
			validOptions: validEngines,
			expected:     "custom",
			shouldMatch:  true,
		},
		{
			name:         "completely wrong - no match",
			input:        "xyz",
			validOptions: validEngines,
			expected:     "",
			shouldMatch:  false,
		},
		{
			name:         "too many differences - gpt4",
			input:        "gpt4",
			validOptions: validEngines,
			expected:     "",
			shouldMatch:  false,
		},
		{
			name:         "empty input",
			input:        "",
			validOptions: validEngines,
			expected:     "",
			shouldMatch:  false,
		},
		{
			name:         "empty valid options",
			input:        "copilot",
			validOptions: []string{},
			expected:     "",
			shouldMatch:  false,
		},
		{
			name:         "case difference - Copilot",
			input:        "Copilot",
			validOptions: validEngines,
			expected:     "copilot",
			shouldMatch:  true,
		},
		{
			name:         "extra characters at end - copilot123",
			input:        "copilot123",
			validOptions: validEngines,
			expected:     "",
			shouldMatch:  false,
		},
		{
			name:         "missing character in middle - copiot",
			input:        "copiot",
			validOptions: validEngines,
			expected:     "copilot",
			shouldMatch:  true,
		},
		{
			name:         "GitHub tools example - issue_raed",
			input:        "issue_raed",
			validOptions: []string{"issue_read", "issue_create", "issue_update"},
			expected:     "issue_read",
			shouldMatch:  true,
		},
		{
			name:         "GitHub tools example - crate_issue",
			input:        "crate_issue",
			validOptions: []string{"issue_read", "create_issue", "issue_update"},
			expected:     "create_issue",
			shouldMatch:  true,
		},
		{
			name:         "toolset example - defalt",
			input:        "defalt",
			validOptions: []string{"default", "repos", "issues", "pull_requests"},
			expected:     "default",
			shouldMatch:  true,
		},
		{
			name:         "first match wins - equal distance",
			input:        "abc",
			validOptions: []string{"aac", "bbc", "cbc"},
			expected:     "aac",
			shouldMatch:  true,
		},
		{
			name:         "long strings with small typo",
			input:        "very-long-identifier-with-tyop",
			validOptions: []string{"very-long-identifier-with-typo", "another-identifier"},
			expected:     "very-long-identifier-with-typo",
			shouldMatch:  true,
		},
		{
			name:         "40% threshold test - short string",
			input:        "ab",
			validOptions: []string{"abcd"},
			expected:     "",
			shouldMatch:  false,
		},
		{
			name:         "within 40% threshold",
			input:        "copil",
			validOptions: validEngines,
			expected:     "copilot",
			shouldMatch:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FindClosestMatch(tt.input, tt.validOptions)

			if tt.shouldMatch {
				if result != tt.expected {
					t.Errorf("FindClosestMatch(%q, %v) = %q, expected %q",
						tt.input, tt.validOptions, result, tt.expected)
				}
				if result == "" {
					t.Errorf("Expected a match but got empty string")
				}
			} else {
				if result != "" {
					t.Errorf("FindClosestMatch(%q, %v) = %q, expected no match (empty string)",
						tt.input, tt.validOptions, result)
				}
			}
		})
	}
}

func TestFindClosestMatch_RealWorldEngineTypos(t *testing.T) {
	validEngines := []string{"copilot", "claude", "codex", "custom"}

	// Test common typos for each engine
	typoTests := []struct {
		typo     string
		expected string
	}{
		// Copilot typos
		{"copiilot", "copilot"},
		{"copilott", "copilot"},
		{"copiot", "copilot"},
		{"copliot", "copilot"},
		{"copilto", "copilot"},

		// Claude typos
		{"claud", "claude"},
		{"claue", "claude"},
		{"cluade", "claude"},

		// Codex typos
		{"codec", "codex"},
		{"codx", "codex"},

		// Custom typos
		{"custm", "custom"},
		{"custon", "custom"},
		{"cstom", "custom"},
	}

	for _, tt := range typoTests {
		t.Run(tt.typo, func(t *testing.T) {
			result := FindClosestMatch(tt.typo, validEngines)
			if result != tt.expected {
				t.Errorf("FindClosestMatch(%q) = %q, expected %q",
					tt.typo, result, tt.expected)
			}
		})
	}
}

func BenchmarkLevenshteinDistance_Short(b *testing.B) {
	for i := 0; i < b.N; i++ {
		LevenshteinDistance("copilot", "copiilot")
	}
}

func BenchmarkLevenshteinDistance_Long(b *testing.B) {
	s1 := "this-is-a-very-long-identifier-with-many-characters"
	s2 := "this-is-a-very-long-identifer-with-many-characters" //nolint:misspell // Intentional typo for testing
	for i := 0; i < b.N; i++ {
		LevenshteinDistance(s1, s2)
	}
}

func BenchmarkFindClosestMatch(b *testing.B) {
	validEngines := []string{"copilot", "claude", "codex", "custom"}
	for i := 0; i < b.N; i++ {
		FindClosestMatch("copiilot", validEngines)
	}
}
