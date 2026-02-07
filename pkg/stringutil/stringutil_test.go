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
