//go:build !integration

package stringutil

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeWhitespace(t *testing.T) {
	t.Parallel()
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
		{
			name:     "windows line endings",
			content:  "hello\r\nworld\r\n",
			expected: "hello\nworld\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := NormalizeWhitespace(tt.content)
			assert.Equal(t, tt.expected, result, "NormalizeWhitespace(%q) should normalize trailing whitespace and newlines", tt.content)
		})
	}
}

func BenchmarkNormalizeWhitespace(b *testing.B) {
	content := "line1  \nline2\t\nline3   \t\nline4\n\n"
	for b.Loop() {
		NormalizeWhitespace(content)
	}
}

func TestNormalizeWhitespace_OnlyWhitespace(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			result := NormalizeWhitespace(tt.content)
			assert.Equal(t, tt.expected, result, "NormalizeWhitespace(%q) should handle whitespace-only input", tt.content)
		})
	}
}

func TestNormalizeWhitespace_ManyLines(t *testing.T) {
	t.Parallel()
	// Test with many lines
	lines := make([]string, 100)
	for i := range 100 {
		lines[i] = "line with trailing spaces  "
	}
	var content strings.Builder
	for _, line := range lines {
		content.WriteString(line + "\n")
	}

	result := NormalizeWhitespace(content.String())

	// Check that all trailing spaces are removed
	expectedLines := make([]string, 100)
	for i := range 100 {
		expectedLines[i] = "line with trailing spaces"
	}
	var expected strings.Builder
	for _, line := range expectedLines {
		expected.WriteString(line + "\n")
	}

	assert.Equal(t, expected.String(), result, "NormalizeWhitespace should remove trailing spaces from all lines in large inputs")
}

func TestNormalizeWhitespace_PreservesContent(t *testing.T) {
	t.Parallel()
	// Ensure that non-trailing whitespace is preserved
	content := "line1  middle  spaces\nline2\t\tmiddle\t\ttabs\n"
	result := NormalizeWhitespace(content)

	assert.Contains(t, result, "middle  spaces", "NormalizeWhitespace should preserve non-trailing spaces inside lines")
	assert.Contains(t, result, "middle\t\ttabs", "NormalizeWhitespace should preserve non-trailing tabs inside lines")
}

func BenchmarkNormalizeWhitespace_NoChange(b *testing.B) {
	content := "line1\nline2\nline3\n"
	for b.Loop() {
		NormalizeWhitespace(content)
	}
}

func BenchmarkNormalizeWhitespace_ManyChanges(b *testing.B) {
	content := "line1  \t  \nline2  \t  \nline3  \t  \n\n\n"
	for b.Loop() {
		NormalizeWhitespace(content)
	}
}

func TestNormalizeLeadingWhitespace(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "removes consistent leading spaces",
			input:    "          Line 1\n          Line 2\n          Line 3",
			expected: "Line 1\nLine 2\nLine 3",
		},
		{
			name:     "handles no leading spaces",
			input:    "Line 1\nLine 2",
			expected: "Line 1\nLine 2",
		},
		{
			name:     "preserves relative indentation",
			input:    "          Line 1\n            Indented Line 2\n          Line 3",
			expected: "Line 1\n  Indented Line 2\nLine 3",
		},
		{
			name:     "handles empty lines",
			input:    "          Line 1\n\n          Line 3",
			expected: "Line 1\n\nLine 3",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "removes consistent leading tabs",
			input:    "\t\tLine 1\n\t\tLine 2\n\t\tLine 3",
			expected: "Line 1\nLine 2\nLine 3",
		},
		{
			name:     "removes consistent mixed tab and space indentation",
			input:    "\t  Line 1\n\t  Line 2\n\t  Line 3",
			expected: "Line 1\nLine 2\nLine 3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := NormalizeLeadingWhitespace(tt.input)
			assert.Equal(t, tt.expected, result, "NormalizeLeadingWhitespace should normalize indentation for case %q", tt.name)
		})
	}
}
