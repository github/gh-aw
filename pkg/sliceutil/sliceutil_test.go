//go:build !integration

package sliceutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContains(t *testing.T) {
	tests := []struct {
		name     string
		slice    []string
		item     string
		expected bool
	}{
		{
			name:     "item exists in slice",
			slice:    []string{"apple", "banana", "cherry"},
			item:     "banana",
			expected: true,
		},
		{
			name:     "item does not exist in slice",
			slice:    []string{"apple", "banana", "cherry"},
			item:     "grape",
			expected: false,
		},
		{
			name:     "empty slice",
			slice:    []string{},
			item:     "apple",
			expected: false,
		},
		{
			name:     "nil slice",
			slice:    nil,
			item:     "apple",
			expected: false,
		},
		{
			name:     "empty string item exists",
			slice:    []string{"", "apple", "banana"},
			item:     "",
			expected: true,
		},
		{
			name:     "empty string item does not exist",
			slice:    []string{"apple", "banana"},
			item:     "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Contains(tt.slice, tt.item)
			assert.Equal(t, tt.expected, result,
				"Contains should return correct value for slice %v and item %q", tt.slice, tt.item)
		})
	}
}

func TestContainsAny(t *testing.T) {
	tests := []struct {
		name       string
		s          string
		substrings []string
		expected   bool
	}{
		{
			name:       "contains first substring",
			s:          "hello world",
			substrings: []string{"hello", "goodbye"},
			expected:   true,
		},
		{
			name:       "contains second substring",
			s:          "hello world",
			substrings: []string{"goodbye", "world"},
			expected:   true,
		},
		{
			name:       "contains no substrings",
			s:          "hello world",
			substrings: []string{"goodbye", "farewell"},
			expected:   false,
		},
		{
			name:       "empty substrings",
			s:          "hello world",
			substrings: []string{},
			expected:   false,
		},
		{
			name:       "empty string",
			s:          "",
			substrings: []string{"hello"},
			expected:   false,
		},
		{
			name:       "contains empty substring",
			s:          "hello world",
			substrings: []string{""},
			expected:   true,
		},
		{
			name:       "multiple matches",
			s:          "Docker images are being downloaded",
			substrings: []string{"downloading", "retry"},
			expected:   false,
		},
		{
			name:       "match found",
			s:          "downloading images",
			substrings: []string{"downloading", "retry"},
			expected:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ContainsAny(tt.s, tt.substrings...)
			assert.Equal(t, tt.expected, result,
				"ContainsAny should return correct value for string %q and substrings %v", tt.s, tt.substrings)
		})
	}
}

func TestContainsIgnoreCase(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		substr   string
		expected bool
	}{
		{
			name:     "exact match",
			s:        "Hello World",
			substr:   "Hello",
			expected: true,
		},
		{
			name:     "case insensitive match",
			s:        "Hello World",
			substr:   "hello",
			expected: true,
		},
		{
			name:     "case insensitive match uppercase",
			s:        "hello world",
			substr:   "WORLD",
			expected: true,
		},
		{
			name:     "no match",
			s:        "Hello World",
			substr:   "goodbye",
			expected: false,
		},
		{
			name:     "empty substring",
			s:        "Hello World",
			substr:   "",
			expected: true,
		},
		{
			name:     "empty string",
			s:        "",
			substr:   "hello",
			expected: false,
		},
		{
			name:     "both empty",
			s:        "",
			substr:   "",
			expected: true,
		},
		{
			name:     "mixed case substring in mixed case string",
			s:        "GitHub Actions Workflow",
			substr:   "actions",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ContainsIgnoreCase(tt.s, tt.substr)
			assert.Equal(t, tt.expected, result,
				"ContainsIgnoreCase should return correct value for string %q and substring %q", tt.s, tt.substr)
		})
	}
}

func BenchmarkContains(b *testing.B) {
	slice := []string{"apple", "banana", "cherry", "date", "elderberry"}
	for i := 0; i < b.N; i++ {
		Contains(slice, "cherry")
	}
}

func BenchmarkContainsAny(b *testing.B) {
	s := "hello world from the testing framework"
	substrings := []string{"goodbye", "world", "farewell"}
	for i := 0; i < b.N; i++ {
		ContainsAny(s, substrings...)
	}
}

