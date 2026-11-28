//go:build !integration

package cli

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsValidToolName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		toolName string
		want     bool
	}{
		{"empty string", "", false},
		{"whitespace only", "   ", false},
		{"dash placeholder", "-", false},
		{"dash with spaces trimmed", "  -  ", false},
		{"single character", "a", false},
		{"single character uppercase", "X", false},
		{"stop word calls", "calls", false},
		{"stop word to", "to", false},
		{"stop word the", "the", false},
		{"stop word Testing (capitalized)", "Testing", false},
		{"stop word after trim", "  calls  ", false},
		{"short lowercase single word no separator", "abcdef", false},
		{"short lowercase single word exactly under length limit", "abcdefghi", false}, // len 9 < 10
		{"valid tool with underscore", "run_tests", true},
		{"valid tool with hyphen", "run-tests", true},
		{"valid camelCase tool", "runTests", true},
		{"valid mixed-case name at length limit", "abcdefghiJ", true},
		{"valid long all-lowercase single word", "abcdefghij", true}, // len 10, not < 10
		{"valid multi-word name", "a b", true},
		{"valid short name with underscore", "a_b", true},
		{"valid short name with hyphen", "a-b", true},
		{"github tool name", "github_search_code", true},
		{"bash tool name", "bash", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isValidToolName(tt.toolName)
			assert.Equal(t, tt.want, got, "isValidToolName(%q)", tt.toolName)
		})
	}
}

func TestIsValidToolName_AllStopWords(t *testing.T) {
	t.Parallel()
	for word := range toolNameStopWords {
		t.Run("stopword_"+word, func(t *testing.T) {
			t.Parallel()
			assert.False(t, isValidToolName(word), "stop word %q should be invalid", word)
		})
	}
}

func FuzzIsValidToolName(f *testing.F) {
	seeds := []string{"", "-", "a", "run_tests", "run-tests", "calls", "camelCase", "   ", "abcdefghij"}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, toolName string) {
		// Must not panic, and must be deterministic (pure function).
		got1 := isValidToolName(toolName)
		got2 := isValidToolName(toolName)
		if got1 != got2 {
			t.Fatalf("isValidToolName(%q) not deterministic: %v != %v", toolName, got1, got2)
		}
		trimmed := strings.TrimSpace(toolName)
		if trimmed == "" || trimmed == "-" {
			if got1 {
				t.Fatalf("expected false for empty/dash input %q", toolName)
			}
		}
	})
}