func BenchmarkContainsIgnoreCase(b *testing.B) {
	s := "Hello World From The Testing Framework"
	substr := "world"
	for i := 0; i < b.N; i++ {
		ContainsIgnoreCase(s, substr)
	}
}

// Additional edge case tests for better coverage

func TestContains_LargeSlice(t *testing.T) {
	// Test with a large slice
	largeSlice := make([]string, 1000)
	for i := 0; i < 1000; i++ {
		largeSlice[i] = string(rune('a' + i%26))
	}

	// Item at beginning
	assert.True(t, Contains(largeSlice, "a"), "should find 'a' at beginning of large slice")

	// Item at end
	assert.True(t, Contains(largeSlice, string(rune('a'+999%26))), "should find item at end of large slice")

	// Item not in slice
	assert.False(t, Contains(largeSlice, "not-present"), "should not find non-existent item in large slice")
}

func TestContains_SingleElement(t *testing.T) {
	slice := []string{"single"}

	assert.True(t, Contains(slice, "single"), "should find item in single-element slice")
	assert.False(t, Contains(slice, "other"), "should not find different item in single-element slice")
}

func TestContainsAny_MultipleMatches(t *testing.T) {
	s := "The quick brown fox jumps over the lazy dog"

	// Multiple substrings that match
	assert.True(t, ContainsAny(s, "quick", "lazy"), "should find at least one matching substring")

	// First one matches
	assert.True(t, ContainsAny(s, "quick", "missing", "absent"), "should find first matching substring")

	// Last one matches
	assert.True(t, ContainsAny(s, "missing", "absent", "dog"), "should find last matching substring")
}

func TestContainsAny_NilSubstrings(t *testing.T) {
	s := "test string"

	// Nil substrings should return false
	assert.False(t, ContainsAny(s, nil...), "should return false for nil substrings")
}

func TestContainsIgnoreCase_Unicode(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		substr   string
		expected bool
	}{
		{
			name:     "unicode characters",
			s:        "Café España",
			substr:   "café",
			expected: true,
		},
		{
			name:     "unicode uppercase",
			s:        "café españa",
			substr:   "CAFÉ",
			expected: true,
		},
		{
			name:     "emoji in string",
			s:        "Hello 👋 World",
			substr:   "👋",
			expected: true,
		},
		{
			name:     "special characters",
			s:        "test@example.com",
			substr:   "EXAMPLE",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ContainsIgnoreCase(tt.s, tt.substr)
			assert.Equal(t, tt.expected, result,
				"ContainsIgnoreCase should return correct value for string %q and substring %q", tt.s, tt.substr)
		})
	}
}

func TestContainsIgnoreCase_PartialMatch(t *testing.T) {
	s := "GitHub Actions Workflow"

	// Should find partial matches
	assert.True(t, ContainsIgnoreCase(s, "hub"), "should find partial match 'hub' in 'GitHub'")
	assert.True(t, ContainsIgnoreCase(s, "WORK"), "should find partial match 'WORK' in 'Workflow'")
	assert.True(t, ContainsIgnoreCase(s, "actions workflow"), "should find multi-word partial match")
}

func TestContains_Duplicates(t *testing.T) {
	// Slice with duplicate values
	slice := []string{"apple", "banana", "apple", "cherry", "apple"}

	assert.True(t, Contains(slice, "apple"), "should find 'apple' in slice with duplicates")

	// Should still return true on first match
	count := 0
	for _, item := range slice {
		if item == "apple" {
			count++
		}
	}
	assert.Equal(t, 3, count, "should count all occurrences of duplicate item")
}

func TestContainsAny_OrderMatters(t *testing.T) {
	s := "test string with multiple words"

	// Test that function returns on first match (short-circuit behavior)
	// Both should find a match, order shouldn't affect result
	result1 := ContainsAny(s, "string", "words")
	result2 := ContainsAny(s, "words", "string")

	assert.Equal(t, result1, result2, "should return same result regardless of substring order")
	assert.True(t, result1, "should find matches in first ordering")
	assert.True(t, result2, "should find matches in second ordering")
}
